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
              <span class="text-gray-900 font-medium">标签管理</span>
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

      <!-- 标签云 -->
      <div v-else-if="tags.length > 0" class="space-y-4">
        <div class="text-sm text-gray-600 mb-4">
          共 {{ tags.length }} 个标签
        </div>
        <div class="flex flex-wrap items-center gap-3">
          <router-link
            v-for="tag in tags"
            :key="tag.id"
            :to="`/tags/${tag.name}/timeline`"
            class="group relative inline-flex items-center px-4 py-2 bg-blue-100 text-blue-700 rounded-lg hover:bg-blue-200 transition-colors"
          >
            <span class="font-medium">#{{ tag.name }}</span>
            <button
              @click.stop="handleDeleteTag(tag)"
              class="ml-2 opacity-0 group-hover:opacity-100 transition-opacity text-red-600 hover:text-red-700"
              title="删除标签"
            >
              <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  stroke-width="2"
                  d="M6 18L18 6M6 6l12 12"
                />
              </svg>
            </button>
          </router-link>
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
              d="M7 7h.01M7 3h5c.512 0 1.024.195 1.414.586l7 7a2 2 0 010 2.828l-7 7a2 2 0 01-2.828 0l-7-7A1.994 1.994 0 013 12V7a4 4 0 014-4z"
            />
          </svg>
          <p class="text-gray-400">还没有标签</p>
          <p class="text-sm text-gray-500">在 Memo 或 Page 中使用 #标签名 来创建标签</p>
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
const tags = ref([])

async function loadTags() {
  loading.value = true

  try {
    const response = await apiClient.get('/api/v1/tags')
    tags.value = response.data.tags || []
  } catch (error) {
    console.error('加载标签失败:', error)
    if (error.response?.status === 401) {
      router.push('/login')
    }
  } finally {
    loading.value = false
  }
}

async function handleDeleteTag(tag) {
  if (!confirm(`确定要删除标签 "#${tag.name}" 吗？删除后标签将从所有关联的内容中移除。`)) {
    return
  }

  try {
    await apiClient.delete(`/api/v1/tags/${tag.id}`)
    
    // 从列表中移除
    const index = tags.value.findIndex((t) => t.id === tag.id)
    if (index > -1) {
      tags.value.splice(index, 1)
    }

    alert('标签删除成功！')
  } catch (error) {
    console.error('删除标签失败:', error)
    alert(error.response?.data?.error || '删除失败，请稍后重试')
  }
}

onMounted(() => {
  loadTags()
})
</script>
