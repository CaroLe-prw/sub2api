package service

import (
	"sync"

	"github.com/gin-gonic/gin"
)

const gatewayStreamKeepaliveBytesKey = "gateway_stream_keepalive_bytes"

type gatewayStreamKeepaliveBytes struct {
	mu    sync.Mutex
	bytes int
}

func recordGatewayStreamKeepaliveBytes(c *gin.Context, written int) {
	if c == nil || written <= 0 {
		return
	}
	value, ok := c.Get(gatewayStreamKeepaliveBytesKey)
	var state *gatewayStreamKeepaliveBytes
	if ok {
		state, _ = value.(*gatewayStreamKeepaliveBytes)
	}
	if state == nil {
		state = &gatewayStreamKeepaliveBytes{}
		c.Set(gatewayStreamKeepaliveBytesKey, state)
	}
	state.mu.Lock()
	state.bytes += written
	state.mu.Unlock()
}

func recordOpenAIStreamKeepaliveBytes(c *gin.Context, written int) {
	recordGatewayStreamKeepaliveBytes(c, written)
}

// GatewayResponseSemanticAdjustedWrittenSize returns downstream bytes excluding
// transport-only keepalives. These bytes keep the connection alive but do not
// commit the request to an account, so they must not prevent pre-output failover.
func GatewayResponseSemanticAdjustedWrittenSize(c *gin.Context) int {
	size := OpenAICompactKeepaliveAdjustedWrittenSize(c)
	if size < 0 || c == nil {
		return size
	}
	value, ok := c.Get(gatewayStreamKeepaliveBytesKey)
	if !ok {
		return size
	}
	state, ok := value.(*gatewayStreamKeepaliveBytes)
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

// OpenAIResponseSemanticAdjustedWrittenSize returns downstream bytes excluding
// compact heartbeats and regular SSE keepalive comments. Both are transport
// liveness signals, not semantic model output, so they must not prevent a safe
// pre-output retry or account failover.
func OpenAIResponseSemanticAdjustedWrittenSize(c *gin.Context) int {
	return GatewayResponseSemanticAdjustedWrittenSize(c)
}
