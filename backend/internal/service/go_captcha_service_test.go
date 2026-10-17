//go:build unit

package service

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type goCaptchaGeneratorStub struct{}

func (*goCaptchaGeneratorStub) Generate(context.Context) (*GoCaptchaChallenge, GoCaptchaAnswer, error) {
	return &GoCaptchaChallenge{Width: 300, Height: 220, ThumbX: 5, ThumbY: 40}, GoCaptchaAnswer{X: 150, Y: 40}, nil
}

type goCaptchaStoreStub struct {
	values map[string]string
	ttls   map[string]time.Duration
	err    error
}

func (s *goCaptchaStoreStub) Put(_ context.Context, key, value string, ttl time.Duration) error {
	if s.err != nil {
		return s.err
	}
	s.values[key] = value
	s.ttls[key] = ttl
	return nil
}

func (s *goCaptchaStoreStub) Take(_ context.Context, key string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	value := s.values[key]
	delete(s.values, key)
	return value, nil
}

func newGoCaptchaTestService() (*GoCaptchaService, *goCaptchaStoreStub) {
	settings := NewSettingService(&settingRepoStub{values: map[string]string{SettingKeyGoCaptchaEnabled: "true"}}, &config.Config{})
	store := &goCaptchaStoreStub{values: map[string]string{}, ttls: map[string]time.Duration{}}
	return NewGoCaptchaService(settings, &goCaptchaGeneratorStub{}, store), store
}

func TestGoCaptchaChallengeAndProofAreSingleUse(t *testing.T) {
	svc, store := newGoCaptchaTestService()
	ctx := context.Background()
	challenge, err := svc.CreateChallenge(ctx, "203.0.113.1")
	require.NoError(t, err)
	require.Len(t, challenge.ID, 64)
	require.Equal(t, 2*time.Minute, store.ttls["challenge:"+challenge.ID])
	// An unverified challenge ID is not a business-action proof.
	require.ErrorIs(t, svc.VerifyToken(ctx, challenge.ID, "203.0.113.1"), ErrGoCaptchaVerificationFailed)
	token, err := svc.CheckChallenge(ctx, challenge.ID, 154, 40, "203.0.113.1")
	require.NoError(t, err)
	require.NotEqual(t, token, challenge.ID)
	require.Equal(t, 2*time.Minute, store.ttls["proof:"+token])
	_, err = svc.CheckChallenge(ctx, challenge.ID, 150, 40, "203.0.113.1")
	require.ErrorIs(t, err, ErrGoCaptchaVerificationFailed)
	require.NoError(t, svc.VerifyToken(ctx, token, "203.0.113.1"))
	require.ErrorIs(t, svc.VerifyToken(ctx, token, "203.0.113.1"), ErrGoCaptchaVerificationFailed)
}

func TestGoCaptchaWrongAnswersConsumeChallenge(t *testing.T) {
	for _, tc := range []struct {
		name string
		x, y float64
		ip   string
	}{
		{"wrong x", 156, 40, "203.0.113.1"},
		{"wrong y", 150, 46, "203.0.113.1"},
		{"different ip", 150, 40, "203.0.113.2"},
		{"nan", math.NaN(), 40, "203.0.113.1"},
		{"infinity", 150, math.Inf(1), "203.0.113.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := newGoCaptchaTestService()
			ctx := context.Background()
			challenge, err := svc.CreateChallenge(ctx, "203.0.113.1")
			require.NoError(t, err)
			_, err = svc.CheckChallenge(ctx, challenge.ID, tc.x, tc.y, tc.ip)
			require.ErrorIs(t, err, ErrGoCaptchaVerificationFailed)
			_, err = svc.CheckChallenge(ctx, challenge.ID, 150, 40, "203.0.113.1")
			require.ErrorIs(t, err, ErrGoCaptchaVerificationFailed)
		})
	}
}

func TestGoCaptchaProofBoundToIPAndStoreFailsClosed(t *testing.T) {
	svc, store := newGoCaptchaTestService()
	ctx := context.Background()
	challenge, err := svc.CreateChallenge(ctx, "203.0.113.1")
	require.NoError(t, err)
	token, err := svc.CheckChallenge(ctx, challenge.ID, 150, 40, "203.0.113.1")
	require.NoError(t, err)
	require.ErrorIs(t, svc.VerifyToken(ctx, token, "203.0.113.2"), ErrGoCaptchaVerificationFailed)
	require.ErrorIs(t, svc.VerifyToken(ctx, token, "203.0.113.1"), ErrGoCaptchaVerificationFailed)
	store.err = errors.New("cache unavailable")
	_, err = svc.CreateChallenge(ctx, "203.0.113.1")
	require.ErrorIs(t, err, ErrServiceUnavailable)
	require.ErrorIs(t, svc.VerifyToken(ctx, token, "203.0.113.1"), ErrServiceUnavailable)
}

func TestGoCaptchaAuthEntryPointsAndProviderConflict(t *testing.T) {
	for _, action := range []bool{false, true} {
		svc, _ := newGoCaptchaTestService()
		auth := &AuthService{settingService: svc.settings, goCaptchaService: svc}
		verify := auth.VerifyCaptcha
		if action {
			verify = auth.VerifyActionCaptchaIfEnabled
		}
		ctx := context.Background()
		require.ErrorIs(t, verify(ctx, CaptchaProof{}, "203.0.113.1"), ErrGoCaptchaVerificationFailed)
		challenge, err := svc.CreateChallenge(ctx, "203.0.113.1")
		require.NoError(t, err)
		token, err := svc.CheckChallenge(ctx, challenge.ID, 150, 40, "203.0.113.1")
		require.NoError(t, err)
		require.NoError(t, verify(ctx, CaptchaProof{TurnstileToken: token}, "203.0.113.1"))
		require.ErrorIs(t, verify(ctx, CaptchaProof{TurnstileToken: token}, "203.0.113.1"), ErrGoCaptchaVerificationFailed)
		auth.goCaptchaService = nil
		require.ErrorIs(t, verify(ctx, CaptchaProof{}, "203.0.113.1"), ErrGoCaptchaNotConfigured)
		auth.settingService = NewSettingService(&settingRepoStub{values: map[string]string{
			SettingKeyGoCaptchaEnabled: "true", SettingKeyTurnstileEnabled: "true",
		}}, &config.Config{})
		require.ErrorIs(t, verify(ctx, CaptchaProof{}, "203.0.113.1"), ErrCaptchaProviderConflict)
	}
}

func TestGoCaptchaChallengeDisabledOrMisconfigured(t *testing.T) {
	svc, _ := newGoCaptchaTestService()
	for _, values := range []map[string]string{
		{}, {SettingKeyGoCaptchaEnabled: "true", SettingKeyTencentCaptchaEnabled: "true"},
	} {
		svc.settings = NewSettingService(&settingRepoStub{values: values}, &config.Config{})
		challenge, err := svc.CreateChallenge(context.Background(), "203.0.113.1")
		require.Error(t, err)
		require.Nil(t, challenge)
	}
}
