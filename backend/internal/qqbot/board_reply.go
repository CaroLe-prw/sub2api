package qqbot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

func (r *Runtime) replyBoard(ctx context.Context, msg Message, query string) bool {
	source, ok := r.source.(BoardSource)
	if !ok {
		return false
	}
	queryCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	board, err := source.StatusBoard(queryCtx, r.cfg, query)
	cancel()
	if err == nil && board == nil {
		return false
	}
	sendCtx, done := context.WithTimeout(ctx, 90*time.Second)
	defer done()
	fallback := func(text string, seq int) {
		if sendCtx.Err() != nil {
			return
		}
		if e := r.api.replyText(sendCtx, msg.Group, msg.ID, text, seq); e != nil {
			slog.Warn("qq_bot: fallback reply failed", "error", e)
			r.status("delivery_error", "QQ 文字回复失败："+e.Error())
		}
	}
	if err != nil {
		fallback("暂时无法读取渠道看板，请管理员检查监控配置。", 1)
		return true
	}
	images, err := RenderBoard(*board)
	if err != nil {
		slog.Warn("qq_bot: board rendering failed", "error", err)
		fallback(boardFallback(*board, "图片生成暂不可用，请管理员检查中文字体。"), 1)
		return true
	}
	for i, data := range images {
		if err := r.api.replyImage(sendCtx, msg.Group, msg.ID, msg.Author.ID, data, i+1); err != nil {
			slog.Warn("qq_bot: image delivery failed", "page", i+1, "error", err)
			// Same sequence as the failed send avoids a second visible reply if
			// QQ accepted the image but its HTTP response was lost.
			fallback(boardFallback(*board, "图片发送暂不可用，以下为文字摘要。"), i+1)
			r.status("delivery_error", "QQ 图片发送失败："+err.Error()+"；已尝试文字回复")
			return true
		}
	}
	r.status("online", "已连接 QQ，状态看板图片发送成功")
	return true
}

func boardFallback(board Board, reason string) string {
	lines := []string{reason, board.Title + " · 最近监控结果（非即时实测）"}
	if board.Stale {
		lines = append(lines, "数据已过期或缺失，无法判断当前状态。")
	}
	if !board.Through.IsZero() {
		lines = append(lines, "数据截至："+board.Through.In(time.FixedZone("UTC+8", 8*3600)).Format("01-02 15:04:05")+" 北京时间")
	}
	for i, card := range board.Cards {
		if i >= 10 {
			lines = append(lines, "更多结果请按平台或分组名筛选。")
			break
		}
		name := []rune(card.Name)
		if len(name) > 60 {
			name = name[:60]
		}
		lines = append(lines, fmt.Sprintf("%s：%s｜可用率 %s｜缓存率 %s｜首 Token %s", string(name), stateName(card.State), card.Availability, card.Cache, card.TTFT))
	}
	if len(board.Cards) == 0 {
		lines = append(lines, "没有匹配数据，不代表渠道正常。")
	}
	return strings.Join(lines, "\n")
}
