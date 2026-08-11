package service

import (
	"sync"

	"github.com/gin-gonic/gin"
)

const openAIStreamKeepaliveBytesKey = "openai_stream_keepalive_bytes"

type openAIStreamKeepaliveBytes struct {
	mu    sync.Mutex
	bytes int
}

func recordOpenAIStreamKeepaliveBytes(c *gin.Context, written int) {
	if c == nil || written <= 0 {
		return
	}
	value, ok := c.Get(openAIStreamKeepaliveBytesKey)
	var state *openAIStreamKeepaliveBytes
	if ok {
		state, _ = value.(*openAIStreamKeepaliveBytes)
	}
	if state == nil {
		state = &openAIStreamKeepaliveBytes{}
		c.Set(openAIStreamKeepaliveBytesKey, state)
	}
	state.mu.Lock()
	state.bytes += written
	state.mu.Unlock()
}

// OpenAIResponseSemanticAdjustedWrittenSize returns downstream bytes excluding
// compact heartbeats and regular SSE keepalive comments. Both are transport
// liveness signals, not semantic model output, so they must not prevent a safe
// pre-output retry or account failover.
func OpenAIResponseSemanticAdjustedWrittenSize(c *gin.Context) int {
	size := OpenAICompactKeepaliveAdjustedWrittenSize(c)
	if size < 0 || c == nil {
		return size
	}
	value, ok := c.Get(openAIStreamKeepaliveBytesKey)
	if !ok {
		return size
	}
	state, ok := value.(*openAIStreamKeepaliveBytes)
	if !ok || state == nil {
		return size
	}
	state.mu.Lock()
	heartbeatBytes := state.bytes
	state.mu.Unlock()
	if real := size - heartbeatBytes; real > 0 {
		return real
	}
	return -1
}
