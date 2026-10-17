package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"math"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrGoCaptchaVerificationFailed = infraerrors.BadRequest("GO_CAPTCHA_VERIFICATION_FAILED", "captcha verification failed or expired, please try again")
	ErrGoCaptchaNotConfigured      = infraerrors.ServiceUnavailable("GO_CAPTCHA_NOT_CONFIGURED", "GoCaptcha is not enabled or available")
)

const goCaptchaTTL = 2 * time.Minute

// GoCaptchaChallenge contains only the images and initial puzzle position. The
// target position must stay on the server, including when serializing responses.
type GoCaptchaChallenge struct {
	ID          string `json:"id"`
	Image       string `json:"image"`
	Thumb       string `json:"thumb"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	ThumbWidth  int    `json:"thumb_width"`
	ThumbHeight int    `json:"thumb_height"`
	ThumbX      int    `json:"thumb_x"`
	ThumbY      int    `json:"thumb_y"`
}

type GoCaptchaAnswer struct {
	X  int    `json:"x"`
	Y  int    `json:"y"`
	IP string `json:"ip"`
}

type GoCaptchaGenerator interface {
	Generate(context.Context) (*GoCaptchaChallenge, GoCaptchaAnswer, error)
}

type GoCaptchaStore interface {
	Put(context.Context, string, string, time.Duration) error
	// Take must atomically read and delete the value; missing/expired keys return "".
	Take(context.Context, string) (string, error)
}

type GoCaptchaService struct {
	settings  *SettingService
	generator GoCaptchaGenerator
	store     GoCaptchaStore
}

func NewGoCaptchaService(settings *SettingService, generator GoCaptchaGenerator, store GoCaptchaStore) *GoCaptchaService {
	return &GoCaptchaService{settings: settings, generator: generator, store: store}
}

func (s *GoCaptchaService) checkEnabled(ctx context.Context) error {
	if s == nil || s.settings == nil || s.generator == nil || s.store == nil {
		return ErrGoCaptchaNotConfigured
	}
	cfg, err := s.settings.GetCaptchaProviderConfig(ctx)
	if err != nil {
		return ErrServiceUnavailable
	}
	if captchaProvidersConflict(cfg.TurnstileEnabled, cfg.Tencent.Enabled, cfg.Aliyun.Enabled, cfg.Vaptcha.Enabled, cfg.GoCaptchaEnabled) {
		return ErrCaptchaProviderConflict
	}
	if !cfg.GoCaptchaEnabled {
		return ErrGoCaptchaNotConfigured
	}
	return nil
}

func (s *GoCaptchaService) CreateChallenge(ctx context.Context, remoteIP string) (*GoCaptchaChallenge, error) {
	if err := s.checkEnabled(ctx); err != nil {
		return nil, err
	}
	challenge, answer, err := s.generator.Generate(ctx)
	if err != nil || challenge == nil {
		return nil, ErrServiceUnavailable
	}
	challenge.ID = newGoCaptchaToken()
	answer.IP = remoteIP
	data, err := json.Marshal(answer)
	if err != nil {
		return nil, ErrServiceUnavailable
	}
	if err := s.store.Put(ctx, "challenge:"+challenge.ID, string(data), goCaptchaTTL); err != nil {
		return nil, ErrServiceUnavailable
	}
	return challenge, nil
}

func (s *GoCaptchaService) CheckChallenge(ctx context.Context, id string, x, y float64, remoteIP string) (string, error) {
	if err := s.checkEnabled(ctx); err != nil {
		return "", err
	}
	if !validGoCaptchaToken(id) {
		return "", ErrGoCaptchaVerificationFailed
	}
	// Even a wrong answer consumes the challenge, preventing repeated guesses.
	raw, err := s.store.Take(ctx, "challenge:"+id)
	if err != nil {
		return "", ErrServiceUnavailable
	}
	var answer GoCaptchaAnswer
	if raw == "" || json.Unmarshal([]byte(raw), &answer) != nil || answer.IP != remoteIP ||
		math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) ||
		math.Abs(x-float64(answer.X)) > 5 || math.Abs(y-float64(answer.Y)) > 5 {
		return "", ErrGoCaptchaVerificationFailed
	}
	token := newGoCaptchaToken()
	if err := s.store.Put(ctx, "proof:"+token, remoteIP, goCaptchaTTL); err != nil {
		return "", ErrServiceUnavailable
	}
	return token, nil
}

// VerifyToken is called only after AuthService selects the enabled provider.
// Challenge IDs and business-action proofs use separate namespaces.
func (s *GoCaptchaService) VerifyToken(ctx context.Context, token, remoteIP string) error {
	if s == nil || s.store == nil {
		return ErrGoCaptchaNotConfigured
	}
	if !validGoCaptchaToken(token) || remoteIP == "" {
		return ErrGoCaptchaVerificationFailed
	}
	ip, err := s.store.Take(ctx, "proof:"+token)
	if err != nil {
		return ErrServiceUnavailable
	}
	if ip == "" || ip != remoteIP {
		return ErrGoCaptchaVerificationFailed
	}
	return nil
}

func newGoCaptchaToken() string {
	var b [32]byte
	// crypto/rand.Read always fills b and fails irrecoverably if entropy is unavailable.
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func validGoCaptchaToken(token string) bool {
	if len(token) != 64 {
		return false
	}
	_, err := hex.DecodeString(token)
	return err == nil
}

func (s *AuthService) CreateGoCaptchaChallenge(ctx context.Context, remoteIP string) (*GoCaptchaChallenge, error) {
	return s.goCaptchaService.CreateChallenge(ctx, remoteIP)
}

func (s *AuthService) CheckGoCaptchaChallenge(ctx context.Context, id string, x, y float64, remoteIP string) (string, error) {
	return s.goCaptchaService.CheckChallenge(ctx, id, x, y, remoteIP)
}
