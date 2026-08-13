package handler

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"go.uber.org/zap"
)

const requestBodyDiagnosticHeaderMaxLen = 128

// LogRequestBodyReadFailure records transport metadata without retaining any
// request content, credentials, image data, or other headers.
func LogRequestBodyReadFailure(reqLog *zap.Logger, req *http.Request, err error) {
	logRequestBodyReadFailure(reqLog, req, err)
}

func logRequestBodyReadFailure(reqLog *zap.Logger, req *http.Request, err error) {
	if reqLog == nil || err == nil {
		return
	}

	stage := "unknown"
	underlyingErr := err
	fields := []zap.Field{
		zap.String("request_body_error_kind", requestBodyReadErrorKind(err)),
	}

	var readErr *pkghttputil.RequestBodyReadError
	if errors.As(err, &readErr) && readErr != nil {
		stage = string(readErr.Stage)
		fields = append(fields,
			zap.Int64("request_body_bytes_read", readErr.BytesRead),
			zap.Int64("request_body_read_ms", readErr.Elapsed.Milliseconds()),
		)
		if readErr.Err != nil {
			underlyingErr = readErr.Err
		}
	}
	fields = append(fields, zap.String("request_body_error_stage", stage))

	if req != nil {
		fields = append(fields,
			zap.Int64("request_content_length", req.ContentLength),
			zap.String("content_encoding", boundedRequestDiagnosticValue(req.Header.Get("Content-Encoding"))),
			zap.String("transfer_encoding", boundedRequestDiagnosticValue(strings.Join(req.TransferEncoding, ","))),
		)
		if readErr != nil && req.ContentLength >= 0 && readErr.BytesRead < req.ContentLength {
			fields = append(fields, zap.Int64("request_body_bytes_missing", req.ContentLength-readErr.BytesRead))
		}
	}

	fields = append(fields, zap.Error(underlyingErr))
	reqLog.Warn("request body read failed", fields...)
}

func requestBodyReadErrorKind(err error) string {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return "body_too_large"
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return "unexpected_eof"
	}
	if errors.Is(err, context.Canceled) {
		return "request_canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	if errors.Is(err, pkghttputil.ErrUnsupportedContentEncoding) {
		return "unsupported_content_encoding"
	}
	var readErr *pkghttputil.RequestBodyReadError
	if errors.As(err, &readErr) && readErr != nil && readErr.Stage == pkghttputil.RequestBodyReadStageDecode {
		return "content_decoding_failed"
	}
	return "read_failed"
}

func boundedRequestDiagnosticValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= requestBodyDiagnosticHeaderMaxLen {
		return value
	}
	return value[:requestBodyDiagnosticHeaderMaxLen]
}
