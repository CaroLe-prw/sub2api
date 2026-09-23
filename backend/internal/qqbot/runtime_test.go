package qqbot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/stretchr/testify/require"
)

type testSource struct{ calls atomic.Int32 }

func (s *testSource) Query(_ context.Context, _ Config, _ string, _ bool) (string, error) {
	s.calls.Add(1)
	return "渠道正常", nil
}

type testGuard struct {
	mu   sync.Mutex
	seen map[string]bool
}

func (g *testGuard) Claim(_ context.Context, id, _ string, _ bool) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.seen[id] {
		return false, nil
	}
	g.seen[id] = true
	return true, nil
}

func TestQQWebSocketDispatchHeartbeatAndResume(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	source := &testSource{}
	guard := &testGuard{seen: map[string]bool{}}
	var connections atomic.Int32
	var heartbeats atomic.Int32
	replies := make(chan string, 4)
	resumed := make(chan bool, 1)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/app/getAppAccessToken":
			fmt.Fprint(w, `{"access_token":"test-token","expires_in":"7200"}`)
		case "/gateway":
			require.Equal(t, "QQBot test-token", req.Header.Get("Authorization"))
			_ = json.NewEncoder(w).Encode(map[string]string{"url": "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"})
		case "/v2/groups/group-a/messages":
			var body map[string]any
			require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
			require.Equal(t, "message-a", body["msg_id"])
			require.EqualValues(t, 1, body["msg_seq"])
			content, ok := body["content"].(string)
			require.True(t, ok)
			replies <- content
			fmt.Fprint(w, `{"id":"reply-a"}`)
		case "/ws":
			conn, err := websocket.Accept(w, req, nil)
			if err != nil {
				return
			}
			defer func() { _ = conn.CloseNow() }()
			n := connections.Add(1)
			_ = wsjson.Write(ctx, conn, map[string]any{"op": 10, "d": map[string]int{"heartbeat_interval": 100}})
			var identify struct {
				Op   int `json:"op"`
				Data struct {
					Token   string `json:"token"`
					Session string `json:"session_id"`
					Seq     int64  `json:"seq"`
					Intents int    `json:"intents"`
				} `json:"d"`
			}
			if wsjson.Read(ctx, conn, &identify) != nil {
				return
			}
			require.Equal(t, "QQBot test-token", identify.Data.Token)
			if n == 1 {
				require.Equal(t, 2, identify.Op)
				require.Equal(t, 1<<25, identify.Data.Intents)
				_ = wsjson.Write(ctx, conn, map[string]any{"op": 0, "t": "READY", "s": 1, "d": map[string]string{"session_id": "test-session"}})
				msg := map[string]any{"op": 0, "t": "GROUP_AT_MESSAGE_CREATE", "s": 2, "d": map[string]any{"id": "message-a", "group_openid": "group-a", "content": "渠道状态", "timestamp": time.Now(), "author": map[string]string{"member_openid": "user-a"}}}
				_ = wsjson.Write(ctx, conn, msg)
				_ = wsjson.Write(ctx, conn, msg)
				var heartbeat payload
				if wsjson.Read(ctx, conn, &heartbeat) != nil {
					return
				}
				require.Equal(t, 1, heartbeat.Op)
				heartbeats.Add(1)
				_ = wsjson.Write(ctx, conn, map[string]int{"op": 11})
				_ = wsjson.Write(ctx, conn, map[string]int{"op": 7})
			} else {
				require.Equal(t, 6, identify.Op)
				require.Equal(t, "test-session", identify.Data.Session)
				require.EqualValues(t, 2, identify.Data.Seq)
				_ = wsjson.Write(ctx, conn, map[string]any{"op": 0, "t": "RESUMED", "s": 3, "d": ""})
				resumed <- true
				for {
					var p payload
					if wsjson.Read(ctx, conn, &p) != nil {
						return
					}
					_ = wsjson.Write(ctx, conn, map[string]int{"op": 11})
				}
			}
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	r := New(Config{AppID: "test", Groups: []string{"group-a"}}, "test", source, guard, nil)
	r.api.baseURL = server.URL
	r.allowTestWS = true
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	select {
	case text := <-replies:
		require.Equal(t, "渠道正常", text)
	case <-ctx.Done():
		t.Fatal("no reply")
	}
	select {
	case <-resumed:
	case <-ctx.Done():
		t.Fatal("did not resume")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown leaked")
	}
	require.EqualValues(t, 1, source.calls.Load())
	require.EqualValues(t, 1, heartbeats.Load())
}

func TestQQDispatchScopeAndProbeAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name, group, user, command string
		allow                      bool
		wantCalls                  int32
	}{
		{"other-group", "group-b", "admin", "渠道状态", true, 0},
		{"ordinary-chat", "group-a", "admin", "你好", true, 0},
		{"no-admin", "group-a", "member", "检测 1", true, 0},
		{"disabled-probe", "group-a", "admin", "检测 1", false, 0},
		{"authorized", "group-a", "admin", "检测 1", true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := &testSource{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "getAppAccessToken") {
					fmt.Fprint(w, `{"access_token":"x","expires_in":7200}`)
				} else {
					fmt.Fprint(w, `{"id":"reply"}`)
				}
			}))
			defer server.Close()
			r := New(Config{AppID: "a", Groups: []string{"group-a"}, Admins: []string{"admin"}, AllowProbe: tc.allow}, "secret", source, &testGuard{seen: map[string]bool{}}, nil)
			r.api.baseURL = server.URL
			msg := Message{ID: "id", Group: tc.group, Content: tc.command, Timestamp: time.Now()}
			msg.Author.ID = tc.user
			require.NoError(t, r.dispatch(context.Background(), msg))
			r.wg.Wait()
			require.Equal(t, tc.wantCalls, source.calls.Load())
		})
	}
}

func TestQQMissingHeartbeatAckDisconnects(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/app/getAppAccessToken" {
			fmt.Fprint(w, `{"access_token":"test-token","expires_in":7200}`)
			return
		}
		if req.URL.Path == "/gateway" {
			_ = json.NewEncoder(w).Encode(map[string]string{"url": "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"})
			return
		}
		conn, err := websocket.Accept(w, req, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		_ = wsjson.Write(ctx, conn, map[string]any{"op": 10, "d": map[string]int{"heartbeat_interval": 100}})
		var p payload
		if wsjson.Read(ctx, conn, &p) != nil {
			return
		}
		_ = wsjson.Write(ctx, conn, map[string]any{"op": 0, "t": "READY", "s": 1, "d": map[string]string{"session_id": "s"}})
		for {
			if wsjson.Read(ctx, conn, &p) != nil {
				return
			}
		}
	}))
	defer server.Close()
	r := New(Config{}, "s", nil, nil, nil)
	r.api.baseURL = server.URL
	r.allowTestWS = true
	err := r.connect(ctx)
	require.ErrorContains(t, err, "heartbeat timeout")
	require.NoError(t, ctx.Err())
}

func TestQQChannelMonitorCommandAlias(t *testing.T) {
	for _, text := range []string{"渠道监测", "/渠道监测", "<@bot> 渠道监测"} {
		command, query := Command(text)
		require.Equal(t, "渠道状态", command)
		require.Empty(t, query)
	}
	command, query := Command("渠道监测 OpenAI")
	require.Equal(t, "渠道状态", command)
	require.Equal(t, "OpenAI", query)
	command, _ = Command("今天渠道监测怎么样")
	require.Empty(t, command)
}

func TestQQUnmentionedQueriesAreOptInScopedAndDeduplicated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "getAppAccessToken") {
			fmt.Fprint(w, `{"access_token":"token","expires_in":7200}`)
		} else {
			fmt.Fprint(w, `{"id":"reply"}`)
		}
	}))
	defer server.Close()
	for _, tc := range []struct {
		name, event, group, content string
		enabled                     bool
		calls                       int32
	}{
		{"off", "GROUP_MESSAGE_CREATE", "group-a", "渠道监测", false, 0},
		{"on", "GROUP_MESSAGE_CREATE", "group-a", "渠道监测", true, 1},
		{"other-group", "GROUP_MESSAGE_CREATE", "group-b", "渠道监测", true, 0},
		{"ordinary-chat", "GROUP_MESSAGE_CREATE", "group-a", "大家好", true, 0},
		{"probe-needs-at", "GROUP_MESSAGE_CREATE", "group-a", "检测 1", true, 0},
		{"at-still-works", "GROUP_AT_MESSAGE_CREATE", "group-a", "渠道监测", false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := &testSource{}
			r := New(Config{AppID: "test", Groups: []string{"group-a"}, Admins: []string{"admin"}, AllowProbe: true, AllowUnmentioned: tc.enabled}, "secret", source, &testGuard{seen: map[string]bool{}}, nil)
			r.api.baseURL = server.URL
			message := Message{ID: "same-id", Group: tc.group, Content: tc.content, Timestamp: time.Now()}
			message.Author.ID = "admin"
			data, err := json.Marshal(message)
			require.NoError(t, err)
			event := payload{Type: tc.event, Data: data}
			require.NoError(t, r.dispatchGroupEvent(context.Background(), event))
			r.wg.Wait()
			require.Equal(t, tc.calls, source.calls.Load())
			if tc.calls > 0 {
				// The same command arriving through both event streams is one reply.
				event.Type = "GROUP_AT_MESSAGE_CREATE"
				require.NoError(t, r.dispatchGroupEvent(context.Background(), event))
				r.wg.Wait()
				require.EqualValues(t, 1, source.calls.Load())
			}
		})
	}
}
