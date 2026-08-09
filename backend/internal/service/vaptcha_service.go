package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

var (
	ErrVaptchaVerificationFailed = infraerrors.BadRequest("VAPTCHA_VERIFICATION_FAILED", "vaptcha verification failed")
	ErrVaptchaNotConfigured      = infraerrors.ServiceUnavailable("VAPTCHA_NOT_CONFIGURED", "vaptcha not configured")
)

type VaptchaConfig struct {
	Enabled bool
	VID     string
	Key     string
	Scene   int
}

type VaptchaVerifyResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Code   int    `json:"code"`
		Note   string `json:"note"`
		Result bool   `json:"result"`
		VID    string `json:"vid"`
	} `json:"data"`
}

type VaptchaVerifier interface {
	Verify(ctx context.Context, config VaptchaConfig, token, remoteIP string) (*VaptchaVerifyResponse, error)
}

type VaptchaService struct {
	verifier VaptchaVerifier
}

func NewVaptchaService(verifier VaptchaVerifier) *VaptchaService {
	return &VaptchaService{verifier: verifier}
}

func (s *VaptchaService) VerifyTokenWithConfig(ctx context.Context, config VaptchaConfig, token, remoteIP string) error {
	if s == nil || s.verifier == nil || strings.TrimSpace(config.VID) == "" || strings.TrimSpace(config.Key) == "" {
		return ErrVaptchaNotConfigured
	}
	if strings.TrimSpace(token) == "" {
		return ErrVaptchaVerificationFailed
	}
	result, err := s.verifier.Verify(ctx, config, token, remoteIP)
	if err != nil {
		logger.LegacyPrintf("service.vaptcha", "[VAPTCHA] verify request failed: %v", err)
		return fmt.Errorf("%w: verifier request failed", ErrVaptchaVerificationFailed)
	}
	if result == nil || result.Code != 0 || result.Data.Code != 0 || !result.Data.Result {
		if result != nil {
			logger.LegacyPrintf(
				"service.vaptcha",
				"[VAPTCHA] verify rejected: response_code=%d verify_code=%d note=%q",
				result.Code,
				result.Data.Code,
				result.Data.Note,
			)
		}
		return ErrVaptchaVerificationFailed
	}
	return nil
}

func parseVaptchaScene(value string) int {
	scene, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || scene < 0 {
		return 0
	}
	return scene
}
