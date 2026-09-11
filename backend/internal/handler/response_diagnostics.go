package handler

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/responsediag"
	"github.com/gin-gonic/gin"
)

type diagnosticResponseWriter struct {
	gin.ResponseWriter
	capture *responsediag.Capture
}

func (w *diagnosticResponseWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	w.capture.WriteDownstream(p[:n], w.Header().Get("Content-Type"), err != nil)
	return n, err
}
func (w *diagnosticResponseWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }

func startResponseDiagnostics(c *gin.Context) {
	if c.Request == nil || strings.EqualFold(c.GetHeader("Upgrade"), "websocket") {
		return
	}
	ctx, capture := responsediag.Start(c.Request.Context())
	c.Request = c.Request.WithContext(ctx)
	c.Writer = &diagnosticResponseWriter{ResponseWriter: c.Writer, capture: capture}
	// Usage is normally submitted immediately after the last downstream write.
	// The immutable snapshot is made at that submission boundary.
}
