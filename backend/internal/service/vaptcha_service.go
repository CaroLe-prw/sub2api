package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
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
	Success int    `json:"success"`
	Msg     string `json:"msg"`
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
		return fmt.Errorf("%w: verifier request failed", ErrVaptchaVerificationFailed)
	}
	if result == nil || result.Success != 1 {
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
