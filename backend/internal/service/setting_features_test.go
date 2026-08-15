package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type lotteryToggleSettingRepoStub struct {
	SettingRepository
	setErr       error
	lastSetKey   string
	lastSetValue string
}

func (s *lotteryToggleSettingRepoStub) Set(_ context.Context, key, value string) error {
	s.lastSetKey = key
	s.lastSetValue = value
	return s.setErr
}

func TestSetLotteryEnabledInvalidatesPublicSettingsCache(t *testing.T) {
	repo := &lotteryToggleSettingRepoStub{}
	settings := &SettingService{settingRepo: repo}
	invalidations := 0
	settings.SetOnUpdateCallback(func() { invalidations++ })

	require.NoError(t, settings.SetLotteryEnabled(context.Background(), true))
	require.Equal(t, SettingKeyLotteryEnabled, repo.lastSetKey)
	require.Equal(t, "true", repo.lastSetValue)
	require.Equal(t, 1, invalidations)
}

func TestSetLotteryEnabledDoesNotInvalidateCacheWhenSaveFails(t *testing.T) {
	repo := &lotteryToggleSettingRepoStub{setErr: errors.New("save failed")}
	settings := &SettingService{settingRepo: repo}
	invalidations := 0
	settings.SetOnUpdateCallback(func() { invalidations++ })

	err := settings.SetLotteryEnabled(context.Background(), false)
	require.EqualError(t, err, "save failed")
	require.Equal(t, 0, invalidations)
}
