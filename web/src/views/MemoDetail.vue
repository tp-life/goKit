<template>
  <div class="min-h-screen bg-gray-50">
    <!-- 导航栏 -->
    <nav class="bg-white border-b border-gray-200">
      <div class="max-w-6xl mx-auto px-6 py-4">
        <div class="flex items-center justify-between">
          <div class="flex items-center gap-4">
            <router-link to="/" class="text-gray-900 font-mono text-sm hover:text-gray-700">
              ← 返回
            </router-link>
            <span class="text-gray-400">|</span>
            <span class="text-sm text-gray-600 font-mono">Memo 详情</span>
          </div>
          
          <div class="flex items-center gap-2">
            <!-- 只读模式：显示编辑和删除按钮 -->
            <template v-if="!isEditing">
              <button
                @click="enterEditMode"
                class="btn btn-ghost text-xs"
              >
                编辑
              </button>
              <button
                @click="handleDelete"
                :disabled="deleting"
                class="btn btn-ghost text-xs text-red-600 hover:text-red-700"
              >
                {{ deleting ? '删除中...' : '删除' }}
              </button>
            </template>
            
            <!-- 编辑模式：显示保存和取消按钮 -->
            <template v-else>
              <button
                @click="exitEditMode"
                class="btn btn-ghost text-xs"
              >
                取消
              </button>
              <button
                @click="handleSave"
                :disabled="saving"
                class="btn btn-primary text-xs"
              >
                {{ saving ? '保存中...' : '保存' }}
              </button>
            </template>
          </div>
        </div>
      </div>
    </nav>

    <!-- 主内容区 -->
    <main class="max-w-3xl mx-auto px-6 py-12">
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

      <!-- Memo 内容 -->
      <div v-else-if="memo" class="bg-white rounded-xl shadow-lg border border-gray-200 overflow-hidden">
        <!-- 头部区域 -->
        <div class="bg-gradient-to-r from-blue-50 to-indigo-50 px-6 py-4 border-b border-gray-200">
          <div class="flex items-center justify-between">
            <div class="text-xs text-gray-500 font-mono">
              {{ formatDate(memo.created_at) }}
            </div>
            <div class="text-xs text-gray-500 font-mono">
              {{ memo.source === 'mobile' ? '📱 移动端' : '💻 网页端' }}
            </div>
          </div>
        </div>

        <!-- 内容区域 -->
        <div class="p-6 space-y-6">
          <!-- 内容文本 -->
          <div class="prose prose-sm max-w-none">
            <!-- 只读模式 -->
            <div
              v-if="!isEditing"
              class="text-gray-900 whitespace-pre-wrap leading-relaxed text-base min-h-[200px]"
              v-html="highlightTags(memo.content)"
            ></div>
            <!-- 编辑模式 -->
            <div v-else class="space-y-4">
              <textarea
                v-model="editContent"
                placeholder="输入 Memo 内容...#标签会自动提取"
                class="w-full min-h-[200px] p-4 border border-gray-300 rounded-lg resize-y focus:outline-none focus:ring-2 focus:ring-blue-500 bg-gray-50"
              ></textarea>
              
              <!-- 标签输入区域 -->
              <div class="border border-gray-200 rounded-lg p-4 bg-gray-50">
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
                    class="flex-1 px-3 py-2 text-sm border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500 bg-white"
                  />
                  <button
                    @click="addTag"
                    class="btn btn-ghost text-xs px-3 py-2"
                  >
                    添加
                  </button>
                </div>

                <!-- 已添加的标签 -->
                <div v-if="editTags.length > 0" class="flex flex-wrap items-center gap-2">
                  <div
                    v-for="(tag, index) in editTags"
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
            </div>
          </div>

        <!-- 图片列表 -->
        <div v-if="(memo.images && memo.images.length > 0) || (isEditing && editImages.length > 0)" class="grid grid-cols-3 gap-3 mt-4">
          <!-- 现有图片（只读模式） -->
          <template v-if="!isEditing">
            <div
              v-for="(image, index) in memo.images"
              :key="index"
              class="relative rounded-lg overflow-hidden bg-gray-100"
            >
              <img
                :src="image"
                :alt="`图片 ${index + 1}`"
                class="w-full h-32 object-cover cursor-pointer rounded-lg hover:opacity-90 transition-opacity shadow-sm"
                @click="previewImage(image)"
              />
            </div>
          </template>
          
          <!-- 编辑模式：显示所有图片（包括新上传的） -->
          <template v-else>
            <div
              v-for="(img, index) in editImages"
              :key="img.id || index"
              class="relative group"
            >
              <img
                :src="img.preview || img.url"
                :alt="`图片 ${index + 1}`"
                class="w-full h-32 object-cover rounded-lg bg-gray-100 hover:opacity-90 transition-opacity shadow-sm cursor-pointer"
                @click="previewImage(img.preview || img.url)"
              />
              <!-- 删除按钮 -->
              <button
                @click="removeImage(index)"
                class="absolute top-2 right-2 bg-black/50 text-white rounded-full w-6 h-6 flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity text-xs"
              >
                ×
              </button>
            </div>
          </template>
        </div>

        <!-- 图片上传按钮（编辑模式） -->
        <div v-if="isEditing" class="mt-4">
          <label class="btn btn-ghost text-xs cursor-pointer inline-flex items-center gap-2 border-2 border-dashed border-gray-300 hover:border-blue-400 hover:text-blue-600 transition-colors rounded-lg px-4 py-3">
            <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z" />
            </svg>
            {{ editImages.length > 0 ? '继续添加图片' : '添加图片' }}
            <input
              type="file"
              accept="image/*"
              multiple
              class="hidden"
              @change="handleImageSelect"
            />
          </label>
        </div>

          <!-- 标签显示（只读模式） -->
          <div v-if="!isEditing && memo.tags && memo.tags.length > 0" class="flex flex-wrap items-center gap-2 pt-4 border-t border-gray-200">
            <router-link
              v-for="(tag, index) in memo.tags"
              :key="index"
              :to="`/tags/${tag}/timeline`"
              class="inline-flex items-center px-3 py-1 bg-blue-100 text-blue-700 rounded-full text-sm hover:bg-blue-200 transition-colors hover:scale-105 transform"
            >
              #{{ tag }}
            </router-link>
          </div>
        </div>
      </div>

      <!-- 错误状态 -->
      <div v-else-if="error" class="card p-6 text-center">
        <p class="text-red-600 text-sm font-mono">{{ error }}</p>
        <router-link to="/" class="mt-4 btn btn-ghost text-sm inline-block">
          返回首页
        </router-link>
      </div>
    </main>

    <!-- 图片预览模态框 -->
    <div
      v-if="previewImageUrl"
      class="fixed inset-0 bg-black bg-opacity-75 z-50 flex items-center justify-center p-4"
      @click="previewImageUrl = null"
    >
      <img
        :src="previewImageUrl"
        alt="预览"
        class="max-w-full max-h-full object-contain"
        @click.stop
      />
      <button
        @click="previewImageUrl = null"
        class="absolute top-4 right-4 text-white hover:text-gray-300"
      >
        <svg class="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"></path>
        </svg>
      </button>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import apiClient from '@/utils/api'
import { compressImage } from '@/utils/imageCompress'

const route = useRoute()
const router = useRouter()

const loading = ref(true)
const error = ref('')
const memo = ref(null)
const previewImageUrl = ref(null)
const isEditing = ref(false) // 编辑模式标志
const editContent = ref('') // 编辑中的内容
const editImages = ref([]) // 编辑中的图片列表（包含现有图片和新上传的）
const editTags = ref([]) // 编辑中的标签列表
const tagInput = ref('') // 标签输入框
const saving = ref(false) // 保存状态
const deleting = ref(false) // 删除状态

// 从内容中提取标签（#标签 格式）
function extractTags(text) {
  const tagRegex = /#(\S+)/g
  const matches = text.match(tagRegex)
  if (!matches) return []
  return matches.map(match => match.substring(1)).filter(tag => tag.trim() !== '')
}

function addTag() {
  const trimmed = tagInput.value.trim()
  if (trimmed && !editTags.value.includes(trimmed)) {
    editTags.value.push(trimmed)
    tagInput.value = ''
  }
}

function removeTag(index) {
  editTags.value.splice(index, 1)
}

function formatDate(dateString) {
  const date = new Date(dateString)
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  const hours = String(date.getHours()).padStart(2, '0')
  const minutes = String(date.getMinutes()).padStart(2, '0')
  const weekdays = ['日', '一', '二', '三', '四', '五', '六']
  const weekday = weekdays[date.getDay()]
  
  return `${year}年${month}月${day}日 星期${weekday} ${hours}:${minutes}`
}

function highlightTags(text) {
  if (!text) return text
  // 将 #标签 转换为可点击的链接
  return text.replace(
    /#(\S+)/g,
    '<a href="/tags/$1/timeline" class="text-blue-600 hover:text-blue-800 hover:underline font-medium">#$1</a>'
  )
}

function previewImage(url) {
  previewImageUrl.value = url
}

async function loadMemo() {
  loading.value = true
  error.value = ''
  
  try {
    const response = await apiClient.get(`/api/v1/memos/${route.params.id}`)
    memo.value = response.data
    editContent.value = memo.value.content // 初始化编辑内容
    editTags.value = [...(memo.value.tags || [])] // 初始化编辑标签
    // 初始化编辑图片列表（将现有图片 URL 转换为编辑格式）
    editImages.value = (memo.value.images || []).map((url, index) => ({
      id: `existing-${index}`,
      url: url,
      preview: url,
      isExisting: true, // 标记为现有图片
    }))
  } catch (err) {
    console.error('加载 Memo 失败:', err)
    if (err.response?.status === 404) {
      error.value = 'Memo 不存在'
    } else {
      error.value = '加载失败，请稍后重试'
    }
  } finally {
    loading.value = false
  }
}

async function handleImageSelect(event) {
  const files = Array.from(event.target.files)
  
  for (const file of files) {
    try {
      const compressed = await compressImage(file, 500)
      const preview = URL.createObjectURL(compressed)
      
      editImages.value.push({
        id: `new-${Date.now()}-${Math.random()}`,
        file: compressed,
        preview,
        isExisting: false, // 标记为新上传的图片
      })
    } catch (err) {
      console.error('Image compression failed:', err)
      error.value = '图片处理失败'
    }
  }
  
  // 清空 input，允许重复选择同一文件
  event.target.value = ''
}

function removeImage(index) {
  const img = editImages.value[index]
  // 如果是新上传的图片，释放预览 URL
  if (img.preview && !img.isExisting) {
    URL.revokeObjectURL(img.preview)
  }
  editImages.value.splice(index, 1)
}

function enterEditMode() {
  isEditing.value = true
  editContent.value = memo.value?.content || ''
  editTags.value = [...(memo.value?.tags || [])]
  // 重新初始化编辑图片列表
  editImages.value = (memo.value?.images || []).map((url, index) => ({
    id: `existing-${index}`,
    url: url,
    preview: url,
    isExisting: true,
  }))
}

function exitEditMode() {
  // 释放新上传图片的预览 URL
  editImages.value.forEach(img => {
    if (img.preview && !img.isExisting) {
      URL.revokeObjectURL(img.preview)
    }
  })
  
  isEditing.value = false
  editContent.value = memo.value?.content || '' // 恢复原始内容
  editTags.value = [...(memo.value?.tags || [])] // 恢复原始标签
  // 恢复原始图片列表
  editImages.value = (memo.value?.images || []).map((url, index) => ({
    id: `existing-${index}`,
    url: url,
    preview: url,
    isExisting: true,
  }))
}

async function handleSave() {
  if (!editContent.value.trim()) {
    error.value = '内容不能为空'
    return
  }

  saving.value = true
  error.value = ''

  try {
    // 收集现有图片的 URL
    const existingImageUrls = editImages.value
      .filter(img => img.isExisting)
      .map(img => img.url)
    
    // 上传新图片
    const newImageUrls = []
    const newImages = editImages.value.filter(img => !img.isExisting)
    
    for (const img of newImages) {
      try {
        const formData = new FormData()
        formData.append('image', img.file)
        
        const uploadRes = await apiClient.post('/api/v1/upload', formData, {
          headers: {
            'Content-Type': 'multipart/form-data',
          },
        })

        if (uploadRes.data.success === 1) {
          newImageUrls.push(uploadRes.data.file.url)
          // 释放预览 URL
          URL.revokeObjectURL(img.preview)
        }
      } catch (err) {
        console.error('Image upload failed:', err)
        // 继续上传其他图片，不中断整个保存流程
      }
    }

    // 合并所有图片 URL（保持顺序：现有图片在前，新图片在后）
    const allImageUrls = [...existingImageUrls, ...newImageUrls]

    // 合并手动添加的标签和从内容中提取的标签
    const extractedTags = extractTags(editContent.value)
    const allTags = [...new Set([...editTags.value, ...extractedTags])]

    // 更新 Memo
    await apiClient.put(`/api/v1/memos/${route.params.id}`, {
      content: editContent.value,
      images: allImageUrls,
      tags: allTags,
    })

    // 更新本地数据
    if (memo.value) {
      memo.value.content = editContent.value
      memo.value.images = allImageUrls
      memo.value.tags = allTags
    }

    // 重新初始化编辑图片列表（转换为现有图片格式）
    editImages.value = allImageUrls.map((url, index) => ({
      id: `existing-${index}`,
      url: url,
      preview: url,
      isExisting: true,
    }))

    // 退出编辑模式
    isEditing.value = false
  } catch (err) {
    console.error('保存 Memo 失败:', err)
    error.value = err.response?.data?.error || '保存失败，请稍后重试'
  } finally {
    saving.value = false
  }
}

async function handleDelete() {
  if (!confirm('确定要删除这个 Memo 吗？删除后可以在回收站中恢复。')) {
    return
  }

  deleting.value = true
  error.value = ''

  try {
    // 调用删除 API（软删除）
    await apiClient.delete(`/api/v1/memos/${route.params.id}`)
    
    // 删除成功后跳转到首页
    router.push('/')
  } catch (err) {
    console.error('删除 Memo 失败:', err)
    error.value = err.response?.data?.error || '删除失败，请稍后重试'
  } finally {
    deleting.value = false
  }
}

onMounted(() => {
  loadMemo()
})
</script>
