package qqbot

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQQAPIErrorsRetainCodesWithoutSecrets(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusOK} {
		client := httpClient()
		client.Transport = mediaTransport(func(_ *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Header: http.Header{"X-Tps-Trace-Id": []string{"trace-12345"}}, Body: io.NopCloser(strings.NewReader(`{"code":850018,"err_code":7,"message":"SECRET-TOKEN https://storage.qq.com/?sign=SECRET","trace_id":"body-trace"}`))}, nil
		})
		var output apiResult
		err := doJSON(context.Background(), client, http.MethodGet, "https://api.bot.qq.com/example", nil, nil, &output)
		require.Error(t, err)
		var apiErr *apiResponseError
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, status, apiErr.Status)
		require.Equal(t, 850018, apiErr.Code)
		require.Contains(t, err.Error(), "trace-12345")
		require.NotContains(t, err.Error(), "SECRET")
		require.NotContains(t, err.Error(), "https://")
		var httpErr httpStatusError
		require.True(t, errors.As(err, &httpErr))
		require.Equal(t, httpStatusError(status), httpErr)
	}
}

func TestQQAPIErrorsDiscardMalformedTrace(t *testing.T) {
	client := httpClient()
	client.Transport = mediaTransport(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 401, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"code":100016,"trace_id":"https://example.invalid/?secret=leak"}`))}, nil
	})
	var output apiResult
	err := doJSON(context.Background(), client, http.MethodGet, "https://api.bot.qq.com/example", nil, nil, &output)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "secret")
	require.NotContains(t, err.Error(), "trace_id")
}

func TestQQEmptySuccessStillRequiresTokenAndMessageID(t *testing.T) {
	q := &client{http: httpClient(), baseURL: "https://api.bot.qq.com"}
	validToken := false
	q.http.Transport = mediaTransport(func(r *http.Request) (*http.Response, error) {
		body := ""
		if validToken && r.URL.Path == "/app/getAppAccessToken" {
			body = `{"access_token":"token","expires_in":7200}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	_, err := q.accessToken(context.Background())
	require.Error(t, err)
	validToken = true
	require.Error(t, q.replyText(context.Background(), "group", "message", "text", 1))
}
