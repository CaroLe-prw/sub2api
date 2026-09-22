// Package qqbot connects to the official QQ gateway. It never polls channel
// health: the embedding service supplies results only for explicit commands.
package qqbot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type Config struct {
	Enabled    bool     `json:"enabled"`
	AppID      string   `json:"app_id"`
	Groups     []string `json:"groups"`
	Admins     []string `json:"admins"`
	MonitorIDs []int64  `json:"monitor_ids"`
	GroupIDs   []int64  `json:"group_ids"`
	AllowProbe bool     `json:"allow_probe"`
}

type Message struct {
	ID        string    `json:"id"`
	Group     string    `json:"group_openid"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
	Author    struct {
		ID  string `json:"member_openid"`
		Bot bool   `json:"bot"`
	} `json:"author"`
}

type Status struct {
	State     string    `json:"state"`
	Detail    string    `json:"detail"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Source interface {
	Query(context.Context, Config, string, bool) (string, error)
}

type BoardSource interface {
	StatusBoard(context.Context, Config, string) (*Board, error)
}

// Claim must be atomic across replicas and persist beyond a reconnect/restart.
type Guard interface {
	Claim(context.Context, string, string, bool) (bool, error)
}

type Runtime struct {
	cfg         Config
	api         *client
	source      Source
	guard       Guard
	report      func(Status)
	session     string
	seq         *int64
	work        chan struct{}
	wg          sync.WaitGroup
	allowTestWS bool
}

func New(cfg Config, secret string, source Source, guard Guard, report func(Status)) *Runtime {
	return &Runtime{cfg: cfg, api: &client{http: httpClient(), baseURL: "https://api.bot.qq.com", appID: cfg.AppID, secret: secret}, source: source, guard: guard, report: report, work: make(chan struct{}, 4)}
}

func (r *Runtime) status(state, detail string) {
	if r.report != nil {
		r.report(Status{state, detail, time.Now().UTC()})
	}
}

func (r *Runtime) Run(ctx context.Context) {
	defer r.wg.Wait()
	backoff := time.Second
	for ctx.Err() == nil {
		r.status("connecting", "正在连接 QQ")
		started := time.Now()
		err := r.connect(ctx)
		if ctx.Err() != nil {
			return
		}
		var httpErr httpStatusError
		if errors.As(err, &httpErr) && httpErr == http.StatusUnauthorized {
			r.api.mu.Lock()
			r.api.token = ""
			r.api.mu.Unlock()
		}
		code := websocket.CloseStatus(err)
		switch code {
		case 4001, 4002, 4010, 4011, 4012, 4013, 4014, 4914, 4915:
			r.status("error", fmt.Sprintf("QQ 拒绝连接（%d），请检查机器人权限、上线状态或开发体验范围", code))
			<-ctx.Done()
			return
		case 4006, 4007:
			r.session = ""
			r.seq = nil
		}
		// No raw gateway errors: they can include URLs and credentials.
		r.status("reconnecting", "连接中断，正在自动重连；请检查凭据及服务器网络")
		if time.Since(started) > time.Minute {
			backoff = time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, time.Minute)
	}
}

type payload struct {
	Op   int             `json:"op"`
	Type string          `json:"t"`
	Seq  *int64          `json:"s"`
	Data json.RawMessage `json:"d"`
}

func (r *Runtime) connect(ctx context.Context) error {
	setupCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	token, err := r.api.accessToken(setupCtx)
	if err != nil {
		return err
	}
	var gateway struct {
		URL string `json:"url"`
	}
	if err := doJSON(setupCtx, r.api.http, http.MethodGet, r.api.baseURL+"/gateway", map[string]string{"Authorization": "QQBot " + token}, nil, &gateway); err != nil {
		return err
	}
	u, err := url.Parse(gateway.URL)
	if err != nil || u.User != nil || (!r.allowTestWS && (u.Scheme != "wss" || !(u.Hostname() == "qq.com" || strings.HasSuffix(u.Hostname(), ".qq.com")))) {
		return errors.New("invalid QQ gateway")
	}
	conn, resp, err := websocket.Dial(setupCtx, gateway.URL, &websocket.DialOptions{HTTPClient: r.api.http})
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return err
	}
	defer conn.CloseNow()
	conn.SetReadLimit(64 * 1024)
	var hello payload
	if err := wsjson.Read(setupCtx, conn, &hello); err != nil {
		return err
	}
	var timing struct {
		Interval int64 `json:"heartbeat_interval"`
	}
	if hello.Op != 10 || json.Unmarshal(hello.Data, &timing) != nil || timing.Interval < 100 || timing.Interval > 300000 {
		return errors.New("invalid QQ hello")
	}
	op := 2
	data := map[string]any{"token": "QQBot " + token, "intents": 1 << 25, "shard": []int{0, 1}}
	if r.session != "" && r.seq != nil {
		op = 6
		data = map[string]any{"token": "QQBot " + token, "session_id": r.session, "seq": *r.seq}
	}
	if err := wsjson.Write(setupCtx, conn, map[string]any{"op": op, "d": data}); err != nil {
		return err
	}
	// Read on one goroutine; heartbeat/identify writes are serialized here.
	readCtx, stop := context.WithCancel(ctx)
	defer stop()
	events := make(chan payload, 16)
	failures := make(chan error, 1)
	go func() {
		for {
			var p payload
			if err := wsjson.Read(readCtx, conn, &p); err != nil {
				failures <- err
				return
			}
			select {
			case events <- p:
			case <-readCtx.Done():
				return
			}
		}
	}()
	interval := time.Duration(timing.Interval) * time.Millisecond
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	readyTimer := time.NewTimer(30 * time.Second)
	defer readyTimer.Stop()
	awaitingAck := false
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-readyTimer.C:
			return errors.New("QQ ready timeout")
		case err := <-failures:
			return err
		case <-ticker.C:
			if awaitingAck {
				return errors.New("QQ heartbeat timeout")
			}
			writeCtx, done := context.WithTimeout(ctx, 5*time.Second)
			err := wsjson.Write(writeCtx, conn, map[string]any{"op": 1, "d": r.seq})
			done()
			if err != nil {
				return err
			}
			awaitingAck = true
		case p := <-events:
			switch p.Op {
			case 11:
				awaitingAck = false
			case 7:
				return errors.New("QQ requested reconnect")
			case 9:
				r.session = ""
				r.seq = nil
				r.api.mu.Lock()
				r.api.token = ""
				r.api.mu.Unlock()
				return errors.New("QQ invalid session")
			case 0:
				switch p.Type {
				case "READY":
					var ready struct {
						Session string `json:"session_id"`
					}
					if json.Unmarshal(p.Data, &ready) != nil || ready.Session == "" {
						return errors.New("invalid QQ ready")
					}
					r.session = ready.Session
					readyTimer.Stop()
					r.status("online", "已连接 QQ，等待群内指令")
				case "RESUMED":
					readyTimer.Stop()
					r.status("online", "QQ 连接已恢复")
				case "GROUP_AT_MESSAGE_CREATE":
					var msg Message
					if json.Unmarshal(p.Data, &msg) == nil {
						if err := r.dispatch(ctx, msg); err != nil {
							return err
						}
					}
				}
				if p.Seq != nil {
					r.seq = p.Seq
				}
			}
		}
	}
}

var mention = regexp.MustCompile(`^<@!?[^>]+>\s*`)

func Command(content string) (string, string) {
	if len(content) > 4096 {
		return "", ""
	}
	parts := strings.Fields(strings.TrimPrefix(strings.TrimSpace(mention.ReplaceAllString(strings.TrimSpace(content), "")), "/"))
	if len(parts) == 0 {
		return "", ""
	}
	switch parts[0] {
	case "渠道状态", "渠道状态文字", "检测", "帮助", "绑定信息":
		return parts[0], strings.Join(parts[1:], " ")
	}
	return "", ""
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

func (r *Runtime) dispatch(ctx context.Context, msg Message) error {
	name, query := Command(msg.Content)
	if name == "" || msg.ID == "" || msg.Group == "" || msg.Author.ID == "" || msg.Author.Bot || msg.Timestamp.IsZero() || time.Since(msg.Timestamp) > 4*time.Minute || msg.Timestamp.After(time.Now().Add(time.Minute)) {
		return nil
	}
	if name != "绑定信息" && !contains(r.cfg.Groups, msg.Group) {
		return nil
	}
	select {
	case r.work <- struct{}{}:
	default:
		return errors.New("QQ work queue full")
	}
	isProbe := name == "检测" && r.cfg.AllowProbe && contains(r.cfg.Admins, msg.Author.ID) && query != ""
	accepted, err := r.guard.Claim(ctx, r.cfg.AppID+":"+msg.Group+":"+msg.ID, r.cfg.AppID+":"+msg.Group, isProbe)
	if err != nil || !accepted {
		<-r.work
		return err
	}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer func() { <-r.work }()
		if name == "渠道状态" && r.replyBoard(ctx, msg, query) {
			return
		}
		reply := ""
		switch {
		case name == "绑定信息":
			reply = "本群 OpenID：" + msg.Group + "\n你的成员 OpenID：" + msg.Author.ID + "\n请管理员在后台 QQ 机器人设置中填写。OpenID 不是 QQ 群号。"
		case name == "帮助":
			reply = "@我 渠道状态 [平台或分组名]：状态看板图片\n@我 渠道状态文字 [名称]：文字结果\n@我 检测 编号：V1 即时检测（需授权，可能计费）\n@我 绑定信息：查看本群和成员标识\n只响应指令，不定时播报。"
		case name == "检测" && !isProbe:
			reply = "即时检测需在后台开启并指定管理员，同时提供渠道编号。可先查询“渠道状态”。"
		default:
			queryCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
			text, err := r.source.Query(queryCtx, r.cfg, query, isProbe)
			cancel()
			if err != nil {
				reply = "暂时无法读取或确认检测结果，请管理员检查后台。超时不代表检测未执行，请勿连续重试。"
			} else {
				reply = text
			}
		}
		if ctx.Err() != nil {
			return
		}
		if runes := []rune(reply); len(runes) > 2000 {
			reply = string(runes[:1950]) + "\n内容较多，请加名称筛选。"
		}
		sendCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		if err := r.api.reply(sendCtx, msg.Group, msg.ID, reply); err != nil {
			r.status("delivery_error", "QQ 回复失败，请检查机器人消息权限和服务器网络")
		}
	}()
	return nil
}
