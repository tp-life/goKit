package tui

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"goKit/internal/application/service"
	"goKit/internal/domain/entity"

	"github.com/charmbracelet/lipgloss"
)

type styles struct {
	doc        lipgloss.Style
	header     lipgloss.Style
	panel      lipgloss.Style
	panelTitle lipgloss.Style
	subtle     lipgloss.Style
	accent     lipgloss.Style
	good       lipgloss.Style
	warn       lipgloss.Style
	bad        lipgloss.Style
	activeRow  lipgloss.Style
	tabActive  lipgloss.Style
	tabIdle    lipgloss.Style
	footer     lipgloss.Style
	modal      lipgloss.Style
	code       lipgloss.Style
	cursor     lipgloss.Style
}

var ui = styles{
	doc:        lipgloss.NewStyle().Padding(0, 1),
	header:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")),
	panel:      lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240")).Padding(0, 1),
	panelTitle: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("222")),
	subtle:     lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
	accent:     lipgloss.NewStyle().Foreground(lipgloss.Color("111")).Bold(true),
	good:       lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true),
	warn:       lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true),
	bad:        lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true),
	activeRow:  lipgloss.NewStyle().Background(lipgloss.Color("25")).Foreground(lipgloss.Color("230")).Bold(true),
	tabActive:  lipgloss.NewStyle().Foreground(lipgloss.Color("17")).Background(lipgloss.Color("111")).Padding(0, 1).Bold(true),
	tabIdle:    lipgloss.NewStyle().Foreground(lipgloss.Color("249")).Background(lipgloss.Color("238")).Padding(0, 1),
	footer:     lipgloss.NewStyle().Foreground(lipgloss.Color("246")),
	modal:      lipgloss.NewStyle().Border(lipgloss.ThickBorder()).BorderForeground(lipgloss.Color("111")).Padding(1, 2),
	code:       lipgloss.NewStyle().Foreground(lipgloss.Color("219")),
	cursor:     lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Bold(true),
}

func renderPanel(width int, height int, content string) string {
	return ui.panel.Width(panelContentWidth(width)).Height(panelContentHeight(height)).Render(content)
}

func renderModal(width int, height int, content string) string {
	return ui.modal.Width(modalContentWidth(width)).Height(modalContentHeight(height)).Render(content)
}

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "终端初始化中..."
	}

	bodyHeight := m.bodyHeight()
	header := m.renderHeader(m.width)
	var body string
	switch {
	case m.confirm != nil:
		body = m.renderConfirm(bodyHeight)
	case m.showHelp:
		body = m.renderHelp(bodyHeight)
	default:
		body = m.renderBody(bodyHeight)
	}
	footer := m.renderFooter(m.width)
	return ui.doc.Width(m.width).Height(m.height).Render(lipgloss.JoinVertical(lipgloss.Left, header, body, footer))
}

func (m Model) renderHeader(width int) string {
	title := fmt.Sprintf("资金费套利 TUI  [%s]", m.view.String())
	strategy := m.data.System.Strategy
	exec := m.data.System.Execution
	statusLine := strings.Join([]string{
		m.chip("模式 "+m.interactionModeLabel(), m.interactionModeTone()),
		m.chip("刷新 "+m.refreshInterval.String(), "accent"),
		m.chip("排序 "+m.sort.String(), "accent"),
		m.chip("交易对 "+m.currentPairLabel(), "accent"),
		m.chip("搜索 "+orDefault(strings.TrimSpace(m.search.Value()), "--"), "accent"),
		m.chip("实盘 "+boolWord(m.data.System.Execution.LiveTradingEnabled), boolTone(m.data.System.Execution.LiveTradingEnabled, true)),
		m.chip("自动开仓 "+boolWord(m.data.System.Execution.AutoEntry), boolTone(m.data.System.Execution.AutoEntry, false)),
		m.chip("自动平仓 "+boolWord(m.data.System.Execution.AutoClose), boolTone(m.data.System.Execution.AutoClose, false)),
	}, "  ")
	limitLine := strings.Join([]string{
		m.chip("最小收益 "+compactUSDT(strategy.MinNetPNL, 3), "good"),
		m.chip("最大价差 "+compactBps(strategy.MaxSpreadBps, 2), "warn"),
		m.chip("策略 "+strategyModeText(strategy.Mode), "accent"),
		m.chip("持有模式 "+holdSelectionModeText(strategy.HoldSelectionMode), "accent"),
		m.chip("提前开仓 "+orDefault(strategy.EntryLeadTime, "--"), "accent"),
		m.chip("平仓缓冲 "+orDefault(exec.CloseGracePeriod, "--"), "accent"),
	}, "  ")

	metrics := strings.Join([]string{
		m.metric("机会", len(m.data.Opportunities)),
		m.metric("就绪", countEligible(m.data.Opportunities)),
		m.metric("计划", len(m.data.AllPlans)),
		m.metric("执行", len(m.data.Executions)),
		m.metric("24h资金费", int(m.data.Stats.FundingCount24h)),
		m.metric("24h盘口", int(m.data.Stats.BookTopCount24h)),
		m.metric("批次", m.data.CurrentBatchID),
	}, "   ")

	stateParts := []string{
		m.chip("来源 "+m.client.BaseURL(), "accent"),
		m.chip("最近刷新 "+orDefault(formatClock(m.lastRefresh), "--"), "accent"),
	}
	if m.loading {
		// 刷新时统一显示“同步中”，避免把 system/opps/exec/batch 这类内部加载分区
		// 直接暴露给用户，造成“界面正在异常轮动”的错觉。
		stateParts = append(stateParts, m.chip("同步中", "warn"))
	}
	if strings.TrimSpace(m.lastError) != "" {
		stateParts = append(stateParts, m.chip("错误 "+clip(m.lastError, 48), "bad"))
	} else if strings.TrimSpace(m.flash) != "" {
		stateParts = append(stateParts, m.chip(m.flash, "good"))
	}

	lines := []string{
		ui.header.Width(width).Render(title),
		clip(statusLine, width),
		clip(limitLine, width),
		clip(metrics, width),
		clip(strings.Join(stateParts, "  "), width),
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderBody(height int) string {
	switch m.view {
	case viewExecution:
		return m.renderExecutionBody(height)
	case viewSystem:
		return m.renderSystemBody(height)
	default:
		return m.renderScannerBody(height)
	}
}

func (m Model) renderScannerBody(height int) string {
	leftWidth, rightWidth := splitWidth(m.width-2, 40)
	if m.width < 120 {
		listHeight, detailHeight := splitStackedHeights(height)
		if detailHeight <= 0 {
			return m.renderOpportunityList(leftWidth+rightWidth, listHeight)
		}
		return lipgloss.JoinVertical(lipgloss.Left,
			m.renderOpportunityList(leftWidth+rightWidth, listHeight),
			m.renderScannerDetail(leftWidth+rightWidth, detailHeight),
		)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top,
		m.renderOpportunityList(leftWidth, height),
		m.renderScannerDetail(rightWidth, height),
	)
}

func (m Model) renderExecutionBody(height int) string {
	leftWidth, rightWidth := splitWidth(m.width-2, 42)
	if m.width < 120 {
		listHeight, detailHeight := splitStackedHeights(height)
		if detailHeight <= 0 {
			return m.renderExecutionList(leftWidth+rightWidth, listHeight)
		}
		return lipgloss.JoinVertical(lipgloss.Left,
			m.renderExecutionList(leftWidth+rightWidth, listHeight),
			m.renderExecutionDetail(leftWidth+rightWidth, detailHeight),
		)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top,
		m.renderExecutionList(leftWidth, height),
		m.renderExecutionDetail(rightWidth, height),
	)
}

func (m Model) renderSystemBody(height int) string {
	leftWidth, rightWidth := splitWidth(m.width-2, 45)
	if m.width < 120 {
		topHeight, restHeight := splitStackedHeights(height)
		if restHeight <= 0 {
			return m.renderConnectorPanel(leftWidth+rightWidth, topHeight)
		}
		midHeight, lowerHeight := splitStackedHeights(restHeight)
		sections := []string{
			m.renderConnectorPanel(leftWidth+rightWidth, topHeight),
			m.renderConfigPanel(leftWidth+rightWidth, midHeight),
		}
		if lowerHeight > 0 {
			autoCloseHeight, livePositionHeight := splitStackedHeights(lowerHeight)
			sections = append(sections, m.renderAutoClosePanel(leftWidth+rightWidth, autoCloseHeight))
			if livePositionHeight > 0 {
				sections = append(sections, m.renderLivePositionPanel(leftWidth+rightWidth, livePositionHeight))
			}
		}
		return lipgloss.JoinVertical(lipgloss.Left, sections...)
	}
	topHeight, lowerHeight := splitStackedHeights(height)
	right := m.renderConfigPanel(rightWidth, height)
	if lowerHeight > 0 {
		autoCloseHeight, livePositionHeight := splitStackedHeights(lowerHeight)
		sections := []string{
			m.renderConfigPanel(rightWidth, topHeight),
			m.renderAutoClosePanel(rightWidth, autoCloseHeight),
		}
		if livePositionHeight > 0 {
			sections = append(sections, m.renderLivePositionPanel(rightWidth, livePositionHeight))
		}
		right = lipgloss.JoinVertical(lipgloss.Left, sections...)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top,
		m.renderConnectorPanel(leftWidth, height),
		right,
	)
}

func (m Model) renderOpportunityList(width int, height int) string {
	items := m.filteredOpportunities()
	lines := []string{
		ui.panelTitle.Render(fmt.Sprintf("Opportunities  %d visible / %d total", len(items), len(m.data.Opportunities))),
		ui.subtle.Render(fmt.Sprintf("sorted by %s  |  j/k move  / search  f pair  s sort  o open  c close", m.sort.String())),
		"",
	}
	if len(items) == 0 {
		if m.isLoading(loadOpportunities) && len(m.data.Opportunities) == 0 {
			lines = append(lines, ui.subtle.Render("Loading opportunities..."))
			return renderPanel(width, height, strings.Join(lines, "\n"))
		}
		lines = append(lines, ui.subtle.Render("No opportunities match the current filter."))
		return renderPanel(width, height, strings.Join(lines, "\n"))
	}

	visibleRows := maxInt(1, panelListContentHeight(height)/listRowHeight)
	selected := m.indexOfOpportunity(items, m.selectedOpportunityKey)
	if selected < 0 {
		selected = 0
	}
	start := clampOffset(m.opportunityOffset, len(items), visibleRows)
	end := minInt(len(items), start+visibleRows)
	lines = append(lines, ui.subtle.Render(fmt.Sprintf("showing %d-%d", start+1, end)), "")
	for i := start; i < end; i++ {
		item := items[i]
		active := i == selected
		planLabel, planTone := m.opportunityPlanLabel(item)
		marker := selectedMarker(active)
		planText := toneStyle(planTone).Render(clip(planLabel, 18))
		pnlText := alignRightValue(renderMoneyValue(item.NetExpectedPNL, 3), 12)
		carryText := alignRightValue(renderPctValue(displayedCarryRate(item, m.data.System.Strategy, entity.ExecutionPlan{}, false), 5), 10)
		row := []string{
			fmt.Sprintf("%s %-3d %-7s %-24s %s", marker, i+1, clip(item.Symbol, 7), clip(opportunityDirection(item), 24), pnlText),
			fmt.Sprintf("     %-18s basis %-9s carry %s", clip(opportunityPair(item), 18), fmtSignedBps(item.BasisBps, 2), carryText),
			fmt.Sprintf("     %-14s  |  %s  |  %s", clip(holdingDurationText(item), 14), planText, clip(statusText(item.Status), 16)),
		}
		block := strings.Join(row, "\n")
		if active {
			block = ui.activeRow.Render(block)
		}
		lines = append(lines, block)
	}
	return renderPanel(width, height, strings.Join(lines, "\n"))
}

func (m Model) renderScannerDetail(width int, height int) string {
	tabs := []string{
		m.renderTab("总览", m.tab == tabOverview),
		m.renderTab("双腿", m.tab == tabLegs),
		m.renderTab("计划", m.tab == tabPlan),
		m.renderTab("预测", m.tab == tabProjection),
		m.renderTab("订单", m.tab == tabOrders),
	}
	lines := []string{
		ui.panelTitle.Render("套利详情"),
		strings.Join(tabs, " "),
		"",
	}

	item, ok := m.selectedOpportunity()
	if !ok {
		lines = append(lines, ui.subtle.Render("请先选择一条套利机会，再查看详情。"))
		return renderPanel(width, height, strings.Join(lines, "\n"))
	}

	detailItem, hasDetail := m.selectedOpportunityDetail()
	loadingDetail := m.opportunityDetailLoading[item.ID]
	plan, hasPlan := m.bestPlanForOpportunity(item)
	rec, hasRec := m.executionByPlanKey(plan.PlanKey)

	var detail string
	switch m.tab {
	case tabLegs:
		detail = m.renderLegsDetail(item, detailItem, hasDetail, loadingDetail, width-4)
	case tabPlan:
		detail = m.renderPlanDetail(item, plan, hasPlan, rec, hasRec, width-4)
	case tabProjection:
		detail = m.renderProjectionDetail(item, detailItem, hasDetail, loadingDetail, width-4)
	case tabOrders:
		detail = m.renderOrdersDetail(plan.PlanKey, width-4)
	default:
		detail = m.renderOverviewDetail(item, detailItem, hasDetail, loadingDetail, plan, hasPlan, rec, hasRec, width-4)
	}
	lines = append(lines, detail)
	return renderPanel(width, height, strings.Join(lines, "\n"))
}

func (m Model) renderExecutionList(width int, height int) string {
	title := fmt.Sprintf("执行  [%s]", m.execTab.String())
	sub := "切换标签  p 计划  x 执行记录  o 开仓  c 平仓"
	lines := []string{ui.panelTitle.Render(title), ui.subtle.Render(sub), ""}

	if m.execTab == execRecords {
		if len(m.data.Executions) == 0 {
			if m.isLoading(loadExecutions) {
				lines = append(lines, ui.subtle.Render("正在加载执行记录..."))
				return renderPanel(width, height, strings.Join(lines, "\n"))
			}
			lines = append(lines, ui.subtle.Render("当前还没有执行记录。"))
			return renderPanel(width, height, strings.Join(lines, "\n"))
		}
		visibleRows := maxInt(1, panelListContentHeight(height)/listRowHeight)
		selected := m.indexOfExecution(m.selectedExecutionPlanKey)
		if selected < 0 {
			selected = 0
		}
		start := clampOffset(m.executionOffset, len(m.data.Executions), visibleRows)
		end := minInt(len(m.data.Executions), start+visibleRows)
		lines = append(lines, ui.subtle.Render(fmt.Sprintf("显示 %d-%d", start+1, end)), "")
		for i := start; i < end; i++ {
			item := m.data.Executions[i]
			marker := selectedMarker(i == selected)
			allocatedText := "--"
			if value := executionAllocatedNotional(item, entity.ExecutionPlan{}, false); value > 0 {
				allocatedText = fmtMoney(value, 2)
			}
			block := strings.Join([]string{
				fmt.Sprintf("%s %-7s %-25s %s", marker, clip(item.Symbol, 7), clip(opportunityDirection(entity.Opportunity{LongExchange: item.LongExchange, ShortExchange: item.ShortExchange}), 25), statusText(item.Status)),
				fmt.Sprintf("    计划 %-28s 实盘 %-3s 自动平仓 %-3s", clip(item.PlanKey, 28), boolWord(item.LiveTrading), boolWord(item.AutoClose)),
				fmt.Sprintf("    占用 %-14s 开仓 %s  平仓 %s", clip(allocatedText, 14), clip(fmtTime(item.OpenedAtMs), 19), clip(fmtTime(item.ClosedAtMs), 19)),
			}, "\n")
			if i == selected {
				block = ui.activeRow.Render(block)
			}
			lines = append(lines, block)
		}
		return renderPanel(width, height, strings.Join(lines, "\n"))
	}

	if len(m.data.AllPlans) == 0 {
		if m.isLoading(loadAllPlans) {
			lines = append(lines, ui.subtle.Render("正在加载执行计划..."))
			return renderPanel(width, height, strings.Join(lines, "\n"))
		}
		lines = append(lines, ui.subtle.Render("当前没有执行计划。"))
		return renderPanel(width, height, strings.Join(lines, "\n"))
	}
	visibleRows := maxInt(1, panelListContentHeight(height)/listRowHeight)
	selected := m.indexOfPlan(m.selectedPlanKey)
	if selected < 0 {
		selected = 0
	}
	start := clampOffset(m.planOffset, len(m.data.AllPlans), visibleRows)
	end := minInt(len(m.data.AllPlans), start+visibleRows)
	lines = append(lines, ui.subtle.Render(fmt.Sprintf("显示 %d-%d", start+1, end)), "")
	for i := start; i < end; i++ {
		item := m.data.AllPlans[i]
		marker := selectedMarker(i == selected)
		pnlText := alignRightValue(renderMoneyValue(item.NetExpectedPNL, 3), 10)
		block := strings.Join([]string{
			fmt.Sprintf("%s %-7s %-25s %s", marker, clip(item.Symbol, 7), clip(opportunityDirection(entity.Opportunity{LongExchange: item.LongExchange, ShortExchange: item.ShortExchange}), 25), pnlText),
			fmt.Sprintf("    %-18s 就绪 %-3s 状态 %-18s", clip(opportunityPair(entity.Opportunity{LongExchange: item.LongExchange, ShortExchange: item.ShortExchange}), 18), boolWord(item.ReadyNow), clip(statusText(item.Status), 18)),
			fmt.Sprintf("    计划 %-16s 名义 %-12s", clip(item.PlanKey, 16), clip(fmtMoney(targetNotional(item), 2), 12)),
		}, "\n")
		if i == selected {
			block = ui.activeRow.Render(block)
		}
		lines = append(lines, block)
	}
	return renderPanel(width, height, strings.Join(lines, "\n"))
}

func (m Model) renderExecutionDetail(width int, height int) string {
	lines := []string{
		ui.panelTitle.Render("执行详情"),
		ui.subtle.Render("展示当前选中项的计划状态、执行记录与订单历史。"),
		"",
	}
	var detail string
	if m.execTab == execRecords {
		rec, ok := m.selectedExecution()
		if !ok {
			detail = ui.subtle.Render("请先选择一条执行记录。")
		} else {
			plan, hasPlan := m.planByKey(rec.PlanKey)
			detail = m.renderExecutionRecordDetail(rec, plan, hasPlan, width-4)
		}
	} else {
		plan, ok := m.selectedPlan()
		if !ok {
			detail = ui.subtle.Render("请先选择一条执行计划。")
		} else {
			rec, hasRec := m.executionByPlanKey(plan.PlanKey)
			detail = m.renderPlanExecutionDetail(plan, rec, hasRec, width-4)
		}
	}
	lines = append(lines, detail)
	return renderPanel(width, height, strings.Join(lines, "\n"))
}

func (m Model) renderConnectorPanel(width int, height int) string {
	lines := []string{
		ui.panelTitle.Render(fmt.Sprintf("连接器状态  %d", len(m.data.System.Connectors))),
		ui.subtle.Render("健康连接器应当持续收到最新行情和盘口事件，并且没有持续错误。"),
		"",
	}
	if len(m.data.System.Connectors) == 0 {
		if m.isLoading(loadSystem) {
			lines = append(lines, ui.subtle.Render("正在加载系统状态..."))
			return renderPanel(width, height, strings.Join(lines, "\n"))
		}
		lines = append(lines, ui.subtle.Render("当前还没有连接器状态上报。"))
		return renderPanel(width, height, strings.Join(lines, "\n"))
	}
	for _, item := range m.data.System.Connectors {
		tone := "good"
		if !item.MarkPriceConnected && !item.BookTickerConnected {
			tone = "bad"
		} else if !item.MarkPriceConnected || !item.BookTickerConnected {
			tone = "warn"
		}
		lines = append(lines,
			toneStyle(tone).Render(fmt.Sprintf("%-12s 行情=%-3s 盘口=%-3s  行情时间=%-19s  盘口时间=%-19s",
				item.Exchange,
				boolWord(item.MarkPriceConnected),
				boolWord(item.BookTickerConnected),
				clip(item.LastMarketEventAt.Format("2006-01-02 15:04:05"), 19),
				clip(item.LastBookEventAt.Format("2006-01-02 15:04:05"), 19),
			)),
		)
		if strings.TrimSpace(item.LastError) != "" {
			lines = append(lines, ui.bad.Render("  错误: "+clip(item.LastError, width-8)))
		}
	}
	return renderPanel(width, height, strings.Join(lines, "\n"))
}

func (m Model) renderConfigPanel(width int, height int) string {
	strategy := m.data.System.Strategy
	exec := m.data.System.Execution
	lines := []string{
		ui.panelTitle.Render("策略与执行配置"),
		ui.subtle.Render("展示当前生效的策略参数和监控名单，用来确认系统处于观察模式还是实盘模式。"),
		"",
		fmt.Sprintf("策略: 启用=%s  模式=%s  持有上限=%sh  持有模式=%s  杠杆=%sx  最小净收益=%s  有效名义=%s",
			boolWord(strategy.Enabled),
			strategyModeText(strategy.Mode),
			fmtNumber(strategy.HoldHours, 1),
			holdSelectionModeText(strategy.HoldSelectionMode),
			fmtNumber(strategy.Leverage, 2),
			fmtMoney(strategy.MinNetPNL, 3),
			fmtMoney(strategy.EffectiveNotional, 2),
		),
		fmt.Sprintf("开平仓: %s / %s  价差限制=%s  数据时效=%s",
			orDefault(strategy.EntryMode, "--"),
			orDefault(strategy.ExitMode, "--"),
			fmtSignedBps(strategy.MaxSpreadBps, 2),
			orDefault(strategy.MaxDataAge, "--"),
		),
		fmt.Sprintf("执行: 实盘=%s  自动开仓=%s  自动平仓=%s  轮询=%s  结算后缓冲=%s  最近计划上限=%d",
			boolWord(exec.LiveTradingEnabled),
			boolWord(exec.AutoEntry),
			boolWord(exec.AutoClose),
			orDefault(exec.LoopInterval, "--"),
			orDefault(exec.CloseGracePeriod, "--"),
			exec.MaxLatestPlans,
		),
		fmt.Sprintf("自动预算: 分配=%s  已用=%s  剩余=%s  持仓计划=%d%s  每轮开仓=%s",
			boolWord(exec.AutoAllocateCapital),
			fmtMoney(exec.ActiveAllocatedNotionalUSDT, 2),
			fmtMoney(exec.RemainingAutoBudgetUSDT, 2),
			exec.ActiveLivePlans,
			renderLimitSuffix(exec.MaxLivePlans),
			renderLoopLimit(exec.MaxAutoOpenPerLoop),
		),
	}
	if isRollingStrategyMode(strategy.Mode) {
		lines = append(lines,
			fmt.Sprintf("Review: 缓冲=%s  快照等待=%s  同向续持=%s  增量净收益门槛=%s  不利即平=%s",
				orDefault(strategy.RollingReviewSettleGracePeriod, "--"),
				orDefault(strategy.RollingReviewFreshSnapshotMaxWait, "--"),
				boolWord(strategy.RollingReviewContinueOnSameDirection),
				fmtMoney(strategy.RollingReviewMinIncrementalNetPNL, 3),
				boolWord(strategy.RollingReviewCloseOnUnprofitable),
			),
			fmt.Sprintf("Flip: 启用=%s  要求净正=%s  最小净收益=%s  滑点倍数=%sx  额外缓冲=%s",
				boolWord(strategy.RollingFlipEnabled),
				boolWord(strategy.RollingFlipRequireNetPositive),
				fmtMoney(strategy.RollingFlipMinNetPNL, 3),
				fmtNumber(strategy.RollingFlipSlippageMultiplier, 2),
				fmtMoney(strategy.RollingFlipExtraSafetyBufferUSDT, 3),
			),
			fmt.Sprintf("Rolling 监控: 记录=%d  分组=%d  待 Review=%d  未到 Review=%d",
				exec.ActiveRollingRecords,
				exec.ActiveRollingGroups,
				exec.RollingDueReviews,
				exec.RollingWaitingReviews,
			),
		)
	}
	lines = append(lines,
		"",
		"监控名单: "+clip(strings.Join(m.data.System.Watchlist, ", "), width-10),
		"深扫名单: "+clip(strings.Join(m.data.System.DeepScanWatchlist, ", "), width-10),
	)
	if m.isLoading(loadSystem) && len(m.data.System.Watchlist) == 0 && len(m.data.System.DeepScanWatchlist) == 0 {
		lines = append(lines, "", ui.subtle.Render("正在加载策略和执行配置..."))
	}
	return renderPanel(width, height, strings.Join(lines, "\n"))
}

func (m Model) renderAutoClosePanel(width int, height int) string {
	items := m.data.AutoClose.Candidates
	lines := []string{
		ui.panelTitle.Render(fmt.Sprintf("自动平仓候选  %d", len(items))),
		ui.subtle.Render("展示当前 live execution 的平仓判断，以及手动 sweep 会实际处理哪些仓位。"),
		"",
		fmt.Sprintf("评估时间=%s  可评估=%d  应平仓=%d  错误=%d",
			fmtTime(m.data.AutoClose.EvaluatedAtMs),
			m.data.AutoClose.Eligible,
			m.data.AutoClose.ShouldClose,
			m.data.AutoClose.DecisionErrors,
		),
		"",
	}
	if len(items) == 0 {
		lines = append(lines, ui.subtle.Render("当前没有需要纳入自动平仓扫描的 live execution。"))
		return renderPanel(width, height, strings.Join(lines, "\n"))
	}

	visibleRows := maxInt(1, panelListContentHeight(height)/listRowHeight)
	_, end := visibleWindow(0, len(items), visibleRows)
	for _, item := range items[:end] {
		rec := item.Execution
		decision := item.Decision
		plan := entity.ExecutionPlan{}
		hasPlan := false
		if item.Plan != nil {
			plan = *item.Plan
			hasPlan = true
		}

		actionLabel := "继续持有"
		actionTone := "accent"
		switch {
		case decision.Error != "":
			actionLabel = "判断失败"
			actionTone = "bad"
		case decision.ShouldClose:
			actionLabel = autoCloseActionText(decision.Trigger)
			if strings.TrimSpace(actionLabel) == "" {
				actionLabel = "立即平仓"
			}
			actionTone = "warn"
		case !decision.Eligible:
			actionLabel = "已跳过"
			actionTone = "bad"
		case !decision.AutoCloseEnabled:
			actionLabel = "自动平仓关闭"
			actionTone = "bad"
		}

		reason := decision.Reason
		if decision.Error != "" {
			reason = decision.Error
		}
		if strings.TrimSpace(reason) == "" {
			reason = "--"
		} else {
			reason = autoCloseReasonText(reason)
		}

		allocated := executionAllocatedNotional(rec, plan, hasPlan)
		block := strings.Join([]string{
			fmt.Sprintf("%-7s %-24s %s", clip(rec.Symbol, 7), clip(rec.PlanKey, 24), statusText(rec.Status)),
			fmt.Sprintf("    到期 %-19s 占用 %-12s 自动平仓 %-3s %s",
				clip(fmtTime(rec.TargetCloseTimeMs), 19),
				clip(renderAllocatedNotionalValue(allocated), 12),
				boolWord(rec.AutoClose),
				toneStyle(actionTone).Render(actionLabel),
			),
			fmt.Sprintf("    %s", clip(reason, maxInt(10, width-8))),
		}, "\n")
		lines = append(lines, block)
	}
	return renderPanel(width, height, strings.Join(lines, "\n"))
}

func (m Model) renderLivePositionPanel(width int, height int) string {
	items := m.data.LivePositions.Candidates
	lines := []string{
		ui.panelTitle.Render(fmt.Sprintf("交易所真实持仓  %d", len(items))),
		ui.subtle.Render("展示每条 live execution 在交易所上的真实双腿持仓，手动平仓或单腿漂移会立即暴露出来。"),
		"",
		fmt.Sprintf("评估时间=%s  同步=%d  待成交=%d  单腿=%d  空仓=%d  方向异常=%d  错误=%d",
			fmtTime(m.data.LivePositions.EvaluatedAtMs),
			m.data.LivePositions.InSync,
			m.data.LivePositions.AwaitingFill,
			m.data.LivePositions.SingleLeg,
			m.data.LivePositions.Flat,
			m.data.LivePositions.SideMismatch,
			m.data.LivePositions.Errors,
		),
		"",
	}
	if len(items) == 0 {
		lines = append(lines, ui.subtle.Render("当前没有需要检查交易所真实持仓的 live execution。"))
		return renderPanel(width, height, strings.Join(lines, "\n"))
	}

	const rowHeight = 4
	visibleRows := maxInt(1, panelListContentHeight(height)/rowHeight)
	_, end := visibleWindow(0, len(items), visibleRows)
	for _, item := range items[:end] {
		statusTone := livePositionStatusTone(item.SyncStatus)
		block := strings.Join([]string{
			fmt.Sprintf("%-7s %-24s %s",
				clip(item.Execution.Symbol, 7),
				clip(item.Execution.PlanKey, 24),
				toneStyle(statusTone).Render(livePositionStatusLabel(item.SyncStatus)),
			),
			fmt.Sprintf("    多 %-8s %-12s %s",
				clip(item.LongLeg.Exchange, 8),
				clip(item.LongLeg.VenueSymbol, 12),
				clip(renderLivePositionLegSummary(item.LongLeg), maxInt(10, width-30)),
			),
			fmt.Sprintf("    空 %-8s %-12s %s",
				clip(item.ShortLeg.Exchange, 8),
				clip(item.ShortLeg.VenueSymbol, 12),
				clip(renderLivePositionLegSummary(item.ShortLeg), maxInt(10, width-30)),
			),
			fmt.Sprintf("    %s", clip(livePositionSummaryText(item.Summary), maxInt(10, width-8))),
		}, "\n")
		lines = append(lines, block)
	}
	return renderPanel(width, height, strings.Join(lines, "\n"))
}

func (m Model) renderOverviewDetail(item OpportunityListItem, detail *entity.Opportunity, hasDetail bool, loading bool, plan entity.ExecutionPlan, hasPlan bool, rec entity.ExecutionRecord, hasRec bool, width int) string {
	planLabel, _ := m.opportunityPlanLabel(item)
	carrySource := any(item)
	if hasDetail && detail != nil {
		carrySource = *detail
	}
	displayCarryRate := displayedCarryRate(carrySource, m.data.System.Strategy, plan, hasPlan)
	displayHourlyCarry := fundingSpreadHourly(carrySource)
	expectedCloseMs := opportunityExpectedCloseTimeMs(carrySource, m.data.System.Execution)
	mode := opportunityStrategyMode(carrySource)
	lines := []string{
		fmt.Sprintf("%s  %s", ui.accent.Render(item.Symbol), ui.subtle.Render(opportunityDirection(item))),
		strings.Join([]string{
			renderField("净收益", renderMoneyValue(item.NetExpectedPNL, 3)),
			renderField("净收益率", renderBpsValue(item.NetExpectedBps, 2)),
			renderField("当前 Carry率", renderPctValue(displayCarryRate, 5)),
			renderField("当前时均边际", renderPctValue(displayHourlyCarry, 5)),
			renderField("基差", renderBasisValue(item.BasisBps, item.MaxAllowedBasisBps)),
		}, "  "),
		strings.Join([]string{
			renderField("状态", renderStatusValue(item.Status)),
			renderField("可执行", renderBoolValue(item.EligibleForExecution, false)),
			renderField("计划", toneStyle(opportunityPlanTone(planLabel)).Render(planLabel)),
			renderField("结算前持有", toneStyle("accent").Render(holdingDurationText(item))),
		}, "  "),
		strings.Join([]string{
			renderField("多头结算倒计时", renderDurationValue(time.Until(time.UnixMilli(item.LongFundingTimeMs)))),
			renderField("空头结算倒计时", renderDurationValue(time.Until(time.UnixMilli(item.ShortFundingTimeMs)))),
			renderField("当前 Entry Path 终点", renderTimeValue(item.ProjectedFundingTimeMs, "accent")),
		}, "  "),
		strings.Join([]string{
			renderField("持有模式", toneStyle("accent").Render(holdSelectionModeText(m.data.System.Strategy.HoldSelectionMode))),
			renderField("持有上限", toneStyle("accent").Render(fmt.Sprintf("%sh", fmtNumber(m.data.System.Strategy.HoldHours, 1)))),
			renderField("预计平仓", renderTimeValue(expectedCloseMs, "accent")),
			renderField("预计总持有", toneStyle("accent").Render(opportunityExpectedHoldText(carrySource, m.data.System.Execution))),
			renderField("结算后缓冲", toneStyle("accent").Render(orDefault(m.data.System.Execution.CloseGracePeriod, "--"))),
		}, "  "),
		strings.Join([]string{
			renderField("产生时间", renderTimeValue(opportunityProducedTime(item).UnixMilli(), "subtle")),
			renderField("批次", toneStyle("accent").Render(orDefault(item.BatchID, "--"))),
			renderField("结算窗口", toneStyle("accent").Render(fmt.Sprintf("%sh", fmtNumber(item.FundingWindowHours, 2)))),
			renderField("策略模式", toneStyle("accent").Render(strategyModeText(mode))),
			renderField("资金费模式", toneStyle("accent").Render(orDefault(item.FundingComputationMode, "--"))),
		}, "  "),
	}
	if isRollingStrategyMode(mode) {
		lines = append(lines, strings.Join([]string{
			renderField("下次 Review", renderTimeValue(opportunityNextReviewTimeMs(carrySource), "accent")),
			renderField("当前共享结算边界", renderTimeValue(opportunitySyncBoundaryTimeMs(carrySource), "accent")),
			renderField("当前 Entry Path 段数", toneStyle("accent").Render(fmt.Sprintf("%d 段", opportunityEntryPathSegmentCount(carrySource)))),
			renderField("Entry Path 截止原因", toneStyle("accent").Render(entryPathStopReasonText(opportunityEntryPathStopReason(carrySource)))),
		}, "  "))
	}
	lines = append(lines,
		"",
		ui.panelTitle.Render("市场快照"),
		renderMarketSummary(m.data.Market, width),
		"",
		ui.panelTitle.Render("双腿信息"),
		m.renderLegsDetail(item, detail, hasDetail, loading, width),
		"",
		ui.panelTitle.Render("收益构成"),
		m.renderPnLBreakdownDetail(item, detail, hasDetail, plan, hasPlan, width),
		"",
		ui.panelTitle.Render("计划信息"),
		m.renderPlanDetail(item, plan, hasPlan, rec, hasRec, width),
	)
	return strings.Join(lines, "\n")
}

func (m Model) renderPnLBreakdownDetail(item OpportunityListItem, detail *entity.Opportunity, hasDetail bool, plan entity.ExecutionPlan, hasPlan bool, width int) string {
	carrySource := any(item)
	if hasDetail && detail != nil {
		carrySource = *detail
	}
	carryRate := displayedCarryRate(carrySource, m.data.System.Strategy, plan, hasPlan)
	hourlyEdge := fundingSpreadHourly(carrySource)
	currentLongEvents, currentShortEvents := displayedFundingEventCounts(carrySource)
	notional := opportunityTargetNotional(carryRate, item.GrossFundingPNL, plan, hasPlan, m.data.System.Strategy)
	strategy := m.data.System.Strategy

	lines := []string{
		clip(strings.Join([]string{
			renderField("估算名义", renderUSDTValue(notional, 2)),
			renderField("当前 Carry率", renderPctValue(carryRate, 5)),
			renderField("当前时均边际", renderPctValue(hourlyEdge, 5)),
			renderField("当前事件数", toneStyle("accent").Render(fmt.Sprintf("多头 %d / 空头 %d", currentLongEvents, currentShortEvents))),
		}, "  "), width),
		clip(toneStyle("subtle").Render("公式: 净收益 = 资金收益 - 入场手续费 - 出场手续费 - 滑点 - 安全缓冲"), width),
		clip(strings.Join([]string{
			toneStyle("subtle").Render("代入:"),
			renderMoneyValue(item.NetExpectedPNL, 3),
			toneStyle("subtle").Render("="),
			renderMoneyValue(item.GrossFundingPNL, 3),
			toneStyle("subtle").Render("-"),
			renderCostAbsValue(item.EntryFeePNL, 3),
			toneStyle("subtle").Render("-"),
			renderCostAbsValue(item.ExitFeePNL, 3),
			toneStyle("subtle").Render("-"),
			renderCostAbsValue(item.SlippagePNL, 3),
			toneStyle("subtle").Render("-"),
			renderCostAbsValue(item.SafetyBufferPNL, 3),
		}, " "), width),
		clip(strings.Join([]string{
			renderField("当前 Entry Path 资金收益", renderMoneyValue(item.GrossFundingPNL, 3)),
			renderField("计算", toneStyle("accent").Render(notionalFormulaText(notional, carryRate))),
		}, "  "), width),
		clip(strings.Join([]string{
			renderField("入场手续费", renderCostSignedValue(item.EntryFeePNL, 3)),
			renderField("模式", toneStyle("accent").Render(feeModeLabel(strategy.EntryMode))),
			renderField("明细", toneStyle("accent").Render(feeBreakdownText(item, strategy, strategy.EntryMode))),
		}, "  "), width),
		clip(strings.Join([]string{
			renderField("出场手续费", renderCostSignedValue(item.ExitFeePNL, 3)),
			renderField("模式", toneStyle("accent").Render(feeModeLabel(strategy.ExitMode))),
			renderField("明细", toneStyle("accent").Render(feeBreakdownText(item, strategy, strategy.ExitMode))),
		}, "  "), width),
		clip(strings.Join([]string{
			renderField("滑点预估", renderCostSignedValue(item.SlippagePNL, 3)),
			renderField("入场惩罚", renderBpsValue(-math.Abs(item.EntryPenaltyBps), 2)),
			renderField("出场惩罚", renderBpsValue(-math.Abs(item.ExitPenaltyBps), 2)),
			renderField("模型", toneStyle("accent").Render(orDefault(item.ExecutionPenaltyModel, "--"))),
			renderField("桶", toneStyle("accent").Render(orDefault(item.ExecutionPenaltyBucket, "--"))),
		}, "  "), width),
		clip(strings.Join([]string{
			renderField("安全缓冲", renderCostSignedValue(item.SafetyBufferPNL, 3)),
			renderField("净收益", renderMoneyValue(item.NetExpectedPNL, 3)),
		}, "  "), width),
		clip(toneStyle("subtle").Render("说明: 资金收益按当前策略选中的持有窗口估算；若会跨多轮 funding，后续事件会结合近期历史做平滑预测。"), width),
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderLegsDetail(item OpportunityListItem, detail *entity.Opportunity, hasDetail bool, loading bool, width int) string {
	lines := []string{renderLegsCompareTable(item, width), ""}
	switch {
	case hasDetail:
		lines = append(lines,
			fmt.Sprintf("%s: %s", ui.subtle.Render("多头规则"), toneStyle("accent").Render(clip(ruleSummary(detail.LongFundingRule), width))),
			fmt.Sprintf("%s: %s", ui.subtle.Render("空头规则"), toneStyle("accent").Render(clip(ruleSummary(detail.ShortFundingRule), width))),
		)
	case loading:
		lines = append(lines, ui.subtle.Render("正在加载资金费规则详情..."))
	default:
		lines = append(lines, ui.subtle.Render("资金费规则详情暂不可用。"))
	}
	return strings.Join(lines, "\n")
}

func renderLegsCompareTable(item OpportunityListItem, width int) string {
	tableWidth := maxInt(48, width)
	metricWidth := 10
	available := tableWidth - metricWidth - 6
	if available < 24 {
		available = 24
	}
	longWidth := available / 2
	shortWidth := available - longWidth

	lines := []string{
		fmt.Sprintf("%s | %s | %s",
			ui.subtle.Render(padTablePlain("指标", metricWidth, lipgloss.Left)),
			ui.good.Render(padTablePlain("做多腿", longWidth, lipgloss.Left)),
			ui.warn.Render(padTablePlain("做空腿", shortWidth, lipgloss.Left)),
		),
		fmt.Sprintf("%s-+-%s-+-%s",
			strings.Repeat("-", metricWidth),
			strings.Repeat("-", longWidth),
			strings.Repeat("-", shortWidth),
		),
		renderLegRow("交易所", item.LongExchange, item.ShortExchange, metricWidth, longWidth, shortWidth, "accent", "accent"),
		renderLegRow("合约", orDefault(item.LongVenueSymbol, "--"), orDefault(item.ShortVenueSymbol, "--"), metricWidth, longWidth, shortWidth, "accent", "accent"),
		renderLegRow("当前 next funding费率", fmtPctRatio(item.LongFundingRate, 5), fmtPctRatio(item.ShortFundingRate, 5), metricWidth, longWidth, shortWidth, signedNumberTone(item.LongFundingRate), signedNumberTone(item.ShortFundingRate)),
		renderLegRow("下一事件预测费率", fmtPctRatio(item.LongFutureFundingRate, 5), fmtPctRatio(item.ShortFutureFundingRate, 5), metricWidth, longWidth, shortWidth, signedNumberTone(item.LongFutureFundingRate), signedNumberTone(item.ShortFutureFundingRate)),
		renderLegRow("小时化", fmtPctRatio(item.LongFundingHourly, 5), fmtPctRatio(item.ShortFundingHourly, 5), metricWidth, longWidth, shortWidth, signedNumberTone(item.LongFundingHourly), signedNumberTone(item.ShortFundingHourly)),
		renderLegRow("买一", priceText(item.LongBidPrice), priceText(item.ShortBidPrice), metricWidth, longWidth, shortWidth, "accent", "accent"),
		renderLegRow("卖一", priceText(item.LongAskPrice), priceText(item.ShortAskPrice), metricWidth, longWidth, shortWidth, "accent", "accent"),
		renderLegRow("标记价", priceText(item.LongMarkPrice), priceText(item.ShortMarkPrice), metricWidth, longWidth, shortWidth, "accent", "accent"),
		renderLegRow("当前 next funding", fmtTime(item.LongFundingTimeMs), fmtTime(item.ShortFundingTimeMs), metricWidth, longWidth, shortWidth, "accent", "accent"),
		renderLegRow("funding间隔", fmt.Sprintf("%sh", fmtNumber(float64(item.LongFundingIntervalHours), 0)), fmt.Sprintf("%sh", fmtNumber(float64(item.ShortFundingIntervalHours), 0)), metricWidth, longWidth, shortWidth, "accent", "accent"),
		renderLegRow("事件数", fmt.Sprintf("%d 次", item.LongFundingEventCount), fmt.Sprintf("%d 次", item.ShortFundingEventCount), metricWidth, longWidth, shortWidth, "accent", "accent"),
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderPlanDetail(item OpportunityListItem, plan entity.ExecutionPlan, hasPlan bool, rec entity.ExecutionRecord, hasRec bool, width int) string {
	if !hasPlan {
		return strings.Join([]string{
			ui.subtle.Render("当前机会尚未关联真实执行计划。"),
			strings.Join([]string{
				renderField("机会状态", renderStatusValue(item.Status)),
				renderField("可执行", renderBoolValue(item.EligibleForExecution, false)),
				renderField("拒绝原因", toneStyle("bad").Render(orDefault(item.RejectReason, "--"))),
			}, "  "),
		}, "\n")
	}
	lines := []string{
		renderField("计划", toneStyle("accent").Render(plan.PlanKey)),
		strings.Join([]string{
			renderField("状态", renderStatusValue(plan.Status)),
			renderField("就绪", renderBoolValue(plan.ReadyNow, false)),
			renderField("预期收益", renderMoneyValue(plan.NetExpectedPNL, 3)),
			renderField("评分", toneStyle("accent").Render(fmtNumber(plan.Score, 2))),
		}, "  "),
		strings.Join([]string{
			renderField("持有模式", toneStyle("accent").Render(holdSelectionModeText(m.data.System.Strategy.HoldSelectionMode))),
			renderField("持有上限", toneStyle("accent").Render(fmt.Sprintf("%sh", fmtNumber(m.data.System.Strategy.HoldHours, 1)))),
			renderField("计划持仓", toneStyle("accent").Render(planExpectedHoldText(plan))),
			renderField("结算后缓冲", toneStyle("accent").Render(orDefault(m.data.System.Execution.CloseGracePeriod, "--"))),
		}, "  "),
		strings.Join([]string{
			renderField("分配资金", renderMoneyValue(plan.CapitalAllocatedUSDT, 2)),
			renderField("目标名义", renderMoneyValue(plan.TargetNotionalUSDT, 2)),
			renderField("取整名义", renderMoneyValue(plan.RoundedNotionalUSDT, 2)),
			renderField("杠杆", toneStyle("accent").Render(fmt.Sprintf("%sx", fmtNumber(plan.TargetLeverage, 2)))),
		}, "  "),
		strings.Join([]string{
			renderField("多头数量", toneStyle("accent").Render(fmtNumber(plan.LongQty, 6))),
			renderField("多头价格", toneStyle("accent").Render(priceText(plan.LongEntryPrice))),
			renderField("空头数量", toneStyle("accent").Render(fmtNumber(plan.ShortQty, 6))),
			renderField("空头价格", toneStyle("accent").Render(priceText(plan.ShortEntryPrice))),
		}, "  "),
		strings.Join([]string{
			renderField("基差", renderBpsValue(plan.CrossVenueBasisBps, 2)),
			renderField("仓位偏斜", renderBpsValue(planPositionSkewBps(plan), 2)),
			renderField("当前 Entry Path 终点", renderTimeValue(plan.ProjectedFundingTimeMs, "accent")),
			renderField("目标平仓", renderTimeValue(plan.TargetCloseTimeMs, "accent")),
			renderField("入场开始", renderTimeValue(plan.EntryWindowOpenMs, "accent")),
			renderField("入场截止", renderTimeValue(plan.EntryWindowCloseMs, "accent")),
		}, "  "),
	}
	if isRollingStrategyMode(plan.StrategyMode) {
		lines = append(lines, strings.Join([]string{
			renderField("策略模式", toneStyle("accent").Render(strategyModeText(plan.StrategyMode))),
			renderField("下次 Review", renderTimeValue(plan.NextReviewTimeMs, "accent")),
			renderField("当前共享结算边界", renderTimeValue(plan.SyncBoundaryTimeMs, "accent")),
			renderField("当前 Entry Path", toneStyle("accent").Render(fmt.Sprintf("%d 段 / %s", plan.EntryPathSegmentCount, entryPathStopReasonText(plan.EntryPathStopReason)))),
		}, "  "))
	}
	if hasRec {
		lines = append(lines,
			strings.Join([]string{
				renderField("执行记录", renderStatusValue(rec.Status)),
				renderField("实盘", renderBoolValue(rec.LiveTrading, true)),
				renderField("占用名义", renderAllocatedNotionalValue(executionAllocatedNotional(rec, plan, true))),
				renderField("开仓单数", toneStyle("accent").Render(fmt.Sprintf("%d", rec.OpenOrderCount))),
				renderField("平仓单数", toneStyle("accent").Render(fmt.Sprintf("%d", rec.CloseOrderCount))),
			}, "  "),
		)
		if isRollingStrategyMode(rec.StrategyMode) || isRollingStrategyMode(plan.StrategyMode) {
			lines = append(lines, strings.Join([]string{
				renderField("Review 次数", toneStyle("accent").Render(fmt.Sprintf("%d", rec.ReviewCount))),
				renderField("最近 Review", renderTimeValue(rec.LastReviewAtMs, "accent")),
				renderField("下次 Review", renderTimeValue(rec.NextReviewTimeMs, "accent")),
				renderField("当前 Boundary", renderTimeValue(rec.CurrentSyncBoundaryMs, "accent")),
			}, "  "))
			lines = append(lines, strings.Join([]string{
				renderField("前驱计划", toneStyle("accent").Render(orDefault(rec.PredecessorPlanKey, "--"))),
				renderField("后继计划", toneStyle("accent").Render(orDefault(rec.SuccessorPlanKey, "--"))),
			}, "  "))
			if strings.TrimSpace(rec.LastReviewReason) != "" {
				lines = append(lines, renderField("Review 说明", toneStyle("warn").Render(clip(rec.LastReviewReason, width))))
			}
		}
		if strings.TrimSpace(rec.LastError) != "" {
			lines = append(lines, renderField("最后错误", toneStyle("bad").Render(clip(rec.LastError, width))))
		}
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderProjectionDetail(_ OpportunityListItem, detail *entity.Opportunity, hasDetail bool, loading bool, width int) string {
	if !hasDetail {
		if loading {
			return ui.subtle.Render("正在加载预测详情...")
		}
		return ui.subtle.Render("当前机会暂无预测详情。")
	}
	if len(detail.ProjectionDetails) == 0 {
		return ui.subtle.Render("当前机会没有可展示的预测详情。")
	}
	lines := []string{ui.accent.Render("标记  兑现时间             窗口     多头  空头  Carry率     时均        资金收益     净收益")}
	for _, row := range detail.ProjectionDetails {
		marker := fmt.Sprintf("%d", row.ProjectionRank)
		if row.IsBestProjection {
			marker = "优选"
		}
		line := fmt.Sprintf("%-5s %-20s %-8s %-5d %-5d %-11s %-11s %-12s %-10s",
			marker,
			clip(fmtTime(row.ProjectedFundingTimeMs), 20),
			clip(fmt.Sprintf("%sh", fmtNumber(row.FundingWindowHours, 2)), 8),
			row.LongFundingEventCount,
			row.ShortFundingEventCount,
			fmtPctRatio(row.CarryRate, 5),
			fmtPctRatio(row.CarryRateHourlyEquivalent, 5),
			fmtMoney(row.GrossFundingPNL, 3),
			fmtMoney(row.NetExpectedPNL, 3),
		)
		lines = append(lines, toneStyle(signedNumberTone(row.NetExpectedPNL)).Render(line))
	}
	if len(detail.EntryPathSegments) > 0 {
		lines = append(lines, "", renderFundingSegmentsSection("当前 Entry Path 段", detail.EntryPathSegments, width))
	}
	if len(detail.FundingSegments) > 0 {
		lines = append(lines, "", renderFundingSegmentsSection("当前 Boundary 内全部结算段", detail.FundingSegments, width))
	}
	return clip(strings.Join(lines, "\n"), width*maxInt(1, len(lines)))
}

func (m Model) renderOrdersDetail(planKey string, width int) string {
	if strings.TrimSpace(planKey) == "" {
		return ui.subtle.Render("当前未选中真实计划，因此没有可展示的订单流。")
	}
	if m.ordersLoading[planKey] {
		return ui.subtle.Render("正在加载订单历史...")
	}
	items, ok := m.orders[planKey]
	if !ok || len(items) == 0 {
		return ui.subtle.Render("当前计划暂无订单记录。")
	}
	lines := []string{ui.accent.Render("阶段    腿角色    交易所       方向   状态               请求数量         成交数量         均价")}
	for _, order := range items {
		line := fmt.Sprintf("%-7s %-8s %-12s %-6s %-18s %-15s %-15s %-10s",
			clip(orderPhaseText(order.Phase), 7),
			clip(orderLegRoleText(order.LegRole), 8),
			clip(order.Exchange, 12),
			clip(orderSideText(order.Side), 6),
			clip(statusText(order.Status), 18),
			fmtNumber(order.RequestedQty, 6),
			fmtNumber(order.ExecutedQty, 6),
			priceText(order.AvgPrice),
		)
		lines = append(lines, toneStyle(statusTone(order.Status)).Render(line))
		if strings.TrimSpace(order.ErrorMessage) != "" {
			lines = append(lines, renderField("错误", toneStyle("bad").Render(clip(order.ErrorMessage, width))))
		}
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderPlanExecutionDetail(plan entity.ExecutionPlan, rec entity.ExecutionRecord, hasRec bool, width int) string {
	targetCloseMs := plan.TargetCloseTimeMs
	if hasRec && rec.TargetCloseTimeMs > 0 {
		targetCloseMs = rec.TargetCloseTimeMs
	}
	lines := []string{
		fmt.Sprintf("%s  %s", ui.accent.Render(plan.Symbol), ui.subtle.Render(opportunityDirection(entity.Opportunity{LongExchange: plan.LongExchange, ShortExchange: plan.ShortExchange}))),
		strings.Join([]string{
			renderField("计划", toneStyle("accent").Render(plan.PlanKey)),
			renderField("就绪", renderBoolValue(plan.ReadyNow, false)),
			renderField("状态", renderStatusValue(plan.Status)),
			renderField("预期收益", renderMoneyValue(plan.NetExpectedPNL, 3)),
		}, "  "),
		strings.Join([]string{
			renderField("名义", renderMoneyValue(targetNotional(plan), 2)),
			renderField("杠杆", toneStyle("accent").Render(fmt.Sprintf("%sx", fmtNumber(plan.TargetLeverage, 2)))),
			renderField("基差", renderBpsValue(plan.CrossVenueBasisBps, 2)),
			renderField("当前 Entry Path 终点", renderTimeValue(plan.ProjectedFundingTimeMs, "accent")),
			renderField("目标平仓", renderTimeValue(targetCloseMs, "accent")),
		}, "  "),
		strings.Join([]string{
			renderField("持有模式", toneStyle("accent").Render(holdSelectionModeText(m.data.System.Strategy.HoldSelectionMode))),
			renderField("持有上限", toneStyle("accent").Render(fmt.Sprintf("%sh", fmtNumber(m.data.System.Strategy.HoldHours, 1)))),
			renderField("计划持仓", toneStyle("accent").Render(planExpectedHoldText(plan))),
			renderField("距计划平仓", renderRemainingTimeValue(targetCloseMs)),
		}, "  "),
		strings.Join([]string{
			renderField("多头", toneStyle("accent").Render(plan.LongVenueSymbol)),
			renderField("数量", toneStyle("accent").Render(fmtNumber(plan.LongQty, 6))),
			renderField("价格", toneStyle("accent").Render(priceText(plan.LongEntryPrice))),
		}, "  "),
		strings.Join([]string{
			renderField("空头", toneStyle("accent").Render(plan.ShortVenueSymbol)),
			renderField("数量", toneStyle("accent").Render(fmtNumber(plan.ShortQty, 6))),
			renderField("价格", toneStyle("accent").Render(priceText(plan.ShortEntryPrice))),
		}, "  "),
	}
	if isRollingStrategyMode(plan.StrategyMode) {
		lines = append(lines, strings.Join([]string{
			renderField("策略模式", toneStyle("accent").Render(strategyModeText(plan.StrategyMode))),
			renderField("下次 Review", renderTimeValue(plan.NextReviewTimeMs, "accent")),
			renderField("当前共享结算边界", renderTimeValue(plan.SyncBoundaryTimeMs, "accent")),
			renderField("当前 Entry Path", toneStyle("accent").Render(fmt.Sprintf("%d 段 / %s", plan.EntryPathSegmentCount, entryPathStopReasonText(plan.EntryPathStopReason)))),
		}, "  "))
	}
	if opp, ok := m.matchingOpportunityForPlan(plan); ok {
		lines = append(lines, strings.Join([]string{
			renderField("关联机会", toneStyle("accent").Render(opportunityKey(opp))),
			renderField("机会状态", renderStatusValue(opp.Status)),
			renderField("结算前持有", toneStyle("accent").Render(holdingDurationText(opp))),
		}, "  "))
	}
	if hasRec {
		lines = append(lines, strings.Join([]string{
			renderField("执行记录", renderStatusValue(rec.Status)),
			renderField("实盘", renderBoolValue(rec.LiveTrading, true)),
			renderField("自动平仓", renderBoolValue(rec.AutoClose, false)),
			renderField("占用名义", renderAllocatedNotionalValue(executionAllocatedNotional(rec, plan, true))),
			renderField("开仓时间", renderTimeValue(rec.OpenedAtMs, "accent")),
			renderField("平仓时间", renderTimeValue(rec.ClosedAtMs, "accent")),
			renderField("预计总持有", toneStyle("accent").Render(executionPlannedHoldText(rec, plan, true))),
		}, "  "))
		if isRollingStrategyMode(rec.StrategyMode) || isRollingStrategyMode(plan.StrategyMode) {
			lines = append(lines, strings.Join([]string{
				renderField("Review 次数", toneStyle("accent").Render(fmt.Sprintf("%d", rec.ReviewCount))),
				renderField("最近 Review", renderTimeValue(rec.LastReviewAtMs, "accent")),
				renderField("下次 Review", renderTimeValue(rec.NextReviewTimeMs, "accent")),
				renderField("当前 Boundary", renderTimeValue(rec.CurrentSyncBoundaryMs, "accent")),
			}, "  "))
			lines = append(lines, strings.Join([]string{
				renderField("前驱计划", toneStyle("accent").Render(orDefault(rec.PredecessorPlanKey, "--"))),
				renderField("后继计划", toneStyle("accent").Render(orDefault(rec.SuccessorPlanKey, "--"))),
			}, "  "))
			if strings.TrimSpace(rec.LastReviewReason) != "" {
				lines = append(lines, renderField("Review 说明", toneStyle("warn").Render(clip(rec.LastReviewReason, width))))
			}
		}
		if strings.TrimSpace(rec.StatusReason) != "" {
			lines = append(lines, renderField("原因", toneStyle("warn").Render(clip(rec.StatusReason, width))))
		}
		if strings.TrimSpace(rec.LastError) != "" {
			lines = append(lines, renderField("最后错误", toneStyle("bad").Render(clip(rec.LastError, width))))
		}
		if positionItem, ok := m.livePositionByPlanKey(plan.PlanKey); ok {
			lines = append(lines, "", ui.panelTitle.Render("交易所持仓"), m.renderLivePositionDetail(positionItem, width))
		}
	}
	lines = append(lines, "", ui.panelTitle.Render("订单记录"), m.renderOrdersDetail(plan.PlanKey, width))
	return strings.Join(lines, "\n")
}

func (m Model) renderExecutionRecordDetail(rec entity.ExecutionRecord, plan entity.ExecutionPlan, hasPlan bool, width int) string {
	targetCloseMs := rec.TargetCloseTimeMs
	if targetCloseMs <= 0 && hasPlan {
		targetCloseMs = plan.TargetCloseTimeMs
	}
	lines := []string{
		fmt.Sprintf("%s  %s", ui.accent.Render(rec.Symbol), ui.subtle.Render(opportunityDirection(entity.Opportunity{LongExchange: rec.LongExchange, ShortExchange: rec.ShortExchange}))),
		strings.Join([]string{
			renderField("计划", toneStyle("accent").Render(rec.PlanKey)),
			renderField("状态", renderStatusValue(rec.Status)),
			renderField("实盘", renderBoolValue(rec.LiveTrading, true)),
			renderField("自动平仓", renderBoolValue(rec.AutoClose, false)),
		}, "  "),
		strings.Join([]string{
			renderField("占用名义", renderAllocatedNotionalValue(executionAllocatedNotional(rec, plan, hasPlan))),
			renderField("开仓时间", renderTimeValue(rec.OpenedAtMs, "accent")),
			renderField("平仓时间", renderTimeValue(rec.ClosedAtMs, "accent")),
			renderField("最近迁移", renderTimeValue(rec.LastTransitionAtMs, "accent")),
			renderField("事件", toneStyle("accent").Render(orDefault(rec.LastTransitionEvent, "--"))),
		}, "  "),
		strings.Join([]string{
			renderField("开仓单数", toneStyle("accent").Render(fmt.Sprintf("%d", rec.OpenOrderCount))),
			renderField("平仓单数", toneStyle("accent").Render(fmt.Sprintf("%d", rec.CloseOrderCount))),
		}, "  "),
		strings.Join([]string{
			renderField("持有模式", toneStyle("accent").Render(holdSelectionModeText(m.data.System.Strategy.HoldSelectionMode))),
			renderField("持有上限", toneStyle("accent").Render(fmt.Sprintf("%sh", fmtNumber(m.data.System.Strategy.HoldHours, 1)))),
			renderField("计划持仓", toneStyle("accent").Render(executionPlannedHoldText(rec, plan, hasPlan))),
			renderField("目标平仓", renderTimeValue(targetCloseMs, "accent")),
			renderField("距计划平仓", renderRemainingTimeValue(targetCloseMs)),
		}, "  "),
	}
	if isRollingStrategyMode(rec.StrategyMode) || (hasPlan && isRollingStrategyMode(plan.StrategyMode)) {
		lines = append(lines, strings.Join([]string{
			renderField("策略模式", toneStyle("accent").Render(strategyModeText(orDefault(rec.StrategyMode, plan.StrategyMode)))),
			renderField("Review 次数", toneStyle("accent").Render(fmt.Sprintf("%d", rec.ReviewCount))),
			renderField("下次 Review", renderTimeValue(rec.NextReviewTimeMs, "accent")),
			renderField("当前 Boundary", renderTimeValue(rec.CurrentSyncBoundaryMs, "accent")),
			renderField("最近 Review", renderTimeValue(rec.LastReviewAtMs, "accent")),
		}, "  "))
		lines = append(lines, strings.Join([]string{
			renderField("前驱计划", toneStyle("accent").Render(orDefault(rec.PredecessorPlanKey, "--"))),
			renderField("后继计划", toneStyle("accent").Render(orDefault(rec.SuccessorPlanKey, "--"))),
		}, "  "))
		if strings.TrimSpace(rec.LastReviewReason) != "" {
			lines = append(lines, renderField("Review 说明", toneStyle("warn").Render(clip(rec.LastReviewReason, width))))
		}
	}
	if strings.TrimSpace(rec.StatusReason) != "" {
		lines = append(lines, renderField("原因", toneStyle("warn").Render(clip(rec.StatusReason, width))))
	}
	if strings.TrimSpace(rec.LastError) != "" {
		lines = append(lines, renderField("最后错误", toneStyle("bad").Render(clip(rec.LastError, width))))
	}
	if hasPlan {
		lines = append(lines, strings.Join([]string{
			renderField("计划状态", renderStatusValue(plan.Status)),
			renderField("预期收益", renderMoneyValue(plan.NetExpectedPNL, 3)),
			renderField("当前 Entry Path 终点", renderTimeValue(plan.ProjectedFundingTimeMs, "accent")),
			renderField("目标平仓", renderTimeValue(plan.TargetCloseTimeMs, "accent")),
		}, "  "))
	}
	if positionItem, ok := m.livePositionByPlanKey(rec.PlanKey); ok {
		lines = append(lines, "", ui.panelTitle.Render("交易所持仓"), m.renderLivePositionDetail(positionItem, width))
	}
	lines = append(lines, "", ui.panelTitle.Render("订单记录"), m.renderOrdersDetail(rec.PlanKey, width))
	return strings.Join(lines, "\n")
}

func (m Model) renderHelp(height int) string {
	lines := []string{
		ui.panelTitle.Render("Keymap"),
		"",
		"General",
		"  q quit   r refresh   ? toggle help   / search   f/F cycle pair filter   s cycle sort",
		"Views",
		"  1 or m scanner   2 or e execution   3 system",
		"Navigation",
		"  j/k or arrows move   g/home first   G/end last   tab switch detail/list mode",
		"Scanner",
		"  o open selected linked plan   c close selected linked plan",
		"执行页",
		"  p 显示计划   x 显示执行记录   o/c 对当前选中项执行开平仓",
		"Safety",
		"  开平仓操作发出前，仍需输入 OPEN 或 CLOSE 进行确认",
		"",
		ui.subtle.Render("Press ? or esc to return."),
	}
	return renderModal(minInt(m.width-4, 96), minInt(height, 18), strings.Join(lines, "\n"))
}

func (m Model) renderConfirm(height int) string {
	target := m.confirm.Target
	word := string(m.confirm.Action)
	liveText := "模拟请求"
	if target.LiveTrading || m.data.System.Execution.LiveTradingEnabled {
		liveText = "实盘请求"
	}
	lines := []string{
		ui.panelTitle.Render(fmt.Sprintf("%s 确认", word)),
		"",
		fmt.Sprintf("来源=%s  模式=%s", target.Source, liveText),
		fmt.Sprintf("计划=%s", target.PlanKey),
		fmt.Sprintf("标的=%s  方向=%s 做多 / %s 做空", target.Symbol, target.LongExchange, target.ShortExchange),
		fmt.Sprintf("状态=%s  预期收益=%s", statusText(target.Status), fmtMoney(target.NetExpectedPNL, 3)),
		"",
		fmt.Sprintf("输入 %s 进行确认：", ui.code.Render(word)),
		m.confirm.Input.View(),
	}
	if strings.TrimSpace(m.confirm.ErrorText) != "" {
		lines = append(lines, "", ui.bad.Render(m.confirm.ErrorText))
	}
	if m.confirm.Submitting {
		lines = append(lines, "", ui.warn.Render("正在提交请求..."))
	}
	lines = append(lines, "", ui.subtle.Render("按 esc 取消"))
	return renderModal(minInt(m.width-4, 88), minInt(height, 14), strings.Join(lines, "\n"))
}

func (m Model) renderFooter(width int) string {
	if m.confirm != nil {
		return ui.footer.Width(width).Render(fmt.Sprintf("模式: %s   输入 %s 确认，按 esc 取消", m.interactionModeLabel(), m.confirm.Action))
	}
	if m.showHelp {
		return ui.footer.Width(width).Render("模式: 帮助   按 esc 或 ? 返回")
	}
	if m.searchMode {
		return ui.footer.Width(width).Render("模式: 搜索   " + m.search.View() + "   enter/esc 应用并关闭")
	}
	return ui.footer.Width(width).Render("模式: 普通   j/k 移动  tab 切换  / 搜索  f 交易对  s 排序  r 刷新  o 开仓  c 平仓  1/2/3 视图  ? 帮助  q 退出")
}

func (m Model) renderTab(label string, active bool) string {
	if active {
		return ui.tabActive.Render(label)
	}
	return ui.tabIdle.Render(label)
}

func (m Model) currentPairLabel() string {
	if strings.TrimSpace(m.pairFilter) == "" {
		return "全部"
	}
	return m.pairFilter
}

func (m Model) metric(label string, value any) string {
	return fmt.Sprintf("%s=%v", label, value)
}

func (m Model) chip(label string, tone string) string {
	switch tone {
	case "good":
		return "[" + ui.good.Render(label) + "]"
	case "warn":
		return "[" + ui.warn.Render(label) + "]"
	case "bad":
		return "[" + ui.bad.Render(label) + "]"
	default:
		return "[" + ui.accent.Render(label) + "]"
	}
}

func (m Model) interactionModeLabel() string {
	switch {
	case m.confirm != nil:
		return "确认"
	case m.searchMode:
		return "搜索"
	case m.showHelp:
		return "帮助"
	default:
		return "普通"
	}
}

func (m Model) interactionModeTone() string {
	switch {
	case m.confirm != nil:
		return "warn"
	case m.searchMode:
		return "accent"
	case m.showHelp:
		return "good"
	default:
		return "good"
	}
}

func (m Model) opportunityPlanLabel(item OpportunityListItem) (string, string) {
	if plan, ok := m.bestPlanForOpportunity(item); ok {
		if plan.ReadyNow {
			return "计划就绪", "good"
		}
		return "计划已生成", "warn"
	}
	if item.EligibleForExecution {
		return "可进入计划", "good"
	}
	return "暂无计划", "bad"
}

func renderMarketSummary(snapshot service.SymbolMarketState, width int) string {
	exchanges := make([]string, 0, len(snapshot.Funding)+len(snapshot.BookTop))
	seen := map[string]struct{}{}
	for ex := range snapshot.Funding {
		seen[ex] = struct{}{}
		exchanges = append(exchanges, ex)
	}
	for ex := range snapshot.BookTop {
		if _, ok := seen[ex]; !ok {
			exchanges = append(exchanges, ex)
		}
	}
	sort.Strings(exchanges)
	if len(exchanges) == 0 {
		return ui.subtle.Render("当前活跃币种暂无市场快照。")
	}
	lines := []string{"交易所        合约            买一         卖一         中间价       标记价       资金费率      下次结算"}
	for _, ex := range exchanges {
		fund := snapshot.Funding[ex]
		book := snapshot.BookTop[ex]
		lines = append(lines, fmt.Sprintf("%-12s %-14s %-12s %-12s %-12s %-12s %-12s %-19s",
			clip(ex, 12),
			clip(orDefault(fund.VenueSymbol, book.VenueSymbol), 14),
			priceText(book.BidPrice),
			priceText(book.AskPrice),
			priceText(midpoint(book.BidPrice, book.AskPrice)),
			priceText(fund.MarkPrice),
			fmtPctRatio(fund.FundingRate, 5),
			clip(fmtTime(fund.FundingTimeMs), 19),
		))
	}
	return clip(strings.Join(lines, "\n"), width*maxInt(1, len(lines)))
}

func ruleSummary(rule entity.OpportunityFundingRule) string {
	parts := []string{
		orDefault(rule.Exchange, "--"),
		orDefault(rule.VenueSymbol, "--"),
		fmt.Sprintf("间隔=%sh", fmtNumber(float64(rule.FundingIntervalHours), 0)),
		"下次=" + fmtTime(rule.NextFundingTimeMs),
		"费率=" + fmtPctRatio(rule.CurrentFundingRate, 5),
	}
	if strings.TrimSpace(rule.ForecastConfidence) != "" {
		parts = append(parts, "置信度="+rule.ForecastConfidence)
	}
	if strings.TrimSpace(rule.MetadataSummary) != "" {
		parts = append(parts, rule.MetadataSummary)
	}
	return strings.Join(parts, " | ")
}

func midpoint(bid, ask float64) float64 {
	if bid <= 0 || ask <= 0 {
		return 0
	}
	return (bid + ask) / 2
}

func visibleWindow(selected, total, visible int) (int, int) {
	if total <= visible {
		return 0, total
	}
	start := selected - visible/2
	if start < 0 {
		start = 0
	}
	end := start + visible
	if end > total {
		end = total
		start = end - visible
	}
	return start, end
}

func splitWidth(total int, leftPercent int) (int, int) {
	if total < 40 {
		return total, total
	}
	left := total * leftPercent / 100
	right := total - left
	if left < 30 {
		left = 30
		right = total - left
	}
	if right < 30 {
		right = 30
		left = total - right
	}
	return left, right
}

func clip(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(text) <= width {
		return text
	}
	if width <= 1 {
		return text[:width]
	}
	runes := []rune(text)
	if len(runes) <= width {
		return text
	}
	return string(runes[:maxInt(1, width-1)]) + "…"
}

func fmtNumber(value float64, digits int) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "--"
	}
	return fmt.Sprintf("%.*f", digits, value)
}

func fmtPctRatio(value float64, digits int) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "--"
	}
	return fmt.Sprintf("%.*f%%", digits, value*100)
}

func fmtMoney(value float64, digits int) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "--"
	}
	sign := ""
	if value > 0 {
		sign = "+"
	}
	return fmt.Sprintf("%s%.*f USDT", sign, digits, value)
}

func fmtSignedBps(value float64, digits int) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "--"
	}
	sign := ""
	if value > 0 {
		sign = "+"
	}
	return fmt.Sprintf("%s%.*f bps", sign, digits, value)
}

func compactUSDT(value float64, digits int) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "--"
	}
	return fmt.Sprintf("%.*fU", digits, value)
}

func compactBps(value float64, digits int) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "--"
	}
	return fmt.Sprintf("%.*fbps", digits, value)
}

func fmtTime(ms int64) string {
	if ms <= 0 {
		return "--"
	}
	return time.UnixMilli(ms).Local().Format("2006-01-02 15:04:05")
}

func fmtDuration(d time.Duration) string {
	if d == 0 {
		return "0s"
	}
	if d < 0 {
		return "passed"
	}
	totalMinutes := int(d.Round(time.Minute).Minutes())
	if totalMinutes <= 0 {
		return fmt.Sprintf("%ds", int(d.Round(time.Second).Seconds()))
	}
	days := totalMinutes / (24 * 60)
	hours := (totalMinutes % (24 * 60)) / 60
	mins := totalMinutes % 60
	parts := make([]string, 0, 3)
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if mins > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%dm", mins))
	}
	return strings.Join(parts, " ")
}

func priceText(value float64) string {
	if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return "--"
	}
	switch {
	case value >= 1000:
		return fmt.Sprintf("%.2f", value)
	case value >= 1:
		return fmt.Sprintf("%.4f", value)
	default:
		return fmt.Sprintf("%.6f", value)
	}
}

func formatClock(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("15:04:05")
}

func boolWord(v bool) string {
	if v {
		return "是"
	}
	return "否"
}

func holdSelectionModeText(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "latest_profitable":
		return "最晚盈利窗口"
	case "strict_target":
		return "严格目标窗口"
	case "best_net":
		return "净收益优先"
	default:
		if strings.TrimSpace(mode) == "" {
			return "--"
		}
		return mode
	}
}

func strategyModeText(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case strings.ToLower(service.StrategyModeRollingCycleAligned):
		return "按结算段滚动"
	case strings.ToLower(service.StrategyModeLegacyProjection):
		return "累计窗口投影"
	default:
		if strings.TrimSpace(mode) == "" {
			return "--"
		}
		return mode
	}
}

func isRollingStrategyMode(mode string) bool {
	return strings.EqualFold(strings.TrimSpace(mode), service.StrategyModeRollingCycleAligned)
}

func entryPathStopReasonText(reason string) string {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "sync_boundary":
		return "到共享结算点"
	case "direction_flip":
		return "到方向翻转"
	case "hold_horizon":
		return "到持有上限"
	default:
		if strings.TrimSpace(reason) == "" {
			return "--"
		}
		return reason
	}
}

func boolTone(v bool, warnWhenTrue bool) string {
	if v && warnWhenTrue {
		return "warn"
	}
	if v {
		return "good"
	}
	return "bad"
}

func statusText(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "eligible":
		return "可执行"
	case "watching":
		return "观察中"
	case "spread_too_small":
		return "价差过小"
	case "basis_too_wide":
		return "基差过宽"
	case "not_profitable":
		return "收益不足"
	case "stale_data":
		return "数据过期"
	case "outside_entry_window":
		return "不在入场窗口"
	case "settlement_window_passed":
		return "结算窗口已过"
	case "pending_open":
		return "待开仓"
	case "opened":
		return "已开仓"
	case "open_partial_failed":
		return "开仓部分失败"
	case "open_failed":
		return "开仓失败"
	case "open_hedging":
		return "开仓对冲中"
	case "risk_blocked":
		return "风控拦截"
	case "api_circuit_open", "circuit_open":
		return "熔断开启"
	case "pending_close":
		return "待平仓"
	case "closed":
		return "已平仓"
	case "close_partial_failed":
		return "平仓部分失败"
	case "close_failed":
		return "平仓失败"
	case "close_hedging":
		return "平仓对冲中"
	case "dry_run_opened":
		return "模拟已开仓"
	case "dry_run_closed":
		return "模拟已平仓"
	case "pending":
		return "处理中"
	case "submitted":
		return "已提交"
	case "new":
		return "新建"
	case "partially_filled":
		return "部分成交"
	case "partially_filled_canceled":
		return "部分成交后撤单"
	case "filled":
		return "全部成交"
	case "canceled", "cancelled":
		return "已撤单"
	case "rejected":
		return "已拒绝"
	case "expired", "expired_in_match":
		return "已过期"
	case "deactivated":
		return "已停用"
	case "error":
		return "错误"
	case "timeout":
		return "超时"
	case "no_position":
		return "无持仓"
	default:
		if strings.TrimSpace(status) == "" {
			return "--"
		}
		return status
	}
}

func toneStyle(tone string) lipgloss.Style {
	switch tone {
	case "good":
		return ui.good
	case "warn":
		return ui.warn
	case "bad":
		return ui.bad
	case "accent":
		return ui.accent
	default:
		return ui.subtle
	}
}

func renderField(label string, value string) string {
	return fmt.Sprintf("%s=%s", ui.subtle.Render(label), value)
}

func alignRightValue(text string, width int) string {
	return lipgloss.NewStyle().Width(width).Align(lipgloss.Right).Render(text)
}

func padTablePlain(text string, width int, align lipgloss.Position) string {
	return lipgloss.NewStyle().Width(width).Align(align).Render(clip(text, width))
}

func renderLegRow(label string, longValue string, shortValue string, metricWidth int, longWidth int, shortWidth int, longTone string, shortTone string) string {
	return fmt.Sprintf("%s | %s | %s",
		ui.subtle.Render(padTablePlain(label, metricWidth, lipgloss.Left)),
		toneStyle(longTone).Render(padTablePlain(longValue, longWidth, lipgloss.Left)),
		toneStyle(shortTone).Render(padTablePlain(shortValue, shortWidth, lipgloss.Left)),
	)
}

func renderMoneyValue(value float64, digits int) string {
	return toneStyle(signedNumberTone(value)).Render(fmtMoney(value, digits))
}

func renderUSDTValue(value float64, digits int) string {
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return toneStyle("subtle").Render("--")
	}
	return toneStyle("accent").Render(fmt.Sprintf("%.*f USDT", digits, value))
}

func renderAllocatedNotionalValue(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return toneStyle("subtle").Render("--")
	}
	return toneStyle("accent").Render(fmtMoney(value, 2))
}

func renderPositionQtyValue(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) || math.Abs(value) <= 1e-9 {
		return toneStyle("subtle").Render("0")
	}
	prefix := ""
	if value > 0 {
		prefix = "+"
	}
	return toneStyle(signedNumberTone(value)).Render(prefix + fmtNumber(value, 6))
}

func renderPctValue(value float64, digits int) string {
	return toneStyle(signedNumberTone(value)).Render(fmtPctRatio(value, digits))
}

func renderBpsValue(value float64, digits int) string {
	return toneStyle(signedNumberTone(value)).Render(fmtSignedBps(value, digits))
}

func renderBasisValue(basisBps float64, maxAllowedBps float64) string {
	if math.IsNaN(basisBps) || math.IsInf(basisBps, 0) {
		return toneStyle("subtle").Render(fmtSignedBps(basisBps, 2))
	}
	absBasis := math.Abs(basisBps)
	tone := "accent"
	switch {
	case maxAllowedBps > 0 && absBasis > maxAllowedBps:
		tone = "bad"
	case maxAllowedBps > 0 && absBasis > maxAllowedBps*0.8:
		tone = "warn"
	}
	return toneStyle(tone).Render(fmtSignedBps(basisBps, 2))
}

func renderStatusValue(status string) string {
	return toneStyle(statusTone(status)).Render(statusText(status))
}

func renderBoolValue(v bool, warnWhenTrue bool) string {
	if v {
		return toneStyle(boolTone(v, warnWhenTrue)).Render("是")
	}
	return toneStyle(boolTone(v, warnWhenTrue)).Render("否")
}

func renderDurationValue(d time.Duration) string {
	tone := "accent"
	switch {
	case d < 0:
		tone = "bad"
	case d <= 30*time.Minute:
		tone = "warn"
	}
	return toneStyle(tone).Render(fmtDuration(d))
}

func livePositionStatusLabel(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "in_sync":
		return "双腿同步"
	case "awaiting_fill":
		return "等待成交"
	case "single_leg":
		return "单腿暴露"
	case "flat":
		return "已空仓"
	case "side_mismatch":
		return "方向异常"
	case "error":
		return "检查错误"
	default:
		if strings.TrimSpace(status) == "" {
			return "未知"
		}
		return status
	}
}

func livePositionStatusTone(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "in_sync":
		return "good"
	case "awaiting_fill":
		return "accent"
	case "flat":
		return "warn"
	case "single_leg", "side_mismatch", "error":
		return "bad"
	default:
		return "subtle"
	}
}

func renderLivePositionLegSummary(leg service.LivePositionLegInspection) string {
	if strings.TrimSpace(leg.Error) != "" {
		return "错误 " + leg.Error
	}
	if !leg.HasPosition {
		return fmt.Sprintf("空仓 预期=%s 数量=%s", positionSideText(leg.ExpectedSide), fmtNumber(leg.ExpectedQty, 6))
	}
	return fmt.Sprintf("数量 %s 开仓价 %s 标记价 %s 浮盈亏 %s",
		renderPositionQtyValue(leg.Position.Quantity),
		priceText(leg.Position.EntryPrice),
		priceText(leg.Position.MarkPrice),
		renderMoneyValue(leg.Position.UnrealizedPnL, 3),
	)
}

func livePositionSummaryText(summary string) string {
	switch strings.TrimSpace(summary) {
	case "":
		return "--"
	case "execution is still pending open; no venue position is visible yet":
		return "执行仍在等待开仓成交，交易所暂未看到持仓"
	case "execution is marked live, but both venue legs are flat on exchange":
		return "执行记录仍标记为 live，但交易所两条腿都已经空仓"
	case "only one venue leg currently has live exposure":
		return "当前只有一条腿在交易所上仍有真实持仓"
	case "both venue legs have exposure matching the expected direction":
		return "两条腿的真实持仓方向都与预期一致"
	case "venue exposure exists, but at least one leg direction differs from the expected side":
		return "交易所上仍有持仓，但至少一条腿的方向和预期不一致"
	default:
		return summary
	}
}

func (m Model) renderLivePositionDetail(item service.LivePositionCandidate, width int) string {
	lines := []string{
		strings.Join([]string{
			renderField("同步", toneStyle(livePositionStatusTone(item.SyncStatus)).Render(livePositionStatusLabel(item.SyncStatus))),
			renderField("摘要", toneStyle(livePositionStatusTone(item.SyncStatus)).Render(clip(livePositionSummaryText(item.Summary), maxInt(10, width-18)))),
		}, "  "),
	}
	lines = append(lines, m.renderLivePositionLegDetail("多头腿", item.LongLeg))
	lines = append(lines, m.renderLivePositionLegDetail("空头腿", item.ShortLeg))
	return strings.Join(lines, "\n")
}

func (m Model) renderLivePositionLegDetail(label string, leg service.LivePositionLegInspection) string {
	if strings.TrimSpace(leg.Error) != "" {
		return strings.Join([]string{
			renderField(label, toneStyle("bad").Render(strings.ToUpper(leg.Exchange+" "+orDefault(leg.VenueSymbol, "--")))),
			renderField("错误", toneStyle("bad").Render(leg.Error)),
		}, "  ")
	}
	return strings.Join([]string{
		renderField(label, toneStyle("accent").Render(strings.ToUpper(leg.Exchange+" "+orDefault(leg.VenueSymbol, "--")))),
		renderField("预期", toneStyle("accent").Render(fmt.Sprintf("%s %s", positionSideText(leg.ExpectedSide), fmtNumber(leg.ExpectedQty, 6)))),
		renderField("数量", renderPositionQtyValue(leg.Position.Quantity)),
		renderField("开仓价", toneStyle("accent").Render(priceText(leg.Position.EntryPrice))),
		renderField("标记价", toneStyle("accent").Render(priceText(leg.Position.MarkPrice))),
		renderField("浮盈亏", renderMoneyValue(leg.Position.UnrealizedPnL, 3)),
		renderField("可见", renderBoolValue(leg.HasPosition, false)),
		renderField("方向正确", renderBoolValue(leg.DirectionOK, false)),
	}, "  ")
}

func renderTimeValue(ms int64, tone string) string {
	return toneStyle(tone).Render(fmtTime(ms))
}

func renderRemainingTimeValue(ms int64) string {
	if ms <= 0 {
		return toneStyle("subtle").Render("--")
	}
	return renderDurationValue(time.Until(time.UnixMilli(ms)))
}

func renderCostSignedValue(value float64, digits int) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return toneStyle("subtle").Render("--")
	}
	return toneStyle("bad").Render(fmtMoney(-math.Abs(value), digits))
}

func renderCostAbsValue(value float64, digits int) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return toneStyle("subtle").Render("--")
	}
	return toneStyle("bad").Render(fmt.Sprintf("%.*f USDT", digits, math.Abs(value)))
}

func signedNumberTone(value float64) string {
	switch {
	case math.IsNaN(value) || math.IsInf(value, 0):
		return "subtle"
	case value > 0:
		return "good"
	case value < 0:
		return "bad"
	default:
		return "subtle"
	}
}

func statusTone(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "--":
		return "subtle"
	case "eligible", "ready", "opened", "closed", "dry_run_opened", "dry_run_closed", "filled", "no_position":
		return "good"
	case "watching", "pending_open", "pending_close", "open_partial_failed", "close_partial_failed", "open_hedging", "close_hedging", "outside_entry_window", "pending", "submitted", "new", "partially_filled", "partially_filled_canceled":
		return "warn"
	default:
		return "bad"
	}
}

func autoCloseActionText(trigger string) string {
	switch strings.ToLower(strings.TrimSpace(trigger)) {
	case "auto_recovery":
		return "自动恢复平仓"
	case "auto_retry_close":
		return "自动重试平仓"
	case "auto_schedule":
		return "按计划平仓"
	case "auto_drawdown_guard":
		return "浮亏护栏平仓"
	case "auto_basis_guard":
		return "基差护栏平仓"
	case "auto_balance_guard":
		return "余额护栏平仓"
	default:
		if strings.TrimSpace(trigger) == "" {
			return ""
		}
		return trigger
	}
}

func autoCloseReasonText(reason string) string {
	out := strings.TrimSpace(reason)
	if out == "" {
		return "--"
	}

	replacements := map[string]string{
		"execution record has auto_close disabled":            "该执行记录已关闭自动平仓",
		"target close time reached":                           "已到目标平仓时间",
		"no auto-close trigger is active":                     "当前未触发自动平仓条件",
		"load execution plan failed:":                         "加载执行计划失败:",
		"close guard mtm: missing latest book snapshot for":   "浮亏护栏: 缺少最新盘口快照 ",
		"close guard mtm: stale book snapshot for":            "浮亏护栏: 盘口快照已过期 ",
		"close guard basis: missing latest book snapshot for": "基差护栏: 缺少最新盘口快照 ",
		"close guard basis: stale book snapshot for":          "基差护栏: 盘口快照已过期 ",
	}
	for from, to := range replacements {
		out = strings.ReplaceAll(out, from, to)
	}

	if strings.HasPrefix(out, "execution status ") && strings.HasSuffix(out, " requires recovery close") {
		status := strings.TrimSuffix(strings.TrimPrefix(out, "execution status "), " requires recovery close")
		return fmt.Sprintf("执行状态 %s，需要恢复性平仓", statusText(status))
	}
	if strings.HasPrefix(out, "execution status ") && strings.HasSuffix(out, " requires close retry") {
		status := strings.TrimSuffix(strings.TrimPrefix(out, "execution status "), " requires close retry")
		return fmt.Sprintf("执行状态 %s，需要重试平仓", statusText(status))
	}
	if strings.HasPrefix(out, "execution status ") && strings.Contains(out, " is outside the auto-close scan set") {
		status := strings.TrimSuffix(strings.TrimPrefix(out, "execution status "), " is outside the auto-close scan set")
		return fmt.Sprintf("执行状态 %s 不在自动平仓扫描范围内", statusText(status))
	}
	if strings.HasPrefix(out, "execution plan ") && strings.HasSuffix(out, " not found") {
		planKey := strings.TrimSuffix(strings.TrimPrefix(out, "execution plan "), " not found")
		return fmt.Sprintf("未找到执行计划 %s", planKey)
	}
	if strings.HasPrefix(out, "waiting until target close time ") && strings.HasSuffix(out, "; safety guards are not triggered") {
		target := strings.TrimSuffix(strings.TrimPrefix(out, "waiting until target close time "), "; safety guards are not triggered")
		return fmt.Sprintf("等待到目标平仓时间 %s；当前未触发安全护栏", target)
	}
	if strings.HasPrefix(out, "mark-to-market pnl ") && strings.Contains(out, " <= -") {
		return strings.ReplaceAll(out, "mark-to-market pnl", "盯市盈亏")
	}
	if strings.HasPrefix(out, "close-side basis ") && strings.Contains(out, " bps > max ") {
		out = strings.ReplaceAll(out, "close-side basis", "平仓基差")
		return strings.ReplaceAll(out, " bps > max ", " bps > 阈值 ")
	}
	if strings.Contains(out, " available ratio ") && strings.Contains(out, " < emergency minimum ") {
		out = strings.ReplaceAll(out, " available ratio ", " 可用余额比例 ")
		return strings.ReplaceAll(out, " < emergency minimum ", " < 紧急下限 ")
	}
	return out
}

func orderPhaseText(phase string) string {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "open":
		return "开仓"
	case "close":
		return "平仓"
	case "hedge_close":
		return "对冲平仓"
	default:
		if strings.TrimSpace(phase) == "" {
			return "--"
		}
		return phase
	}
}

func orderLegRoleText(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "long_leg":
		return "多头腿"
	case "short_leg":
		return "空头腿"
	default:
		if strings.Contains(strings.ToLower(strings.TrimSpace(role)), "hedge") {
			return "对冲腿"
		}
		if strings.TrimSpace(role) == "" {
			return "--"
		}
		return role
	}
}

func orderSideText(side string) string {
	switch strings.ToUpper(strings.TrimSpace(side)) {
	case "BUY":
		return "买入"
	case "SELL":
		return "卖出"
	default:
		if strings.TrimSpace(side) == "" {
			return "--"
		}
		return side
	}
}

func positionSideText(side string) string {
	switch strings.ToUpper(strings.TrimSpace(side)) {
	case "LONG":
		return "做多"
	case "SHORT":
		return "做空"
	case "BUY":
		return "买入"
	case "SELL":
		return "卖出"
	default:
		if strings.TrimSpace(side) == "" {
			return "--"
		}
		return side
	}
}

func opportunityPlanTone(label string) string {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "计划就绪", "可进入计划":
		return "good"
	case "计划已生成":
		return "warn"
	case "暂无计划":
		return "bad"
	default:
		return "accent"
	}
}

func feeModeLabel(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "maker":
		return "maker"
	case "taker":
		return "taker"
	case "mid":
		return "mid"
	default:
		if strings.TrimSpace(mode) == "" {
			return "--"
		}
		return mode
	}
}

func feeBreakdownText(item OpportunityListItem, strategy StrategyStatus, mode string) string {
	parts := make([]string, 0, 2)
	for _, exchangeName := range []string{item.LongExchange, item.ShortExchange} {
		if bps, ok := feeRateBps(strategy, exchangeName, mode); ok {
			parts = append(parts, fmt.Sprintf("%s %s %s", exchangeName, feeModeLabel(mode), fmt.Sprintf("%s bps", fmtNumber(bps, 2))))
		}
	}
	if len(parts) == 0 {
		return "--"
	}
	return strings.Join(parts, " + ")
}

func renderFundingSegmentsSection(title string, segments []entity.OpportunityFundingSegment, width int) string {
	if len(segments) == 0 {
		return ui.subtle.Render(title + ": --")
	}
	lines := []string{
		ui.panelTitle.Render(title),
		ui.subtle.Render("序 结算时间             类型         方向                     Carry率      结算腿        标记"),
	}
	for _, segment := range segments {
		flag := "真实"
		if segment.UsesForecast {
			flag = "预测"
		}
		if !segment.DirectionMatchesHeld {
			flag += "/反向"
		}
		line := fmt.Sprintf("%-2d %-20s %-12s %-24s %-11s %-12s %-8s",
			segment.SegmentRank,
			clip(fmtTime(segment.SettlementTimeMs), 20),
			clip(fundingSegmentTypeText(segment), 12),
			clip(fundingSegmentDirectionText(segment), 24),
			fmtPctRatio(segment.CarryRate, 5),
			clip(fundingSegmentSettlementText(segment), 12),
			clip(flag, 8),
		)
		lines = append(lines, clip(line, width))
	}
	lines = append(lines, ui.subtle.Render("说明: “反向”表示该段最优方向已经不同于当前持仓方向，到该点通常应由 rolling monitor 判断是否翻仓。"))
	return strings.Join(lines, "\n")
}

func fundingSegmentTypeText(segment entity.OpportunityFundingSegment) string {
	switch strings.ToLower(strings.TrimSpace(segment.SegmentType)) {
	case "single_real":
		return "单腿真实"
	case "single_forecast":
		return "单腿预测"
	case "shared_real":
		return "双腿真实"
	default:
		if strings.TrimSpace(segment.SegmentType) == "" {
			return "--"
		}
		return segment.SegmentType
	}
}

func fundingSegmentDirectionText(segment entity.OpportunityFundingSegment) string {
	longExchange := orDefault(segment.OptimalLongExchange, segment.HeldLongExchange)
	shortExchange := orDefault(segment.OptimalShortExchange, segment.HeldShortExchange)
	if strings.TrimSpace(longExchange) == "" && strings.TrimSpace(shortExchange) == "" {
		return "--"
	}
	return fmt.Sprintf("%s 多 / %s 空", orDefault(longExchange, "--"), orDefault(shortExchange, "--"))
}

func fundingSegmentSettlementText(segment entity.OpportunityFundingSegment) string {
	if segment.SharedSettlement {
		return "双边"
	}
	parts := make([]string, 0, 2)
	longExchange := orDefault(segment.HeldLongExchange, segment.OptimalLongExchange)
	shortExchange := orDefault(segment.HeldShortExchange, segment.OptimalShortExchange)
	if segment.LongLegSettles && strings.TrimSpace(longExchange) != "" {
		parts = append(parts, longExchange)
	}
	if segment.ShortLegSettles && strings.TrimSpace(shortExchange) != "" {
		parts = append(parts, shortExchange)
	}
	if len(parts) == 0 {
		return "--"
	}
	return strings.Join(parts, "+")
}

func feeRateBps(strategy StrategyStatus, exchangeName string, mode string) (float64, bool) {
	if len(strategy.FeesByExchange) == 0 {
		return 0, false
	}
	fees, ok := strategy.FeesByExchange[strings.ToLower(strings.TrimSpace(exchangeName))]
	if !ok {
		return 0, false
	}
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "maker":
		return fees.MakerBps, true
	case "taker":
		return fees.TakerBps, true
	case "mid":
		return (fees.MakerBps + fees.TakerBps) / 2, true
	default:
		return fees.MakerBps, true
	}
}

// displayedCarryRate returns the carry rate we want to explain to the user on
// the current screen.
//
// 这里把“展示口径”和“策略内部 projection 口径”刻意分开：
//  1. rolling 模式优先解释当前真实首段 carry，不把 forecast 段直接抬成 headline；
//  2. legacy 或拿不到真实段时，再退回到最佳 projection carry；
//  3. 如果连 projection 也拿不到，最后才用 `gross_funding_pnl / notional` 或 funding 差值兜底。
func displayedCarryRate(item any, strategy StrategyStatus, plan entity.ExecutionPlan, hasPlan bool) float64 {
	if carry, _, _, _, ok := currentCarrySegment(item); ok {
		return carry
	}
	switch v := item.(type) {
	case entity.Opportunity:
		for _, row := range v.ProjectionDetails {
			if row.IsBestProjection {
				return row.CarryRate
			}
		}
		if len(v.ProjectionDetails) > 0 {
			return v.ProjectionDetails[0].CarryRate
		}
		return impliedCarryRate(v.GrossFundingPNL, v.ShortFundingRate-v.LongFundingRate, plan, hasPlan, strategy)
	case OpportunityListItem:
		return impliedCarryRate(v.GrossFundingPNL, v.ShortFundingRate-v.LongFundingRate, plan, hasPlan, strategy)
	default:
		return 0
	}
}

func impliedCarryRate(grossFundingPNL, fallback float64, plan entity.ExecutionPlan, hasPlan bool, strategy StrategyStatus) float64 {
	notional := opportunityTargetNotional(0, grossFundingPNL, plan, hasPlan, strategy)
	if notional > 0 {
		return grossFundingPNL / notional
	}
	return fallback
}

func opportunityTargetNotional(carryRate float64, grossFundingPNL float64, plan entity.ExecutionPlan, hasPlan bool, strategy StrategyStatus) float64 {
	if hasPlan {
		if value := targetNotional(plan); value > 0 {
			return value
		}
	}
	if strategy.EffectiveNotional > 0 {
		return strategy.EffectiveNotional
	}
	if math.Abs(carryRate) > 0 && grossFundingPNL > 0 {
		return grossFundingPNL / math.Abs(carryRate)
	}
	return 0
}

func notionalFormulaText(notional float64, carryRate float64) string {
	if notional <= 0 || math.IsNaN(notional) || math.IsInf(notional, 0) {
		return "--"
	}
	return fmt.Sprintf("%s × %s", fmt.Sprintf("%s USDT", fmtNumber(notional, 2)), fmtPctRatio(carryRate, 5))
}

func selectedMarker(active bool) string {
	if active {
		return ui.cursor.Render(">")
	}
	return " "
}

func countEligible(items []OpportunityListItem) int {
	total := 0
	for _, item := range items {
		if item.EligibleForExecution {
			total++
		}
	}
	return total
}

func orDefault(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func executionAllocatedNotional(rec entity.ExecutionRecord, plan entity.ExecutionPlan, hasPlan bool) float64 {
	if rec.AllocatedNotionalUSDT > 0 {
		return rec.AllocatedNotionalUSDT
	}
	if hasPlan {
		if value := plan.RoundedNotionalUSDT; value > 0 {
			return value
		}
		return targetNotional(plan)
	}
	return 0
}

func renderLimitSuffix(limit int) string {
	if limit <= 0 {
		return "/不限"
	}
	return fmt.Sprintf("/%d", limit)
}

func renderLoopLimit(limit int) string {
	if limit <= 0 {
		return "不限"
	}
	return fmt.Sprintf("%d", limit)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
