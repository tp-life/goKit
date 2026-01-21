<template>
  <div>
    <div id="editorjs" class="prose max-w-none"></div>
  </div>
</template>

<script setup>
import { ref, onMounted, onBeforeUnmount, watch } from 'vue'
import EditorJS from '@editorjs/editorjs'
import Header from '@editorjs/header'
import List from '@editorjs/list'
import Image from '@editorjs/image'
import Paragraph from '@editorjs/paragraph'
import CodeTool from './CodeTool.js'
import MermaidTool from './MermaidTool.js'
import Quote from '@editorjs/quote'
import Table from '@editorjs/table'
import LinkTool from '@editorjs/link'
import Delimiter from '@editorjs/delimiter'
import Marker from '@editorjs/marker'
import InlineCode from '@editorjs/inline-code'
import apiClient from '@/utils/api'

const props = defineProps({
  initialData: {
    type: Object,
    default: null,
  },
  readOnly: {
    type: Boolean,
    default: false,
  },
})

const emit = defineEmits(['change', 'ready'])

const editor = ref(null)
const isInitialized = ref(false)

onMounted(() => {
  // 等待 initialData 准备好后再初始化
  // 如果 initialData 已经存在，立即初始化
  if (props.initialData && !isInitialized.value) {
    initEditor()
  }
})

// 监听 initialData，当数据从 null 变为有值时初始化编辑器
// 只初始化一次，避免重复创建
watch(() => props.initialData, (newData) => {
  if (newData && !editor.value && !isInitialized.value) {
    // 数据准备好了，但编辑器还没初始化
    initEditor()
  }
}, { immediate: true })

onBeforeUnmount(() => {
  if (editor.value) {
    editor.value.destroy()
    editor.value = null
    isInitialized.value = false
  }
})

watch(() => props.readOnly, (newVal) => {
  if (editor.value) {
    editor.value.readOnly.toggle(newVal)
    // 切换到只读模式后，触发代码高亮
    if (newVal) {
      setTimeout(() => {
        triggerCodeHighlight()
      }, 500)
    }
  }
})

// 监听 initialData 变化，如果编辑器已初始化且数据变化，需要重新渲染
// 注意：只在编辑器已初始化且数据真正变化时才重新渲染，避免重复初始化
watch(() => props.initialData, (newData, oldData) => {
  // 如果编辑器还没初始化，不处理（由 initEditor 处理）
  if (!editor.value || !isInitialized.value) {
    return
  }
  
  // 如果数据没有变化，不处理
  if (!newData || newData === oldData) {
    return
  }
  
  // 如果 oldData 为 null，说明是首次加载，已经在 initEditor 中处理了
  if (oldData === null) {
    return
  }
  
  // Editor.js 的 render 方法可以重新渲染数据
  // 但需要确保数据格式正确
  if (newData && newData.blocks && Array.isArray(newData.blocks)) {
    // 确保数据格式正确，并深度克隆以移除 Proxy 对象
    let cleanData
    try {
      // 使用 JSON 序列化/反序列化来清理数据
      cleanData = JSON.parse(JSON.stringify({
        blocks: newData.blocks,
        time: newData.time || Date.now(),
        version: newData.version || '2.0',
      }))
    } catch (err) {
      console.error('Failed to clean editor data for render:', err)
      cleanData = {
        blocks: [],
        time: Date.now(),
        version: '2.0',
      }
    }
    
    // 使用 render 方法重新渲染编辑器内容
    editor.value.render(cleanData).then(() => {
      // 渲染完成后，如果是只读模式，触发代码高亮
      if (props.readOnly) {
        setTimeout(() => {
          triggerCodeHighlight()
        }, 500)
      }
    }).catch(err => {
      console.error('Editor render error:', err)
      console.warn('Editor render failed, data:', cleanData)
      // 如果 render 失败，尝试清空后重新渲染
      editor.value.clear().then(() => {
        editor.value.render(cleanData).then(() => {
          // 渲染完成后，如果是只读模式，触发代码高亮
          if (props.readOnly) {
            setTimeout(() => {
              triggerCodeHighlight()
            }, 500)
          }
        }).catch(renderErr => {
          console.error('Editor re-render error:', renderErr)
        })
      })
    })
  }
}, { deep: true, immediate: false })

function initEditor() {
  // 防止重复初始化
  if (isInitialized.value || editor.value) {
    console.warn('Editor already initialized, skipping...')
    return
  }
  
  const accessToken = localStorage.getItem('access_token')
  
  // 确保数据格式正确
  let editorData = props.initialData
  if (!editorData) {
    editorData = {
      blocks: [],
      time: Date.now(),
      version: '2.0',
    }
  }
  
  // 确保 blocks 是数组
  if (!editorData.blocks || !Array.isArray(editorData.blocks)) {
    editorData.blocks = []
  }
  
  // 深度克隆数据，移除 Proxy 对象和不可序列化的内容
  // 使用 JSON 序列化/反序列化来确保数据是纯 JSON 格式
  try {
    editorData = JSON.parse(JSON.stringify(editorData))
  } catch (err) {
    console.warn('Failed to clean editor data:', err)
    // 如果清理失败，使用空数据
    editorData = {
      blocks: [],
      time: Date.now(),
      version: '2.0',
    }
  }
  
  // 标记为已初始化
  isInitialized.value = true
  
  editor.value = new EditorJS({
    holder: 'editorjs',
    readOnly: props.readOnly,
    data: editorData,
    placeholder: '输入 "/" 插入内容块，或直接开始写作...',
    onReady: () => {
      // Editor.js 渲染完成后，触发代码高亮
      if (props.readOnly) {
        // 延迟触发，确保所有块都已渲染
        setTimeout(() => {
          triggerCodeHighlight()
        }, 500)
      }
      emit('ready')
    },
    tools: {
      header: {
        class: Header,
        config: {
          levels: [1, 2, 3, 4],
          defaultLevel: 2,
        },
      },
      paragraph: {
        class: Paragraph,
        inlineToolbar: true,
      },
      list: {
        class: List,
        inlineToolbar: true,
        config: {
          defaultStyle: 'unordered',
        },
      },
      code: {
        class: CodeTool,
        config: {
          placeholder: '输入代码...',
          
        },
      },
      mermaid: {
        class: MermaidTool,
      },
      quote: {
        class: Quote,
        inlineToolbar: true,
        config: {
          quotePlaceholder: '引用内容',
          captionPlaceholder: '引用来源（可选）',
        },
      },
      table: {
        class: Table,
        inlineToolbar: true,
        config: {
          rows: 2,
          cols: 2,
        },
      },
      linkTool: {
        class: LinkTool,
        config: {
          endpoint: 'http://localhost:8080/api/v1/link-preview', // 可选：链接预览接口
        },
      },
      delimiter: {
        class: Delimiter,
      },
      marker: {
        class: Marker,
      },
      inlineCode: {
        class: InlineCode,
      },
      image: {
        class: Image,
        config: {
          endpoints: {
            byFile: 'http://localhost:8080/api/v1/upload',
          },
          field: 'image',
          types: 'image/*',
          additionalRequestHeaders: {
            Authorization: accessToken ? `Bearer ${accessToken}` : '',
          },
          captionPlaceholder: '图片说明（可选）',
          buttonContent: '上传图片',
          uploader: {
            async uploadByFile(file) {
              const formData = new FormData()
              formData.append('image', file)

              try {
                const response = await apiClient.post('/api/v1/upload', formData, {
                  headers: {
                    'Content-Type': 'multipart/form-data',
                  },
                })

                if (response.data.success === 1) {
                  return {
                    success: 1,
                    file: {
                      url: response.data.file.url,
                    },
                  }
                } else {
                  throw new Error('上传失败')
                }
              } catch (error) {
                console.error('图片上传失败:', error)
                throw error
              }
            },
          },
        },
      },
    },
    onChange: async () => {
      try {
        const outputData = await editor.value.save()
        emit('change', outputData)
      } catch (error) {
        console.error('Editor save error:', error)
      }
    },
  })
}

// 触发所有代码块的语法高亮（只读模式）
// 触发所有代码块的语法高亮（只读模式）
// 注意：CodeTool 已经在 render() 时创建了正确的 pre/code 结构，这里只需要触发高亮
function triggerCodeHighlight() {
  if (typeof window === 'undefined' || !window.Prism) {
    console.warn('Prism.js not loaded, retrying code highlight...')
    setTimeout(() => triggerCodeHighlight(), 500)
    return
  }

  // 查找所有代码块容器
  const codeWrappers = document.querySelectorAll('.ce-code-wrapper')
  
  codeWrappers.forEach((wrapper) => {
    // 查找 pre/code 元素（CodeTool 已经在 render 时创建了）
    const pre = wrapper.querySelector('pre')
    const code = pre ? pre.querySelector('code') : null

    if (!pre || !code) {
      // 如果没找到，CodeTool 可能还没渲染完成，跳过
      return
    }

    // 从 wrapper 的 data-language 属性获取语言
    const languageId = wrapper.getAttribute('data-language') || 'javascript'

    // 检查语言是否已加载
    let language = window.Prism.languages[languageId]
    let targetLanguageId = languageId

    // 如果语言未加载，尝试使用备用语言
    if (!language || typeof language !== 'object') {
      if (window.Prism.languages.javascript && typeof window.Prism.languages.javascript === 'object') {
        language = window.Prism.languages.javascript
        targetLanguageId = 'javascript'
      } else if (window.Prism.languages.markup && typeof window.Prism.languages.markup === 'object') {
        language = window.Prism.languages.markup
        targetLanguageId = 'markup'
      } else {
        code.className = `language-${languageId}`
        return
      }
    }

    code.className = `language-${targetLanguageId}`

    // 使用 highlightElement 方法
    if (window.Prism.highlightElement) {
      try {
        window.Prism.highlightElement(code)
      } catch (err) {
        console.warn('triggerCodeHighlight: Prism highlightElement error:', err)
        // 如果 highlightElement 失败，尝试手动高亮
        try {
          const codeText = code.textContent || ''
          const highlighted = window.Prism.highlight(codeText, language, targetLanguageId)
          code.innerHTML = highlighted
        } catch (highlightErr) {
          console.warn('triggerCodeHighlight: Prism manual highlight error:', highlightErr)
        }
      }
    } else if (window.Prism.highlight) {
      try {
        const codeText = code.textContent || ''
        const highlighted = window.Prism.highlight(codeText, language, targetLanguageId)
        code.innerHTML = highlighted
      } catch (err) {
        console.warn('triggerCodeHighlight: Prism highlight error:', err)
      }
    }
  })
}

defineExpose({
  async save() {
    if (editor.value) {
      return await editor.value.save()
    }
    return null
  },
  async clear() {
    if (editor.value) {
      await editor.value.clear()
    }
  },
})
</script>

<style>
/* Notion 风格的编辑器样式 */
.codex-editor {
  min-height: 400px;
}

.codex-editor__redactor {
  padding-bottom: 200px !important;
  padding-left: 0 !important;
  padding-right: 0 !important;
}

/* 块样式优化 - 与标题对齐 */
.ce-block {
  @apply px-0 py-1;
  margin-left: 0 !important;
  margin-right: 0 !important;
}

.ce-block__content {
  @apply max-w-full;
  margin-left: 0 !important;
  margin-right: 0 !important;
  padding-left: 0 !important;
  padding-right: 0 !important;
}

.ce-paragraph {
  @apply text-base leading-7 text-gray-900;
  @apply focus:outline-none;
}

.ce-header {
  @apply font-bold text-gray-900;
}

.ce-header[data-level="1"] {
  @apply text-3xl mt-6 mb-4;
}

.ce-header[data-level="2"] {
  @apply text-2xl mt-5 mb-3;
}

.ce-header[data-level="3"] {
  @apply text-xl mt-4 mb-2;
}

/* 列表样式 */
.ce-list {
  @apply my-2;
}

.ce-list__item {
  @apply text-base leading-7 text-gray-900;
}

/* 代码块样式 */
.ce-code-wrapper {
  @apply my-4;
}

.ce-code {
  @apply my-2;
}

.ce-code__select-wrapper {
  @apply mb-2 flex items-center gap-2;
}

.ce-code__select-wrapper label {
  @apply text-xs text-gray-600 font-mono;
}

.ce-code__select {
  @apply text-xs text-gray-600 bg-gray-100 border border-gray-300 rounded px-2 py-1;
  @apply font-mono cursor-pointer;
}

.ce-code__select:focus {
  @apply outline-none border-gray-400 ring-1 ring-gray-300;
}

.ce-code__textarea {
  @apply font-mono text-sm bg-gray-50 border border-gray-200 rounded p-4;
  @apply text-gray-900;
  @apply min-h-[200px];
}

/* 代码编辑器容器（编辑模式：叠加层方案） */
.ce-code__editor-container {
  position: relative;
  border: 1px solid #e5e7eb;
  border-radius: 0.375rem;
  overflow: hidden;
  background-color: #111827;
  min-height: 200px;
  /* 确保 z-index 低于 Editor.js 工具栏（通常为 10+） */
  z-index: 0;
  isolation: isolate; /* 创建新的层叠上下文 */
}

/* 语法高亮层（底层） */
.ce-code__highlight-layer {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  margin: 0;
  padding: 1rem;
  overflow: auto;
  background: #111827 !important;
  color: #f3f4f6;
  pointer-events: none;
  white-space: pre;
  word-wrap: break-word;
  font-family: 'Monaco', 'Menlo', 'Ubuntu Mono', 'Consolas', 'source-code-pro', monospace;
  font-size: 14px;
  line-height: 1.5;
  tab-size: 2;
  box-sizing: border-box;
  z-index: 1;
}

.ce-code__highlight-layer code {
  font-size: 14px !important;
  line-height: 1.5 !important;
  background: transparent !important;
  padding: 0 !important;
  margin: 0 !important;
  display: block;
  white-space: pre;
  font-family: inherit !important;
  /* 不设置 color，让 Prism.js 的 token 颜色生效 */
}

/* 确保 Prism.js 的高亮样式生效 - 不要覆盖 token 颜色 */
.ce-code__highlight-layer code[class*="language-"] {
  /* 移除 color: inherit，让 Prism 的 token 颜色显示 */
}

/* Prism.js 的 token 元素应该使用自己的颜色 */
.ce-code__highlight-layer code .token,
.ce-code__highlight-layer code span[class*="token"] {
  /* 让 Prism 的 token 颜色生效，不覆盖 */
}

/* 文本输入层（顶层，透明） */
.ce-code__textarea-overlay {
  position: relative;
  z-index: 2;
  width: 100%;
  min-height: 200px;
  margin: 0;
  padding: 1rem;
  background: transparent !important;
  color: transparent !important;
  caret-color: #ffffff !important;
  border: none !important;
  resize: vertical;
  outline: none !important;
  overflow: auto;
  white-space: pre;
  word-wrap: break-word;
  font-family: 'Monaco', 'Menlo', 'Ubuntu Mono', 'Consolas', 'source-code-pro', monospace !important;
  font-size: 14px !important;
  line-height: 1.5 !important;
  tab-size: 2;
  box-sizing: border-box;
  scrollbar-width: thin;
  scrollbar-color: rgba(255, 255, 255, 0.3) transparent;
  /* 确保不会覆盖 Editor.js 工具栏 */
  isolation: isolate;
}

/* 确保 Editor.js 工具栏和弹出菜单始终在最上层 */
.codex-editor__toolbar,
.ce-popover,
.ce-popover__items {
  z-index: 100 !important;
  position: relative;
}

/* 选中文字时的样式 - 确保文字可见 */
.ce-code__textarea-overlay::selection {
  background-color: rgba(59, 130, 246, 0.5) !important; /* 蓝色半透明背景 */
  color: #ffffff !important; /* 白色文字 */
}

.ce-code__textarea-overlay::-moz-selection {
  background-color: rgba(59, 130, 246, 0.5) !important; /* Firefox */
  color: #ffffff !important;
}

.ce-code__textarea-overlay::-webkit-scrollbar,
.ce-code__highlight-layer::-webkit-scrollbar {
  width: 8px;
  height: 8px;
}

.ce-code__textarea-overlay::-webkit-scrollbar-track,
.ce-code__highlight-layer::-webkit-scrollbar-track {
  background: transparent;
}

.ce-code__textarea-overlay::-webkit-scrollbar-thumb,
.ce-code__highlight-layer::-webkit-scrollbar-thumb {
  background-color: rgba(255, 255, 255, 0.3);
  border-radius: 4px;
}

/* 代码块渲染时的语法高亮样式（只读模式） */
.ce-code pre {
  @apply font-mono text-sm bg-gray-900 rounded p-4 overflow-x-auto;
  @apply border border-gray-700;
  @apply my-2;
  /* 设置默认文字颜色，但让 Prism.js 的 token 颜色覆盖 */
  color: #f3f4f6;
}

.ce-code pre code {
  @apply text-sm;
  background: transparent !important;
  padding: 0 !important;
  /* 不设置 color，让 Prism.js 的 token 颜色生效 */
  color: inherit;
}

/* Prism.js 语法高亮样式 - 不覆盖 token 颜色 */
.ce-code pre code[class*="language-"] {
  background: transparent !important;
  /* 不设置 color，让 Prism.js 的 token 颜色生效 */
}

/* 确保 Prism.js 的 token 颜色可见 */
.ce-code pre code .token,
.ce-code pre code span[class*="token"] {
  /* 让 Prism 的 token 颜色生效，不覆盖 */
}

/* Mermaid 图表样式 */
.ce-mermaid-wrapper {
  @apply my-4;
}

.ce-mermaid__controls {
  @apply mb-2 flex items-center gap-3;
}

.ce-mermaid__theme-wrapper {
  @apply flex items-center;
}

.ce-mermaid__theme-wrapper span {
  @apply text-xs text-gray-600 font-mono;
}

.ce-mermaid__theme-select {
  @apply text-xs text-gray-600 bg-gray-100 border border-gray-300 rounded px-2 py-1;
  @apply font-mono cursor-pointer;
}

.ce-mermaid__theme-select:focus {
  @apply outline-none border-gray-400 ring-1 ring-gray-300;
}

.ce-mermaid__example-btn {
  @apply text-xs px-2 py-1 bg-blue-500 text-white rounded;
  @apply hover:bg-blue-600 transition-colors;
  @apply font-mono;
}

.ce-mermaid__theme-label {
  @apply text-xs text-gray-600 font-mono mb-2;
}

.ce-mermaid__textarea {
  @apply font-mono text-sm bg-gray-50 border border-gray-200 rounded p-4;
  @apply text-gray-900 w-full;
  @apply min-h-[200px] resize-y;
  @apply mb-2;
}

.ce-mermaid__preview {
  @apply border border-gray-200 rounded p-4 bg-white;
  @apply min-h-[200px] flex items-center justify-center;
  @apply overflow-x-auto;
}

.ce-mermaid__preview svg {
  @apply max-w-full h-auto;
}

.ce-mermaid__empty {
  @apply text-gray-400 text-sm text-center;
}

.ce-mermaid__error {
  @apply text-red-500 text-sm text-center p-2;
  @apply bg-red-50 border border-red-200 rounded;
  @apply mt-2;
}

.ce-mermaid__loading {
  @apply text-gray-400 text-sm text-center;
}

/* 引用样式 */
.ce-quote {
  @apply my-4 pl-4 border-l-4 border-gray-300;
}

.ce-quote__text {
  @apply text-base leading-7 text-gray-700 italic;
}

.ce-quote__caption {
  @apply text-sm text-gray-500 mt-2;
}

/* 表格样式 */
.ce-table {
  @apply my-4 overflow-x-auto;
}

.ce-table__cell {
  @apply border border-gray-200 p-2;
}

/* 分隔线样式 */
.ce-delimiter {
  @apply my-6 text-center;
}

.ce-delimiter::before {
  content: '***';
  @apply text-2xl text-gray-400;
}

/* 标记样式 */
.ce-inline-tool--marker {
  @apply bg-yellow-200;
}

/* 工具栏样式 */
.codex-editor__toolbar {
  @apply border-gray-200;
}

.ce-toolbar__content {
  @apply max-w-4xl;
}

.ce-toolbar__plus {
  @apply text-gray-400 hover:text-gray-900;
}

.ce-toolbar__settings-btn {
  @apply text-gray-400 hover:text-gray-900;
}

/* 内联工具栏 */
.ce-inline-toolbar {
  @apply bg-white border border-gray-200 shadow-lg;
}

.ce-inline-toolbar__toggler {
  @apply text-gray-400 hover:text-gray-900;
}

/* 只读模式 */
.codex-editor--readonly .ce-toolbar,
.codex-editor--readonly .ce-inline-toolbar {
  @apply hidden;
}
</style>
