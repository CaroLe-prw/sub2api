package qqbot

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

type ModerationConfig struct {
	Enabled        bool     `json:"enabled"`
	ObserveOnly    bool     `json:"observe_only"`
	ScanImages     bool     `json:"scan_images"`
	TrustedMembers []string `json:"trusted_members"`
}

type AdDecision struct {
	Verdict  string   `json:"verdict"` // allowed, suspected, advertisement
	Reason   string   `json:"reason"`
	Evidence []string `json:"evidence,omitempty"`
}

type ModerationRecord struct {
	At        time.Time `json:"at"`
	Group     string    `json:"group"`
	Member    string    `json:"member"`
	MessageID string    `json:"message_id"`
	Action    string    `json:"action"`
	AdDecision
}

type ModerationSource interface {
	ReviewMessage(context.Context, Config, Message) (AdDecision, error)
	RecordModeration(context.Context, ModerationRecord) error
}
type ModerationGuard interface {
	ClaimModeration(context.Context, string) (bool, error)
}

func normalizedAdText(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return unicode.ToLower(r)
	}, norm.NFKC.String(text))
}

func adMatches(text string, phrases ...string) []string {
	var hits []string
	for _, p := range phrases {
		if strings.Contains(text, p) {
			hits = append(hits, p)
		}
	}
	return hits
}

// EvaluateAdText requires combined commercial/solicitation signals. Generic
// mentions of AI, keys, prices, QR codes or concurrency alone never cause recall.
func EvaluateAdText(text string) AdDecision {
	text = normalizedAdText(text)
	commercial := adMatches(text, "gpt", "ai算力", "aitoken", "ai工具", "词元超市", "工具超市", "官方key", "api", "上游", "创业项目")
	contact := adMatches(text, "来私聊", "私聊我", "联系我", "加我", "加微信", "加微", "联系qq", "扫码咨询", "扫码加入", "欢迎咨询")
	supply := adMatches(text, "找稳定上游", "找上游", "求购", "收key", "收号", "求稳定上游", "寻找上游")
	resell := adMatches(text, "代理贴牌", "招代理", "招募代理", "支持贴牌", "加盟代理", "代理加盟")
	pitch := adMatches(text, "火爆", "量大", "便宜", "超市", "创业项目", "支持代理", "招募", "招商")
	profit := adMatches(text, "赚现金", "赚钱", "日赚", "月赚", "首月可挖", "稳赚", "躺赚", "返佣", "高额收益", "提现", "挖矿")
	leads := adMatches(text, "长按图片识别", "扫码", "扫描二维码", "识别二维码", "识别关注", "加我", "私聊", "免费注册")
	decision := AdDecision{Verdict: "allowed", Reason: "未命中广告组合规则"}
	switch {
	case len(profit) >= 2 && len(leads) > 0:
		decision = AdDecision{Verdict: "advertisement", Reason: "收益诱导与扫码/注册引流", Evidence: append(profit, leads...)}
	case len(commercial) > 0 && len(supply) > 0 && len(contact) > 0:
		decision = AdDecision{Verdict: "advertisement", Reason: "求购上游与私聊引流", Evidence: append(append(commercial, supply...), contact...)}
	case len(commercial) > 0 && len(resell) > 0 && len(pitch) > 0:
		decision = AdDecision{Verdict: "advertisement", Reason: "产品推广与代理招募", Evidence: append(append(commercial, resell...), pitch...)}
	case (len(commercial) > 0 && len(contact) > 0) || (len(profit) > 0 && len(leads) > 0) || len(resell) > 0:
		decision = AdDecision{Verdict: "suspected", Reason: "存在商业推广或引流特征，但证据不足", Evidence: append(append(contact, resell...), leads...)}
	}
	if decision.Verdict != "allowed" && len(adMatches(text, "这是广告", "这种广告", "有人发广告", "不要相信", "别信", "谨防", "诈骗", "骗子", "举报", "不要扫码", "别扫码", "如何识别广告", "是不是广告")) > 0 {
		decision.Verdict = "suspected"
		decision.Reason = "可能是在举报、引用或讨论广告，需人工判断"
	}
	if len(decision.Evidence) > 10 {
		decision.Evidence = decision.Evidence[:10]
	}
	return decision
}

func (r *Runtime) startModeration(ctx context.Context, msg Message) {
	if !r.cfg.Moderation.Enabled || !contains(r.cfg.Groups, msg.Group) || msg.ID == "" || msg.Author.ID == "" || msg.Author.Bot || msg.Timestamp.IsZero() {
		return
	}
	if msg.Author.Role == "owner" || msg.Author.Role == "admin" || contains(r.cfg.Admins, msg.Author.ID) || contains(r.cfg.Moderation.TrustedMembers, msg.Author.ID) {
		return
	}
	age := time.Since(msg.Timestamp)
	if age < -time.Minute || age > 115*time.Second {
		return
	}
	source, ok := r.source.(ModerationSource)
	if !ok {
		return
	}
	guard, ok := r.guard.(ModerationGuard)
	if !ok {
		return
	}
	select {
	case r.moderationSlots <- struct{}{}:
	default:
		return
	}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer func() { <-r.moderationSlots }()
		workCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
		defer cancel()
		claimed, err := guard.ClaimModeration(workCtx, r.cfg.AppID+":"+msg.Group+":"+msg.ID)
		if err != nil {
			slog.Warn("qq_bot: moderation deduplication unavailable", "error", err)
			r.status("moderation_error", "广告检查暂不可用，去重服务异常")
			return
		}
		if !claimed {
			return
		}
		decision, err := source.ReviewMessage(workCtx, r.cfg, msg)
		if err != nil {
			decision = AdDecision{Verdict: "suspected", Reason: "图片识别失败或不可用，仅记录"}
		}
		if decision.Verdict == "allowed" {
			return
		}
		record := ModerationRecord{At: time.Now().UTC(), Group: msg.Group, Member: msg.Author.ID, MessageID: msg.ID, Action: "recorded", AdDecision: decision}
		if decision.Verdict == "advertisement" && !r.cfg.Moderation.ObserveOnly {
			switch {
			case msg.Author.Role != "member":
				record.Reason += "；成员角色未确认，不自动撤回"
			case time.Since(msg.Timestamp) >= 115*time.Second:
				record.Reason += "；已接近撤回时限，不再执行"
			default:
				if err := r.recallModeratedMessage(workCtx, msg); err != nil {
					record.Action = "failed"
					record.Reason += "；撤回失败：" + err.Error()
				} else {
					record.Action = "recalled"
				}
			}
		}
		// Record even if image processing exhausted its own timeout.
		logCtx, done := context.WithTimeout(ctx, 2*time.Second)
		defer done()
		if err := source.RecordModeration(logCtx, record); err != nil {
			r.status("moderation_error", "广告处理记录写入失败")
		}
	}()
}

func (r *Runtime) recallModeratedMessage(ctx context.Context, msg Message) error {
	select {
	case r.recallGate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-r.recallGate }()
	if delay := time.Until(r.nextRecall); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if time.Since(msg.Timestamp) >= 115*time.Second {
		return errors.New("超过安全撤回窗口")
	}
	r.nextRecall = time.Now().Add(150 * time.Millisecond)
	return r.api.recallGroupMessage(ctx, msg.Group, msg.ID)
}
