package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/qqbot"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const qqBotLeaseKey = "qqbot:leader"
const qqBotStatusKey = "qqbot:status"

type qqBotCache struct{ client *redis.Client }

func NewQQBotCache(client *redis.Client) service.QQBotCache {
	return &qqBotCache{client: client}
}

func (c *qqBotCache) Claim(ctx context.Context, id, group string, probe bool) (bool, error) {
	hash := func(value string) string { sum := sha256.Sum256([]byte(value)); return hex.EncodeToString(sum[:]) }
	probeFlag := 0
	if probe {
		probeFlag = 1
	}
	value, err := c.client.Eval(ctx, `
if redis.call('EXISTS', KEYS[1]) == 1 or redis.call('EXISTS', KEYS[2]) == 1 then return 0 end
if ARGV[1] == '1' and redis.call('EXISTS', KEYS[3]) == 1 then return 0 end
redis.call('SET', KEYS[1], '1', 'EX', 600)
redis.call('SET', KEYS[2], '1', 'EX', 5)
if ARGV[1] == '1' then redis.call('SET', KEYS[3], '1', 'EX', 60) end
return 1`, []string{"qqbot:seen:" + hash(id), "qqbot:cooldown:" + hash(group), "qqbot:probe:" + hash(group)}, probeFlag).Int()
	return value == 1, err
}

func (c *qqBotCache) AcquireOrRenewLease(ctx context.Context, owner string) (bool, error) {
	value, err := c.client.Eval(ctx, `
if redis.call('GET',KEYS[1]) == ARGV[1] then redis.call('EXPIRE',KEYS[1],20);return 1 end
if redis.call('SET',KEYS[1],ARGV[1],'NX','EX',20) then return 1 end
return 0`, []string{qqBotLeaseKey}, owner).Int()
	return value == 1, err
}

func (c *qqBotCache) ReleaseLease(ctx context.Context, owner string) error {
	return c.client.Eval(ctx, `if redis.call('GET',KEYS[1]) == ARGV[1] then return redis.call('DEL',KEYS[1]) end return 0`, []string{qqBotLeaseKey}, owner).Err()
}

func (c *qqBotCache) GetStatus(ctx context.Context) (qqbot.Status, error) {
	var status qqbot.Status
	data, err := c.client.Get(ctx, qqBotStatusKey).Bytes()
	if err != nil {
		return status, err
	}
	err = json.Unmarshal(data, &status)
	return status, err
}

func (c *qqBotCache) SetStatus(ctx context.Context, status qqbot.Status) error {
	data, err := json.Marshal(status)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, qqBotStatusKey, data, 30*time.Second).Err()
}

func (c *qqBotCache) RefreshStatus(ctx context.Context) error {
	return c.client.Expire(ctx, qqBotStatusKey, 30*time.Second).Err()
}
