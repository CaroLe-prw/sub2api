package qqbot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type client struct {
	http                   *http.Client
	baseURL, appID, secret string
	mu                     sync.Mutex
	token                  string
	expires                time.Time
}

type httpStatusError int

func (e httpStatusError) Error() string { return fmt.Sprintf("HTTP %d", int(e)) }

// doJSON never follows redirects or includes response bodies in errors: those may
// contain credentials or upstream details inappropriate for a group or log.
func doJSON(ctx context.Context, client *http.Client, method, endpoint string, headers map[string]string, input, output any) error {
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return fmt.Errorf("invalid request URL")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed (network or timeout)")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return httpStatusError(resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024+1))
	if err != nil || len(data) > 2*1024*1024 {
		return fmt.Errorf("invalid or oversized response")
	}
	if err := json.Unmarshal(data, output); err != nil {
		return fmt.Errorf("invalid JSON response")
	}
	return nil
}

func (q *client) accessToken(ctx context.Context) (string, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.token != "" && time.Now().Before(q.expires) {
		return q.token, nil
	}
	var data struct {
		Token   string          `json:"access_token"`
		Expires json.RawMessage `json:"expires_in"`
		Code    int             `json:"code"`
	}
	err := doJSON(ctx, q.http, http.MethodPost, q.baseURL+"/app/getAppAccessToken", nil,
		map[string]string{"appId": q.appID, "clientSecret": q.secret}, &data)
	if err != nil {
		return "", err
	}
	if data.Code != 0 {
		return "", fmt.Errorf("QQ access token code=%d", data.Code)
	}
	seconds, err := strconv.ParseInt(strings.Trim(string(data.Expires), "\""), 10, 64)
	if err != nil || seconds <= 0 || seconds > 86400 || data.Token == "" {
		return "", fmt.Errorf("invalid QQ access token response")
	}
	q.token = data.Token
	q.expires = time.Now().Add(time.Duration(seconds)*time.Second - 60*time.Second)
	return q.token, nil
}

func (q *client) reply(ctx context.Context, group, messageID, content string) error {
	return q.replyText(ctx, group, messageID, content, 1)
}

func (q *client) replyText(ctx context.Context, group, messageID, content string, sequence int) error {
	token, err := q.accessToken(ctx)
	if err != nil {
		return err
	}
	var result struct {
		Code    int    `json:"code"`
		ErrCode int    `json:"err_code"`
		ID      string `json:"id"`
	}
	err = doJSON(ctx, q.http, http.MethodPost, q.baseURL+"/v2/groups/"+url.PathEscape(group)+"/messages",
		map[string]string{"Authorization": "QQBot " + token},
		map[string]any{"msg_type": 0, "msg_id": messageID, "msg_seq": sequence, "content": content}, &result)
	if err != nil {
		return err
	}
	if result.Code != 0 || result.ErrCode != 0 || result.ID == "" {
		return fmt.Errorf("QQ reply failed code=%d err_code=%d", result.Code, result.ErrCode)
	}
	return nil
}

func httpClient() *http.Client {
	return &http.Client{Timeout: 125 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
}
