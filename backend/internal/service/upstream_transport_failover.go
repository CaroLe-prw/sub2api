package service

import (
	"context"
	"errors"
	"net/http"
)

// upstreamTransportFailoverError distinguishes a real client disconnect from
// an upstream request context that was canceled internally.
func upstreamTransportFailoverError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx != nil && ctx.Err() != nil {
		return err
	}
	requestScoped := errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
	scope := GatewayFailureScopeAccount
	if requestScoped {
		scope = GatewayFailureScopeRequest
	}
	safeMessage := sanitizeUpstreamErrorMessage(err.Error())
	return &UpstreamFailoverError{
		StatusCode:             http.StatusBadGateway,
		ResponseBody:           []byte(safeMessage),
		RetryableOnSameAccount: true,
		RequestScopedTransient: requestScoped,
		Stage:                  GatewayFailureStageInference,
		Scope:                  scope,
		Reason:                 GatewayFailureReason("upstream_transport_error"),
		NextAccountAction:      NextAccountRetry,
	}
}
