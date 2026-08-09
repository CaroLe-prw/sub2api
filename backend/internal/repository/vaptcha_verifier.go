package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const vaptchaVerifyURL = "https://v41.vaptcha.com/api/verify"

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
	var proof struct {
		Token string `json:"token"`
		Knock string `json:"knock"`
		DFU   string `json:"dfu"`
		IP    string `json:"ip"`
	}
	if err := json.Unmarshal([]byte(token), &proof); err != nil {
		return nil, fmt.Errorf("decode vaptcha v4 proof: %w", err)
	}
	if strings.TrimSpace(proof.Token) == "" || strings.TrimSpace(proof.Knock) == "" {
		return nil, fmt.Errorf("incomplete vaptcha v4 proof")
	}
	if strings.TrimSpace(proof.IP) == "" {
		proof.IP = remoteIP
	}
	payload := struct {
		VID   string `json:"vid"`
		VKey  string `json:"vkey"`
		Token string `json:"token"`
		Knock string `json:"knock"`
		DFU   string `json:"dfu"`
		IP    string `json:"ip"`
	}{
		VID:   config.VID,
		VKey:  config.Key,
		Token: proof.Token,
		Knock: proof.Knock,
		DFU:   proof.DFU,
		IP:    proof.IP,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode vaptcha verify request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.verifyURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create vaptcha verify request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
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
