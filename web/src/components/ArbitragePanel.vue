<template>
  <div class="bg-slate-800 rounded-lg shadow-lg overflow-hidden">
    <div class="p-4 border-b border-slate-700 flex items-center justify-between">
      <h2 class="text-xl font-semibold text-white">套利机会</h2>
      <div class="flex gap-2">
        <button
          v-for="filter in filters"
          :key="filter.value"
          @click="activeFilter = filter.value"
          :class="[
            'px-3 py-1 rounded text-sm font-medium transition-colors',
            activeFilter === filter.value
              ? 'bg-primary-600 text-white'
              : 'bg-slate-700 text-slate-300 hover:bg-slate-600'
          ]"
        >
          {{ filter.label }}
        </button>
      </div>
    </div>

    <div v-if="loading" class="p-8 text-center text-slate-400">
      加载中...
    </div>

    <div v-else-if="filteredOpportunities.length === 0" class="p-8 text-center text-slate-400">
      暂无套利机会
    </div>

    <div v-else class="overflow-x-auto">
      <table class="w-full">
        <thead class="bg-slate-700">
          <tr>
            <th class="px-4 py-3 text-left text-xs font-medium text-slate-300 uppercase">币种</th>
            <th class="px-4 py-3 text-left text-xs font-medium text-slate-300 uppercase">交易所A</th>
            <th class="px-4 py-3 text-left text-xs font-medium text-slate-300 uppercase">交易所B</th>
            <th class="px-4 py-3 text-right text-xs font-medium text-slate-300 uppercase">费率差</th>
            <th class="px-4 py-3 text-right text-xs font-medium text-slate-300 uppercase">预期收益</th>
            <th class="px-4 py-3 text-center text-xs font-medium text-slate-300 uppercase">级别</th>
            <th class="px-4 py-3 text-left text-xs font-medium text-slate-300 uppercase">发现时间</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-slate-700">
          <tr
            v-for="opp in filteredOpportunities"
            :key="opp.id"
            class="hover:bg-slate-700/50 transition-colors"
          >
            <td class="px-4 py-3 text-sm font-medium text-white">{{ opp.symbol }}</td>
            <td class="px-4 py-3 text-sm text-slate-300">{{ opp.exchangeA }}</td>
            <td class="px-4 py-3 text-sm text-slate-300">{{ opp.exchangeB }}</td>
            <td class="px-4 py-3 text-sm text-right">
              <span :class="getRateDiffClass(opp.rateDiff)">
                {{ formatRate(opp.rateDiff) }}
              </span>
            </td>
            <td class="px-4 py-3 text-sm text-right">
              <span :class="getProfitabilityClass(opp.profitability)">
                {{ formatRate(opp.profitability) }}
              </span>
            </td>
            <td class="px-4 py-3 text-center">
              <span
                :class="getArbitrageBadgeClass(opp.arbitrageLevel)"
                class="px-2 py-1 rounded-full text-xs font-medium"
              >
                {{ getArbitrageLabel(opp.arbitrageLevel) }}
              </span>
            </td>
            <td class="px-4 py-3 text-sm text-slate-400">
              {{ formatTime(opp.detectedAt) }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'

const props = defineProps({
  opportunities: {
    type: Array,
    default: () => [],
  },
  loading: {
    type: Boolean,
    default: false,
  },
})

const activeFilter = ref('all')
const filters = [
  { value: 'all', label: '全部' },
  { value: 'high', label: '高套利' },
  { value: 'medium', label: '中等' },
]

const filteredOpportunities = computed(() => {
  if (activeFilter.value === 'all') {
    return props.opportunities
  }
  return props.opportunities.filter(opp => opp.arbitrageLevel === activeFilter.value)
})

const formatRate = (rate) => {
  if (rate === null || rate === undefined) return 'N/A'
  return (parseFloat(rate) * 100).toFixed(4) + '%'
}

const formatTime = (time) => {
  if (!time) return ''
  return new Date(time).toLocaleString('zh-CN')
}

const getRateDiffClass = (diff) => {
  if (diff === null || diff === undefined) return 'text-slate-500'
  const absDiff = Math.abs(diff)
  if (absDiff >= 0.0005) return 'text-red-400 font-bold'
  if (absDiff >= 0.0002) return 'text-yellow-400'
  return 'text-green-400'
}

const getProfitabilityClass = (profit) => {
  if (profit === null || profit === undefined) return 'text-slate-500'
  if (profit >= 0.0005) return 'text-green-400 font-bold'
  if (profit >= 0.0002) return 'text-yellow-400'
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
