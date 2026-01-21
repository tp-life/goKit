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
              <span class="text-gray-900 font-medium">标签：#{{ tagName }}</span>
            </div>
          </div>
        </div>
      </div>
    </nav>

    <!-- 主内容区 -->
    <main class="max-w-6xl mx-auto px-6 py-8">
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
          <p class="text-sm text-gray-500">加载中...</p>
        </div>
      </div>

      <!-- 时间轴列表 -->
      <div v-else-if="items.length > 0" class="space-y-3">
        <div
          v-for="item in items"
          :key="`${item.type}-${item.id}`"
          class="card p-4 cursor-pointer group"
          @click="handleItemClick(item)"
        >
          <!-- Memo 卡片 -->
          <div v-if="item.type === 'memo'" class="space-y-3">
            <p
              class="text-gray-900 whitespace-pre-wrap leading-relaxed line-clamp-3"
              v-html="highlightTags(item.content)"
            ></p>
            <div v-if="item.images && item.images.length > 0" class="grid grid-cols-3 gap-2 mt-3">
              <img
                v-for="(img, idx) in item.images"
                :key="idx"
                :src="img"
                alt=""
                class="w-full h-32 object-cover rounded"
              />
            </div>
            <div class="flex items-center justify-between">
              <div class="text-xs text-gray-400 font-mono">
                {{ formatDate(item.created_at) }}
              </div>
              <span class="text-xs text-gray-400">点击查看 →</span>
            </div>
          </div>

          <!-- Page 卡片 -->
          <div v-else-if="item.type === 'page'" class="space-y-2">
            <div class="flex items-start gap-4">
              <!-- 封面图片 -->
              <div
                v-if="item.cover"
                class="w-20 h-20 flex-shrink-0 rounded overflow-hidden bg-gray-100"
              >
                <img :src="item.cover" alt="" class="w-full h-full object-cover" />
              </div>
              <div class="flex-1 min-w-0">
                <h3 class="text-lg font-semibold text-gray-900 mb-1 group-hover:text-gray-700">
                  {{ item.title || '无标题' }}
                </h3>
                <p v-if="item.summary" class="text-sm text-gray-600 line-clamp-2">
                  {{ item.summary }}
                </p>
              </div>
            </div>
            <div class="flex items-center justify-between">
              <div class="text-xs text-gray-400 font-mono">
                {{ formatDate(item.created_at) }}
              </div>
              <div class="flex items-center gap-2">
                <span v-if="item.is_shared" class="text-xs text-gray-500">已分享</span>
                <span class="text-xs text-gray-400">点击查看 →</span>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- 空状态 -->
      <div v-else class="flex flex-col items-center justify-center py-20">
        <div class="text-center space-y-4">
          <p class="text-gray-400">该标签下暂无内容</p>
        </div>
      </div>
    </main>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import apiClient from '@/utils/api'

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()

const tagName = ref(route.params.name)
const loading = ref(false)
const items = ref([])

async function loadTagTimeline() {
  if (!tagName.value || !authStore.isAuthenticated) {
    return
  }

  loading.value = true

  try {
    const response = await apiClient.get(`/api/v1/tags/${tagName.value}/timeline`)
    items.value = response.data.items || []
  } catch (error) {
    console.error('加载标签时间轴失败:', error)
    if (error.response?.status === 401) {
      router.push('/login')
    }
  } finally {
    loading.value = false
  }
}

function handleItemClick(item) {
  if (item.type === 'page') {
    router.push(`/pages/${item.id}`)
  } else if (item.type === 'memo') {
    router.push(`/memos/${item.id}`)
  }
}

function highlightTags(text) {
  if (!text) return text
  // 将 #标签 转换为可点击的链接
  return text.replace(
    /#(\S+)/g,
    '<a href="/tags/$1/timeline" class="text-blue-600 hover:text-blue-800 hover:underline font-medium">#$1</a>'
  )
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

onMounted(() => {
  loadTagTimeline()
})
</script>
