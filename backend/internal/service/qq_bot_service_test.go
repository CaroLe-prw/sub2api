package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/qqbot"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestQQBotConfigSecretAndScope(t *testing.T) {
	ctx := context.Background()
	repo := newRuntimeSettingRepoStub()
	settings := NewSettingService(repo, &config.Config{})
	s := NewQQBotService(repo, opsTelegramTestEncryptor{}, nil, settings, nil, nil)
	view, err := s.Update(ctx, QQBotUpdate{Config: qqbot.Config{AppID: "123", Enabled: true}, AppSecret: "not-for-public-output"})
	require.NoError(t, err)
	require.True(t, view.SecretConfigured)
	data, _ := json.Marshal(view)
	require.NotContains(t, string(data), "not-for-public-output")
	require.NotContains(t, repo.values[qqBotSettingKey], "not-for-public-output")
	_, err = s.Update(ctx, QQBotUpdate{Config: view.Config})
	require.NoError(t, err)
	stored, err := s.stored(ctx)
	require.NoError(t, err)
	secret, err := s.encryptor.Decrypt(stored.SecretEncrypted)
	require.NoError(t, err)
	require.Equal(t, "not-for-public-output", secret)
	view.AppID = "456"
	_, err = s.Update(ctx, QQBotUpdate{Config: view.Config})
	require.Error(t, err)
	repo.values[SettingKeyChannelMonitorMode] = ChannelMonitorModeV2
	_, err = s.Update(ctx, QQBotUpdate{Config: qqbot.Config{AppID: "123", Enabled: true, Groups: []string{"group-a"}}})
	require.Error(t, err)
	_, err = s.Update(ctx, QQBotUpdate{Config: qqbot.Config{AppID: "123", Enabled: true, Groups: []string{"group-a"}, GroupIDs: []int64{1}}})
	require.NoError(t, err)
	_, err = s.Update(ctx, QQBotUpdate{Config: qqbot.Config{AppID: "123"}, ClearSecret: true})
	require.NoError(t, err)
	stored, err = s.stored(ctx)
	require.NoError(t, err)
	require.Empty(t, stored.SecretEncrypted)
}

func TestQQBotRedisDeduplicationAndCooldown(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	s := &QQBotService{redis: rdb}
	ctx := context.Background()
	ok, err := s.Claim(ctx, "message-a", "group-a", true)
	require.NoError(t, err)
	require.True(t, ok)
	mr.FastForward(6 * time.Second)
	ok, err = s.Claim(ctx, "message-a", "group-a", false)
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = s.Claim(ctx, "message-b", "group-a", true)
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = s.Claim(ctx, "message-c", "group-a", false)
	require.NoError(t, err)
	require.True(t, ok)
	mr.FastForward(time.Minute)
	ok, err = s.Claim(ctx, "message-d", "group-a", true)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestQQBotV2OnlySelectedGroupsAndOldData(t *testing.T) {
	ctx := context.Background()
	repo := newRuntimeSettingRepoStub()
	repo.values[SettingKeyChannelMonitorMode] = ChannelMonitorModeV2
	settings := NewSettingService(repo, &config.Config{})
	id1, id2 := int64(1), int64(2)
	v2repo := &channelMonitorV2RepoStub{config: ChannelMonitorV2Config{Enabled: true}, matrix: &ChannelMonitorV2Matrix{Coverage: ChannelMonitorV2Coverage{DataThrough: time.Now().Add(-time.Hour)}, Items: []ChannelMonitorV2MatrixRow{
		{GroupID: &id1, GroupName: "public-group", Model: "model-a", Health: ChannelMonitorV2Health{Overall: "healthy"}},
		{GroupID: &id2, GroupName: "secret-group", Model: "private-model", Health: ChannelMonitorV2Health{Overall: "healthy"}},
	}}}
	s := NewQQBotService(repo, nil, nil, settings, nil, NewChannelMonitorV2Service(v2repo))
	text, err := s.Query(ctx, qqbot.Config{GroupIDs: []int64{1}}, "", false)
	require.NoError(t, err)
	require.Contains(t, text, "public-group")
	require.Contains(t, text, "旧数据")
	require.NotContains(t, text, "secret-group")
	require.NotContains(t, text, "private-model")
	text, err = s.Query(ctx, qqbot.Config{}, "", false)
	require.NoError(t, err)
	require.Contains(t, text, "尚未配置")
	text, err = s.Query(ctx, qqbot.Config{GroupIDs: []int64{1}}, "1", true)
	require.NoError(t, err)
	require.Contains(t, text, "不支持")
}
