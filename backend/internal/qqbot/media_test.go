package qqbot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type mediaTransport func(*http.Request) (*http.Response, error)

func (f mediaTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestQQImageUploadAndPassiveReply(t *testing.T) {
	var chunks []string
	var finishes int
	var merged, replied bool
	q := &client{baseURL: "https://api.bot.qq.com", appID: "test", secret: "test", http: httpClient()}
	q.http.Transport = mediaTransport(func(r *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(r.Body)
		result := "{}"
		if r.URL.Host == "storage.qq.com" {
			require.Equal(t, http.MethodPut, r.Method)
			require.Empty(t, r.Header.Get("Authorization"))
			chunks = append(chunks, string(raw))
		} else {
			var body map[string]any
			require.NoError(t, json.Unmarshal(raw, &body))
			switch r.URL.Path {
			case "/app/getAppAccessToken":
				result = `{"access_token":"token","expires_in":7200}`
			case "/v2/groups/g/upload_prepare":
				require.Equal(t, "10", body["file_size"])
				require.Equal(t, "781e5e245d69b566979b86e28d23f2c7", body["md5"])
				result = `{"upload_id":"upload","block_size":"5","parts":[{"index":0,"presigned_url":"https://storage.qq.com/0","block_size":"5"},{"index":1,"presigned_url":"https://storage.qq.com/1","block_size":"5"}]}`
			case "/v2/groups/g/upload_part_finish":
				require.EqualValues(t, finishes, body["part_index"])
				require.Equal(t, "5", body["block_size"])
				finishes++
			case "/v2/groups/g/files":
				require.Equal(t, false, body["srv_send_msg"])
				require.Equal(t, "upload", body["upload_id"])
				merged = true
				result = `{"file_info":"media-info"}`
			case "/v2/groups/g/messages":
				require.True(t, merged)
				require.EqualValues(t, 7, body["msg_type"])
				require.EqualValues(t, 2, body["msg_seq"])
				require.Equal(t, "message", body["msg_id"])
				require.Equal(t, `<qqbot-at-user id="requesting-member" /> 渠道状态看板`, body["content"])
				media, ok := body["media"].(map[string]any)
				require.True(t, ok)
				require.Equal(t, "media-info", media["file_info"])
				replied = true
				result = `{"id":"reply"}`
			default:
				t.Fatalf("unexpected API path: %s", r.URL.Path)
			}
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(result))}, nil
	})
	require.NoError(t, q.replyImage(context.Background(), "g", "message", "requesting-member", []byte("0123456789"), 2))
	require.Equal(t, []string{"01234", "56789"}, chunks)
	require.Equal(t, 2, finishes)
	require.True(t, replied)
}

func TestQQUploadRejectsUntrustedStorage(t *testing.T) {
	for _, raw := range []string{"http://storage.qq.com/file", "https://qq.com.attacker.example/file", "https://127.0.0.1/file", "https://storage.qq.com:8443/file"} {
		u, err := url.Parse(raw)
		require.NoError(t, err)
		require.False(t, trustedUploadURL(u), raw)
	}
	u, _ := url.Parse("https://bucket.cos.ap-shanghai.myqcloud.com/file?sign=abc")
	require.True(t, trustedUploadURL(u))
}

func TestQQUploadBusinessFailureNeverSends(t *testing.T) {
	q := &client{baseURL: "https://api.bot.qq.com", http: httpClient()}
	q.http.Transport = mediaTransport(func(r *http.Request) (*http.Response, error) {
		body := `{"code":850018}`
		if r.URL.Path == "/app/getAppAccessToken" {
			body = `{"access_token":"token","expires_in":7200}`
		} else {
			require.True(t, strings.HasSuffix(r.URL.Path, "/upload_prepare"))
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	require.Error(t, q.replyImage(context.Background(), "g", "id", "requesting-member", []byte("image"), 1))
}

func TestQQImageCaptionOnlyMentionsTheRequestingMember(t *testing.T) {
	require.Equal(t, `<qqbot-at-user id="member-a" /> 渠道状态看板`, imageReplyCaption("member-a"))
	require.Equal(t, `<qqbot-at-user id="member-b" /> 渠道状态看板`, imageReplyCaption("member-b"))
	for _, invalid := range []string{"", `user" /><qqbot-at-everyone /><!--`, "@everyone", "昵称", strings.Repeat("x", 129)} {
		require.Equal(t, "渠道状态看板", imageReplyCaption(invalid))
	}
}

// Tencent's SDK defines one-based part indices and zero per-part block_size as
// "use the response-level size". The older wiki examples used zero-based parts.
func TestQQUploadOfficialOneBasedParts(t *testing.T) {
	var uploaded []string
	var completed []int
	q := &client{baseURL: "https://api.bot.qq.com", http: httpClient()}
	q.http.Transport = mediaTransport(func(r *http.Request) (*http.Response, error) {
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		result := "{}"
		if r.URL.Host == "storage.qq.com" {
			require.Empty(t, r.Header.Get("Authorization"))
			uploaded = append(uploaded, string(raw))
		} else {
			var body map[string]any
			require.NoError(t, json.Unmarshal(raw, &body))
			switch r.URL.Path {
			case "/app/getAppAccessToken":
				result = `{"access_token":"token","expires_in":7200}`
			case "/v2/groups/g/upload_prepare":
				result = `{"upload_id":"u","block_size":5,"parts":[{"index":2,"presigned_url":"https://storage.qq.com/2","block_size":"0"},{"index":1,"presigned_url":"https://storage.qq.com/1","block_size":0}]}`
			case "/v2/groups/g/upload_part_finish":
				index, ok := body["part_index"].(float64)
				require.True(t, ok)
				completed = append(completed, int(index))
				result = "" // Official SDK also accepts an empty successful finish response.
			case "/v2/groups/g/files":
				require.Equal(t, false, body["srv_send_msg"])
				result = `{"file_info":"media-info"}`
			default:
				t.Fatalf("unexpected API path %s", r.URL.Path)
			}
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(result))}, nil
	})
	info, err := q.uploadImage(context.Background(), "g", []byte("0123456789"))
	require.NoError(t, err)
	require.Equal(t, "media-info", info)
	require.Equal(t, []string{"01234", "56789"}, uploaded)
	require.Equal(t, []int{1, 2}, completed)
}
