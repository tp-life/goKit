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
  autoClose: null,
  system: null,
  stats: null,
  refreshMs: 8000,
  selectedOpportunityKey: null,
  selectedPlanKey: null,
  currentOpportunityBatchId: "",
  planViewMode: "batch",
  opportunityDetailTab: "overview",
  opportunityDetails: {},
  opportunityDetailLoading: {},
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
  executionBoardSummary: document.getElementById("execution-board-summary"),
  plansList: document.getElementById("plans-list"),
  plansEmpty: document.getElementById("plans-empty"),
  planViewMode: document.getElementById("plan-view-mode"),
  executionsList: document.getElementById("executions-list"),
  executionsEmpty: document.getElementById("executions-empty"),
  autoCloseSummary: document.getElementById("auto-close-summary"),
  autoCloseList: document.getElementById("auto-close-list"),
  autoCloseEmpty: document.getElementById("auto-close-empty"),
  autoCloseSweepBtn: document.getElementById("auto-close-sweep-btn"),
  manualRefreshBtn: document.getElementById("manual-refresh-btn"),
};

// ------------------------------------------------------------
// 通用 API 方法。
// 后端统一返回 {code,message,data} 时，这里只取 data。
// ------------------------------------------------------------
async function apiGet(path, fallback = null) {
  try {
    const res = await fetch(path, { headers: buildApiHeaders() });
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
  const headers = buildApiHeaders();
  headers["Content-Type"] = "application/json";
  const res = await fetch(path, {
    method: "POST",
    headers,
    body: JSON.stringify(payload),
  });
  const json = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(json?.message || `HTTP ${res.status}`);
  return json && typeof json === "object" && "data" in json ? json.data : json;
}

function buildApiHeaders() {
  const headers = { Accept: "application/json" };
  try {
    const token = window.localStorage.getItem("execution_api_token");
    if (token) {
      headers.Authorization = `Bearer ${token}`;
    }
  } catch (err) {
    console.warn("read execution api token failed", err);
  }
  return headers;
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

function parseTimeMs(value) {
  if (value == null) return 0;
  if (typeof value === "number") return Number.isFinite(value) ? value : 0;
  const text = String(value || "").trim();
  if (!text) return 0;
  const numeric = Number(text);
  if (Number.isFinite(numeric) && numeric > 0) return numeric;
  const parsed = Date.parse(text);
  return Number.isFinite(parsed) ? parsed : 0;
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

function fmtSpan(ms) {
  const n = Number(ms);
  if (!Number.isFinite(n)) return "--";
  if (n <= 0) return "0秒";

  const totalSeconds = Math.round(n / 1000);
  const days = Math.floor(totalSeconds / 86400);
  const hours = Math.floor((totalSeconds % 86400) / 3600);
  const mins = Math.floor((totalSeconds % 3600) / 60);
  const secs = totalSeconds % 60;

  if (days > 0) return `${days}天 ${hours}小时`;
  if (hours > 0) return `${hours}小时 ${mins}分钟`;
  if (mins > 0) return `${mins}分钟 ${secs}秒`;
  return `${secs}秒`;
}

// Go 后端把 duration 序列化成类似 `15s`、`5m0s`、`1h30m0s`。
// 前端只需要把这些值换算成毫秒，用来把“funding 兑现点”推导成“预计平仓点”。
function parseGoDurationMs(value) {
  const text = String(value || "").trim();
  if (!text) return 0;

  const unitMs = {
    ns: 1 / 1e6,
    us: 1 / 1e3,
    "µs": 1 / 1e3,
    ms: 1,
    s: 1000,
    m: 60 * 1000,
    h: 60 * 60 * 1000,
  };

  const matches = text.matchAll(/(-?\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/g);
  let total = 0;
  let matched = false;
  for (const match of matches) {
    const amount = Number(match[1]);
    const factor = unitMs[match[2]];
    if (!Number.isFinite(amount) || !Number.isFinite(factor)) continue;
    total += amount * factor;
    matched = true;
  }
  return matched ? total : 0;
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

function holdSelectionModeText(value) {
  const mode = String(value || "").trim().toLowerCase();
  if (!mode) return "--";
  if (mode === "dynamic_profit") return "动态收益驱动";
  if (mode === "latest_profitable") return "最晚盈利窗口";
  if (mode === "strict_target") return "严格目标窗口";
  if (mode === "best_net") return "净收益优先";
  return value;
}

function arbitrageModeText(value) {
  const mode = String(value || "").trim().toLowerCase();
  if (!mode) return "--";
  if (mode === "same_exchange_spot_perp") return "同所现货 + 永续对冲";
  if (mode === "cross_exchange") return "跨所 funding 套利";
  return value;
}

function strategyModeText(value) {
  const mode = String(value || "").trim().toLowerCase();
  if (!mode) return "--";
  if (mode === "rolling_cycle_aligned") return "按结算段滚动";
  if (mode === "legacy_projection") return "累计窗口投影";
  return value;
}

function isRollingStrategyMode(value) {
  return String(value || "").trim().toLowerCase() === "rolling_cycle_aligned";
}

function strategyArbitrageMode() {
  return String(state.system?.strategy?.arbitrage_mode || "").trim().toLowerCase();
}

function isSpotFundingRule(rule) {
  return String(rule?.clamp_source || "").trim().toLowerCase() === "spot_synthetic_zero";
}

function opportunityArbitrageMode(item) {
  if (isSpotFundingRule(item?.long_funding_rule) || isSpotFundingRule(item?.short_funding_rule)) {
    return "same_exchange_spot_perp";
  }
  return strategyArbitrageMode() || "cross_exchange";
}

function usesDynamicHoldSelection(holdSelectionMode, strategyMode) {
  const holdMode = String(holdSelectionMode || "").trim().toLowerCase();
  if (holdMode === "dynamic_profit") return true;
  return isRollingStrategyMode(strategyMode);
}

function strategyUsesDynamicHold() {
  const strategy = state.system?.strategy || {};
  return usesDynamicHoldSelection(strategy.hold_selection_mode, strategy.mode);
}

function isSameExchangeOpportunity(item) {
  return opportunityArbitrageMode(item) === "same_exchange_spot_perp";
}

function sameExchangeLegs(item) {
  const longLeg = {
    key: "long",
    title: longLegTitle(item),
    exchange: item?.long_exchange,
    venueSymbol: item?.long_venue_symbol,
    fundingRate: item?.long_funding_rate,
    futureFundingRate: item?.long_future_funding_rate,
    hourlyRate: item?.long_funding_hourly,
    fundingTimeMs: item?.long_funding_time_ms,
    fundingIntervalHours: item?.long_funding_interval_hours,
    bidPrice: item?.long_bid_price,
    askPrice: item?.long_ask_price,
    markPrice: item?.long_mark_price,
    fundingRule: item?.long_funding_rule,
  };
  const shortLeg = {
    key: "short",
    title: shortLegTitle(item),
    exchange: item?.short_exchange,
    venueSymbol: item?.short_venue_symbol,
    fundingRate: item?.short_funding_rate,
    futureFundingRate: item?.short_future_funding_rate,
    hourlyRate: item?.short_funding_hourly,
    fundingTimeMs: item?.short_funding_time_ms,
    fundingIntervalHours: item?.short_funding_interval_hours,
    bidPrice: item?.short_bid_price,
    askPrice: item?.short_ask_price,
    markPrice: item?.short_mark_price,
    fundingRule: item?.short_funding_rule,
  };
  if (isSpotFundingRule(item?.short_funding_rule)) {
    return { spot: shortLeg, perp: longLeg };
  }
  return { spot: longLeg, perp: shortLeg };
}

function longLegTitle(item) {
  if (opportunityArbitrageMode(item) === "same_exchange_spot_perp" || isSpotFundingRule(item?.long_funding_rule)) {
    return "现货多头腿";
  }
  return "做多腿";
}

function shortLegTitle(item) {
  if (opportunityArbitrageMode(item) === "same_exchange_spot_perp") {
    return "永续空头腿";
  }
  return "做空腿";
}

function opportunityDisplayTitle(item) {
  if (isSameExchangeOpportunity(item)) {
    return `${item.symbol} · 同所现货对冲`;
  }
  return `${item.symbol} · 跨所 funding 套利`;
}

function opportunityDisplaySubtitle(item) {
  if (isSameExchangeOpportunity(item)) {
    const legs = sameExchangeLegs(item);
    return `${legs.spot.exchange} 现货做多 / ${legs.perp.exchange} 永续做空`;
  }
  return opportunityDirection(item);
}

function opportunityDisplayDescription(item) {
  if (isSameExchangeOpportunity(item)) {
    return "同所模式下，页面会把“现货多头腿”和“永续空头腿”分开展示，重点看永续 funding、现货占资、对冲偏差和换仓信号，而不是继续套用跨所 funding 差的阅读方式。";
  }
  return "跨所模式下，系统同时比较两边 funding、跨所 Basis 和执行成本，当前展示的是此刻更优的 long/short 方向，以及它是否已经进入执行计划。";
}

function basisMetricLabel(item) {
  return isSameExchangeOpportunity(item) ? "现货-永续基差" : "跨所 Basis";
}

function carryMetricLabel(item) {
  return isSameExchangeOpportunity(item) ? "当前 funding Carry率" : "当前 Carry率";
}

function grossFundingMetricLabel(item) {
  return isSameExchangeOpportunity(item) ? "永续 funding 收益" : "当前 Entry Path 资金收益";
}

function targetNotionalLabel(item) {
  return isSameExchangeOpportunity(item) ? "现货占资 / 对冲名义" : "目标仓位(名义)";
}

function currentFundingAnchorLabel(item) {
  return isSameExchangeOpportunity(item) ? "下次永续 funding" : "最早结算倒计时";
}

function currentFundingAnchorValue(item) {
  if (isSameExchangeOpportunity(item)) {
    const legs = sameExchangeLegs(item);
    return fmtTime(legs.perp.fundingTimeMs);
  }
  const earliestDelta = Number(item.earliest_funding_time_ms || 0) - Date.now();
  return fmtDuration(earliestDelta);
}

function opportunityModePillText(item) {
  if (isSameExchangeOpportunity(item)) {
    const legs = sameExchangeLegs(item);
    return `${legs.spot.exchange} Spot / ${legs.perp.exchange} Perp`;
  }
  return opportunityPair(item);
}

function planArbitrageMode(plan, linkedOpp = null) {
  if (linkedOpp) return opportunityArbitrageMode(linkedOpp);
  return strategyArbitrageMode() || "cross_exchange";
}

function isSameExchangePlan(plan, linkedOpp = null) {
  return planArbitrageMode(plan, linkedOpp) === "same_exchange_spot_perp";
}

function planDisplayTitle(plan, linkedOpp = null) {
  if (isSameExchangePlan(plan, linkedOpp)) {
    return `${plan.symbol} · 同所现货对冲`;
  }
  return `${plan.symbol} · ${plan.long_exchange} / ${plan.short_exchange}`;
}

function planDisplaySubtitle(plan, linkedOpp = null) {
  if (isSameExchangePlan(plan, linkedOpp)) {
    return `${plan.long_exchange} 现货做多 ${plan.long_venue_symbol || "--"} · ${plan.short_exchange} 永续做空 ${plan.short_venue_symbol || "--"}`;
  }
  return `${plan.long_exchange} Long ${plan.long_venue_symbol || "--"} · ${plan.short_exchange} Short ${plan.short_venue_symbol || "--"}`;
}

function executionDisplayTitle(item, plan = null) {
  if (isSameExchangePlan(plan || item)) {
    return `${item.symbol || "--"} · 同所现货对冲`;
  }
  return `${item.symbol || "--"} · ${item.long_exchange || "--"} / ${item.short_exchange || "--"}`;
}

function executionDisplaySubtitle(item, plan = null) {
  if (isSameExchangePlan(plan || item)) {
    return `${item.long_exchange || "--"} 现货多头 · ${item.short_exchange || "--"} 永续空头`;
  }
  return `${item.long_exchange || "--"} 多头 · ${item.short_exchange || "--"} 空头`;
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
  if (opportunityArbitrageMode(item) === "same_exchange_spot_perp") {
    return `同所 ${item.long_exchange}(现货) → ${item.short_exchange}(永续)`;
  }
  return `${item.long_exchange} → ${item.short_exchange}`;
}

function opportunityDirection(item) {
  if (opportunityArbitrageMode(item) === "same_exchange_spot_perp") {
    return `${item.long_exchange} 现货做多 / ${item.short_exchange} 永续做空`;
  }
  return `${item.long_exchange} 做多 / ${item.short_exchange} 做空`;
}

function bestProjection(item) {
  const rows = Array.isArray(item?.projection_details) ? item.projection_details : [];
  return rows.find((row) => row?.is_best_projection) || rows[0] || null;
}

function isRollingOpportunity(item) {
  return String(item?.strategy_mode || "").toLowerCase() === "rolling_cycle_aligned";
}

function currentCarrySegment(item) {
  if (!isRollingOpportunity(item)) return null;

  // 展示层先解释“当前真实首段”：
  // - 单腿先结算 => 只看先结算那一腿的真实 funding；
  // - 双腿同结算 => 看当前真实 funding 差；
  // 这样可以避免把 forecast 累加值误读成“马上这一轮就能拿到的 carry”。
  const longFundingTimeMs = Number(item?.long_funding_time_ms || 0);
  const shortFundingTimeMs = Number(item?.short_funding_time_ms || 0);
  const longFundingRate = Number(item?.long_funding_rate || 0);
  const shortFundingRate = Number(item?.short_funding_rate || 0);
  if (longFundingTimeMs <= 0 && shortFundingTimeMs <= 0) return null;

  if (longFundingTimeMs > 0 && (shortFundingTimeMs <= 0 || longFundingTimeMs < shortFundingTimeMs)) {
    return {
      carryRate: -longFundingRate,
      settlementTimeMs: longFundingTimeMs,
      longEvents: 1,
      shortEvents: 0,
    };
  }
  if (shortFundingTimeMs > 0 && (longFundingTimeMs <= 0 || shortFundingTimeMs < longFundingTimeMs)) {
    return {
      carryRate: shortFundingRate,
      settlementTimeMs: shortFundingTimeMs,
      longEvents: 0,
      shortEvents: 1,
    };
  }
  return {
    carryRate: shortFundingRate - longFundingRate,
    settlementTimeMs: Math.max(longFundingTimeMs, shortFundingTimeMs),
    longEvents: 1,
    shortEvents: 1,
  };
}

function fundingSpread(item) {
  const currentCarry = Number(currentCarrySegment(item)?.carryRate);
  if (Number.isFinite(currentCarry)) {
    return currentCarry;
  }
  const bestProjectionCarry = Number(bestProjection(item)?.carry_rate);
  if (Number.isFinite(bestProjectionCarry)) {
    return bestProjectionCarry;
  }
  const targetNotional = strategyTargetNotional();
  if (targetNotional > 0) {
    return Number(item?.gross_funding_pnl || 0) / targetNotional;
  }
  return (
    Number(item.short_funding_rate || 0) - Number(item.long_funding_rate || 0)
  );
}

function fundingSpreadHourly(item) {
  const current = currentCarrySegment(item);
  if (current && Number.isFinite(Number(current.carryRate))) {
    const windowHours = Math.max(
      (Number(current.settlementTimeMs || 0) - Date.now()) / 3600000,
      1 / 60,
    );
    return Number(current.carryRate) / windowHours;
  }
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

function closeGraceMs() {
  return parseGoDurationMs(state.system?.execution?.close_grace_period);
}

function holdLimitText() {
  if (strategyUsesDynamicHold()) return "收益动态控制";
  const hours = Number(state.system?.strategy?.hold_hours || 0);
  if (!Number.isFinite(hours) || hours <= 0) return "--";
  return `${fmtNumber(hours, 1)} h`;
}

function closeGraceText() {
  return state.system?.execution?.close_grace_period || "--";
}

// 机会级别还没有落真实 execution plan 时，前端用
// projected funding time + close grace 推导一个“预计平仓时间”。
function opportunityExpectedCloseMs(item) {
  if (!item || typeof item !== "object") return 0;
  if (isRollingStrategyMode(item.strategy_mode)) {
    const nextReviewMs = Number(item.next_review_time_ms || 0);
    if (Number.isFinite(nextReviewMs) && nextReviewMs > 0) {
      return nextReviewMs + closeGraceMs();
    }
  }
  const projectedMs = Number(
    item.projected_funding_time_ms || item.latest_funding_time_ms || 0,
  );
  if (!Number.isFinite(projectedMs) || projectedMs <= 0) return 0;
  return projectedMs + closeGraceMs();
}

function opportunityExpectedCloseText(item) {
  return fmtTime(opportunityExpectedCloseMs(item));
}

function opportunityExpectedHoldText(item) {
  const closeMs = opportunityExpectedCloseMs(item);
  if (closeMs > 0) {
    return fmtDuration(closeMs - Date.now());
  }
  return holdingDurationText(item);
}

function executionRemainingHoldText(item) {
  const targetCloseMs = Number(item?.target_close_time_ms ?? 0);
  if (!Number.isFinite(targetCloseMs) || targetCloseMs <= 0) return "--";
  return fmtDuration(targetCloseMs - Date.now());
}

function opportunityProducedTimeMs(item) {
  if (!item || typeof item !== "object") return 0;
  return parseTimeMs(item.as_of_time_ms) || parseTimeMs(item.created_at);
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

function hasSameExchangeBasisAssessment(item) {
  if (!item || typeof item !== "object") return false;
  return (
    Boolean(String(item.same_exchange_basis_reason || "").trim()) ||
    Boolean(item.same_exchange_basis_uses_payback_model) ||
    Math.abs(Number(item.same_exchange_basis_cost_bps || 0)) > 1e-9 ||
    Math.abs(Number(item.same_exchange_basis_carry_per_event_bps || 0)) > 1e-9 ||
    Math.abs(Number(item.same_exchange_basis_payback_funding_events || 0)) > 1e-9 ||
    Number(item.same_exchange_basis_risk_size_multiplier || 0) > 0
  );
}

function sameExchangeBasisUsesPayback(item) {
  return Boolean(item?.same_exchange_basis_uses_payback_model);
}

function sameExchangeBasisModeText(item) {
  if (!hasSameExchangeBasisAssessment(item)) return "--";
  return sameExchangeBasisUsesPayback(item) ? "长持回本轮数" : "短持硬阈值";
}

function sameExchangeBasisCostText(item) {
  if (!hasSameExchangeBasisAssessment(item)) return "--";
  return fmtSignedBps(Number(item?.same_exchange_basis_cost_bps || 0), 2);
}

function sameExchangeBasisCarryPerEventText(item) {
  if (!hasSameExchangeBasisAssessment(item)) return "--";
  return fmtSignedBps(
    Number(item?.same_exchange_basis_carry_per_event_bps || 0),
    2,
  );
}

function sameExchangeBasisPaybackText(item) {
  if (!hasSameExchangeBasisAssessment(item)) return "--";
  const events = Number(item?.same_exchange_basis_payback_funding_events || 0);
  if (!Number.isFinite(events) || events <= 0) return "--";
  return `${fmtNumber(events, 2)} 轮`;
}

function sameExchangeBasisThresholdText(item) {
  if (sameExchangeBasisUsesPayback(item)) {
    const maxEvents = Number(
      state.system?.strategy?.same_exchange_max_basis_payback_events || 0,
    );
    const extremeEvents = Number(
      state.system?.strategy?.same_exchange_extreme_basis_payback_events || 0,
    );
    if (Number.isFinite(extremeEvents) && extremeEvents > 0) {
      return `max ${fmtNumber(maxEvents, 2)} 轮 / extreme ${fmtNumber(extremeEvents, 2)} 轮`;
    }
    return `max ${fmtNumber(maxEvents, 2)} 轮`;
  }
  return fmtSignedBps(maxAllowedBasisForItem(item), 2);
}

function sameExchangeBasisReasonText(item) {
  const reason = String(item?.same_exchange_basis_reason || "").toLowerCase();
  switch (reason) {
    case "":
    case "eligible":
      return "基差成本可以被 funding 覆盖";
    case "short_window_too_wide":
      return "短持窗口下基差偏宽，优先缩仓而非直接拒绝";
    case "payback_carry_missing":
      return "每轮 funding 毛收益 <= 0，无法覆盖基差";
    case "payback_too_high":
      return `回本轮数 > ${fmtNumber(Number(state.system?.strategy?.same_exchange_max_basis_payback_events || 0), 2)}，收益回收偏慢`;
    case "extreme_payback":
      return `回本轮数 > ${fmtNumber(Number(state.system?.strategy?.same_exchange_extreme_basis_payback_events || 0), 2)}，先降仓`;
    default:
      return item?.same_exchange_basis_reason || "--";
  }
}

function sameExchangeBasisActionText(item) {
  if (!hasSameExchangeBasisAssessment(item)) return "--";
  const usesPayback = sameExchangeBasisUsesPayback(item);
  const allowed = Boolean(item?.same_exchange_basis_allowed);
  const reason = String(item?.same_exchange_basis_reason || "").toLowerCase();
  const multiplier = Number(item?.same_exchange_basis_risk_size_multiplier || 0);
  if (!usesPayback) {
    if (reason === "short_window_too_wide") {
      return Number.isFinite(multiplier) && multiplier > 0 && multiplier < 1
        ? `短持基差偏宽，降仓至 ${fmtNumber(multiplier * 100, 0)}%`
        : "短持基差偏宽，谨慎开仓";
    }
    return !allowed ? "短持基差超限" : "短持基差通过";
  }
  if (!allowed) {
    if (reason === "payback_carry_missing") return "每轮 funding 不足，拒绝开仓";
    if (reason === "payback_too_high") {
      return `回本 > ${fmtNumber(Number(state.system?.strategy?.same_exchange_max_basis_payback_events || 0), 2)} 轮，拒绝开仓`;
    }
    return "回本轮数超限";
  }
  if (reason === "payback_carry_missing") {
    return "每轮 funding 不足，谨慎开仓";
  }
  if (reason === "payback_too_high") {
    return Number.isFinite(multiplier) && multiplier > 0 && multiplier < 1
      ? `回本偏慢，降仓至 ${fmtNumber(multiplier * 100, 0)}%`
      : `回本 > ${fmtNumber(Number(state.system?.strategy?.same_exchange_max_basis_payback_events || 0), 2)} 轮，谨慎开仓`;
  }
  if (Number.isFinite(multiplier) && multiplier > 0 && multiplier < 1) {
    return `极端基差，降仓至 ${fmtNumber(multiplier * 100, 0)}%`;
  }
  return "回本轮数可覆盖";
}

function sameExchangeBasisActionClass(item) {
  const text = sameExchangeBasisActionText(item);
  if (!text || text === "--") return "";
  if (text.includes("拒绝") || text.includes("超限")) return "negative";
  if (text.includes("降仓") || text.includes("谨慎")) return "warn";
  return "positive";
}

function basisDecisionText(item) {
  if (isSameExchangeOpportunity(item) && hasSameExchangeBasisAssessment(item)) {
    if (sameExchangeBasisUsesPayback(item)) {
      return `基差成本=${sameExchangeBasisCostText(item)}，每轮 funding=${sameExchangeBasisCarryPerEventText(item)}，回本≈${sameExchangeBasisPaybackText(item)}，${sameExchangeBasisActionText(item)}`;
    }
    return `当前基差=${fmtSignedBps(item?.basis_bps, 2)}，短持阈值=${sameExchangeBasisThresholdText(item)}，${sameExchangeBasisActionText(item)}`;
  }
  const basis = Number(item?.basis_bps || 0);
  const maxSpread = maxAllowedBasisForItem(item);
  const label = isSameExchangeOpportunity(item) ? "现货-永续基差" : "当前 Basis";
  if (!Number.isFinite(maxSpread) || maxSpread <= 0) {
    return `${label}=${fmtSignedBps(basis, 2)}（未配置阈值）`;
  }
  return `${label}=${fmtSignedBps(basis, 2)}，阈值=${fmtSignedBps(maxSpread, 2)}，${basis > maxSpread ? "已超限" : "未超限"}`;
}

function basisTooWideHint(item) {
  if (String(item?.status || "").toLowerCase() !== "basis_too_wide") return "";
  if (isSameExchangeOpportunity(item)) {
    if (sameExchangeBasisUsesPayback(item)) {
      return `当前基差回本 ${sameExchangeBasisPaybackText(item)}（上限 ${fmtNumber(Number(state.system?.strategy?.same_exchange_max_basis_payback_events || 0), 2)} 轮）`;
    }
    return `当前现货-永续基差 ${fmtSignedBps(item?.basis_bps, 2)}（阈值 ${fmtSignedBps(maxAllowedBasisForItem(item), 2)}）`;
  }
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

function sameExchangeDisplayPlan(item) {
  return bestPlanForOpportunity(item) || null;
}

function sameExchangeTargetNotional(item) {
  const plan = sameExchangeDisplayPlan(item);
  const planNotional = Number(
    plan?.target_notional_usdt || plan?.rounded_notional_usdt || 0,
  );
  if (Number.isFinite(planNotional) && planNotional > 0) return planNotional;
  return strategyTargetNotional();
}

function sameExchangeNextFundingGrossPNL(item) {
  const rate = Number(item?.short_funding_rate || 0);
  if (!Number.isFinite(rate) || Math.abs(rate) <= 1e-9) return 0;
  const notional = sameExchangeTargetNotional(item);
  if (!Number.isFinite(notional) || notional <= 0) return 0;
  return notional * rate;
}

function sameExchangeWindowGrossPNL(item) {
  return Number(item?.gross_funding_pnl || 0);
}

function sameExchangePlanFundingRankText(item) {
  const plan = sameExchangeDisplayPlan(item);
  const rank = Number(plan?.perp_funding_rank || 0);
  const total = Number(plan?.perp_funding_rank_total || 0);
  if (!Number.isFinite(rank) || !Number.isFinite(total) || rank <= 0 || total <= 0) {
    return "--";
  }
  return `#${fmtNumber(rank, 0)}/${fmtNumber(total, 0)}`;
}

function sameExchangePlanHistoryPositiveText(item) {
  const plan = sameExchangeDisplayPlan(item);
  return fundingHistoryRatioText(
    plan?.perp_funding_history_positive_ratio,
    plan?.perp_funding_history_sample_count,
  );
}

function sameExchangePlanHistoryNegativeText(item) {
  const plan = sameExchangeDisplayPlan(item);
  return fundingHistoryRatioText(
    plan?.perp_funding_history_negative_ratio,
    plan?.perp_funding_history_sample_count,
  );
}

function sameExchangePlanAnnualizedNetText(item) {
  const plan = sameExchangeDisplayPlan(item);
  const sampleCount = Number(plan?.perp_funding_history_sample_count || 0);
  if (!Number.isFinite(sampleCount) || sampleCount <= 0) return "--";
  return fmtPctRatio(plan?.perp_funding_estimated_annualized_net_rate, 2);
}

function formatHoldHoursText(hours) {
  const value = Number(hours || 0);
  if (!Number.isFinite(value) || value <= 0) return "--";
  if (value >= 24) return `${fmtNumber(value, 1)} h / ${fmtNumber(value / 24, 2)} 天`;
  return `${fmtNumber(value, 1)} h`;
}

function sameExchangePerpRule(item) {
  return sameExchangeLegs(item)?.perp?.fundingRule || null;
}

function sameExchangeSuggestedEventRateText(item) {
  const rule = sameExchangePerpRule(item);
  if (Number.isFinite(Number(rule?.estimated_event_rate)) && Number(rule?.history_sample_count || 0) > 0) {
    return fmtPctRatio(rule.estimated_event_rate, 5);
  }
  const plan = sameExchangeDisplayPlan(item);
  if (Number.isFinite(Number(plan?.perp_funding_estimated_event_rate)) && Number(plan?.perp_funding_history_sample_count || 0) > 0) {
    return fmtPctRatio(plan.perp_funding_estimated_event_rate, 5);
  }
  return "--";
}

function sameExchangeSuggestedHoldEventsText(item) {
  const rule = sameExchangePerpRule(item);
  const ruleEvents = Number(rule?.suggested_funding_events || 0);
  if (Number.isFinite(ruleEvents) && ruleEvents > 0) return `${fmtNumber(ruleEvents, 0)} 轮`;
  const plan = sameExchangeDisplayPlan(item);
  const planEvents = Number(plan?.same_exchange_long_hold_suggested_funding_events || 0);
  if (Number.isFinite(planEvents) && planEvents > 0) return `${fmtNumber(planEvents, 0)} 轮`;
  return "--";
}

function sameExchangeSuggestedHoldDurationText(item) {
  const rule = sameExchangePerpRule(item);
  const ruleHours = Number(rule?.suggested_hold_hours || 0);
  if (Number.isFinite(ruleHours) && ruleHours > 0) return formatHoldHoursText(ruleHours);
  const plan = sameExchangeDisplayPlan(item);
  return formatHoldHoursText(plan?.same_exchange_long_hold_suggested_hold_hours);
}

function sameExchangeSuggestedGrossPNLText(item) {
  const rule = sameExchangePerpRule(item);
  const ruleValue = Number(rule?.suggested_gross_funding_pnl);
  if (Number.isFinite(ruleValue) && Number(rule?.suggested_funding_events || 0) > 0) return fmtMoney(ruleValue);
  const plan = sameExchangeDisplayPlan(item);
  const planValue = Number(plan?.same_exchange_long_hold_suggested_gross_funding_pnl);
  if (Number.isFinite(planValue) && Number(plan?.same_exchange_long_hold_suggested_funding_events || 0) > 0) return fmtMoney(planValue);
  return "--";
}

function sameExchangeSuggestedNetPNLText(item) {
  const rule = sameExchangePerpRule(item);
  const ruleValue = Number(rule?.suggested_net_pnl);
  if (Number.isFinite(ruleValue) && Number(rule?.suggested_funding_events || 0) > 0) return fmtMoney(ruleValue);
  const plan = sameExchangeDisplayPlan(item);
  const planValue = Number(plan?.same_exchange_long_hold_suggested_net_pnl);
  if (Number.isFinite(planValue) && Number(plan?.same_exchange_long_hold_suggested_funding_events || 0) > 0) return fmtMoney(planValue);
  return "--";
}

function sameExchangeLongHoldSourceText(item) {
  if (Boolean(item?.same_exchange_long_hold_using_history_estimate)) return "已切换到历史长持预估";
  const plan = sameExchangeDisplayPlan(item);
  if (Boolean(plan?.same_exchange_long_hold_using_history_estimate)) return "已切换到历史长持预估";
  if (sameExchangeSuggestedHoldEventsText(item) !== "--") return "历史长持建议可用";
  return "当前窗口预估";
}

function sameExchangeLongHoldReasonText(item) {
  const rule = sameExchangePerpRule(item);
  const reason = String(rule?.long_hold_reason || "").trim();
  const strategy = state.system?.strategy || {};
  const minSamples = Number(strategy.same_exchange_min_history_sample_count || 0);
  const minSupport = Number(strategy.same_exchange_min_historical_support_ratio || 0);
  const minAnnualized = Number(strategy.same_exchange_min_annualized_net_rate || 0);
  const minNetPnl = Number(strategy.min_net_pnl || 0);
  switch (reason) {
    case "":
    case "eligible":
      return "满足长期持有门槛";
    case "funding_not_positive":
      return "当前永续 funding <= 0";
    case "history_samples_low":
      return `历史样本 < ${fmtNumber(minSamples, 0)}`;
    case "support_ratio_low":
      return `同向历史支持 < ${fmtNumber(minSupport * 100, 1)}%`;
    case "annualized_net_rate_low":
      return `粗年化净收益 < ${fmtNumber(minAnnualized * 100, 1)}%`;
    case "projected_net_pnl_low":
      return `建议持有期净收益 < ${fmtMoney(minNetPnl)}`;
    case "missing_funding_interval":
      return "缺少 funding 间隔";
    case "missing_notional":
      return "缺少可用名义";
    default:
      return reason || "--";
  }
}

function sameExchangePlanLongHoldText(item) {
  const plan = sameExchangeDisplayPlan(item);
  if (!plan || typeof plan !== "object") return "--";
  const sampleCount = Number(plan?.perp_funding_history_sample_count || 0);
  const reason = String(plan?.same_exchange_long_hold_reason || "").trim();
  if (sampleCount <= 0 && !reason) return "--";
  if (Boolean(plan?.same_exchange_long_hold_eligible)) return "适合长期持有";
  return Boolean(state.system?.strategy?.same_exchange_require_long_hold_eligible)
    ? "不建议开仓"
    : "不建议长期持有";
}

function syncOpportunitySortControl() {
  if (!els.sortMode) return;
  const sameExchangeMode =
    String(state.system?.strategy?.arbitrage_mode || "").toLowerCase() ===
    "same_exchange_spot_perp";
  for (const option of Array.from(els.sortMode.options || [])) {
    if (option.value === "net") {
      option.textContent = sameExchangeMode ? "按预计净收益排序" : "按净收益排序";
    } else if (option.value === "edge") {
      option.textContent = sameExchangeMode
        ? "按主窗口 funding 收益率排序"
        : "按小时 funding edge 排序";
    } else if (option.value === "funding") {
      option.disabled = !sameExchangeMode;
      option.hidden = !sameExchangeMode;
    }
  }
  if (sameExchangeMode) {
    if (!els.sortMode.dataset.userSelected && (!els.sortMode.value || els.sortMode.value === "net")) {
      els.sortMode.value = "funding";
    }
    return;
  }
  if (els.sortMode.value === "funding") {
    els.sortMode.value = "net";
    return;
  }
  if (!els.sortMode.dataset.userSelected && !els.sortMode.value) {
    els.sortMode.value = "net";
  }
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

function livePositionCandidateForPlan(planKey) {
  if (!planKey) return null;
  const items = Array.isArray(state.livePositions?.candidates)
    ? state.livePositions.candidates
    : [];
  return (
    items.find((item) => String(item?.execution?.plan_key || "") === String(planKey)) ||
    null
  );
}

function sameExchangePerpLiveLeg(candidate) {
  if (!candidate || typeof candidate !== "object") return null;
  return candidate.short_leg || null;
}

function liquidationDistanceRatioFromPosition(position) {
  const qty = Number(position?.quantity || 0);
  const mark = Number(position?.mark_price || 0);
  const liq = Number(position?.liquidation_price || 0);
  if (!Number.isFinite(qty) || !Number.isFinite(mark) || !Number.isFinite(liq)) return null;
  if (Math.abs(qty) <= 1e-9 || mark <= 0 || liq <= 0) return null;
  if (qty < 0) return Math.max(0, (liq - mark) / mark);
  return Math.max(0, (mark - liq) / mark);
}

function sameExchangePriceRiskReasonText(reason) {
  const normalized = String(reason || "").trim().toLowerCase();
  if (!normalized || normalized === "eligible") return "--";
  if (normalized === "max_1h_price_shock") return "1h 拉升过快";
  return reason || "--";
}

function sameExchangeProtectiveStatusText(risk) {
  const status = String(risk?.protective_order_status || "").trim().toLowerCase();
  if (!status) return "未挂保护单";
  if (risk?.protective_order_armed) return "已挂保护单";
  if (status === "filled") return "已触发";
  if (status === "canceled" || status === "cancelled") return "已撤销";
  if (status === "error") return "保护单失败";
  return String(risk?.protective_order_status || "--").toUpperCase();
}

function sameExchangeProtectiveStatusClass(risk) {
  const status = String(risk?.protective_order_status || "").trim().toLowerCase();
  if (risk?.protective_order_armed) return "positive";
  if (status === "filled") return "warn";
  if (status === "error") return "negative";
  return "muted-text";
}

function sameExchangeLiquidationClass(risk, ratio) {
  if (!Number.isFinite(ratio)) return "muted-text";
  const emergency = Number(risk?.emergency_liq_distance_ratio || 0);
  const reduce = Number(risk?.reduce_liq_distance_ratio || 0);
  if (emergency > 0 && ratio <= emergency) return "negative";
  if (reduce > 0 && ratio <= reduce) return "warn";
  return "positive";
}

function sameExchangeRiskSnapshot(item, plan) {
  const liveCandidate = livePositionCandidateForPlan(plan?.plan_key);
  const liveRisk = liveCandidate?.risk || null;
  const perpLeg = sameExchangePerpLiveLeg(liveCandidate);
  const liqRatio = liquidationDistanceRatioFromPosition(perpLeg?.position);
  return {
    liveCandidate,
    liveRisk,
    priceShockRatio: Number(
      liveRisk?.price_shock_ratio ??
        plan?.same_exchange_price_shock_ratio ??
        item?.same_exchange_price_shock_ratio ??
        0,
    ),
    priceShockThresholdRatio: Number(
      liveRisk?.price_shock_threshold_ratio ??
        state.system?.strategy?.same_exchange_max_1h_price_shock_ratio ??
        0,
    ),
    priceShockAllowed: Boolean(
      liveRisk?.enabled
        ? liveRisk?.price_shock_allowed
        : plan?.same_exchange_price_risk_allowed ?? item?.same_exchange_price_risk_allowed,
    ),
    priceShockReason:
      liveRisk?.price_shock_reason ||
      plan?.same_exchange_price_risk_reason ||
      item?.same_exchange_price_risk_reason ||
      "",
    baselineMarkPrice: Number(
      liveRisk?.price_shock_baseline_mark_price ??
        plan?.same_exchange_price_shock_baseline_mark_price ??
        item?.same_exchange_price_shock_baseline_mark_price ??
        0,
    ),
    currentMarkPrice: Number(
      liveRisk?.price_shock_current_mark_price ??
        plan?.same_exchange_price_shock_current_mark_price ??
        item?.same_exchange_price_shock_current_mark_price ??
        0,
    ),
    liquidationPrice: Number(
      perpLeg?.position?.liquidation_price ?? liveRisk?.liquidation_price ?? 0,
    ),
    liquidationDistanceRatio: liqRatio,
  };
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
      reason: `planKey=${plan.plan_key} · 状态=${explainStatus(plan.status)}`,
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
    case "price_risk_guard":
      return "价格异动风控";
    case "not_profitable":
      return "净收益不足";
    case "stale_data":
      return "行情已过期";
    case "outside_entry_window":
      return "尚未进入开仓窗口";
    case "settlement_window_passed":
      return "最近结算点已过";
    case "pending_open":
      return "开仓进行中";
    case "opened":
      return "已开仓";
    case "open_partial_failed":
      return "开仓部分失败";
    case "open_failed":
      return "开仓失败";
    case "open_hedging":
      return "开仓对冲中";
    case "risk_blocked":
      return "风控阻断";
    case "circuit_open":
      return "交易熔断";
    case "pending_close":
      return "平仓进行中";
    case "closed":
      return "已平仓";
    case "close_partial_failed":
      return "平仓部分失败";
    case "close_failed":
      return "平仓失败";
    case "close_hedging":
      return "平仓对冲中";
    case "dry_run_opened":
      return "模拟已开仓";
    case "dry_run_closed":
      return "模拟已平仓";
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

function opportunityDetailCacheKey(itemOrId) {
  const raw = typeof itemOrId === "object" ? itemOrId?.id : itemOrId;
  const id = Number(raw);
  if (!Number.isFinite(id) || id <= 0) return "";
  return String(id);
}

function opportunityDetailFor(item) {
  const key = opportunityDetailCacheKey(item);
  if (!key) return null;
  const cached = state.opportunityDetails[key];
  if (!cached || typeof cached !== "object") return null;

  const summaryBatch = String(item?.batch_id || "");
  const detailBatch = String(cached?.batch_id || "");
  if (summaryBatch && detailBatch && summaryBatch !== detailBatch) return null;

  const summaryAsOf = Number(item?.as_of_time_ms || 0);
  const detailAsOf = Number(cached?.as_of_time_ms || 0);
  if (summaryAsOf > 0 && detailAsOf > 0 && detailAsOf < summaryAsOf) return null;

  return cached;
}

function pruneOpportunityDetailCache(items) {
  const keep = new Set(
    (Array.isArray(items) ? items : [])
      .map((item) => opportunityDetailCacheKey(item))
      .filter(Boolean),
  );

  Object.keys(state.opportunityDetails).forEach((key) => {
    if (!keep.has(key)) delete state.opportunityDetails[key];
  });
  Object.keys(state.opportunityDetailLoading).forEach((key) => {
    if (!keep.has(key)) delete state.opportunityDetailLoading[key];
  });
}

async function ensureOpportunityDetailLoaded(item) {
  const key = opportunityDetailCacheKey(item);
  if (!key) return;
  if (opportunityDetailFor(item) || state.opportunityDetailLoading[key]) return;

  state.opportunityDetailLoading[key] = true;
  renderOpportunityDetail(state.opportunities);

  try {
    const detail = await apiGet(`/api/v1/opportunities/${encodeURIComponent(key)}`, null);
    if (detail && typeof detail === "object") {
      state.opportunityDetails[key] = detail;
    }
  } finally {
    delete state.opportunityDetailLoading[key];
    renderOpportunityDetail(state.opportunities);
    requestAnimationFrame(syncOpportunityHeights);
  }
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
    infoCell("策略模式", strategyModeText(strategy.mode)),
    infoCell("套利模式", arbitrageModeText(strategy.arbitrage_mode)),
    infoCell("持有规则", holdLimitText()),
    infoCell("持有窗口模式", holdSelectionModeText(strategy.hold_selection_mode)),
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
    infoCell("Review 缓冲", strategy.rolling_review_settle_grace_period || "--"),
    infoCell(
      "Review 快照等待",
      strategy.rolling_review_fresh_snapshot_max_wait || "--",
    ),
  ].join("");

  els.executionSummary.innerHTML = [
    infoCell(
      "实盘开关",
      boolText(execution.live_trading_enabled),
      execution.live_trading_enabled ? "warn" : "good",
    ),
    infoCell("自动开仓", boolText(execution.auto_entry)),
    infoCell("自动平仓", boolText(execution.auto_close)),
    infoCell("主动换仓", boolText(execution.active_replacement_enabled)),
    infoCell("自动分配资金", boolText(execution.auto_allocate_capital)),
    infoCell("轮询间隔", execution.loop_interval || "--"),
    infoCell("结算后缓冲", execution.close_grace_period || "--"),
    infoCell(
      "换仓最小净增益",
      `${fmtNumber(execution.active_replacement_min_net_improvement_pnl || 0, 2)} USDT`,
    ),
    infoCell(
      "仓位偏差容忍",
      fmtPctRatio(execution.position_monitor_max_qty_deviation_ratio || 0, 1),
    ),
    infoCell("最新计划窗口", String(execution.max_latest_plans || 0)),
    infoCell(
      "每轮开仓上限",
      execution.max_auto_open_per_loop > 0
        ? String(execution.max_auto_open_per_loop)
        : "不限",
    ),
    infoCell(
      "当前 Live 计划",
      execution.max_live_plans > 0
        ? `${Number(execution.active_live_plans || 0)}/${Number(execution.max_live_plans || 0)}`
        : `${Number(execution.active_live_plans || 0)}/不限`,
    ),
    infoCell(
      "已占用名义",
      `${fmtNumber(execution.active_allocated_notional_usdt || 0, 2)} USDT`,
    ),
    infoCell(
      "剩余预算",
      `${fmtNumber(execution.remaining_auto_budget_usdt || 0, 2)} USDT`,
      Number(execution.remaining_auto_budget_usdt || 0) > 0 ? "good" : "warn",
    ),
    infoCell("Rolling 分组", String(execution.active_rolling_groups || 0)),
    infoCell("待 Review", String(execution.rolling_due_reviews || 0)),
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
  const capitalAllocated =
    Number(strategy.capital_total_usdt || 0) *
    Number(strategy.capital_utilization || 0);
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
    strategy_mode: item.strategy_mode,
    next_review_time_ms: item.next_review_time_ms,
    sync_boundary_time_ms: item.sync_boundary_time_ms,
    entry_path_segment_count: item.entry_path_segment_count,
    entry_path_stop_reason: item.entry_path_stop_reason,
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
    target_close_time_ms: opportunityExpectedCloseMs(item),
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

function findPlanByKey(planKey) {
  const key = String(planKey || "");
  if (!key) return null;

  for (const collection of [state.allPlans, state.batchPlans, state.plans]) {
    const found = (collection || []).find(
      (item) => String(item?.plan_key || "") === key,
    );
    if (found) return found;
  }
  return null;
}

function executionExpectedPnl(item) {
  const plan = findPlanByKey(item?.plan_key);
  if (!plan) return null;
  const pnl = Number(plan.net_expected_pnl);
  return Number.isFinite(pnl) ? pnl : null;
}

function executionAllocatedNotional(item) {
  const direct = Number(item?.allocated_notional_usdt ?? 0);
  if (Number.isFinite(direct) && direct > 0) return direct;

  const plan = findPlanByKey(item?.plan_key);
  if (!plan) return null;

  const rounded = Number(plan?.rounded_notional_usdt ?? 0);
  if (Number.isFinite(rounded) && rounded > 0) return rounded;

  const target = Number(plan?.target_notional_usdt ?? 0);
  if (Number.isFinite(target) && target > 0) return target;

  return null;
}

function planExpectedHoldText(item) {
  const targetCloseMs = Number(item?.target_close_time_ms ?? 0);
  const entryOpenMs = Number(item?.entry_window_open_ms ?? 0);
  const entryCloseMs = Number(item?.entry_window_close_ms ?? 0);
  if (!Number.isFinite(targetCloseMs) || targetCloseMs <= 0) return "--";

  const shortestHoldMs =
    Number.isFinite(entryCloseMs) && entryCloseMs > 0
      ? targetCloseMs - entryCloseMs
      : 0;
  const longestHoldMs =
    Number.isFinite(entryOpenMs) && entryOpenMs > 0
      ? targetCloseMs - entryOpenMs
      : 0;

  if (shortestHoldMs > 0 && longestHoldMs > 0) {
    if (Math.abs(longestHoldMs - shortestHoldMs) < 1000) {
      return fmtSpan(longestHoldMs);
    }
    return `${fmtSpan(shortestHoldMs)} - ${fmtSpan(longestHoldMs)}`;
  }
  if (shortestHoldMs > 0) return fmtSpan(shortestHoldMs);
  if (longestHoldMs > 0) return fmtSpan(longestHoldMs);
  return "--";
}

function executionPlannedHoldText(item) {
  const openedAtMs = Number(item?.opened_at_ms ?? 0);
  const targetCloseMs = Number(item?.target_close_time_ms ?? 0);
  if (
    Number.isFinite(openedAtMs) &&
    openedAtMs > 0 &&
    Number.isFinite(targetCloseMs) &&
    targetCloseMs > openedAtMs
  ) {
    return fmtSpan(targetCloseMs - openedAtMs);
  }

  const plan = findPlanByKey(item?.plan_key);
  return planExpectedHoldText(plan);
}

function isExecutedExecutionItem(item) {
  if (!item || !item.live_trading) return false;
  const status = String(item.status || "").toLowerCase().trim();
  return [
    "opened",
    "open_partial_failed",
    "open_hedging",
    "pending_close",
    "closed",
    "close_partial_failed",
    "close_failed",
    "close_hedging",
  ].includes(status);
}

function sumExpectedPnl(items, valueGetter) {
  return (items || []).reduce((total, item) => {
    const value = Number(valueGetter(item));
    return Number.isFinite(value) ? total + value : total;
  }, 0);
}

function resolvePlanViewState() {
  const selectedOpportunity = currentSelectedOpportunity(
    state.opportunities || [],
  );
  let sourcePlans = state.batchPlans || [];
  let emptyText = state.currentOpportunityBatchId
    ? `当前机会批次（${state.currentOpportunityBatchId}）下没有可展示的执行计划。`
    : "当前没有执行计划。";
  let batchBanner = "";

  if (state.planViewMode === "all") {
    sourcePlans = state.allPlans || [];
    emptyText = "当前没有可展示的历史/最新执行计划。";
    batchBanner =
      '<div class="plan-batch-banner">当前展示的是最新执行计划全集，不按机会批次过滤。</div>';
  } else if (state.planViewMode === "related") {
    sourcePlans = selectedOpportunity
      ? matchingPlansForOpportunity(selectedOpportunity)
      : [];
    emptyText = selectedOpportunity
      ? "当前选中机会没有可展示的真实执行计划。"
      : "请先在左侧选择一条套利机会，再查看相关执行计划。";
    batchBanner = `<div class="plan-batch-banner">当前展示的是“${selectedOpportunity?.symbol || "--"}”对应的真实执行计划；详情里的“仅展示估算仓位”不会出现在这里。</div>`;
  } else if (state.currentOpportunityBatchId) {
    batchBanner = `<div class="plan-batch-banner">当前执行计划已按机会批次对齐：<strong>${state.currentOpportunityBatchId}</strong></div>`;
  }

  return {
    selectedOpportunity,
    sourcePlans,
    emptyText,
    batchBanner,
  };
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

function renderExecutionBoardSummary(visiblePlans) {
  if (!els.executionBoardSummary) return;

  const plans = Array.isArray(visiblePlans) ? visiblePlans : [];
  const planTotalPnl = sumExpectedPnl(plans, (item) => item?.net_expected_pnl);
  const executedItems = (state.executions || []).filter((item) =>
    isExecutedExecutionItem(item),
  );
  const executionExpectedPnls = executedItems
    .map((item) => executionExpectedPnl(item))
    .filter((value) => Number.isFinite(value));
  const executionTotalPnl = executionExpectedPnls.reduce(
    (total, value) => total + value,
    0,
  );
  const matchedExecutionCount = executionExpectedPnls.length;
  const totalExpectedPnlText = executedItems.length
    ? matchedExecutionCount
      ? fmtMoney(executionTotalPnl)
      : "--"
    : fmtMoney(0);
  const totalExpectedPnlClass = executedItems.length
    ? matchedExecutionCount
      ? classForNumber(executionTotalPnl)
      : "muted-text"
    : classForNumber(0);

  els.executionBoardSummary.innerHTML = `
    <div class="compact-stat-grid execution-board-summary-grid">
      ${compactStat("当前计划数", String(plans.length))}
      ${compactStat(
        "当前计划预期收益",
        fmtMoney(planTotalPnl),
        classForNumber(planTotalPnl),
      )}
      ${compactStat("已执行计划数", String(executedItems.length))}
      ${compactStat(
        "总预期收益",
        totalExpectedPnlText,
        totalExpectedPnlClass,
      )}
    </div>
    <div class="status-desc execution-board-summary-note">
      总预期收益按 live execution 统计，仅累计已执行的
      <code>opened / closing / closed / recovery</code> 记录；<code>pending_open</code>
      与 <code>dry_run</code> 不计入。若 execution 对应的历史 plan 当前未加载，
      该条记录不会被计入收益汇总。
    </div>
  `;
}

function planCompactMeta(item, linkedOpp) {
  const sameExchange = isSameExchangePlan(item, linkedOpp);
  const metrics = [
    compactStat("状态", item.status || "--"),
    compactStat(
      "预期收益",
      fmtMoney(item.net_expected_pnl),
      classForNumber(item.net_expected_pnl),
    ),
    compactStat("投入", fmtMoney(planCapitalAllocated(item), 2)),
    compactStat(
      "目标名义",
      Number(item.target_notional_usdt || 0) > 0
        ? fmtMoney(item.target_notional_usdt, 2)
        : "--",
    ),
    compactStat(
      "取整名义",
      Number(item.rounded_notional_usdt || 0) > 0
        ? fmtMoney(item.rounded_notional_usdt, 2)
        : "--",
    ),
    compactStat(
      sameExchange ? "现货 / 永续杠杆" : "杠杆",
      sameExchange
        ? `现货全额 / 永续 ${fmtNumber(planShortLeverage(item), 2)}x`
        : `${fmtNumber(planLongLeverage(item), 2)}x / ${fmtNumber(planShortLeverage(item), 2)}x`,
    ),
    compactStat(
      "持有模式",
      holdSelectionModeText(state.system?.strategy?.hold_selection_mode),
    ),
    compactStat(sameExchange ? "Funding / 锚定" : "Funding", fundingEventsText(item)),
    compactStat("预计持仓", planExpectedHoldText(item)),
    compactStat(
      "兑现",
      fmtTime(item.projected_funding_time_ms || item.latest_funding_time_ms),
    ),
    compactStat("最晚平仓", fmtTime(item.target_close_time_ms)),
    compactStat(
      "关联机会",
      linkedOpp ? "已关联" : "--",
      linkedOpp ? "positive" : "muted-text",
    ),
  ];
  return `<div class="compact-stat-grid">${metrics.join("")}</div>`;
}

function renderCompactPlanCard(
  item,
  { selected = false, linkedOpp = null, showActions = true } = {},
) {
  const linkedOppKey = linkedOpp ? opportunityKey(linkedOpp) : "";
  const isInteractive = Boolean(showActions) || Boolean(linkedOppKey);
  const positionSkew = planPositionSkewBps(item);
  const sameExchange = isSameExchangePlan(item, linkedOpp);
  const summaryLine = [
    planDisplaySubtitle(item, linkedOpp),
    `${sameExchange ? "对冲偏差" : "仓位偏移"} ${positionSkew == null ? "--" : fmtSignedBps(positionSkew, 2)}`,
    `${sameExchange ? "现货-永续基差" : "Basis"} ${fmtNumber(item.cross_venue_basis_bps, 4)} bps`,
  ].join(" · ");
  return `
    <div class="plan-card plan-card-compact ${isInteractive ? "clickable" : ""} ${selected ? "active" : ""}" ${isInteractive ? `data-plan-select="1" data-plan-key="${item.plan_key}" data-opportunity-key="${linkedOppKey}"` : ""}>
      <div class="plan-head plan-head-compact">
        <div>
          <div class="plan-title">${planDisplayTitle(item, linkedOpp)}</div>
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
  const expectedPnl = executionExpectedPnl(item);
  const allocatedNotional = executionAllocatedNotional(item);
  return `
    <div class="plan-card plan-card-compact">
      <div class="plan-head plan-head-compact">
        <div>
          <div class="plan-title">${executionDisplayTitle(item)}</div>
          <div class="plan-sub">${item.plan_key} · ${executionDisplaySubtitle(item)}</div>
        </div>
        <div class="plan-head-right">
          <span class="pill ${String(item.status || "").includes("closed") ? "warn" : "good"}">${explainStatus(item.status)}</span>
          <div class="plan-pnl">${item.live_trading ? "LIVE" : "DRY"}</div>
        </div>
      </div>
      <div class="compact-stat-grid">
        ${compactStat("开仓时间", fmtTime(item.opened_at_ms))}
        ${compactStat("平仓时间", fmtTime(item.closed_at_ms))}
        ${compactStat(
          "占用名义",
          allocatedNotional == null ? "--" : fmtMoney(allocatedNotional, 2),
        )}
        ${compactStat(
          "持有模式",
          holdSelectionModeText(state.system?.strategy?.hold_selection_mode),
        )}
        ${compactStat("计划持仓", executionPlannedHoldText(item))}
        ${compactStat("剩余持有", executionRemainingHoldText(item))}
        ${compactStat("最晚平仓", fmtTime(item.target_close_time_ms))}
        ${compactStat("开仓单", String(openCount))}
        ${compactStat("平仓单", String(closeCount))}
        ${compactStat("自动平仓", item.auto_close ? "YES" : "NO")}
        ${compactStat(
          "预期收益",
          expectedPnl == null ? "--" : fmtMoney(expectedPnl),
          expectedPnl == null ? "muted-text" : classForNumber(expectedPnl),
        )}
      </div>
      ${item.last_error ? `<div class="status-desc compact-status-desc">${item.last_error}</div>` : ""}
      ${renderExecutionControls(item.plan_key, item.status)}
    </div>
  `;
}

function renderPlans() {
  const { sourcePlans, emptyText, batchBanner } = resolvePlanViewState();

  if (!sourcePlans.length) {
    els.plansEmpty.style.display = "block";
    els.plansEmpty.textContent = emptyText;
    els.plansList.innerHTML = "";
    renderExecutionBoardSummary([]);
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
  renderExecutionBoardSummary(sortedPlans);
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

function autoCloseDecisionText(item) {
  const decision = item?.decision || {};
  if (decision.error) return "判断失败";
  if (decision.should_close) return decision.trigger || "应平仓";
  if (!decision.eligible) return "不在扫描范围";
  if (!decision.auto_close_enabled) return "已禁用自动平仓";
  return "继续持有";
}

function autoCloseDecisionClass(item) {
  const decision = item?.decision || {};
  if (decision.error) return "negative";
  if (decision.should_close) return "positive";
  if (!decision.eligible || !decision.auto_close_enabled) return "negative";
  return "muted-text";
}

function autoCloseReasonText(item) {
  const decision = item?.decision || {};
  return decision.error || decision.reason || "--";
}

function renderAutoCloseSummary() {
  if (!els.autoCloseSummary) return;
  const info = state.autoClose || {};
  els.autoCloseSummary.innerHTML = `
    <div class="compact-stat-grid execution-board-summary-grid">
      ${compactStat("Live 候选", String(info.total || 0))}
      ${compactStat("可扫描", String(info.eligible || 0))}
      ${compactStat("现在应平仓", String(info.should_close || 0), Number(info.should_close || 0) > 0 ? "positive" : "muted-text")}
      ${compactStat("判断错误", String(info.decision_errors || 0), Number(info.decision_errors || 0) > 0 ? "negative" : "muted-text")}
      ${compactStat("已禁用 Auto-Close", String(info.auto_close_disabled || 0), Number(info.auto_close_disabled || 0) > 0 ? "warn" : "muted-text")}
      ${compactStat("最近评估", fmtTime(info.evaluated_at_ms))}
    </div>
    <div class="status-desc execution-board-summary-note">
      候选列表只展示当前仍然带 <code>live_trading=true</code> 的 execution record。卡片上的“继续持有 / 应平仓 / 判断失败”直接对应服务端本轮 auto-close 决策。
    </div>
  `;
}

function renderAutoCloseCandidateCard(item) {
  const execution = item?.execution || {};
  const plan = item?.plan || null;
  const allocatedNotional = executionAllocatedNotional(execution);
  const plannedHold = plan
    ? planExpectedHoldText(plan)
    : executionPlannedHoldText(execution);
  const reasonText = autoCloseReasonText(item);
  return `
    <div class="plan-card plan-card-compact">
      <div class="plan-head plan-head-compact">
        <div>
          <div class="plan-title">${executionDisplayTitle(execution, plan)}</div>
          <div class="plan-sub">${execution.plan_key || "--"}${plan?.plan_key ? ` · plan=${plan.plan_key}` : ""} · ${executionDisplaySubtitle(execution, plan)}</div>
        </div>
        <div class="plan-head-right">
          <span class="pill ${autoCloseDecisionClass(item) === "positive" ? "good" : autoCloseDecisionClass(item) === "negative" ? "bad" : "warn"}">${autoCloseDecisionText(item)}</span>
          <div class="plan-pnl">${execution.live_trading ? "LIVE" : "DRY"}</div>
        </div>
      </div>
      <div class="compact-stat-grid">
        ${compactStat("状态", explainStatus(execution.status))}
        ${compactStat("自动平仓", execution.auto_close ? "YES" : "NO")}
        ${compactStat("占用名义", allocatedNotional == null ? "--" : fmtMoney(allocatedNotional, 2))}
        ${compactStat(
          "持有模式",
          holdSelectionModeText(state.system?.strategy?.hold_selection_mode),
        )}
        ${compactStat("计划持仓", plannedHold)}
        ${compactStat("剩余持有", executionRemainingHoldText(execution))}
        ${compactStat("目标平仓", fmtTime(execution.target_close_time_ms))}
        ${compactStat("开仓时间", fmtTime(execution.opened_at_ms))}
        ${compactStat("决策", autoCloseDecisionText(item), autoCloseDecisionClass(item))}
        ${compactStat("触发器", item?.decision?.trigger || "--", item?.decision?.should_close ? "positive" : "muted-text")}
        ${compactStat("最后状态迁移", fmtTime(execution.last_transition_at_ms))}
      </div>
      <div class="status-desc compact-status-desc">${reasonText}</div>
      ${execution.last_error ? `<div class="status-desc compact-status-desc negative">${execution.last_error}</div>` : ""}
      ${renderExecutionControls(execution.plan_key, execution.status)}
    </div>
  `;
}

function renderAutoCloseCandidates() {
  renderAutoCloseSummary();
  const items = Array.isArray(state.autoClose?.candidates)
    ? state.autoClose.candidates
    : [];
  if (!items.length) {
    els.autoCloseEmpty.style.display = "block";
    els.autoCloseList.innerHTML = "";
    return;
  }
  els.autoCloseEmpty.style.display = "none";
  els.autoCloseList.innerHTML = items
    .map((item) => renderAutoCloseCandidateCard(item))
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
  const sameExchangeMode =
    String(state.system?.strategy?.arbitrage_mode || "").toLowerCase() ===
    "same_exchange_spot_perp";

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
    if (sameExchangeMode) {
      if (sortMode === "funding") {
        if (Number(b.short_funding_rate || 0) !== Number(a.short_funding_rate || 0)) {
          return Number(b.short_funding_rate || 0) - Number(a.short_funding_rate || 0);
        }
        if (
          Number(a.same_exchange_basis_payback_funding_events || 0) !==
          Number(b.same_exchange_basis_payback_funding_events || 0)
        ) {
          const leftPayback = Number(a.same_exchange_basis_payback_funding_events || 0);
          const rightPayback = Number(b.same_exchange_basis_payback_funding_events || 0);
          if (leftPayback <= 0) return 1;
          if (rightPayback <= 0) return -1;
          return leftPayback - rightPayback;
        }
        return Number(b.net_expected_pnl || 0) - Number(a.net_expected_pnl || 0);
      }
      if (sortMode === "score") {
        return Number(b.score || 0) - Number(a.score || 0);
      }
      if (sortMode === "edge") {
        return Number(b.gross_funding_pnl || 0) - Number(a.gross_funding_pnl || 0);
      }
      return Number(b.net_expected_pnl || 0) - Number(a.net_expected_pnl || 0);
    }

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

  const planStatus = opportunityPlanStatus(item);
  const matchedPlan = bestPlanForOpportunity(item);
  const displayPlan = matchedPlan || syntheticPlanForOpportunity(item);
  const sameExchange = isSameExchangeOpportunity(item);
  const summaryMetrics = sameExchange
    ? [
        summaryMetric("预计净收益", fmtMoney(item.net_expected_pnl), classForNumber(item.net_expected_pnl)),
        summaryMetric("净收益率", fmtSignedBps(item.net_expected_bps, 2), classForNumber(item.net_expected_bps)),
        summaryMetric("现货占资 / 对冲名义", targetNotionalText(item, displayPlan)),
        summaryMetric(carryMetricLabel(item), fmtPctRatio(fundingSpread(item), 5), classForNumber(fundingSpread(item))),
        summaryMetric(basisMetricLabel(item), fmtSignedBps(item.basis_bps, 2)),
        summaryMetric("基差回本", sameExchangeBasisPaybackText(item)),
        summaryMetric("基差动作", sameExchangeBasisActionText(item), sameExchangeBasisActionClass(item)),
        summaryMetric("历史负费率", sameExchangePlanHistoryNegativeText(item)),
        summaryMetric("长持判定", sameExchangePlanLongHoldText(item)),
        summaryMetric(currentFundingAnchorLabel(item), currentFundingAnchorValue(item)),
        summaryMetric("计划状态", planStatus.text, planStatus.cls === "good" ? "positive" : planStatus.cls === "bad" ? "negative" : "muted-text"),
        summaryMetric("进入计划说明", planStatus.reason || "--"),
        summaryMetric("套利模式", arbitrageModeText(opportunityArbitrageMode(item))),
        summaryMetric("持有模式", holdSelectionModeText(state.system?.strategy?.hold_selection_mode)),
        summaryMetric("预计持有时长", opportunityExpectedHoldText(item)),
        summaryMetric("预计持有到", opportunityExpectedCloseText(item)),
      ]
    : [
        summaryMetric("净收益", fmtMoney(item.net_expected_pnl), classForNumber(item.net_expected_pnl)),
        summaryMetric("净收益率", fmtSignedBps(item.net_expected_bps, 2), classForNumber(item.net_expected_bps)),
        summaryMetric(carryMetricLabel(item), fmtPctRatio(fundingSpread(item), 5), classForNumber(fundingSpread(item))),
        summaryMetric(currentFundingAnchorLabel(item), currentFundingAnchorValue(item)),
        summaryMetric("计划状态", planStatus.text, planStatus.cls === "good" ? "positive" : planStatus.cls === "bad" ? "negative" : "muted-text"),
        summaryMetric("进入计划说明", planStatus.reason || "--"),
        summaryMetric("套利模式", arbitrageModeText(opportunityArbitrageMode(item))),
        summaryMetric("持有模式", holdSelectionModeText(state.system?.strategy?.hold_selection_mode)),
        summaryMetric("预计持有时长", opportunityExpectedHoldText(item)),
        summaryMetric("预计持有到", opportunityExpectedCloseText(item)),
        summaryMetric("产生时间", fmtTime(opportunityProducedTimeMs(item))),
      ];
  els.opportunitySummary.innerHTML = `
    <div class="summary-card">
      <div>
        <div class="summary-kicker">当前默认展示</div>
        <div class="summary-title">${opportunityDisplayTitle(item)}</div>
        <div class="summary-desc">${opportunityDisplayDescription(item)}</div>
      </div>
      <div class="summary-metric-grid">
        ${summaryMetrics.join("")}
      </div>
    </div>
  `;
}

function renderOpportunityList(items) {
  els.opportunitiesList.innerHTML = items
    .map((item) => {
      const active = opportunityKey(item) === state.selectedOpportunityKey;
      const planStatus = opportunityPlanStatus(item);
      const sameExchange = isSameExchangeOpportunity(item);
      const listMetrics = sameExchange
        ? [
            `<div><span>套利模式</span><strong>${arbitrageModeText(opportunityArbitrageMode(item))}</strong></div>`,
            `<div><span>结构</span><strong>${opportunityModePillText(item)}</strong></div>`,
            `<div><span>当前永续 funding</span><strong class="${classForNumber(item.short_funding_rate)}">${fmtPctRatio(item.short_funding_rate, 5)}</strong></div>`,
            `<div><span>单轮毛收</span><strong class="${classForNumber(sameExchangeNextFundingGrossPNL(item))}">${fmtMoney(sameExchangeNextFundingGrossPNL(item))}</strong></div>`,
            `<div><span>主窗毛收</span><strong class="${classForNumber(sameExchangeWindowGrossPNL(item))}">${fmtMoney(sameExchangeWindowGrossPNL(item))}</strong></div>`,
            `<div><span>基差回本</span><strong>${sameExchangeBasisPaybackText(item)}</strong></div>`,
            `<div><span>基差动作</span><strong class="${sameExchangeBasisActionClass(item)}">${sameExchangeBasisActionText(item)}</strong></div>`,
            `<div><span>历史负费率</span><strong>${sameExchangePlanHistoryNegativeText(item)}</strong></div>`,
            `<div><span>长持判定</span><strong>${sameExchangePlanLongHoldText(item)}</strong></div>`,
          ]
        : [
            `<div><span>套利模式</span><strong>${arbitrageModeText(opportunityArbitrageMode(item))}</strong></div>`,
            `<div><span>组合</span><strong>${opportunityPair(item)}</strong></div>`,
            `<div><span>${basisMetricLabel(item)}</span><strong>${fmtSignedBps(item.basis_bps, 2)}</strong></div>`,
            `<div><span>${carryMetricLabel(item)}</span><strong>${fmtPctRatio(fundingSpread(item), 5)}</strong></div>`,
            `<div><span>${currentFundingAnchorLabel(item)}</span><strong>${currentFundingAnchorValue(item)}</strong></div>`,
            `<div><span>预计持有</span><strong>${opportunityExpectedHoldText(item)}</strong></div>`,
            `<div><span>产生时间</span><strong>${fmtTime(opportunityProducedTimeMs(item))}</strong></div>`,
          ];
      return `
	        <button class="opportunity-item ${active ? "active" : ""}" data-opportunity-key="${opportunityKey(item)}" type="button">
          <div class="opportunity-item-head">
            <div>
              <div class="opportunity-item-title">${opportunityDisplayTitle(item)}</div>
              <div class="opportunity-item-sub">${sameExchange ? `${opportunityDisplaySubtitle(item)} · 排名 ${sameExchangePlanFundingRankText(item)}` : opportunityDisplaySubtitle(item)}</div>
            </div>
            <div class="opportunity-item-pnl ${classForNumber(item.net_expected_pnl)}">${fmtMoney(item.net_expected_pnl)}</div>
          </div>
	          <div class="opportunity-mini-grid">
	            ${listMetrics.join("")}
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

function detailTabButton(key, label) {
  const active = String(state.opportunityDetailTab || "overview") === String(key);
  return `
    <button
      class="detail-tab-btn ${active ? "active" : ""}"
      data-detail-tab="${key}"
      type="button"
    >
      ${label}
    </button>
  `;
}

function diagnosticItem(label, value, extraClass = "") {
  return `
    <div class="detail-item">
      <span class="detail-k">${label}</span>
      <span class="detail-v ${extraClass}">${value}</span>
    </div>
  `;
}

function diagnosticSection(title, subtitle, rows, pill = "") {
  return `
    <div class="detail-section detail-section-tight">
      <div class="detail-section-head">
        <div>
          <div class="detail-section-title">${title}</div>
          ${subtitle ? `<div class="detail-subtitle">${subtitle}</div>` : ""}
        </div>
        ${pill}
      </div>
      <div class="detail-list compact-list">
        ${rows.join("")}
      </div>
    </div>
  `;
}

function sameExchangeHistoricalSupportText(item, plan) {
  const rule = sameExchangePerpRule(item);
  const ruleValue = Number(rule?.historical_support_ratio);
  if (Number.isFinite(ruleValue) && Number(rule?.history_sample_count || 0) > 0) {
    return fmtPctRatio(ruleValue, 2);
  }
  const planValue = Number(plan?.perp_funding_historical_support_ratio);
  if (
    Number.isFinite(planValue) &&
    Number(plan?.perp_funding_history_sample_count || 0) > 0
  ) {
    return fmtPctRatio(planValue, 2);
  }
  return "--";
}

function sameExchangeSuggestedNetPNLValue(item, plan) {
  const rule = sameExchangePerpRule(item);
  const ruleValue = Number(rule?.suggested_net_pnl);
  if (Number.isFinite(ruleValue) && Number(rule?.suggested_funding_events || 0) > 0) {
    return ruleValue;
  }
  const planValue = Number(plan?.same_exchange_long_hold_suggested_net_pnl);
  if (Number.isFinite(planValue) && Number(plan?.same_exchange_long_hold_suggested_funding_events || 0) > 0) {
    return planValue;
  }
  return null;
}

function opportunityDiagnosticSummary(item, matchedPlan, displayPlan, sameExchange, sameExchangeRisk) {
  const strategy = state.system?.strategy || {};
  const execution = state.system?.execution || {};
  const minNetPnl = Number(strategy.min_net_pnl || 0);
  const netExpectedPnl = Number(item?.net_expected_pnl || 0);
  const minAnnualized = Number(strategy.same_exchange_min_annualized_net_rate || 0);
  const annualizedNet = Number(displayPlan?.perp_funding_estimated_annualized_net_rate);
  const liqDistance = Number(sameExchangeRisk?.liquidationDistanceRatio);
  const minLiqDistance = Number(strategy.same_exchange_min_liq_distance_ratio || 0);
  const requireLongHold = Boolean(strategy.same_exchange_require_long_hold_eligible);
  const longHoldEligible = Boolean(
    displayPlan?.same_exchange_long_hold_eligible ??
      sameExchangePerpRule(item)?.long_hold_eligible,
  );
  const suggestedNet = sameExchangeSuggestedNetPNLValue(item, displayPlan);
  const blockers = [];
  const notes = [];

  if (!execution.live_trading_enabled) blockers.push("实盘开关未开启，当前只会 dry-run。");
  if (!execution.auto_entry) blockers.push("自动开仓未开启，需要手动触发。");
  if (!item?.eligible_for_execution) {
    blockers.push(`机会层未通过: ${explainOpportunityRejectReason(item) || explainStatus(item?.status)}`);
  }
  if (!matchedPlan) {
    blockers.push(
      item?.eligible_for_execution
        ? "机会已通过筛选，但最新计划批次里还没有 ready plan。"
        : "当前没有生成可用执行计划。",
    );
  } else if (!matchedPlan.ready_now) {
    blockers.push(`执行计划尚未 ready: ${explainStatus(matchedPlan.status) || matchedPlan.status || "--"}`);
  }
  if (Number.isFinite(minNetPnl) && netExpectedPnl < minNetPnl) {
    blockers.push(
      `预计净收益 ${fmtMoney(netExpectedPnl)} 仍低于门槛 ${fmtMoney(minNetPnl)}。`,
    );
  }
  if (sameExchange && requireLongHold && !longHoldEligible) {
    blockers.push(`长期持有条件未通过: ${sameExchangeLongHoldReasonText(item)}。`);
  }
  if (sameExchange && sameExchangeRisk && !sameExchangeRisk.priceShockAllowed) {
    blockers.push(
      `价格异动风控阻断: ${sameExchangePriceRiskReasonText(
        sameExchangeRisk.priceShockReason,
      )}。`,
    );
  }
  if (
    sameExchange &&
    Number.isFinite(liqDistance) &&
    minLiqDistance > 0 &&
    liqDistance < minLiqDistance
  ) {
    blockers.push(
      `爆仓距离 ${fmtPctRatio(liqDistance, 2)} 低于开仓线 ${fmtPctRatio(
        minLiqDistance,
        2,
      )}。`,
    );
  }

  if (
    sameExchange &&
    Number.isFinite(annualizedNet) &&
    annualizedNet >= minAnnualized &&
    Number.isFinite(suggestedNet) &&
    suggestedNet < minNetPnl
  ) {
    notes.push(
      `粗年化净收益已经达到 ${fmtPctRatio(
        annualizedNet,
        2,
      )}，但建议持有期净收益只有 ${fmtMoney(
        suggestedNet,
      )}。这通常不是年化不够，而是名义仓位偏小，或手续费、滑点、安全缓冲把绝对收益吃掉了。`,
    );
  }
  if (!matchedPlan && item?.eligible_for_execution) {
    notes.push("机会本身已经可执行，通常只是在等下一轮计划刷新把它落成 ready plan。");
  }
  if (sameExchange) {
    notes.push("余额、最小下单量、暴露上限会在真实发单前由后端再复核一次，这里先展示页面可见的主要阻塞条件。");
  }

  return {
    blockers,
    notes,
    canAutoOpen: blockers.length === 0,
  };
}

function renderOpportunityDiagnosticsTab(
  item,
  matchedPlan,
  displayPlan,
  sameExchange,
  sameExchangeRisk,
  planStatus,
) {
  const strategy = state.system?.strategy || {};
  const execution = state.system?.execution || {};
  const summary = opportunityDiagnosticSummary(
    item,
    matchedPlan,
    displayPlan,
    sameExchange,
    sameExchangeRisk,
  );
  const rule = sameExchange ? sameExchangePerpRule(item) : null;
  const historySampleCount = Number(
    rule?.history_sample_count ?? displayPlan?.perp_funding_history_sample_count ?? 0,
  );
  const historyPositiveRatio =
    historySampleCount > 0
      ? fundingHistoryRatioText(
          rule?.history_positive_ratio ??
            displayPlan?.perp_funding_history_positive_ratio,
          historySampleCount,
        )
      : "--";
  const historyNegativeRatio =
    historySampleCount > 0
      ? fundingHistoryRatioText(
          rule?.history_negative_ratio ??
            displayPlan?.perp_funding_history_negative_ratio,
          historySampleCount,
        )
      : "--";
  const annualizedNet = Number(displayPlan?.perp_funding_estimated_annualized_net_rate);
  const minAnnualized = Number(strategy.same_exchange_min_annualized_net_rate || 0);
  const minNetPnl = Number(strategy.min_net_pnl || 0);
  const netExpectedPnl = Number(item?.net_expected_pnl || 0);
  const netGap = netExpectedPnl - minNetPnl;
  const liqDistance = Number(sameExchangeRisk?.liquidationDistanceRatio);
  const minLiqDistance = Number(strategy.same_exchange_min_liq_distance_ratio || 0);
  const priceShockThreshold = Number(
    sameExchangeRisk?.priceShockThresholdRatio ??
      strategy.same_exchange_max_1h_price_shock_ratio ??
      0,
  );

  const summarySection = diagnosticSection(
    "自动下单结论",
    summary.canAutoOpen
      ? "当前页面可见条件已经通过，下一轮执行循环可以自动尝试开仓。"
      : "下面会把最主要的阻塞条件拆成几层，方便我们判断到底卡在收益、计划还是风控。",
    [
      diagnosticItem(
        "当前结论",
        summary.canAutoOpen ? "满足自动下单前置条件" : "暂不满足自动下单条件",
        summary.canAutoOpen ? "positive" : "negative",
      ),
      diagnosticItem(
        "当前主要卡点",
        summary.blockers.length ? summary.blockers[0] : "未发现页面可见阻塞项",
        summary.blockers.length ? "negative" : "positive",
      ),
      diagnosticItem(
        "阻塞项数量",
        summary.blockers.length ? `${summary.blockers.length} 项` : "0 项",
        summary.blockers.length ? "warn" : "positive",
      ),
      diagnosticItem(
        "补充说明",
        summary.notes[0] || "真实发单前，后端还会再做一次余额、最小下单量和暴露风控复核。",
      ),
    ],
    `<span class="pill ${summary.canAutoOpen ? "good" : "bad"}">${summary.canAutoOpen ? "可自动开仓" : "仍有阻塞"}</span>`,
  );

  const executionSwitchSection = diagnosticSection(
    "交易开关",
    "先确认系统是否允许真实自动下单。",
    [
      diagnosticItem(
        "实盘开关",
        execution.live_trading_enabled ? "已开启" : "未开启，仅会 dry-run",
        execution.live_trading_enabled ? "positive" : "negative",
      ),
      diagnosticItem(
        "自动开仓",
        execution.auto_entry ? "已开启" : "未开启",
        execution.auto_entry ? "positive" : "negative",
      ),
      diagnosticItem(
        "自动分配资金",
        boolText(execution.auto_allocate_capital),
      ),
      diagnosticItem(
        "当前 Live 计划",
        execution.max_live_plans > 0
          ? `${Number(execution.active_live_plans || 0)}/${Number(execution.max_live_plans || 0)}`
          : `${Number(execution.active_live_plans || 0)}/不限`,
      ),
      diagnosticItem(
        "每轮开仓上限",
        execution.max_auto_open_per_loop > 0
          ? String(execution.max_auto_open_per_loop)
          : "不限",
      ),
    ],
    `<span class="pill ${execution.live_trading_enabled && execution.auto_entry ? "good" : "warn"}">${execution.live_trading_enabled && execution.auto_entry ? "允许自动下单" : "先检查开关"}</span>`,
  );

  const opportunityStageRows = [
    diagnosticItem("机会状态", explainStatus(item.status)),
    diagnosticItem(
      "是否可执行",
      item.eligible_for_execution ? "是" : "否",
      item.eligible_for_execution ? "positive" : "negative",
    ),
    diagnosticItem("拒绝原因", explainOpportunityRejectReason(item) || "--"),
    diagnosticItem(
      "预计净收益",
      fmtMoney(netExpectedPnl),
      classForNumber(netExpectedPnl),
    ),
    diagnosticItem("最低净收益门槛", fmtMoney(minNetPnl)),
    diagnosticItem(
      "净收益差额",
      `${netGap >= 0 ? "高出" : "仍差"} ${fmtMoney(Math.abs(netGap))}`,
      netGap >= 0 ? "positive" : "negative",
    ),
    diagnosticItem("计划状态", planStatus.text, planStatus.cls === "good" ? "positive" : planStatus.cls === "bad" ? "negative" : "warn"),
    diagnosticItem("计划说明", planStatus.reason || "--"),
  ];
  if (sameExchange) {
    opportunityStageRows.push(
      diagnosticItem("收益口径", sameExchangeLongHoldSourceText(item)),
      diagnosticItem("长持原因", sameExchangeLongHoldReasonText(item)),
      diagnosticItem(
        "建议持有",
        `${sameExchangeSuggestedHoldEventsText(item)} / ${sameExchangeSuggestedHoldDurationText(item)}`,
      ),
      diagnosticItem(
        "历史预估净收益",
        sameExchangeSuggestedNetPNLText(item),
        classForNumber(sameExchangeSuggestedNetPNLValue(item, displayPlan)),
      ),
    );
  }
  const opportunityStageSection = diagnosticSection(
    sameExchange ? "机会层与长持门槛" : "机会层门槛",
    sameExchange
      ? "同所模式会同时检查绝对净收益门槛和长期持有资格；年化高不等于一定能自动下单。"
      : "跨所模式主要看当前机会状态、净收益门槛和计划状态。",
    opportunityStageRows,
    `<span class="pill ${item.eligible_for_execution ? "good" : "bad"}">${item.eligible_for_execution ? "机会层通过" : "机会层未过"}</span>`,
  );

  const planStageRows = [
    diagnosticItem(
      "是否已有真实计划",
      matchedPlan ? "是" : "否，仅能看到预览",
      matchedPlan ? "positive" : "negative",
    ),
    diagnosticItem("计划主键", matchedPlan?.plan_key || "--"),
    diagnosticItem("计划状态", matchedPlan?.status || "--"),
    diagnosticItem(
      "ReadyNow",
      matchedPlan ? (matchedPlan.ready_now ? "是" : "否") : "--",
      matchedPlan?.ready_now ? "positive" : matchedPlan ? "warn" : "muted-text",
    ),
    diagnosticItem("最晚入场时间", fmtTime(item.required_entry_by_funding_time_ms || item.earliest_funding_time_ms)),
    diagnosticItem("当前收益兑现点", fmtTime(item.projected_funding_time_ms || item.latest_funding_time_ms)),
    diagnosticItem("预计持有到", opportunityExpectedCloseText(item)),
    diagnosticItem("预计总持有", opportunityExpectedHoldText(item)),
  ];
  if (sameExchange) {
    planStageRows.push(
      diagnosticItem("现货占资 / 对冲名义", targetNotionalText(item, displayPlan)),
      diagnosticItem(
        "永续目标杠杆",
        displayPlan ? `${fmtNumber(planTargetLeverage(displayPlan), 2)}x` : "--",
      ),
    );
  }
  const planStageSection = diagnosticSection(
    "计划与时机",
    "自动开仓一定要等到 ready plan；机会本身可执行，不等于这一刻已经有可发单计划。",
    planStageRows,
    `<span class="pill ${matchedPlan?.ready_now ? "good" : matchedPlan ? "warn" : "bad"}">${matchedPlan?.ready_now ? "计划就绪" : matchedPlan ? "等待就绪" : "尚无计划"}</span>`,
  );

  const riskRows = [];
  if (sameExchange) {
    riskRows.push(
      diagnosticItem(
        "1h 价格冲击",
        fmtPctRatio(sameExchangeRisk?.priceShockRatio, 2),
        classForNumber(sameExchangeRisk?.priceShockRatio),
      ),
      diagnosticItem("价格风控阈值", fmtPctRatio(priceShockThreshold, 2)),
      diagnosticItem(
        "价格风控",
        sameExchangeRisk?.priceShockAllowed
          ? "安全"
          : sameExchangePriceRiskReasonText(sameExchangeRisk?.priceShockReason),
        sameExchangeRisk?.priceShockAllowed ? "positive" : "negative",
      ),
      diagnosticItem(
        "爆仓距离",
        Number.isFinite(liqDistance) ? fmtPctRatio(liqDistance, 2) : "--",
        sameExchangeLiquidationClass(sameExchangeRisk?.liveRisk, liqDistance),
      ),
      diagnosticItem(
        "最低爆仓距离",
        minLiqDistance > 0 ? fmtPctRatio(minLiqDistance, 2) : "--",
      ),
      diagnosticItem(
        "保护单状态",
        sameExchangeProtectiveStatusText(sameExchangeRisk?.liveRisk),
        sameExchangeProtectiveStatusClass(sameExchangeRisk?.liveRisk),
      ),
    );
  } else {
    riskRows.push(
      diagnosticItem("跨所价差判断", basisDecisionText(item)),
      diagnosticItem("动态价差阈值", fmtSignedBps(maxAllowedBasisForItem(item), 2)),
      diagnosticItem("Entry Path 截止原因", item.entry_path_stop_reason || "--"),
      diagnosticItem("当前事件窗口", `${fmtNumber(item.funding_window_hours || 0, 2)} h`),
    );
  }
  const riskSection = diagnosticSection(
    sameExchange ? "开仓前风控" : "机会风控",
    sameExchange
      ? "同所模式下，真正决定能不能持有的是价格异动、爆仓距离和账户层最终复核。"
      : "跨所模式下，当前风控重点还是 basis、持有窗口和资金收益覆盖。",
    riskRows,
    `<span class="pill ${sameExchange ? (sameExchangeRisk?.priceShockAllowed ? "good" : "bad") : item.eligible_for_execution ? "good" : "warn"}">${sameExchange ? (sameExchangeRisk?.priceShockAllowed ? "风控通过" : "风控阻断") : "关注机会风控"}</span>`,
  );

  const sameExchangeHistorySection =
    sameExchange
      ? diagnosticSection(
          "历史 funding 参考",
          "这里把长期持有判断用到的历史样本单独拎出来，方便判断为什么年化高但绝对收益还不够。",
          [
            diagnosticItem("历史样本数", historySampleCount > 0 ? fmtNumber(historySampleCount, 0) : "--"),
            diagnosticItem("历史正费率占比", historyPositiveRatio),
            diagnosticItem("历史负费率占比", historyNegativeRatio),
            diagnosticItem("历史同向支持", sameExchangeHistoricalSupportText(item, displayPlan)),
            diagnosticItem(
              "粗年化净收益",
              Number.isFinite(annualizedNet) ? fmtPctRatio(annualizedNet, 2) : "--",
              Number.isFinite(annualizedNet) && annualizedNet >= minAnnualized
                ? "positive"
                : Number.isFinite(annualizedNet)
                  ? "negative"
                  : "muted-text",
            ),
            diagnosticItem(
              "最低年化门槛",
              fmtPctRatio(minAnnualized, 2),
            ),
          ],
          `<span class="pill ${historySampleCount > 0 ? "good" : "warn"}">${historySampleCount > 0 ? "已用历史样本" : "历史样本不足"}</span>`,
        )
      : "";

  const blockerList = summary.blockers.length
    ? `
      <div class="detail-section detail-section-tight">
        <div class="detail-section-head">
          <div>
            <div class="detail-section-title">当前阻塞项</div>
            <div class="detail-subtitle">这里只列最可能影响真实自动下单的页面可见条件。</div>
          </div>
          <span class="pill bad">${summary.blockers.length} 项</span>
        </div>
        <div class="detail-bullet-list">
          ${summary.blockers
            .map((text) => `<div class="detail-bullet negative">${text}</div>`)
            .join("")}
        </div>
      </div>
    `
    : `
      <div class="detail-section detail-section-tight">
        <div class="detail-section-head">
          <div>
            <div class="detail-section-title">当前阻塞项</div>
            <div class="detail-subtitle">页面可见条件已经通过，接下来主要取决于执行循环和后端实时账户复核。</div>
          </div>
          <span class="pill good">0 项</span>
        </div>
        <div class="detail-bullet-list">
          <div class="detail-bullet positive">未发现页面可见阻塞项。</div>
        </div>
      </div>
    `;

  const noteList = summary.notes.length
    ? `
      <div class="detail-section detail-section-tight">
        <div class="detail-section-head">
          <div>
            <div class="detail-section-title">补充说明</div>
            <div class="detail-subtitle">把容易让人误判的地方单独说明清楚。</div>
          </div>
          <span class="pill warn">${summary.notes.length} 条</span>
        </div>
        <div class="detail-bullet-list">
          ${summary.notes
            .map((text) => `<div class="detail-bullet muted-text">${text}</div>`)
            .join("")}
        </div>
      </div>
    `
    : "";

  return `
    <div class="detail-tab-pane">
      ${summarySection}
      <div class="detail-grid-2 detail-diagnostic-grid">
        ${executionSwitchSection}
        ${opportunityStageSection}
        ${planStageSection}
        ${riskSection}
      </div>
      ${sameExchangeHistorySection}
      ${blockerList}
      ${noteList}
    </div>
  `;
}

function sameExchangeRiskCard(item, plan) {
  const snapshot = sameExchangeRiskSnapshot(item, plan);
  const risk = snapshot.liveRisk;
  const priceGuardText = snapshot.priceShockAllowed
    ? "安全"
    : sameExchangePriceRiskReasonText(snapshot.priceShockReason);
  const liqText = Number.isFinite(snapshot.liquidationDistanceRatio)
    ? fmtPctRatio(snapshot.liquidationDistanceRatio, 2)
    : "--";
  return `
    <div class="detail-section">
      <div class="detail-section-head">
        <div class="detail-section-title">风险监控</div>
        <span class="pill ${snapshot.priceShockAllowed ? "good" : "bad"}">${priceGuardText}</span>
      </div>
      <div class="detail-list compact-list">
        <div class="detail-item"><span class="detail-k">1h 价格冲击</span><span class="detail-v ${classForNumber(snapshot.priceShockRatio)}">${fmtPctRatio(snapshot.priceShockRatio, 2)}</span></div>
        <div class="detail-item"><span class="detail-k">风控阈值</span><span class="detail-v">${fmtPctRatio(snapshot.priceShockThresholdRatio, 2)}</span></div>
        <div class="detail-item"><span class="detail-k">基准标记价</span><span class="detail-v">${priceText(snapshot.baselineMarkPrice)}</span></div>
        <div class="detail-item"><span class="detail-k">当前标记价</span><span class="detail-v">${priceText(snapshot.currentMarkPrice)}</span></div>
        <div class="detail-item"><span class="detail-k">价格风控</span><span class="detail-v ${snapshot.priceShockAllowed ? "positive" : "negative"}">${priceGuardText}</span></div>
        <div class="detail-item"><span class="detail-k">爆仓价</span><span class="detail-v">${priceText(snapshot.liquidationPrice)}</span></div>
        <div class="detail-item"><span class="detail-k">爆仓距离</span><span class="detail-v ${sameExchangeLiquidationClass(risk, snapshot.liquidationDistanceRatio)}">${liqText}</span></div>
        <div class="detail-item"><span class="detail-k">保护单</span><span class="detail-v ${sameExchangeProtectiveStatusClass(risk)}">${sameExchangeProtectiveStatusText(risk)}</span></div>
        <div class="detail-item"><span class="detail-k">保护价</span><span class="detail-v">${priceText(risk?.protective_order_stop_price)}</span></div>
        <div class="detail-item"><span class="detail-k">保护更新时间</span><span class="detail-v">${fmtTime(risk?.protective_order_updated_at_ms)}</span></div>
      </div>
      ${
        snapshot.priceShockReason && !snapshot.priceShockAllowed
          ? `<div class="formula-box">${sameExchangePriceRiskReasonText(snapshot.priceShockReason)}</div>`
          : ""
      }
      ${
        risk?.protective_order_error_message
          ? `<div class="formula-box">${risk.protective_order_error_message}</div>`
          : ""
      }
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
	        <span class="pill ${title.includes("做空") || title.includes("空头") ? "warn" : "good"}">${exchange}</span>
	      </div>
      <div class="detail-list compact-list">
        <div class="detail-item"><span class="detail-k">交易对</span><span class="detail-v">${venueSymbol || "--"}</span></div>
        <div class="detail-item"><span class="detail-k">当前 next funding费率</span><span class="detail-v ${classForNumber(fundingRate)}">${fmtPctRatio(fundingRate, 5)}</span></div>
        <div class="detail-item"><span class="detail-k">下一事件预测费率</span><span class="detail-v ${classForNumber(futureFundingRate)}">${fmtPctRatio(futureFundingRate, 5)}</span></div>
        <div class="detail-item"><span class="detail-k">小时费率</span><span class="detail-v ${classForNumber(hourlyRate)}">${fmtPctRatio(hourlyRate, 5)}</span></div>
        <div class="detail-item"><span class="detail-k">当前 next funding</span><span class="detail-v">${fmtTime(fundingTimeMs)}</span></div>
        <div class="detail-item"><span class="detail-k">结算周期</span><span class="detail-v">${fundingIntervalText(fundingIntervalHours)}</span></div>
        <div class="detail-item"><span class="detail-k">盘口买一 / 卖一</span><span class="detail-v">${priceText(bidPrice)} / ${priceText(askPrice)}</span></div>
        <div class="detail-item"><span class="detail-k">盘口中间价</span><span class="detail-v">${priceText(mid)}</span></div>
        <div class="detail-item"><span class="detail-k">Mark</span><span class="detail-v">${priceText(markPrice)}</span></div>
      </div>
	    </div>
  `;
}

function spotLegCard(leg, anchorFundingTimeMs) {
  const mid = midpoint(leg?.bidPrice, leg?.askPrice);
  return `
    <div class="detail-section">
      <div class="detail-section-head">
        <div class="leg-title">${leg?.title || "现货腿"}</div>
        <span class="pill good">${leg?.exchange || "--"}</span>
      </div>
      <div class="detail-list compact-list">
        <div class="detail-item"><span class="detail-k">交易对</span><span class="detail-v">${leg?.venueSymbol || "--"}</span></div>
        <div class="detail-item"><span class="detail-k">市场类型</span><span class="detail-v">现货多头</span></div>
        <div class="detail-item"><span class="detail-k">资金费规则</span><span class="detail-v">现货腿按 0 funding 处理</span></div>
        <div class="detail-item"><span class="detail-k">收益锚点</span><span class="detail-v">跟随永续腿结算时间 ${fmtTime(anchorFundingTimeMs)}</span></div>
        <div class="detail-item"><span class="detail-k">盘口买一 / 卖一</span><span class="detail-v">${priceText(leg?.bidPrice)} / ${priceText(leg?.askPrice)}</span></div>
        <div class="detail-item"><span class="detail-k">盘口中间价</span><span class="detail-v">${priceText(mid)}</span></div>
        <div class="detail-item"><span class="detail-k">参考价格</span><span class="detail-v">${priceText(leg?.markPrice || mid)}</span></div>
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
        <div class="detail-item"><span class="detail-k">当前 next funding</span><span class="detail-v">${fmtTime(rule.next_funding_time_ms)}</span></div>
        <div class="detail-item"><span class="detail-k">当前 next funding费率</span><span class="detail-v ${classForNumber(rule.current_funding_rate)}">${fmtPctRatio(rule.current_funding_rate, 5)}</span></div>
        <div class="detail-item"><span class="detail-k">Clamp 来源</span><span class="detail-v">${rule.clamp_source || "--"}</span></div>
        <div class="detail-item"><span class="detail-k">有效区间</span><span class="detail-v">${fmtPctRatio(rule.effective_floor_rate, 5)} ~ ${fmtPctRatio(rule.effective_cap_rate, 5)}</span></div>
        <div class="detail-item"><span class="detail-k">Regime / 置信度</span><span class="detail-v">${rule.forecast_regime || "--"} / ${rule.forecast_confidence || "--"}</span></div>
      </div>
      <div class="formula-box">${rule.metadata_summary || "--"}</div>
    </div>
  `;
}

function spotFundingRuleSummaryCard(title, leg, anchorFundingTimeMs) {
  return `
    <div class="detail-section">
      <div class="detail-section-head">
        <div class="detail-section-title">${title}</div>
        <span class="pill warn">现货锚定腿</span>
      </div>
      <div class="detail-list compact-list">
        <div class="detail-item"><span class="detail-k">交易所 / 合约</span><span class="detail-v">${leg?.exchange || "--"} / ${leg?.venueSymbol || "--"}</span></div>
        <div class="detail-item"><span class="detail-k">Clamp 来源</span><span class="detail-v">spot_synthetic_zero</span></div>
        <div class="detail-item"><span class="detail-k">当前 next funding费率</span><span class="detail-v">0.00000%</span></div>
        <div class="detail-item"><span class="detail-k">收益锚点</span><span class="detail-v">跟随永续腿结算时间 ${fmtTime(anchorFundingTimeMs)}</span></div>
        <div class="detail-item"><span class="detail-k">解释</span><span class="detail-v">现货腿不直接产生 funding，主要承担价格对冲与占资。</span></div>
      </div>
      <div class="formula-box">同所现货对冲模式下，现货腿按 synthetic zero funding 展示，用来表达“现货腿不收也不付资金费”，收益主来源来自永续空头腿的 funding。</div>
    </div>
  `;
}

function fundingHistoryRatioText(ratio, sampleCount) {
  const samples = Number(sampleCount || 0);
  if (samples <= 0) return "--";
  return `${fmtPctRatio(ratio, 1)} / ${samples}`;
}

function fundingHistorySeries(rule, limit = 8) {
  const rows = Array.isArray(rule?.history_series) ? rule.history_series.slice() : [];
  rows.sort(
    (left, right) =>
      Number(right?.funding_time_ms || 0) - Number(left?.funding_time_ms || 0),
  );
  return rows.slice(0, limit);
}

function fundingHistoryDetailCard(title, rule, loading, emptyText) {
  if (!rule || typeof rule !== "object") {
    return `
      <div class="detail-section">
        <div class="detail-section-title">${title}</div>
        <div class="empty-state show compact-empty">${loading ? "正在加载历史 funding 明细..." : emptyText}</div>
      </div>
    `;
  }

  if (isSpotFundingRule(rule)) {
    return spotFundingHistoryDetailCard(title, loading, rule.next_funding_time_ms);
  }

  const rows = fundingHistorySeries(rule);
  const sampleCount = Math.max(Number(rule.history_sample_count || 0), rows.length);
  const positiveText = fundingHistoryRatioText(
    rule.history_positive_ratio,
    sampleCount,
  );
  const negativeText = fundingHistoryRatioText(
    rule.history_negative_ratio,
    sampleCount,
  );
  const headPill = rows.length
    ? `${rows.length} 条明细`
    : sampleCount > 0
      ? `${sampleCount} 个样本`
      : "历史序列";
  const emptyMessage = loading
    ? "正在加载历史 funding 明细..."
    : sampleCount > 0
      ? "历史 funding 画像已就绪，但最近明细序列还没同步完成。"
      : emptyText;

  return `
    <div class="detail-section">
      <div class="detail-section-head">
        <div class="detail-section-title">${title}</div>
        <span class="pill ${rows.length ? "good" : "warn"}">${headPill}</span>
      </div>
      <div class="detail-list compact-list">
        <div class="detail-item"><span class="detail-k">交易所 / 合约</span><span class="detail-v">${rule.exchange || "--"} / ${rule.venue_symbol || "--"}</span></div>
        <div class="detail-item"><span class="detail-k">历史样本</span><span class="detail-v">${sampleCount > 0 ? `${sampleCount} 个周期` : "--"}</span></div>
        <div class="detail-item"><span class="detail-k">历史正费率</span><span class="detail-v">${positiveText}</span></div>
        <div class="detail-item"><span class="detail-k">历史负费率</span><span class="detail-v">${negativeText}</span></div>
      </div>
      ${
        rows.length
          ? `
        <div class="projection-table-wrap">
          <table class="projection-table">
            <thead>
              <tr>
                <th>Funding 时间</th>
                <th>费率</th>
                <th>标记价</th>
              </tr>
            </thead>
            <tbody>
              ${rows
                .map(
                  (row) => `
                    <tr>
                      <td>${fmtTime(row.funding_time_ms)}</td>
                      <td class="${classForNumber(row.funding_rate)}">${fmtPctRatio(row.funding_rate, 5)}</td>
                      <td>${priceText(row.mark_price)}</td>
                    </tr>`,
                )
                .join("")}
            </tbody>
          </table>
        </div>`
          : `<div class="empty-state show compact-empty">${emptyMessage}</div>`
      }
    </div>
  `;
}

function spotFundingHistoryDetailCard(title, loading, anchorFundingTimeMs) {
  return `
    <div class="detail-section">
      <div class="detail-section-head">
        <div class="detail-section-title">${title}</div>
        <span class="pill warn">现货锚定腿</span>
      </div>
      <div class="detail-list compact-list">
        <div class="detail-item"><span class="detail-k">历史序列</span><span class="detail-v">现货腿不直接产生 funding 事件</span></div>
        <div class="detail-item"><span class="detail-k">展示语义</span><span class="detail-v">synthetic zero funding</span></div>
        <div class="detail-item"><span class="detail-k">收益锚点</span><span class="detail-v">${fmtTime(anchorFundingTimeMs)}</span></div>
        <div class="detail-item"><span class="detail-k">说明</span><span class="detail-v">${loading ? "正在刷新永续腿历史 funding..." : "现货腿主要承担价格对冲和占资，不直接提供 funding 历史序列。"}</span></div>
      </div>
      <div class="formula-box">同所模式下只对永续腿维护真实 funding 历史；现货腿保持 0 funding 锚定语义，用来说明收益兑现点跟随永续腿结算。</div>
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
            <th>Carry率</th>
            <th>时均</th>
            <th>资金收益</th>
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
                  <td class="${classForNumber(row.gross_funding_pnl)}">${fmtMoney(row.gross_funding_pnl)}</td>
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
  const summaryItem = currentSelectedOpportunity(items);
  if (!summaryItem) {
    els.opportunityDetail.innerHTML =
      '<div class="empty-state show">当前没有可查看的套利机会。</div>';
    return;
  }
  ensureOpportunityDetailLoaded(summaryItem);
  const detailKey = opportunityDetailCacheKey(summaryItem);
  const detailItem = opportunityDetailFor(summaryItem);
  const item = detailItem ? { ...summaryItem, ...detailItem } : summaryItem;
  const detailLoading = Boolean(detailKey && state.opportunityDetailLoading[detailKey]);

  const selectedProjection = bestProjection(item);
  const currentSegment = currentCarrySegment(item);
  const fundingDelta = Number.isFinite(Number(selectedProjection?.carry_rate))
    ? Number(selectedProjection.carry_rate)
    : fundingSpread(item);
  const displayedCarry = Number.isFinite(Number(currentSegment?.carryRate))
    ? Number(currentSegment.carryRate)
    : fundingDelta;
  const hourlyDelta = Number.isFinite(
    Number(selectedProjection?.carry_rate_hourly_equivalent),
  )
    ? Number(selectedProjection.carry_rate_hourly_equivalent)
    : fundingSpreadHourly(item);
  const displayedHourly = Number.isFinite(Number(currentSegment?.carryRate))
    ? fundingSpreadHourly(item)
    : hourlyDelta;
  const grossFundingPNL = Number.isFinite(Number(selectedProjection?.gross_funding_pnl))
    ? Number(selectedProjection.gross_funding_pnl)
    : Number(item.gross_funding_pnl || 0);
  const selectedLongEvents = Number.isFinite(Number(currentSegment?.longEvents))
    ? Number(currentSegment.longEvents)
    : Number.isFinite(Number(selectedProjection?.long_funding_event_count))
      ? Number(selectedProjection.long_funding_event_count)
      : Number(item.long_funding_event_count || 0);
  const selectedShortEvents = Number.isFinite(Number(currentSegment?.shortEvents))
    ? Number(currentSegment.shortEvents)
    : Number.isFinite(Number(selectedProjection?.short_funding_event_count))
      ? Number(selectedProjection.short_funding_event_count)
      : Number(item.short_funding_event_count || 0);
  const selectedProjectedFundingTimeMs = Number.isFinite(
    Number(selectedProjection?.projected_funding_time_ms),
  )
    ? Number(selectedProjection.projected_funding_time_ms)
    : Number(item.projected_funding_time_ms || item.latest_funding_time_ms || 0);
  const selectedFundingWindowHours = Number.isFinite(
    Number(selectedProjection?.funding_window_hours),
  )
    ? Number(selectedProjection.funding_window_hours)
    : Number(item.funding_window_hours || 0);
  const nextLongMs = Number(item.long_funding_time_ms || 0) - Date.now();
  const nextShortMs = Number(item.short_funding_time_ms || 0) - Date.now();
  const matchedPlan = bestPlanForOpportunity(item);
  const displayPlan = matchedPlan || syntheticPlanForOpportunity(item);
  const planPreviewLabel = matchedPlan ? "真实执行计划" : "估算执行计划预览";
  const planStatus = opportunityPlanStatus(item);
  const sameExchange = isSameExchangeOpportunity(item);
  const sameExchangeContext = sameExchange ? sameExchangeLegs(item) : null;
  const sameExchangeRisk = sameExchange ? sameExchangeRiskSnapshot(item, matchedPlan) : null;
  const opportunityMetrics = sameExchange
    ? [
        detailMetric("预计净收益", fmtMoney(item.net_expected_pnl), classForNumber(item.net_expected_pnl)),
        detailMetric("净收益率", fmtSignedBps(item.net_expected_bps, 2), classForNumber(item.net_expected_bps)),
        detailMetric(carryMetricLabel(item), fmtPctRatio(displayedCarry, 5), classForNumber(displayedCarry)),
        detailMetric("1h 价格冲击", fmtPctRatio(sameExchangeRisk?.priceShockRatio, 2), classForNumber(sameExchangeRisk?.priceShockRatio)),
        detailMetric("价格风控", sameExchangeRisk?.priceShockAllowed ? "安全" : sameExchangePriceRiskReasonText(sameExchangeRisk?.priceShockReason), sameExchangeRisk?.priceShockAllowed ? "positive" : "negative"),
        detailMetric("爆仓距离", Number.isFinite(sameExchangeRisk?.liquidationDistanceRatio) ? fmtPctRatio(sameExchangeRisk?.liquidationDistanceRatio, 2) : "--", sameExchangeLiquidationClass(sameExchangeRisk?.liveRisk, sameExchangeRisk?.liquidationDistanceRatio)),
        detailMetric("保护单", sameExchangeProtectiveStatusText(sameExchangeRisk?.liveRisk), sameExchangeProtectiveStatusClass(sameExchangeRisk?.liveRisk)),
        detailMetric(basisMetricLabel(item), fmtSignedBps(item.basis_bps, 2)),
        detailMetric("基差模型", sameExchangeBasisModeText(item)),
        detailMetric("基差阈值", sameExchangeBasisThresholdText(item)),
        detailMetric("基差成本", sameExchangeBasisCostText(item)),
        detailMetric("每轮 funding 毛收益", sameExchangeBasisCarryPerEventText(item)),
        detailMetric("基差回本", sameExchangeBasisPaybackText(item)),
        detailMetric("基差动作", sameExchangeBasisActionText(item), sameExchangeBasisActionClass(item)),
        detailMetric(grossFundingMetricLabel(item), fmtMoney(grossFundingPNL), classForNumber(grossFundingPNL)),
        detailMetric("历史预估单轮", sameExchangeSuggestedEventRateText(item)),
        detailMetric("建议持有", sameExchangeSuggestedHoldEventsText(item)),
        detailMetric("建议时长", sameExchangeSuggestedHoldDurationText(item)),
        detailMetric("历史预估毛收益", sameExchangeSuggestedGrossPNLText(item)),
        detailMetric("历史预估净收益", sameExchangeSuggestedNetPNLText(item), classForNumber(Number((sameExchangePerpRule(item)?.suggested_net_pnl) ?? sameExchangeDisplayPlan(item)?.same_exchange_long_hold_suggested_net_pnl))),
        detailMetric("长持原因", sameExchangeLongHoldReasonText(item)),
        detailMetric("收益口径", sameExchangeLongHoldSourceText(item)),
        detailMetric("现货占资 / 对冲名义", targetNotionalText(item, displayPlan)),
        detailMetric("对冲偏差", displayPlan ? (planPositionSkewBps(displayPlan) == null ? "--" : fmtSignedBps(planPositionSkewBps(displayPlan), 2)) : "--"),
        detailMetric("永续目标杠杆", displayPlan ? `${fmtNumber(planTargetLeverage(displayPlan), 2)}x` : "--"),
        detailMetric("计划状态", planStatus.text, planStatus.cls === "good" ? "positive" : planStatus.cls === "bad" ? "negative" : "muted-text"),
        detailMetric("下次永续 funding", fmtTime(sameExchangeContext?.perp?.fundingTimeMs)),
        detailMetric("下次 Review", fmtTime(item.next_review_time_ms)),
        detailMetric("当前收益兑现点", fmtTime(selectedProjectedFundingTimeMs)),
        detailMetric("预计持有到", opportunityExpectedCloseText(item)),
        detailMetric("预计总持有", opportunityExpectedHoldText(item)),
        detailMetric("最晚入场时间", fmtTime(item.required_entry_by_funding_time_ms || item.earliest_funding_time_ms)),
        detailMetric("Funding 事件窗口", `${fmtNumber(selectedFundingWindowHours, 2)} h`),
        detailMetric("产生时间", fmtTime(opportunityProducedTimeMs(item))),
      ]
    : [
        detailMetric("净收益", fmtMoney(item.net_expected_pnl), classForNumber(item.net_expected_pnl)),
        detailMetric("净收益率", fmtSignedBps(item.net_expected_bps, 2), classForNumber(item.net_expected_bps)),
        detailMetric("当前 Carry率", fmtPctRatio(displayedCarry, 5), classForNumber(displayedCarry)),
        detailMetric("当前时均边际", fmtPctRatio(displayedHourly, 5), classForNumber(displayedHourly)),
        detailMetric("Basis", fmtSignedBps(item.basis_bps, 2)),
        detailMetric("动态价差阈值", fmtSignedBps(maxAllowedBasisForItem(item), 2)),
        detailMetric("当前 Entry Path 资金收益", fmtMoney(grossFundingPNL), classForNumber(grossFundingPNL)),
        detailMetric("计划状态", planStatus.text, planStatus.cls === "good" ? "positive" : planStatus.cls === "bad" ? "negative" : "muted-text"),
        detailMetric("预计投入资金", displayPlan ? fmtMoney(planCapitalAllocated(displayPlan), 2) : "--"),
        detailMetric("目标杠杆", displayPlan ? `${fmtNumber(planTargetLeverage(displayPlan), 2)}x` : "--"),
        detailMetric("Long / Short 杠杆", displayPlan ? `${fmtNumber(planLongLeverage(displayPlan), 2)}x / ${fmtNumber(planShortLeverage(displayPlan), 2)}x` : "--"),
        detailMetric("仓位指标", displayPlan ? (planPositionSkewBps(displayPlan) == null ? "--" : fmtSignedBps(planPositionSkewBps(displayPlan), 2)) : "--"),
        detailMetric("目标仓位(名义)", targetNotionalText(item, displayPlan)),
        detailMetric("套利模式", arbitrageModeText(opportunityArbitrageMode(item))),
        detailMetric("策略模式", strategyModeText(item.strategy_mode)),
        detailMetric("持有模式", holdSelectionModeText(state.system?.strategy?.hold_selection_mode)),
        detailMetric("持有规则", holdLimitText()),
        detailMetric("结算后缓冲", closeGraceText()),
        detailMetric("下次 Review", fmtTime(item.next_review_time_ms)),
        detailMetric("当前共享结算边界", fmtTime(item.sync_boundary_time_ms)),
        detailMetric("当前 Entry Path 段数", `${Number(item.entry_path_segment_count || 0)} 段`),
        detailMetric("Entry Path 截止原因", item.entry_path_stop_reason || "--"),
        detailMetric("Long 结算倒计时", fmtDuration(nextLongMs)),
        detailMetric("Short 结算倒计时", fmtDuration(nextShortMs)),
        detailMetric("当前 Entry Path 终点", fmtTime(selectedProjectedFundingTimeMs)),
        detailMetric("预计持有到", opportunityExpectedCloseText(item)),
        detailMetric("最晚入场时间", fmtTime(item.required_entry_by_funding_time_ms || item.earliest_funding_time_ms)),
        detailMetric("Funding 事件窗口", `${fmtNumber(selectedFundingWindowHours, 2)} h`),
        detailMetric("结算前持有", holdingDurationText(item)),
        detailMetric("预计总持有", opportunityExpectedHoldText(item)),
        detailMetric("产生时间", fmtTime(opportunityProducedTimeMs(item))),
        detailMetric("当前事件数", `Long ${selectedLongEvents} 次 / Short ${selectedShortEvents} 次`),
        detailMetric("估算模式", fundingEstimateModeText(item)),
        detailMetric("估算置信度", fundingEstimateConfidenceText(item)),
        detailMetric("平滑回看窗口", fundingSmoothingLookbackText()),
        detailMetric("当前值权重", fundingSmoothingWeightText()),
      ];
  const opportunityLegSection = sameExchange
    ? `
      <div class="detail-grid-2">
        ${spotLegCard(sameExchangeContext?.spot, sameExchangeContext?.perp?.fundingTimeMs)}
        ${legCard(
          sameExchangeContext?.perp?.title,
          sameExchangeContext?.perp?.exchange,
          sameExchangeContext?.perp?.venueSymbol,
          sameExchangeContext?.perp?.fundingRate,
          sameExchangeContext?.perp?.futureFundingRate,
          sameExchangeContext?.perp?.hourlyRate,
          sameExchangeContext?.perp?.fundingTimeMs,
          sameExchangeContext?.perp?.fundingIntervalHours,
          sameExchangeContext?.perp?.bidPrice,
          sameExchangeContext?.perp?.askPrice,
          sameExchangeContext?.perp?.markPrice,
        )}
      </div>
    `
    : `
      <div class="detail-grid-2">
        ${legCard(longLegTitle(item), item.long_exchange, item.long_venue_symbol, item.long_funding_rate, item.long_future_funding_rate, item.long_funding_hourly, item.long_funding_time_ms, item.long_funding_interval_hours, item.long_bid_price, item.long_ask_price, item.long_mark_price)}
        ${legCard(shortLegTitle(item), item.short_exchange, item.short_venue_symbol, item.short_funding_rate, item.short_future_funding_rate, item.short_funding_hourly, item.short_funding_time_ms, item.short_funding_interval_hours, item.short_bid_price, item.short_ask_price, item.short_mark_price)}
      </div>
    `;
  const fundingRuleSection = sameExchange
    ? `
      <div class="detail-grid-2">
        ${spotFundingRuleSummaryCard(`${sameExchangeContext?.spot?.title || "现货腿"} funding rule`, sameExchangeContext?.spot, sameExchangeContext?.perp?.fundingTimeMs)}
        ${fundingRuleSummaryCard(`${sameExchangeContext?.perp?.title || "永续腿"} funding rule`, sameExchangeContext?.perp?.fundingRule, sameExchangeContext?.perp?.key === "long" ? selectedLongEvents : selectedShortEvents)}
      </div>
    `
    : `
      <div class="detail-grid-2">
        ${fundingRuleSummaryCard(`${longLegTitle(item)} funding rule`, item.long_funding_rule, selectedLongEvents)}
        ${fundingRuleSummaryCard(`${shortLegTitle(item)} funding rule`, item.short_funding_rule, selectedShortEvents)}
      </div>
    `;
  const fundingHistorySection = sameExchange
    ? `
      <div class="detail-grid-2">
        ${spotFundingHistoryDetailCard(`${sameExchangeContext?.spot?.title || "现货腿"} 历史 funding`, detailLoading, sameExchangeContext?.perp?.fundingTimeMs)}
        ${fundingHistoryDetailCard(`${sameExchangeContext?.perp?.title || "永续腿"} 历史 funding`, sameExchangeContext?.perp?.fundingRule, detailLoading, "暂无永续历史 funding 明细。")}
      </div>
    `
    : `
      <div class="detail-grid-2">
        ${fundingHistoryDetailCard(`${longLegTitle(item)} 历史 funding`, item.long_funding_rule, detailLoading, "暂无多头腿历史 funding 明细。")}
        ${fundingHistoryDetailCard(`${shortLegTitle(item)} 历史 funding`, item.short_funding_rule, detailLoading, "暂无空头腿历史 funding 明细。")}
      </div>
    `;
  const pnlSectionTitle = sameExchange ? "现货对冲收益拆解" : "收益构成";
  const fundingPnLLabel = sameExchange ? "永续 funding 收益" : "资金收益";
  const pnlFormulaText = sameExchange
    ? "净收益 = 永续 funding 收益 - 现货/永续开平手续费 - 滑点 - 安全缓冲。现货腿不直接产生 funding，主要承担价格对冲与占资。"
    : "净收益 = 资金收益 - 入场手续费 - 出场手续费 - 滑点 - 安全缓冲。说明：这里不是只看“当前这一期” funding，而是按当前策略选中的持有窗口估算；若只覆盖当前这一轮结算，则直接使用当前 funding 快照；若会跨到后续多轮结算，则对后续事件结合近期历史均值做平滑估算。";
  const decisionSectionTitle = sameExchange ? "现货对冲判断" : "机会判断";
  const basisDecisionLabel = sameExchange ? "现货-永续基差判断" : "跨所价差判定";
  const basisThresholdLabel = sameExchange
    ? sameExchangeBasisUsesPayback(item)
      ? "基差回本上限"
      : "短持基差阈值"
    : "跨所价差阈值";
  const insightTitle = sameExchange ? "怎么看这组现货对冲" : "怎么看这组机会";
  const insightText = sameExchange
    ? "同所模式固定为“现货多头 + 永续空头”。重点不是比较两边 funding 差，而是看永续空头腿的 funding 能否覆盖现货占资、开平手续费、滑点和基差风险。短持窗口下仍按基差硬阈值控制；长持窗口则改成看“基差+摩擦成本需要几轮 funding 才能回本”。当更优机会出现时，系统会比较换仓净增益，只有覆盖当前平仓成本后仍明显更优才会切换。"
    : "方向不是固定死的。系统会在每次刷新时，把“Long A / Short B”和“Long B / Short A”两个方向都完整计算一遍，再选当前更优的方向展示。Funding 收益也不再按统一小时平均外推，而是按当前已知的真实 funding 结算事件逐腿估算。状态里“跨所价差过大”表示当前 Basis（shortBid 与 longAsk 的相对偏离）超过动态阈值（基础阈值 `max_spread_bps` 按持有时长可放宽），为避免入场成本吞噬 funding 收益会被拦截。若后续 funding 或 basis 变化导致反方向更优，下一轮机会就会切换成反方向。";
  state.selectedOpportunityKey = opportunityKey(summaryItem);
  state.selectedPlanKey = matchedPlan?.plan_key || null;
  const activeDetailTab = String(state.opportunityDetailTab || "overview");
  const detailTabs = `
    <div class="detail-tab-row">
      ${detailTabButton("overview", "概览")}
      ${detailTabButton("execution", "下单诊断")}
    </div>
  `;
  const overviewContent = `
    <div class="detail-tab-pane">
      <div class="detail-metric-grid">
        ${opportunityMetrics.join("")}
      </div>

      ${opportunityLegSection}

      ${sameExchange ? sameExchangeRiskCard(item, matchedPlan) : ""}

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

      ${fundingRuleSection}

      ${fundingHistorySection}

      <div class="detail-section detail-section-tight">
        <div class="detail-section-head">
	          <div>
	            <div class="detail-section-title">候选持有窗口 / Projection 明细</div>
	            <div class="detail-subtitle">${strategyUsesDynamicHold() ? "当前模式会按收益自动搜索更优兑现点；这里展开的是本轮可见的全部候选路径。" : "系统会在允许持有窗口内评估多个兑现点，主卡片展示当前策略选中的窗口，这里则展开所有候选路径。"}</div>
          </div>
          <span class="pill good">${Array.isArray(item.projection_details) ? item.projection_details.length : 0} 个窗口</span>
        </div>
        ${renderProjectionDetails(item)}
      </div>

      <div class="detail-grid-2">
        <div class="detail-section opportunity-explain">
          <div class="detail-section-title">${pnlSectionTitle}</div>
          <div class="detail-list">
            <div class="detail-item"><span class="detail-k">${fundingPnLLabel}</span><span class="detail-v ${classForNumber(item.gross_funding_pnl)}">${fmtMoney(item.gross_funding_pnl)}</span></div>
            <div class="detail-item"><span class="detail-k">入场手续费</span><span class="detail-v negative">-${fmtNumber(Math.abs(Number(item.entry_fee_pnl || 0)), 3)} USDT（${feeBreakdownText(item, state.system?.strategy?.entry_mode)}）</span></div>
            <div class="detail-item"><span class="detail-k">出场手续费</span><span class="detail-v negative">-${fmtNumber(Math.abs(Number(item.exit_fee_pnl || 0)), 3)} USDT（${feeBreakdownText(item, state.system?.strategy?.exit_mode)}）</span></div>
            <div class="detail-item"><span class="detail-k">滑点预估</span><span class="detail-v negative">-${fmtNumber(Math.abs(Number(item.slippage_pnl || 0)), 3)} USDT</span></div>
            <div class="detail-item"><span class="detail-k">安全缓冲</span><span class="detail-v negative">-${fmtNumber(Math.abs(Number(item.safety_buffer_pnl || 0)), 3)} USDT</span></div>
            <div class="detail-item total-row"><span class="detail-k">净收益</span><span class="detail-v ${classForNumber(item.net_expected_pnl)}">${fmtMoney(item.net_expected_pnl)}</span></div>
          </div>
          <div class="formula-box">${pnlFormulaText}</div>
        </div>

        <div class="detail-section opportunity-explain">
          <div class="detail-section-title">${decisionSectionTitle}</div>
          <div class="detail-list">
            <div class="detail-item"><span class="detail-k">策略状态</span><span class="detail-v">${explainStatus(item.status)}</span></div>
            <div class="detail-item"><span class="detail-k">是否已进入执行计划</span><span class="detail-v ${planStatus.cls === "good" ? "positive" : planStatus.cls === "bad" ? "negative" : "muted-text"}">${planStatus.text}</span></div>
            <div class="detail-item"><span class="detail-k">计划说明</span><span class="detail-v">${planStatus.reason || "--"}</span></div>
            <div class="detail-item"><span class="detail-k">计划主键</span><span class="detail-v">${matchedPlan?.plan_key || "--"}</span></div>
            <div class="detail-item"><span class="detail-k">计划状态</span><span class="detail-v">${matchedPlan?.status || "--"}</span></div>
            <div class="detail-item"><span class="detail-k">套利模式</span><span class="detail-v">${arbitrageModeText(opportunityArbitrageMode(item))}</span></div>
            <div class="detail-item"><span class="detail-k">策略模式</span><span class="detail-v">${strategyModeText(item.strategy_mode)}</span></div>
            <div class="detail-item"><span class="detail-k">持有模式</span><span class="detail-v">${holdSelectionModeText(state.system?.strategy?.hold_selection_mode)}</span></div>
            <div class="detail-item"><span class="detail-k">持有上限</span><span class="detail-v">${holdLimitText()}</span></div>
            <div class="detail-item"><span class="detail-k">结算后缓冲</span><span class="detail-v">${closeGraceText()}</span></div>
            <div class="detail-item"><span class="detail-k">Funding 计算模式</span><span class="detail-v">${fundingModeText(item)}</span></div>
            <div class="detail-item"><span class="detail-k">当前机会批次</span><span class="detail-v">${state.currentOpportunityBatchId || item.batch_id || "--"}</span></div>
            <div class="detail-item"><span class="detail-k">机会产生时间</span><span class="detail-v">${fmtTime(opportunityProducedTimeMs(item))}</span></div>
            <div class="detail-item"><span class="detail-k">计划关联批次</span><span class="detail-v">${matchedPlan?.opportunity_batch_id || "--"}</span></div>
            <div class="detail-item"><span class="detail-k">${sameExchange ? "现货腿名义" : "Long 仓位名义"}</span><span class="detail-v">${matchedPlan ? fmtMoney(planLongNotional(matchedPlan), 2) : "--"}</span></div>
            <div class="detail-item"><span class="detail-k">${sameExchange ? "永续腿名义" : "Short 仓位名义"}</span><span class="detail-v">${matchedPlan ? fmtMoney(planShortNotional(matchedPlan), 2) : "--"}</span></div>
            <div class="detail-item"><span class="detail-k">机会拒绝原因</span><span class="detail-v">${explainOpportunityRejectReason(item) || "--"}</span></div>
            <div class="detail-item"><span class="detail-k">${basisDecisionLabel}</span><span class="detail-v">${basisDecisionText(item)}</span></div>
            <div class="detail-item"><span class="detail-k">${basisThresholdLabel}</span><span class="detail-v">${sameExchange ? sameExchangeBasisThresholdText(item) : fmtSignedBps(maxAllowedBasisForItem(item), 2)}</span></div>
            ${
              sameExchange
                ? `<div class="detail-item"><span class="detail-k">基差成本（含摩擦）</span><span class="detail-v">${sameExchangeBasisCostText(item)}</span></div>
                   <div class="detail-item"><span class="detail-k">每轮 funding 毛收益</span><span class="detail-v">${sameExchangeBasisCarryPerEventText(item)}</span></div>
                   <div class="detail-item"><span class="detail-k">基差回本轮数</span><span class="detail-v">${sameExchangeBasisPaybackText(item)}</span></div>
                   <div class="detail-item"><span class="detail-k">基差处理动作</span><span class="detail-v ${sameExchangeBasisActionClass(item)}">${sameExchangeBasisActionText(item)}</span></div>
                   <div class="detail-item"><span class="detail-k">基差判定原因</span><span class="detail-v">${sameExchangeBasisReasonText(item)}</span></div>`
                : ""
            }
            <div class="detail-item"><span class="detail-k">是否可执行</span><span class="detail-v">${item.eligible_for_execution ? "是" : "否"}</span></div>
            <div class="detail-item"><span class="detail-k">${sameExchange ? "现货锚定时间" : "最早结算时间"}</span><span class="detail-v">${fmtTime(item.earliest_funding_time_ms)}</span></div>
            <div class="detail-item"><span class="detail-k">${sameExchange ? "永续结算时间" : "最晚结算时间"}</span><span class="detail-v">${fmtTime(item.latest_funding_time_ms)}</span></div>
            <div class="detail-item"><span class="detail-k">${sameExchange ? "当前收益兑现点" : "当前 Entry Path 终点"}</span><span class="detail-v">${fmtTime(item.projected_funding_time_ms || item.latest_funding_time_ms)}</span></div>
            <div class="detail-item"><span class="detail-k">预计持有到</span><span class="detail-v">${opportunityExpectedCloseText(item)}</span></div>
            <div class="detail-item"><span class="detail-k">${sameExchange ? "Funding 前持有" : "结算前持有"}</span><span class="detail-v">${holdingDurationText(item)}</span></div>
            <div class="detail-item"><span class="detail-k">预计总持有</span><span class="detail-v">${opportunityExpectedHoldText(item)}</span></div>
            <div class="detail-item"><span class="detail-k">最晚入场时间</span><span class="detail-v">${fmtTime(item.required_entry_by_funding_time_ms || item.earliest_funding_time_ms)}</span></div>
            <div class="detail-item"><span class="detail-k">${sameExchange ? "现货锚定 / 永续事件" : "Funding 事件次数"}</span><span class="detail-v">${sameExchange ? `现货 ${sameExchangeContext?.spot?.key === "long" ? selectedLongEvents : selectedShortEvents} 次锚定 / 永续 ${sameExchangeContext?.perp?.key === "long" ? selectedLongEvents : selectedShortEvents} 次 funding` : fundingEventsText(item)}</span></div>
            <div class="detail-item"><span class="detail-k">Funding 事件窗口</span><span class="detail-v">${fmtNumber(item.funding_window_hours || 0, 2)} h</span></div>
          </div>
          <div class="insight-box">
            <div class="insight-title">${insightTitle}</div>
            <div class="insight-text">${insightText}</div>
          </div>
        </div>
      </div>
    </div>
  `;

  els.opportunityDetail.innerHTML = `
    <div class="detail-shell">
      <div class="detail-hero">
        <div>
          <div class="detail-eyebrow">机会详情</div>
          <div class="detail-title">${opportunityDisplayTitle(item)}</div>
          <div class="detail-subtitle">${opportunityDisplaySubtitle(item)} · ${item.long_venue_symbol} ↔ ${item.short_venue_symbol}</div>
        </div>
        <div class="detail-hero-right">
          <span class="pill ${statusClass(item.status)}">${explainStatus(item.status)}</span>
          <div class="detail-score">评分 ${fmtNumber(item.score, 2)}</div>
        </div>
      </div>

      ${detailTabs}

      ${
        activeDetailTab === "execution"
          ? renderOpportunityDiagnosticsTab(
              item,
              matchedPlan,
              displayPlan,
              sameExchange,
              sameExchangeRisk,
              planStatus,
            )
          : overviewContent
      }
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
  const [system, opportunities, executions, autoClose, stats] = await Promise.all([
    apiGet("/api/v1/system/status", {}),
    apiGet(`/api/v1/opportunities?limit=${OPPORTUNITY_FETCH_LIMIT}`, []),
    apiGet("/api/v1/executions?limit=50", []),
    apiGet("/api/v1/executions/auto-close-candidates", {}),
    apiGet("/api/v1/snapshot-stats", {}),
  ]);

  const normalizedOpportunities = Array.isArray(opportunities)
    ? opportunities
    : [];
  const currentBatchId = normalizedOpportunities.length
    ? String(normalizedOpportunities[0].batch_id || "")
    : "";

  // 只请求“当前机会批次”对应的 plans，避免 plans 和 opportunities 不是同一批。
  const [plans, allPlans] = await Promise.all([
    currentBatchId
      ? apiGet(
          `/api/v1/plans?limit=100&opportunity_batch_id=${encodeURIComponent(currentBatchId)}`,
          [],
        )
      : Promise.resolve([]),
    apiGet("/api/v1/plans?limit=100", []),
  ]);

  state.system = system || {};
  state.opportunities = normalizedOpportunities;
  pruneOpportunityDetailCache(normalizedOpportunities);
  state.currentOpportunityBatchId = currentBatchId;
  state.batchPlans = Array.isArray(plans) ? plans : [];
  state.allPlans = Array.isArray(allPlans) ? allPlans : [];
  state.plans = state.allPlans;
  state.executions = Array.isArray(executions) ? executions : [];
  state.autoClose = autoClose || {};
  state.stats = stats || {};

  syncSelectors();
  syncOpportunitySortControl();
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
  renderAutoCloseCandidates();
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
    return;
  }

  const detailTabBtn = event.target.closest("button[data-detail-tab]");
  if (detailTabBtn) {
    state.opportunityDetailTab = detailTabBtn.dataset.detailTab || "overview";
    renderOpportunityDetail(state.opportunities);
    requestAnimationFrame(syncOpportunityHeights);
  }
}

function bindEvents() {
  els.manualRefreshBtn.addEventListener("click", refreshAll);
  els.autoCloseSweepBtn?.addEventListener("click", async () => {
    els.autoCloseSweepBtn.disabled = true;
    try {
      const report = await apiPost("/api/v1/executions/auto-close-sweep");
      await refreshAll();
      alert(
        `auto-close sweep 完成: attempted=${report?.attempted || 0}, closed=${report?.closed || 0}, failed=${report?.failed || 0}, skipped=${report?.skipped || 0}`,
      );
    } catch (err) {
      alert(err.message || String(err));
    } finally {
      els.autoCloseSweepBtn.disabled = false;
    }
  });

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
    els.sortMode.dataset.userSelected = "1";
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
