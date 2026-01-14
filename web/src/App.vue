<template>
  <div class="min-h-screen bg-slate-900">
    <!-- Header -->
    <header class="bg-slate-800 border-b border-slate-700">
      <div class="container mx-auto px-4 py-4">
        <h1 class="text-2xl font-bold text-white">套利监控系统</h1>
        <p class="text-sm text-slate-400 mt-1">实时监控多交易所资金费率和价格差异</p>
      </div>
    </header>

    <!-- Main Content -->
    <main class="container mx-auto px-4 py-6">
      <!-- Status Bar -->
      <div class="mb-6 flex items-center justify-between">
        <div class="flex items-center gap-4">
          <StatusIndicator
            v-for="exchange in exchanges"
            :key="exchange"
            :name="exchange"
            :connected="connectionStatus[exchange]"
          />
        </div>
        <div class="text-sm text-slate-400">
          最后更新: {{ lastUpdateTime || '未更新' }}
        </div>
      </div>

      <!-- Tabs -->
      <div class="mb-6">
        <div class="border-b border-slate-700">
          <nav class="flex space-x-8">
            <button
              v-for="tab in tabs"
              :key="tab.id"
              @click="activeTab = tab.id"
              :class="[
                'py-4 px-1 border-b-2 font-medium text-sm transition-colors',
                activeTab === tab.id
                  ? 'border-primary-500 text-primary-400'
                  : 'border-transparent text-slate-400 hover:text-slate-300 hover:border-slate-300'
              ]"
            >
              {{ tab.label }}
            </button>
          </nav>
        </div>
      </div>

      <!-- Tab Content -->
      <div v-if="activeTab === 'comparison'">
        <ComparisonTable
          :comparisons="comparisons"
          :loading="loading"
        />
      </div>

      <div v-if="activeTab === 'arbitrage'">
        <ArbitragePanel
          :opportunities="arbitrageOpportunities"
          :loading="loading"
        />
      </div>

      <div v-if="activeTab === 'config'">
        <ConfigPanel
          :thresholds="thresholds"
          @update-threshold="handleUpdateThreshold"
        />
      </div>
    </main>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import ComparisonTable from './components/ComparisonTable.vue'
import ArbitragePanel from './components/ArbitragePanel.vue'
import ConfigPanel from './components/ConfigPanel.vue'
import StatusIndicator from './components/StatusIndicator.vue'
import { useWebSocket } from './composables/useWebSocket'
import { useAPI } from './composables/useAPI'

const activeTab = ref('comparison')
const tabs = [
  { id: 'comparison', label: '价格对比' },
  { id: 'arbitrage', label: '套利机会' },
  { id: 'config', label: '配置' },
]

const exchanges = ['binance', 'lighter', 'hyperliquid']
const connectionStatus = ref({})
const comparisons = ref({})
const arbitrageOpportunities = ref([])
const thresholds = ref({})
const loading = ref(false)
const lastUpdateTime = ref('')

const { connect, disconnect, isConnected } = useWebSocket()
const api = useAPI()

// 格式化时间
const formatTime = (time) => {
  if (!time) return ''
  return new Date(time).toLocaleTimeString('zh-CN')
}

// 更新连接状态
const updateConnectionStatus = () => {
  // 从 WebSocket 连接状态更新
  exchanges.forEach(exchange => {
    connectionStatus.value[exchange] = isConnected.value
  })
}

// 处理 WebSocket 消息
const handleWebSocketMessage = (data) => {
  if (data.type === 'comparison') {
    console.log('Received comparison data:', data.data)
    comparisons.value = data.data
    lastUpdateTime.value = formatTime(new Date())
  } else if (data.type === 'arbitrage') {
    arbitrageOpportunities.value = data.data
  }
}

// 加载初始数据
const loadInitialData = async () => {
  loading.value = true
  try {
    const [comparisonData, arbitrageData] = await Promise.all([
      api.getComparisons(),
      api.getArbitrageOpportunities(),
    ])
    comparisons.value = comparisonData
    arbitrageOpportunities.value = arbitrageData
    lastUpdateTime.value = formatTime(new Date())
  } catch (error) {
    console.error('Failed to load initial data:', error)
  } finally {
    loading.value = false
  }
}

// 更新阈值
const handleUpdateThreshold = async (threshold) => {
  try {
    await api.updateThreshold(threshold)
    // 重新加载数据
    await loadInitialData()
  } catch (error) {
    console.error('Failed to update threshold:', error)
  }
}

onMounted(async () => {
  // 连接 WebSocket
  connect(handleWebSocketMessage)
  
  // 加载初始数据
  await loadInitialData()
  
  // 定时更新连接状态
  const statusInterval = setInterval(updateConnectionStatus, 5000)
  
  onUnmounted(() => {
    clearInterval(statusInterval)
    disconnect()
  })
})
</script>
