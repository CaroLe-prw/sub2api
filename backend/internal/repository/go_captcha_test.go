package repository

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestGoCaptchaGeneratorProducesImagesWithoutLeakingTarget(t *testing.T) {
	generator := NewGoCaptchaGenerator()
	challenge, answer, err := generator.Generate(context.Background())
	require.NoError(t, err)
	require.NotEqual(t, answer.X, challenge.ThumbX)
	require.Equal(t, answer.Y, challenge.ThumbY)
	for _, tc := range []struct {
		data          string
		width, height int
	}{
		{challenge.Image, challenge.Width, challenge.Height},
		{challenge.Thumb, challenge.ThumbWidth, challenge.ThumbHeight},
	} {
		_, encoded, ok := strings.Cut(tc.data, ",")
		require.True(t, ok)
		raw, err := base64.StdEncoding.DecodeString(encoded)
		require.NoError(t, err)
		cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
		require.NoError(t, err)
		require.Equal(t, tc.width, cfg.Width)
		require.Equal(t, tc.height, cfg.Height)
	}
	raw, err := json.Marshal(challenge)
	require.NoError(t, err)
	var public map[string]any
	require.NoError(t, json.Unmarshal(raw, &public))
	require.NotContains(t, public, "x")
	require.NotContains(t, public, "y")
	require.NotContains(t, public, "answer")
}

func TestGoCaptchaStoreAtomicConsumptionAndExpiry(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	store := NewGoCaptchaStore(client)
	ctx := context.Background()
	require.NoError(t, store.Put(ctx, "proof:once", "203.0.113.1", 2*time.Minute))
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, err := store.Take(ctx, "proof:once")
			if err != nil {
				t.Error(err)
				return
			}
			if value != "" {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, successes.Load())
	require.NoError(t, store.Put(ctx, "challenge:expired", "answer", 2*time.Minute))
	server.FastForward(2 * time.Minute)
	value, err := store.Take(ctx, "challenge:expired")
	require.NoError(t, err)
	require.Empty(t, value)
}
