package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const vaptchaVerifyURL = "https://0.vaptcha.com/verify"

type vaptchaVerifier struct {
	httpClient *http.Client
	verifyURL  string
}

func NewVaptchaVerifier() service.VaptchaVerifier {
	return &vaptchaVerifier{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		verifyURL:  vaptchaVerifyURL,
	}
}

func (v *vaptchaVerifier) Verify(ctx context.Context, config service.VaptchaConfig, token, remoteIP string) (*service.VaptchaVerifyResponse, error) {
	var serverToken struct {
		Token  string `json:"token"`
		Server string `json:"server"`
	}
	verifyURL := v.verifyURL
	if json.Unmarshal([]byte(token), &serverToken) == nil && serverToken.Token != "" {
		token = serverToken.Token
		if serverToken.Server != "" {
			parsed, err := url.Parse(serverToken.Server)
			if err != nil || parsed.Scheme != "https" || !isVaptchaHost(parsed.Hostname()) {
				return nil, fmt.Errorf("invalid vaptcha verification server")
			}
			verifyURL = parsed.String()
		}
	}
	form := url.Values{
		"id":        {config.VID},
		"secretkey": {config.Key},
		"scene":     {strconv.Itoa(config.Scene)},
		"token":     {token},
		"ip":        {remoteIP},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, verifyURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create vaptcha verify request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := v.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send vaptcha verify request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("vaptcha verify returned status %d", resp.StatusCode)
	}
	var result service.VaptchaVerifyResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode vaptcha verify response: %w", err)
	}
	return &result, nil
}

func isVaptchaHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == "vaptcha.com" || strings.HasSuffix(host, ".vaptcha.com") ||
		host == "vaptcha.net" || strings.HasSuffix(host, ".vaptcha.net")
}
