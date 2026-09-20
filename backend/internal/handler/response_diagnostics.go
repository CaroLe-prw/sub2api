package handler

import (
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/responsediag"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type diagnosticResponseWriter struct {
	gin.ResponseWriter
	capture *responsediag.Capture
}

func (w *diagnosticResponseWriter) Write(p []byte) (int, error) {
	started := time.Now()
	n, err := w.ResponseWriter.Write(p)
	w.capture.WriteDownstreamTimed(p[:n], w.Header().Get("Content-Type"), err != nil, started)
	return n, err
}
func (w *diagnosticResponseWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }

func (w *diagnosticResponseWriter) Flush() {
	mark := w.capture.BeginFlush()
	w.ResponseWriter.Flush()
	w.capture.EndFlush(mark)
}

func logResponseStreamTiming(c *gin.Context) {
	if c.Request == nil {
		return
	}
	if endpoint := GetInboundEndpoint(c); endpoint != EndpointResponses && endpoint != EndpointResponsesCompact {
		return
	}
	timing := responsediag.StreamTiming(c.Request.Context())
	if timing == nil {
		return
	}
	logger.FromContext(c.Request.Context()).Info("gateway.response_stream_timing",
		zap.String("component", "http.access.stream_timing"),
		zap.Any("stream_timing", timing),
	)
}

func startResponseDiagnostics(c *gin.Context, limits ...int) {
	if c.Request == nil || strings.EqualFold(c.GetHeader("Upgrade"), "websocket") {
		return
	}
	ctx, capture := responsediag.Start(c.Request.Context(), limits...)
	c.Request = responsediag.WrapRequest(c.Request.WithContext(ctx), false)
	c.Writer = &diagnosticResponseWriter{ResponseWriter: c.Writer, capture: capture}
	// Usage is normally submitted immediately after the last downstream write.
	// The immutable snapshot is made at that submission boundary.
}
