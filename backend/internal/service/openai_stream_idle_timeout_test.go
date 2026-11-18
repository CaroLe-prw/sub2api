package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRelayStallsIdleDeadlineTracksLastRead(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{
		MaxLineSize: defaultMaxLineSize, StreamDataIntervalTimeout: 1,
	}}}
	pr, pw := io.Pipe()
	defer func() { _ = pr.Close() }()
	defer func() { _ = pw.Close() }()
	go func() {
		time.Sleep(250 * time.Millisecond)
		_, _ = io.WriteString(pw, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"r\"}}\n\n")
	}()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: pr}
	account := &Account{ID: 1, Platform: PlatformGrok, Type: AccountTypeOAuth}
	start := time.Now()
	_, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, account, start, "model", "model")
	elapsed := time.Since(start)
	t.Logf("idle timeout after %s (last upstream read at approximately 250ms)", elapsed)
	require.Error(t, err)
	require.GreaterOrEqual(t, elapsed, 1200*time.Millisecond, "do not shorten the configured idle interval")
	require.Less(t, elapsed, 1800*time.Millisecond, "do not wait for the next full polling interval")
	var failover *UpstreamFailoverError
	require.ErrorAs(t, err, &failover, "non-OpenAI streams retain the generic idle deadline")
	require.Empty(t, rec.Body.String())
}

// OpenAI deliberately ignores the generic idle deadline: silence can still be
// billable. Keep exercising the timer path without replaying an ambiguous attempt.
func TestOpenAIIdleDeadlineDoesNotReplayBeforeOrAfterOutput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, output := range []bool{false, true} {
		name := "before_output"
		if output {
			name = "after_output"
		}
		t.Run(name, func(t *testing.T) {
			svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{
				MaxLineSize: defaultMaxLineSize, StreamDataIntervalTimeout: 1,
			}}}
			pr, pw := io.Pipe()
			defer pr.Close()
			defer pw.Close()
			go func() {
				frame := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"r\"}}\n\n"
				if output {
					frame += "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n"
				}
				_, _ = io.WriteString(pw, frame)
				time.Sleep(1300 * time.Millisecond)
				if !output {
					_, _ = io.WriteString(pw, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n")
				}
				_, _ = io.WriteString(pw, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"status\":\"completed\",\"output\":[]}}\n\n")
				_ = pw.Close()
			}()
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: pr}
			account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
			_, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "model", "model")
			require.NoError(t, err)
			require.Contains(t, rec.Body.String(), "response.completed")
			require.NotContains(t, rec.Body.String(), "stream_timeout")
			if output {
				require.Contains(t, rec.Body.String(), "hello")
			}
		})
	}
}

func TestOpenAIIdleDeadlineKeepsPreludePrivateUntilExplicitFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{
		MaxLineSize: defaultMaxLineSize, StreamDataIntervalTimeout: 1, StreamKeepaliveInterval: 1,
	}}}
	pr, pw := io.Pipe()
	defer pr.Close()
	defer pw.Close()
	go func() {
		_, _ = io.WriteString(pw, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"r\"}}\n\n")
		time.Sleep(1300 * time.Millisecond)
		_, _ = io.WriteString(pw, "data: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"error\":{\"code\":\"server_error\",\"message\":\"An error occurred while processing your request.\"}}}\n\n")
		_ = pw.Close()
	}()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: pr}
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	_, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "model", "model")
	var failover *UpstreamFailoverError
	require.ErrorAs(t, err, &failover)
	require.True(t, failover.ExplicitUpstreamResponse)
	body := rec.Body.String()
	require.NotEmpty(t, body)
	require.Empty(t, strings.ReplaceAll(body, ":\n\n", ""), "only keepalive comments may precede the explicit failure")
	require.Equal(t, -1, OpenAICompactKeepaliveAdjustedWrittenSize(c))
}
