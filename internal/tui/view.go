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
		return "loading terminal..."
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
	title := fmt.Sprintf("Funding Arbitrage TUI  [%s]", m.view.String())
	strategy := m.data.System.Strategy
	exec := m.data.System.Execution
	statusLine := strings.Join([]string{
		m.chip("mode "+m.interactionModeLabel(), m.interactionModeTone()),
		m.chip("refresh "+m.refreshInterval.String(), "accent"),
		m.chip("sort "+m.sort.String(), "accent"),
		m.chip("pair "+m.currentPairLabel(), "accent"),
		m.chip("search "+orDefault(strings.TrimSpace(m.search.Value()), "--"), "accent"),
		m.chip("live "+boolWord(m.data.System.Execution.LiveTradingEnabled), boolTone(m.data.System.Execution.LiveTradingEnabled, true)),
		m.chip("auto_entry "+boolWord(m.data.System.Execution.AutoEntry), boolTone(m.data.System.Execution.AutoEntry, false)),
		m.chip("auto_close "+boolWord(m.data.System.Execution.AutoClose), boolTone(m.data.System.Execution.AutoClose, false)),
	}, "  ")
	limitLine := strings.Join([]string{
		m.chip("min_pnl "+compactUSDT(strategy.MinNetPNL, 3), "good"),
		m.chip("max_spread "+compactBps(strategy.MaxSpreadBps, 2), "warn"),
		m.chip("entry_lead "+orDefault(strategy.EntryLeadTime, "--"), "accent"),
		m.chip("close_grace "+orDefault(exec.CloseGracePeriod, "--"), "accent"),
	}, "  ")

	metrics := strings.Join([]string{
		m.metric("opps", len(m.data.Opportunities)),
		m.metric("ready", countEligible(m.data.Opportunities)),
		m.metric("plans", len(m.data.AllPlans)),
		m.metric("exec", len(m.data.Executions)),
		m.metric("fund24h", int(m.data.Stats.FundingCount24h)),
		m.metric("book24h", int(m.data.Stats.BookTopCount24h)),
		m.metric("batch", m.data.CurrentBatchID),
	}, "   ")

	stateParts := []string{
		m.chip("source "+m.client.BaseURL(), "accent"),
		m.chip("last "+orDefault(formatClock(m.lastRefresh), "--"), "accent"),
	}
	if m.loading {
		label := "syncing"
		if summary := m.loadingSummary(); summary != "" {
			label = "sync " + summary
		}
		stateParts = append(stateParts, m.chip(label, "warn"))
	}
	if strings.TrimSpace(m.lastError) != "" {
		stateParts = append(stateParts, m.chip("error "+clip(m.lastError, 48), "bad"))
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
		midHeight, bottomHeight := splitStackedHeights(restHeight)
		sections := []string{
			m.renderConnectorPanel(leftWidth+rightWidth, topHeight),
			m.renderConfigPanel(leftWidth+rightWidth, midHeight),
		}
		if bottomHeight > 0 {
			sections = append(sections, m.renderAutoClosePanel(leftWidth+rightWidth, bottomHeight))
		}
		return lipgloss.JoinVertical(lipgloss.Left, sections...)
	}
	topHeight, bottomHeight := splitStackedHeights(height)
	right := m.renderConfigPanel(rightWidth, height)
	if bottomHeight > 0 {
		right = lipgloss.JoinVertical(
			lipgloss.Left,
			m.renderConfigPanel(rightWidth, topHeight),
			m.renderAutoClosePanel(rightWidth, bottomHeight),
		)
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
		edgeText := alignRightValue(renderPctValue(fundingSpreadHourly(item), 5), 10)
		row := []string{
			fmt.Sprintf("%s %-3d %-7s %-24s %s", marker, i+1, clip(item.Symbol, 7), clip(opportunityDirection(item), 24), pnlText),
			fmt.Sprintf("     %-18s basis %-9s edge %s", clip(opportunityPair(item), 18), fmtSignedBps(item.BasisBps, 2), edgeText),
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
	title := fmt.Sprintf("Execution  [%s]", m.execTab.String())
	sub := "tab switch  p plans  x executions  o open  c close"
	lines := []string{ui.panelTitle.Render(title), ui.subtle.Render(sub), ""}

	if m.execTab == execRecords {
		if len(m.data.Executions) == 0 {
			if m.isLoading(loadExecutions) {
				lines = append(lines, ui.subtle.Render("Loading execution records..."))
				return renderPanel(width, height, strings.Join(lines, "\n"))
			}
			lines = append(lines, ui.subtle.Render("No execution records yet."))
			return renderPanel(width, height, strings.Join(lines, "\n"))
		}
		visibleRows := maxInt(1, panelListContentHeight(height)/listRowHeight)
		selected := m.indexOfExecution(m.selectedExecutionPlanKey)
		if selected < 0 {
			selected = 0
		}
		start := clampOffset(m.executionOffset, len(m.data.Executions), visibleRows)
		end := minInt(len(m.data.Executions), start+visibleRows)
		lines = append(lines, ui.subtle.Render(fmt.Sprintf("showing %d-%d", start+1, end)), "")
		for i := start; i < end; i++ {
			item := m.data.Executions[i]
			marker := selectedMarker(i == selected)
			allocatedText := "--"
			if value := executionAllocatedNotional(item, entity.ExecutionPlan{}, false); value > 0 {
				allocatedText = fmtMoney(value, 2)
			}
			block := strings.Join([]string{
				fmt.Sprintf("%s %-7s %-25s %s", marker, clip(item.Symbol, 7), clip(opportunityDirection(entity.Opportunity{LongExchange: item.LongExchange, ShortExchange: item.ShortExchange}), 25), statusText(item.Status)),
				fmt.Sprintf("    plan %-28s live %-3s auto_close %-3s", clip(item.PlanKey, 28), boolWord(item.LiveTrading), boolWord(item.AutoClose)),
				fmt.Sprintf("    alloc %-14s open %s  close %s", clip(allocatedText, 14), clip(fmtTime(item.OpenedAtMs), 19), clip(fmtTime(item.ClosedAtMs), 19)),
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
			lines = append(lines, ui.subtle.Render("Loading plans..."))
			return renderPanel(width, height, strings.Join(lines, "\n"))
		}
		lines = append(lines, ui.subtle.Render("No plans available."))
		return renderPanel(width, height, strings.Join(lines, "\n"))
	}
	visibleRows := maxInt(1, panelListContentHeight(height)/listRowHeight)
	selected := m.indexOfPlan(m.selectedPlanKey)
	if selected < 0 {
		selected = 0
	}
	start := clampOffset(m.planOffset, len(m.data.AllPlans), visibleRows)
	end := minInt(len(m.data.AllPlans), start+visibleRows)
	lines = append(lines, ui.subtle.Render(fmt.Sprintf("showing %d-%d", start+1, end)), "")
	for i := start; i < end; i++ {
		item := m.data.AllPlans[i]
		marker := selectedMarker(i == selected)
		pnlText := alignRightValue(renderMoneyValue(item.NetExpectedPNL, 3), 10)
		block := strings.Join([]string{
			fmt.Sprintf("%s %-7s %-25s %s", marker, clip(item.Symbol, 7), clip(opportunityDirection(entity.Opportunity{LongExchange: item.LongExchange, ShortExchange: item.ShortExchange}), 25), pnlText),
			fmt.Sprintf("    %-18s ready %-3s status %-18s", clip(opportunityPair(entity.Opportunity{LongExchange: item.LongExchange, ShortExchange: item.ShortExchange}), 18), boolWord(item.ReadyNow), clip(statusText(item.Status), 18)),
			fmt.Sprintf("    plan %-16s notional %-12s", clip(item.PlanKey, 16), clip(fmtMoney(targetNotional(item), 2), 12)),
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
		ui.panelTitle.Render("Execution Detail"),
		ui.subtle.Render("Shows plan state, execution record, and order history for the selected item."),
		"",
	}
	var detail string
	if m.execTab == execRecords {
		rec, ok := m.selectedExecution()
		if !ok {
			detail = ui.subtle.Render("Select an execution record first.")
		} else {
			plan, hasPlan := m.planByKey(rec.PlanKey)
			detail = m.renderExecutionRecordDetail(rec, plan, hasPlan, width-4)
		}
	} else {
		plan, ok := m.selectedPlan()
		if !ok {
			detail = ui.subtle.Render("Select a plan first.")
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
		ui.panelTitle.Render(fmt.Sprintf("Connectors  %d", len(m.data.System.Connectors))),
		ui.subtle.Render("Healthy connectors should have recent market/book activity and no sticky error."),
		"",
	}
	if len(m.data.System.Connectors) == 0 {
		if m.isLoading(loadSystem) {
			lines = append(lines, ui.subtle.Render("Loading system status..."))
			return renderPanel(width, height, strings.Join(lines, "\n"))
		}
		lines = append(lines, ui.subtle.Render("No connector status has been reported yet."))
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
			toneStyle(tone).Render(fmt.Sprintf("%-12s market=%-3s book=%-3s  market_at=%-19s  book_at=%-19s",
				item.Exchange,
				boolWord(item.MarkPriceConnected),
				boolWord(item.BookTickerConnected),
				clip(item.LastMarketEventAt.Format("2006-01-02 15:04:05"), 19),
				clip(item.LastBookEventAt.Format("2006-01-02 15:04:05"), 19),
			)),
		)
		if strings.TrimSpace(item.LastError) != "" {
			lines = append(lines, ui.bad.Render("  err: "+clip(item.LastError, width-8)))
		}
	}
	return renderPanel(width, height, strings.Join(lines, "\n"))
}

func (m Model) renderConfigPanel(width int, height int) string {
	strategy := m.data.System.Strategy
	exec := m.data.System.Execution
	lines := []string{
		ui.panelTitle.Render("Strategy / Execution"),
		ui.subtle.Render("Static config and current watchlists, useful for verifying whether the engine is in monitor or live mode."),
		"",
		fmt.Sprintf("Strategy: enabled=%s  hold=%sh  leverage=%sx  min_net=%s  effective_notional=%s",
			boolWord(strategy.Enabled),
			fmtNumber(strategy.HoldHours, 1),
			fmtNumber(strategy.Leverage, 2),
			fmtMoney(strategy.MinNetPNL, 3),
			fmtMoney(strategy.EffectiveNotional, 2),
		),
		fmt.Sprintf("Entry/Exit: %s / %s  spread_limit=%s  data_age=%s",
			orDefault(strategy.EntryMode, "--"),
			orDefault(strategy.ExitMode, "--"),
			fmtSignedBps(strategy.MaxSpreadBps, 2),
			orDefault(strategy.MaxDataAge, "--"),
		),
		fmt.Sprintf("Execution: live=%s  auto_entry=%s  auto_close=%s  loop=%s  close_grace=%s  max_latest_plans=%d",
			boolWord(exec.LiveTradingEnabled),
			boolWord(exec.AutoEntry),
			boolWord(exec.AutoClose),
			orDefault(exec.LoopInterval, "--"),
			orDefault(exec.CloseGracePeriod, "--"),
			exec.MaxLatestPlans,
		),
		fmt.Sprintf("Auto budget: allocate=%s  used=%s  remain=%s  live_plans=%d%s  per_loop=%s",
			boolWord(exec.AutoAllocateCapital),
			fmtMoney(exec.ActiveAllocatedNotionalUSDT, 2),
			fmtMoney(exec.RemainingAutoBudgetUSDT, 2),
			exec.ActiveLivePlans,
			renderLimitSuffix(exec.MaxLivePlans),
			renderLoopLimit(exec.MaxAutoOpenPerLoop),
		),
		"",
		"Watchlist: " + clip(strings.Join(m.data.System.Watchlist, ", "), width-8),
		"DeepScan: " + clip(strings.Join(m.data.System.DeepScanWatchlist, ", "), width-8),
	}
	if m.isLoading(loadSystem) && len(m.data.System.Watchlist) == 0 && len(m.data.System.DeepScanWatchlist) == 0 {
		lines = append(lines, "", ui.subtle.Render("Loading strategy and execution config..."))
	}
	return renderPanel(width, height, strings.Join(lines, "\n"))
}

func (m Model) renderAutoClosePanel(width int, height int) string {
	items := m.data.AutoClose.Candidates
	lines := []string{
		ui.panelTitle.Render(fmt.Sprintf("Auto Close Live  %d", len(items))),
		ui.subtle.Render("Shows live executions, the current close decision, and what a manual sweep would act on."),
		"",
		fmt.Sprintf("evaluated=%s  eligible=%d  should_close=%d  errors=%d",
			fmtTime(m.data.AutoClose.EvaluatedAtMs),
			m.data.AutoClose.Eligible,
			m.data.AutoClose.ShouldClose,
			m.data.AutoClose.DecisionErrors,
		),
		"",
	}
	if len(items) == 0 {
		lines = append(lines, ui.subtle.Render("No live execution is currently tracked for auto-close."))
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

		actionLabel := "holding"
		actionTone := "accent"
		switch {
		case decision.Error != "":
			actionLabel = "decision_error"
			actionTone = "bad"
		case decision.ShouldClose:
			actionLabel = decision.Trigger
			if strings.TrimSpace(actionLabel) == "" {
				actionLabel = "close_now"
			}
			actionTone = "warn"
		case !decision.Eligible:
			actionLabel = "skipped"
			actionTone = "bad"
		case !decision.AutoCloseEnabled:
			actionLabel = "auto_close_off"
			actionTone = "bad"
		}

		reason := decision.Reason
		if decision.Error != "" {
			reason = decision.Error
		}
		if strings.TrimSpace(reason) == "" {
			reason = "--"
		}

		allocated := executionAllocatedNotional(rec, plan, hasPlan)
		block := strings.Join([]string{
			fmt.Sprintf("%-7s %-24s %s", clip(rec.Symbol, 7), clip(rec.PlanKey, 24), statusText(rec.Status)),
			fmt.Sprintf("    due %-19s alloc %-12s auto_close %-3s %s",
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

func (m Model) renderOverviewDetail(item OpportunityListItem, detail *entity.Opportunity, hasDetail bool, loading bool, plan entity.ExecutionPlan, hasPlan bool, rec entity.ExecutionRecord, hasRec bool, width int) string {
	planLabel, _ := m.opportunityPlanLabel(item)
	lines := []string{
		fmt.Sprintf("%s  %s", ui.accent.Render(item.Symbol), ui.subtle.Render(opportunityDirection(item))),
		strings.Join([]string{
			renderField("净收益", renderMoneyValue(item.NetExpectedPNL, 3)),
			renderField("净收益率", renderBpsValue(item.NetExpectedBps, 2)),
			renderField("资金费收益", renderPctValue(fundingSpread(item), 5)),
			renderField("时均边际", renderPctValue(fundingSpreadHourly(item), 5)),
			renderField("基差", renderBasisValue(item.BasisBps, item.MaxAllowedBasisBps)),
		}, "  "),
		strings.Join([]string{
			renderField("状态", renderStatusValue(item.Status)),
			renderField("可执行", renderBoolValue(item.EligibleForExecution, false)),
			renderField("计划", toneStyle(opportunityPlanTone(planLabel)).Render(planLabel)),
			renderField("建议持有", toneStyle("accent").Render(holdingDurationText(item))),
		}, "  "),
		strings.Join([]string{
			renderField("多头结算倒计时", renderDurationValue(time.Until(time.UnixMilli(item.LongFundingTimeMs)))),
			renderField("空头结算倒计时", renderDurationValue(time.Until(time.UnixMilli(item.ShortFundingTimeMs)))),
			renderField("预计兑现时间", renderTimeValue(item.ProjectedFundingTimeMs, "accent")),
		}, "  "),
		strings.Join([]string{
			renderField("产生时间", renderTimeValue(opportunityProducedTime(item).UnixMilli(), "subtle")),
			renderField("批次", toneStyle("accent").Render(orDefault(item.BatchID, "--"))),
			renderField("结算窗口", toneStyle("accent").Render(fmt.Sprintf("%sh", fmtNumber(item.FundingWindowHours, 2)))),
			renderField("资金费模式", toneStyle("accent").Render(orDefault(item.FundingComputationMode, "--"))),
		}, "  "),
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
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderPnLBreakdownDetail(item OpportunityListItem, detail *entity.Opportunity, hasDetail bool, plan entity.ExecutionPlan, hasPlan bool, width int) string {
	carrySource := any(item)
	if hasDetail && detail != nil {
		carrySource = *detail
	}
	carryRate := fundingSpread(carrySource)
	hourlyEdge := fundingSpreadHourly(carrySource)
	notional := opportunityTargetNotional(carryRate, item.GrossFundingPNL, plan, hasPlan, m.data.System.Strategy)
	strategy := m.data.System.Strategy

	lines := []string{
		clip(strings.Join([]string{
			renderField("估算名义", renderUSDTValue(notional, 2)),
			renderField("Funding carry", renderPctValue(carryRate, 5)),
			renderField("时均 edge", renderPctValue(hourlyEdge, 5)),
			renderField("事件数", toneStyle("accent").Render(fmt.Sprintf("多头 %d / 空头 %d", item.LongFundingEventCount, item.ShortFundingEventCount))),
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
			renderField("资金收益", renderMoneyValue(item.GrossFundingPNL, 3)),
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
		clip(toneStyle("subtle").Render("说明: 资金收益按当前最佳持有窗口估算；若会跨多轮 funding，后续事件会结合近期历史做平滑预测。"), width),
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
		renderLegRow("当前费率", fmtPctRatio(item.LongFundingRate, 5), fmtPctRatio(item.ShortFundingRate, 5), metricWidth, longWidth, shortWidth, signedNumberTone(item.LongFundingRate), signedNumberTone(item.ShortFundingRate)),
		renderLegRow("预测费率", fmtPctRatio(item.LongFutureFundingRate, 5), fmtPctRatio(item.ShortFutureFundingRate, 5), metricWidth, longWidth, shortWidth, signedNumberTone(item.LongFutureFundingRate), signedNumberTone(item.ShortFutureFundingRate)),
		renderLegRow("小时化", fmtPctRatio(item.LongFundingHourly, 5), fmtPctRatio(item.ShortFundingHourly, 5), metricWidth, longWidth, shortWidth, signedNumberTone(item.LongFundingHourly), signedNumberTone(item.ShortFundingHourly)),
		renderLegRow("买一", priceText(item.LongBidPrice), priceText(item.ShortBidPrice), metricWidth, longWidth, shortWidth, "accent", "accent"),
		renderLegRow("卖一", priceText(item.LongAskPrice), priceText(item.ShortAskPrice), metricWidth, longWidth, shortWidth, "accent", "accent"),
		renderLegRow("标记价", priceText(item.LongMarkPrice), priceText(item.ShortMarkPrice), metricWidth, longWidth, shortWidth, "accent", "accent"),
		renderLegRow("下次结算", fmtTime(item.LongFundingTimeMs), fmtTime(item.ShortFundingTimeMs), metricWidth, longWidth, shortWidth, "accent", "accent"),
		renderLegRow("结算间隔", fmt.Sprintf("%sh", fmtNumber(float64(item.LongFundingIntervalHours), 0)), fmt.Sprintf("%sh", fmtNumber(float64(item.ShortFundingIntervalHours), 0)), metricWidth, longWidth, shortWidth, "accent", "accent"),
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
			renderField("目标平仓", renderTimeValue(plan.TargetCloseTimeMs, "accent")),
			renderField("入场开始", renderTimeValue(plan.EntryWindowOpenMs, "accent")),
			renderField("入场截止", renderTimeValue(plan.EntryWindowCloseMs, "accent")),
		}, "  "),
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
	lines := []string{ui.accent.Render("序号  兑现时间             窗口     多头  空头  收益        时均        预期收益")}
	for _, row := range detail.ProjectionDetails {
		line := fmt.Sprintf("%-5d %-20s %-8s %-5d %-5d %-11s %-11s %-10s",
			row.ProjectionRank,
			clip(fmtTime(row.ProjectedFundingTimeMs), 20),
			clip(fmt.Sprintf("%sh", fmtNumber(row.FundingWindowHours, 2)), 8),
			row.LongFundingEventCount,
			row.ShortFundingEventCount,
			fmtPctRatio(row.CarryRate, 5),
			fmtPctRatio(row.CarryRateHourlyEquivalent, 5),
			fmtMoney(row.NetExpectedPNL, 3),
		)
		lines = append(lines, toneStyle(signedNumberTone(row.NetExpectedPNL)).Render(line))
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
	lines := []string{ui.accent.Render("阶段    腿       交易所       方向   状态               请求数量         成交数量         均价")}
	for _, order := range items {
		line := fmt.Sprintf("%-7s %-8s %-12s %-6s %-18s %-15s %-15s %-10s",
			clip(order.Phase, 7),
			clip(order.LegRole, 8),
			clip(order.Exchange, 12),
			clip(order.Side, 6),
			clip(order.Status, 18),
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
	lines := []string{
		fmt.Sprintf("%s  %s", ui.accent.Render(plan.Symbol), ui.subtle.Render(opportunityDirection(entity.Opportunity{LongExchange: plan.LongExchange, ShortExchange: plan.ShortExchange}))),
		strings.Join([]string{
			renderField("plan", toneStyle("accent").Render(plan.PlanKey)),
			renderField("ready", renderBoolValue(plan.ReadyNow, false)),
			renderField("status", renderStatusValue(plan.Status)),
			renderField("pnl", renderMoneyValue(plan.NetExpectedPNL, 3)),
		}, "  "),
		strings.Join([]string{
			renderField("notional", renderMoneyValue(targetNotional(plan), 2)),
			renderField("leverage", toneStyle("accent").Render(fmt.Sprintf("%sx", fmtNumber(plan.TargetLeverage, 2)))),
			renderField("basis", renderBpsValue(plan.CrossVenueBasisBps, 2)),
			renderField("target_close", renderTimeValue(plan.TargetCloseTimeMs, "accent")),
		}, "  "),
		strings.Join([]string{
			renderField("long", toneStyle("accent").Render(plan.LongVenueSymbol)),
			renderField("qty", toneStyle("accent").Render(fmtNumber(plan.LongQty, 6))),
			renderField("@", toneStyle("accent").Render(priceText(plan.LongEntryPrice))),
		}, "  "),
		strings.Join([]string{
			renderField("short", toneStyle("accent").Render(plan.ShortVenueSymbol)),
			renderField("qty", toneStyle("accent").Render(fmtNumber(plan.ShortQty, 6))),
			renderField("@", toneStyle("accent").Render(priceText(plan.ShortEntryPrice))),
		}, "  "),
	}
	if opp, ok := m.matchingOpportunityForPlan(plan); ok {
		lines = append(lines, strings.Join([]string{
			renderField("matched_opp", toneStyle("accent").Render(opportunityKey(opp))),
			renderField("opp_status", renderStatusValue(opp.Status)),
			renderField("hold", toneStyle("accent").Render(holdingDurationText(opp))),
		}, "  "))
	}
	if hasRec {
		lines = append(lines, strings.Join([]string{
			renderField("record", renderStatusValue(rec.Status)),
			renderField("live", renderBoolValue(rec.LiveTrading, true)),
			renderField("auto_close", renderBoolValue(rec.AutoClose, false)),
			renderField("alloc", renderAllocatedNotionalValue(executionAllocatedNotional(rec, plan, true))),
			renderField("opened", renderTimeValue(rec.OpenedAtMs, "accent")),
			renderField("closed", renderTimeValue(rec.ClosedAtMs, "accent")),
		}, "  "))
		if strings.TrimSpace(rec.StatusReason) != "" {
			lines = append(lines, renderField("reason", toneStyle("warn").Render(clip(rec.StatusReason, width))))
		}
		if strings.TrimSpace(rec.LastError) != "" {
			lines = append(lines, renderField("last_error", toneStyle("bad").Render(clip(rec.LastError, width))))
		}
	}
	lines = append(lines, "", ui.panelTitle.Render("Orders"), m.renderOrdersDetail(plan.PlanKey, width))
	return strings.Join(lines, "\n")
}

func (m Model) renderExecutionRecordDetail(rec entity.ExecutionRecord, plan entity.ExecutionPlan, hasPlan bool, width int) string {
	lines := []string{
		fmt.Sprintf("%s  %s", ui.accent.Render(rec.Symbol), ui.subtle.Render(opportunityDirection(entity.Opportunity{LongExchange: rec.LongExchange, ShortExchange: rec.ShortExchange}))),
		strings.Join([]string{
			renderField("plan", toneStyle("accent").Render(rec.PlanKey)),
			renderField("status", renderStatusValue(rec.Status)),
			renderField("live", renderBoolValue(rec.LiveTrading, true)),
			renderField("auto_close", renderBoolValue(rec.AutoClose, false)),
		}, "  "),
		strings.Join([]string{
			renderField("alloc", renderAllocatedNotionalValue(executionAllocatedNotional(rec, plan, hasPlan))),
			renderField("opened", renderTimeValue(rec.OpenedAtMs, "accent")),
			renderField("closed", renderTimeValue(rec.ClosedAtMs, "accent")),
			renderField("transition", renderTimeValue(rec.LastTransitionAtMs, "accent")),
			renderField("event", toneStyle("accent").Render(orDefault(rec.LastTransitionEvent, "--"))),
		}, "  "),
		strings.Join([]string{
			renderField("open_orders", toneStyle("accent").Render(fmt.Sprintf("%d", rec.OpenOrderCount))),
			renderField("close_orders", toneStyle("accent").Render(fmt.Sprintf("%d", rec.CloseOrderCount))),
		}, "  "),
	}
	if strings.TrimSpace(rec.StatusReason) != "" {
		lines = append(lines, renderField("reason", toneStyle("warn").Render(clip(rec.StatusReason, width))))
	}
	if strings.TrimSpace(rec.LastError) != "" {
		lines = append(lines, renderField("last_error", toneStyle("bad").Render(clip(rec.LastError, width))))
	}
	if hasPlan {
		lines = append(lines, strings.Join([]string{
			renderField("plan_status", renderStatusValue(plan.Status)),
			renderField("expected_pnl", renderMoneyValue(plan.NetExpectedPNL, 3)),
			renderField("target_close", renderTimeValue(plan.TargetCloseTimeMs, "accent")),
		}, "  "))
	}
	lines = append(lines, "", ui.panelTitle.Render("Orders"), m.renderOrdersDetail(rec.PlanKey, width))
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
		"Execution",
		"  p show plans   x show execution records   o/c operate on current selection",
		"Safety",
		"  open/close actions require typing OPEN or CLOSE before the request is sent",
		"",
		ui.subtle.Render("Press ? or esc to return."),
	}
	return renderModal(minInt(m.width-4, 96), minInt(height, 18), strings.Join(lines, "\n"))
}

func (m Model) renderConfirm(height int) string {
	target := m.confirm.Target
	word := string(m.confirm.Action)
	liveText := "dry-run request"
	if target.LiveTrading || m.data.System.Execution.LiveTradingEnabled {
		liveText = "live request"
	}
	lines := []string{
		ui.panelTitle.Render(fmt.Sprintf("%s Confirmation", word)),
		"",
		fmt.Sprintf("source=%s  mode=%s", target.Source, liveText),
		fmt.Sprintf("plan=%s", target.PlanKey),
		fmt.Sprintf("symbol=%s  direction=%s long / %s short", target.Symbol, target.LongExchange, target.ShortExchange),
		fmt.Sprintf("status=%s  expected_pnl=%s", statusText(target.Status), fmtMoney(target.NetExpectedPNL, 3)),
		"",
		fmt.Sprintf("Type %s to confirm:", ui.code.Render(word)),
		m.confirm.Input.View(),
	}
	if strings.TrimSpace(m.confirm.ErrorText) != "" {
		lines = append(lines, "", ui.bad.Render(m.confirm.ErrorText))
	}
	if m.confirm.Submitting {
		lines = append(lines, "", ui.warn.Render("Submitting request..."))
	}
	lines = append(lines, "", ui.subtle.Render("esc cancels"))
	return renderModal(minInt(m.width-4, 88), minInt(height, 14), strings.Join(lines, "\n"))
}

func (m Model) renderFooter(width int) string {
	if m.confirm != nil {
		return ui.footer.Width(width).Render(fmt.Sprintf("Mode: %s   type %s to confirm, esc to cancel", m.interactionModeLabel(), m.confirm.Action))
	}
	if m.showHelp {
		return ui.footer.Width(width).Render("Mode: HELP   esc or ? to return")
	}
	if m.searchMode {
		return ui.footer.Width(width).Render("Mode: SEARCH   " + m.search.View() + "   enter/esc to apply and close")
	}
	return ui.footer.Width(width).Render("Mode: NORMAL   j/k move  tab switch  / search  f pair  s sort  r refresh  o open  c close  1/2/3 views  ? help  q quit")
}

func (m Model) renderTab(label string, active bool) string {
	if active {
		return ui.tabActive.Render(label)
	}
	return ui.tabIdle.Render(label)
}

func (m Model) currentPairLabel() string {
	if strings.TrimSpace(m.pairFilter) == "" {
		return "all"
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
		return "CONFIRM"
	case m.searchMode:
		return "SEARCH"
	case m.showHelp:
		return "HELP"
	default:
		return "NORMAL"
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
		return "yes"
	}
	return "no"
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

func renderTimeValue(ms int64, tone string) string {
	return toneStyle(tone).Render(fmtTime(ms))
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
	case "eligible", "ready", "opened", "closed", "dry_run_opened", "dry_run_closed":
		return "good"
	case "watching", "pending_open", "pending_close", "open_partial_failed", "close_partial_failed", "open_hedging", "close_hedging", "outside_entry_window":
		return "warn"
	default:
		return "bad"
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
		return "/unlimited"
	}
	return fmt.Sprintf("/%d", limit)
}

func renderLoopLimit(limit int) string {
	if limit <= 0 {
		return "unlimited"
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
