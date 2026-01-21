<template>
  <div class="tag-cloud bg-gradient-to-br from-blue-50 via-indigo-50 to-purple-50 rounded-2xl p-6 shadow-sm border border-gray-100">
    <div v-if="loading" class="flex items-center justify-center py-4">
      <div class="flex items-center gap-2 text-sm text-gray-500">
        <svg class="animate-spin h-4 w-4" fill="none" viewBox="0 0 24 24">
          <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
          <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
        </svg>
        <span>加载标签...</span>
      </div>
    </div>
    <div v-else-if="tags.length > 0" class="space-y-3">
      <div class="flex items-center gap-2 mb-4">
        <svg class="w-5 h-5 text-indigo-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M7 7h.01M7 3h5c.512 0 1.024.195 1.414.586l7 7a2 2 0 010 2.828l-7 7a2 2 0 01-2.828 0l-7-7A1.994 1.994 0 013 12V7a4 4 0 014-4z" />
        </svg>
        <h3 class="text-base font-semibold text-gray-800">标签云</h3>
      </div>
      <div class="flex flex-wrap items-center gap-2 md:gap-3">
        <router-link
          v-for="(tag, index) in tags"
          :key="tag.id"
          :to="`/tags/${tag.name}/timeline`"
          :style="{ animationDelay: `${index * 0.05}s` }"
          class="tag-item inline-flex items-center px-3 py-1.5 md:px-4 md:py-2 bg-white text-gray-700 rounded-full text-xs md:text-sm font-medium shadow-sm hover:shadow-md hover:scale-110 hover:bg-indigo-50 hover:text-indigo-700 transition-all duration-300 cursor-pointer border border-gray-200 hover:border-indigo-300"
        >
          <span class="text-indigo-500">#</span>{{ tag.name }}
        </router-link>
      </div>
    </div>
    <div v-else class="text-center py-6 text-sm text-gray-400">
      <svg class="w-12 h-12 mx-auto mb-2 text-gray-300" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M7 7h.01M7 3h5c.512 0 1.024.195 1.414.586l7 7a2 2 0 010 2.828l-7 7a2 2 0 01-2.828 0l-7-7A1.994 1.994 0 013 12V7a4 4 0 014-4z" />
      </svg>
      <p>暂无标签</p>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useAuthStore } from '@/stores/auth'
import apiClient from '@/utils/api'

const authStore = useAuthStore()
const loading = ref(false)
const tags = ref([])

async function loadTags() {
  if (!authStore.isAuthenticated) {
    return
  }

  loading.value = true

  try {
    const response = await apiClient.get('/api/v1/tags')
    tags.value = response.data.tags || []
  } catch (error) {
    console.error('加载标签失败:', error)
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  loadTags()
})
</script>

<style scoped>
.tag-item {
  animation: fadeInUp 0.5s ease-out backwards;
}

@keyframes fadeInUp {
  from {
    opacity: 0;
    transform: translateY(10px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

/* 移动端优化 */
@media (max-width: 768px) {
  .tag-cloud {
    padding: 1rem;
  }
  
  .tag-item {
    font-size: 0.75rem;
    padding: 0.375rem 0.75rem;
  }
}
</style>
