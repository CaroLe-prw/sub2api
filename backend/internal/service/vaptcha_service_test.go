package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type vaptchaVerifierStub struct {
	result *VaptchaVerifyResponse
	err    error
}

func (v vaptchaVerifierStub) Verify(context.Context, VaptchaConfig, string, string) (*VaptchaVerifyResponse, error) {
	return v.result, v.err
}

func TestVaptchaServiceVerifyTokenWithConfig(t *testing.T) {
	config := VaptchaConfig{Enabled: true, VID: "vid", Key: "key", Scene: 0}

	t.Run("accepts successful verification", func(t *testing.T) {
		result := &VaptchaVerifyResponse{}
		result.Data.Result = true
		svc := NewVaptchaService(vaptchaVerifierStub{result: result})
		require.NoError(t, svc.VerifyTokenWithConfig(context.Background(), config, "token", "127.0.0.1"))
	})

	t.Run("rejects empty token", func(t *testing.T) {
		svc := NewVaptchaService(vaptchaVerifierStub{})
		require.ErrorIs(t, svc.VerifyTokenWithConfig(context.Background(), config, "", ""), ErrVaptchaVerificationFailed)
	})

	t.Run("rejects failed verification and request errors", func(t *testing.T) {
		failed := NewVaptchaService(vaptchaVerifierStub{result: &VaptchaVerifyResponse{}})
		require.ErrorIs(t, failed.VerifyTokenWithConfig(context.Background(), config, "token", ""), ErrVaptchaVerificationFailed)

		unavailable := NewVaptchaService(vaptchaVerifierStub{err: errors.New("network")})
		require.ErrorIs(t, unavailable.VerifyTokenWithConfig(context.Background(), config, "token", ""), ErrVaptchaVerificationFailed)
	})

	t.Run("requires credentials", func(t *testing.T) {
		svc := NewVaptchaService(vaptchaVerifierStub{})
		require.ErrorIs(t, svc.VerifyTokenWithConfig(context.Background(), VaptchaConfig{}, "token", ""), ErrVaptchaNotConfigured)
	})
}
