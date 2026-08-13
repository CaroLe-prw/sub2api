//go:build unit

package handler

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
)

type handlerPartialErrorBody struct {
	data []byte
	err  error
}

func (b *handlerPartialErrorBody) Read(p []byte) (int, error) {
	if len(b.data) == 0 {
		return 0, b.err
	}
	n := copy(p, b.data)
	b.data = b.data[n:]
	if len(b.data) == 0 {
		return n, b.err
	}
	return n, nil
}

func (b *handlerPartialErrorBody) Close() error { return nil }

func TestLogRequestBodyReadFailure_RecordsSafeTransportMetadata(t *testing.T) {
	log, logs := newObservedLogger(t)
	req := httptest.NewRequest(http.MethodPost, "/responses", nil)
	req.ContentLength = 10 << 20
	req.Header.Set("Content-Encoding", "gzip")
	req.TransferEncoding = []string{"chunked"}

	err := &pkghttputil.RequestBodyReadError{
		Stage:     pkghttputil.RequestBodyReadStageRead,
		BytesRead: 8 << 20,
		Elapsed:   125000000000,
		Err:       io.ErrUnexpectedEOF,
	}
	logRequestBodyReadFailure(log, req, err)

	entries := logs.All()
	require.Len(t, entries, 1)
	require.Equal(t, "request body read failed", entries[0].Message)
	fields := observedLogFields(entries[0].Context)
	require.Equal(t, "unexpected_eof", fields["request_body_error_kind"])
	require.Equal(t, "read", fields["request_body_error_stage"])
	require.EqualValues(t, 8<<20, fields["request_body_bytes_read"])
	require.EqualValues(t, 10<<20, fields["request_content_length"])
	require.EqualValues(t, 2<<20, fields["request_body_bytes_missing"])
	require.EqualValues(t, 125000, fields["request_body_read_ms"])
	require.Equal(t, "gzip", fields["content_encoding"])
	require.Equal(t, "chunked", fields["transfer_encoding"])
	require.Equal(t, io.ErrUnexpectedEOF.Error(), fields["error"])
}

func TestLogRequestBodyReadFailure_DoesNotLogRequestContent(t *testing.T) {
	log, logs := newObservedLogger(t)
	const secret = "secret-base64-image-payload"
	req := httptest.NewRequest(http.MethodPost, "/responses", strings.NewReader(secret))
	err := &pkghttputil.RequestBodyReadError{
		Stage:     pkghttputil.RequestBodyReadStageRead,
		BytesRead: 7,
		Err:       errors.New("connection reset by peer"),
	}

	logRequestBodyReadFailure(log, req, err)

	entries := logs.All()
	require.Len(t, entries, 1)
	require.NotContains(t, fmt.Sprint(entries[0].ContextMap()), secret)
	require.NotContains(t, entries[0].Message, secret)
}

func TestOpenAIResponses_PartialBodyKeepsResponseAndAddsDiagnostics(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logSink, restore := captureHandlerStructuredLog(t)
	defer restore()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/responses", nil)
	c.Request.Body = &handlerPartialErrorBody{data: []byte(`{"model":"gpt-5.5"`), err: io.ErrUnexpectedEOF}
	c.Request.ContentLength = 10 << 20
	c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 7, UserID: 9})
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 9, Concurrency: 1})

	newOpenAIHandlerForPreviousResponseIDValidation(t, nil).Responses(c)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.JSONEq(t, `{"error":{"message":"Failed to read request body","type":"invalid_request_error"}}`, recorder.Body.String())
	require.True(t, logSink.ContainsMessageAtLevel("request body read failed", "warn"))
	require.True(t, logSink.ContainsFieldValue("request_body_error_kind", "unexpected_eof"))
	require.True(t, logSink.ContainsFieldValue("request_content_length", fmt.Sprint(10<<20)))
}

func observedLogFields(fields []zapcore.Field) map[string]any {
	result := make(map[string]any, len(fields))
	for _, field := range fields {
		switch field.Type {
		case zapcore.ErrorType:
			result[field.Key] = field.Interface.(error).Error()
		case zapcore.Int64Type, zapcore.Int32Type, zapcore.Int16Type, zapcore.Int8Type:
			result[field.Key] = field.Integer
		default:
			result[field.Key] = field.String
		}
	}
	return result
}
