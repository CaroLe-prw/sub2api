package service

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/qqbot"
	_ "golang.org/x/image/webp"
)

const qqOCRMaxBytes = 5 * 1024 * 1024

var qqOCRSlots = make(chan struct{}, 2)

func qqOCRAvailable(ctx context.Context) bool {
	checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(checkCtx, "tesseract", "--list-langs")
	var out qqOCROutput
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if cmd.Run() != nil {
		return false
	}
	hasEnglish, hasChinese := false, false
	for _, line := range strings.Fields(out.String()) {
		if line == "eng" {
			hasEnglish = true
		}
		if line == "chi_sim" {
			hasChinese = true
		}
	}
	return hasEnglish && hasChinese
}

type qqOCROutput struct{ bytes.Buffer }

func (w *qqOCROutput) Write(p []byte) (int, error) {
	if w.Len()+len(p) > 32768 {
		return 0, errors.New("OCR output limit exceeded")
	}
	return w.Buffer.Write(p)
}

func qqImageURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Host == "" || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, domain := range []string{"qq.com", "qq.com.cn", "qpic.cn", "gtimg.cn", "gtimg.com"} {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

func qqReadImageText(ctx context.Context, rawURL string) (string, error) {
	if !qqImageURL(rawURL) {
		return "", errors.New("unsupported QQ attachment URL")
	}
	select {
	case qqOCRSlots <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-qqOCRSlots }()
	ocrCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	client := newSSRFSafeHTTPClient(10 * time.Second)
	defer client.CloseIdleConnections()
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ocrCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", errors.New("invalid image request")
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", errors.New("image download failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", errors.New("image download rejected")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, qqOCRMaxBytes+1))
	if err != nil || len(data) > qqOCRMaxBytes {
		return "", errors.New("image too large or unreadable")
	}
	info, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || info.Width < 1 || info.Height < 1 || int64(info.Width)*int64(info.Height) > 12_000_000 {
		return "", errors.New("unsupported image dimensions")
	}
	// Re-encode only validated images so the local OCR process never sees a
	// PDF, SVG, URL list or other input interpreted as additional file paths.
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", errors.New("invalid image")
	}
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, decoded); err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ocrCtx, "tesseract", "stdin", "stdout", "-l", "chi_sim+eng", "--psm", "11")
	cmd.Env = append(os.Environ(), "OMP_THREAD_LIMIT=1")
	cmd.WaitDelay = time.Second
	cmd.Stdin = &pngData
	var output qqOCROutput
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return "", errors.New("local OCR unavailable or failed")
	}
	return output.String(), nil
}

func (s *QQBotService) ReviewMessage(ctx context.Context, cfg qqbot.Config, msg qqbot.Message) (qqbot.AdDecision, error) {
	return reviewQQAdvertisement(ctx, cfg, msg, qqReadImageText)
}

func reviewQQAdvertisement(ctx context.Context, cfg qqbot.Config, msg qqbot.Message, readImage func(context.Context, string) (string, error)) (qqbot.AdDecision, error) {
	decision := qqbot.EvaluateAdText(msg.Content)
	if decision.Verdict == "advertisement" {
		return decision, nil
	}
	var texts []string
	imageCount := 0
	unreadable := false
	for _, attachment := range msg.Attachments {
		if !strings.HasPrefix(attachment.ContentType, "image/") {
			continue
		}
		imageCount++
		if !cfg.Moderation.ScanImages || imageCount > 2 {
			unreadable = true
			continue
		}
		text, err := readImage(ctx, attachment.URL)
		if err != nil || strings.TrimSpace(text) == "" {
			unreadable = true
			continue
		}
		texts = append(texts, text)
	}
	if len(texts) > 0 {
		// Include the caption so a warning/report of a scam is kept for review.
		decision = qqbot.EvaluateAdText(msg.Content + "\n" + strings.Join(texts, "\n"))
	}
	if unreadable && decision.Verdict != "advertisement" {
		decision = qqbot.AdDecision{Verdict: "suspected", Reason: "含未识别图片，无法可靠判断，仅记录"}
	}
	return decision, nil
}

func (s *QQBotService) RecordModeration(ctx context.Context, record qqbot.ModerationRecord) error {
	return s.cache.RecordModeration(ctx, record)
}
func (s *QQBotService) ModerationRecords(ctx context.Context) ([]qqbot.ModerationRecord, error) {
	return s.cache.ModerationRecords(ctx)
}
