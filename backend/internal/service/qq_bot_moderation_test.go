package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/qqbot"
	"github.com/stretchr/testify/require"
)

func TestQQImageAdOCRAndSafeFailure(t *testing.T) {
	var message qqbot.Message
	// Build the same attachment representation as a QQ group event.
	message.Attachments = append(message.Attachments, struct {
		ContentType string `json:"content_type"`
		URL         string `json:"url"`
	}{"image/png", "https://multimedia.nt.qq.com.cn/download?fileid=test"})
	cfg := qqbot.Config{Moderation: qqbot.ModerationConfig{ScanImages: true}}
	// Actual local OCR output from the user-provided ad poster (not a model guess).
	ocr := func(context.Context, string) (string, error) {
		return "挖 矿 ， 好 玩 还 能 赚 现 金 !\n当前 在 线 : 81741 人\n长 按 图 片 识 别 关注\n首 月 可 挖 300-1000 元", nil
	}
	decision, err := reviewQQAdvertisement(context.Background(), cfg, message, ocr)
	require.NoError(t, err)
	require.Equal(t, "advertisement", decision.Verdict)
	message.Content = "不要相信这种诈骗广告"
	decision, err = reviewQQAdvertisement(context.Background(), cfg, message, ocr)
	require.NoError(t, err)
	require.Equal(t, "suspected", decision.Verdict)
	message.Content = ""
	decision, err = reviewQQAdvertisement(context.Background(), cfg, message, func(context.Context, string) (string, error) { return "", errors.New("unavailable") })
	require.NoError(t, err)
	require.Equal(t, "suspected", decision.Verdict)
	decision, err = reviewQQAdvertisement(context.Background(), cfg, message, func(context.Context, string) (string, error) { return "模型使用教程 扫码查看", nil })
	require.NoError(t, err)
	require.Equal(t, "allowed", decision.Verdict)
}

func TestQQAdImageURLRestrictions(t *testing.T) {
	for _, raw := range []string{"http://qpic.cn/a", "https://127.0.0.1/a", "https://qq.com.attacker.example/a", "https://user:pass@qq.com/a", "file:///etc/passwd", "https://qq.com:8443/a"} {
		require.False(t, qqImageURL(raw), raw)
	}
	require.True(t, qqImageURL("https://multimedia.nt.qq.com.cn/download?fileid=123"))
	require.True(t, qqImageURL("https://gchat.qpic.cn/a"))
}
