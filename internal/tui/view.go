package tui

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"goKit/internal/application/service"
	"goKit/internal/domain/entity"
	"goKit/internal/infrastructure/exchange"

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
	contentHeight := panelContentHeight(height)
	return ui.panel.Width(panelContentWidth(width)).Height(contentHeight).Render(clampRenderedContentHeight(content, contentHeight))
}

func renderModal(width int, height int, content string) string {
	contentHeight := modalContentHeight(height)
	return ui.modal.Width(modalContentWidth(width)).Height(contentHeight).Render(clampRenderedContentHeight(content, contentHeight))
}

func clampRenderedContentHeight(content string, height int) string {
	height = maxInt(1, height)
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.TrimRight(content, "\n")
	if content == "" {
		return ""
	}

	lines := strings.Split(content, "\n")
	if len(lines) <= height {
		return strings.Join(lines, "\n")
	}
	lines = append([]string(nil), lines[:height]...)
	if height > 0 {
		lines[height-1] = ui.subtle.Render("...")
	}
	return strings.Join(lines, "\n")
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
		m.chip("排序 "+m.sortLabel(), "accent"),
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
		m.chip("套利 "+arbitrageModeText(strategy.ArbitrageMode), "accent"),
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
	arbitrageMode := normalizeArbitrageMode(m.data.System.Strategy.ArbitrageMode, service.ArbitrageModeCrossExchange)
	if isSameExchangeArbitrageMode(arbitrageMode) {
		return m.renderSameExchangeOpportunityList(width, height)
	}
	return m.renderCrossExchangeOpportunityList(width, height)
}

func (m Model) renderCrossExchangeOpportunityList(width int, height int) string {
	items := m.filteredOpportunities()
	lines := []string{
		ui.panelTitle.Render(fmt.Sprintf("Opportunities [%s]  %d visible / %d total", arbitrageModeText(service.ArbitrageModeCrossExchange), len(items), len(m.data.Opportunities))),
		ui.subtle.Render(fmt.Sprintf("sorted by %s  |  j/k move  / search  f pair  s sort  o open  c close", m.sortLabel())),
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
			fmt.Sprintf("%s %-3d %-7s %-24s %s", marker, i+1, clip(item.Symbol, 7), clip(opportunityDirectionDisplay(item, service.ArbitrageModeCrossExchange), 24), pnlText),
			fmt.Sprintf("     %-22s 基差 %-9s carry %s", clip(opportunityPairDisplay(item, service.ArbitrageModeCrossExchange), 22), fmtSignedBps(item.BasisBps, 2), carryText),
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

func (m Model) renderSameExchangeOpportunityList(width int, height int) string {
	items := m.filteredOpportunities()
	const rowHeight = 5
	lines := []string{
		ui.panelTitle.Render(fmt.Sprintf("现货对冲机会  %d visible / %d total", len(items), len(m.data.Opportunities))),
		ui.subtle.Render(fmt.Sprintf("sorted by %s  |  j/k move  / search  f pair  s sort  o open  c close", m.sortLabel())),
		"",
	}
	if len(items) == 0 {
		if m.isLoading(loadOpportunities) && len(m.data.Opportunities) == 0 {
			lines = append(lines, ui.subtle.Render("正在加载同所现货 / 永续对冲机会..."))
			return renderPanel(width, height, strings.Join(lines, "\n"))
		}
		lines = append(lines, ui.subtle.Render("当前筛选条件下没有同所现货 / 永续对冲机会。"))
		return renderPanel(width, height, strings.Join(lines, "\n"))
	}

	visibleRows := maxInt(1, panelListContentHeight(height)/rowHeight)
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
		planText := toneStyle(planTone).Render(clip(planLabel, 18))
		marker := selectedMarker(active)
		fundingText := alignRightValue(renderPctValue(item.ShortFundingRate, 5), 10)
		posText, negText, rankText := "--", "--", "--"
		annualizedNetText, longHoldText := "--", "--"
		plan := entity.ExecutionPlan{}
		hasPlan := false
		if matchedPlan, ok := m.bestPlanForOpportunity(item); ok {
			plan = matchedPlan
			hasPlan = true
			if value := perpFundingHistoryPositivePlanText(plan); value != "" {
				posText = value
			}
			if value := perpFundingHistoryNegativePlanText(plan); value != "" {
				negText = value
			}
			if value := fundingRankPlanText(plan); value != "" {
				rankText = value
			}
			if value := perpFundingAnnualizedNetPlanText(plan); value != "" {
				annualizedNetText = value
			}
			if value := sameExchangeLongHoldPlanText(plan, m.data.System.Strategy); value != "" {
				longHoldText = value
			}
		}
		nextFundingGrossText := fmtMoney(sameExchangeNextFundingGrossPNL(item, plan, hasPlan, m.data.System.Strategy), 3)
		windowFundingGrossText := fmtMoney(sameExchangeWindowGrossPNL(item), 3)
		netText := fmtMoney(item.NetExpectedPNL, 3)
		basisPaybackText := "--"
		basisActionText := "--"
		if _, _, _, paybackText, actionText, _, _ := sameExchangeBasisContext(nil, item, plan, hasPlan, m.data.System.Strategy); paybackText != "" || actionText != "" {
			if strings.TrimSpace(paybackText) != "" {
				basisPaybackText = paybackText
			}
			if strings.TrimSpace(actionText) != "" {
				basisActionText = actionText
			}
		}
		longHoldTone := toneStyle(sameExchangeLongHoldTone(longHoldText)).Render(clip(orDefault(longHoldText, "--"), 12))
		basisActionTone := toneStyle(sameExchangeBasisActionTone(basisActionText)).Render(clip(orDefault(basisActionText, "--"), 16))
		row := []string{
			fmt.Sprintf("%s %-3d %-7s %-24s funding %s", marker, i+1, clip(item.Symbol, 7), clip(opportunityDirectionDisplay(item, service.ArbitrageModeSameExchangeSpotPerp), 24), fundingText),
			fmt.Sprintf("     %-18s 单轮毛收 %-14s 主窗毛收 %-14s", clip(opportunityPairDisplay(item, service.ArbitrageModeSameExchangeSpotPerp), 18), clip(nextFundingGrossText, 14), clip(windowFundingGrossText, 14)),
			fmt.Sprintf("     基差 %-10s 回本 %-10s 动作 %s", clip(fmtSignedBps(item.BasisBps, 2), 10), clip(orDefault(basisPaybackText, "--"), 10), basisActionTone),
			fmt.Sprintf("     净收 %-14s 排名 %-10s 历史- %-12s 长持 %s", clip(netText, 14), clip(rankText, 10), clip(negText, 12), longHoldTone),
			fmt.Sprintf("     粗年化净 %-10s 历史+ %-12s  |  %s  |  %s", clip(annualizedNetText, 10), clip(posText, 12), planText, clip(statusText(item.Status), 16)),
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
	arbitrageMode := normalizeArbitrageMode(m.data.System.Strategy.ArbitrageMode, service.ArbitrageModeCrossExchange)
	title := "套利详情"
	if isSameExchangeArbitrageMode(arbitrageMode) {
		title = "现货对冲详情"
	}
	tabs := []string{
		m.renderTab("总览", m.tab == tabOverview),
		m.renderTab("双腿", m.tab == tabLegs),
		m.renderTab("计划", m.tab == tabPlan),
		m.renderTab("预测", m.tab == tabProjection),
		m.renderTab("订单", m.tab == tabOrders),
	}
	lines := []string{
		ui.panelTitle.Render(title),
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
	globalMode := normalizeArbitrageMode(m.data.System.Strategy.ArbitrageMode, service.ArbitrageModeCrossExchange)
	title := fmt.Sprintf("执行  [%s | %s]", m.execTab.String(), arbitrageModeText(globalMode))
	sub := "切换标签  p 计划  x 执行记录  o 开仓  c 平仓"
	if isSameExchangeArbitrageMode(globalMode) {
		sub = "切换标签  p 现货对冲计划  x 现货对冲执行记录  o 开仓  c 平仓"
	}
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
			arbitrageMode := recordArbitrageMode(item, m.data.System.Strategy.ArbitrageMode)
			marker := selectedMarker(i == selected)
			allocatedText := "--"
			if value := executionAllocatedNotional(item, entity.ExecutionPlan{}, false); value > 0 {
				allocatedText = fmtMoney(value, 2)
			}
			block := m.renderExecutionRecordListBlock(item, arbitrageMode, marker, allocatedText)
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
		arbitrageMode := planArbitrageMode(item, m.data.System.Strategy.ArbitrageMode)
		marker := selectedMarker(i == selected)
		block := m.renderExecutionPlanListBlock(item, arbitrageMode, marker)
		if i == selected {
			block = ui.activeRow.Render(block)
		}
		lines = append(lines, block)
	}
	return renderPanel(width, height, strings.Join(lines, "\n"))
}

func (m Model) renderExecutionDetail(width int, height int) string {
	arbitrageMode := normalizeArbitrageMode(m.data.System.Strategy.ArbitrageMode, service.ArbitrageModeCrossExchange)
	title := "执行详情"
	subtitle := "展示当前选中项的计划状态、执行记录与订单历史。"
	if isSameExchangeArbitrageMode(arbitrageMode) {
		title = "现货对冲执行详情"
		subtitle = "展示当前选中项的现货腿、永续腿、执行状态与订单历史。"
	}
	lines := []string{
		ui.panelTitle.Render(title),
		ui.subtle.Render(subtitle),
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
		fmt.Sprintf("策略: 启用=%s  模式=%s  套利=%s  持有上限=%sh  持有模式=%s  杠杆=%sx  最小净收益=%s  有效名义=%s",
			boolWord(strategy.Enabled),
			strategyModeText(strategy.Mode),
			arbitrageModeText(strategy.ArbitrageMode),
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
	if isSameExchangeArbitrageMode(strategy.ArbitrageMode) {
		lines = append(lines, fmt.Sprintf("同所开仓: 长持达标必需=%s  历史样本>=%d  同向支持>=%s  粗年化净收益>=%s",
			boolWord(strategy.SameExchangeRequireLongHoldEligible),
			strategy.SameExchangeMinHistorySampleCount,
			fmtPctRatio(strategy.SameExchangeMinHistoricalSupportRatio, 1),
			fmtPctRatio(strategy.SameExchangeMinAnnualizedNetRate, 1),
		))
		lines = append(lines, fmt.Sprintf("同所退出: funding转负即评估=%s  历史负费率阈值=%s  要求平仓盈利=%s  最低平仓盈利=%s",
			boolWord(strategy.SameExchangeCloseOnNegativeFunding),
			fmtPctRatio(strategy.SameExchangeHistoryNegativeExitThreshold, 1),
			boolWord(strategy.SameExchangeExitRequirePositiveClosePNL),
			fmtMoney(strategy.SameExchangeExitMinClosePNL, 3),
		))
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
		arbitrageMode := recordArbitrageMode(item.Execution, m.data.System.Strategy.ArbitrageMode)
		if item.Plan != nil {
			arbitrageMode = planArbitrageMode(*item.Plan, arbitrageMode)
		}
		longLabel, shortLabel := livePositionLegLabels(arbitrageMode)
		block := strings.Join([]string{
			fmt.Sprintf("%-7s %-24s %s",
				clip(item.Execution.Symbol, 7),
				clip(item.Execution.PlanKey, 24),
				toneStyle(statusTone).Render(livePositionStatusLabel(item.SyncStatus)),
			),
			fmt.Sprintf("    %-4s %-8s %-12s %s",
				clip(longLabel, 4),
				clip(item.LongLeg.Exchange, 8),
				clip(item.LongLeg.VenueSymbol, 12),
				clip(renderLivePositionLegSummary(item.LongLeg), maxInt(10, width-30)),
			),
			fmt.Sprintf("    %-4s %-8s %-12s %s",
				clip(shortLabel, 4),
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
	arbitrageMode := normalizeArbitrageMode(m.data.System.Strategy.ArbitrageMode, service.ArbitrageModeCrossExchange)
	if hasPlan {
		arbitrageMode = planArbitrageMode(plan, arbitrageMode)
	}
	if isSameExchangeArbitrageMode(arbitrageMode) {
		return m.renderSameExchangeOverviewDetail(item, detail, hasDetail, loading, plan, hasPlan, rec, hasRec, width)
	}
	return m.renderCrossExchangeOverviewDetail(item, detail, hasDetail, loading, plan, hasPlan, rec, hasRec, width)
}

func (m Model) renderCrossExchangeOverviewDetail(item OpportunityListItem, detail *entity.Opportunity, hasDetail bool, loading bool, plan entity.ExecutionPlan, hasPlan bool, rec entity.ExecutionRecord, hasRec bool, width int) string {
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
		fmt.Sprintf("%s  %s", ui.accent.Render(item.Symbol), ui.subtle.Render(opportunityDirectionDisplay(item, service.ArbitrageModeCrossExchange))),
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
		ui.panelTitle.Render("历史资金费率明细"),
		m.renderCrossExchangeFundingHistoryDetail(detail, loading, width),
		"",
		ui.panelTitle.Render("收益构成"),
		m.renderPnLBreakdownDetail(item, detail, hasDetail, plan, hasPlan, width),
		"",
		ui.panelTitle.Render("计划信息"),
		m.renderPlanDetail(item, plan, hasPlan, rec, hasRec, width),
	)
	return strings.Join(lines, "\n")
}

func (m Model) renderSameExchangeOverviewDetail(item OpportunityListItem, detail *entity.Opportunity, hasDetail bool, loading bool, plan entity.ExecutionPlan, hasPlan bool, rec entity.ExecutionRecord, hasRec bool, width int) string {
	planLabel, _ := m.opportunityPlanLabel(item)
	carrySource := any(item)
	if hasDetail && detail != nil {
		carrySource = *detail
	}
	displayCarryRate := displayedCarryRate(carrySource, m.data.System.Strategy, plan, hasPlan)
	displayHourlyCarry := fundingSpreadHourly(carrySource)
	expectedCloseMs := opportunityExpectedCloseTimeMs(carrySource, m.data.System.Execution)
	mode := opportunityStrategyMode(carrySource)
	currentFundingRate := sameExchangeCurrentFundingRate(carrySource)
	rankText, positiveText, negativeText, percentileText := sameExchangeFundingContext(detail, plan, hasPlan)
	sampleText := sameExchangeFundingSampleText(detail, plan, hasPlan, loading)
	historyMeanText, supportText, annualizedCarryText, annualizedNetText, longHoldText, longHoldReasonText := sameExchangeLongHoldContext(detail, plan, hasPlan, m.data.System.Strategy)
	eventRateText, suggestedEventsText, suggestedHoldText, suggestedGrossText, suggestedNetText, holdEstimateSourceText := sameExchangeLongHoldRecommendationContext(detail, plan, hasPlan)
	basisModeText, basisCostText, basisCarryText, basisPaybackText, basisActionText, basisReasonText, basisThresholdText := sameExchangeBasisContext(detail, item, plan, hasPlan, m.data.System.Strategy)
	nextFundingGrossPNL := sameExchangeNextFundingGrossPNL(carrySource, plan, hasPlan, m.data.System.Strategy)
	windowFundingGrossPNL := sameExchangeWindowGrossPNL(carrySource)
	totalFrictionPNL := sameExchangeTotalFrictionPNL(carrySource)
	liveRisk := service.SameExchangeLiveRiskInspection{}
	if hasPlan {
		if positionItem, ok := m.livePositionByPlanKey(plan.PlanKey); ok {
			liveRisk = positionItem.Risk
		}
	}
	lines := []string{
		fmt.Sprintf("%s  %s", ui.accent.Render(item.Symbol), ui.subtle.Render("同所现货多 / 永续空")),
		ui.panelTitle.Render("选标视角"),
		strings.Join([]string{
			renderField("当前永续 funding", renderPctValue(currentFundingRate, 5)),
			renderField("单轮 funding 毛收益", renderMoneyValue(nextFundingGrossPNL, 3)),
			renderField("主窗口 funding 收益", renderMoneyValue(windowFundingGrossPNL, 3)),
			renderField("永续横向排名", toneStyle("accent").Render(orDefault(rankText, "--"))),
			renderField("现货-永续基差", renderBasisValue(item.BasisBps, item.MaxAllowedBasisBps)),
			renderField("基差回本", toneStyle("accent").Render(orDefault(basisPaybackText, "--"))),
		}, "  "),
		ui.panelTitle.Render("执行视角"),
		strings.Join([]string{
			renderField("预计净收益", renderMoneyValue(item.NetExpectedPNL, 3)),
			renderField("净收益率", renderBpsValue(item.NetExpectedBps, 2)),
			renderField("当前时均边际", renderPctValue(displayHourlyCarry, 5)),
			renderField("当前 Carry率", renderPctValue(displayCarryRate, 5)),
			renderField("总摩擦成本", renderCostSignedValue(totalFrictionPNL, 3)),
		}, "  "),
		ui.panelTitle.Render("风险视角"),
	}
	lines = append(lines, renderSameExchangeRiskSummary(m.data.System.Strategy, detail, item, plan, hasPlan, liveRisk)...)
	lines = append(lines,
		ui.panelTitle.Render("基差视角"),
		strings.Join([]string{
			renderField("模型", toneStyle("accent").Render(orDefault(basisModeText, "--"))),
			renderField("阈值", toneStyle("accent").Render(orDefault(basisThresholdText, "--"))),
			renderField("基差成本", toneStyle("accent").Render(orDefault(basisCostText, "--"))),
			renderField("每轮 funding", toneStyle("accent").Render(orDefault(basisCarryText, "--"))),
			renderField("回本轮数", toneStyle("accent").Render(orDefault(basisPaybackText, "--"))),
		}, "  "),
		strings.Join([]string{
			renderField("处理动作", toneStyle(sameExchangeBasisActionTone(basisActionText)).Render(orDefault(basisActionText, "--"))),
			renderField("原因", toneStyle("accent").Render(orDefault(basisReasonText, "--"))),
		}, "  "),
		strings.Join([]string{
			renderField("现货腿", toneStyle("accent").Render(item.LongExchange+" 做多")),
			renderField("永续腿", toneStyle("accent").Render(item.ShortExchange+" 做空")),
			renderField("历史正费率", toneStyle("accent").Render(orDefault(positiveText, "--"))),
			renderField("历史负费率", toneStyle("accent").Render(orDefault(negativeText, "--"))),
			renderField("历史分位", toneStyle("accent").Render(orDefault(percentileText, "--"))),
		}, "  "),
		strings.Join([]string{
			renderField("状态", renderStatusValue(item.Status)),
			renderField("可执行", renderBoolValue(item.EligibleForExecution, false)),
			renderField("计划", toneStyle(opportunityPlanTone(planLabel)).Render(planLabel)),
			renderField("当前 funding 窗口", toneStyle("accent").Render(holdingDurationText(item))),
			renderField("主窗净边际", renderPctValue(displayCarryRate, 5)),
		}, "  "),
		"",
		ui.panelTitle.Render("历史资金费率画像"),
		strings.Join([]string{
			renderField("历史样本", toneStyle("accent").Render(sampleText)),
			renderField("历史正费率占比", toneStyle("accent").Render(orDefault(positiveText, "--"))),
			renderField("历史负费率占比", toneStyle("accent").Render(orDefault(negativeText, "--"))),
			renderField("当前费率历史分位", toneStyle("accent").Render(orDefault(percentileText, "--"))),
		}, "  "),
		"",
		ui.panelTitle.Render("历史资金费率明细"),
		m.renderSameExchangeFundingHistoryDetail(detail, loading, width),
		"",
		ui.panelTitle.Render("长期持有评估"),
		strings.Join([]string{
			renderField("历史均值 funding", toneStyle("accent").Render(orDefault(historyMeanText, "--"))),
			renderField("同向历史支持", toneStyle("accent").Render(orDefault(supportText, "--"))),
			renderField("粗略年化毛收益", toneStyle("accent").Render(orDefault(annualizedCarryText, "--"))),
			renderField("粗略年化净收益", toneStyle("accent").Render(orDefault(annualizedNetText, "--"))),
		}, "  "),
		strings.Join([]string{
			renderField("长持判定", toneStyle(sameExchangeLongHoldTone(longHoldText)).Render(orDefault(longHoldText, "--"))),
			renderField("判定原因", toneStyle("accent").Render(orDefault(longHoldReasonText, "--"))),
		}, "  "),
		strings.Join([]string{
			renderField("历史预估单轮", toneStyle("accent").Render(orDefault(eventRateText, "--"))),
			renderField("建议持有", toneStyle("accent").Render(orDefault(suggestedEventsText, "--"))),
			renderField("建议时长", toneStyle("accent").Render(orDefault(suggestedHoldText, "--"))),
			renderField("历史预估毛收益", toneStyle("accent").Render(orDefault(suggestedGrossText, "--"))),
			renderField("历史预估净收益", toneStyle("accent").Render(orDefault(suggestedNetText, "--"))),
		}, "  "),
		strings.Join([]string{
			renderField("下次永续 funding", renderTimeValue(item.ShortFundingTimeMs, "accent")),
			renderField("当前 Entry Path 终点", renderTimeValue(item.ProjectedFundingTimeMs, "accent")),
			renderField("预计离场", renderTimeValue(expectedCloseMs, "accent")),
			renderField("预计总持有", toneStyle("accent").Render(opportunityExpectedHoldText(carrySource, m.data.System.Execution))),
			renderField("持有逻辑", toneStyle("accent").Render(sameExchangeHoldLogicText(m.data.System.Strategy))),
		}, "  "),
		strings.Join([]string{
			renderField("离场守则", toneStyle("accent").Render(sameExchangeExitRuleText(m.data.System.Strategy))),
			renderField("收益口径", toneStyle("accent").Render(orDefault(holdEstimateSourceText, "当前窗口预估"))),
			renderField("批次", toneStyle("accent").Render(orDefault(item.BatchID, "--"))),
			renderField("策略模式", toneStyle("accent").Render(strategyModeText(mode))),
			renderField("资金费模式", toneStyle("accent").Render(orDefault(item.FundingComputationMode, "--"))),
		}, "  "),
	)
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
		ui.panelTitle.Render("现货 / 永续快照"),
		renderMarketSummary(m.data.Market, width),
		"",
		ui.panelTitle.Render("对冲结构"),
		m.renderLegsDetail(item, detail, hasDetail, loading, width),
		"",
		ui.panelTitle.Render("收益拆解"),
		m.renderPnLBreakdownDetail(item, detail, hasDetail, plan, hasPlan, width),
		"",
		ui.panelTitle.Render("执行计划"),
		m.renderPlanDetail(item, plan, hasPlan, rec, hasRec, width),
	)
	return strings.Join(lines, "\n")
}

func (m Model) renderPnLBreakdownDetail(item OpportunityListItem, detail *entity.Opportunity, hasDetail bool, plan entity.ExecutionPlan, hasPlan bool, width int) string {
	arbitrageMode := normalizeArbitrageMode(m.data.System.Strategy.ArbitrageMode, service.ArbitrageModeCrossExchange)
	if hasPlan {
		arbitrageMode = planArbitrageMode(plan, arbitrageMode)
	}
	if isSameExchangeArbitrageMode(arbitrageMode) {
		return m.renderSameExchangePnLBreakdownDetail(item, detail, hasDetail, plan, hasPlan, width)
	}
	return m.renderCrossExchangePnLBreakdownDetail(item, detail, hasDetail, plan, hasPlan, width)
}

func (m Model) renderCrossExchangePnLBreakdownDetail(item OpportunityListItem, detail *entity.Opportunity, hasDetail bool, plan entity.ExecutionPlan, hasPlan bool, width int) string {
	carrySource := any(item)
	if hasDetail && detail != nil {
		carrySource = *detail
	}
	carryRate := displayedCarryRate(carrySource, m.data.System.Strategy, plan, hasPlan)
	hourlyEdge := fundingSpreadHourly(carrySource)
	currentLongEvents, currentShortEvents := displayedFundingEventCounts(carrySource)
	notional := opportunityTargetNotional(carryRate, item.GrossFundingPNL, plan, hasPlan, m.data.System.Strategy)
	strategy := m.data.System.Strategy
	formulaText := "公式: 净收益 = 资金收益 - 入场手续费 - 出场手续费 - 滑点 - 安全缓冲"

	lines := []string{
		clip(strings.Join([]string{
			renderField("估算名义", renderUSDTValue(notional, 2)),
			renderField("当前 Carry率", renderPctValue(carryRate, 5)),
			renderField("当前时均边际", renderPctValue(hourlyEdge, 5)),
			renderField("当前事件数", toneStyle("accent").Render(fmt.Sprintf("多头 %d / 空头 %d", currentLongEvents, currentShortEvents))),
		}, "  "), width),
		clip(toneStyle("subtle").Render(formulaText), width),
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
	arbitrageMode := normalizeArbitrageMode(m.data.System.Strategy.ArbitrageMode, service.ArbitrageModeCrossExchange)
	if isSameExchangeArbitrageMode(arbitrageMode) {
		return m.renderSameExchangeLegsDetail(item, detail, hasDetail, loading, width)
	}
	return m.renderCrossExchangeLegsDetail(item, detail, hasDetail, loading, width)
}

func (m Model) renderSameExchangePnLBreakdownDetail(item OpportunityListItem, detail *entity.Opportunity, hasDetail bool, plan entity.ExecutionPlan, hasPlan bool, width int) string {
	carrySource := any(item)
	if hasDetail && detail != nil {
		carrySource = *detail
	}
	carryRate := displayedCarryRate(carrySource, m.data.System.Strategy, plan, hasPlan)
	hourlyEdge := fundingSpreadHourly(carrySource)
	currentFundingRate := sameExchangeCurrentFundingRate(carrySource)
	windowFundingGrossPNL := sameExchangeWindowGrossPNL(carrySource)
	notional := opportunityTargetNotional(carryRate, windowFundingGrossPNL, plan, hasPlan, m.data.System.Strategy)
	strategy := m.data.System.Strategy
	rankText, positiveText, negativeText, percentileText := sameExchangeFundingContext(detail, plan, hasPlan)
	historyMeanText, supportText, annualizedCarryText, annualizedNetText, longHoldText, longHoldReasonText := sameExchangeLongHoldContext(detail, plan, hasPlan, strategy)
	eventRateText, suggestedEventsText, suggestedHoldText, suggestedGrossText, suggestedNetText, holdEstimateSourceText := sameExchangeLongHoldRecommendationContext(detail, plan, hasPlan)
	basisModeText, basisCostText, basisCarryText, basisPaybackText, basisActionText, basisReasonText, basisThresholdText := sameExchangeBasisContext(detail, item, plan, hasPlan, strategy)
	nextFundingGrossPNL := sameExchangeNextFundingGrossPNL(carrySource, plan, hasPlan, strategy)
	lines := []string{
		clip(strings.Join([]string{
			renderField("估算名义", renderUSDTValue(notional, 2)),
			renderField("当前永续 funding", renderPctValue(currentFundingRate, 5)),
			renderField("单轮 funding 毛收益", renderMoneyValue(nextFundingGrossPNL, 3)),
			renderField("主窗口 funding 收益", renderMoneyValue(windowFundingGrossPNL, 3)),
			renderField("当前时均边际", renderPctValue(hourlyEdge, 5)),
			renderField("现货-永续基差", renderBasisValue(item.BasisBps, item.MaxAllowedBasisBps)),
		}, "  "), width),
		clip(toneStyle("subtle").Render("公式: 净收益 = 永续 funding 收益 - 现货/永续手续费 - 滑点 - 安全缓冲"), width),
		clip(strings.Join([]string{
			toneStyle("subtle").Render("代入:"),
			renderMoneyValue(item.NetExpectedPNL, 3),
			toneStyle("subtle").Render("="),
			renderMoneyValue(windowFundingGrossPNL, 3),
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
			renderField("主窗口 funding 收益", renderMoneyValue(windowFundingGrossPNL, 3)),
			renderField("计算", toneStyle("accent").Render(notionalFormulaText(notional, carryRate))),
		}, "  "), width),
		clip(strings.Join([]string{
			renderField("入场手续费", renderCostSignedValue(item.EntryFeePNL, 3)),
			renderField("出场手续费", renderCostSignedValue(item.ExitFeePNL, 3)),
			renderField("滑点预估", renderCostSignedValue(item.SlippagePNL, 3)),
			renderField("安全缓冲", renderCostSignedValue(item.SafetyBufferPNL, 3)),
			renderField("净收益", renderMoneyValue(item.NetExpectedPNL, 3)),
		}, "  "), width),
		clip(strings.Join([]string{
			renderField("永续横向排名", toneStyle("accent").Render(orDefault(rankText, "--"))),
			renderField("历史正费率", toneStyle("accent").Render(orDefault(positiveText, "--"))),
			renderField("历史负费率", toneStyle("accent").Render(orDefault(negativeText, "--"))),
			renderField("历史分位", toneStyle("accent").Render(orDefault(percentileText, "--"))),
		}, "  "), width),
		clip(strings.Join([]string{
			renderField("历史均值 funding", toneStyle("accent").Render(orDefault(historyMeanText, "--"))),
			renderField("同向历史支持", toneStyle("accent").Render(orDefault(supportText, "--"))),
			renderField("粗年化毛收益", toneStyle("accent").Render(orDefault(annualizedCarryText, "--"))),
			renderField("粗年化净收益", toneStyle("accent").Render(orDefault(annualizedNetText, "--"))),
		}, "  "), width),
		clip(strings.Join([]string{
			renderField("长持判定", toneStyle(sameExchangeLongHoldTone(longHoldText)).Render(orDefault(longHoldText, "--"))),
			renderField("原因", toneStyle("accent").Render(orDefault(longHoldReasonText, "--"))),
		}, "  "), width),
		clip(strings.Join([]string{
			renderField("历史预估单轮", toneStyle("accent").Render(orDefault(eventRateText, "--"))),
			renderField("建议持有", toneStyle("accent").Render(orDefault(suggestedEventsText, "--"))),
			renderField("建议时长", toneStyle("accent").Render(orDefault(suggestedHoldText, "--"))),
			renderField("历史预估毛收益", toneStyle("accent").Render(orDefault(suggestedGrossText, "--"))),
			renderField("历史预估净收益", toneStyle("accent").Render(orDefault(suggestedNetText, "--"))),
		}, "  "), width),
		clip(strings.Join([]string{
			renderField("离场守则", toneStyle("accent").Render(sameExchangeExitRuleText(strategy))),
			renderField("手续费模式", toneStyle("accent").Render(feeModeLabel(strategy.EntryMode)+" / "+feeModeLabel(strategy.ExitMode))),
			renderField("收益口径", toneStyle("accent").Render(orDefault(holdEstimateSourceText, "当前窗口预估"))),
		}, "  "), width),
		clip(strings.Join([]string{
			renderField("基差模型", toneStyle("accent").Render(orDefault(basisModeText, "--"))),
			renderField("阈值", toneStyle("accent").Render(orDefault(basisThresholdText, "--"))),
			renderField("基差成本", toneStyle("accent").Render(orDefault(basisCostText, "--"))),
			renderField("每轮 funding", toneStyle("accent").Render(orDefault(basisCarryText, "--"))),
			renderField("回本轮数", toneStyle("accent").Render(orDefault(basisPaybackText, "--"))),
		}, "  "), width),
		clip(strings.Join([]string{
			renderField("处理动作", toneStyle(sameExchangeBasisActionTone(basisActionText)).Render(orDefault(basisActionText, "--"))),
			renderField("原因", toneStyle("accent").Render(orDefault(basisReasonText, "--"))),
		}, "  "), width),
		clip(toneStyle("subtle").Render("说明: 同所模式优先看永续 funding 的横向高位、历史回落概率，以及扣除手续费和滑点后的净收益空间。"), width),
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderCrossExchangeLegsDetail(item OpportunityListItem, detail *entity.Opportunity, hasDetail bool, loading bool, width int) string {
	lines := []string{renderLegsCompareTable(item, width, service.ArbitrageModeCrossExchange), ""}
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

func (m Model) renderSameExchangeLegsDetail(item OpportunityListItem, detail *entity.Opportunity, hasDetail bool, loading bool, width int) string {
	plan, hasPlan := m.bestPlanForOpportunity(item)
	rankText, positiveText, negativeText, percentileText := sameExchangeFundingContext(detail, plan, hasPlan)
	historyMeanText, supportText, annualizedCarryText, annualizedNetText, longHoldText, longHoldReasonText := sameExchangeLongHoldContext(detail, plan, hasPlan, m.data.System.Strategy)
	eventRateText, suggestedEventsText, suggestedHoldText, _, suggestedNetText, holdEstimateSourceText := sameExchangeLongHoldRecommendationContext(detail, plan, hasPlan)
	lines := []string{
		strings.Join([]string{
			renderField("现货腿", toneStyle("good").Render(item.LongExchange+" "+orDefault(item.LongVenueSymbol, "--"))),
			renderField("方向", toneStyle("accent").Render("BUY")),
			renderField("买一 / 卖一", toneStyle("accent").Render(priceText(item.LongBidPrice)+" / "+priceText(item.LongAskPrice))),
			renderField("标记价", toneStyle("accent").Render(priceText(item.LongMarkPrice))),
		}, "  "),
		strings.Join([]string{
			renderField("永续腿", toneStyle("warn").Render(item.ShortExchange+" "+orDefault(item.ShortVenueSymbol, "--"))),
			renderField("方向", toneStyle("accent").Render("SELL")),
			renderField("当前 funding", renderPctValue(item.ShortFundingRate, 5)),
			renderField("下一事件预测", renderPctValue(item.ShortFutureFundingRate, 5)),
			renderField("下次 funding", renderTimeValue(item.ShortFundingTimeMs, "accent")),
		}, "  "),
		strings.Join([]string{
			renderField("永续横向排名", toneStyle("accent").Render(orDefault(rankText, "--"))),
			renderField("历史正费率", toneStyle("accent").Render(orDefault(positiveText, "--"))),
			renderField("历史负费率", toneStyle("accent").Render(orDefault(negativeText, "--"))),
			renderField("历史分位", toneStyle("accent").Render(orDefault(percentileText, "--"))),
		}, "  "),
		strings.Join([]string{
			renderField("历史均值 funding", toneStyle("accent").Render(orDefault(historyMeanText, "--"))),
			renderField("同向历史支持", toneStyle("accent").Render(orDefault(supportText, "--"))),
			renderField("粗年化毛收益", toneStyle("accent").Render(orDefault(annualizedCarryText, "--"))),
			renderField("粗年化净收益", toneStyle("accent").Render(orDefault(annualizedNetText, "--"))),
		}, "  "),
		strings.Join([]string{
			renderField("长持判定", toneStyle(sameExchangeLongHoldTone(longHoldText)).Render(orDefault(longHoldText, "--"))),
			renderField("原因", toneStyle("accent").Render(orDefault(longHoldReasonText, "--"))),
		}, "  "),
		strings.Join([]string{
			renderField("历史预估单轮", toneStyle("accent").Render(orDefault(eventRateText, "--"))),
			renderField("建议持有", toneStyle("accent").Render(orDefault(suggestedEventsText, "--"))),
			renderField("建议时长", toneStyle("accent").Render(orDefault(suggestedHoldText, "--"))),
			renderField("历史预估净收益", toneStyle("accent").Render(orDefault(suggestedNetText, "--"))),
			renderField("收益口径", toneStyle("accent").Render(orDefault(holdEstimateSourceText, "当前窗口预估"))),
		}, "  "),
	}
	switch {
	case hasDetail && detail != nil:
		lines = append(lines,
			fmt.Sprintf("%s: %s", ui.subtle.Render("现货腿规则"), toneStyle("accent").Render(clip(ruleSummary(detail.LongFundingRule), width))),
			fmt.Sprintf("%s: %s", ui.subtle.Render("永续腿规则"), toneStyle("accent").Render(clip(ruleSummary(detail.ShortFundingRule), width))),
		)
	case loading:
		lines = append(lines, ui.subtle.Render("正在加载现货 / 永续 funding 详情..."))
	default:
		lines = append(lines, ui.subtle.Render("当前还没有更细的现货 / 永续 funding 规则详情。"))
	}
	return strings.Join(lines, "\n")
}

func renderLegsCompareTable(item OpportunityListItem, width int, arbitrageMode string) string {
	tableWidth := maxInt(48, width)
	metricWidth := 10
	available := tableWidth - metricWidth - 6
	if available < 24 {
		available = 24
	}
	longWidth := available / 2
	shortWidth := available - longWidth
	longTitle := "做多腿"
	shortTitle := "做空腿"
	if isSameExchangeArbitrageMode(arbitrageMode) {
		longTitle = "现货多头腿"
		shortTitle = "永续空头腿"
	}

	lines := []string{
		fmt.Sprintf("%s | %s | %s",
			ui.subtle.Render(padTablePlain("指标", metricWidth, lipgloss.Left)),
			ui.good.Render(padTablePlain(longTitle, longWidth, lipgloss.Left)),
			ui.warn.Render(padTablePlain(shortTitle, shortWidth, lipgloss.Left)),
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
	arbitrageMode := planArbitrageMode(plan, m.data.System.Strategy.ArbitrageMode)
	if isSameExchangeArbitrageMode(arbitrageMode) {
		return m.renderSameExchangePlanDetail(item, plan, rec, hasRec, width)
	}
	return m.renderCrossExchangePlanDetail(item, plan, rec, hasRec, width)
}

func (m Model) renderCrossExchangePlanDetail(_ OpportunityListItem, plan entity.ExecutionPlan, rec entity.ExecutionRecord, hasRec bool, width int) string {
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

func (m Model) renderSameExchangePlanDetail(_ OpportunityListItem, plan entity.ExecutionPlan, rec entity.ExecutionRecord, hasRec bool, width int) string {
	longHoldText := sameExchangeLongHoldPlanText(plan, m.data.System.Strategy)
	longHoldReasonText := sameExchangeLongHoldReasonPlanText(plan, m.data.System.Strategy)
	eventRateText := perpFundingEstimatedEventRatePlanText(plan)
	suggestedEventsText := perpFundingSuggestedHoldEventsPlanText(plan)
	suggestedHoldText := perpFundingSuggestedHoldDurationPlanText(plan)
	suggestedGrossText := perpFundingSuggestedGrossPNLPlanText(plan)
	suggestedNetText := perpFundingSuggestedNetPNLPlanText(plan)
	holdEstimateSourceText := sameExchangeLongHoldEstimateSourceText(nil, plan, true)
	basisModeText, basisCostText, basisCarryText, basisPaybackText, basisActionText, basisReasonText, basisThresholdText := sameExchangeBasisContext(nil, OpportunityListItem{}, plan, true, m.data.System.Strategy)
	liveRisk := service.SameExchangeLiveRiskInspection{}
	if positionItem, ok := m.livePositionByPlanKey(plan.PlanKey); ok {
		liveRisk = positionItem.Risk
	}
	lines := []string{
		renderField("计划", toneStyle("accent").Render(plan.PlanKey)),
		strings.Join([]string{
			renderField("状态", renderStatusValue(plan.Status)),
			renderField("就绪", renderBoolValue(plan.ReadyNow, false)),
			renderField("预期收益", renderMoneyValue(plan.NetExpectedPNL, 3)),
			renderField("评分", toneStyle("accent").Render(fmtNumber(plan.Score, 2))),
			renderField("模式", toneStyle("accent").Render(arbitrageModeText(planArbitrageMode(plan, service.ArbitrageModeSameExchangeSpotPerp)))),
		}, "  "),
		strings.Join([]string{
			renderField("现货腿", toneStyle("good").Render(plan.LongExchange+" "+orDefault(plan.LongVenueSymbol, "--"))),
			renderField("永续腿", toneStyle("warn").Render(plan.ShortExchange+" "+orDefault(plan.ShortVenueSymbol, "--"))),
			renderField("现货数量", toneStyle("accent").Render(fmtNumber(plan.LongQty, 6))),
			renderField("永续数量", toneStyle("accent").Render(fmtNumber(plan.ShortQty, 6))),
			renderField("仓位偏斜", renderBpsValue(planPositionSkewBps(plan), 2)),
		}, "  "),
		strings.Join([]string{
			renderField("分配资金", renderMoneyValue(plan.CapitalAllocatedUSDT, 2)),
			renderField("目标名义", renderMoneyValue(plan.TargetNotionalUSDT, 2)),
			renderField("取整名义", renderMoneyValue(plan.RoundedNotionalUSDT, 2)),
			renderField("现货价格", toneStyle("accent").Render(priceText(plan.LongEntryPrice))),
			renderField("永续价格", toneStyle("accent").Render(priceText(plan.ShortEntryPrice))),
		}, "  "),
		strings.Join([]string{
			renderField("永续横向排名", toneStyle("accent").Render(orDefault(fundingRankPlanText(plan), "--"))),
			renderField("历史正费率", toneStyle("accent").Render(orDefault(perpFundingHistoryPositivePlanText(plan), "--"))),
			renderField("历史负费率", toneStyle("accent").Render(orDefault(perpFundingHistoryNegativePlanText(plan), "--"))),
			renderField("历史分位", toneStyle("accent").Render(orDefault(perpFundingHistoricalPercentilePlanText(plan), "--"))),
		}, "  "),
		strings.Join([]string{
			renderField("历史均值 funding", toneStyle("accent").Render(orDefault(perpFundingHistoryMeanPlanText(plan), "--"))),
			renderField("同向历史支持", toneStyle("accent").Render(orDefault(perpFundingSupportPlanText(plan), "--"))),
			renderField("粗年化毛收益", toneStyle("accent").Render(orDefault(perpFundingAnnualizedCarryPlanText(plan), "--"))),
			renderField("粗年化净收益", toneStyle("accent").Render(orDefault(perpFundingAnnualizedNetPlanText(plan), "--"))),
		}, "  "),
		strings.Join([]string{
			renderField("长持判定", toneStyle(sameExchangeLongHoldTone(longHoldText)).Render(orDefault(longHoldText, "--"))),
			renderField("原因", toneStyle("accent").Render(orDefault(longHoldReasonText, "--"))),
		}, "  "),
		strings.Join([]string{
			renderField("历史预估单轮", toneStyle("accent").Render(orDefault(eventRateText, "--"))),
			renderField("建议持有", toneStyle("accent").Render(orDefault(suggestedEventsText, "--"))),
			renderField("建议时长", toneStyle("accent").Render(orDefault(suggestedHoldText, "--"))),
			renderField("历史预估毛收益", toneStyle("accent").Render(orDefault(suggestedGrossText, "--"))),
			renderField("历史预估净收益", toneStyle("accent").Render(orDefault(suggestedNetText, "--"))),
		}, "  "),
		strings.Join([]string{
			renderField("持有逻辑", toneStyle("accent").Render(sameExchangeHoldLogicText(m.data.System.Strategy))),
			renderField("计划持仓", toneStyle("accent").Render(planExpectedHoldText(plan))),
			renderField("当前 Entry Path 终点", renderTimeValue(plan.ProjectedFundingTimeMs, "accent")),
			renderField("目标平仓", renderTimeValue(plan.TargetCloseTimeMs, "accent")),
			renderField("入场截止", renderTimeValue(plan.EntryWindowCloseMs, "accent")),
		}, "  "),
		strings.Join([]string{
			renderField("现货-永续基差", renderBpsValue(plan.CrossVenueBasisBps, 2)),
			renderField("基差模型", toneStyle("accent").Render(orDefault(basisModeText, "--"))),
			renderField("回本轮数", toneStyle("accent").Render(orDefault(basisPaybackText, "--"))),
			renderField("基差成本", toneStyle("accent").Render(orDefault(basisCostText, "--"))),
			renderField("每轮 funding", toneStyle("accent").Render(orDefault(basisCarryText, "--"))),
			renderField("基差动作", toneStyle(sameExchangeBasisActionTone(basisActionText)).Render(orDefault(basisActionText, "--"))),
		}, "  "),
		ui.panelTitle.Render("风险视角"),
		strings.Join([]string{
			renderField("基差阈值", toneStyle("accent").Render(orDefault(basisThresholdText, "--"))),
			renderField("原因", toneStyle("accent").Render(orDefault(basisReasonText, "--"))),
			renderField("离场守则", toneStyle("accent").Render(sameExchangeExitRuleText(m.data.System.Strategy))),
			renderField("收益口径", toneStyle("accent").Render(orDefault(holdEstimateSourceText, "当前窗口预估"))),
		}, "  "),
	}
	lines = append(lines, renderSameExchangeRiskSummary(m.data.System.Strategy, nil, OpportunityListItem{}, plan, true, liveRisk)...)
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
	arbitrageMode := planArbitrageMode(plan, m.data.System.Strategy.ArbitrageMode)
	if isSameExchangeArbitrageMode(arbitrageMode) {
		return m.renderSameExchangePlanExecutionDetail(plan, rec, hasRec, width)
	}
	return m.renderCrossExchangePlanExecutionDetail(plan, rec, hasRec, width)
}

func (m Model) renderCrossExchangePlanExecutionDetail(plan entity.ExecutionPlan, rec entity.ExecutionRecord, hasRec bool, width int) string {
	targetCloseMs := plan.TargetCloseTimeMs
	if hasRec && rec.TargetCloseTimeMs > 0 {
		targetCloseMs = rec.TargetCloseTimeMs
	}
	lines := []string{
		fmt.Sprintf("%s  %s", ui.accent.Render(plan.Symbol), ui.subtle.Render(opportunityDirectionDisplay(entity.Opportunity{LongExchange: plan.LongExchange, ShortExchange: plan.ShortExchange}, service.ArbitrageModeCrossExchange))),
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
	arbitrageMode := recordArbitrageMode(rec, m.data.System.Strategy.ArbitrageMode)
	if hasPlan {
		arbitrageMode = planArbitrageMode(plan, arbitrageMode)
	}
	if isSameExchangeArbitrageMode(arbitrageMode) {
		return m.renderSameExchangeExecutionRecordDetail(rec, plan, hasPlan, width)
	}
	return m.renderCrossExchangeExecutionRecordDetail(rec, plan, hasPlan, width)
}

func (m Model) renderSameExchangePlanExecutionDetail(plan entity.ExecutionPlan, rec entity.ExecutionRecord, hasRec bool, width int) string {
	targetCloseMs := rec.TargetCloseTimeMs
	if targetCloseMs <= 0 {
		targetCloseMs = plan.TargetCloseTimeMs
	}
	longHoldText := sameExchangeLongHoldPlanText(plan, m.data.System.Strategy)
	longHoldReasonText := sameExchangeLongHoldReasonPlanText(plan, m.data.System.Strategy)
	basisModeText, basisCostText, basisCarryText, basisPaybackText, basisActionText, basisReasonText, basisThresholdText := sameExchangeBasisContext(nil, OpportunityListItem{}, plan, true, m.data.System.Strategy)
	liveRisk := service.SameExchangeLiveRiskInspection{}
	if positionItem, ok := m.livePositionByPlanKey(plan.PlanKey); ok {
		liveRisk = positionItem.Risk
	}
	lines := []string{
		fmt.Sprintf("%s  %s", ui.accent.Render(plan.Symbol), ui.subtle.Render("现货多 / 永续空")),
		strings.Join([]string{
			renderField("计划", toneStyle("accent").Render(plan.PlanKey)),
			renderField("就绪", renderBoolValue(plan.ReadyNow, false)),
			renderField("状态", renderStatusValue(plan.Status)),
			renderField("预期收益", renderMoneyValue(plan.NetExpectedPNL, 3)),
			renderField("模式", toneStyle("accent").Render(arbitrageModeText(planArbitrageMode(plan, service.ArbitrageModeSameExchangeSpotPerp)))),
		}, "  "),
		strings.Join([]string{
			renderField("现货腿", toneStyle("good").Render(plan.LongExchange+" "+orDefault(plan.LongVenueSymbol, "--"))),
			renderField("永续腿", toneStyle("warn").Render(plan.ShortExchange+" "+orDefault(plan.ShortVenueSymbol, "--"))),
			renderField("现货数量", toneStyle("accent").Render(fmtNumber(plan.LongQty, 6))),
			renderField("永续数量", toneStyle("accent").Render(fmtNumber(plan.ShortQty, 6))),
			renderField("占用名义", renderAllocatedNotionalValue(executionAllocatedNotional(rec, plan, true))),
		}, "  "),
		strings.Join([]string{
			renderField("永续横向排名", toneStyle("accent").Render(orDefault(fundingRankPlanText(plan), "--"))),
			renderField("历史正费率", toneStyle("accent").Render(orDefault(perpFundingHistoryPositivePlanText(plan), "--"))),
			renderField("历史负费率", toneStyle("accent").Render(orDefault(perpFundingHistoryNegativePlanText(plan), "--"))),
			renderField("历史分位", toneStyle("accent").Render(orDefault(perpFundingHistoricalPercentilePlanText(plan), "--"))),
		}, "  "),
		strings.Join([]string{
			renderField("历史均值 funding", toneStyle("accent").Render(orDefault(perpFundingHistoryMeanPlanText(plan), "--"))),
			renderField("同向历史支持", toneStyle("accent").Render(orDefault(perpFundingSupportPlanText(plan), "--"))),
			renderField("粗年化毛收益", toneStyle("accent").Render(orDefault(perpFundingAnnualizedCarryPlanText(plan), "--"))),
			renderField("粗年化净收益", toneStyle("accent").Render(orDefault(perpFundingAnnualizedNetPlanText(plan), "--"))),
		}, "  "),
		strings.Join([]string{
			renderField("长持判定", toneStyle(sameExchangeLongHoldTone(longHoldText)).Render(orDefault(longHoldText, "--"))),
			renderField("原因", toneStyle("accent").Render(orDefault(longHoldReasonText, "--"))),
		}, "  "),
		strings.Join([]string{
			renderField("基差模型", toneStyle("accent").Render(orDefault(basisModeText, "--"))),
			renderField("基差阈值", toneStyle("accent").Render(orDefault(basisThresholdText, "--"))),
			renderField("基差成本", toneStyle("accent").Render(orDefault(basisCostText, "--"))),
			renderField("每轮 funding", toneStyle("accent").Render(orDefault(basisCarryText, "--"))),
			renderField("回本轮数", toneStyle("accent").Render(orDefault(basisPaybackText, "--"))),
		}, "  "),
		strings.Join([]string{
			renderField("持有逻辑", toneStyle("accent").Render(sameExchangeHoldLogicText(m.data.System.Strategy))),
			renderField("计划持仓", toneStyle("accent").Render(planExpectedHoldText(plan))),
			renderField("目标平仓", renderTimeValue(targetCloseMs, "accent")),
			renderField("距计划平仓", renderRemainingTimeValue(targetCloseMs)),
			renderField("离场守则", toneStyle("accent").Render(sameExchangeExitRuleText(m.data.System.Strategy))),
			renderField("基差动作", toneStyle(sameExchangeBasisActionTone(basisActionText)).Render(orDefault(basisActionText, "--"))),
		}, "  "),
		ui.panelTitle.Render("风险视角"),
	}
	lines = append(lines, renderSameExchangeRiskSummary(m.data.System.Strategy, nil, OpportunityListItem{}, plan, true, liveRisk)...)
	lines = append(lines,
		strings.Join([]string{
			renderField("基差原因", toneStyle("accent").Render(orDefault(basisReasonText, "--"))),
		}, "  "),
	)
	if isRollingStrategyMode(plan.StrategyMode) || (hasRec && isRollingStrategyMode(rec.StrategyMode)) {
		lines = append(lines, strings.Join([]string{
			renderField("策略模式", toneStyle("accent").Render(strategyModeText(plan.StrategyMode))),
			renderField("下次 Review", renderTimeValue(plan.NextReviewTimeMs, "accent")),
			renderField("当前 Boundary", renderTimeValue(plan.SyncBoundaryTimeMs, "accent")),
			renderField("当前 Entry Path", toneStyle("accent").Render(fmt.Sprintf("%d 段 / %s", plan.EntryPathSegmentCount, entryPathStopReasonText(plan.EntryPathStopReason)))),
		}, "  "))
	}
	if opp, ok := m.matchingOpportunityForPlan(plan); ok {
		lines = append(lines, strings.Join([]string{
			renderField("关联机会", toneStyle("accent").Render(opportunityKey(opp))),
			renderField("机会状态", renderStatusValue(opp.Status)),
			renderField("当前 funding 窗口", toneStyle("accent").Render(holdingDurationText(opp))),
		}, "  "))
	}
	if hasRec {
		lines = append(lines, strings.Join([]string{
			renderField("执行记录", renderStatusValue(rec.Status)),
			renderField("实盘", renderBoolValue(rec.LiveTrading, true)),
			renderField("自动平仓", renderBoolValue(rec.AutoClose, false)),
			renderField("开仓时间", renderTimeValue(rec.OpenedAtMs, "accent")),
			renderField("平仓时间", renderTimeValue(rec.ClosedAtMs, "accent")),
			renderField("预计总持有", toneStyle("accent").Render(executionPlannedHoldText(rec, plan, true))),
		}, "  "))
		lines = append(lines, strings.Join([]string{
			renderField("开仓单数", toneStyle("accent").Render(fmt.Sprintf("%d", rec.OpenOrderCount))),
			renderField("平仓单数", toneStyle("accent").Render(fmt.Sprintf("%d", rec.CloseOrderCount))),
			renderField("最近迁移", renderTimeValue(rec.LastTransitionAtMs, "accent")),
			renderField("事件", toneStyle("accent").Render(orDefault(rec.LastTransitionEvent, "--"))),
		}, "  "))
		if isRollingStrategyMode(rec.StrategyMode) || isRollingStrategyMode(plan.StrategyMode) {
			lines = append(lines, strings.Join([]string{
				renderField("Review 次数", toneStyle("accent").Render(fmt.Sprintf("%d", rec.ReviewCount))),
				renderField("最近 Review", renderTimeValue(rec.LastReviewAtMs, "accent")),
				renderField("下次 Review", renderTimeValue(rec.NextReviewTimeMs, "accent")),
				renderField("当前 Boundary", renderTimeValue(rec.CurrentSyncBoundaryMs, "accent")),
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

func (m Model) renderCrossExchangeExecutionRecordDetail(rec entity.ExecutionRecord, plan entity.ExecutionPlan, hasPlan bool, width int) string {
	targetCloseMs := rec.TargetCloseTimeMs
	if targetCloseMs <= 0 && hasPlan {
		targetCloseMs = plan.TargetCloseTimeMs
	}
	lines := []string{
		fmt.Sprintf("%s  %s", ui.accent.Render(rec.Symbol), ui.subtle.Render(opportunityDirectionDisplay(entity.Opportunity{LongExchange: rec.LongExchange, ShortExchange: rec.ShortExchange}, service.ArbitrageModeCrossExchange))),
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

func (m Model) renderSameExchangeExecutionRecordDetail(rec entity.ExecutionRecord, plan entity.ExecutionPlan, hasPlan bool, width int) string {
	targetCloseMs := rec.TargetCloseTimeMs
	if targetCloseMs <= 0 && hasPlan {
		targetCloseMs = plan.TargetCloseTimeMs
	}
	planKey := rec.PlanKey
	if hasPlan && strings.TrimSpace(plan.PlanKey) != "" {
		planKey = plan.PlanKey
	}
	liveRisk := service.SameExchangeLiveRiskInspection{}
	if positionItem, ok := m.livePositionByPlanKey(rec.PlanKey); ok {
		liveRisk = positionItem.Risk
	}
	lines := []string{
		fmt.Sprintf("%s  %s", ui.accent.Render(rec.Symbol), ui.subtle.Render("现货多 / 永续空")),
		strings.Join([]string{
			renderField("计划", toneStyle("accent").Render(planKey)),
			renderField("状态", renderStatusValue(rec.Status)),
			renderField("实盘", renderBoolValue(rec.LiveTrading, true)),
			renderField("自动平仓", renderBoolValue(rec.AutoClose, false)),
			renderField("模式", toneStyle("accent").Render(arbitrageModeText(service.ArbitrageModeSameExchangeSpotPerp))),
		}, "  "),
		strings.Join([]string{
			renderField("占用名义", renderAllocatedNotionalValue(executionAllocatedNotional(rec, plan, hasPlan))),
			renderField("开仓时间", renderTimeValue(rec.OpenedAtMs, "accent")),
			renderField("平仓时间", renderTimeValue(rec.ClosedAtMs, "accent")),
			renderField("最近迁移", renderTimeValue(rec.LastTransitionAtMs, "accent")),
			renderField("事件", toneStyle("accent").Render(orDefault(rec.LastTransitionEvent, "--"))),
		}, "  "),
		strings.Join([]string{
			renderField("持有逻辑", toneStyle("accent").Render(sameExchangeHoldLogicText(m.data.System.Strategy))),
			renderField("计划持仓", toneStyle("accent").Render(executionPlannedHoldText(rec, plan, hasPlan))),
			renderField("目标平仓", renderTimeValue(targetCloseMs, "accent")),
			renderField("距计划平仓", renderRemainingTimeValue(targetCloseMs)),
			renderField("离场守则", toneStyle("accent").Render(sameExchangeExitRuleText(m.data.System.Strategy))),
		}, "  "),
		ui.panelTitle.Render("风险视角"),
	}
	lines = append(lines, renderSameExchangeRiskSummary(m.data.System.Strategy, nil, OpportunityListItem{}, plan, hasPlan, liveRisk)...)
	if hasPlan {
		basisModeText, basisCostText, basisCarryText, basisPaybackText, basisActionText, basisReasonText, basisThresholdText := sameExchangeBasisContext(nil, OpportunityListItem{}, plan, true, m.data.System.Strategy)
		lines = append(lines, strings.Join([]string{
			renderField("现货腿", toneStyle("good").Render(plan.LongExchange+" "+orDefault(plan.LongVenueSymbol, "--"))),
			renderField("永续腿", toneStyle("warn").Render(plan.ShortExchange+" "+orDefault(plan.ShortVenueSymbol, "--"))),
			renderField("永续横向排名", toneStyle("accent").Render(orDefault(fundingRankPlanText(plan), "--"))),
			renderField("历史正费率", toneStyle("accent").Render(orDefault(perpFundingHistoryPositivePlanText(plan), "--"))),
			renderField("历史负费率", toneStyle("accent").Render(orDefault(perpFundingHistoryNegativePlanText(plan), "--"))),
			renderField("历史分位", toneStyle("accent").Render(orDefault(perpFundingHistoricalPercentilePlanText(plan), "--"))),
		}, "  "))
		lines = append(lines, strings.Join([]string{
			renderField("现货-永续基差", renderBpsValue(plan.CrossVenueBasisBps, 2)),
			renderField("基差模型", toneStyle("accent").Render(orDefault(basisModeText, "--"))),
			renderField("基差阈值", toneStyle("accent").Render(orDefault(basisThresholdText, "--"))),
			renderField("基差成本", toneStyle("accent").Render(orDefault(basisCostText, "--"))),
			renderField("每轮 funding", toneStyle("accent").Render(orDefault(basisCarryText, "--"))),
			renderField("回本轮数", toneStyle("accent").Render(orDefault(basisPaybackText, "--"))),
			renderField("基差动作", toneStyle(sameExchangeBasisActionTone(basisActionText)).Render(orDefault(basisActionText, "--"))),
		}, "  "))
		lines = append(lines, strings.Join([]string{
			renderField("基差原因", toneStyle("accent").Render(orDefault(basisReasonText, "--"))),
			renderField("当前 Entry Path 终点", renderTimeValue(plan.ProjectedFundingTimeMs, "accent")),
			renderField("目标平仓", renderTimeValue(plan.TargetCloseTimeMs, "accent")),
		}, "  "))
	}
	lines = append(lines, strings.Join([]string{
		renderField("开仓单数", toneStyle("accent").Render(fmt.Sprintf("%d", rec.OpenOrderCount))),
		renderField("平仓单数", toneStyle("accent").Render(fmt.Sprintf("%d", rec.CloseOrderCount))),
	}, "  "))
	if isRollingStrategyMode(rec.StrategyMode) || (hasPlan && isRollingStrategyMode(plan.StrategyMode)) {
		lines = append(lines, strings.Join([]string{
			renderField("策略模式", toneStyle("accent").Render(strategyModeText(orDefault(rec.StrategyMode, plan.StrategyMode)))),
			renderField("Review 次数", toneStyle("accent").Render(fmt.Sprintf("%d", rec.ReviewCount))),
			renderField("下次 Review", renderTimeValue(rec.NextReviewTimeMs, "accent")),
			renderField("当前 Boundary", renderTimeValue(rec.CurrentSyncBoundaryMs, "accent")),
			renderField("最近 Review", renderTimeValue(rec.LastReviewAtMs, "accent")),
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

func (m Model) renderExecutionPlanListBlock(item entity.ExecutionPlan, arbitrageMode string, marker string) string {
	if isSameExchangeArbitrageMode(arbitrageMode) {
		return strings.Join([]string{
			fmt.Sprintf("%s %-7s %-25s %s", marker, clip(item.Symbol, 7), clip(opportunityDirectionDisplay(entity.Opportunity{LongExchange: item.LongExchange, ShortExchange: item.ShortExchange}, service.ArbitrageModeSameExchangeSpotPerp), 25), alignRightValue(renderMoneyValue(item.NetExpectedPNL, 3), 10)),
			fmt.Sprintf("    %-22s 就绪 %-3s 状态 %-18s", clip(opportunityPairDisplay(entity.Opportunity{LongExchange: item.LongExchange, ShortExchange: item.ShortExchange}, service.ArbitrageModeSameExchangeSpotPerp), 22), boolWord(item.ReadyNow), clip(statusText(item.Status), 18)),
			fmt.Sprintf("    排名 %-10s 历史+ %-12s 历史- %-12s", clip(orDefault(fundingRankPlanText(item), "--"), 10), clip(orDefault(perpFundingHistoryPositivePlanText(item), "--"), 12), clip(orDefault(perpFundingHistoryNegativePlanText(item), "--"), 12)),
		}, "\n")
	}
	return strings.Join([]string{
		fmt.Sprintf("%s %-7s %-25s %s", marker, clip(item.Symbol, 7), clip(opportunityDirectionDisplay(entity.Opportunity{LongExchange: item.LongExchange, ShortExchange: item.ShortExchange}, service.ArbitrageModeCrossExchange), 25), alignRightValue(renderMoneyValue(item.NetExpectedPNL, 3), 10)),
		fmt.Sprintf("    %-22s 就绪 %-3s 状态 %-18s", clip(opportunityPairDisplay(entity.Opportunity{LongExchange: item.LongExchange, ShortExchange: item.ShortExchange}, service.ArbitrageModeCrossExchange), 22), boolWord(item.ReadyNow), clip(statusText(item.Status), 18)),
		fmt.Sprintf("    计划 %-16s 名义 %-12s", clip(item.PlanKey, 16), clip(fmtMoney(targetNotional(item), 2), 12)),
	}, "\n")
}

func (m Model) renderExecutionRecordListBlock(item entity.ExecutionRecord, arbitrageMode string, marker string, allocatedText string) string {
	if isSameExchangeArbitrageMode(arbitrageMode) {
		plan, hasPlan := m.planByKey(item.PlanKey)
		rankText, posText, negText := "--", "--", "--"
		if hasPlan {
			if value := fundingRankPlanText(plan); value != "" {
				rankText = value
			}
			if value := perpFundingHistoryPositivePlanText(plan); value != "" {
				posText = value
			}
			if value := perpFundingHistoryNegativePlanText(plan); value != "" {
				negText = value
			}
		}
		return strings.Join([]string{
			fmt.Sprintf("%s %-7s %-25s %s", marker, clip(item.Symbol, 7), clip(opportunityDirectionDisplay(entity.Opportunity{LongExchange: item.LongExchange, ShortExchange: item.ShortExchange}, service.ArbitrageModeSameExchangeSpotPerp), 25), statusText(item.Status)),
			fmt.Sprintf("    计划 %-24s 实盘 %-3s 自动平仓 %-3s", clip(item.PlanKey, 24), boolWord(item.LiveTrading), boolWord(item.AutoClose)),
			fmt.Sprintf("    占用 %-12s 排名 %-8s 历史+ %-10s 历史- %-10s", clip(allocatedText, 12), clip(rankText, 8), clip(posText, 10), clip(negText, 10)),
		}, "\n")
	}
	return strings.Join([]string{
		fmt.Sprintf("%s %-7s %-25s %s", marker, clip(item.Symbol, 7), clip(opportunityDirectionDisplay(entity.Opportunity{LongExchange: item.LongExchange, ShortExchange: item.ShortExchange}, service.ArbitrageModeCrossExchange), 25), statusText(item.Status)),
		fmt.Sprintf("    计划 %-28s 实盘 %-3s 自动平仓 %-3s", clip(item.PlanKey, 28), boolWord(item.LiveTrading), boolWord(item.AutoClose)),
		fmt.Sprintf("    占用 %-14s 开仓 %s  平仓 %s", clip(allocatedText, 14), clip(fmtTime(item.OpenedAtMs), 19), clip(fmtTime(item.ClosedAtMs), 19)),
	}, "\n")
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
	if rank := fundingRankText(rule); rank != "--" {
		parts = append(parts, "横向排名="+rank)
	}
	if history := fundingHistoryPositiveText(rule); history != "--" {
		parts = append(parts, "历史正费率="+history)
	}
	if history := fundingHistoryNegativeText(rule); history != "--" {
		parts = append(parts, "历史负费率="+history)
	}
	if percentile := fundingHistoricalPercentileText(rule); percentile != "--" {
		parts = append(parts, "历史分位="+percentile)
	}
	if historyMean := fundingHistoryMeanText(rule); historyMean != "--" {
		parts = append(parts, "历史均值="+historyMean)
	}
	if support := fundingHistoricalSupportText(rule); support != "--" {
		parts = append(parts, "同向支持="+support)
	}
	if annualizedNet := fundingAnnualizedNetText(rule); annualizedNet != "--" {
		parts = append(parts, "粗年化净="+annualizedNet)
	}
	if decision := sameExchangeLongHoldRuleText(rule); decision != "--" {
		parts = append(parts, "长持="+decision)
	}
	if strings.TrimSpace(rule.MetadataSummary) != "" {
		parts = append(parts, rule.MetadataSummary)
	}
	return strings.Join(parts, " | ")
}

func fundingRankPlanText(plan entity.ExecutionPlan) string {
	if plan.PerpFundingRank <= 0 || plan.PerpFundingRankTotal <= 0 {
		return ""
	}
	return fmt.Sprintf("#%d/%d", plan.PerpFundingRank, plan.PerpFundingRankTotal)
}

func perpFundingHistoryPositivePlanText(plan entity.ExecutionPlan) string {
	if plan.PerpFundingHistorySampleCount <= 0 {
		return ""
	}
	return fmt.Sprintf("%.1f%% / %d", plan.PerpFundingHistoryPositiveRatio*100, plan.PerpFundingHistorySampleCount)
}

func perpFundingHistoryNegativePlanText(plan entity.ExecutionPlan) string {
	if plan.PerpFundingHistorySampleCount <= 0 {
		return ""
	}
	return fmt.Sprintf("%.1f%% / %d", plan.PerpFundingHistoryNegativeRatio*100, plan.PerpFundingHistorySampleCount)
}

func perpFundingHistoricalPercentilePlanText(plan entity.ExecutionPlan) string {
	if plan.PerpFundingHistorySampleCount <= 0 {
		return ""
	}
	return fmt.Sprintf("%.1f%%", plan.PerpFundingCurrentHistoricalPercentile*100)
}

func perpFundingHistoryMeanPlanText(plan entity.ExecutionPlan) string {
	if plan.PerpFundingHistorySampleCount <= 0 {
		return ""
	}
	return fmtPctRatio(plan.PerpFundingHistoryMeanRate, 5)
}

func perpFundingSupportPlanText(plan entity.ExecutionPlan) string {
	if plan.PerpFundingHistorySampleCount <= 0 {
		return ""
	}
	return fmt.Sprintf("%.1f%% / %d", plan.PerpFundingHistoricalSupportRatio*100, plan.PerpFundingHistorySampleCount)
}

func perpFundingAnnualizedCarryPlanText(plan entity.ExecutionPlan) string {
	if plan.PerpFundingHistorySampleCount <= 0 {
		return ""
	}
	return fmtPctRatio(plan.PerpFundingEstimatedAnnualizedCarryRate, 2)
}

func perpFundingAnnualizedNetPlanText(plan entity.ExecutionPlan) string {
	if plan.PerpFundingHistorySampleCount <= 0 {
		return ""
	}
	return fmtPctRatio(plan.PerpFundingEstimatedAnnualizedNetRate, 2)
}

func perpFundingEstimatedEventRatePlanText(plan entity.ExecutionPlan) string {
	if plan.PerpFundingHistorySampleCount <= 0 || plan.PerpFundingEstimatedEventRate == 0 {
		return ""
	}
	return fmtPctRatio(plan.PerpFundingEstimatedEventRate, 5)
}

func perpFundingSuggestedHoldEventsPlanText(plan entity.ExecutionPlan) string {
	if plan.SameExchangeLongHoldSuggestedFundingEvents <= 0 {
		return ""
	}
	return fmt.Sprintf("%d 轮", plan.SameExchangeLongHoldSuggestedFundingEvents)
}

func perpFundingSuggestedHoldDurationPlanText(plan entity.ExecutionPlan) string {
	if plan.SameExchangeLongHoldSuggestedHoldHours <= 0 {
		return ""
	}
	return formatHoldHours(plan.SameExchangeLongHoldSuggestedHoldHours)
}

func perpFundingSuggestedGrossPNLPlanText(plan entity.ExecutionPlan) string {
	if plan.SameExchangeLongHoldSuggestedFundingEvents <= 0 {
		return ""
	}
	return renderMoneyValue(plan.SameExchangeLongHoldSuggestedGrossFundingPNL, 3)
}

func perpFundingSuggestedNetPNLPlanText(plan entity.ExecutionPlan) string {
	if plan.SameExchangeLongHoldSuggestedFundingEvents <= 0 {
		return ""
	}
	return renderMoneyValue(plan.SameExchangeLongHoldSuggestedNetPNL, 3)
}

func sameExchangeLongHoldPlanText(plan entity.ExecutionPlan, strategy StrategyStatus) string {
	if plan.PerpFundingHistorySampleCount <= 0 && strings.TrimSpace(plan.SameExchangeLongHoldReason) == "" {
		return ""
	}
	if plan.SameExchangeLongHoldEligible {
		return "适合长期持有"
	}
	if strategy.SameExchangeRequireLongHoldEligible {
		return "不建议开仓"
	}
	return "不建议长期持有"
}

func sameExchangeLongHoldReasonPlanText(plan entity.ExecutionPlan, strategy StrategyStatus) string {
	if plan.PerpFundingHistorySampleCount <= 0 && strings.TrimSpace(plan.SameExchangeLongHoldReason) == "" {
		return ""
	}
	return sameExchangeLongHoldReasonLabel(plan.SameExchangeLongHoldReason, strategy)
}

func sameExchangeFundingContext(detail *entity.Opportunity, plan entity.ExecutionPlan, hasPlan bool) (string, string, string, string) {
	rule := sameExchangePerpRule(detail)
	rankText := ""
	if value := fundingRankText(rule); value != "--" {
		rankText = value
	} else if hasPlan {
		rankText = fundingRankPlanText(plan)
	}

	positiveText := ""
	if value := fundingHistoryPositiveText(rule); value != "--" {
		positiveText = value
	} else if hasPlan {
		positiveText = perpFundingHistoryPositivePlanText(plan)
	}

	negativeText := ""
	if value := fundingHistoryNegativeText(rule); value != "--" {
		negativeText = value
	} else if hasPlan {
		negativeText = perpFundingHistoryNegativePlanText(plan)
	}

	percentileText := ""
	if value := fundingHistoricalPercentileText(rule); value != "--" {
		percentileText = value
	} else if hasPlan {
		percentileText = perpFundingHistoricalPercentilePlanText(plan)
	}
	return rankText, positiveText, negativeText, percentileText
}

func sameExchangeLongHoldContext(detail *entity.Opportunity, plan entity.ExecutionPlan, hasPlan bool, strategy StrategyStatus) (string, string, string, string, string, string) {
	rule := sameExchangePerpRule(detail)
	historyMeanText := ""
	if value := fundingHistoryMeanText(rule); value != "--" {
		historyMeanText = value
	} else if hasPlan {
		historyMeanText = perpFundingHistoryMeanPlanText(plan)
	}

	supportText := ""
	if value := fundingHistoricalSupportText(rule); value != "--" {
		supportText = value
	} else if hasPlan {
		supportText = perpFundingSupportPlanText(plan)
	}

	annualizedCarryText := ""
	if value := fundingAnnualizedCarryText(rule); value != "--" {
		annualizedCarryText = value
	} else if hasPlan {
		annualizedCarryText = perpFundingAnnualizedCarryPlanText(plan)
	}

	annualizedNetText := ""
	if value := fundingAnnualizedNetText(rule); value != "--" {
		annualizedNetText = value
	} else if hasPlan {
		annualizedNetText = perpFundingAnnualizedNetPlanText(plan)
	}

	longHoldText := ""
	if value := sameExchangeLongHoldRuleDisplay(rule, strategy); value != "--" {
		longHoldText = value
	} else if hasPlan {
		longHoldText = sameExchangeLongHoldPlanText(plan, strategy)
	}

	longHoldReasonText := ""
	if value := sameExchangeLongHoldRuleReasonText(rule, strategy); value != "--" {
		longHoldReasonText = value
	} else if hasPlan {
		longHoldReasonText = sameExchangeLongHoldReasonPlanText(plan, strategy)
	}

	return historyMeanText, supportText, annualizedCarryText, annualizedNetText, longHoldText, longHoldReasonText
}

func sameExchangeLongHoldRecommendationContext(detail *entity.Opportunity, plan entity.ExecutionPlan, hasPlan bool) (string, string, string, string, string, string) {
	rule := sameExchangePerpRule(detail)

	eventRateText := ""
	if value := fundingEstimatedEventRateText(rule); value != "--" {
		eventRateText = value
	} else if hasPlan {
		eventRateText = perpFundingEstimatedEventRatePlanText(plan)
	}

	holdEventsText := ""
	if value := fundingSuggestedHoldEventsText(rule); value != "--" {
		holdEventsText = value
	} else if hasPlan {
		holdEventsText = perpFundingSuggestedHoldEventsPlanText(plan)
	}

	holdDurationText := ""
	if value := fundingSuggestedHoldDurationText(rule); value != "--" {
		holdDurationText = value
	} else if hasPlan {
		holdDurationText = perpFundingSuggestedHoldDurationPlanText(plan)
	}

	suggestedGrossText := ""
	if value := fundingSuggestedGrossPNLText(rule); value != "--" {
		suggestedGrossText = value
	} else if hasPlan {
		suggestedGrossText = perpFundingSuggestedGrossPNLPlanText(plan)
	}

	suggestedNetText := ""
	if value := fundingSuggestedNetPNLText(rule); value != "--" {
		suggestedNetText = value
	} else if hasPlan {
		suggestedNetText = perpFundingSuggestedNetPNLPlanText(plan)
	}

	sourceText := sameExchangeLongHoldEstimateSourceText(detail, plan, hasPlan)
	return eventRateText, holdEventsText, holdDurationText, suggestedGrossText, suggestedNetText, sourceText
}

func sameExchangeBasisContext(detail *entity.Opportunity, item OpportunityListItem, plan entity.ExecutionPlan, hasPlan bool, strategy StrategyStatus) (string, string, string, string, string, string, string) {
	usesPayback := false
	costBps := 0.0
	carryPerEventBps := 0.0
	paybackEvents := 0.0
	allowed := true
	reason := ""
	sizeMultiplier := 0.0

	switch {
	case detail != nil && sameExchangeBasisDataPresent(detail.SameExchangeBasisReason, detail.SameExchangeBasisUsesPaybackModel, detail.SameExchangeBasisCostBps, detail.SameExchangeBasisCarryPerEventBps, detail.SameExchangeBasisPaybackFundingEvents, detail.SameExchangeBasisRiskSizeMultiplier):
		usesPayback = detail.SameExchangeBasisUsesPaybackModel
		costBps = detail.SameExchangeBasisCostBps
		carryPerEventBps = detail.SameExchangeBasisCarryPerEventBps
		paybackEvents = detail.SameExchangeBasisPaybackFundingEvents
		allowed = detail.SameExchangeBasisAllowed
		reason = detail.SameExchangeBasisReason
		sizeMultiplier = detail.SameExchangeBasisRiskSizeMultiplier
	case sameExchangeBasisDataPresent(item.SameExchangeBasisReason, item.SameExchangeBasisUsesPaybackModel, item.SameExchangeBasisCostBps, item.SameExchangeBasisCarryPerEventBps, item.SameExchangeBasisPaybackFundingEvents, item.SameExchangeBasisRiskSizeMultiplier):
		usesPayback = item.SameExchangeBasisUsesPaybackModel
		costBps = item.SameExchangeBasisCostBps
		carryPerEventBps = item.SameExchangeBasisCarryPerEventBps
		paybackEvents = item.SameExchangeBasisPaybackFundingEvents
		allowed = item.SameExchangeBasisAllowed
		reason = item.SameExchangeBasisReason
		sizeMultiplier = item.SameExchangeBasisRiskSizeMultiplier
	case hasPlan && sameExchangeBasisDataPresent(plan.SameExchangeBasisReason, plan.SameExchangeBasisUsesPaybackModel, plan.SameExchangeBasisCostBps, plan.SameExchangeBasisCarryPerEventBps, plan.SameExchangeBasisPaybackFundingEvents, plan.SameExchangeBasisRiskSizeMultiplier):
		usesPayback = plan.SameExchangeBasisUsesPaybackModel
		costBps = plan.SameExchangeBasisCostBps
		carryPerEventBps = plan.SameExchangeBasisCarryPerEventBps
		paybackEvents = plan.SameExchangeBasisPaybackFundingEvents
		allowed = plan.SameExchangeBasisAllowed
		reason = plan.SameExchangeBasisReason
		sizeMultiplier = plan.SameExchangeBasisRiskSizeMultiplier
	default:
		return "--", "--", "--", "--", "--", "--", "--"
	}

	modeText := "短持硬阈值"
	maxAllowedBasisBps := item.MaxAllowedBasisBps
	if maxAllowedBasisBps <= 0 && hasPlan {
		maxAllowedBasisBps = strategyAllowedBasisThresholdBps(strategy, plan.FundingWindowHours)
	}
	thresholdText := fmtSignedBps(maxAllowedBasisBps, 2)
	if usesPayback {
		modeText = "长持回本轮数"
		thresholdText = fmt.Sprintf("max %.2f 轮 / extreme %.2f 轮", strategy.SameExchangeMaxBasisPaybackEvents, strategy.SameExchangeExtremeBasisPaybackEvents)
	}

	costText := "--"
	if math.Abs(costBps) > 1e-9 {
		costText = fmtSignedBps(costBps, 2)
	}
	carryText := "--"
	if math.Abs(carryPerEventBps) > 1e-9 {
		carryText = fmtSignedBps(carryPerEventBps, 2)
	}
	paybackText := "--"
	if paybackEvents > 0 {
		paybackText = fmt.Sprintf("%.2f 轮", paybackEvents)
	}
	actionText := sameExchangeBasisActionLabel(usesPayback, allowed, sizeMultiplier, reason, strategy)
	reasonText := sameExchangeBasisReasonLabel(reason, strategy)
	return modeText, costText, carryText, paybackText, actionText, reasonText, thresholdText
}

func strategyAllowedBasisThresholdBps(strategy StrategyStatus, fundingWindowHours float64) float64 {
	base := strategy.MaxSpreadBps
	if base <= 0 {
		base = 12
	}
	multiplier := strategy.DynamicMaxSpreadMultiplier
	if multiplier < 1 {
		multiplier = 1
	}
	refHours := strategy.DynamicMaxSpreadReferenceHours
	if refHours <= 0 {
		refHours = strategy.HoldHours
	}
	if refHours <= 0 {
		return base
	}
	ratio := fundingWindowHours / refHours
	if ratio > 1 {
		ratio = 1
	}
	if ratio < 0 {
		ratio = 0
	}
	return base * (1 + (multiplier-1)*ratio)
}

func sameExchangeBasisDataPresent(reason string, usesPayback bool, costBps, carryPerEventBps, paybackEvents, sizeMultiplier float64) bool {
	return strings.TrimSpace(reason) != "" ||
		usesPayback ||
		math.Abs(costBps) > 1e-9 ||
		math.Abs(carryPerEventBps) > 1e-9 ||
		math.Abs(paybackEvents) > 1e-9 ||
		sizeMultiplier > 0
}

func sameExchangeBasisReasonLabel(reason string, strategy StrategyStatus) string {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "", "eligible":
		return "基差成本可以被 funding 覆盖"
	case "short_window_too_wide":
		return "短持窗口下基差偏宽，优先缩仓而非直接拒绝"
	case "payback_carry_missing":
		return "每轮 funding 毛收益 <= 0，无法覆盖基差"
	case "payback_too_high":
		return fmt.Sprintf("回本轮数 > %.2f，收益回收偏慢", strategy.SameExchangeMaxBasisPaybackEvents)
	case "extreme_payback":
		return fmt.Sprintf("回本轮数 > %.2f，先降仓", strategy.SameExchangeExtremeBasisPaybackEvents)
	default:
		return reason
	}
}

func sameExchangeBasisActionLabel(usesPayback bool, allowed bool, sizeMultiplier float64, reason string, strategy StrategyStatus) string {
	if !usesPayback {
		if strings.EqualFold(strings.TrimSpace(reason), "short_window_too_wide") {
			if sizeMultiplier > 0 && sizeMultiplier < 1 {
				return fmt.Sprintf("短持基差偏宽，降仓至 %.0f%%", sizeMultiplier*100)
			}
			return "短持基差偏宽，谨慎开仓"
		}
		if !allowed {
			return "短持基差超限"
		}
		return "短持基差通过"
	}
	if !allowed {
		switch strings.ToLower(strings.TrimSpace(reason)) {
		case "payback_carry_missing":
			return "每轮 funding 不足，拒绝开仓"
		case "payback_too_high":
			return fmt.Sprintf("回本 > %.2f 轮，拒绝开仓", strategy.SameExchangeMaxBasisPaybackEvents)
		default:
			return "回本轮数超限"
		}
	}
	if strings.EqualFold(strings.TrimSpace(reason), "payback_carry_missing") {
		return "每轮 funding 不足，谨慎开仓"
	}
	if strings.EqualFold(strings.TrimSpace(reason), "payback_too_high") {
		if sizeMultiplier > 0 && sizeMultiplier < 1 {
			return fmt.Sprintf("回本偏慢，降仓至 %.0f%%", sizeMultiplier*100)
		}
		return fmt.Sprintf("回本 > %.2f 轮，谨慎开仓", strategy.SameExchangeMaxBasisPaybackEvents)
	}
	if sizeMultiplier > 0 && sizeMultiplier < 1 {
		return fmt.Sprintf("极端基差，降仓至 %.0f%%", sizeMultiplier*100)
	}
	return "回本轮数可覆盖"
}

func sameExchangeBasisActionTone(action string) string {
	switch {
	case strings.Contains(action, "拒绝"), strings.Contains(action, "超限"):
		return "bad"
	case strings.Contains(action, "降仓"), strings.Contains(action, "谨慎"):
		return "warn"
	case action == "", action == "--":
		return "subtle"
	default:
		return "good"
	}
}

func sameExchangeFundingSampleText(detail *entity.Opportunity, plan entity.ExecutionPlan, hasPlan bool, loading bool) string {
	samples := 0
	if detail != nil && detail.ShortFundingRule.HistorySampleCount > 0 {
		samples = detail.ShortFundingRule.HistorySampleCount
	} else if hasPlan {
		samples = plan.PerpFundingHistorySampleCount
	}
	if samples > 0 {
		return fmt.Sprintf("%d 个周期", samples)
	}
	if loading {
		return "加载中"
	}
	return "--"
}

func (m Model) renderCrossExchangeFundingHistoryDetail(detail *entity.Opportunity, loading bool, width int) string {
	if detail == nil {
		if loading {
			return ui.subtle.Render("正在加载历史 funding 明细...")
		}
		return ui.subtle.Render("当前机会暂无历史 funding 明细。")
	}
	return strings.Join([]string{
		renderFundingHistorySeriesBlock("多头腿最近 funding", detail.LongFundingRule, width),
		"",
		renderFundingHistorySeriesBlock("空头腿最近 funding", detail.ShortFundingRule, width),
	}, "\n")
}

func (m Model) renderSameExchangeFundingHistoryDetail(detail *entity.Opportunity, loading bool, width int) string {
	if detail == nil {
		if loading {
			return ui.subtle.Render("正在加载历史 funding 明细...")
		}
		return ui.subtle.Render("当前机会暂无历史 funding 明细。")
	}
	return strings.Join([]string{
		renderSpotFundingHistorySeriesBlock("现货腿最近 funding", detail.LongFundingRule, sameExchangePerpRule(detail), width),
		"",
		renderFundingHistorySeriesBlock("永续腿最近 funding", sameExchangePerpRule(detail), width),
	}, "\n")
}

func renderSpotFundingHistorySeriesBlock(title string, spotRule entity.OpportunityFundingRule, perpRule entity.OpportunityFundingRule, width int) string {
	lines := []string{
		toneStyle("accent").Render(title),
		clip(strings.Join([]string{
			renderField("展示语义", toneStyle("accent").Render("现货锚定腿 / synthetic zero funding")),
			renderField("交易所 / 合约", toneStyle("accent").Render(orDefault(spotRule.Exchange, "--")+" / "+orDefault(spotRule.VenueSymbol, "--"))),
			renderField("收益锚点", renderTimeValue(perpRule.NextFundingTimeMs, "accent")),
		}, "  "), width),
		ui.subtle.Render("现货腿不直接产生 funding 事件；这里保留 0 funding 锚定语义，方便把收益兑现点与永续腿结算时间对齐。"),
	}
	return strings.Join(lines, "\n")
}

func renderFundingHistorySeriesBlock(title string, rule entity.OpportunityFundingRule, width int) string {
	lines := []string{toneStyle("accent").Render(title)}
	if isSpotFundingRule(rule) {
		lines = append(lines, ui.subtle.Render("该腿为现货锚定腿，不直接产生历史 funding 事件。"))
		return strings.Join(lines, "\n")
	}

	series := displayedFundingHistorySeries(rule)
	sampleCount := rule.HistorySampleCount
	if sampleCount < len(series) {
		sampleCount = len(series)
	}
	if len(series) == 0 {
		if sampleCount > 0 {
			lines = append(lines, ui.subtle.Render("历史 funding 画像已就绪，但最近 funding 明细序列暂未同步完成。"))
		} else {
			lines = append(lines, ui.subtle.Render("暂无历史 funding 明细。"))
		}
		return strings.Join(lines, "\n")
	}

	lines = append(lines, clip(strings.Join([]string{
		renderField("交易所 / 合约", toneStyle("accent").Render(orDefault(rule.Exchange, "--")+" / "+orDefault(rule.VenueSymbol, "--"))),
		renderField("最近明细", toneStyle("accent").Render(fmt.Sprintf("%d 条", len(series)))),
		renderField("历史正费率", toneStyle("accent").Render(orDefault(fundingHistoryPositiveText(rule), "--"))),
		renderField("历史负费率", toneStyle("accent").Render(orDefault(fundingHistoryNegativeText(rule), "--"))),
	}, "  "), width))
	lines = append(lines, ui.subtle.Render("时间                 费率           标记价"))
	for _, point := range series {
		markPrice := "--"
		if point.MarkPrice > 0 {
			markPrice = priceText(point.MarkPrice)
		}
		line := fmt.Sprintf("%-20s %-14s %-12s",
			clip(fmtTime(point.FundingTimeMs), 20),
			fmtPctRatio(point.FundingRate, 5),
			markPrice,
		)
		lines = append(lines, toneStyle(signedNumberTone(point.FundingRate)).Render(clip(line, width)))
	}
	return strings.Join(lines, "\n")
}

func displayedFundingHistorySeries(rule entity.OpportunityFundingRule) []entity.OpportunityFundingHistoryPoint {
	if len(rule.HistorySeries) == 0 {
		return nil
	}
	items := append([]entity.OpportunityFundingHistoryPoint(nil), rule.HistorySeries...)
	sort.Slice(items, func(i, j int) bool {
		return items[i].FundingTimeMs > items[j].FundingTimeMs
	})
	if len(items) > 8 {
		items = items[:8]
	}
	return items
}

func isSpotFundingRule(rule entity.OpportunityFundingRule) bool {
	return strings.EqualFold(strings.TrimSpace(rule.ClampSource), "spot_synthetic_zero")
}

func sameExchangeCurrentFundingRate(item any) float64 {
	switch v := item.(type) {
	case entity.Opportunity:
		return v.ShortFundingRate
	case OpportunityListItem:
		return v.ShortFundingRate
	default:
		return 0
	}
}

func sameExchangeWindowGrossPNL(item any) float64 {
	switch v := item.(type) {
	case entity.Opportunity:
		return v.GrossFundingPNL
	case OpportunityListItem:
		return v.GrossFundingPNL
	default:
		return 0
	}
}

func sameExchangeTotalFrictionPNL(item any) float64 {
	switch v := item.(type) {
	case entity.Opportunity:
		return v.EntryFeePNL + v.ExitFeePNL + v.SlippagePNL + v.SafetyBufferPNL
	case OpportunityListItem:
		return v.EntryFeePNL + v.ExitFeePNL + v.SlippagePNL + v.SafetyBufferPNL
	default:
		return 0
	}
}

func sameExchangeNextFundingGrossPNL(item any, plan entity.ExecutionPlan, hasPlan bool, strategy StrategyStatus) float64 {
	rate := sameExchangeCurrentFundingRate(item)
	if math.Abs(rate) <= 1e-9 {
		return 0
	}
	notional := opportunityTargetNotional(rate, sameExchangeWindowGrossPNL(item), plan, hasPlan, strategy)
	if notional <= 0 {
		return 0
	}
	return notional * rate
}

func sameExchangePerpRule(detail *entity.Opportunity) entity.OpportunityFundingRule {
	if detail == nil {
		return entity.OpportunityFundingRule{}
	}
	return detail.ShortFundingRule
}

func sameExchangeHoldLogicText(strategy StrategyStatus) string {
	if strings.EqualFold(strings.TrimSpace(strategy.HoldSelectionMode), "dynamic_profit") {
		return "收益驱动动态持有"
	}
	return holdSelectionModeText(strategy.HoldSelectionMode)
}

func sameExchangeExitRuleText(strategy StrategyStatus) string {
	parts := make([]string, 0, 3)
	if strategy.SameExchangeCloseOnNegativeFunding {
		parts = append(parts, "永续 funding 转负即复核")
	}
	if strategy.SameExchangeHistoryNegativeExitThreshold > 0 {
		parts = append(parts, fmt.Sprintf("历史负费率>=%.1f%%", strategy.SameExchangeHistoryNegativeExitThreshold*100))
	}
	if strategy.SameExchangeExitRequirePositiveClosePNL {
		parts = append(parts, "平仓净收益>="+fmtMoney(strategy.SameExchangeExitMinClosePNL, 3))
	} else {
		parts = append(parts, "允许收益回撤离场")
	}
	return strings.Join(parts, " / ")
}

func fundingHistoryMeanText(rule entity.OpportunityFundingRule) string {
	if rule.HistorySampleCount <= 0 {
		return "--"
	}
	return fmtPctRatio(rule.HistoryMeanRate, 5)
}

func fundingHistoricalSupportText(rule entity.OpportunityFundingRule) string {
	if rule.HistorySampleCount <= 0 {
		return "--"
	}
	return fmt.Sprintf("%.1f%% / %d", rule.HistoricalSupportRatio*100, rule.HistorySampleCount)
}

func fundingAnnualizedCarryText(rule entity.OpportunityFundingRule) string {
	if rule.HistorySampleCount <= 0 {
		return "--"
	}
	return fmtPctRatio(rule.EstimatedAnnualizedCarryRate, 2)
}

func fundingAnnualizedNetText(rule entity.OpportunityFundingRule) string {
	if rule.HistorySampleCount <= 0 {
		return "--"
	}
	return fmtPctRatio(rule.EstimatedAnnualizedNetRate, 2)
}

func fundingEstimatedEventRateText(rule entity.OpportunityFundingRule) string {
	if rule.HistorySampleCount <= 0 || rule.EstimatedEventRate == 0 {
		return "--"
	}
	return fmtPctRatio(rule.EstimatedEventRate, 5)
}

func fundingSuggestedHoldEventsText(rule entity.OpportunityFundingRule) string {
	if rule.SuggestedFundingEvents <= 0 {
		return "--"
	}
	return fmt.Sprintf("%d 轮", rule.SuggestedFundingEvents)
}

func fundingSuggestedHoldDurationText(rule entity.OpportunityFundingRule) string {
	if rule.SuggestedHoldHours <= 0 {
		return "--"
	}
	return formatHoldHours(rule.SuggestedHoldHours)
}

func fundingSuggestedGrossPNLText(rule entity.OpportunityFundingRule) string {
	if rule.SuggestedFundingEvents <= 0 {
		return "--"
	}
	return renderMoneyValue(rule.SuggestedGrossFundingPNL, 3)
}

func fundingSuggestedNetPNLText(rule entity.OpportunityFundingRule) string {
	if rule.SuggestedFundingEvents <= 0 {
		return "--"
	}
	return renderMoneyValue(rule.SuggestedNetPNL, 3)
}

func sameExchangeLongHoldRuleText(rule entity.OpportunityFundingRule) string {
	if rule.HistorySampleCount <= 0 && strings.TrimSpace(rule.LongHoldReason) == "" {
		return "--"
	}
	if rule.LongHoldEligible {
		return "适合长期持有"
	}
	return "不建议长期持有"
}

func sameExchangeLongHoldRuleDisplay(rule entity.OpportunityFundingRule, strategy StrategyStatus) string {
	if rule.HistorySampleCount <= 0 && strings.TrimSpace(rule.LongHoldReason) == "" {
		return "--"
	}
	if rule.LongHoldEligible {
		return "适合长期持有"
	}
	if strategy.SameExchangeRequireLongHoldEligible {
		return "不建议开仓"
	}
	return "不建议长期持有"
}

func sameExchangeLongHoldRuleReasonText(rule entity.OpportunityFundingRule, strategy StrategyStatus) string {
	if rule.HistorySampleCount <= 0 && strings.TrimSpace(rule.LongHoldReason) == "" {
		return "--"
	}
	return sameExchangeLongHoldReasonLabel(rule.LongHoldReason, strategy)
}

func sameExchangeLongHoldReasonLabel(reason string, strategy StrategyStatus) string {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "", "eligible":
		return "满足长期持有门槛"
	case "funding_not_positive":
		return "当前永续 funding <= 0"
	case "history_samples_low":
		return fmt.Sprintf("历史样本 < %d", strategy.SameExchangeMinHistorySampleCount)
	case "support_ratio_low":
		return fmt.Sprintf("同向历史支持 < %.1f%%", strategy.SameExchangeMinHistoricalSupportRatio*100)
	case "annualized_net_rate_low":
		return fmt.Sprintf("粗年化净收益 < %.1f%%", strategy.SameExchangeMinAnnualizedNetRate*100)
	case "projected_net_pnl_low":
		return "建议持有期净收益仍不足"
	case "missing_funding_interval":
		return "缺少 funding 间隔"
	case "missing_notional":
		return "缺少可用名义"
	default:
		return reason
	}
}

func sameExchangeLongHoldTone(label string) string {
	switch strings.TrimSpace(label) {
	case "适合长期持有":
		return "good"
	case "", "--":
		return "subtle"
	default:
		return "warn"
	}
}

func sameExchangeLongHoldEstimateSourceText(detail *entity.Opportunity, plan entity.ExecutionPlan, hasPlan bool) string {
	if detail != nil && detail.SameExchangeLongHoldUsingHistoryEstimate {
		return "已切换到历史长持预估"
	}
	if hasPlan && plan.SameExchangeLongHoldUsingHistoryEstimate {
		return "已切换到历史长持预估"
	}
	rule := sameExchangePerpRule(detail)
	if rule.SuggestedFundingEvents > 0 {
		return "历史长持建议可用"
	}
	if hasPlan && plan.SameExchangeLongHoldSuggestedFundingEvents > 0 {
		return "历史长持建议可用"
	}
	return ""
}

func formatHoldHours(hours float64) string {
	if hours <= 0 {
		return "--"
	}
	if hours >= 24 {
		return fmt.Sprintf("%.1f h / %.2f 天", hours, hours/24)
	}
	return fmt.Sprintf("%.1f h", hours)
}

func sameExchangePriceRiskContext(detail *entity.Opportunity, item OpportunityListItem, plan entity.ExecutionPlan, hasPlan bool) (bool, string, float64, float64, float64) {
	if detail != nil {
		return detail.SameExchangePriceRiskAllowed,
			detail.SameExchangePriceRiskReason,
			detail.SameExchangePriceShockRatio,
			detail.SameExchangePriceShockCurrentMarkPrice,
			detail.SameExchangePriceShockBaselineMarkPrice
	}
	if hasPlan {
		return plan.SameExchangePriceRiskAllowed,
			plan.SameExchangePriceRiskReason,
			plan.SameExchangePriceShockRatio,
			plan.SameExchangePriceShockCurrentMarkPrice,
			plan.SameExchangePriceShockBaselineMarkPrice
	}
	return item.SameExchangePriceRiskAllowed,
		item.SameExchangePriceRiskReason,
		item.SameExchangePriceShockRatio,
		item.SameExchangePriceShockCurrentMarkPrice,
		item.SameExchangePriceShockBaselineMarkPrice
}

func renderSameExchangeRiskSummary(strategy StrategyStatus, detail *entity.Opportunity, item OpportunityListItem, plan entity.ExecutionPlan, hasPlan bool, liveRisk service.SameExchangeLiveRiskInspection) []string {
	priceAllowed, priceReason, shockRatio, currentMark, baselineMark := sameExchangePriceRiskContext(detail, item, plan, hasPlan)
	if liveRisk.Enabled {
		priceAllowed = liveRisk.PriceShockAllowed
		priceReason = liveRisk.PriceShockReason
		if liveRisk.PriceShockRatio != 0 {
			shockRatio = liveRisk.PriceShockRatio
		}
		if liveRisk.PriceShockCurrentMarkPrice > 0 {
			currentMark = liveRisk.PriceShockCurrentMarkPrice
		}
		if liveRisk.PriceShockBaselineMarkPrice > 0 {
			baselineMark = liveRisk.PriceShockBaselineMarkPrice
		}
	}
	priceGuardText := "监控中"
	if priceAllowed {
		priceGuardText = "安全"
	} else if strings.TrimSpace(priceReason) != "" {
		priceGuardText = priceRiskReasonLabel(priceReason)
	}
	lines := []string{
		strings.Join([]string{
			renderField("1h 价格冲击", toneStyle(signedNumberTone(shockRatio)).Render(fmtPctRatio(shockRatio, 2))),
			renderField("风控阈值", toneStyle("accent").Render(fmtPctRatio(strategy.SameExchangeMax1hPriceShockRatio, 2))),
			renderField("基准标记价", toneStyle("accent").Render(priceText(baselineMark))),
			renderField("当前标记价", toneStyle("accent").Render(priceText(currentMark))),
			renderField("价格风控", toneStyle(riskGuardTone(priceAllowed, priceReason)).Render(priceGuardText)),
		}, "  "),
	}
	if liveRisk.Enabled {
		lines = append(lines, strings.Join([]string{
			renderField("爆仓价", toneStyle("accent").Render(priceText(liveRisk.LiquidationPrice))),
			renderField("爆仓距离", toneStyle(liquidationDistanceTone(liveRisk.LiquidationDistanceRatio, liveRisk)).Render(liquidationDistanceText(liveRisk.LiquidationDistanceRatio))),
			renderField("保护单", toneStyle(protectiveOrderTone(liveRisk)).Render(sameExchangeProtectiveStatusText(liveRisk))),
			renderField("保护价", toneStyle("accent").Render(priceText(liveRisk.ProtectiveOrderStopPrice))),
			renderField("保护更新时间", renderTimeValue(liveRisk.ProtectiveOrderUpdatedAtMs, "accent")),
		}, "  "))
		if strings.TrimSpace(liveRisk.ProtectiveOrderErrorMessage) != "" {
			lines = append(lines, renderField("保护单说明", toneStyle("warn").Render(liveRisk.ProtectiveOrderErrorMessage)))
		}
	}
	if strings.TrimSpace(priceReason) != "" && !priceAllowed {
		lines = append(lines, renderField("风险说明", toneStyle("warn").Render(priceRiskReasonLabel(priceReason))))
	}
	return lines
}

func priceRiskReasonLabel(reason string) string {
	switch normalizeTUIStatus(reason) {
	case "max_1h_price_shock":
		return "1h 拉升过快"
	case "eligible", "":
		return "--"
	default:
		return reason
	}
}

func riskGuardTone(allowed bool, reason string) string {
	if allowed {
		return "good"
	}
	if strings.TrimSpace(reason) != "" {
		return "bad"
	}
	return "warn"
}

func sameExchangeProtectiveStatusText(risk service.SameExchangeLiveRiskInspection) string {
	status := normalizeTUIStatus(risk.ProtectiveOrderStatus)
	switch {
	case !risk.Enabled:
		return "--"
	case status == "":
		return "未挂保护单"
	case risk.ProtectiveOrderArmed:
		return "已挂保护单"
	case status == "filled":
		return "已触发"
	case status == "canceled" || status == "cancelled":
		return "已撤销"
	case status == "error":
		return "保护单失败"
	default:
		return strings.ToUpper(strings.TrimSpace(risk.ProtectiveOrderStatus))
	}
}

func protectiveOrderTone(risk service.SameExchangeLiveRiskInspection) string {
	status := normalizeTUIStatus(risk.ProtectiveOrderStatus)
	switch {
	case !risk.Enabled:
		return "subtle"
	case risk.ProtectiveOrderArmed:
		return "good"
	case status == "filled":
		return "warn"
	case status == "error":
		return "bad"
	default:
		return "subtle"
	}
}

func liquidationDistanceText(ratio float64) string {
	if ratio <= 0 {
		return "--"
	}
	return fmtPctRatio(ratio, 2)
}

func liquidationDistanceTone(ratio float64, risk service.SameExchangeLiveRiskInspection) string {
	if ratio <= 0 {
		return "subtle"
	}
	switch {
	case risk.EmergencyLiqDistanceRatio > 0 && ratio <= risk.EmergencyLiqDistanceRatio:
		return "bad"
	case risk.ReduceLiqDistanceRatio > 0 && ratio <= risk.ReduceLiqDistanceRatio:
		return "warn"
	default:
		return "good"
	}
}

func normalizeTUIStatus(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
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
	case "price_risk_guard":
		return "价格异动风控"
	case "not_profitable":
		return "收益不足"
	case "long_hold_guard":
		return "长持条件不足"
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

func livePositionLegLabels(arbitrageMode string) (string, string) {
	if isSameExchangeArbitrageMode(arbitrageMode) {
		return "现货腿", "永续腿"
	}
	return "多头腿", "空头腿"
}

func (m Model) renderLivePositionDetail(item service.LivePositionCandidate, width int) string {
	arbitrageMode := recordArbitrageMode(item.Execution, m.data.System.Strategy.ArbitrageMode)
	if item.Plan != nil {
		arbitrageMode = planArbitrageMode(*item.Plan, arbitrageMode)
	}
	longLabel, shortLabel := livePositionLegLabels(arbitrageMode)
	lines := []string{
		strings.Join([]string{
			renderField("同步", toneStyle(livePositionStatusTone(item.SyncStatus)).Render(livePositionStatusLabel(item.SyncStatus))),
			renderField("摘要", toneStyle(livePositionStatusTone(item.SyncStatus)).Render(clip(livePositionSummaryText(item.Summary), maxInt(10, width-18)))),
		}, "  "),
	}
	if item.Risk.Enabled {
		lines = append(lines, renderSameExchangeRiskSummary(m.data.System.Strategy, nil, OpportunityListItem{}, valueOrZeroPlan(item.Plan), item.Plan != nil, item.Risk)...)
	}
	lines = append(lines, m.renderLivePositionLegDetail(longLabel, item.LongLeg))
	lines = append(lines, m.renderLivePositionLegDetail(shortLabel, item.ShortLeg))
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
		renderField("爆仓价", toneStyle("accent").Render(priceText(leg.Position.LiquidationPrice))),
		renderField("爆仓距离", toneStyle("accent").Render(liquidationDistanceText(livePositionLiquidationDistanceRatio(leg.Position)))),
		renderField("浮盈亏", renderMoneyValue(leg.Position.UnrealizedPnL, 3)),
		renderField("可见", renderBoolValue(leg.HasPosition, false)),
		renderField("方向正确", renderBoolValue(leg.DirectionOK, false)),
	}, "  ")
}

func valueOrZeroPlan(plan *entity.ExecutionPlan) entity.ExecutionPlan {
	if plan == nil {
		return entity.ExecutionPlan{}
	}
	return *plan
}

func livePositionLiquidationDistanceRatio(pos exchange.Position) float64 {
	if pos.MarkPrice <= 0 || pos.LiquidationPrice <= 0 || math.Abs(pos.Quantity) <= 1e-9 {
		return 0
	}
	if pos.Quantity < 0 {
		return math.Max(0, (pos.LiquidationPrice-pos.MarkPrice)/pos.MarkPrice)
	}
	return math.Max(0, (pos.MarkPrice-pos.LiquidationPrice)/pos.MarkPrice)
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
	case "watching", "pending_open", "pending_close", "open_partial_failed", "close_partial_failed", "open_hedging", "close_hedging", "outside_entry_window", "pending", "submitted", "new", "partially_filled", "partially_filled_canceled", "long_hold_guard":
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
