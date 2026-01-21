<template>
  <div class="min-h-screen bg-white">
    <!-- 顶部工具栏 - 极简风格 -->
    <div class="sticky top-0 z-10 bg-white/80 backdrop-blur-sm border-b border-gray-200">
      <div class="max-w-4xl mx-auto px-6">
        <div class="flex items-center justify-between h-14">
          <router-link
            to="/"
            class="text-sm text-gray-500 hover:text-gray-900 transition-colors"
          >
            ← 返回
          </router-link>
          
          <div class="flex items-center gap-2">
            <!-- 只读模式：显示编辑和删除按钮 -->
            <template v-if="isReadOnly && !isEditing">
              <button
                @click="enterEditMode"
                class="btn btn-ghost text-xs"
              >
                编辑
              </button>
              <button
                v-if="pageId && authStore.isAuthenticated"
                @click="handleDelete"
                :disabled="deleting"
                class="btn btn-ghost text-xs text-red-600 hover:text-red-700"
              >
                {{ deleting ? '删除中...' : '删除' }}
              </button>
            </template>
            
            <!-- 编辑模式：显示保存和取消按钮 -->
            <template v-else-if="isEditing || isNewPage">
              <button
                v-if="pageId && pageData"
                @click="handleShare"
                class="btn btn-ghost text-xs"
              >
                {{ pageData?.is_shared ? '取消分享' : '分享' }}
              </button>
              <button
                v-if="!isNewPage"
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
    </div>

    <!-- 主内容区 - Notion 风格 -->
    <main class="max-w-4xl mx-auto px-6 py-12">
      <!-- 主内容 -->
      <div class="w-full">
      <!-- 标题区域 - Notion 风格的无缝输入 -->
      <div class="mb-6">
        <input
          v-model="title"
          :readonly="isReadOnly && !isEditing"
          type="text"
          placeholder="无标题"
          class="notion-title w-full"
          @blur="autoSave"
        />
        <!-- 标签输入（编辑模式） -->
        <div v-if="(isEditing || isNewPage) && authStore.isAuthenticated" class="mt-3">
          <div class="flex flex-wrap items-center gap-2">
            <span
              v-for="(tag, index) in tags"
              :key="index"
              class="inline-flex items-center gap-1 px-2 py-1 bg-blue-100 text-blue-700 rounded text-sm"
            >
              {{ tag }}
              <button
                @click="removeTag(index)"
                class="text-blue-500 hover:text-blue-700"
              >
                ×
              </button>
            </span>
            <input
              v-model="tagInput"
              @keyup.enter="addTag"
              @blur="addTag"
              type="text"
              placeholder="添加标签..."
              class="px-2 py-1 border border-gray-300 rounded text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 min-w-[120px]"
            />
          </div>
        </div>
        <!-- 标签显示（只读模式） -->
        <div v-else-if="pageTags && pageTags.length > 0" class="mt-3">
          <div class="flex flex-wrap items-center gap-2">
            <router-link
              v-for="(tag, index) in pageTags"
              :key="index"
              :to="`/tags/${tag}/timeline`"
              class="inline-flex items-center px-2 py-1 bg-blue-100 text-blue-700 rounded text-sm hover:bg-blue-200"
            >
              #{{ tag }}
            </router-link>
          </div>
        </div>
        <!-- 日期显示 -->
        <div class="mt-2 text-sm text-gray-400 font-mono">
          {{ currentDate }}
        </div>
      </div>

      <!-- Editor.js 编辑器 - 与标题对齐 -->
      <div class="min-h-[400px]">
        <div v-if="loading" class="flex items-center justify-center py-20">
          <div class="text-sm text-gray-400 font-mono">加载中...</div>
        </div>
        <Editor
          v-else-if="editorData"
          :key="`editor-${pageId || 'new'}-${isEditing}`"
          ref="editorRef"
          :initial-data="editorData"
          :read-only="isReadOnly && !isEditing"
          @change="handleEditorChange"
        />
        <div v-else class="flex items-center justify-center py-20">
          <div class="text-sm text-gray-400 font-mono">初始化中...</div>
        </div>
      </div>

      <!-- 状态提示 -->
      <div v-if="error" class="mt-4 text-xs text-red-600 font-mono">
        {{ error }}
      </div>
      <div v-if="success" class="mt-4 text-xs text-green-600 font-mono">
        已保存
      </div>
      
      <!-- 访客提示 -->
      <div v-if="isReadOnly && !pageData" class="mt-8 text-center text-gray-400 text-sm">
        正在加载页面...
      </div>
      </div>
    </main>
  </div>
</template>

<script setup>
import { ref, onMounted, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import Editor from '@/components/Editor.vue'
import apiClient from '@/utils/api'

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()

const pageId = ref(route.params.id)
// 默认只读模式，除非是新页面
// PageShare 路由：分享页面，只读（但 PageShare 路由使用 PageShare.vue 组件，不会到这里）
// PageEdit 路由且有 pageId：详情页面，默认只读，可点击编辑进入编辑模式
// PageNew 路由：新页面，直接编辑模式
const isReadOnly = ref(route.name === 'PageEdit' && pageId.value)
const isEditing = ref(false) // 编辑模式标志
const title = ref('')
const tags = ref([]) // 编辑中的标签列表
const tagInput = ref('') // 标签输入框
const pageTags = ref([]) // 页面标签（只读模式显示）
const editorData = ref(null)
const pageData = ref(null)
const saving = ref(false)
const deleting = ref(false)
const loading = ref(false)
const error = ref('')
const success = ref(false)
const editorRef = ref(null)
const autoSaveTimer = ref(null)

const isNewPage = computed(() => route.name === 'PageNew' || !pageId.value)

// 当前日期格式化
const currentDate = computed(() => {
  const date = pageData.value?.created_at 
    ? new Date(pageData.value.created_at)
    : new Date()
  
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  const weekdays = ['日', '一', '二', '三', '四', '五', '六']
  const weekday = weekdays[date.getDay()]
  
  return `${year}年${month}月${day}日 星期${weekday}`
})

async function loadPage() {
  try {
    loading.value = true
    const response = await apiClient.get(`/api/v1/pages/${pageId.value}`)
    pageData.value = response.data
    title.value = pageData.value.title || ''
    
    // 加载标签
    if (pageData.value.tags) {
      tags.value = [...pageData.value.tags]
      pageTags.value = [...pageData.value.tags]
    } else {
      tags.value = []
      pageTags.value = []
    }
    
    // 确保 blocks 数据格式正确，并深度克隆以移除 Proxy 对象
    const blocks = pageData.value.blocks || []
    // 使用 JSON 序列化/反序列化来清理数据，移除不可序列化的对象
    // 但要确保数据不为空
    let cleanBlocks = []
    if (blocks && blocks.length > 0) {
      try {
        cleanBlocks = JSON.parse(JSON.stringify(blocks))
      } catch (err) {
        console.warn('Failed to clean blocks, using original:', err)
        cleanBlocks = blocks
      }
    }
    editorData.value = {
      blocks: cleanBlocks,
      time: new Date(pageData.value.created_at).getTime(),
      version: '2.0',
    }
    
    console.log('Loaded page data:', {
      pageId: pageId.value,
      blocksCount: blocks.length,
      blocks: blocks,
    })
  } catch (err) {
    error.value = '加载页面失败'
    console.error('Load page error:', err)
  } finally {
    loading.value = false
  }
}

async function loadSharedPage(shareId) {
  try {
    loading.value = true
    const response = await apiClient.get(`/api/v1/public/pages/${shareId}`)
    pageData.value = response.data
    title.value = pageData.value.title || ''
    
    const blocks = pageData.value.blocks || []
    // 使用 JSON 序列化/反序列化来清理数据，移除不可序列化的对象
    // 但要确保数据不为空
    let cleanBlocks = []
    if (blocks && blocks.length > 0) {
      try {
        cleanBlocks = JSON.parse(JSON.stringify(blocks))
      } catch (err) {
        console.warn('Failed to clean blocks, using original:', err)
        cleanBlocks = blocks
      }
    }
    editorData.value = {
      blocks: cleanBlocks,
      time: new Date(pageData.value.created_at).getTime(),
      version: '2.0',
    }
    
    console.log('Loaded shared page data:', {
      shareId,
      blocksCount: cleanBlocks.length,
      blocks: cleanBlocks,
    })
  } catch (err) {
    error.value = '加载页面失败'
    console.error('Load shared page error:', err)
  } finally {
    loading.value = false
  }
}

function enterEditMode() {
  isEditing.value = true
  isReadOnly.value = false
  // 同步标签
  if (pageData.value && pageData.value.tags) {
    tags.value = [...pageData.value.tags]
  }
}

async function exitEditMode() {
  isEditing.value = false
  isReadOnly.value = true
  // 重新加载页面数据，恢复原始内容
  if (pageId.value) {
    await loadPage()
  } else {
    // 如果是新页面，清空内容
    title.value = ''
    editorData.value = {
      blocks: [],
      time: Date.now(),
      version: '2.0',
    }
  }
}

function handleEditorChange(data) {
  // 自动保存（防抖）- 只在编辑模式下
  if (autoSaveTimer.value) {
    clearTimeout(autoSaveTimer.value)
  }
  autoSaveTimer.value = setTimeout(() => {
    if ((!isReadOnly.value || isEditing.value) && !isNewPage.value) {
      autoSave()
    }
  }, 2000) // 2秒后自动保存
}

async function autoSave() {
  if (!editorRef.value || saving.value || (isReadOnly.value && !isEditing.value)) {
    return
  }

  try {
    const outputData = await editorRef.value.save()
    
    // 确保 outputData 格式正确
    if (!outputData) {
      console.error('Editor save returned null/undefined:', outputData)
      error.value = '保存失败：编辑器数据为空'
      return
    }
    
    if (!outputData.blocks || !Array.isArray(outputData.blocks)) {
      console.error('Invalid editor output data - blocks missing or not array:', outputData)
      error.value = '保存失败：编辑器数据格式错误'
      return
    }
    
    console.log('Saving page with blocks:', {
      blocksCount: outputData.blocks.length,
      blocks: outputData.blocks,
      fullOutput: outputData,
    })
    
    // 确保发送的数据格式正确
    const requestData = {
      id: pageId.value || undefined,
      title: title.value || '无标题',
      cover: '',
      tags: tags.value,
      blocks: {
        time: outputData.time || Date.now(),
        version: outputData.version || '2.0',
        blocks: outputData.blocks,
      },
    }
    
    console.log('Request data:', requestData)
    
    await apiClient.post('/api/v1/pages', requestData)

    if (isNewPage.value && !pageId.value) {
      // 新页面保存后更新路由
      const response = await apiClient.get('/api/v1/timeline', { params: { limit: 1 } })
      if (response.data.items.length > 0 && response.data.items[0].type === 'page') {
        pageId.value = response.data.items[0].id
        router.replace(`/pages/${pageId.value}`)
      }
    }
    
    // 自动保存不应该退出编辑模式，只保存内容
    // 只有手动保存（handleSave）才会退出编辑模式
    
    success.value = true
    setTimeout(() => {
      success.value = false
    }, 2000)
  } catch (err) {
    console.error('Auto save failed:', err)
  }
}

async function handleSave() {
  if (!editorRef.value) {
    return
  }

  error.value = ''
  success.value = false
  saving.value = true

  try {
    const outputData = await editorRef.value.save()
    
    // 确保 outputData 格式正确
    if (!outputData) {
      console.error('Editor save returned null/undefined:', outputData)
      error.value = '保存失败：编辑器数据为空'
      return
    }
    
    if (!outputData.blocks || !Array.isArray(outputData.blocks)) {
      console.error('Invalid editor output data - blocks missing or not array:', outputData)
      error.value = '保存失败：编辑器数据格式错误'
      return
    }
    
    console.log('Saving page with blocks:', {
      blocksCount: outputData.blocks.length,
      blocks: outputData.blocks,
      fullOutput: outputData,
    })
    
    // 确保发送的数据格式正确
    const requestData = {
      id: pageId.value || undefined,
      title: title.value || '无标题',
      cover: '',
      tags: tags.value,
      blocks: {
        time: outputData.time || Date.now(),
        version: outputData.version || '2.0',
        blocks: outputData.blocks,
      },
    }
    
    console.log('Request data:', requestData)

    const response = await apiClient.post('/api/v1/pages', requestData)

    if (response.data) {
      pageId.value = response.data.id
      success.value = true
      
      if (isNewPage.value) {
        router.replace(`/pages/${pageId.value}`)
      }
      
      // 保存成功后退出编辑模式
      if (isEditing.value) {
        isEditing.value = false
        isReadOnly.value = true
        // 重新加载页面数据
        if (pageId.value) {
          await loadPage()
        }
      }
      
      setTimeout(() => {
        success.value = false
      }, 2000)
    }
  } catch (err) {
    error.value = err.response?.data?.error || '保存失败'
  } finally {
    saving.value = false
  }
}

async function handleShare() {
  if (!pageId.value) {
    error.value = '请先保存页面'
    return
  }

  try {
    const isShared = pageData.value?.is_shared || false
    const response = await apiClient.post(`/api/v1/pages/${pageId.value}/share`, {
      enable: !isShared,
    })

    if (response.data) {
      pageData.value.is_shared = !isShared
      pageData.value.share_id = response.data.share_id || ''
      
      if (!isShared && response.data.share_id) {
        const shareUrl = `${window.location.origin}/s/${response.data.share_id}`
        navigator.clipboard.writeText(shareUrl)
        alert(`分享链接已复制：${shareUrl}`)
      }
    }
  } catch (err) {
    error.value = err.response?.data?.error || '操作失败'
  }
}

function addTag() {
  const tag = tagInput.value.trim()
  if (tag && !tags.value.includes(tag)) {
    tags.value.push(tag)
    tagInput.value = ''
  }
}

function removeTag(index) {
  tags.value.splice(index, 1)
}

async function handleDelete() {
  if (!confirm('确定要删除这个 Page 吗？删除后可以在回收站中恢复。')) {
    return
  }

  deleting.value = true
  error.value = ''

  try {
    // 调用删除 API（软删除）
    await apiClient.delete(`/api/v1/pages/${pageId.value}`)
    
    // 删除成功后跳转到首页
    router.push('/')
  } catch (err) {
    console.error('删除 Page 失败:', err)
    error.value = err.response?.data?.error || '删除失败，请稍后重试'
  } finally {
    deleting.value = false
  }
}

onMounted(async () => {
  // 判断是否是分享页面路由
  if (route.name === 'PageShare' && route.params.share_id) {
    // 分享页面：使用 share_id 加载
    await loadSharedPage(route.params.share_id)
  } else if (pageId.value) {
    // 普通页面详情：使用 pageId 加载
    await loadPage()
  } else {
    // 新页面，初始化空数据
    // 确保 editorData 不为 null，这样 Editor 组件才能正常渲染
    editorData.value = {
      blocks: [],
      time: Date.now(),
      version: '2.0',
    }
  }
})
</script>
