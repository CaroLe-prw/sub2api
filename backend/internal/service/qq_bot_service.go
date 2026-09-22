package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/qqbot"
	"github.com/google/uuid"
)

const qqBotSettingKey = "qq_bot_config"

// QQBotCache coordinates connection ownership, command deduplication and status
// across replicas. Storage details belong to the repository implementation.
type QQBotCache interface {
	qqbot.Guard
	AcquireOrRenewLease(context.Context, string) (bool, error)
	ReleaseLease(context.Context, string) error
	GetStatus(context.Context) (qqbot.Status, error)
	SetStatus(context.Context, qqbot.Status) error
	RefreshStatus(context.Context) error
}

type qqBotStored struct {
	qqbot.Config
	SecretEncrypted string `json:"secret_encrypted"`
}
type QQBotUpdate struct {
	qqbot.Config
	AppSecret   string `json:"app_secret"`
	ClearSecret bool   `json:"clear_secret"`
}
type QQBotView struct {
	qqbot.Config
	SecretConfigured bool         `json:"secret_configured"`
	MonitorMode      string       `json:"monitor_mode"`
	Status           qqbot.Status `json:"status"`
}

type QQBotService struct {
	repo      SettingRepository
	encryptor SecretEncryptor
	cache     QQBotCache
	settings  *SettingService
	v1        *ChannelMonitorService
	v2        *ChannelMonitorV2Service
	updateMu  sync.Mutex
	cancel    context.CancelFunc
	done      chan struct{}
}

func NewQQBotService(repo SettingRepository, encryptor SecretEncryptor, cache QQBotCache, settings *SettingService, v1 *ChannelMonitorService, v2 *ChannelMonitorV2Service) *QQBotService {
	return &QQBotService{repo: repo, encryptor: encryptor, cache: cache, settings: settings, v1: v1, v2: v2}
}

func ProvideQQBotService(repo SettingRepository, encryptor SecretEncryptor, cache QQBotCache, settings *SettingService, v1 *ChannelMonitorService, v2 *ChannelMonitorV2Service) *QQBotService {
	s := NewQQBotService(repo, encryptor, cache, settings, v1, v2)
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done = make(chan struct{})
	go func() { defer close(s.done); s.loop(ctx) }()
	return s
}

func (s *QQBotService) Stop() {
	if s.cancel != nil {
		s.cancel()
		<-s.done
	}
}

func (s *QQBotService) stored(ctx context.Context) (qqBotStored, error) {
	var cfg qqBotStored
	raw, err := s.repo.GetValue(ctx, qqBotSettingKey)
	if errors.Is(err, ErrSettingNotFound) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if strings.TrimSpace(raw) == "" {
		return cfg, nil
	}
	if json.Unmarshal([]byte(raw), &cfg) != nil {
		return cfg, errors.New("QQ bot configuration is invalid")
	}
	return cfg, nil
}

func (s *QQBotService) Get(ctx context.Context) (*QQBotView, error) {
	cfg, err := s.stored(ctx)
	if err != nil {
		return nil, err
	}
	view := &QQBotView{Config: cfg.Config, SecretConfigured: cfg.SecretEncrypted != "", MonitorMode: s.settings.GetChannelMonitorRuntime(ctx).Mode}
	view.Status = qqbot.Status{State: "disabled", Detail: "未启用"}
	if cfg.Enabled {
		view.Status = qqbot.Status{State: "connecting", Detail: "等待连接，配置最多约 5 秒生效"}
		if s.cache != nil {
			status, err := s.cache.GetStatus(ctx)
			if err == nil {
				view.Status = status
			}
		}
	}
	return view, nil
}

var qqBotIdentifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func normalizeQQIDs(values []string) ([]string, error) {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	if len(values) > 100 {
		return nil, errors.New("最多配置 100 个标识")
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if !qqBotIdentifier.MatchString(value) {
			return nil, errors.New("OpenID 格式不正确")
		}
		if !seen[value] {
			out = append(out, value)
			seen[value] = true
		}
	}
	return out, nil
}

func (s *QQBotService) Update(ctx context.Context, input QQBotUpdate) (*QQBotView, error) {
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	bad := func(message string) (*QQBotView, error) {
		return nil, infraerrors.BadRequest("QQ_BOT_CONFIG_INVALID", message)
	}
	old, err := s.stored(ctx)
	if err != nil {
		return nil, err
	}
	input.AppID = strings.TrimSpace(input.AppID)
	if input.AppID != "" && !regexp.MustCompile(`^[0-9]{1,32}$`).MatchString(input.AppID) {
		return bad("AppID 必须是数字")
	}
	if len(input.AppSecret) > 512 {
		return bad("AppSecret 过长")
	}
	input.Groups, err = normalizeQQIDs(input.Groups)
	if err != nil {
		return bad(err.Error())
	}
	input.Admins, err = normalizeQQIDs(input.Admins)
	if err != nil {
		return bad(err.Error())
	}
	for _, ids := range [][]int64{input.MonitorIDs, input.GroupIDs} {
		if len(ids) > 100 {
			return bad("最多配置 100 个监控或分组")
		}
		for _, id := range ids {
			if id <= 0 {
				return bad("监控和分组编号必须是正整数")
			}
		}
	}
	next := qqBotStored{Config: input.Config, SecretEncrypted: old.SecretEncrypted}
	if input.ClearSecret {
		next.SecretEncrypted = ""
	}
	if input.AppID != old.AppID && input.AppSecret == "" {
		next.SecretEncrypted = ""
	}
	if input.AppSecret != "" {
		next.SecretEncrypted, err = s.encryptor.Encrypt(input.AppSecret)
		if err != nil {
			return nil, errors.New("QQ secret encryption failed")
		}
	}
	if input.Enabled {
		if next.AppID == "" || next.SecretEncrypted == "" {
			return bad("启用前请填写 AppID 和 AppSecret")
		}
		if s.settings.GetChannelMonitorRuntime(ctx).Mode == ChannelMonitorModeV2 && len(next.Groups) > 0 && len(next.GroupIDs) == 0 {
			return bad("V2 模式请明确配置允许群成员查看的分组编号")
		}
	}
	data, err := json.Marshal(next)
	if err != nil {
		return nil, err
	}
	if err = s.repo.Set(ctx, qqBotSettingKey, string(data)); err != nil {
		return nil, err
	}
	return s.Get(ctx)
}

func (s *QQBotService) loop(ctx context.Context) {
	token := uuid.NewString()
	var activeCancel context.CancelFunc
	var activeDone chan struct{}
	var activeConfig string
	stop := func() {
		if activeCancel != nil {
			activeCancel()
			<-activeDone
			activeCancel = nil
			activeConfig = ""
		}
	}
	defer func() {
		stop()
		c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.cache.ReleaseLease(c, token)
	}()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		cfg, err := s.stored(checkCtx)
		owned := false
		if err == nil && cfg.Enabled {
			lease, e := s.cache.AcquireOrRenewLease(checkCtx, token)
			owned = e == nil && lease
		}
		cancel()
		if !owned {
			stop()
		} else {
			encoded, _ := json.Marshal(cfg)
			if activeConfig != string(encoded) {
				stop()
				secret, e := s.encryptor.Decrypt(cfg.SecretEncrypted)
				report := func(status qqbot.Status) {
					c, cancel := context.WithTimeout(ctx, 2*time.Second)
					defer cancel()
					_ = s.cache.SetStatus(c, status)
				}
				if e != nil || secret == "" {
					report(qqbot.Status{State: "error", Detail: "凭据解密失败，请重新填写 AppSecret", UpdatedAt: time.Now()})
				} else {
					runCtx, c := context.WithCancel(ctx)
					activeCancel = c
					activeDone = make(chan struct{})
					activeConfig = string(encoded)
					runtime := qqbot.New(cfg.Config, secret, s, s.cache, report)
					go func(done chan struct{}) { defer close(done); runtime.Run(runCtx) }(activeDone)
				}
			}
			// Keep an online/error snapshot visible on every application replica.
			c, cancel := context.WithTimeout(ctx, 2*time.Second)
			_ = s.cache.RefreshStatus(c)
			cancel()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func qqContainsID(ids []int64, id int64) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

func qqStatusLabel(status string) string {
	switch status {
	case "operational", "healthy":
		return "正常"
	case "degraded", "warning":
		return "有波动"
	case "failed", "critical":
		return "异常"
	case "error":
		return "检测出错"
	default:
		return "未知"
	}
}
func qqSafe(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	r := []rune(value)
	if len(r) > 80 {
		return string(r[:80]) + "…"
	}
	return value
}
func qqTime(t time.Time) string {
	if t.IsZero() {
		return "暂无时间"
	}
	return t.In(time.FixedZone("UTC+8", 8*3600)).Format("01-02 15:04:05")
}
func qqMatches(query string, values ...string) bool {
	for _, v := range values {
		if strings.Contains(strings.ToLower(v), strings.ToLower(query)) {
			return true
		}
	}
	return false
}

func (s *QQBotService) Query(ctx context.Context, cfg qqbot.Config, query string, probe bool) (string, error) {
	runtime := s.settings.GetChannelMonitorRuntime(ctx)
	if !runtime.Enabled {
		return "渠道监控未开启，请管理员在后台开启。", nil
	}
	if runtime.Mode == ChannelMonitorModeV2 {
		if probe {
			return "V2 使用已有流量和探测汇总；当前不支持通过机器人发起即时实测。", nil
		}
		if len(cfg.GroupIDs) == 0 {
			return "尚未配置允许展示的分组，请管理员在后台 QQ 机器人设置中选择。", nil
		}
		filter, err := s.v2.ParseFilter("90m", nil, nil, cfg.GroupIDs)
		if err != nil {
			return "", err
		}
		matrix, err := s.v2.Matrix(ctx, filter, ChannelMonitorV2GroupByPlatformGroupModel, true)
		if err != nil {
			return "", err
		}
		lines := []string{"渠道状态（最近 90 分钟汇总，非即时实测）", "数据截至：" + qqTime(matrix.Coverage.DataThrough) + " 北京时间"}
		stale := matrix.Coverage.DataThrough.IsZero() || time.Since(matrix.Coverage.DataThrough) > 15*time.Minute
		if stale {
			lines = append(lines, "数据缺失或过期，无法判断当前可用性。")
		}
		count := 0
		for _, row := range matrix.Items {
			if row.GroupID == nil || !qqContainsID(cfg.GroupIDs, *row.GroupID) || !qqMatches(query, row.GroupName, row.Platform, row.Model) {
				continue
			}
			count++
			if count > 10 {
				continue
			}
			label := qqStatusLabel(row.Health.Overall)
			if stale {
				label = "旧数据：" + label
			}
			line := fmt.Sprintf("%s / %s / %s：%s", qqSafe(row.GroupName), qqSafe(row.Platform), qqSafe(row.Model), label)
			if row.Metrics.TTFT.P50Ms != nil {
				line += fmt.Sprintf("｜首输出 %dms", *row.Metrics.TTFT.P50Ms)
			}
			lines = append(lines, line)
		}
		if count == 0 {
			lines = append(lines, "没有匹配的数据，不代表渠道正常。")
		}
		if count > 10 {
			lines = append(lines, "结果较多，请加分组或模型名称筛选。")
		}
		return strings.Join(lines, "\n"), nil
	}
	views, err := s.v1.ListUserView(ctx)
	if err != nil {
		return "", err
	}
	allowed := make([]*UserMonitorView, 0, len(views))
	for _, view := range views {
		if len(cfg.MonitorIDs) == 0 || qqContainsID(cfg.MonitorIDs, view.ID) {
			allowed = append(allowed, view)
		}
	}
	if probe {
		var selected []*UserMonitorView
		for _, view := range allowed {
			if fmt.Sprint(view.ID) == strings.TrimPrefix(query, "#") || strings.EqualFold(view.Name, query) {
				selected = append(selected, view)
			}
		}
		if len(selected) != 1 {
			return "请指定唯一的已公开渠道编号或完整名称，例如“检测 3”。", nil
		}
		monitor, err := s.v1.Get(ctx, selected[0].ID)
		if err != nil {
			return "", err
		}
		if !monitor.Enabled || !monitor.PublicVisible {
			return "该渠道不再公开或已停用。", nil
		}
		results, err := s.v1.RunCheck(ctx, monitor.ID)
		if err != nil {
			return "", err
		}
		lines := []string{"即时检测：" + qqSafe(monitor.Name)}
		if monitor.CheckMode == "quota" {
			lines[0] += "（仅配额，不验证模型调用）"
		}
		for i, result := range results {
			if i >= 10 {
				break
			}
			lines = append(lines, qqSafe(result.Model)+"："+qqStatusLabel(result.Status)+"｜"+qqTime(result.CheckedAt))
		}
		if len(results) == 0 {
			lines = append(lines, "未返回结果，无法判断状态。")
		}
		return strings.Join(lines, "\n"), nil
	}
	lines := []string{"渠道状态（最近监控结果，非即时实测；北京时间）"}
	count := 0
	for _, view := range allowed {
		if !qqMatches(query, view.Name, view.PrimaryModel, fmt.Sprint(view.ID)) {
			continue
		}
		count++
		if count > 10 {
			continue
		}
		var checked time.Time
		if len(view.Timeline) > 0 {
			checked = view.Timeline[0].CheckedAt
		}
		label := qqStatusLabel(view.PrimaryStatus)
		staleAfter := 3 * time.Duration(view.IntervalSeconds) * time.Second
		if staleAfter < 5*time.Minute {
			staleAfter = 5 * time.Minute
		}
		if checked.IsZero() {
			label = "暂无检测数据"
		} else if time.Since(checked) > staleAfter {
			label = "旧数据：" + label
		}
		if view.CheckMode == "quota" {
			label += "（仅配额检查）"
		}
		line := fmt.Sprintf("#%d %s / %s：%s｜%s", view.ID, qqSafe(view.Name), qqSafe(view.PrimaryModel), label, qqTime(checked))
		if view.PrimaryLatencyMs != nil {
			line += fmt.Sprintf("｜%dms", *view.PrimaryLatencyMs)
		}
		lines = append(lines, line)
	}
	if count == 0 {
		lines = append(lines, "没有匹配的已公开渠道。")
	} else if count > 10 {
		lines = append(lines, "结果较多，请加渠道名称筛选。")
	}
	return strings.Join(lines, "\n"), nil
}
