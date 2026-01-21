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
              <span class="text-gray-900 font-medium">回收站</span>
            </div>
          </div>
          <div class="flex items-center gap-3">
            <button
              v-if="selectedItems.length > 0"
              @click="batchRestore"
              :disabled="processing"
              class="btn btn-ghost text-sm"
            >
              恢复选中 ({{ selectedItems.length }})
            </button>
            <button
              v-if="selectedItems.length > 0"
              @click="batchDelete"
              :disabled="processing"
              class="btn btn-ghost text-sm text-red-600 hover:text-red-700"
            >
              彻底删除 ({{ selectedItems.length }})
            </button>
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

      <!-- 回收站列表 -->
      <div v-else-if="items.length > 0" class="space-y-3">
        <div
          v-for="item in items"
          :key="`${item.type}-${item.id}`"
          class="card p-4 group"
          :class="{ 'ring-2 ring-blue-500': isSelected(item) }"
        >
          <div class="flex items-start gap-4">
            <!-- 复选框 -->
            <input
              type="checkbox"
              :checked="isSelected(item)"
              @change="toggleSelect(item)"
              class="mt-1 w-4 h-4 text-blue-600 border-gray-300 rounded focus:ring-blue-500"
            />

            <!-- 内容 -->
            <div
              class="flex-1 cursor-pointer"
              @click="toggleSelect(item)"
            >
              <!-- Memo -->
              <div v-if="item.type === 'memo'" class="space-y-2">
                <div class="flex items-center gap-2">
                  <span class="px-2 py-1 bg-blue-100 text-blue-700 rounded text-xs">Memo</span>
                  <span class="text-xs text-gray-400 font-mono">
                    删除于 {{ formatDate(item.deleted_at) }}
                  </span>
                </div>
                <p class="text-gray-900 whitespace-pre-wrap leading-relaxed line-clamp-2">
                  {{ item.content }}
                </p>
              </div>

              <!-- Page -->
              <div v-else-if="item.type === 'page'" class="space-y-2">
                <div class="flex items-center gap-2">
                  <span class="px-2 py-1 bg-green-100 text-green-700 rounded text-xs">Page</span>
                  <span class="text-xs text-gray-400 font-mono">
                    删除于 {{ formatDate(item.deleted_at) }}
                  </span>
                </div>
                <h3 class="text-lg font-semibold text-gray-900">
                  {{ item.title || '无标题' }}
                </h3>
              </div>
            </div>

            <!-- 操作按钮 -->
            <div class="flex items-center gap-2 opacity-0 group-hover:opacity-100 transition-opacity">
              <button
                @click.stop="restoreItem(item)"
                :disabled="processing"
                class="btn btn-ghost text-xs"
                title="恢复"
              >
                恢复
              </button>
              <button
                @click.stop="deleteItem(item)"
                :disabled="processing"
                class="btn btn-ghost text-xs text-red-600 hover:text-red-700"
                title="彻底删除"
              >
                彻底删除
              </button>
            </div>
          </div>
        </div>
      </div>

      <!-- 空状态 -->
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
              d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"
            />
          </svg>
          <p class="text-gray-400">回收站是空的</p>
        </div>
      </div>
    </main>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import apiClient from '@/utils/api'

const router = useRouter()
const authStore = useAuthStore()

// 检查是否已登录
if (!authStore.isAuthenticated) {
  router.push('/login')
}

const loading = ref(false)
const processing = ref(false)
const items = ref([])
const selectedItems = ref([])

async function loadTrash() {
  loading.value = true

  try {
    const response = await apiClient.get('/api/v1/trash')
    items.value = response.data.items || []
  } catch (error) {
    console.error('加载回收站失败:', error)
    if (error.response?.status === 401) {
      router.push('/login')
    }
  } finally {
    loading.value = false
  }
}

function isSelected(item) {
  return selectedItems.value.some(
    (selected) => selected.type === item.type && selected.id === item.id
  )
}

function toggleSelect(item) {
  const index = selectedItems.value.findIndex(
    (selected) => selected.type === item.type && selected.id === item.id
  )

  if (index > -1) {
    selectedItems.value.splice(index, 1)
  } else {
    selectedItems.value.push(item)
  }
}

async function restoreItem(item) {
  if (!confirm(`确定要恢复这个${item.type === 'memo' ? 'Memo' : 'Page'}吗？`)) {
    return
  }

  processing.value = true

  try {
    await apiClient.post(`/api/v1/trash/${item.type}/${item.id}/restore`)
    
    // 从列表中移除
    const index = items.value.findIndex(
      (i) => i.type === item.type && i.id === item.id
    )
    if (index > -1) {
      items.value.splice(index, 1)
    }

    // 从选中列表中移除
    const selectedIndex = selectedItems.value.findIndex(
      (selected) => selected.type === item.type && selected.id === item.id
    )
    if (selectedIndex > -1) {
      selectedItems.value.splice(selectedIndex, 1)
    }

    alert('恢复成功！')
  } catch (error) {
    console.error('恢复失败:', error)
    alert(error.response?.data?.error || '恢复失败，请稍后重试')
  } finally {
    processing.value = false
  }
}

async function deleteItem(item) {
  if (
    !confirm(
      `确定要彻底删除这个${item.type === 'memo' ? 'Memo' : 'Page'}吗？此操作不可恢复！`
    )
  ) {
    return
  }

  processing.value = true

  try {
    await apiClient.delete(`/api/v1/trash/${item.type}/${item.id}`)

    // 从列表中移除
    const index = items.value.findIndex(
      (i) => i.type === item.type && i.id === item.id
    )
    if (index > -1) {
      items.value.splice(index, 1)
    }

    // 从选中列表中移除
    const selectedIndex = selectedItems.value.findIndex(
      (selected) => selected.type === item.type && selected.id === item.id
    )
    if (selectedIndex > -1) {
      selectedItems.value.splice(selectedIndex, 1)
    }

    alert('删除成功！')
  } catch (error) {
    console.error('删除失败:', error)
    alert(error.response?.data?.error || '删除失败，请稍后重试')
  } finally {
    processing.value = false
  }
}

async function batchRestore() {
  if (selectedItems.value.length === 0) return

  if (
    !confirm(`确定要恢复选中的 ${selectedItems.value.length} 个项目吗？`)
  ) {
    return
  }

  processing.value = true

  try {
    const promises = selectedItems.value.map((item) =>
      apiClient.post(`/api/v1/trash/${item.type}/${item.id}/restore`)
    )

    await Promise.all(promises)

    // 重新加载列表
    await loadTrash()
    selectedItems.value = []

    alert('批量恢复成功！')
  } catch (error) {
    console.error('批量恢复失败:', error)
    alert(error.response?.data?.error || '批量恢复失败，请稍后重试')
  } finally {
    processing.value = false
  }
}

async function batchDelete() {
  if (selectedItems.value.length === 0) return

  if (
    !confirm(
      `确定要彻底删除选中的 ${selectedItems.value.length} 个项目吗？此操作不可恢复！`
    )
  ) {
    return
  }

  processing.value = true

  try {
    const promises = selectedItems.value.map((item) =>
      apiClient.delete(`/api/v1/trash/${item.type}/${item.id}`)
    )

    await Promise.all(promises)

    // 重新加载列表
    await loadTrash()
    selectedItems.value = []

    alert('批量删除成功！')
  } catch (error) {
    console.error('批量删除失败:', error)
    alert(error.response?.data?.error || '批量删除失败，请稍后重试')
  } finally {
    processing.value = false
  }
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
  return date.toLocaleDateString('zh-CN', {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

onMounted(() => {
  loadTrash()
})
</script>
