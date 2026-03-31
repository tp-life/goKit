package tui

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"goKit/internal/application/dto"
	service "goKit/internal/application/service"
	"goKit/internal/domain/entity"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	priceHistoryLimit = 30
	flashTTL          = 2 * time.Second
	confirmTTL        = 5 * time.Second
	statusTTL         = 6 * time.Second
	actionTimeout     = 20 * time.Second
	tradeTabIndex     = 4
	// positionSizeDiffTolerance 用于忽略钱包持仓与本地持仓之间的微小浮点误差。
	positionSizeDiffTolerance = 0.0001
)

var (
	rootStyle = lipgloss.NewStyle().
			Padding(1, 2).
			Foreground(lipgloss.Color("#E8EEF8"))

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#88C0FF"))

	tabStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(lipgloss.Color("#7B8BA8"))

	activeTabStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Bold(true).
			Foreground(lipgloss.Color("#111827")).
			Background(lipgloss.Color("#8CC6FF"))

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#2C4767")).
			Padding(0, 1).
			MarginTop(1)

	mutedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#93A4BF"))

	okStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#39D98A"))

	warnStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F5C451"))

	errStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF7A90"))
)

type snapshotMsg struct {
	state entity.DashboardState
}

type tickMsg struct {
	at time.Time
}

type actionResultMsg struct {
	action  string
	message string
	level   string
	err     error
}

type streamClosedMsg struct{}

type priceSeries struct {
	chainlink []float64
	binance   []float64
	ptb       []float64
	diff      []float64
	up        []float64
	down      []float64
}

type flashMarker struct {
	direction int
	changedAt time.Time
}

type confirmState struct {
	key       string
	action    string
	label     string
	expiresAt time.Time
	req       *dto.ManualOrderReq
}

type tradeField int

const (
	tradeFieldAmount tradeField = iota
	tradeFieldProbability
)

type tradeForm struct {
	action            string
	outcome           string
	amountInput       string
	probabilityInput  string
	focus             tradeField
	editing           bool
	editBuffer        string
	editOriginal      string
	customAmount      bool
	customProbability bool
}

type model struct {
	cfg     Config
	svc     *service.PolymarketService
	updates <-chan entity.DashboardState

	state     entity.DashboardState
	activeTab int
	width     int
	height    int
	now       time.Time

	series  priceSeries
	flashes map[string]flashMarker
	confirm confirmState
	trade   tradeForm

	statusMessage string
	statusLevel   string
	statusUntil   time.Time
}

// newModel 创建 TUI 视图模型，并预装当前快照避免首屏空白。
func newModel(cfg Config, svc *service.PolymarketService, updates <-chan entity.DashboardState) model {
	state := svc.Snapshot()
	m := model{
		cfg:     cfg,
		svc:     svc,
		updates: updates,
		state:   state,
		now:     time.Now(),
		flashes: map[string]flashMarker{},
	}
	m.seedSeries(state)
	m.syncTradeForm(true)
	return m
}

// Init 启动订阅与定时刷新命令。
func (m model) Init() tea.Cmd {
	return tea.Batch(waitForSnapshot(m.updates), tickEvery(m.cfg.RefreshInterval()))
}

// Update 处理键盘事件、服务快照与定时刷新消息。
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = typed.Width
		m.height = typed.Height
		return m, nil
	case tea.KeyMsg:
		if m.activeTab == tradeTabIndex {
			if next, cmd, handled := m.handleTradeKey(typed); handled {
				return next, cmd
			}
		}

		switch typed.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "tab", "right", "l":
			m.activeTab = (m.activeTab + 1) % len(tabTitles())
			return m, nil
		case "shift+tab", "left", "h":
			m.activeTab--
			if m.activeTab < 0 {
				m.activeTab = len(tabTitles()) - 1
			}
			return m, nil
		case "1", "2", "3", "4", "5":
			m.activeTab = int(typed.Runes[0] - '1')
			if m.activeTab < 0 || m.activeTab >= len(tabTitles()) {
				m.activeTab = 0
			}
			return m, nil
		case "r":
			// 手动刷新只读取当前服务内存快照，不额外发网络请求。
			next := m.svc.Snapshot()
			m.recordSnapshot(next)
			m.state = next
			m.syncTradeForm(false)
			m.now = time.Now()
			m.setStatus("OK", "已刷新当前快照")
			return m, nil
		case "esc":
			m.confirm = confirmState{}
			return m, nil
		case "u":
			return m.armOrExecuteAction("u", "buy_up", "快捷买入 UP")
		case "d":
			return m.armOrExecuteAction("d", "buy_down", "快捷买入 DOWN")
		case "s":
			return m.armOrExecuteAction("s", "sell_current", "快捷卖出当前持仓")
		case "c":
			return m.armOrExecuteAction("c", "cancel_order", "撤销当前挂单")
		}
	case snapshotMsg:
		m.recordSnapshot(typed.state)
		m.state = typed.state
		m.syncTradeForm(false)
		m.now = time.Now()
		return m, waitForSnapshot(m.updates)
	case tickMsg:
		m.now = typed.at
		m.clearExpiredState(typed.at)
		return m, tickEvery(m.cfg.RefreshInterval())
	case actionResultMsg:
		if typed.err != nil {
			m.setStatus("ERR", typed.err.Error())
		} else {
			m.setStatus(typed.level, typed.message)
		}
		return m, nil
	case streamClosedMsg:
		return m, tea.Quit
	}

	return m, nil
}

// View 渲染当前终端界面的完整内容。
func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "正在初始化 TUI..."
	}

	header := m.renderHeader()
	tabs := m.renderTabs()
	body := m.renderActiveTab()
	footer := m.renderFooter()

	return rootStyle.Width(m.width).Render(
		lipgloss.JoinVertical(lipgloss.Left, header, tabs, body, footer),
	)
}

// waitForSnapshot 等待服务层推送下一条 dashboard 快照。
func waitForSnapshot(ch <-chan entity.DashboardState) tea.Cmd {
	return func() tea.Msg {
		snapshot, ok := <-ch
		if !ok {
			return streamClosedMsg{}
		}
		return snapshotMsg{state: snapshot}
	}
}

// tickEvery 定期更新时间显示，避免界面时钟停住。
func tickEvery(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return tickMsg{at: t}
	})
}

// armOrExecuteAction 先进入二次确认状态，再在同一热键再次按下时真正执行敏感操作。
func (m model) armOrExecuteAction(key, action, label string) (model, tea.Cmd) {
	now := time.Now()
	if m.confirm.action == action && m.confirm.key == key && now.Before(m.confirm.expiresAt) {
		m.confirm = confirmState{}
		m.setStatus("WARN", fmt.Sprintf("正在执行: %s", label))
		return m, executeActionCmd(m.svc, action)
	}

	m.confirm = confirmState{
		key:       key,
		action:    action,
		label:     label,
		expiresAt: now.Add(confirmTTL),
		req:       nil,
	}
	m.setStatus("WARN", fmt.Sprintf("再次按 %s 确认%s", strings.ToUpper(key), label))
	return m, nil
}

// handleTradeKey 处理交易 tab 内部的表单交互与精确下单。
func (m model) handleTradeKey(key tea.KeyMsg) (model, tea.Cmd, bool) {
	if m.trade.editing {
		return m.handleTradeEditKey(key)
	}

	switch key.String() {
	case "j", "down":
		m.trade.focus = nextTradeField(m.trade.focus)
		return m, nil, true
	case "k", "up":
		m.trade.focus = prevTradeField(m.trade.focus)
		return m, nil, true
	case "b":
		m.trade.action = "BUY"
		m.syncTradeForm(false)
		return m, nil, true
	case "s":
		m.trade.action = "SELL"
		m.syncTradeForm(false)
		return m, nil, true
	case "o":
		if strings.EqualFold(m.trade.action, "SELL") && m.state.Position != nil {
			m.setStatus("WARN", "卖出方向会跟随当前持仓")
			return m, nil, true
		}
		if strings.EqualFold(m.trade.outcome, "UP") {
			m.trade.outcome = "DOWN"
		} else {
			m.trade.outcome = "UP"
		}
		m.trade.customProbability = false
		m.syncTradeForm(false)
		return m, nil, true
	case "u":
		if strings.EqualFold(m.trade.action, "SELL") && m.state.Position != nil {
			m.setStatus("WARN", "卖出方向会跟随当前持仓")
			return m, nil, true
		}
		m.trade.outcome = "UP"
		m.trade.customProbability = false
		m.syncTradeForm(false)
		return m, nil, true
	case "d":
		if strings.EqualFold(m.trade.action, "SELL") && m.state.Position != nil {
			m.setStatus("WARN", "卖出方向会跟随当前持仓")
			return m, nil, true
		}
		m.trade.outcome = "DOWN"
		m.trade.customProbability = false
		m.syncTradeForm(false)
		return m, nil, true
	case "+", "=":
		m.bumpTradeField(1)
		return m, nil, true
	case "-", "_":
		m.bumpTradeField(-1)
		return m, nil, true
	case "e":
		m.startTradeEdit()
		return m, nil, true
	case "x":
		m.resetTradeForm()
		m.setStatus("OK", "交易表单已重置为市场默认值")
		return m, nil, true
	case "c":
		next, cmd := m.armOrExecuteAction("c", "cancel_order", "撤销当前挂单")
		return next, cmd, true
	case "enter":
		return m.armOrExecuteTradeSubmit()
	}

	return m, nil, false
}

// handleTradeEditKey 处理数值输入模式下的按键行为。
func (m model) handleTradeEditKey(key tea.KeyMsg) (model, tea.Cmd, bool) {
	switch key.String() {
	case "esc":
		m.trade.editing = false
		m.trade.editBuffer = ""
		m.trade.editOriginal = ""
		m.setStatus("WARN", "已取消当前编辑")
		return m, nil, true
	case "enter":
		if err := m.commitTradeEdit(); err != nil {
			m.setStatus("ERR", err.Error())
		}
		return m, nil, true
	case "backspace":
		if len(m.trade.editBuffer) > 0 {
			m.trade.editBuffer = m.trade.editBuffer[:len(m.trade.editBuffer)-1]
		}
		return m, nil, true
	}

	if isTradeInputRune(key) {
		m.trade.editBuffer += key.String()
		return m, nil, true
	}
	return m, nil, true
}

// armOrExecuteTradeSubmit 为当前交易表单生成一条待确认的下单请求。
func (m model) armOrExecuteTradeSubmit() (model, tea.Cmd, bool) {
	req, label, err := m.buildTradeFormReq()
	if err != nil {
		m.setStatus("ERR", err.Error())
		return m, nil, true
	}

	now := time.Now()
	if m.confirm.action == "submit_trade" && m.confirm.key == "enter" && now.Before(m.confirm.expiresAt) {
		m.confirm = confirmState{}
		m.setStatus("WARN", fmt.Sprintf("正在执行: %s", label))
		return m, executeManualOrderCmd(m.svc, req), true
	}

	reqCopy := req
	m.confirm = confirmState{
		key:       "enter",
		action:    "submit_trade",
		label:     label,
		expiresAt: now.Add(confirmTTL),
		req:       &reqCopy,
	}
	m.setStatus("WARN", "再次按 Enter 确认提交当前交易表单")
	return m, nil, true
}

// executeActionCmd 在后台调用 service 层的快捷操作，并把结果回推给 TUI 主循环。
func executeActionCmd(svc *service.PolymarketService, action string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
		defer cancel()

		switch action {
		case "buy_up":
			resp, err := svc.SubmitTUIQuickOrder(ctx, "BUY", "UP")
			if err != nil {
				return actionResultMsg{action: action, err: err}
			}
			return actionResultMsg{
				action:  action,
				level:   "TRADE",
				message: fmt.Sprintf("已提交 BUY UP @ %.2f%%", resp.Price*100),
			}
		case "buy_down":
			resp, err := svc.SubmitTUIQuickOrder(ctx, "BUY", "DOWN")
			if err != nil {
				return actionResultMsg{action: action, err: err}
			}
			return actionResultMsg{
				action:  action,
				level:   "TRADE",
				message: fmt.Sprintf("已提交 BUY DOWN @ %.2f%%", resp.Price*100),
			}
		case "sell_current":
			resp, err := svc.SubmitTUIQuickOrder(ctx, "SELL", "")
			if err != nil {
				return actionResultMsg{action: action, err: err}
			}
			return actionResultMsg{
				action:  action,
				level:   "TRADE",
				message: fmt.Sprintf("已提交 SELL %s @ %.2f%%", resp.Outcome, resp.Price*100),
			}
		case "cancel_order":
			if err := svc.CancelActiveOrder(ctx); err != nil {
				return actionResultMsg{action: action, err: err}
			}
			return actionResultMsg{
				action:  action,
				level:   "WARN",
				message: "已撤销当前挂单",
			}
		default:
			return actionResultMsg{
				action: action,
				err:    fmt.Errorf("未知操作: %s", action),
			}
		}
	}
}

// executeManualOrderCmd 在后台提交一笔自定义手动单，并把结果回推给 TUI。
func executeManualOrderCmd(svc *service.PolymarketService, req dto.ManualOrderReq) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
		defer cancel()

		resp, err := svc.SubmitManualOrder(ctx, req)
		if err != nil {
			return actionResultMsg{action: "submit_trade", err: err}
		}
		return actionResultMsg{
			action:  "submit_trade",
			level:   "TRADE",
			message: fmt.Sprintf("已提交 %s %s @ %.2f%%", resp.Action, resp.Outcome, resp.Price*100),
		}
	}
}

// seedSeries 用初始快照填充价格序列，避免首屏趋势图为空。
func (m *model) seedSeries(state entity.DashboardState) {
	m.series.chainlink = appendValue(m.series.chainlink, state.Prices.ChainlinkBTC, priceHistoryLimit)
	m.series.binance = appendValue(m.series.binance, state.Prices.BinanceBTC, priceHistoryLimit)
	m.series.ptb = appendValue(m.series.ptb, state.Prices.PTB, priceHistoryLimit)
	m.series.diff = appendValue(m.series.diff, state.Prices.Diff, priceHistoryLimit)
	m.series.up = appendValue(m.series.up, state.Prices.UpPrice, priceHistoryLimit)
	m.series.down = appendValue(m.series.down, state.Prices.DownPrice, priceHistoryLimit)
}

// recordSnapshot 记录最新快照的价格序列，供趋势图稳定展示最近一段走势。
func (m *model) recordSnapshot(next entity.DashboardState) {
	now := time.Now()
	m.updateFlash("chainlink", m.state.Prices.ChainlinkBTC, next.Prices.ChainlinkBTC, now)
	m.updateFlash("binance", m.state.Prices.BinanceBTC, next.Prices.BinanceBTC, now)
	m.updateFlash("ptb", m.state.Prices.PTB, next.Prices.PTB, now)
	m.updateFlash("diff", m.state.Prices.Diff, next.Prices.Diff, now)
	m.updateFlash("up", m.state.Prices.UpPrice, next.Prices.UpPrice, now)
	m.updateFlash("down", m.state.Prices.DownPrice, next.Prices.DownPrice, now)

	m.series.chainlink = appendValue(m.series.chainlink, next.Prices.ChainlinkBTC, priceHistoryLimit)
	m.series.binance = appendValue(m.series.binance, next.Prices.BinanceBTC, priceHistoryLimit)
	m.series.ptb = appendValue(m.series.ptb, next.Prices.PTB, priceHistoryLimit)
	m.series.diff = appendValue(m.series.diff, next.Prices.Diff, priceHistoryLimit)
	m.series.up = appendValue(m.series.up, next.Prices.UpPrice, priceHistoryLimit)
	m.series.down = appendValue(m.series.down, next.Prices.DownPrice, priceHistoryLimit)
}

// updateFlash 对比前后两个价格，记录最近一次涨跌方向用于界面高亮。
func (m *model) updateFlash(key string, prev, next *float64, changedAt time.Time) {
	if prev == nil || next == nil {
		return
	}
	switch {
	case *next > *prev:
		m.flashes[key] = flashMarker{direction: 1, changedAt: changedAt}
	case *next < *prev:
		m.flashes[key] = flashMarker{direction: -1, changedAt: changedAt}
	}
}

// clearExpiredState 清理过期的确认提示和状态提示。
func (m *model) clearExpiredState(now time.Time) {
	if !m.confirm.expiresAt.IsZero() && now.After(m.confirm.expiresAt) {
		m.confirm = confirmState{}
	}
	if !m.statusUntil.IsZero() && now.After(m.statusUntil) {
		m.statusMessage = ""
		m.statusLevel = ""
		m.statusUntil = time.Time{}
	}
	for key, marker := range m.flashes {
		if now.Sub(marker.changedAt) > flashTTL {
			delete(m.flashes, key)
		}
	}
}

// setStatus 设置底部状态栏消息。
func (m *model) setStatus(level, message string) {
	m.statusLevel = strings.ToUpper(strings.TrimSpace(level))
	m.statusMessage = strings.TrimSpace(message)
	m.statusUntil = time.Now().Add(statusTTL)
}

// syncTradeForm 根据当前市场和仓位状态补齐交易表单默认值。
func (m *model) syncTradeForm(force bool) {
	if force || (m.trade.action != "BUY" && m.trade.action != "SELL") {
		m.trade.action = "BUY"
	}
	if force || (m.trade.outcome != "UP" && m.trade.outcome != "DOWN") {
		m.trade.outcome = "UP"
	}
	if strings.EqualFold(m.trade.action, "SELL") && m.state.Position != nil {
		m.trade.outcome = strings.ToUpper(strings.TrimSpace(m.state.Position.Side))
	}

	if force || strings.TrimSpace(m.trade.amountInput) == "" {
		defaultAmount := m.svc.DefaultTradeAmount()
		if defaultAmount <= 0 {
			defaultAmount = 5
		}
		m.trade.amountInput = formatTradeAmount(defaultAmount)
		m.trade.customAmount = false
	}

	shouldRefreshProbability := force || strings.TrimSpace(m.trade.probabilityInput) == "" || (!m.trade.customProbability && !m.trade.editing)
	if shouldRefreshProbability {
		suggested := m.suggestedProbability()
		if suggested <= 0 {
			suggested = 0.50
		}
		m.trade.probabilityInput = formatTradeProbability(clampTradeProbability(suggested))
		if force {
			m.trade.customProbability = false
		}
	}
}

// resetTradeForm 把交易表单恢复到默认状态，方便快速回到市场参考值。
func (m *model) resetTradeForm() {
	m.trade = tradeForm{
		action:  "BUY",
		outcome: "UP",
		focus:   tradeFieldAmount,
	}
	m.syncTradeForm(true)
}

// suggestedProbability 根据当前 dashboard 快照返回最适合表单预填的概率。
func (m model) suggestedProbability() float64 {
	switch {
	case strings.EqualFold(m.trade.action, "BUY") && strings.EqualFold(m.trade.outcome, "UP"):
		return firstPositiveValue(m.state.Prices.UpAsk, m.state.Prices.UpPrice)
	case strings.EqualFold(m.trade.action, "BUY") && strings.EqualFold(m.trade.outcome, "DOWN"):
		return firstPositiveValue(m.state.Prices.DownAsk, m.state.Prices.DownPrice)
	case strings.EqualFold(m.trade.action, "SELL") && strings.EqualFold(m.trade.outcome, "UP"):
		return firstPositiveValue(m.state.Prices.UpBid, m.state.Prices.UpPrice)
	case strings.EqualFold(m.trade.action, "SELL") && strings.EqualFold(m.trade.outcome, "DOWN"):
		return firstPositiveValue(m.state.Prices.DownBid, m.state.Prices.DownPrice)
	default:
		return 0
	}
}

// bumpTradeField 用固定步长调整当前聚焦的数值字段。
func (m *model) bumpTradeField(direction int) {
	if direction == 0 {
		return
	}

	switch m.trade.focus {
	case tradeFieldAmount:
		current, err := strconv.ParseFloat(strings.TrimSpace(m.trade.amountInput), 64)
		if err != nil {
			current = m.svc.DefaultTradeAmount()
		}
		current += float64(direction)
		if current < 1 {
			current = 1
		}
		m.trade.amountInput = formatTradeAmount(current)
		m.trade.customAmount = true
	case tradeFieldProbability:
		current, err := strconv.ParseFloat(strings.TrimSpace(m.trade.probabilityInput), 64)
		if err != nil {
			current = m.suggestedProbability()
		}
		current += 0.01 * float64(direction)
		m.trade.probabilityInput = formatTradeProbability(clampTradeProbability(current))
		m.trade.customProbability = true
	}
}

// startTradeEdit 进入精确编辑模式，并把当前字段值复制到编辑缓冲区。
func (m *model) startTradeEdit() {
	m.trade.editing = true
	switch m.trade.focus {
	case tradeFieldAmount:
		m.trade.editBuffer = m.trade.amountInput
		m.trade.editOriginal = m.trade.amountInput
	default:
		m.trade.editBuffer = m.trade.probabilityInput
		m.trade.editOriginal = m.trade.probabilityInput
	}
}

// commitTradeEdit 校验并提交当前编辑缓冲区。
func (m *model) commitTradeEdit() error {
	value := strings.TrimSpace(m.trade.editBuffer)
	if value == "" {
		return fmt.Errorf("输入不能为空")
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fmt.Errorf("数值格式不正确")
	}

	switch m.trade.focus {
	case tradeFieldAmount:
		if parsed <= 0 {
			return fmt.Errorf("金额必须大于 0")
		}
		m.trade.amountInput = formatTradeAmount(parsed)
		m.trade.customAmount = true
	case tradeFieldProbability:
		if parsed < 0.01 || parsed > 0.99 {
			return fmt.Errorf("概率必须在 0.01 到 0.99 之间")
		}
		m.trade.probabilityInput = formatTradeProbability(parsed)
		m.trade.customProbability = true
	}

	m.trade.editing = false
	m.trade.editBuffer = ""
	m.trade.editOriginal = ""
	m.setStatus("OK", "已更新当前交易表单")
	return nil
}

// buildTradeFormReq 从交易表单拼出一条可直接提交给 service 的手动下单请求。
func (m model) buildTradeFormReq() (dto.ManualOrderReq, string, error) {
	amount, err := strconv.ParseFloat(strings.TrimSpace(m.trade.amountInput), 64)
	if err != nil {
		return dto.ManualOrderReq{}, "", fmt.Errorf("金额格式不正确")
	}
	if amount <= 0 {
		return dto.ManualOrderReq{}, "", fmt.Errorf("金额必须大于 0")
	}
	probability, err := strconv.ParseFloat(strings.TrimSpace(m.trade.probabilityInput), 64)
	if err != nil {
		return dto.ManualOrderReq{}, "", fmt.Errorf("概率格式不正确")
	}
	if probability < 0.01 || probability > 0.99 {
		return dto.ManualOrderReq{}, "", fmt.Errorf("概率必须在 0.01 到 0.99 之间")
	}

	req := dto.ManualOrderReq{
		Action:      strings.ToUpper(strings.TrimSpace(m.trade.action)),
		Outcome:     strings.ToUpper(strings.TrimSpace(m.trade.outcome)),
		Amount:      amount,
		Probability: probability,
		Intent:      "tui_panel",
	}
	if req.Action == "SELL" {
		req.Amount = 0
	}

	label := fmt.Sprintf("%s %s @ %.2f%%", req.Action, req.Outcome, req.Probability*100)
	return req, label, nil
}

// renderHeader 渲染顶部摘要栏，展示核心运行状态与价格概览。
func (m model) renderHeader() string {
	state := m.state
	title := titleStyle.Render("Polymarket TUI")

	lines := []string{
		fmt.Sprintf("市场: %s", emptyFallback(state.Market.Slug, "-")),
		fmt.Sprintf("状态: %s", emptyFallback(state.Market.Status, "waiting")),
		fmt.Sprintf("剩余: %s", emptyFallback(state.Market.RemainingText, fmt.Sprintf("%ds", state.Market.Remaining))),
		fmt.Sprintf("UP: %s", m.renderMetricValue("up", state.Prices.UpPrice)),
		fmt.Sprintf("DOWN: %s", m.renderMetricValue("down", state.Prices.DownPrice)),
		fmt.Sprintf("Chainlink: %s", m.renderMetricValue("chainlink", state.Prices.ChainlinkBTC)),
		fmt.Sprintf("Binance: %s", m.renderMetricValue("binance", state.Prices.BinanceBTC)),
		fmt.Sprintf("PTB: %s", m.renderMetricValue("ptb", state.Prices.PTB)),
		fmt.Sprintf("Diff: %s", m.renderMetricValue("diff", state.Prices.Diff)),
		fmt.Sprintf("更新时间: %s", emptyFallback(state.UpdatedAt, m.now.Format(time.RFC3339))),
	}

	return lipgloss.JoinVertical(lipgloss.Left, title, mutedStyle.Render(strings.Join(lines, "    ")))
}

// renderTabs 渲染顶部 tab 栏，帮助单终端查看不同维度的数据。
func (m model) renderTabs() string {
	items := make([]string, 0, len(tabTitles()))
	for idx, title := range tabTitles() {
		style := tabStyle
		if idx == m.activeTab {
			style = activeTabStyle
		}
		items = append(items, style.Render(title))
	}
	return lipgloss.JoinHorizontal(lipgloss.Left, items...)
}

// renderActiveTab 根据当前选中的 tab 输出对应内容。
func (m model) renderActiveTab() string {
	switch m.activeTab {
	case 1:
		return m.renderWalletTab()
	case 2:
		return m.renderHistoryTab()
	case 3:
		return m.renderLogsTab()
	case tradeTabIndex:
		return m.renderTradeTab()
	default:
		return m.renderOverviewTab()
	}
}

// renderOverviewTab 展示实盘观察最常用的总览信息。
func (m model) renderOverviewTab() string {
	topLeft := m.renderPanel("持仓与订单", []string{
		fmt.Sprintf("当前持仓: %s", renderPosition(m.state.Position)),
		fmt.Sprintf("仓位校验: %s", m.renderPositionBalanceCheck()),
		fmt.Sprintf("挂单状态: %s", renderPending(m.state.PendingOrder)),
		fmt.Sprintf("最近下单: %s", renderLastOrder(m.state.LastOrder)),
		fmt.Sprintf("自动兑奖: %s", renderAutoRedeem(m.state.AutoRedeem)),
	})

	topRight := m.renderPanel("轮次结果", renderRoundResults(m.state.RoundResults, 6))
	bottomLeft := m.renderPanel("价格趋势", m.renderPriceTrendLines())
	bottomRight := m.renderPanel("最新日志", renderLogs(m.state.Activity, 8))

	return lipgloss.JoinVertical(
		lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Top, topLeft, topRight),
		lipgloss.JoinHorizontal(lipgloss.Top, bottomLeft, bottomRight),
	)
}

// renderWalletTab 展示余额、PnL 与钱包持仓摘要。
func (m model) renderWalletTab() string {
	summary := []string{
		fmt.Sprintf("钱包余额: %s", formatFloatPtr(m.state.WalletBalance)),
		fmt.Sprintf("实时持仓数: %d", m.state.LivePositionsCount),
		fmt.Sprintf("仓位校验: %s", m.renderPositionBalanceCheck()),
		fmt.Sprintf("已实现 PnL: %.4f", m.state.LiveRealizedPnL),
		fmt.Sprintf("未实现 PnL: %.4f", m.state.LiveUnrealizedPnL),
		fmt.Sprintf("总 PnL: %.4f", m.state.LiveTotalPnL),
	}

	positions := make([]string, 0, 8)
	for _, item := range lastWalletPositions(m.state.WalletPositions, 8) {
		positions = append(positions,
			fmt.Sprintf("%s | %s | size=%.4f | avg=%s | cur=%s",
				emptyFallback(item.Slug, "-"),
				emptyFallback(item.Outcome, "-"),
				item.Size,
				formatFloatPtr(item.AvgPrice),
				formatFloatPtr(item.CurPrice),
			),
		)
	}
	if len(positions) == 0 {
		positions = []string{"暂无钱包持仓"}
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.renderPanel("钱包摘要", summary),
		m.renderPanel("钱包持仓", positions),
	)
}

// renderHistoryTab 展示成交历史与实时聚合交易。
func (m model) renderHistoryTab() string {
	history := make([]string, 0, 8)
	for _, item := range lastTradeHistory(m.state.TradeHistory, 8) {
		history = append(history, renderTradeHistoryLine(item))
	}
	if len(history) == 0 {
		history = []string{"暂无本地成交历史"}
	}

	liveTrades := make([]string, 0, 8)
	for _, item := range lastLiveTrades(m.state.LiveTrades, 8) {
		liveTrades = append(liveTrades,
			fmt.Sprintf("%s | buy=%d sell=%d redeem=%d | pnl=%.4f | %s",
				emptyFallback(item.Slug, "-"),
				item.BuyCount,
				item.SellCount,
				item.RedeemCount,
				item.Profit,
				emptyFallback(item.Status, "-"),
			),
		)
	}
	if len(liveTrades) == 0 {
		liveTrades = []string{"暂无实时交易摘要"}
	}

	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.renderPanel("本地历史", history),
		m.renderPanel("实时聚合", liveTrades),
	)
}

// renderLogsTab 专门给远程值守时查看活动日志使用。
func (m model) renderLogsTab() string {
	return m.renderPanel("活动日志", renderLogs(m.state.Activity, 24))
}

// renderTradeTab 渲染可调金额/概率的终端交易面板。
func (m model) renderTradeTab() string {
	suggested := "-"
	if price := m.suggestedProbability(); price > 0 {
		suggested = formatTradeProbability(clampTradeProbability(price))
	}
	amountValue := m.trade.amountInput
	probabilityValue := m.trade.probabilityInput
	if m.trade.editing && m.trade.focus == tradeFieldAmount {
		amountValue = m.trade.editBuffer
	}
	if m.trade.editing && m.trade.focus == tradeFieldProbability {
		probabilityValue = m.trade.editBuffer
	}

	formLines := []string{
		renderTradeModeLine("动作", m.trade.action, "BUY", "SELL", "b / s"),
		renderTradeModeLine("方向", m.trade.outcome, "UP", "DOWN", "u / d / o"),
		renderTradeInputLine("金额", amountValue, m.trade.focus == tradeFieldAmount, m.trade.editing && m.trade.focus == tradeFieldAmount, "j/k 聚焦  +/- 调整  e 精确编辑"),
		renderTradeInputLine("概率", probabilityValue, m.trade.focus == tradeFieldProbability, m.trade.editing && m.trade.focus == tradeFieldProbability, "j/k 聚焦  +/- 调整  e 精确编辑"),
		fmt.Sprintf("持仓: %s", renderPosition(m.state.Position)),
		fmt.Sprintf("仓位校验: %s", m.renderPositionBalanceCheck()),
		fmt.Sprintf("建议价: %s", suggested),
	}

	referenceLines := []string{
		fmt.Sprintf("UP 买入参考(ask): %s", formatFloatPtr(m.state.Prices.UpAsk)),
		fmt.Sprintf("UP 卖出参考(bid): %s", formatFloatPtr(m.state.Prices.UpBid)),
		fmt.Sprintf("DOWN 买入参考(ask): %s", formatFloatPtr(m.state.Prices.DownAsk)),
		fmt.Sprintf("DOWN 卖出参考(bid): %s", formatFloatPtr(m.state.Prices.DownBid)),
		fmt.Sprintf("UP 展示价: %s", formatFloatPtr(m.state.Prices.UpPrice)),
		fmt.Sprintf("DOWN 展示价: %s", formatFloatPtr(m.state.Prices.DownPrice)),
	}

	helpLines := []string{
		"b 切到 BUY",
		"s 切到 SELL",
		"u / d 直接选方向，o 切换方向",
		"j / k 聚焦金额或概率",
		"+ / - 按步长调整",
		"e 进入精确编辑",
		"c 撤销当前挂单",
		"Enter 提交当前表单",
		"x 重置表单",
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		lipgloss.JoinHorizontal(
			lipgloss.Top,
			m.renderPanel("交易表单", formLines),
			m.renderPanel("盘口参考", referenceLines),
		),
		m.renderPanel("操作说明", helpLines),
	)
}

// renderFooter 渲染底部状态栏和快捷键说明。
func (m model) renderFooter() string {
	lines := []string{
		mutedStyle.Render(m.footerHelpText()),
	}

	if m.confirm.action != "" && time.Now().Before(m.confirm.expiresAt) {
		lines = append([]string{
			warnStyle.Render(fmt.Sprintf("等待确认: 5 秒内再次按 %s 以%s", strings.ToUpper(m.confirm.key), m.confirm.label)),
		}, lines...)
	}
	if strings.TrimSpace(m.statusMessage) != "" && time.Now().Before(m.statusUntil) {
		lines = append([]string{renderStatusLine(m.statusLevel, m.statusMessage)}, lines...)
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// renderPanel 渲染一个统一风格的内容面板。
func (m model) renderPanel(title string, lines []string) string {
	body := "暂无数据"
	if len(lines) > 0 {
		body = strings.Join(lines, "\n")
	}

	// 依据终端宽度给面板一个较稳定的显示宽度，减少频繁换行抖动。
	width := maxInt(40, (m.width-8)/2)
	if m.activeTab == 1 || m.activeTab == 3 {
		width = maxInt(60, m.width-8)
	}

	return panelStyle.
		Width(width).
		Render(titleStyle.Render(title) + "\n" + body)
}

// renderPriceTrendLines 输出总览页的价格趋势行。
func (m model) renderPriceTrendLines() []string {
	return []string{
		m.renderTrendLine("UP", "up", m.state.Prices.UpPrice),
		m.renderTrendLine("DOWN", "down", m.state.Prices.DownPrice),
		m.renderTrendLine("Chainlink", "chainlink", m.state.Prices.ChainlinkBTC),
		m.renderTrendLine("Binance", "binance", m.state.Prices.BinanceBTC),
		m.renderTrendLine("PTB", "ptb", m.state.Prices.PTB),
		m.renderTrendLine("Diff", "diff", m.state.Prices.Diff),
	}
}

// renderTrendLine 为单个指标生成静态数值行，不再渲染横向趋势条。
func (m model) renderTrendLine(label, flashKey string, current *float64) string {
	value := m.renderMetricValue(flashKey, current)
	return fmt.Sprintf("%-10s %s", label, value)
}

// renderMetricValue 根据最近涨跌方向给指标值加上颜色和方向提示。
func (m model) renderMetricValue(key string, value *float64) string {
	base := formatFloatPtr(value)
	marker, ok := m.flashes[key]
	if !ok || time.Since(marker.changedAt) > flashTTL {
		return base
	}

	switch marker.direction {
	case 1:
		return okStyle.Render(base + " +")
	case -1:
		return errStyle.Render(base + " -")
	default:
		return base
	}
}

// renderPositionBalanceCheck 输出本地持仓与钱包同向持仓的对账结果。
func (m model) renderPositionBalanceCheck() string {
	position := m.state.Position
	if position == nil {
		return mutedStyle.Render("暂无本地持仓")
	}

	// 优先按当前本地持仓的市场和方向汇总钱包侧真实可见仓位。
	walletSize, matched := walletPositionSizeForPosition(position, m.state.WalletPositions)
	diff := walletSize - position.Size
	line := fmt.Sprintf("本地=%.4f | 钱包=%.4f | 差值=%+.4f", position.Size, walletSize, diff)

	switch {
	case !matched:
		return warnStyle.Render(line + " | 未找到钱包侧同向持仓")
	case diff < -positionSizeDiffTolerance:
		return errStyle.Render(line)
	case math.Abs(diff) <= positionSizeDiffTolerance:
		return okStyle.Render(line)
	default:
		return warnStyle.Render(line)
	}
}

// walletPositionSizeForPosition 汇总钱包侧与本地持仓同市场同方向的份额。
func walletPositionSizeForPosition(position *entity.Position, walletPositions []entity.WalletPosition) (float64, bool) {
	if position == nil {
		return 0, false
	}

	targetSlug := strings.TrimSpace(position.Slug)
	targetSide := strings.ToUpper(strings.TrimSpace(position.Side))
	var total float64
	var matched bool

	for _, item := range walletPositions {
		// 钱包持仓有时只填 outcome，有时 side 也会回传，所以两者都做兼容匹配。
		itemSlug := strings.TrimSpace(item.Slug)
		itemSide := strings.ToUpper(strings.TrimSpace(item.Side))
		itemOutcome := strings.ToUpper(strings.TrimSpace(item.Outcome))

		if targetSlug != "" && !strings.EqualFold(itemSlug, targetSlug) {
			continue
		}
		if targetSide == "" {
			continue
		}
		if itemSide != targetSide && itemOutcome != targetSide {
			continue
		}

		total += item.Size
		matched = true
	}

	return total, matched
}

// renderRoundResults 格式化最近几条轮次结果，避免总览页过长。
func renderRoundResults(items []entity.RoundResult, limit int) []string {
	recent := lastRoundResults(items, limit)
	lines := make([]string, 0, len(recent))
	for _, item := range recent {
		lines = append(lines,
			fmt.Sprintf("%s | %s | %s | %s",
				emptyFallback(item.Slug, "-"),
				emptyFallback(item.EntrySide, "-"),
				emptyFallback(item.Status, "-"),
				emptyFallback(item.FinalOutcome, emptyFallback(item.SkipReason, "-")),
			),
		)
	}
	if len(lines) == 0 {
		return []string{"暂无轮次结果"}
	}
	return lines
}

// renderLogs 格式化日志列表，并对不同级别加简单颜色标记。
func renderLogs(items []entity.ActivityLog, limit int) []string {
	recent := lastActivityLogs(items, limit)
	lines := make([]string, 0, len(recent))
	for _, item := range recent {
		line := fmt.Sprintf("%s [%s] %s", emptyFallback(item.Time, "-"), emptyFallback(item.Level, "INFO"), emptyFallback(item.Message, "-"))
		switch strings.ToUpper(item.Level) {
		case "ERR":
			lines = append(lines, errStyle.Render(line))
		case "WARN":
			lines = append(lines, warnStyle.Render(line))
		default:
			lines = append(lines, okStyle.Render(line))
		}
	}
	if len(lines) == 0 {
		return []string{"暂无活动日志"}
	}
	return lines
}

// renderStatusLine 按级别渲染底部状态栏。
func renderStatusLine(level, message string) string {
	switch strings.ToUpper(strings.TrimSpace(level)) {
	case "ERR":
		return errStyle.Render(message)
	case "WARN":
		return warnStyle.Render(message)
	default:
		return okStyle.Render(message)
	}
}

// renderPosition 把当前持仓压缩成一行，方便总览快速判断。
func renderPosition(position *entity.Position) string {
	if position == nil {
		return "暂无持仓"
	}
	return fmt.Sprintf("%s | %s | entry=%.4f | size=%.4f | amount=%.4f",
		emptyFallback(position.Slug, "-"),
		emptyFallback(position.Side, "-"),
		position.EntryPrice,
		position.Size,
		position.Amount,
	)
}

// renderPending 输出当前挂单摘要。
func renderPending(order *entity.PendingOrder) string {
	if order == nil {
		return "暂无挂单"
	}
	return fmt.Sprintf("%s %s | price=%.4f | size=%.4f | %s",
		emptyFallback(order.Action, "-"),
		emptyFallback(order.Side, "-"),
		order.Price,
		order.Size,
		emptyFallback(order.Reason, "-"),
	)
}

// renderLastOrder 输出最近一次下单尝试状态。
func renderLastOrder(order *entity.LastOrder) string {
	if order == nil {
		return "暂无最近下单"
	}
	line := fmt.Sprintf("%s | retry=%d | last=%.4f | %s",
		emptyFallback(order.Key, "-"),
		order.RetryCount,
		order.LastPrice,
		emptyFallback(order.Time, "-"),
	)
	if strings.TrimSpace(order.Error) != "" {
		line += " | err=" + compactDetail(order.Error, 44)
	}
	return line
}

// renderAutoRedeem 输出自动兑奖计划摘要。
func renderAutoRedeem(status entity.AutoRedeemStatus) string {
	if !status.Enabled {
		if strings.TrimSpace(status.LastError) != "" {
			return "已关闭 | " + compactDetail(status.LastError, 52)
		}
		return "已关闭"
	}
	line := fmt.Sprintf("待兑=%d | 可兑=%d | 下次=%s",
		status.PendingCount,
		status.ClaimableCount,
		emptyFallback(status.NextRunAt, "-"),
	)
	if strings.TrimSpace(status.LastError) != "" {
		line += " | err=" + compactDetail(status.LastError, 36)
	}
	return line
}

// formatFloatPtr 统一格式化可空浮点数。
func formatFloatPtr(v *float64) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf("%.4f", *v)
}

// formatTradeAmount 统一格式化交易表单里的金额输入。
func formatTradeAmount(v float64) string {
	return fmt.Sprintf("%.2f", v)
}

// formatTradeProbability 统一格式化交易表单里的概率输入。
func formatTradeProbability(v float64) string {
	return fmt.Sprintf("%.4f", v)
}

// renderTradeHistoryLine 输出包含失败原因的历史摘要，方便值守时快速定位问题。
func renderTradeHistoryLine(item entity.TradeHistoryItem) string {
	line := fmt.Sprintf("%s | %s %s | price=%.4f | amount=%.4f | %s",
		emptyFallback(item.Time, "-"),
		emptyFallback(item.Action, "-"),
		emptyFallback(item.Side, "-"),
		item.Price,
		item.Amount,
		emptyFallback(item.Status, "-"),
	)
	if strings.TrimSpace(item.Error) != "" {
		line += " | err=" + compactDetail(item.Error, 42)
	}
	return line
}

// compactDetail 压缩过长的错误消息，避免 TUI 单行内容过度拉宽。
func compactDetail(text string, limit int) string {
	normalized := strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if normalized == "" || limit <= 0 {
		return "-"
	}
	if len(normalized) <= limit {
		return normalized
	}
	if limit <= 3 {
		return normalized[:limit]
	}
	return normalized[:limit-3] + "..."
}

// clampTradeProbability 把表单里的概率约束到安全区间。
func clampTradeProbability(v float64) float64 {
	switch {
	case v < 0.01:
		return 0.01
	case v > 0.99:
		return 0.99
	default:
		return v
	}
}

// firstPositiveValue 返回第一个可用的正价格。
func firstPositiveValue(values ...*float64) float64 {
	for _, value := range values {
		if value != nil && *value > 0 {
			return *value
		}
	}
	return 0
}

// appendValue 向历史序列追加一个新值，并限制最大长度。
func appendValue(items []float64, value *float64, limit int) []float64 {
	if value == nil {
		return items
	}
	items = append(items, *value)
	if len(items) > limit {
		items = append([]float64(nil), items[len(items)-limit:]...)
	}
	return items
}

// renderSparkline 把最近一段价格序列压缩成单行迷你趋势图。
func renderSparkline(values []float64, width int) string {
	if len(values) == 0 {
		return "-"
	}
	if width > 0 && len(values) > width {
		values = values[len(values)-width:]
	}

	minValue := values[0]
	maxValue := values[0]
	for _, value := range values[1:] {
		if value < minValue {
			minValue = value
		}
		if value > maxValue {
			maxValue = value
		}
	}

	blocks := []rune("▁▂▃▄▅▆▇█")
	if maxValue-minValue < 1e-9 {
		return strings.Repeat(string(blocks[len(blocks)/2]), len(values))
	}

	var builder strings.Builder
	for _, value := range values {
		index := int((value - minValue) / (maxValue - minValue) * float64(len(blocks)-1))
		if index < 0 {
			index = 0
		}
		if index >= len(blocks) {
			index = len(blocks) - 1
		}
		builder.WriteRune(blocks[index])
	}
	return builder.String()
}

// emptyFallback 为字符串字段提供空值兜底。
func emptyFallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// footerHelpText 根据当前 tab 输出对应的快捷键说明。
func (m model) footerHelpText() string {
	if m.activeTab == tradeTabIndex {
		return "快捷键: 1-5切页  tab/h/l切页  b买入  s卖出  o切方向  j/k聚焦  +/-调整  e编辑  enter提交  x重置  esc取消  q退出"
	}
	return "快捷键: 1-5切页  tab/h/l切页  r刷新  u买UP  d买DOWN  s卖当前持仓  c撤单  esc取消确认  q退出"
}

// renderTradeModeLine 渲染动作和方向这类二选一模式的当前状态。
func renderTradeModeLine(label, active, left, right, hint string) string {
	leftText := left
	rightText := right
	if strings.EqualFold(active, left) {
		leftText = okStyle.Render(left)
	} else {
		leftText = mutedStyle.Render(left)
	}
	if strings.EqualFold(active, right) {
		rightText = okStyle.Render(right)
	} else {
		rightText = mutedStyle.Render(right)
	}
	return fmt.Sprintf("%s: %s / %s  (%s)", label, leftText, rightText, hint)
}

// renderTradeInputLine 渲染金额或概率输入项，并标出当前聚焦与编辑状态。
func renderTradeInputLine(label, value string, focused, editing bool, hint string) string {
	prefix := "  "
	if focused {
		prefix = "> "
	}
	suffix := ""
	if editing {
		suffix = " [编辑中]"
	}
	return fmt.Sprintf("%s%s: %s%s  (%s)", prefix, label, value, suffix, hint)
}

// tabTitles 返回 TUI 当前支持的所有页面标签。
func tabTitles() []string {
	return []string{"概览", "钱包", "历史", "日志", "交易"}
}

// nextTradeField 切到下一个可编辑字段。
func nextTradeField(current tradeField) tradeField {
	switch current {
	case tradeFieldAmount:
		return tradeFieldProbability
	default:
		return tradeFieldAmount
	}
}

// prevTradeField 切到上一个可编辑字段。
func prevTradeField(current tradeField) tradeField {
	switch current {
	case tradeFieldProbability:
		return tradeFieldAmount
	default:
		return tradeFieldProbability
	}
}

// isTradeInputRune 判断当前按键是否可写入数值输入框。
func isTradeInputRune(key tea.KeyMsg) bool {
	value := key.String()
	if len(value) != 1 {
		return false
	}
	ch := value[0]
	return (ch >= '0' && ch <= '9') || ch == '.'
}

// lastTradeHistory 截取最近几条本地交易历史。
func lastTradeHistory(items []entity.TradeHistoryItem, limit int) []entity.TradeHistoryItem {
	if len(items) <= limit {
		return append([]entity.TradeHistoryItem(nil), items...)
	}
	return append([]entity.TradeHistoryItem(nil), items[len(items)-limit:]...)
}

// lastWalletPositions 截取最近几条钱包持仓。
func lastWalletPositions(items []entity.WalletPosition, limit int) []entity.WalletPosition {
	if len(items) <= limit {
		return append([]entity.WalletPosition(nil), items...)
	}
	return append([]entity.WalletPosition(nil), items[:limit]...)
}

// lastLiveTrades 截取最近几条实时聚合交易。
func lastLiveTrades(items []entity.LiveTradeSummary, limit int) []entity.LiveTradeSummary {
	if len(items) <= limit {
		return append([]entity.LiveTradeSummary(nil), items...)
	}
	return append([]entity.LiveTradeSummary(nil), items[:limit]...)
}

// lastActivityLogs 截取最后几条活动日志。
func lastActivityLogs(items []entity.ActivityLog, limit int) []entity.ActivityLog {
	if len(items) <= limit {
		return append([]entity.ActivityLog(nil), items...)
	}
	return append([]entity.ActivityLog(nil), items[len(items)-limit:]...)
}

// lastRoundResults 截取最近几条轮次结果。
func lastRoundResults(items []entity.RoundResult, limit int) []entity.RoundResult {
	if len(items) <= limit {
		return append([]entity.RoundResult(nil), items...)
	}
	return append([]entity.RoundResult(nil), items[len(items)-limit:]...)
}

// maxInt 返回两个整数中的较大值。
func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
