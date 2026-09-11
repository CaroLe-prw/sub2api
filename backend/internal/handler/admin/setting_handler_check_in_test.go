package admin

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUpdateSettingsCheckInRechargeRequirement(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
	for _, tc := range []struct {
		body   map[string]any
		want   float64
		stored string
	}{
		{map[string]any{"check_in_min_recharge": 10.25}, 10.25, "10.25000000"},
		{map[string]any{"check_in_reward_min": 0.02}, 10.25, "10.25000000"},
		{map[string]any{"check_in_min_recharge": 0}, 0, "0.00000000"},
	} {
		rec := doUpdateSettings(t, h, tc.body, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Equal(t, tc.stored, repo.values[service.SettingKeyCheckInMinRecharge])
		var resp struct {
			Data struct {
				MinRecharge float64 `json:"check_in_min_recharge"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		require.Equal(t, tc.want, resp.Data.MinRecharge)
	}
}

func TestUpdateSettingsCheckInRejectsInvalidRechargeRequirement(t *testing.T) {
	for _, amount := range []float64{-1, 1e13} {
		h, repo := newStepUpSwitchTestHandler(t, map[string]string{service.SettingKeyCheckInMinRecharge: "10"})
		rec := doUpdateSettings(t, h, map[string]any{"check_in_min_recharge": amount}, nil)
		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Equal(t, "10", repo.values[service.SettingKeyCheckInMinRecharge])
	}
}
