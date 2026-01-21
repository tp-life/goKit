<template>
  <div class="min-h-screen bg-white">
    <!-- 顶部导航栏 -->
    <nav class="sticky top-0 z-10 bg-white/80 backdrop-blur-sm border-b border-gray-200">
      <div class="max-w-6xl mx-auto px-6">
        <div class="flex items-center justify-between h-16">
          <div class="flex items-center gap-8">
            <router-link to="/" class="text-xl font-bold text-gray-900">
              Notion-Lite
            </router-link>
            <div class="hidden md:flex items-center gap-4 text-sm text-gray-600">
              <span class="text-gray-900 font-medium">搜索</span>
            </div>
          </div>
        </div>
      </div>
    </nav>

    <!-- 主内容区 -->
    <main class="max-w-6xl mx-auto px-6 py-8">
      <!-- 搜索框 -->
      <div class="mb-8">
        <div class="relative">
          <input
            v-model="searchQuery"
            @keyup.enter="performSearch"
            type="text"
            placeholder="输入关键词搜索..."
            class="w-full px-4 py-3 pl-12 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
          <svg
            class="absolute left-4 top-1/2 transform -translate-y-1/2 w-5 h-5 text-gray-400"
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
          >
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              stroke-width="2"
              d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z"
            />
          </svg>
          <button
            v-if="searchQuery"
            @click="clearSearch"
            class="absolute right-4 top-1/2 transform -translate-y-1/2 text-gray-400 hover:text-gray-600"
          >
            <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                stroke-width="2"
                d="M6 18L18 6M6 6l12 12"
              />
            </svg>
          </button>
        </div>
      </div>

      <!-- 加载状态 -->
      <div v-if="loading" class="flex items-center justify-center py-20">
        <div class="flex flex-col items-center gap-3">
          <svg
            class="animate-spin h-6 w-6 text-gray-400"
            xmlns="http://www.w3.org/2000/svg"
            fill="none"
            viewBox="0 0 24 24"
          >
            <circle
              class="opacity-25"
              cx="12"
              cy="12"
              r="10"
              stroke="currentColor"
              stroke-width="4"
            ></circle>
            <path
              class="opacity-75"
              fill="currentColor"
              d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
            ></path>
          </svg>
          <p class="text-sm text-gray-500">搜索中...</p>
        </div>
      </div>

      <!-- 搜索结果 -->
      <div v-else-if="searchQuery && results.length > 0" class="space-y-3">
        <div class="text-sm text-gray-600 mb-4">
          找到 {{ total }} 个结果（关键词：<span class="font-medium text-gray-900">{{ searchQuery }}</span>）
        </div>

        <div
          v-for="item in results"
          :key="`${item.type}-${item.id}`"
          class="card p-4 cursor-pointer group"
          @click="handleItemClick(item)"
        >
          <!-- Memo 结果 -->
          <div v-if="item.type === 'memo'" class="space-y-2">
            <div class="flex items-center gap-2 text-xs text-gray-400">
              <span class="px-2 py-1 bg-blue-100 text-blue-700 rounded">Memo</span>
            </div>
            <p
              class="text-gray-900 whitespace-pre-wrap leading-relaxed line-clamp-3"
              v-html="highlightText(item.content, searchQuery)"
            ></p>
            <div class="text-xs text-gray-400 font-mono">
              {{ formatDate(item.created_at) }}
            </div>
          </div>

          <!-- Page 结果 -->
          <div v-else-if="item.type === 'page'" class="space-y-2">
            <div class="flex items-center gap-2 text-xs text-gray-400">
              <span class="px-2 py-1 bg-green-100 text-green-700 rounded">Page</span>
            </div>
            <h3 class="text-lg font-semibold text-gray-900 group-hover:text-gray-700">
              {{ item.title || '无标题' }}
            </h3>
            <!-- 命中片段 -->
            <p
              v-if="item.hit_fragment"
              class="text-sm text-gray-600 line-clamp-2"
              v-html="highlightText(item.hit_fragment, searchQuery)"
            ></p>
            <!-- 摘要 -->
            <p v-else-if="item.summary" class="text-sm text-gray-600 line-clamp-2">
              {{ item.summary }}
            </p>
            <div class="flex items-center justify-between">
              <div class="text-xs text-gray-400 font-mono">
                {{ formatDate(item.created_at) }}
              </div>
              <span v-if="item.is_shared" class="text-xs text-gray-500">已分享</span>
            </div>
          </div>
        </div>
      </div>

      <!-- 空状态 -->
      <div
        v-else-if="searchQuery && !loading"
        class="flex flex-col items-center justify-center py-20"
      >
        <div class="text-center space-y-4">
          <p class="text-gray-400">未找到相关结果</p>
          <p class="text-sm text-gray-500">尝试使用其他关键词搜索</p>
        </div>
      </div>

      <!-- 初始状态 -->
      <div v-else class="flex flex-col items-center justify-center py-20">
        <div class="text-center space-y-4">
          <svg
            class="w-16 h-16 text-gray-300 mx-auto"
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
          >
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              stroke-width="2"
              d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z"
            />
          </svg>
          <p class="text-gray-400">输入关键词开始搜索</p>
        </div>
      </div>
    </main>
  </div>
</template>

<script setup>
import { ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import apiClient from '@/utils/api'

const route = useRoute()
const router = useRouter()

const searchQuery = ref('')
const loading = ref(false)
const results = ref([])
const total = ref(0)

// 从 URL 参数获取搜索关键词
if (route.query.q) {
  searchQuery.value = route.query.q
  performSearch()
}

// 监听搜索关键词变化，延迟搜索
let searchTimer = null
watch(searchQuery, (newQuery) => {
  if (searchTimer) {
    clearTimeout(searchTimer)
  }
  
  if (newQuery.trim()) {
    searchTimer = setTimeout(() => {
      performSearch()
    }, 500) // 500ms 防抖
  } else {
    results.value = []
    total.value = 0
  }
})

async function performSearch() {
  if (!searchQuery.value.trim()) {
    results.value = []
    total.value = 0
    return
  }

  loading.value = true

  try {
    const response = await apiClient.get('/api/v1/search', {
      params: {
        q: searchQuery.value.trim(),
        limit: 50,
        offset: 0,
      },
    })

    results.value = response.data.results || []
    total.value = response.data.total || 0

    // 更新 URL 参数
    router.replace({ query: { q: searchQuery.value.trim() } })
  } catch (error) {
    console.error('搜索失败:', error)
    results.value = []
    total.value = 0
  } finally {
    loading.value = false
  }
}

function clearSearch() {
  searchQuery.value = ''
  results.value = []
  total.value = 0
  router.replace({ query: {} })
}

function handleItemClick(item) {
  if (item.type === 'page') {
    router.push(`/pages/${item.id}`)
  } else if (item.type === 'memo') {
    router.push(`/memos/${item.id}`)
  }
}

function highlightText(text, query) {
  if (!text || !query) return text
  
  const regex = new RegExp(`(${query})`, 'gi')
  return text.replace(regex, '<mark class="bg-yellow-200">$1</mark>')
}

function formatDate(dateString) {
  if (!dateString) return ''
  const date = new Date(dateString)
  const now = new Date()
  const diff = now - date
  const minutes = Math.floor(diff / 60000)
  const hours = Math.floor(diff / 3600000)
  const days = Math.floor(diff / 86400000)

  if (minutes < 1) return '刚刚'
  if (minutes < 60) return `${minutes}分钟前`
  if (hours < 24) return `${hours}小时前`
  if (days < 7) return `${days}天前`
  return date.toLocaleDateString('zh-CN', { month: 'short', day: 'numeric' })
}
</script>
