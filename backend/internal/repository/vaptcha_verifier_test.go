package repository

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestVaptchaVerifierUsesV4Protocol(t *testing.T) {
	require.Equal(t, "https://v41.vaptcha.com/api/verify", vaptchaVerifyURL)

	type verifyRequest struct {
		VID   string `json:"vid"`
		VKey  string `json:"vkey"`
		Token string `json:"token"`
		Knock string `json:"knock"`
		DFU   string `json:"dfu"`
		IP    string `json:"ip"`
	}
	var received verifyRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&received))
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"code":0,"msg":"success","data":{"code":0,"note":"success","result":true,"vid":"vid-1"}}`))
		require.NoError(t, err)
	}))
	t.Cleanup(server.Close)

	verifier := &vaptchaVerifier{httpClient: server.Client(), verifyURL: server.URL}
	vaptchaService := service.NewVaptchaService(verifier)
	proof := `{"token":"1700000000.token-id.signature","knock":"knock-1","dfu":"dfu-1","ip":"203.0.113.8"}`
	err := vaptchaService.VerifyTokenWithConfig(context.Background(), service.VaptchaConfig{
		VID: "vid-1",
		Key: "vkey-1",
	}, proof, "198.51.100.2")

	require.NoError(t, err)
	require.Equal(t, verifyRequest{
		VID:   "vid-1",
		VKey:  "vkey-1",
		Token: "1700000000.token-id.signature",
		Knock: "knock-1",
		DFU:   "dfu-1",
		IP:    "203.0.113.8",
	}, received, "V4 验签必须优先使用 SDK 返回的签名 IP 快照")
}
