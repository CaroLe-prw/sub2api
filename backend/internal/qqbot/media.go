package qqbot

import (
	"bytes"
	"context"
	"crypto/md5" // QQ media protocol requires these checksums, not used for security.
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

type apiResult struct {
	Code    int `json:"code"`
	ErrCode int `json:"err_code"`
}

func (r apiResult) check() error {
	if r.Code != 0 || r.ErrCode != 0 {
		return fmt.Errorf("QQ media code=%d err_code=%d", r.Code, r.ErrCode)
	}
	return nil
}

type uploadPart struct {
	Index int             `json:"index"`
	URL   string          `json:"presigned_url"`
	Size  json.RawMessage `json:"block_size"`
}

func mediaSize(raw json.RawMessage) (int, error) {
	n, err := strconv.ParseInt(strings.Trim(string(raw), "\""), 10, 32)
	if err != nil || n <= 0 {
		return 0, errors.New("invalid QQ upload block size")
	}
	return int(n), nil
}
func md5hex(data []byte) string { sum := md5.Sum(data); return hex.EncodeToString(sum[:]) }

// uploadImage sends local bytes directly using QQ's presigned upload protocol.
// No public image URL or new application endpoint is needed.
func (q *client) uploadImage(ctx context.Context, group string, data []byte) (info string, err error) {
	stage := "validate_image"
	defer func() {
		if err != nil {
			err = fmt.Errorf("%s: %w", stage, err)
		}
	}()
	if len(data) == 0 || len(data) > 8*1024*1024 {
		return "", errors.New("invalid board image size")
	}
	stage = "access_token"
	token, err := q.accessToken(ctx)
	if err != nil {
		return "", err
	}
	headers := map[string]string{"Authorization": "QQBot " + token}
	base := q.baseURL + "/v2/groups/" + url.PathEscape(group)
	sha := sha1.Sum(data)
	var prepare struct {
		apiResult
		ID        string          `json:"upload_id"`
		BlockSize json.RawMessage `json:"block_size"`
		Parts     []uploadPart    `json:"parts"`
	}
	stage = "upload_prepare"
	err = doJSON(ctx, q.http, http.MethodPost, base+"/upload_prepare", headers, map[string]any{"file_type": 1, "file_size": strconv.Itoa(len(data)), "file_name": "channel-status.png", "md5": md5hex(data), "sha1": hex.EncodeToString(sha[:]), "md5_10m": md5hex(data)}, &prepare)
	if err != nil {
		return "", err
	}
	if err = prepare.check(); err != nil {
		return "", err
	}
	stage = "validate_upload_plan"
	if prepare.ID == "" || len(prepare.Parts) == 0 || len(prepare.Parts) > 32 {
		return "", errors.New("invalid QQ upload plan")
	}
	sort.Slice(prepare.Parts, func(i, j int) bool { return prepare.Parts[i].Index < prepare.Parts[j].Index })
	// The current official SDK uses one-based indices; older wiki examples
	// use zero-based indices. Preserve whichever valid sequence QQ returned.
	firstIndex := prepare.Parts[0].Index
	if firstIndex != 0 && firstIndex != 1 {
		return "", errors.New("invalid QQ upload starting index")
	}
	// Validate the complete plan before transmitting any bytes. No credentials
	// are forwarded to the storage host, and redirects are never followed.
	type chunk struct {
		url   string
		data  []byte
		index int
	}
	chunks := make([]chunk, 0, len(prepare.Parts))
	offset := 0
	for i, part := range prepare.Parts {
		if part.Index != firstIndex+i || offset >= len(data) {
			return "", errors.New("invalid QQ upload part order")
		}
		raw := part.Size
		sizeValue := strings.Trim(strings.TrimSpace(string(raw)), "\"")
		if sizeValue == "" || sizeValue == "0" || sizeValue == "null" {
			raw = prepare.BlockSize
		}
		size, e := mediaSize(raw)
		if e != nil {
			return "", e
		}
		end := offset + size
		if end > len(data) {
			end = len(data)
		}
		u, e := url.Parse(part.URL)
		if e != nil || u.User != nil || !trustedUploadURL(u) {
			return "", errors.New("untrusted QQ storage URL")
		}
		chunks = append(chunks, chunk{part.URL, data[offset:end], part.Index})
		offset = end
	}
	if offset != len(data) {
		return "", errors.New("incomplete QQ upload plan")
	}
	for _, part := range chunks {
		stage = fmt.Sprintf("upload_part_%d", part.index)
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, part.url, bytes.NewReader(part.data))
		if err != nil {
			return "", errors.New("invalid QQ storage request")
		}
		req.Header.Set("Content-Type", "application/octet-stream")
		resp, err := q.http.Do(req)
		if err != nil {
			return "", errors.New("QQ image upload failed")
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return "", httpStatusError(resp.StatusCode)
		}
		var finish apiResult
		stage = fmt.Sprintf("upload_part_finish_%d", part.index)
		if err := doJSON(ctx, q.http, http.MethodPost, base+"/upload_part_finish", headers, map[string]any{"upload_id": prepare.ID, "part_index": part.index, "block_size": strconv.Itoa(len(part.data)), "md5": md5hex(part.data)}, &finish); err != nil {
			return "", err
		}
		if err := finish.check(); err != nil {
			return "", err
		}
	}
	var merged struct {
		apiResult
		Info string `json:"file_info"`
	}
	stage = "complete_upload"
	if err := doJSON(ctx, q.http, http.MethodPost, base+"/files", headers, map[string]any{"file_type": 1, "upload_id": prepare.ID, "srv_send_msg": false, "file_name": "channel-status.png"}, &merged); err != nil {
		return "", err
	}
	if err := merged.check(); err != nil {
		return "", err
	}
	if merged.Info == "" {
		return "", errors.New("missing QQ image file info")
	}
	return merged.Info, nil
}

func trustedUploadURL(u *url.URL) bool {
	if u.Scheme != "https" || u.Host == "" || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, domain := range []string{"myqcloud.com", "qcloud.com", "qq.com", "qpic.cn"} {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

func (q *client) replyImage(ctx context.Context, group, messageID string, data []byte, sequence int) error {
	info, err := q.uploadImage(ctx, group, data)
	if err != nil {
		return err
	}
	token, err := q.accessToken(ctx)
	if err != nil {
		return err
	}
	var result struct {
		apiResult
		ID string `json:"id"`
	}
	err = doJSON(ctx, q.http, http.MethodPost, q.baseURL+"/v2/groups/"+url.PathEscape(group)+"/messages", map[string]string{"Authorization": "QQBot " + token}, map[string]any{"msg_type": 7, "msg_id": messageID, "msg_seq": sequence, "media": map[string]string{"file_info": info}}, &result)
	if err != nil {
		return fmt.Errorf("send_image_message: %w", err)
	}
	if err = result.check(); err != nil {
		return err
	}
	if result.ID == "" {
		return errors.New("QQ image reply missing ID")
	}
	return nil
}
