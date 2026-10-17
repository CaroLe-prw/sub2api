//go:build unit

package admin

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGoCaptchaSettingsEnablePreserveAndDisable(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
	rec := doUpdateSettings(t, h, map[string]any{"go_captcha_enabled": true}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "true", repo.values[service.SettingKeyGoCaptchaEnabled])
	require.Contains(t, rec.Body.String(), `"go_captcha_enabled":true`)
	rec = doUpdateSettings(t, h, map[string]any{"site_name": "Test"}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "true", repo.values[service.SettingKeyGoCaptchaEnabled])
	rec = doUpdateSettings(t, h, map[string]any{"go_captcha_enabled": false}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "false", repo.values[service.SettingKeyGoCaptchaEnabled])
}

func TestGoCaptchaSettingsCannotCoexistWithOtherProviders(t *testing.T) {
	for _, provider := range []string{
		service.SettingKeyTurnstileEnabled, service.SettingKeyTencentCaptchaEnabled,
		service.SettingKeyAliyunCaptchaEnabled, service.SettingKeyVaptchaEnabled,
	} {
		t.Run(provider, func(t *testing.T) {
			h, repo := newStepUpSwitchTestHandler(t, map[string]string{provider: "true"})
			rec := doUpdateSettings(t, h, map[string]any{"go_captcha_enabled": true}, nil)
			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.NotEqual(t, "true", repo.values[service.SettingKeyGoCaptchaEnabled])
			rec = doUpdateSettings(t, h, map[string]any{"go_captcha_enabled": true, provider: false}, nil)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.Equal(t, "true", repo.values[service.SettingKeyGoCaptchaEnabled])
		})
	}
}
