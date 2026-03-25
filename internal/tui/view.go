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

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "loading terminal..."
	}

	bodyHeight := maxInt(8, m.height-8)
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
		listHeight := maxInt(8, height/2)
		return lipgloss.JoinVertical(lipgloss.Left,
			m.renderOpportunityList(leftWidth+rightWidth, listHeight),
			m.renderScannerDetail(leftWidth+rightWidth, maxInt(8, height-listHeight)),
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
		listHeight := maxInt(8, height/2)
		return lipgloss.JoinVertical(lipgloss.Left,
			m.renderExecutionList(leftWidth+rightWidth, listHeight),
			m.renderExecutionDetail(leftWidth+rightWidth, maxInt(8, height-listHeight)),
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
		leftHeight := maxInt(8, height/2)
		return lipgloss.JoinVertical(lipgloss.Left,
			m.renderConnectorPanel(leftWidth+rightWidth, leftHeight),
			m.renderConfigPanel(leftWidth+rightWidth, maxInt(8, height-leftHeight)),
		)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top,
		m.renderConnectorPanel(leftWidth, height),
		m.renderConfigPanel(rightWidth, height),
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
			return ui.panel.Width(width).Height(height).Render(strings.Join(lines, "\n"))
		}
		lines = append(lines, ui.subtle.Render("No opportunities match the current filter."))
		return ui.panel.Width(width).Height(height).Render(strings.Join(lines, "\n"))
	}

	rowHeight := 3
	visibleRows := maxInt(1, (height-5)/rowHeight)
	selected := m.indexOfOpportunity(items, m.selectedOpportunityKey)
	if selected < 0 {
		selected = 0
	}
	start := clampOffset(m.opportunityOffset, len(items), visibleRows)
	end := minInt(len(items), start+visibleRows)
	for i := start; i < end; i++ {
		item := items[i]
		active := i == selected
		planLabel, planTone := m.opportunityPlanLabel(item)
		marker := selectedMarker(active)
		planText := toneStyle(planTone).Render(clip(planLabel, 18))
		row := []string{
			fmt.Sprintf("%s %-3d %-7s %-24s %12s", marker, i+1, clip(item.Symbol, 7), clip(opportunityDirection(item), 24), fmtMoney(item.NetExpectedPNL, 3)),
			fmt.Sprintf("     %-18s basis %-9s edge %-10s", clip(opportunityPair(item), 18), fmtSignedBps(item.BasisBps, 2), fmtPctRatio(fundingSpreadHourly(item), 5)),
			fmt.Sprintf("     %-14s  |  %s  |  %s", clip(holdingDurationText(item), 14), planText, clip(statusText(item.Status), 16)),
		}
		block := strings.Join(row, "\n")
		if active {
			block = ui.activeRow.Render(block)
		}
		lines = append(lines, block)
	}
	return ui.panel.Width(width).Height(height).Render(strings.Join(lines, "\n"))
}

func (m Model) renderScannerDetail(width int, height int) string {
	tabs := []string{
		m.renderTab("Overview", m.tab == tabOverview),
		m.renderTab("Legs", m.tab == tabLegs),
		m.renderTab("Plan", m.tab == tabPlan),
		m.renderTab("Projection", m.tab == tabProjection),
		m.renderTab("Orders", m.tab == tabOrders),
	}
	lines := []string{
		ui.panelTitle.Render("Detail"),
		strings.Join(tabs, " "),
		"",
	}

	item, ok := m.selectedOpportunity()
	if !ok {
		lines = append(lines, ui.subtle.Render("Select an opportunity to inspect its details."))
		return ui.panel.Width(width).Height(height).Render(strings.Join(lines, "\n"))
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
		detail = m.renderOverviewDetail(item, plan, hasPlan, rec, hasRec, width-4)
	}
	lines = append(lines, detail)
	return ui.panel.Width(width).Height(height).Render(strings.Join(lines, "\n"))
}

func (m Model) renderExecutionList(width int, height int) string {
	title := fmt.Sprintf("Execution  [%s]", m.execTab.String())
	sub := "tab switch  p plans  x executions  o open  c close"
	lines := []string{ui.panelTitle.Render(title), ui.subtle.Render(sub), ""}

	if m.execTab == execRecords {
		if len(m.data.Executions) == 0 {
			if m.isLoading(loadExecutions) {
				lines = append(lines, ui.subtle.Render("Loading execution records..."))
				return ui.panel.Width(width).Height(height).Render(strings.Join(lines, "\n"))
			}
			lines = append(lines, ui.subtle.Render("No execution records yet."))
			return ui.panel.Width(width).Height(height).Render(strings.Join(lines, "\n"))
		}
		rowHeight := 3
		visibleRows := maxInt(1, (height-5)/rowHeight)
		selected := m.indexOfExecution(m.selectedExecutionPlanKey)
		if selected < 0 {
			selected = 0
		}
		start := clampOffset(m.executionOffset, len(m.data.Executions), visibleRows)
		end := minInt(len(m.data.Executions), start+visibleRows)
		for i := start; i < end; i++ {
			item := m.data.Executions[i]
			marker := selectedMarker(i == selected)
			block := strings.Join([]string{
				fmt.Sprintf("%s %-7s %-25s %s", marker, clip(item.Symbol, 7), clip(opportunityDirection(entity.Opportunity{LongExchange: item.LongExchange, ShortExchange: item.ShortExchange}), 25), statusText(item.Status)),
				fmt.Sprintf("    plan %-28s live %-3s auto_close %-3s", clip(item.PlanKey, 28), boolWord(item.LiveTrading), boolWord(item.AutoClose)),
				fmt.Sprintf("    open %s  close %s", clip(fmtTime(item.OpenedAtMs), 19), clip(fmtTime(item.ClosedAtMs), 19)),
			}, "\n")
			if i == selected {
				block = ui.activeRow.Render(block)
			}
			lines = append(lines, block)
		}
		return ui.panel.Width(width).Height(height).Render(strings.Join(lines, "\n"))
	}

	if len(m.data.AllPlans) == 0 {
		if m.isLoading(loadAllPlans) {
			lines = append(lines, ui.subtle.Render("Loading plans..."))
			return ui.panel.Width(width).Height(height).Render(strings.Join(lines, "\n"))
		}
		lines = append(lines, ui.subtle.Render("No plans available."))
		return ui.panel.Width(width).Height(height).Render(strings.Join(lines, "\n"))
	}
	rowHeight := 3
	visibleRows := maxInt(1, (height-5)/rowHeight)
	selected := m.indexOfPlan(m.selectedPlanKey)
	if selected < 0 {
		selected = 0
	}
	start := clampOffset(m.planOffset, len(m.data.AllPlans), visibleRows)
	end := minInt(len(m.data.AllPlans), start+visibleRows)
	for i := start; i < end; i++ {
		item := m.data.AllPlans[i]
		marker := selectedMarker(i == selected)
		block := strings.Join([]string{
			fmt.Sprintf("%s %-7s %-25s %10s", marker, clip(item.Symbol, 7), clip(opportunityDirection(entity.Opportunity{LongExchange: item.LongExchange, ShortExchange: item.ShortExchange}), 25), fmtMoney(item.NetExpectedPNL, 3)),
			fmt.Sprintf("    %-18s ready %-3s status %-18s", clip(opportunityPair(entity.Opportunity{LongExchange: item.LongExchange, ShortExchange: item.ShortExchange}), 18), boolWord(item.ReadyNow), clip(statusText(item.Status), 18)),
			fmt.Sprintf("    plan %-36s", clip(item.PlanKey, 36)),
		}, "\n")
		if i == selected {
			block = ui.activeRow.Render(block)
		}
		lines = append(lines, block)
	}
	return ui.panel.Width(width).Height(height).Render(strings.Join(lines, "\n"))
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
	return ui.panel.Width(width).Height(height).Render(strings.Join(lines, "\n"))
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
			return ui.panel.Width(width).Height(height).Render(strings.Join(lines, "\n"))
		}
		lines = append(lines, ui.subtle.Render("No connector status has been reported yet."))
		return ui.panel.Width(width).Height(height).Render(strings.Join(lines, "\n"))
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
	return ui.panel.Width(width).Height(height).Render(strings.Join(lines, "\n"))
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
		"",
		"Watchlist: " + clip(strings.Join(m.data.System.Watchlist, ", "), width-8),
		"DeepScan: " + clip(strings.Join(m.data.System.DeepScanWatchlist, ", "), width-8),
	}
	if m.isLoading(loadSystem) && len(m.data.System.Watchlist) == 0 && len(m.data.System.DeepScanWatchlist) == 0 {
		lines = append(lines, "", ui.subtle.Render("Loading strategy and execution config..."))
	}
	return ui.panel.Width(width).Height(height).Render(strings.Join(lines, "\n"))
}

func (m Model) renderOverviewDetail(item OpportunityListItem, plan entity.ExecutionPlan, hasPlan bool, rec entity.ExecutionRecord, hasRec bool, width int) string {
	planLabel, _ := m.opportunityPlanLabel(item)
	lines := []string{
		fmt.Sprintf("%s  %s", ui.accent.Render(item.Symbol), ui.subtle.Render(opportunityDirection(item))),
		fmt.Sprintf("Net=%s  NetBps=%s  Carry=%s  HourlyEdge=%s  Basis=%s",
			fmtMoney(item.NetExpectedPNL, 3),
			fmtSignedBps(item.NetExpectedBps, 2),
			fmtPctRatio(fundingSpread(item), 5),
			fmtPctRatio(fundingSpreadHourly(item), 5),
			fmtSignedBps(item.BasisBps, 2),
		),
		fmt.Sprintf("Status=%s  Eligible=%s  Plan=%s  Hold=%s",
			statusText(item.Status),
			boolWord(item.EligibleForExecution),
			planLabel,
			holdingDurationText(item),
		),
		fmt.Sprintf("Funding ETA long=%s  short=%s  projected=%s",
			fmtDuration(time.Until(time.UnixMilli(item.LongFundingTimeMs))),
			fmtDuration(time.Until(time.UnixMilli(item.ShortFundingTimeMs))),
			fmtTime(item.ProjectedFundingTimeMs),
		),
		fmt.Sprintf("Produced=%s  batch=%s  settle_window=%sh  funding_mode=%s",
			fmtTime(opportunityProducedTime(item).UnixMilli()),
			orDefault(item.BatchID, "--"),
			fmtNumber(item.FundingWindowHours, 2),
			orDefault(item.FundingComputationMode, "--"),
		),
		"",
		"Market Snapshot",
		renderMarketSummary(m.data.Market, width),
	}
	if hasPlan {
		lines = append(lines, "",
			"Linked Plan",
			fmt.Sprintf("plan=%s  ready=%s  status=%s  pnl=%s  target_notional=%s  leverage=%sx",
				clip(plan.PlanKey, 28),
				boolWord(plan.ReadyNow),
				statusText(plan.Status),
				fmtMoney(plan.NetExpectedPNL, 3),
				fmtMoney(targetNotional(plan), 2),
				fmtNumber(plan.TargetLeverage, 2),
			),
		)
	}
	if hasRec {
		lines = append(lines,
			fmt.Sprintf("record=%s  live=%s  open_orders=%d  close_orders=%d  reason=%s",
				statusText(rec.Status),
				boolWord(rec.LiveTrading),
				rec.OpenOrderCount,
				rec.CloseOrderCount,
				clip(orDefault(rec.StatusReason, "--"), width-24),
			),
		)
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderLegsDetail(item OpportunityListItem, detail *entity.Opportunity, hasDetail bool, loading bool, width int) string {
	lines := []string{
		fmt.Sprintf("%s  venue=%s  funding=%s  future=%s  hourly=%s  next=%s  interval=%sh",
			ui.good.Render("LONG"),
			orDefault(item.LongVenueSymbol, "--"),
			fmtPctRatio(item.LongFundingRate, 5),
			fmtPctRatio(item.LongFutureFundingRate, 5),
			fmtPctRatio(item.LongFundingHourly, 5),
			fmtTime(item.LongFundingTimeMs),
			fmtNumber(float64(item.LongFundingIntervalHours), 0),
		),
		fmt.Sprintf("  bid=%s  ask=%s  mark=%s", priceText(item.LongBidPrice), priceText(item.LongAskPrice), priceText(item.LongMarkPrice)),
		"",
		fmt.Sprintf("%s  venue=%s  funding=%s  future=%s  hourly=%s  next=%s  interval=%sh",
			ui.warn.Render("SHORT"),
			orDefault(item.ShortVenueSymbol, "--"),
			fmtPctRatio(item.ShortFundingRate, 5),
			fmtPctRatio(item.ShortFutureFundingRate, 5),
			fmtPctRatio(item.ShortFundingHourly, 5),
			fmtTime(item.ShortFundingTimeMs),
			fmtNumber(float64(item.ShortFundingIntervalHours), 0),
		),
		fmt.Sprintf("  bid=%s  ask=%s  mark=%s", priceText(item.ShortBidPrice), priceText(item.ShortAskPrice), priceText(item.ShortMarkPrice)),
		"",
	}
	switch {
	case hasDetail:
		lines = append(lines,
			fmt.Sprintf("Long rule: %s", clip(ruleSummary(detail.LongFundingRule), width)),
			fmt.Sprintf("Short rule: %s", clip(ruleSummary(detail.ShortFundingRule), width)),
		)
	case loading:
		lines = append(lines, ui.subtle.Render("Loading funding rule detail..."))
	default:
		lines = append(lines, ui.subtle.Render("Funding rule detail is not available yet."))
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderPlanDetail(item OpportunityListItem, plan entity.ExecutionPlan, hasPlan bool, rec entity.ExecutionRecord, hasRec bool, width int) string {
	if !hasPlan {
		return strings.Join([]string{
			ui.subtle.Render("No real execution plan is currently linked to this opportunity."),
			fmt.Sprintf("Opportunity status=%s  eligible=%s  reject_reason=%s", statusText(item.Status), boolWord(item.EligibleForExecution), orDefault(item.RejectReason, "--")),
		}, "\n")
	}
	lines := []string{
		fmt.Sprintf("plan=%s", plan.PlanKey),
		fmt.Sprintf("status=%s  ready=%s  pnl=%s  score=%s", statusText(plan.Status), boolWord(plan.ReadyNow), fmtMoney(plan.NetExpectedPNL, 3), fmtNumber(plan.Score, 2)),
		fmt.Sprintf("capital=%s  target_notional=%s  rounded_notional=%s  leverage=%sx", fmtMoney(plan.CapitalAllocatedUSDT, 2), fmtMoney(plan.TargetNotionalUSDT, 2), fmtMoney(plan.RoundedNotionalUSDT, 2), fmtNumber(plan.TargetLeverage, 2)),
		fmt.Sprintf("qty long=%s @ %s  short=%s @ %s", fmtNumber(plan.LongQty, 6), priceText(plan.LongEntryPrice), fmtNumber(plan.ShortQty, 6), priceText(plan.ShortEntryPrice)),
		fmt.Sprintf("basis=%s  skew=%s  target_close=%s  entry_open=%s  entry_close=%s", fmtSignedBps(plan.CrossVenueBasisBps, 2), fmtSignedBps(planPositionSkewBps(plan), 2), fmtTime(plan.TargetCloseTimeMs), fmtTime(plan.EntryWindowOpenMs), fmtTime(plan.EntryWindowCloseMs)),
	}
	if hasRec {
		lines = append(lines,
			fmt.Sprintf("record=%s  live=%s  open_orders=%d  close_orders=%d", statusText(rec.Status), boolWord(rec.LiveTrading), rec.OpenOrderCount, rec.CloseOrderCount),
		)
		if strings.TrimSpace(rec.LastError) != "" {
			lines = append(lines, "last_error="+clip(rec.LastError, width))
		}
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderProjectionDetail(_ OpportunityListItem, detail *entity.Opportunity, hasDetail bool, loading bool, width int) string {
	if !hasDetail {
		if loading {
			return ui.subtle.Render("Loading projection detail...")
		}
		return ui.subtle.Render("Projection detail is not available for this opportunity.")
	}
	if len(detail.ProjectionDetails) == 0 {
		return ui.subtle.Render("No projection detail is available for this opportunity.")
	}
	lines := []string{"rank  funding_time          window   long short  carry       hourly      net_pnl"}
	for _, row := range detail.ProjectionDetails {
		lines = append(lines, fmt.Sprintf("%-5d %-20s %-8s %-5d %-5d %-11s %-11s %-10s",
			row.ProjectionRank,
			clip(fmtTime(row.ProjectedFundingTimeMs), 20),
			clip(fmt.Sprintf("%sh", fmtNumber(row.FundingWindowHours, 2)), 8),
			row.LongFundingEventCount,
			row.ShortFundingEventCount,
			fmtPctRatio(row.CarryRate, 5),
			fmtPctRatio(row.CarryRateHourlyEquivalent, 5),
			fmtMoney(row.NetExpectedPNL, 3),
		))
	}
	return clip(strings.Join(lines, "\n"), width*maxInt(1, len(lines)))
}

func (m Model) renderOrdersDetail(planKey string, width int) string {
	if strings.TrimSpace(planKey) == "" {
		return ui.subtle.Render("No real plan is selected, so there is no order stream to show.")
	}
	if m.ordersLoading[planKey] {
		return ui.subtle.Render("Loading order history...")
	}
	items, ok := m.orders[planKey]
	if !ok || len(items) == 0 {
		return ui.subtle.Render("No orders were found for this plan key yet.")
	}
	lines := []string{"phase   leg      exchange     side   status              requested        executed         price"}
	for _, order := range items {
		lines = append(lines, fmt.Sprintf("%-7s %-8s %-12s %-6s %-18s %-15s %-15s %-10s",
			clip(order.Phase, 7),
			clip(order.LegRole, 8),
			clip(order.Exchange, 12),
			clip(order.Side, 6),
			clip(order.Status, 18),
			fmtNumber(order.RequestedQty, 6),
			fmtNumber(order.ExecutedQty, 6),
			priceText(order.AvgPrice),
		))
		if strings.TrimSpace(order.ErrorMessage) != "" {
			lines = append(lines, "  err="+clip(order.ErrorMessage, width))
		}
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderPlanExecutionDetail(plan entity.ExecutionPlan, rec entity.ExecutionRecord, hasRec bool, width int) string {
	lines := []string{
		fmt.Sprintf("%s  %s", ui.accent.Render(plan.Symbol), ui.subtle.Render(opportunityDirection(entity.Opportunity{LongExchange: plan.LongExchange, ShortExchange: plan.ShortExchange}))),
		fmt.Sprintf("plan=%s  ready=%s  status=%s  pnl=%s", plan.PlanKey, boolWord(plan.ReadyNow), statusText(plan.Status), fmtMoney(plan.NetExpectedPNL, 3)),
		fmt.Sprintf("notional=%s  leverage=%sx  basis=%s  target_close=%s", fmtMoney(targetNotional(plan), 2), fmtNumber(plan.TargetLeverage, 2), fmtSignedBps(plan.CrossVenueBasisBps, 2), fmtTime(plan.TargetCloseTimeMs)),
		fmt.Sprintf("long=%s qty=%s @ %s", plan.LongVenueSymbol, fmtNumber(plan.LongQty, 6), priceText(plan.LongEntryPrice)),
		fmt.Sprintf("short=%s qty=%s @ %s", plan.ShortVenueSymbol, fmtNumber(plan.ShortQty, 6), priceText(plan.ShortEntryPrice)),
	}
	if opp, ok := m.matchingOpportunityForPlan(plan); ok {
		lines = append(lines, fmt.Sprintf("matched_opp=%s  opp_status=%s  hold=%s", opportunityKey(opp), statusText(opp.Status), holdingDurationText(opp)))
	}
	if hasRec {
		lines = append(lines, fmt.Sprintf("record=%s  live=%s  auto_close=%s  opened=%s  closed=%s", statusText(rec.Status), boolWord(rec.LiveTrading), boolWord(rec.AutoClose), fmtTime(rec.OpenedAtMs), fmtTime(rec.ClosedAtMs)))
		if strings.TrimSpace(rec.StatusReason) != "" {
			lines = append(lines, "reason="+clip(rec.StatusReason, width))
		}
		if strings.TrimSpace(rec.LastError) != "" {
			lines = append(lines, "last_error="+clip(rec.LastError, width))
		}
	}
	lines = append(lines, "", "Orders", m.renderOrdersDetail(plan.PlanKey, width))
	return strings.Join(lines, "\n")
}

func (m Model) renderExecutionRecordDetail(rec entity.ExecutionRecord, plan entity.ExecutionPlan, hasPlan bool, width int) string {
	lines := []string{
		fmt.Sprintf("%s  %s", ui.accent.Render(rec.Symbol), ui.subtle.Render(opportunityDirection(entity.Opportunity{LongExchange: rec.LongExchange, ShortExchange: rec.ShortExchange}))),
		fmt.Sprintf("plan=%s  status=%s  live=%s  auto_close=%s", rec.PlanKey, statusText(rec.Status), boolWord(rec.LiveTrading), boolWord(rec.AutoClose)),
		fmt.Sprintf("opened=%s  closed=%s  transition=%s  event=%s", fmtTime(rec.OpenedAtMs), fmtTime(rec.ClosedAtMs), fmtTime(rec.LastTransitionAtMs), orDefault(rec.LastTransitionEvent, "--")),
		fmt.Sprintf("open_orders=%d  close_orders=%d", rec.OpenOrderCount, rec.CloseOrderCount),
	}
	if strings.TrimSpace(rec.StatusReason) != "" {
		lines = append(lines, "reason="+clip(rec.StatusReason, width))
	}
	if strings.TrimSpace(rec.LastError) != "" {
		lines = append(lines, "last_error="+clip(rec.LastError, width))
	}
	if hasPlan {
		lines = append(lines, fmt.Sprintf("plan_status=%s  expected_pnl=%s  target_close=%s", statusText(plan.Status), fmtMoney(plan.NetExpectedPNL, 3), fmtTime(plan.TargetCloseTimeMs)))
	}
	lines = append(lines, "", "Orders", m.renderOrdersDetail(rec.PlanKey, width))
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
	return ui.modal.Width(minInt(m.width-4, 96)).Height(minInt(height, 18)).Render(strings.Join(lines, "\n"))
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
	return ui.modal.Width(minInt(m.width-4, 88)).Height(minInt(height, 14)).Render(strings.Join(lines, "\n"))
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
			return "plan ready", "good"
		}
		return "plan staged", "warn"
	}
	if item.EligibleForExecution {
		return "plan candidate", "good"
	}
	return "no plan", "bad"
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
		return ui.subtle.Render("No market snapshot for the active symbol.")
	}
	lines := []string{"exchange      venue           bid          ask          mid          mark         funding       next_funding"}
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
		fmt.Sprintf("interval=%sh", fmtNumber(float64(rule.FundingIntervalHours), 0)),
		"next=" + fmtTime(rule.NextFundingTimeMs),
		"rate=" + fmtPctRatio(rule.CurrentFundingRate, 5),
	}
	if strings.TrimSpace(rule.ForecastConfidence) != "" {
		parts = append(parts, "confidence="+rule.ForecastConfidence)
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
		return "eligible"
	case "watching":
		return "watching"
	case "spread_too_small":
		return "spread_too_small"
	case "basis_too_wide":
		return "basis_too_wide"
	case "not_profitable":
		return "not_profitable"
	case "stale_data":
		return "stale_data"
	case "outside_entry_window":
		return "outside_entry_window"
	case "settlement_window_passed":
		return "settlement_window_passed"
	case "pending_open":
		return "pending_open"
	case "opened":
		return "opened"
	case "open_partial_failed":
		return "open_partial_failed"
	case "open_failed":
		return "open_failed"
	case "open_hedging":
		return "open_hedging"
	case "risk_blocked":
		return "risk_blocked"
	case "api_circuit_open", "circuit_open":
		return "circuit_open"
	case "pending_close":
		return "pending_close"
	case "closed":
		return "closed"
	case "close_partial_failed":
		return "close_partial_failed"
	case "close_failed":
		return "close_failed"
	case "close_hedging":
		return "close_hedging"
	case "dry_run_opened":
		return "dry_run_opened"
	case "dry_run_closed":
		return "dry_run_closed"
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
	default:
		return ui.subtle
	}
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
