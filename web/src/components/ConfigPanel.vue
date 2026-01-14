<template>
  <div class="bg-slate-800 rounded-lg shadow-lg overflow-hidden">
    <div class="p-4 border-b border-slate-700">
      <h2 class="text-xl font-semibold text-white">套利阈值配置</h2>
    </div>

    <div class="p-6">
      <div class="space-y-6">
        <div>
          <label class="block text-sm font-medium text-slate-300 mb-2">
            高套利阈值
          </label>
          <input
            v-model.number="localThresholds.high"
            type="number"
            step="0.0001"
            class="w-full px-3 py-2 bg-slate-700 border border-slate-600 rounded text-white focus:outline-none focus:ring-2 focus:ring-primary-500"
            placeholder="0.0005"
          />
          <p class="mt-1 text-xs text-slate-400">费率差绝对值 ≥ 此值时视为高套利机会（默认: 0.05%）</p>
        </div>

        <div>
          <label class="block text-sm font-medium text-slate-300 mb-2">
            中等套利阈值
          </label>
          <input
            v-model.number="localThresholds.medium"
            type="number"
            step="0.0001"
            class="w-full px-3 py-2 bg-slate-700 border border-slate-600 rounded text-white focus:outline-none focus:ring-2 focus:ring-primary-500"
            placeholder="0.0002"
          />
          <p class="mt-1 text-xs text-slate-400">费率差绝对值 ≥ 此值时视为中等套利机会（默认: 0.02%）</p>
        </div>

        <div>
          <label class="block text-sm font-medium text-slate-300 mb-2">
            最小预期收益率
          </label>
          <input
            v-model.number="localThresholds.minProfitability"
            type="number"
            step="0.0001"
            class="w-full px-3 py-2 bg-slate-700 border border-slate-600 rounded text-white focus:outline-none focus:ring-2 focus:ring-primary-500"
            placeholder="0.0001"
          />
          <p class="mt-1 text-xs text-slate-400">扣除手续费后的最小预期收益率（默认: 0.01%）</p>
        </div>

        <div class="flex justify-end gap-3">
          <button
            @click="handleReset"
            class="px-4 py-2 bg-slate-700 text-slate-300 rounded hover:bg-slate-600 transition-colors"
          >
            重置
          </button>
          <button
            @click="handleSave"
            class="px-4 py-2 bg-primary-600 text-white rounded hover:bg-primary-700 transition-colors"
          >
            保存
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, watch } from 'vue'

const props = defineProps({
  thresholds: {
    type: Object,
    default: () => ({
      high: 0.0005,
      medium: 0.0002,
      minProfitability: 0.0001,
    }),
  },
})

const emit = defineEmits(['update-threshold'])

const localThresholds = ref({ ...props.thresholds })

watch(() => props.thresholds, (newVal) => {
  localThresholds.value = { ...newVal }
}, { deep: true })

const handleSave = () => {
  emit('update-threshold', localThresholds.value)
}

const handleReset = () => {
  localThresholds.value = {
    high: 0.0005,
    medium: 0.0002,
    minProfitability: 0.0001,
  }
}
</script>
