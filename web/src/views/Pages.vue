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
              <span class="text-gray-900 font-medium">页面</span>
            </div>
          </div>
          <router-link to="/pages/new" class="btn btn-primary text-sm">
            新建页面
          </router-link>
        </div>
      </div>
    </nav>

    <!-- 主内容区 -->
    <main class="max-w-6xl mx-auto px-6 py-8">
      <!-- 页面列表 -->
      <div v-if="pages.length > 0" class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        <div
          v-for="page in pages"
          :key="page.id"
          class="card p-4 cursor-pointer group"
          @click="$router.push(`/pages/${page.id}`)"
        >
          <!-- 封面图片（优先显示） -->
          <div
            v-if="page.cover"
            class="w-full h-40 mb-3 rounded overflow-hidden bg-gray-100"
          >
            <img :src="page.cover" alt="" class="w-full h-full object-cover" />
          </div>
          <!-- 从 blocks 中提取的图片预览（如果没有封面） -->
          <div
            v-else-if="page.images && page.images.length > 0"
            class="w-full mb-3 rounded overflow-hidden bg-gray-100"
          >
            <div class="grid grid-cols-2 gap-1">
              <div
                v-for="(img, idx) in page.images.slice(0, 4)"
                :key="idx"
                class="aspect-square overflow-hidden"
              >
                <img
                  :src="img"
                  :alt="`预览图 ${idx + 1}`"
                  class="w-full h-full object-cover"
                />
              </div>
            </div>
          </div>
          
          <h3 class="text-base font-semibold text-gray-900 mb-2 group-hover:text-gray-700">
            {{ page.title || '无标题' }}
          </h3>
          <p v-if="page.summary" class="text-sm text-gray-600 line-clamp-2 mb-3">
            {{ page.summary }}
          </p>
          <!-- 标签显示 -->
          <div v-if="page.tags && page.tags.length > 0" class="flex flex-wrap items-center gap-2 mb-3">
            <router-link
              v-for="(tag, index) in page.tags"
              :key="index"
              :to="`/tags/${tag}/timeline`"
              class="inline-flex items-center px-2 py-0.5 bg-green-100 text-green-700 rounded text-xs hover:bg-green-200"
              @click.stop
            >
              #{{ tag }}
            </router-link>
          </div>
          <div class="flex items-center justify-between text-xs text-gray-400">
            <span class="font-mono">{{ formatDate(page.created_at) }}</span>
            <span v-if="page.is_shared" class="text-gray-500">已分享</span>
          </div>
        </div>
      </div>

      <!-- 空状态 -->
      <div v-else class="flex flex-col items-center justify-center py-20">
        <div class="text-center space-y-4">
          <p class="text-gray-400">还没有页面</p>
          <router-link to="/pages/new" class="btn btn-primary text-sm">
            创建第一个页面
          </router-link>
        </div>
      </div>
    </main>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import apiClient from '@/utils/api'

const pages = ref([])

async function loadPages() {
  try {
    const response = await apiClient.get('/api/v1/timeline', {
      params: {
        limit: 100,
        offset: 0,
      },
    })

    pages.value = response.data.items.filter(item => item.type === 'page')
  } catch (error) {
    console.error('Failed to load pages:', error)
  }
}

function formatDate(dateString) {
  const date = new Date(dateString)
  return date.toLocaleDateString('zh-CN', { month: 'short', day: 'numeric' })
}

onMounted(() => {
  loadPages()
})
</script>
