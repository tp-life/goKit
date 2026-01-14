<template>
  <div class="bg-slate-800 rounded-lg shadow-lg overflow-hidden">
    <div class="p-4 border-b border-slate-700">
      <h2 class="text-xl font-semibold text-white">交易所价格对比</h2>
    </div>

    <div v-if="loading" class="p-8 text-center text-slate-400">
      加载中...
    </div>

    <div v-else-if="Object.keys(comparisons).length === 0" class="p-8 text-center text-slate-400">
      暂无数据
    </div>

    <div v-else class="overflow-x-auto">
      <table class="w-full">
        <thead class="bg-slate-700">
          <tr>
            <th class="px-4 py-3 text-left text-xs font-medium text-slate-300 uppercase">币种</th>
            <th class="px-4 py-3 text-right text-xs font-medium text-slate-300 uppercase">Binance价格</th>
            <th class="px-4 py-3 text-right text-xs font-medium text-slate-300 uppercase">Binance费率</th>
            <th class="px-4 py-3 text-right text-xs font-medium text-slate-300 uppercase">Lighter价格</th>
            <th class="px-4 py-3 text-right text-xs font-medium text-slate-300 uppercase">Lighter费率</th>
            <th class="px-4 py-3 text-right text-xs font-medium text-slate-300 uppercase">Hyperliquid价格</th>
            <th class="px-4 py-3 text-right text-xs font-medium text-slate-300 uppercase">Hyperliquid费率</th>
            <th class="px-4 py-3 text-right text-xs font-medium text-slate-300 uppercase">价差</th>
            <th class="px-4 py-3 text-right text-xs font-medium text-slate-300 uppercase">费率差</th>
            <th class="px-4 py-3 text-center text-xs font-medium text-slate-300 uppercase">套利</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-slate-700">
          <tr
            v-for="(comparison, symbol) in comparisons"
            :key="symbol"
            class="hover:bg-slate-700/50 transition-colors"
          >
            <td class="px-4 py-3 text-sm font-medium text-white">{{ symbol }}</td>
            <!-- Binance 价格 -->
            <td class="px-4 py-3 text-sm text-slate-300 text-right">
              <div v-if="comparison.prices?.binance?.markPrice !== null && comparison.prices?.binance?.markPrice !== undefined">
                {{ formatPrice(comparison.prices.binance.markPrice) }}
              </div>
              <div v-else-if="comparison.prices?.binance?.spotPrice !== null && comparison.prices?.binance?.spotPrice !== undefined">
                {{ formatPrice(comparison.prices.binance.spotPrice) }}
              </div>
              <span v-else class="text-slate-500">N/A</span>
            </td>
            <!-- Binance 费率 -->
            <td class="px-4 py-3 text-sm text-right">
              <div v-if="comparison.rates?.binance?.rate8H !== null && comparison.rates?.binance?.rate8H !== undefined">
                <span :class="getRateClass(comparison.rates.binance.rate8H)">
                  {{ formatRate(comparison.rates.binance.rate8H) }}
                </span>
              </div>
              <span v-else class="text-slate-500">N/A</span>
            </td>
            <!-- Lighter 价格 -->
            <td class="px-4 py-3 text-sm text-slate-300 text-right">
              <div v-if="comparison.prices?.lighter">
                {{ formatPrice(comparison.prices.lighter.markPrice) }}
              </div>
              <span v-else class="text-slate-500">N/A</span>
            </td>
            <!-- Lighter 费率 -->
            <td class="px-4 py-3 text-sm text-right">
              <div v-if="comparison.rates?.lighter?.rate8H !== null && comparison.rates?.lighter?.rate8H !== undefined">
                <span :class="getRateClass(comparison.rates.lighter.rate8H)">
                  {{ formatRate(comparison.rates.lighter.rate8H) }}
                </span>
              </div>
              <span v-else class="text-slate-500">N/A</span>
            </td>
            <!-- Hyperliquid 价格 -->
            <td class="px-4 py-3 text-sm text-slate-300 text-right">
              <div v-if="comparison.prices?.hyperliquid">
                {{ formatPrice(comparison.prices.hyperliquid.markPrice) }}
              </div>
              <span v-else class="text-slate-500">N/A</span>
            </td>
            <!-- Hyperliquid 费率 -->
            <td class="px-4 py-3 text-sm text-right">
              <div v-if="comparison.rates?.hyperliquid?.rate8H !== null && comparison.rates?.hyperliquid?.rate8H !== undefined">
                <span :class="getRateClass(comparison.rates.hyperliquid.rate8H)">
                  {{ formatRate(comparison.rates.hyperliquid.rate8H) }}
                </span>
              </div>
              <span v-else class="text-slate-500">N/A</span>
            </td>
            <!-- 价差 -->
            <td class="px-4 py-3 text-sm text-right">
              <span :class="getPriceDiffClass(comparison.priceDiff)">
                {{ formatPrice(comparison.priceDiff) }}
              </span>
            </td>
            <!-- 费率差 -->
            <td class="px-4 py-3 text-sm text-right">
              <span :class="getRateDiffClass(comparison.rateDiff)">
                {{ formatRate(comparison.rateDiff) }}
              </span>
            </td>
            <!-- 套利 -->
            <td class="px-4 py-3 text-center">
              <span
                :class="getArbitrageBadgeClass(comparison.arbitrageLevel)"
                class="px-2 py-1 rounded-full text-xs font-medium"
              >
                {{ getArbitrageLabel(comparison.arbitrageLevel) }}
              </span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<script setup>
const props = defineProps({
  comparisons: {
    type: Object,
    default: () => ({}),
  },
  loading: {
    type: Boolean,
    default: false,
  },
})

const formatPrice = (price) => {
  if (!price) return 'N/A'
  return parseFloat(price).toLocaleString('zh-CN', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 8,
  })
}

const formatRate = (rate) => {
  if (rate === null || rate === undefined) return 'N/A'
  return (parseFloat(rate) * 100).toFixed(4) + '%'
}

const getPriceDiffClass = (diff) => {
  if (!diff) return 'text-slate-500'
  const absDiff = Math.abs(diff)
  if (absDiff > 100) return 'text-red-400'
  if (absDiff > 10) return 'text-yellow-400'
  return 'text-green-400'
}

const getRateDiffClass = (diff) => {
  if (diff === null || diff === undefined) return 'text-slate-500'
  const absDiff = Math.abs(diff)
  if (absDiff >= 0.0005) return 'text-red-400 font-bold'
  if (absDiff >= 0.0002) return 'text-yellow-400'
  return 'text-green-400'
}

const getRateClass = (rate) => {
  if (rate === null || rate === undefined) return 'text-slate-500'
  const absRate = Math.abs(rate)
  // 正费率（做多付费）用红色，负费率（做空付费）用绿色
  if (rate > 0) {
    if (absRate >= 0.001) return 'text-red-400 font-medium'
    return 'text-red-300'
  } else if (rate < 0) {
    if (absRate >= 0.001) return 'text-green-400 font-medium'
    return 'text-green-300'
  }
  return 'text-slate-400'
}

const getArbitrageBadgeClass = (level) => {
  switch (level) {
    case 'high':
      return 'bg-red-500/20 text-red-400 border border-red-500/50'
    case 'medium':
      return 'bg-yellow-500/20 text-yellow-400 border border-yellow-500/50'
    default:
      return 'bg-slate-600/20 text-slate-400 border border-slate-600/50'
  }
}

const getArbitrageLabel = (level) => {
  switch (level) {
    case 'high':
      return '高'
    case 'medium':
      return '中'
    default:
      return '-'
  }
}
</script>
