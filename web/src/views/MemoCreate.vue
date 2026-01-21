<template>
  <div class="min-h-screen bg-white">
    <!-- 顶部工具栏 -->
    <div class="sticky top-0 z-10 bg-white/80 backdrop-blur-sm border-b border-gray-200">
      <div class="max-w-2xl mx-auto px-6">
        <div class="flex items-center justify-between h-14">
          <router-link
            to="/"
            class="text-sm text-gray-500 hover:text-gray-900 transition-colors"
          >
            ← 返回
          </router-link>
          <button
            @click="handleSubmit"
            :disabled="loading || !content.trim()"
            class="btn btn-primary text-xs"
          >
            {{ loading ? '保存中...' : '保存' }}
          </button>
        </div>
      </div>
    </div>

    <!-- 主内容区 -->
    <main class="max-w-2xl mx-auto px-6 py-8">
      <div class="space-y-6">
        <!-- 文本输入区域 - 美化 -->
        <div class="relative bg-gradient-to-br from-gray-50 to-white rounded-xl p-6 shadow-sm border border-gray-100">
          <textarea
            v-model="content"
            placeholder="记录你的想法...#标签会自动提取"
            class="w-full min-h-[300px] px-0 py-4 text-base leading-7 text-gray-900 bg-transparent border-0 focus:outline-none focus:ring-0 resize-none placeholder:text-gray-400"
            rows="10"
            @keydown.ctrl.enter="handleSubmit"
          ></textarea>
          <!-- 字数统计 -->
          <div class="absolute bottom-2 right-4 text-xs text-gray-400">
            {{ content.length }}
          </div>
        </div>

        <!-- 标签输入区域 -->
        <div class="bg-white rounded-xl p-4 shadow-sm border border-gray-100">
          <label class="block text-sm font-medium text-gray-700 mb-3">
            标签 <span class="text-gray-400 font-normal text-xs">(可选，或使用 #标签 格式)</span>
          </label>
          
          <!-- 标签输入框 -->
          <div class="flex items-center gap-2 mb-3">
            <input
              v-model="tagInput"
              @keyup.enter="addTag"
              type="text"
              placeholder="输入标签后按 Enter 添加"
              class="flex-1 px-3 py-2 text-sm border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent"
            />
            <button
              @click="addTag"
              class="btn btn-ghost text-xs px-3 py-2"
            >
              添加
            </button>
          </div>

          <!-- 已添加的标签 -->
          <div v-if="tags.length > 0" class="flex flex-wrap items-center gap-2 mt-2">
            <div
              v-for="(tag, index) in tags"
              :key="index"
              class="inline-flex items-center gap-1 px-3 py-1 bg-blue-100 text-blue-700 rounded-full text-sm"
            >
              <span>#{{ tag }}</span>
              <button
                @click="removeTag(index)"
                class="ml-1 text-blue-600 hover:text-blue-800 transition-colors"
              >
                <svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
                </svg>
              </button>
            </div>
          </div>
        </div>

        <!-- 图片上传区域 -->
        <div class="bg-white rounded-xl p-4 shadow-sm border border-gray-100">
          <label class="block text-sm font-medium text-gray-700 mb-3">图片</label>
          
          <!-- 图片预览 -->
          <div v-if="images.length > 0" class="grid grid-cols-3 gap-3 mb-3">
            <div
              v-for="(img, idx) in images"
              :key="idx"
              class="relative group aspect-square"
            >
              <img
                :src="img.preview"
                alt=""
                class="w-full h-full object-cover rounded-lg bg-gray-100 shadow-sm"
              />
              <button
                @click="removeImage(idx)"
                class="absolute top-2 right-2 bg-black/70 text-white rounded-full w-7 h-7 flex items-center justify-center opacity-0 group-hover:opacity-100 transition-all hover:bg-black text-sm"
              >
                ×
              </button>
            </div>
          </div>

          <!-- 图片上传按钮 -->
          <label class="btn btn-ghost text-xs cursor-pointer inline-flex items-center gap-2 border-2 border-dashed border-gray-300 hover:border-blue-400 hover:text-blue-600 transition-colors rounded-lg px-4 py-3">
            <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z" />
            </svg>
            {{ images.length > 0 ? '继续添加图片' : '添加图片' }}
            <input
              type="file"
              accept="image/*"
              multiple
              class="hidden"
              @change="handleImageSelect"
            />
          </label>
        </div>

        <!-- 状态提示 -->
        <div v-if="error" class="bg-red-50 border border-red-200 rounded-lg p-3 text-sm text-red-700">
          {{ error }}
        </div>
        <div v-if="success" class="bg-green-50 border border-green-200 rounded-lg p-3 text-sm text-green-700 flex items-center gap-2">
          <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" />
          </svg>
          已保存
        </div>
      </div>
    </main>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import apiClient from '@/utils/api'
import { compressImage } from '@/utils/imageCompress'
import { OfflineQueue } from '@/utils/offlineQueue'

const router = useRouter()

const content = ref('')
const images = ref([])
const tags = ref([])
const tagInput = ref('')
const loading = ref(false)
const error = ref('')
const success = ref(false)

// 从内容中提取标签（#标签 格式）
function extractTags(text) {
  const tagRegex = /#(\S+)/g
  const matches = text.match(tagRegex)
  if (!matches) return []
  return matches.map(match => match.substring(1)).filter(tag => tag.trim() !== '')
}

function addTag() {
  const trimmed = tagInput.value.trim()
  if (trimmed && !tags.value.includes(trimmed)) {
    tags.value.push(trimmed)
    tagInput.value = ''
  }
}

function removeTag(index) {
  tags.value.splice(index, 1)
}

async function handleImageSelect(event) {
  const files = Array.from(event.target.files)
  
  for (const file of files) {
    try {
      const compressed = await compressImage(file, 500)
      const preview = URL.createObjectURL(compressed)
      
      images.value.push({
        file: compressed,
        preview,
      })
    } catch (err) {
      console.error('Image compression failed:', err)
      error.value = '图片处理失败'
    }
  }
}

function removeImage(index) {
  URL.revokeObjectURL(images.value[index].preview)
  images.value.splice(index, 1)
}

async function handleSubmit() {
  if (!content.value.trim()) {
    return
  }

  error.value = ''
  success.value = false
  loading.value = true

  try {
    if (!navigator.onLine) {
      OfflineQueue.add({
        content: content.value,
        images: images.value.map(img => img.file),
        source: 'mobile',
      })
      success.value = true
      setTimeout(() => {
        router.push('/')
      }, 1500)
      return
    }

    const imageUrls = []
    for (const img of images.value) {
      try {
        const formData = new FormData()
        formData.append('image', img.file)
        
        const uploadRes = await apiClient.post('/api/v1/upload', formData, {
          headers: {
            'Content-Type': 'multipart/form-data',
          },
        })

        if (uploadRes.data.success === 1) {
          imageUrls.push(uploadRes.data.file.url)
        }
      } catch (err) {
        console.error('Image upload failed:', err)
      }
    }

    // 合并手动添加的标签和从内容中提取的标签
    const extractedTags = extractTags(content.value)
    const allTags = [...new Set([...tags.value, ...extractedTags])]

    await apiClient.post('/api/v1/memos', {
      content: content.value,
      images: imageUrls,
      tags: allTags,
      source: 'mobile',
    })

    success.value = true
    setTimeout(() => {
      router.push('/')
    }, 1000)
  } catch (err) {
    error.value = err.response?.data?.error || '保存失败，已保存到本地'
    OfflineQueue.add({
      content: content.value,
      images: images.value.map(img => img.file),
      source: 'mobile',
    })
  } finally {
    loading.value = false
  }
}
</script>
