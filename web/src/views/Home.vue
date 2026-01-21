<template>
  <div class="min-h-screen bg-white">
    <!-- 顶部导航栏 - HeroUI 风格 -->
    <nav class="sticky top-0 z-10 bg-white/80 backdrop-blur-sm border-b border-gray-200">
      <div class="max-w-6xl mx-auto px-6">
        <div class="flex items-center justify-between h-16">
          <div class="flex items-center gap-8">
            <h1 class="text-xl font-bold text-gray-900">Notion-Lite</h1>
            <div class="hidden md:flex items-center gap-4 text-sm text-gray-600">
              <span class="text-gray-900 font-medium">时间轴</span>
            </div>
          </div>
          
          <div class="flex items-center gap-3">
            <!-- 搜索入口 -->
            <button
              @click="showSearchModal = true"
              class="btn btn-ghost text-sm p-2 relative group hover:scale-110 transition-transform duration-200"
              title="搜索"
            >
              <svg 
                class="w-5 h-5 transition-all duration-200 group-hover:text-blue-600 group-hover:rotate-90" 
                fill="none" 
                stroke="currentColor" 
                viewBox="0 0 24 24"
              >
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
              </svg>
            </button>
            
            <template v-if="authStore.isAuthenticated">
              <router-link
                to="/memo/create"
                class="btn btn-ghost text-sm"
              >
                快速记录
              </router-link>
              <router-link
                to="/pages/new"
                class="btn btn-primary text-sm"
              >
                新建页面
              </router-link>
              <!-- 标签管理入口 -->
              <router-link
                to="/tags"
                class="btn btn-ghost text-sm"
                title="标签管理"
              >
                标签
              </router-link>
              <!-- 回收站入口 -->
              <router-link
                to="/trash"
                class="btn btn-ghost text-sm"
                title="回收站"
              >
                回收站
              </router-link>
              <button
                @click="handleLogout"
                class="btn btn-ghost text-sm"
              >
                退出
              </button>
            </template>
            <template v-else>
              <router-link
                to="/login"
                class="btn btn-ghost text-sm"
              >
                登录
              </router-link>
              <router-link
                to="/register"
                class="btn btn-primary text-sm"
              >
                注册
              </router-link>
            </template>
          </div>
        </div>
      </div>
    </nav>

    <!-- 主内容区 -->
    <main class="max-w-6xl mx-auto px-6 py-8">
      <!-- 标签云区域 -->
      <div v-if="authStore.isAuthenticated && !loading" class="mb-8">
        <TagCloud />
      </div>

      <!-- 加载状态 -->
      <div v-if="loading" class="flex items-center justify-center py-20">
        <div class="flex flex-col items-center gap-3">
          <svg class="animate-spin h-6 w-6 text-gray-400" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
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
            <!-- 标签显示 -->
            <div v-if="item.tags && item.tags.length > 0" class="flex flex-wrap items-center gap-2">
              <router-link
                v-for="(tag, index) in item.tags"
                :key="index"
                :to="`/tags/${tag}/timeline`"
                class="inline-flex items-center px-2 py-0.5 bg-blue-100 text-blue-700 rounded text-xs hover:bg-blue-200"
                @click.stop
              >
                #{{ tag }}
              </router-link>
            </div>
            <div class="flex items-center justify-between">
              <div class="text-xs text-gray-400 font-mono">
                {{ formatDate(item.created_at) }}
              </div>
              <span v-if="authStore.isAuthenticated" class="text-xs text-gray-400">点击查看 →</span>
            </div>
          </div>

          <!-- Page 卡片 -->
          <div v-else-if="item.type === 'page'" class="space-y-2">
            <div class="flex items-start gap-4">
              <!-- 封面图片（优先显示） -->
              <div
                v-if="item.cover"
                class="w-20 h-20 flex-shrink-0 rounded overflow-hidden bg-gray-100"
              >
                <img :src="item.cover" alt="" class="w-full h-full object-cover" />
              </div>
              <!-- 从 blocks 中提取的图片预览（如果没有封面） -->
              <div
                v-else-if="item.images && item.images.length > 0"
                class="w-20 h-20 flex-shrink-0 rounded overflow-hidden bg-gray-100 grid grid-cols-2 gap-0.5"
              >
                <div
                  v-for="(img, idx) in item.images.slice(0, 4)"
                  :key="idx"
                  class="overflow-hidden"
                >
                  <img
                    :src="img"
                    :alt="`预览图 ${idx + 1}`"
                    class="w-full h-full object-cover"
                  />
                </div>
              </div>
              <div class="flex-1 min-w-0">
                <h3 class="text-lg font-semibold text-gray-900 mb-1 group-hover:text-gray-700">
                  {{ item.title || '无标题' }}
                </h3>
                <p v-if="item.summary" class="text-sm text-gray-600 line-clamp-2">
                  {{ item.summary }}
                </p>
                <!-- 标签显示 -->
                <div v-if="item.tags && item.tags.length > 0" class="flex flex-wrap items-center gap-2 mt-2">
                  <router-link
                    v-for="(tag, index) in item.tags"
                    :key="index"
                    :to="`/tags/${tag}/timeline`"
                    class="inline-flex items-center px-2 py-0.5 bg-green-100 text-green-700 rounded text-xs hover:bg-green-200"
                    @click.stop
                  >
                    #{{ tag }}
                  </router-link>
                </div>
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

        <!-- 加载更多 -->
        <div v-if="hasMore" class="text-center py-6">
          <button
            @click="loadMore"
            :disabled="loadingMore"
            class="btn btn-ghost text-sm"
          >
            {{ loadingMore ? '加载中...' : '加载更多' }}
          </button>
        </div>
      </div>

      <!-- 空状态 -->
      <div v-else class="flex flex-col items-center justify-center py-20">
        <div class="text-center space-y-4">
          <p v-if="authStore.isAuthenticated" class="text-gray-400">还没有内容，开始记录吧！</p>
          <p v-else class="text-gray-400">暂无公开内容，<router-link to="/login" class="text-gray-900 underline">登录</router-link>后查看您的内容</p>
          <div v-if="authStore.isAuthenticated" class="flex items-center gap-3 justify-center">
            <router-link to="/memo/create" class="btn btn-ghost text-sm">
              快速记录
            </router-link>
            <router-link to="/pages/new" class="btn btn-primary text-sm">
              新建页面
            </router-link>
          </div>
          <div v-else class="flex items-center gap-3 justify-center">
            <router-link to="/login" class="btn btn-primary text-sm">
              登录
            </router-link>
            <router-link to="/register" class="btn btn-ghost text-sm">
              注册
            </router-link>
          </div>
        </div>
      </div>
    </main>

    <!-- 搜索弹窗 -->
    <Teleport to="body">
      <transition
        enter-active-class="transition-opacity duration-300"
        enter-from-class="opacity-0"
        enter-to-class="opacity-100"
        leave-active-class="transition-opacity duration-200"
        leave-from-class="opacity-100"
        leave-to-class="opacity-0"
      >
        <div
          v-if="showSearchModal"
          class="fixed inset-0 z-50"
        >
          <!-- 半透明蒙层 -->
          <div 
            class="fixed inset-0 bg-black/50 backdrop-blur-sm transition-opacity" 
            @click="closeSearchModal"
          ></div>
          
          <!-- 弹窗内容 -->
          <transition
            enter-active-class="transition-all duration-300 ease-out"
            enter-from-class="opacity-0 scale-95 translate-y-4"
            enter-to-class="opacity-100 scale-100 translate-y-0"
            leave-active-class="transition-all duration-200 ease-in"
            leave-from-class="opacity-100 scale-100 translate-y-0"
            leave-to-class="opacity-0 scale-95 translate-y-4"
          >
            <div
              v-if="showSearchModal"
              class="fixed left-1/2 top-4 md:top-20 transform -translate-x-1/2 w-[calc(100%-2rem)] md:w-full max-w-3xl mx-4 bg-white rounded-xl md:rounded-lg shadow-2xl max-h-[calc(100vh-2rem)] md:max-h-[80vh] flex flex-col"
              @click.stop
            >
        <!-- 搜索框 -->
        <div class="p-4 md:p-6 border-b border-gray-200 relative bg-gradient-to-r from-blue-50 to-indigo-50">
          <!-- 关闭按钮（右上角） -->
          <button
            @click="closeSearchModal"
            class="absolute right-3 top-3 md:right-4 md:top-4 text-gray-400 hover:text-gray-600 z-10 p-1 rounded-full hover:bg-white/50 transition-all"
            title="关闭 (Esc)"
          >
            <svg class="w-5 h-5 md:w-6 md:h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                stroke-width="2"
                d="M6 18L18 6M6 6l12 12"
              />
            </svg>
          </button>
          
          <div class="relative pr-10 md:pr-12">
            <input
              v-model="searchQuery"
              @keyup.enter="performSearch"
              @input="handleSearchInput"
              type="text"
              placeholder="输入关键词搜索..."
              class="w-full px-4 md:px-5 py-3 md:py-4 pl-12 md:pl-14 pr-10 md:pr-12 border-2 border-gray-200 rounded-xl focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500 bg-white shadow-sm transition-all duration-200 text-base md:text-lg"
              ref="searchInputRef"
              autofocus
            />
            <svg
              class="absolute left-4 md:left-5 top-1/2 transform -translate-y-1/2 w-5 h-5 md:w-6 md:h-6 text-gray-400 transition-colors duration-200"
              :class="{ 'text-blue-500': searchQuery }"
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
              class="absolute right-4 md:right-5 top-1/2 transform -translate-y-1/2 text-gray-400 hover:text-gray-600 p-1 rounded-full hover:bg-gray-100 transition-all"
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

        <!-- 搜索结果区域 -->
        <div class="flex-1 overflow-y-auto p-4">
          <!-- 加载状态 -->
          <div v-if="searchLoading" class="flex items-center justify-center py-12">
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
          <div v-else-if="searchQuery && searchResults.length > 0" class="space-y-3">
            <div class="text-sm text-gray-600 mb-4">
              找到 {{ searchTotal }} 个结果（关键词：<span class="font-medium text-gray-900">{{ searchQuery }}</span>）
            </div>

            <div
              v-for="item in searchResults"
              :key="`${item.type}-${item.id}`"
              class="card p-4 cursor-pointer group hover:bg-gray-50 transition-colors"
              @click="handleSearchItemClick(item)"
            >
              <!-- Memo 结果 -->
              <div v-if="item.type === 'memo'" class="space-y-2">
                <div class="flex items-center gap-2 text-xs text-gray-400">
                  <span class="px-2 py-1 bg-blue-100 text-blue-700 rounded">Memo</span>
                </div>
                <p
                  class="text-gray-900 whitespace-pre-wrap leading-relaxed line-clamp-3"
                  v-html="highlightSearchText(item.content, searchQuery)"
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
                  v-html="highlightSearchText(item.hit_fragment, searchQuery)"
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
            v-else-if="searchQuery && !searchLoading"
            class="flex flex-col items-center justify-center py-12"
          >
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
              <p class="text-gray-400">未找到相关结果</p>
              <p class="text-sm text-gray-500">尝试使用其他关键词搜索</p>
            </div>
          </div>

          <!-- 初始状态 -->
          <div v-else class="flex flex-col items-center justify-center py-12">
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
          </div>
        </div>
        </div>
        </transition>
      </div>
      </transition>
    </Teleport>
  </div>
</template>

<script setup>
import { ref, onMounted, nextTick, onUnmounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import apiClient from '@/utils/api'
import TagCloud from '@/components/TagCloud.vue'

const authStore = useAuthStore()

const router = useRouter()

const items = ref([])
const loading = ref(false)
const loadingMore = ref(false)
const hasMore = ref(true)
const offset = ref(0)
const limit = 20

// 搜索弹窗相关
const showSearchModal = ref(false)
const searchQuery = ref('')
const searchLoading = ref(false)
const searchResults = ref([])
const searchTotal = ref(0)
const searchInputRef = ref(null)
let searchTimer = null

async function loadTimeline(reset = false) {
  if (reset) {
    offset.value = 0
    items.value = []
  }

  const currentLoading = offset.value === 0 ? loading : loadingMore
  currentLoading.value = true

  try {
    // 支持访客模式：未登录时也可以获取公开内容
    const response = await apiClient.get('/api/v1/timeline', {
      params: {
        limit,
        offset: offset.value,
      },
    })

    const { items: newItems, total } = response.data

    if (reset) {
      items.value = newItems
    } else {
      items.value.push(...newItems)
    }

    offset.value += newItems.length
    hasMore.value = offset.value < total
  } catch (error) {
    console.error('加载时间轴失败:', error)
    // 如果是 401 错误，说明需要登录（但访客模式应该不会返回 401）
    if (error.response?.status === 401) {
      // 不显示错误，只是不加载数据
    }
  } finally {
    currentLoading.value = false
  }
}

function loadMore() {
  if (!loadingMore.value && hasMore.value) {
    loadTimeline(false)
  }
}

function handleItemClick(item) {
  if (item.type === 'page') {
    // 点击页面，进入详情页（支持访客模式）
    router.push(`/pages/${item.id}`)
  } else if (item.type === 'memo') {
    // 点击 Memo，进入详情页（需要登录）
    if (authStore.isAuthenticated) {
      router.push(`/memos/${item.id}`)
    }
  }
}

function formatDate(dateString) {
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

function highlightTags(text) {
  if (!text) return text
  // 将 #标签 转换为可点击的链接
  return text.replace(
    /#(\S+)/g,
    '<a href="/tags/$1/timeline" class="text-blue-600 hover:text-blue-800 hover:underline font-medium">#$1</a>'
  )
}

function handleLogout() {
  authStore.logout()
  router.push('/login')
}

// 搜索相关函数
function closeSearchModal() {
  showSearchModal.value = false
  searchQuery.value = ''
  searchResults.value = []
  searchTotal.value = 0
}

function handleSearchInput() {
  if (searchTimer) {
    clearTimeout(searchTimer)
  }
  
  if (searchQuery.value.trim()) {
    searchTimer = setTimeout(() => {
      performSearch()
    }, 500) // 500ms 防抖
  } else {
    searchResults.value = []
    searchTotal.value = 0
  }
}

async function performSearch() {
  if (!searchQuery.value.trim()) {
    searchResults.value = []
    searchTotal.value = 0
    return
  }

  searchLoading.value = true

  try {
    const response = await apiClient.get('/api/v1/search', {
      params: {
        q: searchQuery.value.trim(),
        limit: 50,
        offset: 0,
      },
    })

    searchResults.value = response.data.results || []
    searchTotal.value = response.data.total || 0
  } catch (error) {
    console.error('搜索失败:', error)
    searchResults.value = []
    searchTotal.value = 0
  } finally {
    searchLoading.value = false
  }
}

function clearSearch() {
  searchQuery.value = ''
  searchResults.value = []
  searchTotal.value = 0
}

function handleSearchItemClick(item) {
  closeSearchModal()
  if (item.type === 'page') {
    router.push(`/pages/${item.id}`)
  } else if (item.type === 'memo') {
    router.push(`/memos/${item.id}`)
  }
}

function highlightSearchText(text, query) {
  if (!text || !query) return text
  
  const regex = new RegExp(`(${query.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')})`, 'gi')
  return text.replace(regex, '<mark class="bg-yellow-200">$1</mark>')
}

// 监听 ESC 键关闭弹窗
function handleKeyDown(event) {
  if (event.key === 'Escape' && showSearchModal.value) {
    closeSearchModal()
  }
}

// 监听弹窗显示，自动聚焦输入框
async function watchSearchModal() {
  if (showSearchModal.value) {
    await nextTick()
    if (searchInputRef.value) {
      searchInputRef.value.focus()
    }
    document.addEventListener('keydown', handleKeyDown)
  } else {
    document.removeEventListener('keydown', handleKeyDown)
  }
}

// 监听 showSearchModal 变化
watch(showSearchModal, watchSearchModal)

onMounted(() => {
  loadTimeline(true)
})

onUnmounted(() => {
  if (searchTimer) {
    clearTimeout(searchTimer)
  }
  document.removeEventListener('keydown', handleKeyDown)
})
</script>
