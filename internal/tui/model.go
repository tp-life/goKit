package tui

import (
	"context"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"goKit/internal/application/service"
	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type OpportunityListItem = repository.OpportunitySummary

type mainView int

const (
	viewScanner mainView = iota
	viewExecution
	viewSystem
)

func (v mainView) String() string {
	switch v {
	case viewExecution:
		return "Execution"
	case viewSystem:
		return "System"
	default:
		return "Scanner"
	}
}

type scannerTab int

const (
	tabOverview scannerTab = iota
	tabLegs
	tabPlan
	tabProjection
	tabOrders
)

func (t scannerTab) String() string {
	switch t {
	case tabLegs:
		return "Legs"
	case tabPlan:
		return "Plan"
	case tabProjection:
		return "Projection"
	case tabOrders:
		return "Orders"
	default:
		return "Overview"
	}
}

type sortMode int

const (
	sortByNet sortMode = iota
	sortByScore
	sortByEdge
)

func (s sortMode) String() string {
	switch s {
	case sortByScore:
		return "score"
	case sortByEdge:
		return "edge"
	default:
		return "net"
	}
}

func (s sortMode) next() sortMode {
	switch s {
	case sortByNet:
		return sortByScore
	case sortByScore:
		return sortByEdge
	default:
		return sortByNet
	}
}

type executionListMode int

const (
	execPlans executionListMode = iota
	execRecords
)

func (m executionListMode) String() string {
	if m == execRecords {
		return "Executions"
	}
	return "Plans"
}

type actionType string

const (
	actionOpen  actionType = "OPEN"
	actionClose actionType = "CLOSE"
)

type actionTarget struct {
	PlanKey        string
	Symbol         string
	LongExchange   string
	ShortExchange  string
	NetExpectedPNL float64
	Status         string
	LiveTrading    bool
	Source         string
}

type confirmState struct {
	Action     actionType
	Target     actionTarget
	Input      textinput.Model
	Submitting bool
	ErrorText  string
}

type systemLoadedMsg struct {
	System SystemStatus
}

type systemErrMsg struct {
	Err error
}

type opportunitiesLoadedMsg struct {
	Items []OpportunityListItem
}

type opportunitiesErrMsg struct {
	Err error
}

type executionsLoadedMsg struct {
	Items []entity.ExecutionRecord
}

type executionsErrMsg struct {
	Err error
}

type statsLoadedMsg struct {
	Stats repository.SnapshotStats
}

type statsErrMsg struct {
	Err error
}

type plansLoadedMsg struct {
	Items   []entity.ExecutionPlan
	BatchID string
	Scoped  bool
}

type plansErrMsg struct {
	BatchID string
	Scoped  bool
	Err     error
}

type marketLoadedMsg struct {
	Symbol   string
	Snapshot service.SymbolMarketState
}

type marketErrMsg struct {
	Symbol string
	Err    error
}

type ordersLoadedMsg struct {
	PlanKey string
	Orders  []entity.OrderRecord
}

type ordersErrMsg struct {
	PlanKey string
	Err     error
}

type opportunityDetailLoadedMsg struct {
	ID   uint
	Item entity.Opportunity
}

type opportunityDetailErrMsg struct {
	ID  uint
	Err error
}

type actionDoneMsg struct {
	Action  actionType
	PlanKey string
}

type actionErrMsg struct {
	Action  actionType
	PlanKey string
	Err     error
}

type tickMsg time.Time

type refreshLoadedMsg struct {
	Seq           int
	System        SystemStatus
	Opportunities []OpportunityListItem
	Executions    []entity.ExecutionRecord
	Stats         repository.SnapshotStats
	AllPlans      []entity.ExecutionPlan
	BatchPlans    []entity.ExecutionPlan
	CurrentBatch  string
}

type refreshErrMsg struct {
	Seq int
	Err error
}

const (
	loadSystem        = "system"
	loadOpportunities = "opportunities"
	loadExecutions    = "executions"
	loadStats         = "stats"
	loadAllPlans      = "plans"
	loadBatchPlans    = "batch-plans"
	loadMarket        = "market"
)

const (
	tuiHeaderLines  = 4
	tuiFooterLines  = 1
	listHeaderLines = 5
	listRowHeight   = 3
)

type Model struct {
	client          *Client
	refreshInterval time.Duration

	width  int
	height int

	view    mainView
	tab     scannerTab
	execTab executionListMode
	sort    sortMode

	loading  bool
	showHelp bool

	lastRefresh time.Time
	lastError   string
	flash       string

	searchMode bool
	search     textinput.Model

	confirm *confirmState

	data DashboardData

	pairFilter               string
	selectedOpportunityKey   string
	selectedPlanKey          string
	selectedExecutionPlanKey string
	opportunityAutoFollow    bool
	opportunityOffset        int
	planOffset               int
	executionOffset          int
	marketLoadingSymbol      string

	orders                   map[string][]entity.OrderRecord
	ordersLoading            map[string]bool
	opportunityDetails       map[uint]entity.Opportunity
	opportunityDetailLoading map[uint]bool
	loadingSections          map[string]bool
	refreshSeq               int
}

func NewModel(client *Client, refreshInterval time.Duration) Model {
	search := textinput.New()
	search.Prompt = "/ "
	search.Placeholder = "Search symbol / exchange / venue"
	search.CharLimit = 120
	search.Width = 40

	return Model{
		client:                   client,
		refreshInterval:          refreshInterval,
		view:                     viewScanner,
		tab:                      tabOverview,
		execTab:                  execPlans,
		sort:                     sortByNet,
		loading:                  true,
		search:                   search,
		opportunityAutoFollow:    true,
		orders:                   make(map[string][]entity.OrderRecord),
		ordersLoading:            make(map[string]bool),
		opportunityDetails:       make(map[uint]entity.Opportunity),
		opportunityDetailLoading: make(map[uint]bool),
		loadingSections:          make(map[string]bool),
	}
}

func (m Model) Init() tea.Cmd {
	cmds := m.startRefreshCmds()
	cmds = append(cmds, tickCmd(m.refreshInterval))
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case refreshLoadedMsg:
		if msg.Seq != m.refreshSeq {
			return m, nil
		}
		m.data.System = msg.System
		m.data.Opportunities = msg.Opportunities
		m.data.Executions = msg.Executions
		m.data.Stats = msg.Stats
		m.data.AllPlans = msg.AllPlans
		m.data.BatchPlans = msg.BatchPlans
		m.data.CurrentBatchID = strings.TrimSpace(msg.CurrentBatch)
		m.finishLoading(loadSystem)
		m.finishLoading(loadOpportunities)
		m.finishLoading(loadExecutions)
		m.finishLoading(loadStats)
		m.finishLoading(loadAllPlans)
		m.finishLoading(loadBatchPlans)
		m.pruneOpportunityDetails()
		m.normalizeSelections()
		return m, tea.Batch(m.postSelectionCmds()...)
	case refreshErrMsg:
		if msg.Seq != m.refreshSeq {
			return m, nil
		}
		m.finishLoading(loadSystem)
		m.finishLoading(loadOpportunities)
		m.finishLoading(loadExecutions)
		m.finishLoading(loadStats)
		m.finishLoading(loadAllPlans)
		m.finishLoading(loadBatchPlans)
		m.lastError = msg.Err.Error()
		return m, nil
	case systemLoadedMsg:
		m.data.System = msg.System
		m.finishLoading(loadSystem)
		m.normalizeSelections()
		return m, tea.Batch(m.postSelectionCmds()...)
	case systemErrMsg:
		m.finishLoading(loadSystem)
		m.lastError = msg.Err.Error()
		return m, nil
	case opportunitiesLoadedMsg:
		prevBatchID := strings.TrimSpace(m.data.CurrentBatchID)
		m.data.Opportunities = msg.Items
		m.data.CurrentBatchID = ""
		if len(msg.Items) > 0 {
			m.data.CurrentBatchID = strings.TrimSpace(msg.Items[0].BatchID)
		}
		m.finishLoading(loadOpportunities)
		m.pruneOpportunityDetails()
		m.normalizeSelections()
		if batchID := strings.TrimSpace(m.data.CurrentBatchID); batchID != "" && batchID != prevBatchID {
			m.data.BatchPlans = nil
		}
		return m, tea.Batch(m.postSelectionCmds()...)
	case opportunitiesErrMsg:
		m.finishLoading(loadOpportunities)
		m.lastError = msg.Err.Error()
		return m, nil
	case executionsLoadedMsg:
		m.data.Executions = msg.Items
		m.finishLoading(loadExecutions)
		m.normalizeSelections()
		return m, tea.Batch(m.postSelectionCmds()...)
	case executionsErrMsg:
		m.finishLoading(loadExecutions)
		m.lastError = msg.Err.Error()
		return m, nil
	case statsLoadedMsg:
		m.data.Stats = msg.Stats
		m.finishLoading(loadStats)
		return m, nil
	case statsErrMsg:
		m.finishLoading(loadStats)
		m.lastError = msg.Err.Error()
		return m, nil
	case plansLoadedMsg:
		if msg.Scoped {
			if strings.TrimSpace(msg.BatchID) == strings.TrimSpace(m.data.CurrentBatchID) {
				m.data.BatchPlans = msg.Items
			}
			m.finishLoading(loadBatchPlans)
			return m, nil
		}
		m.data.AllPlans = msg.Items
		m.finishLoading(loadAllPlans)
		m.normalizeSelections()
		return m, tea.Batch(m.postSelectionCmds()...)
	case plansErrMsg:
		if msg.Scoped {
			m.finishLoading(loadBatchPlans)
		} else {
			m.finishLoading(loadAllPlans)
		}
		m.lastError = msg.Err.Error()
		return m, nil
	case marketLoadedMsg:
		if strings.EqualFold(strings.TrimSpace(msg.Symbol), strings.TrimSpace(m.marketLoadingSymbol)) {
			m.marketLoadingSymbol = ""
			m.finishLoading(loadMarket)
		}
		if strings.EqualFold(strings.TrimSpace(msg.Symbol), m.currentSymbol()) || strings.TrimSpace(m.data.Market.Symbol) == "" {
			m.data.Market = msg.Snapshot
		}
		return m, nil
	case marketErrMsg:
		if strings.EqualFold(strings.TrimSpace(msg.Symbol), strings.TrimSpace(m.marketLoadingSymbol)) {
			m.marketLoadingSymbol = ""
			m.finishLoading(loadMarket)
		}
		if strings.EqualFold(strings.TrimSpace(msg.Symbol), m.currentSymbol()) {
			m.lastError = msg.Err.Error()
		}
		return m, nil
	case ordersLoadedMsg:
		m.orders[msg.PlanKey] = msg.Orders
		delete(m.ordersLoading, msg.PlanKey)
		return m, nil
	case ordersErrMsg:
		delete(m.ordersLoading, msg.PlanKey)
		m.lastError = msg.Err.Error()
		return m, nil
	case opportunityDetailLoadedMsg:
		m.opportunityDetails[msg.ID] = msg.Item
		delete(m.opportunityDetailLoading, msg.ID)
		return m, nil
	case opportunityDetailErrMsg:
		delete(m.opportunityDetailLoading, msg.ID)
		m.lastError = msg.Err.Error()
		return m, nil
	case actionDoneMsg:
		m.flash = fmt.Sprintf("%s %s succeeded", msg.Action, msg.PlanKey)
		m.lastError = ""
		m.confirm = nil
		return m, tea.Batch(m.startRefreshCmds()...)
	case actionErrMsg:
		if m.confirm != nil {
			m.confirm.Submitting = false
			m.confirm.ErrorText = msg.Err.Error()
			return m, nil
		}
		m.lastError = msg.Err.Error()
		return m, nil
	case tickMsg:
		cmds := m.startRefreshCmds()
		cmds = append(cmds, tickCmd(m.refreshInterval))
		return m, tea.Batch(cmds...)
	}

	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	if m.confirm != nil {
		return m.updateConfirm(keyMsg)
	}

	if m.searchMode {
		return m.updateSearch(keyMsg)
	}

	switch keyMsg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "?":
		m.showHelp = !m.showHelp
		return m, nil
	case "esc":
		if m.showHelp {
			m.showHelp = false
		}
		return m, nil
	case "r":
		return m, tea.Batch(m.startRefreshCmds()...)
	case "/":
		m.searchMode = true
		m.search.Focus()
		return m, nil
	case "s":
		m.sort = m.sort.next()
		m.opportunityAutoFollow = true
		m.opportunityOffset = 0
		m.normalizeSelections()
		return m, tea.Batch(m.postSelectionCmds()...)
	case "f":
		m.cyclePairFilter(1)
		m.opportunityAutoFollow = true
		m.opportunityOffset = 0
		return m, tea.Batch(m.postSelectionCmds()...)
	case "F":
		m.cyclePairFilter(-1)
		m.opportunityAutoFollow = true
		m.opportunityOffset = 0
		return m, tea.Batch(m.postSelectionCmds()...)
	case "1", "m":
		m.view = viewScanner
		return m, tea.Batch(m.postSelectionCmds()...)
	case "2", "e":
		m.view = viewExecution
		return m, tea.Batch(m.postSelectionCmds()...)
	case "3":
		m.view = viewSystem
		return m, nil
	case "tab", "l":
		switch m.view {
		case viewScanner:
			m.tab = (m.tab + 1) % 5
			return m, tea.Batch(m.postSelectionCmds()...)
		case viewExecution:
			m.execTab = (m.execTab + 1) % 2
			m.normalizeSelections()
			return m, tea.Batch(m.postSelectionCmds()...)
		default:
			return m, nil
		}
	case "shift+tab", "h":
		switch m.view {
		case viewScanner:
			m.tab = (m.tab + 4) % 5
			return m, tea.Batch(m.postSelectionCmds()...)
		case viewExecution:
			m.execTab = (m.execTab + 1) % 2
			m.normalizeSelections()
			return m, tea.Batch(m.postSelectionCmds()...)
		default:
			return m, nil
		}
	case "p":
		if m.view == viewExecution {
			m.execTab = execPlans
			m.normalizeSelections()
			return m, tea.Batch(m.postSelectionCmds()...)
		}
	case "x":
		if m.view == viewExecution {
			m.execTab = execRecords
			m.normalizeSelections()
			return m, tea.Batch(m.postSelectionCmds()...)
		}
	case "j", "down":
		return m.moveSelection(1)
	case "k", "up":
		return m.moveSelection(-1)
	case "g", "home":
		return m.jumpSelection(0)
	case "G", "end":
		return m.jumpSelection(-1)
	case "o":
		return m.startConfirm(actionOpen)
	case "c":
		return m.startConfirm(actionClose)
	}

	if m.showHelp && keyMsg.String() == "enter" {
		m.showHelp = false
	}

	return m, nil
}

func (m Model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "enter":
		m.searchMode = false
		m.search.Blur()
		m.opportunityAutoFollow = true
		m.normalizeSelections()
		return m, tea.Batch(m.postSelectionCmds()...)
	}

	var cmd tea.Cmd
	m.search, cmd = m.search.Update(msg)
	m.opportunityAutoFollow = true
	m.opportunityOffset = 0
	m.normalizeSelections()
	return m, tea.Batch(cmd, tea.Batch(m.postSelectionCmds()...))
}

func (m Model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.confirm == nil {
		return m, nil
	}
	switch msg.String() {
	case "esc":
		m.confirm = nil
		return m, nil
	case "enter":
		if m.confirm.Submitting {
			return m, nil
		}
		word := strings.TrimSpace(m.confirm.Input.Value())
		if !strings.EqualFold(word, string(m.confirm.Action)) {
			m.confirm.ErrorText = fmt.Sprintf("type %s to confirm", m.confirm.Action)
			return m, nil
		}
		m.confirm.Submitting = true
		m.confirm.ErrorText = ""
		return m, performActionCmd(m.client, m.confirm.Action, m.confirm.Target.PlanKey)
	}

	var cmd tea.Cmd
	m.confirm.Input, cmd = m.confirm.Input.Update(msg)
	m.confirm.ErrorText = ""
	return m, cmd
}

func (m Model) moveSelection(delta int) (tea.Model, tea.Cmd) {
	switch m.view {
	case viewScanner:
		items := m.filteredOpportunities()
		if len(items) == 0 {
			return m, nil
		}
		m.opportunityAutoFollow = false
		idx := m.indexOfOpportunity(items, m.selectedOpportunityKey)
		idx = clampIndex(idx+delta, len(items))
		m.selectedOpportunityKey = opportunityKey(items[idx])
		m.ensureOpportunityVisible(items, idx)
		return m, tea.Batch(m.postSelectionCmds()...)
	case viewExecution:
		if m.execTab == execRecords {
			if len(m.data.Executions) == 0 {
				return m, nil
			}
			idx := m.indexOfExecution(m.selectedExecutionPlanKey)
			idx = clampIndex(idx+delta, len(m.data.Executions))
			m.selectedExecutionPlanKey = m.data.Executions[idx].PlanKey
			m.ensureExecutionRecordVisible(idx)
			return m, tea.Batch(m.postSelectionCmds()...)
		}
		if len(m.data.AllPlans) == 0 {
			return m, nil
		}
		idx := m.indexOfPlan(m.selectedPlanKey)
		idx = clampIndex(idx+delta, len(m.data.AllPlans))
		m.selectedPlanKey = m.data.AllPlans[idx].PlanKey
		m.ensureExecutionPlanVisible(idx)
		return m, tea.Batch(m.postSelectionCmds()...)
	default:
		return m, nil
	}
}

func (m Model) jumpSelection(target int) (tea.Model, tea.Cmd) {
	switch m.view {
	case viewScanner:
		items := m.filteredOpportunities()
		if len(items) == 0 {
			return m, nil
		}
		m.opportunityAutoFollow = false
		idx := target
		if idx < 0 {
			idx = len(items) - 1
		}
		m.selectedOpportunityKey = opportunityKey(items[idx])
		if idx == 0 {
			m.opportunityOffset = 0
		} else {
			m.ensureOpportunityVisible(items, idx)
		}
		return m, tea.Batch(m.postSelectionCmds()...)
	case viewExecution:
		if m.execTab == execRecords {
			if len(m.data.Executions) == 0 {
				return m, nil
			}
			idx := target
			if idx < 0 {
				idx = len(m.data.Executions) - 1
			}
			m.selectedExecutionPlanKey = m.data.Executions[idx].PlanKey
			if idx == 0 {
				m.executionOffset = 0
			} else {
				m.ensureExecutionRecordVisible(idx)
			}
			return m, tea.Batch(m.postSelectionCmds()...)
		}
		if len(m.data.AllPlans) == 0 {
			return m, nil
		}
		idx := target
		if idx < 0 {
			idx = len(m.data.AllPlans) - 1
		}
		m.selectedPlanKey = m.data.AllPlans[idx].PlanKey
		if idx == 0 {
			m.planOffset = 0
		} else {
			m.ensureExecutionPlanVisible(idx)
		}
		return m, tea.Batch(m.postSelectionCmds()...)
	default:
		return m, nil
	}
}

func (m *Model) startConfirm(action actionType) (tea.Model, tea.Cmd) {
	target, ok := m.currentActionTarget()
	if !ok {
		m.lastError = "no executable plan is selected"
		return *m, nil
	}
	input := textinput.New()
	input.Prompt = "> "
	input.Placeholder = string(action)
	input.CharLimit = 16
	input.Width = 16
	input.Focus()
	m.confirm = &confirmState{
		Action: action,
		Target: target,
		Input:  input,
	}
	return *m, nil
}

func (m *Model) normalizeSelections() {
	items := m.filteredOpportunities()
	if len(items) == 0 {
		m.selectedOpportunityKey = ""
		m.opportunityOffset = 0
		m.opportunityAutoFollow = true
	} else if m.opportunityAutoFollow {
		m.selectedOpportunityKey = opportunityKey(items[0])
		m.opportunityOffset = 0
	} else if m.indexOfOpportunity(items, m.selectedOpportunityKey) < 0 {
		m.selectedOpportunityKey = opportunityKey(items[0])
		m.opportunityOffset = 0
	}
	if idx := m.indexOfOpportunity(items, m.selectedOpportunityKey); idx >= 0 {
		m.ensureOpportunityVisible(items, idx)
	}

	if len(m.data.AllPlans) == 0 {
		m.selectedPlanKey = ""
		m.planOffset = 0
	} else if m.indexOfPlan(m.selectedPlanKey) < 0 {
		m.selectedPlanKey = m.data.AllPlans[0].PlanKey
		m.planOffset = 0
	}
	if idx := m.indexOfPlan(m.selectedPlanKey); idx >= 0 {
		m.ensureExecutionPlanVisible(idx)
	}

	if len(m.data.Executions) == 0 {
		m.selectedExecutionPlanKey = ""
		m.executionOffset = 0
	} else if m.indexOfExecution(m.selectedExecutionPlanKey) < 0 {
		m.selectedExecutionPlanKey = m.data.Executions[0].PlanKey
		m.executionOffset = 0
	}
	if idx := m.indexOfExecution(m.selectedExecutionPlanKey); idx >= 0 {
		m.ensureExecutionRecordVisible(idx)
	}

	if !slices.Contains(m.pairOptions(), m.pairFilter) {
		m.pairFilter = ""
	}
}

func (m *Model) pruneOpportunityDetails() {
	if len(m.opportunityDetails) == 0 && len(m.opportunityDetailLoading) == 0 {
		return
	}
	keep := make(map[uint]struct{}, len(m.data.Opportunities))
	for _, item := range m.data.Opportunities {
		if item.ID > 0 {
			keep[item.ID] = struct{}{}
		}
	}
	for id := range m.opportunityDetails {
		if _, ok := keep[id]; !ok {
			delete(m.opportunityDetails, id)
		}
	}
	for id := range m.opportunityDetailLoading {
		if _, ok := keep[id]; !ok {
			delete(m.opportunityDetailLoading, id)
		}
	}
}

func (m *Model) startLoading(keys ...string) {
	for _, key := range keys {
		if strings.TrimSpace(key) == "" {
			continue
		}
		m.loadingSections[key] = true
	}
	m.loading = len(m.loadingSections) > 0
}

func (m *Model) finishLoading(key string) {
	if strings.TrimSpace(key) != "" {
		delete(m.loadingSections, key)
	}
	m.loading = len(m.loadingSections) > 0
	if !m.loading {
		m.lastRefresh = time.Now()
		if strings.TrimSpace(m.lastError) == "" {
			m.flash = fmt.Sprintf("Refreshed %s", m.lastRefresh.Format("15:04:05"))
		}
	}
}

func (m Model) isLoading(key string) bool {
	return m.loadingSections[key]
}

func (m Model) loadingSummary() string {
	if len(m.loadingSections) == 0 {
		return ""
	}
	order := []string{
		loadSystem,
		loadOpportunities,
		loadExecutions,
		loadStats,
		loadAllPlans,
		loadBatchPlans,
		loadMarket,
	}
	labels := map[string]string{
		loadSystem:        "system",
		loadOpportunities: "opps",
		loadExecutions:    "exec",
		loadStats:         "stats",
		loadAllPlans:      "plans",
		loadBatchPlans:    "batch",
		loadMarket:        "market",
	}
	parts := make([]string, 0, len(m.loadingSections))
	for _, key := range order {
		if m.loadingSections[key] {
			parts = append(parts, labels[key])
		}
	}
	return strings.Join(parts, ",")
}

func (m *Model) startRefreshCmds() []tea.Cmd {
	m.lastError = ""
	m.refreshSeq++
	seq := m.refreshSeq
	m.startLoading(
		loadSystem,
		loadOpportunities,
		loadExecutions,
		loadStats,
		loadAllPlans,
		loadBatchPlans,
	)
	return []tea.Cmd{fetchRefreshCmd(m.client, seq)}
}

func (m *Model) startMarketRefreshCmd(symbol string) tea.Cmd {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return nil
	}
	if strings.EqualFold(m.marketLoadingSymbol, symbol) {
		return nil
	}
	m.marketLoadingSymbol = symbol
	m.startLoading(loadMarket)
	return fetchMarketCmd(m.client, symbol)
}

func (m Model) bodyHeight() int {
	available := m.height - tuiHeaderLines - tuiFooterLines - ui.doc.GetVerticalFrameSize()
	return maxInt(1, available)
}

func (m Model) scannerListHeight() int {
	bodyHeight := m.bodyHeight()
	if m.width < 120 {
		listHeight, _ := splitStackedHeights(bodyHeight)
		return listHeight
	}
	return bodyHeight
}

func (m Model) executionListHeight() int {
	bodyHeight := m.bodyHeight()
	if m.width < 120 {
		listHeight, _ := splitStackedHeights(bodyHeight)
		return listHeight
	}
	return bodyHeight
}

func (m Model) scannerVisibleRows() int {
	return maxInt(1, panelListContentHeight(m.scannerListHeight())/listRowHeight)
}

func (m Model) executionVisibleRows() int {
	return maxInt(1, panelListContentHeight(m.executionListHeight())/listRowHeight)
}

func panelOuterHeight(height int) int {
	return maxInt(1, height)
}

func panelContentHeight(height int) int {
	return maxInt(1, panelOuterHeight(height)-ui.panel.GetVerticalFrameSize())
}

func panelContentWidth(width int) int {
	return maxInt(1, width-ui.panel.GetHorizontalFrameSize())
}

func panelListContentHeight(height int) int {
	return maxInt(1, panelContentHeight(height)-listHeaderLines)
}

func modalContentWidth(width int) int {
	return maxInt(1, width-ui.modal.GetHorizontalFrameSize())
}

func modalContentHeight(height int) int {
	return maxInt(1, height-ui.modal.GetVerticalFrameSize())
}

func splitStackedHeights(total int) (top int, bottom int) {
	total = maxInt(1, total)
	if total == 1 {
		return 1, 0
	}

	top = total / 2
	bottom = total - top

	const preferredMinPanelHeight = 8
	if total >= preferredMinPanelHeight*2 {
		if top < preferredMinPanelHeight {
			top = preferredMinPanelHeight
			bottom = total - top
		}
		if bottom < preferredMinPanelHeight {
			bottom = preferredMinPanelHeight
			top = total - bottom
		}
	}

	if top <= 0 {
		top = 1
		bottom = total - top
	}
	if bottom < 0 {
		bottom = 0
	}
	return top, bottom
}

func (m *Model) ensureOpportunityVisible(items []OpportunityListItem, idx int) {
	visible := m.scannerVisibleRows()
	m.opportunityOffset = adjustOffsetForSelection(m.opportunityOffset, idx, len(items), visible)
}

func (m *Model) ensureExecutionPlanVisible(idx int) {
	visible := m.executionVisibleRows()
	m.planOffset = adjustOffsetForSelection(m.planOffset, idx, len(m.data.AllPlans), visible)
}

func (m *Model) ensureExecutionRecordVisible(idx int) {
	visible := m.executionVisibleRows()
	m.executionOffset = adjustOffsetForSelection(m.executionOffset, idx, len(m.data.Executions), visible)
}

func adjustOffsetForSelection(offset, idx, total, visible int) int {
	if total <= 0 || visible <= 0 || total <= visible {
		return 0
	}
	offset = clampOffset(offset, total, visible)
	idx = clampIndex(idx, total)
	if idx < offset {
		offset = idx
	}
	if idx >= offset+visible {
		offset = idx - visible + 1
	}
	return clampOffset(offset, total, visible)
}

func clampOffset(offset, total, visible int) int {
	if total <= 0 || visible <= 0 || total <= visible {
		return 0
	}
	maxOffset := total - visible
	if offset < 0 {
		return 0
	}
	if offset > maxOffset {
		return maxOffset
	}
	return offset
}

func (m *Model) postSelectionCmds() []tea.Cmd {
	cmds := make([]tea.Cmd, 0, 3)
	if cmd := m.ensureMarketCmd(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if cmd := m.ensureOrdersCmd(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if cmd := m.ensureOpportunityDetailCmd(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return cmds
}

func (m *Model) ensureMarketCmd() tea.Cmd {
	symbol := m.currentSymbol()
	if strings.TrimSpace(symbol) == "" {
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(m.data.Market.Symbol), symbol) && !m.isLoading(loadMarket) {
		return nil
	}
	return m.startMarketRefreshCmd(symbol)
}

func (m *Model) ensureOrdersCmd() tea.Cmd {
	planKey := m.activeOrdersPlanKey()
	if strings.TrimSpace(planKey) == "" {
		return nil
	}
	if _, ok := m.orders[planKey]; ok {
		return nil
	}
	if m.ordersLoading[planKey] {
		return nil
	}
	m.ordersLoading[planKey] = true
	return fetchOrdersCmd(m.client, planKey)
}

func (m *Model) ensureOpportunityDetailCmd() tea.Cmd {
	summary, ok := m.selectedOpportunity()
	if !ok {
		return nil
	}
	if _, ok := m.opportunityDetails[summary.ID]; ok {
		return nil
	}
	if m.opportunityDetailLoading[summary.ID] {
		return nil
	}
	m.opportunityDetailLoading[summary.ID] = true
	return fetchOpportunityDetailCmd(m.client, summary.ID)
}

func (m Model) currentSymbol() string {
	if opp, ok := m.selectedOpportunity(); ok {
		return strings.ToUpper(strings.TrimSpace(opp.Symbol))
	}
	if strings.TrimSpace(m.data.SelectedMarketSym) != "" {
		return strings.ToUpper(strings.TrimSpace(m.data.SelectedMarketSym))
	}
	if len(m.data.System.Watchlist) > 0 {
		return strings.ToUpper(strings.TrimSpace(m.data.System.Watchlist[0]))
	}
	return strings.ToUpper(strings.TrimSpace(m.data.Market.Symbol))
}

func (m Model) selectedOpportunity() (OpportunityListItem, bool) {
	items := m.filteredOpportunities()
	if len(items) == 0 {
		return OpportunityListItem{}, false
	}
	idx := m.indexOfOpportunity(items, m.selectedOpportunityKey)
	if idx < 0 {
		idx = 0
	}
	return items[idx], true
}

func (m Model) selectedOpportunityDetail() (*entity.Opportunity, bool) {
	summary, ok := m.selectedOpportunity()
	if !ok {
		return nil, false
	}
	item, ok := m.opportunityDetails[summary.ID]
	if !ok {
		return nil, false
	}
	return &item, true
}

func (m Model) selectedPlan() (entity.ExecutionPlan, bool) {
	for _, plan := range m.data.AllPlans {
		if plan.PlanKey == m.selectedPlanKey {
			return plan, true
		}
	}
	return entity.ExecutionPlan{}, false
}

func (m Model) selectedExecution() (entity.ExecutionRecord, bool) {
	for _, rec := range m.data.Executions {
		if rec.PlanKey == m.selectedExecutionPlanKey {
			return rec, true
		}
	}
	return entity.ExecutionRecord{}, false
}

func (m Model) currentActionTarget() (actionTarget, bool) {
	switch m.view {
	case viewScanner:
		opp, ok := m.selectedOpportunity()
		if !ok {
			return actionTarget{}, false
		}
		plan, ok := m.bestPlanForOpportunity(opp)
		if !ok {
			return actionTarget{}, false
		}
		rec, _ := m.executionByPlanKey(plan.PlanKey)
		return actionTarget{
			PlanKey:        plan.PlanKey,
			Symbol:         plan.Symbol,
			LongExchange:   plan.LongExchange,
			ShortExchange:  plan.ShortExchange,
			NetExpectedPNL: plan.NetExpectedPNL,
			Status:         plan.Status,
			LiveTrading:    rec.LiveTrading,
			Source:         "scanner",
		}, true
	case viewExecution:
		if m.execTab == execRecords {
			rec, ok := m.selectedExecution()
			if !ok {
				return actionTarget{}, false
			}
			target := actionTarget{
				PlanKey:       rec.PlanKey,
				Symbol:        rec.Symbol,
				LongExchange:  rec.LongExchange,
				ShortExchange: rec.ShortExchange,
				Status:        rec.Status,
				LiveTrading:   rec.LiveTrading,
				Source:        "execution-record",
			}
			if plan, ok := m.planByKey(rec.PlanKey); ok {
				target.NetExpectedPNL = plan.NetExpectedPNL
			}
			return target, true
		}
		plan, ok := m.selectedPlan()
		if !ok {
			return actionTarget{}, false
		}
		rec, _ := m.executionByPlanKey(plan.PlanKey)
		return actionTarget{
			PlanKey:        plan.PlanKey,
			Symbol:         plan.Symbol,
			LongExchange:   plan.LongExchange,
			ShortExchange:  plan.ShortExchange,
			NetExpectedPNL: plan.NetExpectedPNL,
			Status:         plan.Status,
			LiveTrading:    rec.LiveTrading,
			Source:         "execution-plan",
		}, true
	default:
		return actionTarget{}, false
	}
}

func (m Model) activeOrdersPlanKey() string {
	switch m.view {
	case viewScanner:
		if m.tab != tabPlan && m.tab != tabOrders {
			return ""
		}
		opp, ok := m.selectedOpportunity()
		if !ok {
			return ""
		}
		plan, ok := m.bestPlanForOpportunity(opp)
		if !ok {
			return ""
		}
		return plan.PlanKey
	case viewExecution:
		if m.execTab == execRecords {
			return strings.TrimSpace(m.selectedExecutionPlanKey)
		}
		return strings.TrimSpace(m.selectedPlanKey)
	default:
		return ""
	}
}

func (m Model) cyclePairFilter(delta int) {
	options := m.pairOptions()
	if len(options) == 0 {
		m.pairFilter = ""
		return
	}
	idx := slices.Index(options, m.pairFilter)
	if idx < 0 {
		idx = 0
	}
	idx += delta
	for idx < 0 {
		idx += len(options)
	}
	idx = idx % len(options)
	next := options[idx]
	if next == "all" {
		m.pairFilter = ""
	} else {
		m.pairFilter = next
	}
}

func (m Model) pairOptions() []string {
	if len(m.data.Opportunities) == 0 {
		return []string{"all"}
	}
	pairs := map[string]struct{}{}
	for _, item := range m.data.Opportunities {
		pairs[opportunityPair(item)] = struct{}{}
	}
	out := make([]string, 0, len(pairs)+1)
	out = append(out, "all")
	for pair := range pairs {
		out = append(out, pair)
	}
	sort.Strings(out[1:])
	return out
}

func (m Model) filteredOpportunities() []OpportunityListItem {
	searchText := strings.ToUpper(strings.TrimSpace(m.search.Value()))
	pairFilter := strings.TrimSpace(m.pairFilter)
	items := make([]OpportunityListItem, 0, len(m.data.Opportunities))
	for _, item := range m.data.Opportunities {
		if pairFilter != "" && opportunityPair(item) != pairFilter {
			continue
		}
		if searchText != "" {
			haystack := strings.ToUpper(strings.Join([]string{
				item.Symbol,
				item.LongExchange,
				item.ShortExchange,
				item.LongVenueSymbol,
				item.ShortVenueSymbol,
				opportunityPair(item),
				opportunityDirection(item),
			}, " "))
			if !strings.Contains(haystack, searchText) {
				continue
			}
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		p1 := m.opportunityPriority(items[i])
		p2 := m.opportunityPriority(items[j])
		if p1 != p2 {
			return p1 > p2
		}
		switch m.sort {
		case sortByScore:
			return items[i].Score > items[j].Score
		case sortByEdge:
			return items[i].GrossEdgeHourly > items[j].GrossEdgeHourly
		default:
			return items[i].NetExpectedPNL > items[j].NetExpectedPNL
		}
	})
	return items
}

func (m Model) opportunityPriority(item OpportunityListItem) int {
	if plan, ok := m.bestPlanForOpportunity(item); ok {
		if plan.ReadyNow {
			return 3
		}
		return 2
	}
	if item.EligibleForExecution {
		return 1
	}
	return 0
}

func (m Model) bestPlanForOpportunity(item OpportunityListItem) (entity.ExecutionPlan, bool) {
	plans := m.matchingPlansForOpportunity(item)
	if len(plans) == 0 {
		return entity.ExecutionPlan{}, false
	}
	sort.Slice(plans, func(i, j int) bool {
		if plans[i].ReadyNow != plans[j].ReadyNow {
			return plans[i].ReadyNow && !plans[j].ReadyNow
		}
		return plans[i].NetExpectedPNL > plans[j].NetExpectedPNL
	})
	return plans[0], true
}

func (m Model) matchingPlansForOpportunity(item OpportunityListItem) []entity.ExecutionPlan {
	out := make([]entity.ExecutionPlan, 0)
	for _, plan := range m.data.AllPlans {
		sameCore := plan.Symbol == item.Symbol &&
			plan.LongExchange == item.LongExchange &&
			plan.ShortExchange == item.ShortExchange &&
			plan.LongVenueSymbol == item.LongVenueSymbol &&
			plan.ShortVenueSymbol == item.ShortVenueSymbol
		if !sameCore {
			continue
		}
		if item.BatchID != "" && plan.OpportunityBatchID != "" && item.BatchID != plan.OpportunityBatchID {
			continue
		}
		out = append(out, plan)
	}
	return out
}

func (m Model) matchingOpportunityForPlan(plan entity.ExecutionPlan) (OpportunityListItem, bool) {
	candidates := make([]OpportunityListItem, 0)
	for _, item := range m.data.Opportunities {
		sameCore := plan.Symbol == item.Symbol &&
			plan.LongExchange == item.LongExchange &&
			plan.ShortExchange == item.ShortExchange &&
			plan.LongVenueSymbol == item.LongVenueSymbol &&
			plan.ShortVenueSymbol == item.ShortVenueSymbol
		if !sameCore {
			continue
		}
		if item.BatchID != "" && plan.OpportunityBatchID != "" && item.BatchID != plan.OpportunityBatchID {
			continue
		}
		candidates = append(candidates, item)
	}
	if len(candidates) == 0 {
		return OpportunityListItem{}, false
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].NetExpectedPNL > candidates[j].NetExpectedPNL
	})
	return candidates[0], true
}

func (m Model) planByKey(planKey string) (entity.ExecutionPlan, bool) {
	for _, plan := range m.data.AllPlans {
		if plan.PlanKey == planKey {
			return plan, true
		}
	}
	for _, plan := range m.data.BatchPlans {
		if plan.PlanKey == planKey {
			return plan, true
		}
	}
	return entity.ExecutionPlan{}, false
}

func (m Model) executionByPlanKey(planKey string) (entity.ExecutionRecord, bool) {
	for _, rec := range m.data.Executions {
		if rec.PlanKey == planKey {
			return rec, true
		}
	}
	return entity.ExecutionRecord{}, false
}

func (m Model) indexOfOpportunity(items []OpportunityListItem, key string) int {
	for i, item := range items {
		if opportunityKey(item) == key {
			return i
		}
	}
	return -1
}

func (m Model) indexOfPlan(planKey string) int {
	for i, plan := range m.data.AllPlans {
		if plan.PlanKey == planKey {
			return i
		}
	}
	return -1
}

func (m Model) indexOfExecution(planKey string) int {
	for i, rec := range m.data.Executions {
		if rec.PlanKey == planKey {
			return i
		}
	}
	return -1
}

func clampIndex(idx int, length int) int {
	if length == 0 {
		return 0
	}
	if idx < 0 {
		return 0
	}
	if idx >= length {
		return length - 1
	}
	return idx
}

func fetchRefreshCmd(client *Client, seq int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
		defer cancel()

		var (
			system   SystemStatus
			opps     []OpportunityListItem
			execs    []entity.ExecutionRecord
			stats    repository.SnapshotStats
			allPlans []entity.ExecutionPlan
		)

		var (
			wg       sync.WaitGroup
			errOnce  sync.Once
			firstErr error
		)
		setErr := func(err error) {
			if err == nil {
				return
			}
			errOnce.Do(func() {
				firstErr = err
				cancel()
			})
		}

		wg.Add(5)
		go func() {
			defer wg.Done()
			var err error
			system, err = client.getSystemStatus(ctx)
			setErr(err)
		}()
		go func() {
			defer wg.Done()
			var err error
			opps, err = client.getOpportunitySummaries(ctx, client.opportunityLimit)
			setErr(err)
		}()
		go func() {
			defer wg.Done()
			var err error
			execs, err = client.getExecutions(ctx, defaultExecutionLimit)
			setErr(err)
		}()
		go func() {
			defer wg.Done()
			var err error
			stats, err = client.getSnapshotStats(ctx)
			setErr(err)
		}()
		go func() {
			defer wg.Done()
			var err error
			allPlans, err = client.getPlans(ctx, defaultPlanLimit, "")
			setErr(err)
		}()
		wg.Wait()

		if firstErr != nil {
			return refreshErrMsg{Seq: seq, Err: firstErr}
		}

		currentBatch := ""
		if len(opps) > 0 {
			currentBatch = strings.TrimSpace(opps[0].BatchID)
		}

		var batchPlans []entity.ExecutionPlan
		if currentBatch != "" {
			items, err := client.getPlans(ctx, defaultPlanLimit, currentBatch)
			if err != nil {
				return refreshErrMsg{Seq: seq, Err: err}
			}
			batchPlans = items
		}

		return refreshLoadedMsg{
			Seq:           seq,
			System:        system,
			Opportunities: opps,
			Executions:    execs,
			Stats:         stats,
			AllPlans:      allPlans,
			BatchPlans:    batchPlans,
			CurrentBatch:  currentBatch,
		}
	}
}

func fetchSystemCmd(client *Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		system, err := client.getSystemStatus(ctx)
		if err != nil {
			return systemErrMsg{Err: err}
		}
		return systemLoadedMsg{System: system}
	}
}

func fetchOpportunitySummariesCmd(client *Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		items, err := client.getOpportunitySummaries(ctx, client.opportunityLimit)
		if err != nil {
			return opportunitiesErrMsg{Err: err}
		}
		return opportunitiesLoadedMsg{Items: items}
	}
}

func fetchExecutionsCmd(client *Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		items, err := client.getExecutions(ctx, defaultExecutionLimit)
		if err != nil {
			return executionsErrMsg{Err: err}
		}
		return executionsLoadedMsg{Items: items}
	}
}

func fetchStatsCmd(client *Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		stats, err := client.getSnapshotStats(ctx)
		if err != nil {
			return statsErrMsg{Err: err}
		}
		return statsLoadedMsg{Stats: stats}
	}
}

func fetchPlansCmd(client *Client, limit int, batchID string, scoped bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		items, err := client.getPlans(ctx, limit, batchID)
		if err != nil {
			return plansErrMsg{BatchID: batchID, Scoped: scoped, Err: err}
		}
		return plansLoadedMsg{Items: items, BatchID: batchID, Scoped: scoped}
	}
}

func fetchMarketCmd(client *Client, symbol string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		snapshot, err := client.GetMarket(ctx, symbol)
		if err != nil {
			return marketErrMsg{Symbol: symbol, Err: err}
		}
		return marketLoadedMsg{Symbol: symbol, Snapshot: snapshot}
	}
}

func fetchOrdersCmd(client *Client, planKey string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		items, err := client.GetOrders(ctx, planKey)
		if err != nil {
			return ordersErrMsg{PlanKey: planKey, Err: err}
		}
		return ordersLoadedMsg{PlanKey: planKey, Orders: items}
	}
}

func fetchOpportunityDetailCmd(client *Client, id uint) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		item, err := client.GetOpportunityDetail(ctx, id)
		if err != nil {
			return opportunityDetailErrMsg{ID: id, Err: err}
		}
		return opportunityDetailLoadedMsg{ID: id, Item: item}
	}
}

func performActionCmd(client *Client, action actionType, planKey string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		var err error
		switch action {
		case actionClose:
			err = client.ClosePlan(ctx, planKey)
		default:
			err = client.OpenPlan(ctx, planKey)
		}
		if err != nil {
			return actionErrMsg{Action: action, PlanKey: planKey, Err: err}
		}
		return actionDoneMsg{Action: action, PlanKey: planKey}
	}
}

func tickCmd(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func opportunityKey(item any) string {
	symbol, longExchange, shortExchange, longVenueSymbol, shortVenueSymbol := opportunityIdentity(item)
	return strings.Join([]string{
		strings.ToUpper(symbol),
		strings.ToLower(longExchange),
		strings.ToLower(shortExchange),
		strings.ToUpper(longVenueSymbol),
		strings.ToUpper(shortVenueSymbol),
	}, "|")
}

func opportunityPair(item any) string {
	_, longExchange, shortExchange, _, _ := opportunityIdentity(item)
	return fmt.Sprintf("%s -> %s", longExchange, shortExchange)
}

func opportunityDirection(item any) string {
	_, longExchange, shortExchange, _, _ := opportunityIdentity(item)
	return fmt.Sprintf("%s long / %s short", longExchange, shortExchange)
}

func opportunityIdentity(item any) (symbol, longExchange, shortExchange, longVenueSymbol, shortVenueSymbol string) {
	switch v := item.(type) {
	case entity.Opportunity:
		return v.Symbol, v.LongExchange, v.ShortExchange, v.LongVenueSymbol, v.ShortVenueSymbol
	case OpportunityListItem:
		return v.Symbol, v.LongExchange, v.ShortExchange, v.LongVenueSymbol, v.ShortVenueSymbol
	default:
		return "", "", "", "", ""
	}
}

func fundingSpread(item any) float64 {
	switch v := item.(type) {
	case entity.Opportunity:
		if len(v.ProjectionDetails) > 0 && v.ProjectionDetails[0].CarryRate != 0 {
			return v.ProjectionDetails[0].CarryRate
		}
		return v.ShortFundingRate - v.LongFundingRate
	case OpportunityListItem:
		return v.ShortFundingRate - v.LongFundingRate
	default:
		return 0
	}
}

func fundingSpreadHourly(item any) float64 {
	switch v := item.(type) {
	case entity.Opportunity:
		return v.GrossEdgeHourly
	case OpportunityListItem:
		return v.GrossEdgeHourly
	default:
		return 0
	}
}

func holdingDurationText(item any) string {
	switch v := item.(type) {
	case entity.Opportunity:
		if v.ProjectedFundingTimeMs > 0 {
			return fmtDuration(time.Until(time.UnixMilli(v.ProjectedFundingTimeMs)))
		}
		if v.FundingWindowHours > 0 {
			return fmtDuration(time.Duration(v.FundingWindowHours * float64(time.Hour)))
		}
	case OpportunityListItem:
		if v.ProjectedFundingTimeMs > 0 {
			return fmtDuration(time.Until(time.UnixMilli(v.ProjectedFundingTimeMs)))
		}
		if v.FundingWindowHours > 0 {
			return fmtDuration(time.Duration(v.FundingWindowHours * float64(time.Hour)))
		}
	}
	return "--"
}

func opportunityProducedTime(item any) time.Time {
	switch v := item.(type) {
	case entity.Opportunity:
		if v.AsOfTimeMs > 0 {
			return time.UnixMilli(v.AsOfTimeMs)
		}
		return v.CreatedAt
	case OpportunityListItem:
		if v.AsOfTimeMs > 0 {
			return time.UnixMilli(v.AsOfTimeMs)
		}
		return v.CreatedAt
	default:
		return time.Time{}
	}
}

func targetNotional(plan entity.ExecutionPlan) float64 {
	if plan.TargetNotionalUSDT > 0 {
		return plan.TargetNotionalUSDT
	}
	return plan.RoundedNotionalUSDT
}

func planLongNotional(plan entity.ExecutionPlan) float64 {
	if plan.LongQty > 0 && plan.LongEntryPrice > 0 {
		return plan.LongQty * plan.LongEntryPrice
	}
	return 0
}

func planShortNotional(plan entity.ExecutionPlan) float64 {
	if plan.ShortQty > 0 && plan.ShortEntryPrice > 0 {
		return plan.ShortQty * plan.ShortEntryPrice
	}
	return 0
}

func planPositionSkewBps(plan entity.ExecutionPlan) float64 {
	longNotional := math.Abs(planLongNotional(plan))
	shortNotional := math.Abs(planShortNotional(plan))
	avg := (longNotional + shortNotional) / 2
	if avg == 0 {
		return 0
	}
	return math.Abs(longNotional-shortNotional) / avg * 10000
}

func executionExpectedPnL(plan entity.ExecutionPlan, ok bool) string {
	if !ok {
		return "--"
	}
	return fmtMoney(plan.NetExpectedPNL, 3)
}
