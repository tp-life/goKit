// ------------------------------------------------------------
// 页面级状态。
// 这里只保存“当前页面需要展示”的数据，不承担后端业务逻辑。
// ------------------------------------------------------------
const state = {
  symbols: [],
  activeSymbol: "BTC",
  opportunities: [],
  plans: [],
  batchPlans: [],
  allPlans: [],
  executions: [],
  system: null,
  stats: null,
  refreshMs: 8000,
  selectedOpportunityKey: null,
  selectedPlanKey: null,
  currentOpportunityBatchId: "",
  planViewMode: "batch",
};

const OPPORTUNITY_FETCH_LIMIT = 5000;

// ------------------------------------------------------------
// DOM 引用集中管理，避免后续维护时到处 querySelector。
// ------------------------------------------------------------
const els = {
  healthLabel: document.getElementById("health-label"),
  lastRefreshLabel: document.getElementById("last-refresh-label"),
  metricOpps: document.getElementById("metric-opps"),
  metricReady: document.getElementById("metric-ready"),
  metricFunding: document.getElementById("metric-funding-snapshots"),
  metricBook: document.getElementById("metric-book-snapshots"),
  watchlistCount: document.getElementById("watchlist-count"),
  connectorCount: document.getElementById("connector-count"),
  watchlistChips: document.getElementById("watchlist-chips"),
  strategySummary: document.getElementById("strategy-summary"),
  executionSummary: document.getElementById("execution-summary"),
  symbolFilter: document.getElementById("symbol-filter"),
  exchangeFilter: document.getElementById("exchange-filter"),
  sortMode: document.getElementById("sort-mode"),
  marketSelect: document.getElementById("market-symbol-select"),
  activeSymbolLabel: document.getElementById("active-symbol-label"),
  marketCards: document.getElementById("market-cards"),
  statusStack: document.getElementById("status-stack"),
  opportunitiesList: document.getElementById("opportunities-list"),
  opportunitySummary: document.getElementById("opportunity-summary"),
  opportunityDetail: document.getElementById("opportunity-detail"),
  opportunitiesEmpty: document.getElementById("opportunities-empty"),
  plansList: document.getElementById("plans-list"),
  plansEmpty: document.getElementById("plans-empty"),
  planViewMode: document.getElementById("plan-view-mode"),
  executionsList: document.getElementById("executions-list"),
  executionsEmpty: document.getElementById("executions-empty"),
  manualRefreshBtn: document.getElementById("manual-refresh-btn"),
};

// ------------------------------------------------------------
// 通用 API 方法。
// 后端统一返回 {code,message,data} 时，这里只取 data。
// ------------------------------------------------------------
async function apiGet(path, fallback = null) {
  try {
    const res = await fetch(path, { headers: { Accept: "application/json" } });
    if (!res.ok) return fallback;
    const json = await res.json();
    return json && typeof json === "object" && "data" in json
      ? json.data
      : json;
  } catch (err) {
    console.warn("api get failed", path, err);
    return fallback;
  }
}

async function apiPost(path, payload = {}) {
  const res = await fetch(path, {
    method: "POST",
    headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: JSON.stringify(payload),
  });
  const json = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(json?.message || `HTTP ${res.status}`);
  return json && typeof json === "object" && "data" in json ? json.data : json;
}

// ------------------------------------------------------------
// 基础格式化函数。
// ------------------------------------------------------------
function fmtNumber(value, digits = 2) {
  const n = Number(value);
  if (!Number.isFinite(n)) return "--";
  return n.toLocaleString("zh-CN", {
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  });
}

function fmtPctRatio(value, digits = 4) {
  const n = Number(value);
  if (!Number.isFinite(n)) return "--";
  return `${fmtNumber(n * 100, digits)}%`;
}

function fmtMoney(value, digits = 3) {
  const n = Number(value);
  if (!Number.isFinite(n)) return "--";
  return `${n >= 0 ? "+" : ""}${fmtNumber(n, digits)} USDT`;
}

function fmtSignedBps(value, digits = 2) {
  const n = Number(value);
  if (!Number.isFinite(n)) return "--";
  return `${n >= 0 ? "+" : ""}${fmtNumber(n, digits)} bps`;
}

function fmtTime(value) {
  const n = Number(value);
  if (!Number.isFinite(n) || n <= 0) return "--";
  return new Date(n).toLocaleString("zh-CN", { hour12: false });
}

function fmtDuration(ms) {
  const n = Number(ms);
  if (!Number.isFinite(n)) return "--";
  if (n <= 0) return "最近结算点已过";

  const totalMinutes = Math.floor(n / 60000);
  const days = Math.floor(totalMinutes / 1440);
  const hours = Math.floor((totalMinutes % 1440) / 60);
  const mins = totalMinutes % 60;
  const chunks = [];
  if (days) chunks.push(`${days}天`);
  if (hours || days) chunks.push(`${hours}小时`);
  chunks.push(`${mins}分钟`);
  return chunks.join(" ");
}

function priceText(value) {
  const n = Number(value);
  if (!Number.isFinite(n)) return "--";
  if (n >= 1000) return fmtNumber(n, 2);
  if (n >= 1) return fmtNumber(n, 4);
  return fmtNumber(n, 6);
}

function classForNumber(value) {
  const n = Number(value);
  if (!Number.isFinite(n)) return "muted-text";
  if (n > 0) return "positive";
  if (n < 0) return "negative";
  return "muted-text";
}

function statusClass(status) {
  const st = String(status || "").toLowerCase();
  if (["eligible", "ready", "opened", "dry_run_opened"].includes(st))
    return "good";
  if (["watching", "outside_entry_window", "stale_data"].includes(st))
    return "warn";
  return "bad";
}

function boolText(value) {
  return value ? "是" : "否";
}

function midpoint(bid, ask) {
  const b = Number(bid);
  const a = Number(ask);
  if (!Number.isFinite(b) || !Number.isFinite(a) || b <= 0 || a <= 0)
    return null;
  return (b + a) / 2;
}

// ------------------------------------------------------------
// 后端字段兼容层。
// 后续后端字段如果略有变化，只需要在这里兜底。
// ------------------------------------------------------------
function normalizeFundingMap(payload) {
  return payload?.funding || payload?.Funding || {};
}

function normalizeBookMap(payload) {
  return payload?.book_top || payload?.bookTop || payload?.BookTop || {};
}

// ------------------------------------------------------------
// 机会列表相关辅助函数。
// ------------------------------------------------------------
function opportunityKey(item) {
  if (!item || typeof item !== "object") return "";
  // 选中态必须跨 batch 稳定：
  // 不能依赖 id / batch_id（每轮刷新都会变），否则详情会被“强制跳回默认项”。
  // 这里使用机会核心维度作为稳定 key。
  return [
    String(item.symbol || "").toUpperCase(),
    String(item.long_exchange || "").toLowerCase(),
    String(item.short_exchange || "").toLowerCase(),
    String(item.long_venue_symbol || "").toUpperCase(),
    String(item.short_venue_symbol || "").toUpperCase(),
  ].join("|");
}

function opportunityPair(item) {
  return `${item.long_exchange} → ${item.short_exchange}`;
}

function opportunityDirection(item) {
  return `${item.long_exchange} 做多 / ${item.short_exchange} 做空`;
}

function fundingSpread(item) {
  return (
    Number(item.short_funding_rate || 0) - Number(item.long_funding_rate || 0)
  );
}

function fundingSpreadHourly(item) {
  return Number(item.gross_edge_hourly || 0);
}

function fundingModeText(item) {
  const mode = String(item?.funding_computation_mode || "").toLowerCase();
  if (mode === "event_based_known_next_funding")
    return "按真实已知 funding 事件计算";
  return mode || "--";
}

function fundingEstimateModeText(item) {
  const mode = String(item?.funding_estimate_mode || "").toLowerCase();
  if (mode === "single_cycle_spot") return "单轮现值";
  if (mode === "multi_cycle_smoothed") return "多轮平滑";
  return mode || "--";
}

function fundingEstimateConfidenceText(item) {
  const level = String(item?.funding_estimate_confidence || "").toLowerCase();
  if (level === "high") return "高";
  if (level === "medium") return "中";
  if (level === "guarded") return "谨慎";
  return level || "--";
}

function fundingSmoothingLookbackText() {
  return state.system?.strategy?.funding_history_lookback || "--";
}

function fundingSmoothingWeightText() {
  const weight = Number(
    state.system?.strategy?.funding_smoothing_current_weight,
  );
  if (!Number.isFinite(weight)) return "--";
  return fmtNumber(weight, 2);
}

function fundingEventsText(item) {
  const longCount = Number(item?.long_funding_event_count || 0);
  const shortCount = Number(item?.short_funding_event_count || 0);
  return `Long ${longCount} 次 / Short ${shortCount} 次`;
}

function holdingDurationText(item) {
  if (!item || typeof item !== "object") return "--";
  const projectedMs = Number(item.projected_funding_time_ms || 0);
  if (projectedMs > 0) {
    return fmtDuration(projectedMs - Date.now());
  }
  const windowHours = Number(item.funding_window_hours || 0);
  if (Number.isFinite(windowHours) && windowHours > 0) {
    const totalMinutes = Math.round(windowHours * 60);
    return fmtDuration(totalMinutes * 60 * 1000);
  }
  return "--";
}

function fundingIntervalText(hours) {
  const h = Number(hours || 0);
  if (!Number.isFinite(h) || h <= 0) return "--";
  return `${fmtNumber(h, 0)}h`;
}

function strategyMaxSpreadBps() {
  return Number(state.system?.strategy?.max_spread_bps || 0);
}

function strategyTargetNotional() {
  return Number(state.system?.strategy?.effective_notional || 0);
}

function exchangeFeeConfig(exchangeName) {
  const key = String(exchangeName || "").toLowerCase();
  return state.system?.strategy?.fees_by_exchange?.[key] || null;
}

function feeRateBps(exchangeName, mode) {
  const fees = exchangeFeeConfig(exchangeName);
  if (!fees) return null;
  const makerBps = Number(fees.maker_bps || 0);
  const takerBps = Number(fees.taker_bps || 0);
  const normalizedMode = String(mode || "").toLowerCase();
  switch (normalizedMode) {
    case "maker":
      return makerBps;
    case "taker":
      return takerBps;
    case "mid":
      return (makerBps + takerBps) / 2;
    default:
      return makerBps;
  }
}

function feeModeLabel(mode) {
  const normalizedMode = String(mode || "").toLowerCase();
  switch (normalizedMode) {
    case "maker":
      return "maker";
    case "taker":
      return "taker";
    case "mid":
      return "mid";
    default:
      return normalizedMode || "--";
  }
}

function feeBreakdownText(item, mode) {
  if (!item || typeof item !== "object") return "--";
  const parts = [
    [item.long_exchange, feeRateBps(item.long_exchange, mode)],
    [item.short_exchange, feeRateBps(item.short_exchange, mode)],
  ]
    .filter(([exchangeName, bps]) => exchangeName && bps != null)
    .map(
      ([exchangeName, bps]) =>
        `${exchangeName} ${feeModeLabel(mode)} ${fmtNumber(bps, 2)} bps`,
    );
  return parts.length ? parts.join(" + ") : "--";
}

function maxAllowedBasisForItem(item) {
  const dynamic = Number(item?.max_allowed_basis_bps || 0);
  if (Number.isFinite(dynamic) && dynamic > 0) return dynamic;
  return strategyMaxSpreadBps();
}

function basisDecisionText(item) {
  const basis = Number(item?.basis_bps || 0);
  const maxSpread = maxAllowedBasisForItem(item);
  if (!Number.isFinite(maxSpread) || maxSpread <= 0) {
    return `当前 Basis=${fmtSignedBps(basis, 2)}（未配置阈值）`;
  }
  return `当前 Basis=${fmtSignedBps(basis, 2)}，阈值=${fmtSignedBps(maxSpread, 2)}，${basis > maxSpread ? "已超限" : "未超限"}`;
}

function basisTooWideHint(item) {
  if (String(item?.status || "").toLowerCase() !== "basis_too_wide") return "";
  return `当前跨所价差 ${fmtSignedBps(item?.basis_bps, 2)}（阈值 ${fmtSignedBps(maxAllowedBasisForItem(item), 2)}）`;
}

function explainOpportunityRejectReason(item) {
  const basisHint = basisTooWideHint(item);
  if (basisHint) {
    return item?.reject_reason
      ? `${item.reject_reason} · ${basisHint}`
      : basisHint;
  }
  return item?.reject_reason || explainStatus(item?.status);
}

function targetNotionalText(item, matchedPlan) {
  const planNotional = Number(
    matchedPlan?.target_notional_usdt ||
      matchedPlan?.rounded_notional_usdt ||
      0,
  );
  if (Number.isFinite(planNotional) && planNotional > 0)
    return fmtMoney(planNotional, 2);
  const fallback = strategyTargetNotional();
  if (Number.isFinite(fallback) && fallback > 0)
    return `${fmtMoney(fallback, 2)}（策略配置）`;
  return "--";
}

// ------------------------------------------------------------
// 机会与执行计划的匹配逻辑。
// 目的：让前端能明确知道“这条机会是否已经进入执行计划”，
// 并且优先把已经进入计划/可进入计划的机会排在前面展示。
// ------------------------------------------------------------
function matchingPlansForOpportunity(item) {
  if (!item) return [];
  return (state.allPlans || []).filter((plan) => {
    if (!plan) return false;
    const sameCore =
      String(plan.symbol || "") === String(item.symbol || "") &&
      String(plan.long_exchange || "") === String(item.long_exchange || "") &&
      String(plan.short_exchange || "") === String(item.short_exchange || "") &&
      String(plan.long_venue_symbol || "") ===
        String(item.long_venue_symbol || "") &&
      String(plan.short_venue_symbol || "") ===
        String(item.short_venue_symbol || "");
    if (!sameCore) return false;

    // 如果后端同时返回了机会批次和计划关联批次，则优先按批次收紧匹配。
    const oppBatch = String(item.batch_id || "");
    const planOppBatch = String(plan.opportunity_batch_id || "");
    if (oppBatch && planOppBatch) return oppBatch === planOppBatch;
    return true;
  });
}

function bestPlanForOpportunity(item) {
  const plans = matchingPlansForOpportunity(item);
  if (!plans.length) return null;
  plans.sort((a, b) => {
    if (Boolean(b.ready_now) !== Boolean(a.ready_now))
      return Number(Boolean(b.ready_now)) - Number(Boolean(a.ready_now));
    return Number(b.net_expected_pnl || 0) - Number(a.net_expected_pnl || 0);
  });
  return plans[0];
}

function matchingOpportunityForPlan(plan) {
  if (!plan) return null;
  const matches = (state.opportunities || []).filter((item) => {
    const sameCore =
      String(plan.symbol || "") === String(item.symbol || "") &&
      String(plan.long_exchange || "") === String(item.long_exchange || "") &&
      String(plan.short_exchange || "") === String(item.short_exchange || "") &&
      String(plan.long_venue_symbol || "") ===
        String(item.long_venue_symbol || "") &&
      String(plan.short_venue_symbol || "") ===
        String(item.short_venue_symbol || "");
    if (!sameCore) return false;

    const oppBatch = String(item.batch_id || "");
    const planOppBatch = String(plan.opportunity_batch_id || "");
    if (oppBatch && planOppBatch) return oppBatch === planOppBatch;
    return true;
  });
  if (!matches.length) return null;
  matches.sort(
    (a, b) => Number(b.net_expected_pnl || 0) - Number(a.net_expected_pnl || 0),
  );
  return matches[0];
}

function syncSelectedPlanFromOpportunity(item) {
  const plan = bestPlanForOpportunity(item);
  state.selectedPlanKey = plan?.plan_key || null;
}

function opportunityPriority(item) {
  const plan = bestPlanForOpportunity(item);
  if (plan && plan.ready_now) return 3;
  if (plan) return 2;
  if (item.eligible_for_execution) return 1;
  return 0;
}

function opportunityPlanStatus(item) {
  const plan = bestPlanForOpportunity(item);
  if (plan && plan.ready_now) {
    return {
      text: "已进入执行计划",
      cls: "good",
      reason: `planKey=${plan.plan_key} · 已就绪`,
    };
  }
  if (plan) {
    return {
      text: "已生成计划",
      cls: "warn",
      reason: `planKey=${plan.plan_key} · status=${plan.status || "--"}`,
    };
  }
  if (item.eligible_for_execution) {
    return {
      text: "可进入计划",
      cls: "good",
      reason: "满足机会筛选，但最新计划批次中暂未命中。",
    };
  }
  return {
    text: "未进入计划",
    cls: "bad",
    reason: explainOpportunityRejectReason(item),
  };
}

function explainStatus(status) {
  const st = String(status || "").toLowerCase();
  switch (st) {
    case "eligible":
      return "可执行";
    case "watching":
      return "观察中";
    case "spread_too_small":
      return "资金利差不足";
    case "basis_too_wide":
      return "跨所价差过大";
    case "not_profitable":
      return "净收益不足";
    case "stale_data":
      return "行情已过期";
    case "outside_entry_window":
      return "尚未进入开仓窗口";
    case "settlement_window_passed":
      return "最近结算点已过";
    default:
      return status || "--";
  }
}

function currentSelectedOpportunity(items) {
  if (!items.length) return null;
  const found = items.find(
    (item) => opportunityKey(item) === state.selectedOpportunityKey,
  );
  if (found) {
    syncSelectedPlanFromOpportunity(found);
    return found;
  }
  state.selectedOpportunityKey = opportunityKey(items[0]);
  syncSelectedPlanFromOpportunity(items[0]);
  return items[0];
}

// ------------------------------------------------------------
// 概览与系统信息渲染。
// ------------------------------------------------------------
function infoCell(label, value, extraClass = "") {
  return `
    <div class="info-cell ${extraClass}">
      <span>${label}</span>
      <strong>${value}</strong>
    </div>
  `;
}

function renderOverview() {
  // “监控池概览”模块已被移除；这里保留空函数，避免刷新链路报错。
  // 如果后续要恢复该模块，再把 watchlist / connector 摘要逻辑放回这里即可。
  if (!els.strategySummary || !els.executionSummary) return;

  const system = state.system || {};
  const strategy = system.strategy || {};
  const execution = system.execution || {};

  els.strategySummary.innerHTML = [
    infoCell("策略开关", boolText(strategy.enabled)),
    infoCell(
      "总资金",
      `${fmtNumber(strategy.capital_total_usdt || 0, 2)} USDT`,
    ),
    infoCell("资金利用率", fmtPctRatio(strategy.capital_utilization || 0, 2)),
    infoCell(
      "有效名义价值",
      `${fmtNumber(strategy.effective_notional || 0, 2)} USDT`,
    ),
    infoCell("杠杆", `${fmtNumber(strategy.leverage || 0, 2)}x`),
    infoCell("最小净收益", `${fmtNumber(strategy.min_net_pnl || 0, 3)} USDT`),
    infoCell("最大价差", fmtSignedBps(strategy.max_spread_bps || 0, 2)),
    infoCell("最大数据年龄", strategy.max_data_age || "--"),
    infoCell("入场提前", strategy.entry_lead_time || "--"),
    infoCell("入场截止", strategy.entry_cutoff_time || "--"),
  ].join("");

  els.executionSummary.innerHTML = [
    infoCell(
      "实盘开关",
      boolText(execution.live_trading_enabled),
      execution.live_trading_enabled ? "warn" : "good",
    ),
    infoCell("自动开仓", boolText(execution.auto_entry)),
    infoCell("自动平仓", boolText(execution.auto_close)),
    infoCell("轮询间隔", execution.loop_interval || "--"),
    infoCell("平仓保护期", execution.close_grace_period || "--"),
    infoCell("最新计划窗口", String(execution.max_latest_plans || 0)),
  ].join("");
}

function renderMetrics() {
  els.metricOpps.textContent = String(state.opportunities.length || 0);
  els.metricReady.textContent = String(
    state.opportunities.filter((item) => item.eligible_for_execution).length ||
      0,
  );
  els.metricFunding.textContent = String(
    state.stats?.funding_count_24h ?? state.stats?.funding_snapshots_24h ?? 0,
  );
  els.metricBook.textContent = String(
    state.stats?.book_top_count_24h ?? state.stats?.book_top_snapshots_24h ?? 0,
  );
}

function renderSystem() {
  const connectors = state.system?.connectors || [];
  els.healthLabel.textContent = connectors.length ? "RUNNING" : "NO DATA";
  els.statusStack.innerHTML =
    connectors
      .map((item) => {
        const healthy = item.mark_price_connected || item.book_ticker_connected;
        return `
          <div class="status-item ${healthy ? "positive" : "negative"}">
            <div class="status-title-row">
              <div class="status-title">${item.exchange}</div>
              <span class="pill ${healthy ? "good" : "bad"}">${healthy ? "已连通" : "异常"}</span>
            </div>
            <div class="status-desc">market=${item.mark_price_connected ? "on" : "off"} · book=${item.book_ticker_connected ? "on" : "off"}</div>
            <div class="status-desc">market_at=${fmtTime(Date.parse(item.last_market_event_at || ""))} · book_at=${fmtTime(Date.parse(item.last_book_event_at || ""))}</div>
            <div class="status-desc">${item.last_error || "--"}</div>
          </div>
        `;
      })
      .join("") ||
    '<div class="empty-state show">当前还没有 connector 状态。</div>';
}

// ------------------------------------------------------------
// 市场快照渲染。
// 页面故意把“盘口买一/卖一/中间价”和“标记价”分开显示，避免误读。
// ------------------------------------------------------------
function renderMarket(snapshot) {
  const funding = normalizeFundingMap(snapshot);
  const bookTop = normalizeBookMap(snapshot);
  const exchanges = Array.from(
    new Set([...Object.keys(funding), ...Object.keys(bookTop)]),
  ).sort();
  els.activeSymbolLabel.textContent = state.activeSymbol;

  els.marketCards.innerHTML =
    exchanges
      .map((exchange) => {
        const fund = funding[exchange] || {};
        const book = bookTop[exchange] || {};
        const mid = midpoint(book.bid_price, book.ask_price);
        return `
          <div class="mini-card">
            <div class="mini-title">${exchange}</div>
            <div class="mini-row"><span>交易所符号</span><strong>${fund.venue_symbol || book.venue_symbol || "--"}</strong></div>
            <div class="mini-row"><span>盘口买一</span><strong>${priceText(book.bid_price)}</strong></div>
            <div class="mini-row"><span>盘口卖一</span><strong>${priceText(book.ask_price)}</strong></div>
            <div class="mini-row"><span>盘口中间价</span><strong>${priceText(mid)}</strong></div>
            <div class="mini-row"><span>标记价格</span><strong>${priceText(fund.mark_price)}</strong></div>
            <div class="mini-row"><span>资金费率</span><strong class="${classForNumber(fund.funding_rate)}">${fmtPctRatio(fund.funding_rate, 5)}</strong></div>
            <div class="mini-row"><span>下次结算</span><strong>${fmtTime(fund.funding_time_ms)}</strong></div>
          </div>
        `;
      })
      .join("") ||
    '<div class="empty-state show">当前交易对还没有市场快照。</div>';
}

// ------------------------------------------------------------
// 执行计划展示辅助函数。
// 注意：后端当前返回的是目标杠杆 target_leverage、投入资金 capital_allocated_usdt，
// 并没有单独的 long_leverage / short_leverage / estimated_margin_usdt 字段。
// 前端这里统一做兼容和兜底，避免页面显示为空。
// ------------------------------------------------------------
function planCapitalAllocated(plan) {
  return Number(
    plan?.capital_allocated_usdt ??
      plan?.estimated_margin_usdt ??
      plan?.margin_required_usdt ??
      0,
  );
}

function planTargetLeverage(plan) {
  return Number(plan?.target_leverage ?? plan?.leverage ?? 0);
}

function planLongLeverage(plan) {
  return Number(
    plan?.long_leverage ?? plan?.target_leverage ?? plan?.leverage ?? 0,
  );
}

function planShortLeverage(plan) {
  return Number(
    plan?.short_leverage ?? plan?.target_leverage ?? plan?.leverage ?? 0,
  );
}

function planLongNotional(plan) {
  const qty = Number(plan?.long_qty ?? 0);
  const px = Number(plan?.long_entry_price ?? 0);
  const direct = Number(plan?.long_notional_usdt ?? 0);
  if (Number.isFinite(direct) && direct > 0) return direct;
  if (!Number.isFinite(qty) || !Number.isFinite(px)) return 0;
  return qty * px;
}

function planShortNotional(plan) {
  const qty = Number(plan?.short_qty ?? 0);
  const px = Number(plan?.short_entry_price ?? 0);
  const direct = Number(plan?.short_notional_usdt ?? 0);
  if (Number.isFinite(direct) && direct > 0) return direct;
  if (!Number.isFinite(qty) || !Number.isFinite(px)) return 0;
  return qty * px;
}

function planPositionSkewBps(plan) {
  const longNotional = planLongNotional(plan);
  const shortNotional = planShortNotional(plan);
  const avg = (Math.abs(longNotional) + Math.abs(shortNotional)) / 2;
  if (!avg) return null;
  return (Math.abs(longNotional - shortNotional) / avg) * 10000;
}

function syntheticPlanForOpportunity(item) {
  if (!item || typeof item !== "object") return null;
  const strategy = state.system?.strategy || {};
  const targetLeverage = Number(strategy.leverage || 0);
  const capitalAllocated = Number(
    strategy.capital_total_usdt || 0,
  ) * Number(strategy.capital_utilization || 0);
  const targetNotional = Number(strategy.effective_notional || 0);
  const longEntryPrice = Number(item.long_ask_price || 0);
  const shortEntryPrice = Number(item.short_bid_price || 0);

  const synthetic = {
    plan_key: "synthetic-preview",
    symbol: item.symbol,
    status: item.eligible_for_execution ? "preview_ready" : "preview_only",
    ready_now: Boolean(item.eligible_for_execution),
    long_exchange: item.long_exchange,
    short_exchange: item.short_exchange,
    long_venue_symbol: item.long_venue_symbol,
    short_venue_symbol: item.short_venue_symbol,
    net_expected_pnl: item.net_expected_pnl,
    projected_funding_time_ms: item.projected_funding_time_ms,
    latest_funding_time_ms: item.latest_funding_time_ms,
    funding_event_count: item.funding_event_count,
    funding_event_count_estimate: item.funding_event_count_estimate,
    cross_venue_basis_bps: item.basis_bps,
    target_leverage: targetLeverage,
    capital_allocated_usdt: capitalAllocated,
    target_notional_usdt: targetNotional,
    long_leverage: targetLeverage,
    short_leverage: targetLeverage,
    long_entry_price: longEntryPrice,
    short_entry_price: shortEntryPrice,
  };

  if (targetNotional > 0 && longEntryPrice > 0) {
    synthetic.long_qty = targetNotional / longEntryPrice;
    synthetic.long_notional_usdt = targetNotional;
  }
  if (targetNotional > 0 && shortEntryPrice > 0) {
    synthetic.short_qty = targetNotional / shortEntryPrice;
    synthetic.short_notional_usdt = targetNotional;
  }
  return synthetic;
}

// ------------------------------------------------------------
// 执行计划 / 执行记录。
// ------------------------------------------------------------
function renderExecutionControls(planKey, status) {
  const st = String(status || "").toLowerCase();
  const openDisabled = [
    "opened",
    "dry_run_opened",
    "closed",
    "dry_run_closed",
  ].includes(st)
    ? "disabled"
    : "";
  const closeDisabled = ["closed", "dry_run_closed"].includes(st)
    ? "disabled"
    : "";
  return `
    <div class="hero-actions compact-actions">
      <button class="btn btn-secondary" data-action="open" data-plan-key="${planKey}" ${openDisabled}>手动开仓</button>
      <button class="btn btn-primary" data-action="close" data-plan-key="${planKey}" ${closeDisabled}>手动平仓</button>
    </div>
  `;
}

function compactStat(label, value, extraClass = "") {
  return `
    <div class="compact-stat ${extraClass}">
      <span>${label}</span>
      <strong>${value}</strong>
    </div>
  `;
}

function planCompactMeta(item, linkedOpp) {
  const metrics = [
    compactStat("状态", item.status || "--"),
    compactStat("净收益", fmtMoney(item.net_expected_pnl), classForNumber(item.net_expected_pnl)),
    compactStat("投入", fmtMoney(planCapitalAllocated(item), 2)),
    compactStat("杠杆", `${fmtNumber(planLongLeverage(item), 2)}x / ${fmtNumber(planShortLeverage(item), 2)}x`),
    compactStat("Funding", fundingEventsText(item)),
    compactStat("兑现", fmtTime(item.projected_funding_time_ms || item.latest_funding_time_ms)),
    compactStat("关联机会", linkedOpp ? "已关联" : "--", linkedOpp ? "positive" : "muted-text"),
  ];
  return `<div class="compact-stat-grid">${metrics.join("")}</div>`;
}

function renderCompactPlanCard(item, { selected = false, linkedOpp = null, showActions = true } = {}) {
  const linkedOppKey = linkedOpp ? opportunityKey(linkedOpp) : "";
  const isInteractive = Boolean(showActions) || Boolean(linkedOppKey);
  const positionSkew = planPositionSkewBps(item);
  const summaryLine = [
    `${item.long_exchange} Long ${item.long_venue_symbol || "--"}`,
    `${item.short_exchange} Short ${item.short_venue_symbol || "--"}`,
    `仓位偏移 ${positionSkew == null ? "--" : fmtSignedBps(positionSkew, 2)}`,
    `Basis ${fmtNumber(item.cross_venue_basis_bps, 4)} bps`,
  ].join(" · ");
  return `
    <div class="plan-card plan-card-compact ${isInteractive ? "clickable" : ""} ${selected ? "active" : ""}" ${isInteractive ? `data-plan-select="1" data-plan-key="${item.plan_key}" data-opportunity-key="${linkedOppKey}"` : ""}>
      <div class="plan-head plan-head-compact">
        <div>
          <div class="plan-title">${item.symbol} · ${item.long_exchange} / ${item.short_exchange}</div>
          <div class="plan-sub">${item.plan_key} · ${summaryLine}</div>
        </div>
        <div class="plan-head-right">
          <span class="pill ${item.ready_now ? "good" : "warn"}">${item.ready_now ? "Ready" : "Waiting"}</span>
          <div class="plan-pnl ${classForNumber(item.net_expected_pnl)}">${fmtMoney(item.net_expected_pnl)}</div>
        </div>
      </div>
      ${planCompactMeta(item, linkedOpp)}
      ${showActions ? renderExecutionControls(item.plan_key, item.status) : ""}
    </div>
  `;
}

function renderCompactExecutionCard(item) {
  const openCount = item.open_order_count || 0;
  const closeCount = item.close_order_count || 0;
  return `
    <div class="plan-card plan-card-compact">
      <div class="plan-head plan-head-compact">
        <div>
          <div class="plan-title">${item.symbol} · ${item.long_exchange} / ${item.short_exchange}</div>
          <div class="plan-sub">${item.plan_key}</div>
        </div>
        <div class="plan-head-right">
          <span class="pill ${String(item.status || "").includes("closed") ? "warn" : "good"}">${item.status || "--"}</span>
          <div class="plan-pnl">${item.live_trading ? "LIVE" : "DRY"}</div>
        </div>
      </div>
      <div class="compact-stat-grid">
        ${compactStat("开仓时间", fmtTime(item.opened_at_ms))}
        ${compactStat("平仓时间", fmtTime(item.closed_at_ms))}
        ${compactStat("开仓单", String(openCount))}
        ${compactStat("平仓单", String(closeCount))}
        ${compactStat("自动平仓", item.auto_close ? "YES" : "NO")}
      </div>
      ${item.last_error ? `<div class="status-desc compact-status-desc">${item.last_error}</div>` : ""}
      ${renderExecutionControls(item.plan_key, item.status)}
    </div>
  `;
}

function renderPlans() {
  const selectedOpportunity = selectedOpportunity(state.opportunities || []);
  let sourcePlans = state.batchPlans || [];
  let emptyText = state.currentOpportunityBatchId
    ? `当前机会批次（${state.currentOpportunityBatchId}）下没有可展示的执行计划。`
    : "当前没有执行计划。";
  if (state.planViewMode === "all") {
    sourcePlans = state.allPlans || [];
    emptyText = "当前没有可展示的历史/最新执行计划。";
  } else if (state.planViewMode === "related") {
    sourcePlans = selectedOpportunity
      ? matchingPlansForOpportunity(selectedOpportunity)
      : [];
    emptyText = selectedOpportunity
      ? "当前选中机会没有可展示的真实执行计划。"
      : "请先在左侧选择一条套利机会，再查看相关执行计划。";
  }

  if (!sourcePlans.length) {
    els.plansEmpty.style.display = "block";
    els.plansEmpty.textContent = emptyText;
    els.plansList.innerHTML = "";
    return;
  }

  const sortedPlans = [...sourcePlans].sort((a, b) => {
    const aSelected =
      String(a.plan_key || "") === String(state.selectedPlanKey || "");
    const bSelected =
      String(b.plan_key || "") === String(state.selectedPlanKey || "");
    if (aSelected !== bSelected) return Number(bSelected) - Number(aSelected);
    if (Boolean(b.ready_now) !== Boolean(a.ready_now))
      return Number(Boolean(b.ready_now)) - Number(Boolean(a.ready_now));
    return Number(b.net_expected_pnl || 0) - Number(a.net_expected_pnl || 0);
  });

  els.plansEmpty.style.display = "none";
  let batchBanner = "";
  if (state.planViewMode === "batch" && state.currentOpportunityBatchId) {
    batchBanner = `<div class="plan-batch-banner">当前执行计划已按机会批次对齐：<strong>${state.currentOpportunityBatchId}</strong></div>`;
  } else if (state.planViewMode === "all") {
    batchBanner = '<div class="plan-batch-banner">当前展示的是最新执行计划全集，不按机会批次过滤。</div>';
  } else if (state.planViewMode === "related") {
    batchBanner = `<div class="plan-batch-banner">当前展示的是“${selectedOpportunity?.symbol || "--"}”对应的真实执行计划；详情里的“仅展示估算仓位”不会出现在这里。</div>`;
  }
  els.plansList.innerHTML =
    batchBanner +
    sortedPlans
      .map((item) =>
        renderCompactPlanCard(item, {
          selected:
            String(item.plan_key || "") === String(state.selectedPlanKey || ""),
          linkedOpp: matchingOpportunityForPlan(item),
        }),
      )
      .join("");
}

function renderExecutions() {
  if (!state.executions.length) {
    els.executionsEmpty.style.display = "block";
    els.executionsList.innerHTML = "";
    return;
  }
  els.executionsEmpty.style.display = "none";
  els.executionsList.innerHTML = state.executions
    .map((item) => renderCompactExecutionCard(item))
    .join("");
}

// ------------------------------------------------------------
// 套利机会：筛选、摘要、列表、详情。
// ------------------------------------------------------------
function normalizePairs() {
  const pairs = new Set();
  for (const item of state.opportunities) pairs.add(opportunityPair(item));
  return Array.from(pairs).sort();
}

function filteredOpportunities() {
  const searchKey = (els.symbolFilter.value || "").trim().toUpperCase();
  const pairKey = els.exchangeFilter.value || "all";
  const sortMode = els.sortMode.value || "net";

  const items = state.opportunities.filter((item) => {
    if (pairKey !== "all" && opportunityPair(item) !== pairKey) return false;
    if (!searchKey) return true;

    const haystack = [
      item.symbol,
      item.long_exchange,
      item.short_exchange,
      item.long_venue_symbol,
      item.short_venue_symbol,
      opportunityPair(item),
      opportunityDirection(item),
    ]
      .map((v) => String(v || "").toUpperCase())
      .join(" ");

    return haystack.includes(searchKey);
  });

  items.sort((a, b) => {
    // 第一优先级：优先展示已经进入执行计划的数据；其次展示可进入计划的数据。
    const priorityDiff = opportunityPriority(b) - opportunityPriority(a);
    if (priorityDiff !== 0) return priorityDiff;

    if (sortMode === "score")
      return Number(b.score || 0) - Number(a.score || 0);
    if (sortMode === "edge")
      return (
        Number(b.gross_edge_hourly || 0) - Number(a.gross_edge_hourly || 0)
      );
    return Number(b.net_expected_pnl || 0) - Number(a.net_expected_pnl || 0);
  });

  return items;
}

function summaryMetric(label, value, extraClass = "") {
  return `
    <div class="summary-metric">
      <span>${label}</span>
      <strong class="${extraClass}">${value}</strong>
    </div>
  `;
}

function renderOpportunitySummary(items) {
  const item = currentSelectedOpportunity(items);
  if (!item) {
    els.opportunitySummary.innerHTML = "";
    return;
  }

  const earliestDelta = Number(item.earliest_funding_time_ms || 0) - Date.now();
  const planStatus = opportunityPlanStatus(item);
  els.opportunitySummary.innerHTML = `
    <div class="summary-card">
      <div>
        <div class="summary-kicker">当前默认展示</div>
        <div class="summary-title">${item.symbol} · ${opportunityDirection(item)}</div>
        <div class="summary-desc">左侧列表会优先展示已经进入执行计划、或者至少满足计划条件的机会。搜索为本地多字段检索（symbol/交易所/venue symbol/方向），仅对当前已加载机会生效。右侧则展示当前选中机会的两腿数据、收益构成与“是否进入计划”的解释。</div>
      </div>
      <div class="summary-metric-grid">
        ${summaryMetric("净收益", fmtMoney(item.net_expected_pnl), classForNumber(item.net_expected_pnl))}
        ${summaryMetric("净收益率", fmtSignedBps(item.net_expected_bps, 2), classForNumber(item.net_expected_bps))}
        ${summaryMetric("事件化时均 edge", fmtPctRatio(fundingSpreadHourly(item), 5), classForNumber(fundingSpreadHourly(item)))}
        ${summaryMetric("最早结算倒计时", fmtDuration(earliestDelta))}
        ${summaryMetric("计划状态", planStatus.text, planStatus.cls === "good" ? "positive" : planStatus.cls === "bad" ? "negative" : "muted-text")}
        ${summaryMetric("进入计划说明", planStatus.reason || "--")}
        ${summaryMetric("建议持有时长", holdingDurationText(item))}
      </div>
    </div>
  `;
}

function renderOpportunityList(items) {
  els.opportunitiesList.innerHTML = items
    .map((item) => {
      const active = opportunityKey(item) === state.selectedOpportunityKey;
      const earliestDelta =
        Number(item.earliest_funding_time_ms || 0) - Date.now();
      const planStatus = opportunityPlanStatus(item);
      return `
	        <button class="opportunity-item ${active ? "active" : ""}" data-opportunity-key="${opportunityKey(item)}" type="button">
          <div class="opportunity-item-head">
            <div>
              <div class="opportunity-item-title">${item.symbol}</div>
              <div class="opportunity-item-sub">${opportunityDirection(item)}</div>
            </div>
            <div class="opportunity-item-pnl ${classForNumber(item.net_expected_pnl)}">${fmtMoney(item.net_expected_pnl)}</div>
          </div>
          <div class="opportunity-mini-grid">
            <div><span>组合</span><strong>${opportunityPair(item)}</strong></div>
            <div><span>Basis</span><strong>${fmtSignedBps(item.basis_bps, 2)}</strong></div>
            <div><span>事件化时均 edge</span><strong>${fmtPctRatio(fundingSpreadHourly(item), 5)}</strong></div>
            <div><span>最早结算</span><strong>${fmtDuration(earliestDelta)}</strong></div>
            <div><span>建议持有</span><strong>${holdingDurationText(item)}</strong></div>
          </div>
          <div class="opportunity-foot dual-pill">
            <span class="pill ${planStatus.cls}">${planStatus.text}</span>
            <span class="pill ${item.eligible_for_execution ? "good" : "warn"}">${item.eligible_for_execution ? "可执行" : explainStatus(item.status)}</span>
          </div>
          ${basisTooWideHint(item) ? `<div class="opportunity-item-hint muted-text">${basisTooWideHint(item)}</div>` : ""}
        </button>
      `;
    })
    .join("");
}

function detailMetric(label, value, extraClass = "") {
  return `
    <div class="detail-metric ${extraClass}">
      <span>${label}</span>
      <strong>${value}</strong>
    </div>
  `;
}

function legCard(
  title,
  exchange,
  venueSymbol,
  fundingRate,
  futureFundingRate,
  hourlyRate,
  fundingTimeMs,
  fundingIntervalHours,
  bidPrice,
  askPrice,
  markPrice,
) {
  const mid = midpoint(bidPrice, askPrice);
  return `
    <div class="detail-section">
      <div class="detail-section-head">
        <div class="leg-title">${title}</div>
        <span class="pill ${title.includes("做空") ? "warn" : "good"}">${exchange}</span>
      </div>
      <div class="detail-list compact-list">
        <div class="detail-item"><span class="detail-k">交易对</span><span class="detail-v">${venueSymbol || "--"}</span></div>
        <div class="detail-item"><span class="detail-k">资金费率</span><span class="detail-v ${classForNumber(fundingRate)}">${fmtPctRatio(fundingRate, 5)}</span></div>
        <div class="detail-item"><span class="detail-k">平滑后后续费率</span><span class="detail-v ${classForNumber(futureFundingRate)}">${fmtPctRatio(futureFundingRate, 5)}</span></div>
        <div class="detail-item"><span class="detail-k">小时费率</span><span class="detail-v ${classForNumber(hourlyRate)}">${fmtPctRatio(hourlyRate, 5)}</span></div>
        <div class="detail-item"><span class="detail-k">下次结算</span><span class="detail-v">${fmtTime(fundingTimeMs)}</span></div>
        <div class="detail-item"><span class="detail-k">结算周期</span><span class="detail-v">${fundingIntervalText(fundingIntervalHours)}</span></div>
        <div class="detail-item"><span class="detail-k">盘口买一 / 卖一</span><span class="detail-v">${priceText(bidPrice)} / ${priceText(askPrice)}</span></div>
        <div class="detail-item"><span class="detail-k">盘口中间价</span><span class="detail-v">${priceText(mid)}</span></div>
        <div class="detail-item"><span class="detail-k">Mark</span><span class="detail-v">${priceText(markPrice)}</span></div>
      </div>
    </div>
  `;
}

function fundingRuleSummaryCard(title, rule, eventCount) {
  if (!rule || typeof rule !== "object") {
    return `
      <div class="detail-section">
        <div class="detail-section-title">${title}</div>
        <div class="empty-state show compact-empty">暂无 funding rule 元数据。</div>
      </div>
    `;
  }
  return `
    <div class="detail-section">
      <div class="detail-section-head">
        <div class="detail-section-title">${title}</div>
        <span class="pill ${eventCount > 1 ? "good" : "warn"}">${eventCount || 0} 次事件</span>
      </div>
      <div class="detail-list compact-list">
        <div class="detail-item"><span class="detail-k">交易所 / 合约</span><span class="detail-v">${rule.exchange || "--"} / ${rule.venue_symbol || "--"}</span></div>
        <div class="detail-item"><span class="detail-k">结算周期</span><span class="detail-v">${fundingIntervalText(rule.funding_interval_hours)}</span></div>
        <div class="detail-item"><span class="detail-k">下一次结算</span><span class="detail-v">${fmtTime(rule.next_funding_time_ms)}</span></div>
        <div class="detail-item"><span class="detail-k">当前 funding</span><span class="detail-v ${classForNumber(rule.current_funding_rate)}">${fmtPctRatio(rule.current_funding_rate, 5)}</span></div>
        <div class="detail-item"><span class="detail-k">Clamp 来源</span><span class="detail-v">${rule.clamp_source || "--"}</span></div>
        <div class="detail-item"><span class="detail-k">有效区间</span><span class="detail-v">${fmtPctRatio(rule.effective_floor_rate, 5)} ~ ${fmtPctRatio(rule.effective_cap_rate, 5)}</span></div>
        <div class="detail-item"><span class="detail-k">Regime / 置信度</span><span class="detail-v">${rule.forecast_regime || "--"} / ${rule.forecast_confidence || "--"}</span></div>
      </div>
      <div class="formula-box">${rule.metadata_summary || "--"}</div>
    </div>
  `;
}

function renderProjectionDetails(item) {
  const rows = Array.isArray(item?.projection_details)
    ? item.projection_details
    : [];
  if (!rows.length) {
    return '<div class="empty-state show compact-empty">当前没有可展示的多窗口 projection 明细。</div>';
  }
  return `
    <div class="projection-table-wrap">
      <table class="projection-table">
        <thead>
          <tr>
            <th>Rank</th>
            <th>兑现点</th>
            <th>窗口</th>
            <th>Long 事件</th>
            <th>Short 事件</th>
            <th>Carry</th>
            <th>时均</th>
            <th>净收益</th>
          </tr>
        </thead>
        <tbody>
          ${rows
            .map(
              (row) => `
                <tr class="${row.is_best_projection ? "best" : ""}">
                  <td>${row.is_best_projection ? "✅" : row.projection_rank || "--"}</td>
                  <td>${fmtTime(row.projected_funding_time_ms)}</td>
                  <td>${fmtNumber(row.funding_window_hours || 0, 2)} h</td>
                  <td>${row.long_funding_event_count || 0}</td>
                  <td>${row.short_funding_event_count || 0}</td>
                  <td class="${classForNumber(row.carry_rate)}">${fmtPctRatio(row.carry_rate, 5)}</td>
                  <td class="${classForNumber(row.carry_rate_hourly_equivalent)}">${fmtPctRatio(row.carry_rate_hourly_equivalent, 5)}</td>
                  <td class="${classForNumber(row.net_expected_pnl)}">${fmtMoney(row.net_expected_pnl)}</td>
                </tr>`,
            )
            .join("")}
        </tbody>
      </table>
    </div>
  `;
}

function renderOpportunityDetail(items) {
  const item = currentSelectedOpportunity(items);
  if (!item) {
    els.opportunityDetail.innerHTML =
      '<div class="empty-state show">当前没有可查看的套利机会。</div>';
    return;
  }

  const fundingDelta = fundingSpread(item);
  const hourlyDelta = fundingSpreadHourly(item);
  const nextLongMs = Number(item.long_funding_time_ms || 0) - Date.now();
  const nextShortMs = Number(item.short_funding_time_ms || 0) - Date.now();
  const matchedPlan = bestPlanForOpportunity(item);
  const displayPlan = matchedPlan || syntheticPlanForOpportunity(item);
  const planPreviewLabel = matchedPlan ? "真实执行计划" : "估算执行计划预览";
  const planStatus = opportunityPlanStatus(item);
  state.selectedOpportunityKey = opportunityKey(item);
  state.selectedPlanKey = matchedPlan?.plan_key || null;

  els.opportunityDetail.innerHTML = `
    <div class="detail-shell">
      <div class="detail-hero">
        <div>
          <div class="detail-eyebrow">机会详情</div>
          <div class="detail-title">${item.symbol} · ${opportunityDirection(item)}</div>
          <div class="detail-subtitle">${item.long_venue_symbol} ↔ ${item.short_venue_symbol}</div>
        </div>
        <div class="detail-hero-right">
          <span class="pill ${statusClass(item.status)}">${explainStatus(item.status)}</span>
          <div class="detail-score">评分 ${fmtNumber(item.score, 2)}</div>
        </div>
      </div>

      <div class="detail-metric-grid">
        ${detailMetric("净收益", fmtMoney(item.net_expected_pnl), classForNumber(item.net_expected_pnl))}
        ${detailMetric("净收益率", fmtSignedBps(item.net_expected_bps, 2), classForNumber(item.net_expected_bps))}
        ${detailMetric("资金费率差", fmtPctRatio(fundingDelta, 5), classForNumber(fundingDelta))}
        ${detailMetric("事件化时均 edge", fmtPctRatio(hourlyDelta, 5), classForNumber(hourlyDelta))}
        ${detailMetric("Basis", fmtSignedBps(item.basis_bps, 2))}
        ${detailMetric("动态价差阈值", fmtSignedBps(maxAllowedBasisForItem(item), 2))}
        ${detailMetric("资金收益", fmtMoney(item.gross_funding_pnl), classForNumber(item.gross_funding_pnl))}
        ${detailMetric("计划状态", planStatus.text, planStatus.cls === "good" ? "positive" : planStatus.cls === "bad" ? "negative" : "muted-text")}
        ${detailMetric("预计投入资金", displayPlan ? fmtMoney(planCapitalAllocated(displayPlan), 2) : "--")}
        ${detailMetric("目标杠杆", displayPlan ? `${fmtNumber(planTargetLeverage(displayPlan), 2)}x` : "--")}
        ${detailMetric("Long / Short 杠杆", displayPlan ? `${fmtNumber(planLongLeverage(displayPlan), 2)}x / ${fmtNumber(planShortLeverage(displayPlan), 2)}x` : "--")}
        ${detailMetric("仓位指标", displayPlan ? (planPositionSkewBps(displayPlan) == null ? "--" : fmtSignedBps(planPositionSkewBps(displayPlan), 2)) : "--")}
        ${detailMetric("目标仓位(名义)", targetNotionalText(item, displayPlan))}
        ${detailMetric("Long 结算倒计时", fmtDuration(nextLongMs))}
        ${detailMetric("Short 结算倒计时", fmtDuration(nextShortMs))}
        ${detailMetric("预计 funding 兑现点", fmtTime(item.projected_funding_time_ms || item.latest_funding_time_ms))}
        ${detailMetric("最晚入场时间", fmtTime(item.required_entry_by_funding_time_ms || item.earliest_funding_time_ms))}
        ${detailMetric("Funding 事件窗口", `${fmtNumber(item.funding_window_hours || 0, 2)} h`)}
        ${detailMetric("建议持有时长", holdingDurationText(item))}
        ${detailMetric("Funding 事件次数", fundingEventsText(item))}
        ${detailMetric("估算模式", fundingEstimateModeText(item))}
        ${detailMetric("估算置信度", fundingEstimateConfidenceText(item))}
        ${detailMetric("平滑回看窗口", fundingSmoothingLookbackText())}
        ${detailMetric("当前值权重", fundingSmoothingWeightText())}
      </div>

      <div class="detail-grid-2">
        ${legCard("做多腿", item.long_exchange, item.long_venue_symbol, item.long_funding_rate, item.long_future_funding_rate, item.long_funding_hourly, item.long_funding_time_ms, item.long_funding_interval_hours, item.long_bid_price, item.long_ask_price, item.long_mark_price)}
        ${legCard("做空腿", item.short_exchange, item.short_venue_symbol, item.short_funding_rate, item.short_future_funding_rate, item.short_funding_hourly, item.short_funding_time_ms, item.short_funding_interval_hours, item.short_bid_price, item.short_ask_price, item.short_mark_price)}
      </div>

      <div class="detail-section detail-section-tight linked-plan-section">
        <div class="detail-section-head">
          <div>
            <div class="detail-section-title">关联执行计划</div>
            <div class="detail-subtitle">把最相关的计划压缩进机会详情里，查看机会时不需要再被整块执行计划列表打断。</div>
          </div>
          <span class="pill ${matchedPlan ? "good" : "warn"}">${matchedPlan ? "已生成计划" : "仅展示估算仓位"}</span>
        </div>
        ${displayPlan ? renderCompactPlanCard(displayPlan, { selected: Boolean(matchedPlan), linkedOpp: item, showActions: Boolean(matchedPlan) }) : '<div class="empty-state show compact-empty">当前没有可展示的关联执行计划。</div>'}
      </div>

      <div class="detail-grid-2">
        ${fundingRuleSummaryCard("做多腿 funding rule", item.long_funding_rule, item.long_funding_event_count)}
        ${fundingRuleSummaryCard("做空腿 funding rule", item.short_funding_rule, item.short_funding_event_count)}
      </div>

      <div class="detail-section detail-section-tight">
        <div class="detail-section-head">
          <div>
            <div class="detail-section-title">候选持有窗口 / Projection 明细</div>
            <div class="detail-subtitle">同一条机会只保留一个最佳 projection 进入主列表，但这里会展示同方向下多个候选兑现窗口，帮助理解为什么最终选中了当前窗口。</div>
          </div>
          <span class="pill good">${Array.isArray(item.projection_details) ? item.projection_details.length : 0} 个窗口</span>
        </div>
        ${renderProjectionDetails(item)}
      </div>

      <div class="detail-section detail-section-tight linked-plan-section">
        <div class="detail-section-head">
          <div>
            <div class="detail-section-title">关联执行计划</div>
            <div class="detail-subtitle">这里优先展示真实 execution plan；若当前没有匹配到真实 plan，则退化为前端估算预览。因此这里能看到内容，并不代表底部“执行计划速览”一定有真实记录。</div>
          </div>
          <span class="pill ${matchedPlan ? "good" : "warn"}">${planPreviewLabel}</span>
        </div>
        ${displayPlan ? renderCompactPlanCard(displayPlan, { selected: Boolean(matchedPlan), linkedOpp: item, showActions: Boolean(matchedPlan) }) : '<div class="empty-state show compact-empty">当前没有可展示的关联执行计划。</div>'}
      </div>

      <div class="detail-grid-2">
        <div class="detail-section opportunity-explain">
          <div class="detail-section-title">收益构成</div>
          <div class="detail-list">
            <div class="detail-item"><span class="detail-k">资金收益</span><span class="detail-v ${classForNumber(item.gross_funding_pnl)}">${fmtMoney(item.gross_funding_pnl)}</span></div>
            <div class="detail-item"><span class="detail-k">入场手续费</span><span class="detail-v negative">-${fmtNumber(Math.abs(Number(item.entry_fee_pnl || 0)), 3)} USDT（${feeBreakdownText(item, state.system?.strategy?.entry_mode)}）</span></div>
            <div class="detail-item"><span class="detail-k">出场手续费</span><span class="detail-v negative">-${fmtNumber(Math.abs(Number(item.exit_fee_pnl || 0)), 3)} USDT（${feeBreakdownText(item, state.system?.strategy?.exit_mode)}）</span></div>
            <div class="detail-item"><span class="detail-k">滑点预估</span><span class="detail-v negative">-${fmtNumber(Math.abs(Number(item.slippage_pnl || 0)), 3)} USDT</span></div>
            <div class="detail-item"><span class="detail-k">安全缓冲</span><span class="detail-v negative">-${fmtNumber(Math.abs(Number(item.safety_buffer_pnl || 0)), 3)} USDT</span></div>
            <div class="detail-item total-row"><span class="detail-k">净收益</span><span class="detail-v ${classForNumber(item.net_expected_pnl)}">${fmtMoney(item.net_expected_pnl)}</span></div>
          </div>
          <div class="formula-box">
            净收益 = 资金收益 - 入场手续费 - 出场手续费 - 滑点 - 安全缓冲
            <br />
            说明：这里不是只看“当前这一期” funding，而是按当前最优持有窗口估算；若只覆盖当前这一轮结算，则直接使用当前 funding 快照；若会跨到后续多轮结算，则对后续事件结合近期历史均值做平滑估算。
          </div>
        </div>

        <div class="detail-section opportunity-explain">
          <div class="detail-section-title">机会判断</div>
          <div class="detail-list">
            <div class="detail-item"><span class="detail-k">策略状态</span><span class="detail-v">${explainStatus(item.status)}</span></div>
            <div class="detail-item"><span class="detail-k">是否已进入执行计划</span><span class="detail-v ${planStatus.cls === "good" ? "positive" : planStatus.cls === "bad" ? "negative" : "muted-text"}">${planStatus.text}</span></div>
            <div class="detail-item"><span class="detail-k">计划说明</span><span class="detail-v">${planStatus.reason || "--"}</span></div>
            <div class="detail-item"><span class="detail-k">计划主键</span><span class="detail-v">${matchedPlan?.plan_key || "--"}</span></div>
            <div class="detail-item"><span class="detail-k">计划状态</span><span class="detail-v">${matchedPlan?.status || "--"}</span></div>
            <div class="detail-item"><span class="detail-k">Funding 计算模式</span><span class="detail-v">${fundingModeText(item)}</span></div>
            <div class="detail-item"><span class="detail-k">当前机会批次</span><span class="detail-v">${state.currentOpportunityBatchId || item.batch_id || "--"}</span></div>
            <div class="detail-item"><span class="detail-k">计划关联批次</span><span class="detail-v">${matchedPlan?.opportunity_batch_id || "--"}</span></div>
            <div class="detail-item"><span class="detail-k">Long 仓位名义</span><span class="detail-v">${matchedPlan ? fmtMoney(planLongNotional(matchedPlan), 2) : "--"}</span></div>
            <div class="detail-item"><span class="detail-k">Short 仓位名义</span><span class="detail-v">${matchedPlan ? fmtMoney(planShortNotional(matchedPlan), 2) : "--"}</span></div>
            <div class="detail-item"><span class="detail-k">机会拒绝原因</span><span class="detail-v">${explainOpportunityRejectReason(item) || "--"}</span></div>
            <div class="detail-item"><span class="detail-k">跨所价差判定</span><span class="detail-v">${basisDecisionText(item)}</span></div>
            <div class="detail-item"><span class="detail-k">跨所价差阈值</span><span class="detail-v">${fmtSignedBps(maxAllowedBasisForItem(item), 2)}</span></div>
            <div class="detail-item"><span class="detail-k">是否可执行</span><span class="detail-v">${item.eligible_for_execution ? "是" : "否"}</span></div>
            <div class="detail-item"><span class="detail-k">最早结算时间</span><span class="detail-v">${fmtTime(item.earliest_funding_time_ms)}</span></div>
            <div class="detail-item"><span class="detail-k">最晚结算时间</span><span class="detail-v">${fmtTime(item.latest_funding_time_ms)}</span></div>
            <div class="detail-item"><span class="detail-k">预计 funding 兑现点</span><span class="detail-v">${fmtTime(item.projected_funding_time_ms || item.latest_funding_time_ms)}</span></div>
            <div class="detail-item"><span class="detail-k">最晚入场时间</span><span class="detail-v">${fmtTime(item.required_entry_by_funding_time_ms || item.earliest_funding_time_ms)}</span></div>
            <div class="detail-item"><span class="detail-k">Funding 事件次数</span><span class="detail-v">${fundingEventsText(item)}</span></div>
            <div class="detail-item"><span class="detail-k">Funding 事件窗口</span><span class="detail-v">${fmtNumber(item.funding_window_hours || 0, 2)} h</span></div>
          </div>
          <div class="insight-box">
            <div class="insight-title">怎么看这组机会</div>
            <div class="insight-text">方向不是固定死的。系统会在每次刷新时，把“Long A / Short B”和“Long B / Short A”两个方向都完整计算一遍，再选当前更优的方向展示。Funding 收益也不再按统一小时平均外推，而是按当前已知的真实 funding 结算事件逐腿估算。状态里“跨所价差过大”表示当前 Basis（shortBid 与 longAsk 的相对偏离）超过动态阈值（基础阈值 \`max_spread_bps\` 按持有时长可放宽），为避免入场成本吞噬 funding 收益会被拦截。若后续 funding 或 basis 变化导致反方向更优，下一轮机会就会切换成反方向。</div>
          </div>
        </div>
      </div>
    </div>
  `;
}

function syncOpportunityHeights() {
  // 桌面端让列表与详情区等高，并在列表内容过多时滚动，避免“列表明显高于详情”的视觉失衡。
  const listEl = els.opportunitiesList;
  const detailEl = els.opportunityDetail;
  if (!listEl || !detailEl) return;

  listEl.style.minHeight = "";
  listEl.style.maxHeight = "";

  if (window.innerWidth <= 1080) return;

  const detailHeight = detailEl.offsetHeight;
  if (!detailHeight) return;

  listEl.style.minHeight = `${detailHeight}px`;
  listEl.style.maxHeight = `${detailHeight}px`;
}

function renderOpportunities() {
  const items = filteredOpportunities();
  const hasItems = items.length > 0;
  els.opportunitiesEmpty.style.display = hasItems ? "none" : "block";
  if (!hasItems) {
    els.opportunitySummary.innerHTML = "";
    els.opportunitiesList.innerHTML = "";
    els.opportunityDetail.innerHTML = "";
    return;
  }
  renderOpportunitySummary(items);
  renderOpportunityList(items);
  renderOpportunityDetail(items);
  requestAnimationFrame(syncOpportunityHeights);
}

// ------------------------------------------------------------
// 选择器同步。
// watchlist 直接来自后端，前端不自行推断。
// ------------------------------------------------------------
function syncSelectors() {
  const symbols = Array.isArray(state.system?.watchlist)
    ? state.system.watchlist
    : [];
  state.symbols = symbols;
  if (!symbols.includes(state.activeSymbol) && symbols.length)
    state.activeSymbol = symbols[0];
  els.marketSelect.innerHTML = symbols
    .map((item) => `<option value="${item}">${item}</option>`)
    .join("");
  els.marketSelect.value = state.activeSymbol;

  const pairs = normalizePairs();
  const old = els.exchangeFilter.value || "all";
  els.exchangeFilter.innerHTML = [`<option value="all">全部交易所组合</option>`]
    .concat(pairs.map((item) => `<option value="${item}">${item}</option>`))
    .join("");
  els.exchangeFilter.value = pairs.includes(old) ? old : "all";
}

// ------------------------------------------------------------
// 整体刷新入口。
// 所有区块都在这里统一刷新，避免页面各部分时间不同步。
// ------------------------------------------------------------
async function refreshAll() {
  // 先拿机会列表。当前页面的 plans 必须与 opportunities 使用同一批 batch_id，
  // 否则很容易出现：
  // - 机会列表是新一批；
  // - 执行计划却还是旧一批；
  // 前端看起来就会互相矛盾。
  const [system, opportunities, executions, stats] = await Promise.all([
    apiGet("/api/v1/system/status", {}),
    apiGet(`/api/v1/opportunities?limit=${OPPORTUNITY_FETCH_LIMIT}`, []),
    apiGet("/api/v1/executions?limit=50", []),
    apiGet("/api/v1/snapshot-stats", {}),
  ]);

  const normalizedOpportunities = Array.isArray(opportunities)
    ? opportunities
    : [];
  const currentBatchId = normalizedOpportunities.length
    ? String(normalizedOpportunities[0].batch_id || "")
    : "";

  // 只请求“当前机会批次”对应的 plans，避免 plans 和 opportunities 不是同一批。
  const plansPath = currentBatchId
    ? `/api/v1/plans?limit=100&opportunity_batch_id=${encodeURIComponent(currentBatchId)}`
    : `/api/v1/plans?limit=100`;
  const [plans, allPlans] = await Promise.all([
    apiGet(plansPath, []),
    apiGet("/api/v1/plans?limit=100", []),
  ]);

  state.system = system || {};
  state.opportunities = normalizedOpportunities;
  state.currentOpportunityBatchId = currentBatchId;
  state.batchPlans = Array.isArray(plans) ? plans : [];
  state.allPlans = Array.isArray(allPlans) ? allPlans : [];
  state.plans = state.allPlans;
  state.executions = Array.isArray(executions) ? executions : [];
  state.stats = stats || {};

  syncSelectors();
  const market = await apiGet(
    `/api/v1/market/${encodeURIComponent(state.activeSymbol)}`,
    {},
  );

  renderOverview();
  renderMetrics();
  renderSystem();
  renderMarket(market || {});
  renderPlans();
  renderExecutions();
  renderOpportunities();
  els.lastRefreshLabel.textContent = new Date().toLocaleString("zh-CN", {
    hour12: false,
  });
}

// ------------------------------------------------------------
// 全局点击事件。
// 统一处理手动开仓/平仓，以及机会列表点击切换。
// ------------------------------------------------------------
async function handleActionClick(event) {
  const actionButton = event.target.closest("button[data-action]");
  if (actionButton) {
    const action = actionButton.dataset.action;
    const planKey = actionButton.dataset.planKey;
    if (!action || !planKey) return;
    actionButton.disabled = true;
    try {
      if (action === "open") {
        await apiPost(`/api/v1/executions/${planKey}/open`);
      } else if (action === "close") {
        await apiPost(`/api/v1/executions/${planKey}/close`);
      }
      await refreshAll();
    } catch (err) {
      alert(err.message || String(err));
    } finally {
      actionButton.disabled = false;
    }
    return;
  }

  const planCard = event.target.closest("[data-plan-select='1']");
  if (planCard) {
    state.selectedPlanKey = planCard.dataset.planKey || null;
    const oppKey = planCard.dataset.opportunityKey || "";
    if (oppKey) {
      state.selectedOpportunityKey = oppKey;
      renderOpportunities();
    }
    renderPlans();
    return;
  }

  const opportunityBtn = event.target.closest("button[data-opportunity-key]");
  if (opportunityBtn) {
    state.selectedOpportunityKey = opportunityBtn.dataset.opportunityKey;
    renderOpportunities();
    renderPlans();
  }
}

function bindEvents() {
  els.manualRefreshBtn.addEventListener("click", refreshAll);

  els.marketSelect.addEventListener("change", async (event) => {
    state.activeSymbol = event.target.value;
    const market = await apiGet(
      `/api/v1/market/${encodeURIComponent(state.activeSymbol)}`,
      {},
    );
    renderMarket(market || {});
  });

  els.symbolFilter.addEventListener("input", renderOpportunities);
  els.exchangeFilter.addEventListener("change", renderOpportunities);
  els.planViewMode?.addEventListener("change", (event) => {
    state.planViewMode = event.target.value || "batch";
    renderPlans();
  });
  els.sortMode.addEventListener("change", () => {
    state.selectedOpportunityKey = null;
    renderOpportunities();
  });

  window.addEventListener("resize", () =>
    requestAnimationFrame(syncOpportunityHeights),
  );
  document.body.addEventListener("click", handleActionClick);
}

async function bootstrap() {
  bindEvents();
  await refreshAll();
  setInterval(refreshAll, state.refreshMs);
}

bootstrap();
