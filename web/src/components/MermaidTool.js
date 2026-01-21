/**
 * Mermaid 图表工具 - 支持流程图、序列图、甘特图等
 * 基于 Editor.js Tool API
 */
export default class MermaidTool {
  static get toolbox() {
    return {
      title: '流程图/图表',
      icon: '<svg width="20" height="20" viewBox="0 0 20 20" xmlns="http://www.w3.org/2000/svg"><path d="M3 3h14v14H3V3zm1 1v12h12V4H4zm2 2h8v1H6V6zm0 2h8v1H6V8zm0 2h5v1H6v-1z"/></svg>'
    }
  }

  static get isReadOnlySupported() {
    return true
  }

  constructor({ data, api, readOnly }) {
    this.api = api
    this.readOnly = readOnly

    // 解码 HTML 实体（如果数据被转义了）
    // 例如：&gt; 转换为 >, &lt; 转换为 <, &amp; 转换为 &, &quot; 转换为 "
    let code = data?.code || ''
    if (code && typeof code === 'string') {
      // 始终解码 HTML 实体，因为可能从数据库返回时被转义了
      code = this.decodeHtmlEntities(code)
    }

    this.data = {
      code: code,
      theme: data?.theme || 'default',
    }
    this.wrapper = null
    this.textarea = null
    this.preview = null
    this.mermaidId = `mermaid-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`
  }

  // 解码 HTML 实体
  decodeHtmlEntities(str) {
    if (!str || typeof str !== 'string') {
      return str
    }
    // 使用 DOM API 解码 HTML 实体（最可靠的方法）
    const textarea = document.createElement('textarea')
    textarea.innerHTML = str
    return textarea.value
  }

  render() {
    this.wrapper = document.createElement('div')
    this.wrapper.classList.add('ce-mermaid-wrapper')

    // 主题选择器（编辑模式）
    if (!this.readOnly) {
      const controlsWrapper = document.createElement('div')
      controlsWrapper.classList.add('ce-mermaid__controls')

      // 主题选择
      const themeWrapper = document.createElement('div')
      themeWrapper.classList.add('ce-mermaid__theme-wrapper')

      const themeLabel = document.createElement('span')
      themeLabel.textContent = '主题: '
      themeLabel.style.cssText = 'font-size: 0.75rem; color: #6b7280; font-family: monospace; margin-right: 0.5rem;'
      themeWrapper.appendChild(themeLabel)

      const themeSelect = document.createElement('select')
      themeSelect.classList.add('ce-mermaid__theme-select')
      themeSelect.value = this.data.theme

      const themes = [
        { value: 'default', label: '默认' },
        { value: 'dark', label: '深色' },
        { value: 'forest', label: '森林' },
        { value: 'neutral', label: '中性' },
      ]

      themes.forEach(theme => {
        const option = document.createElement('option')
        option.value = theme.value
        option.textContent = theme.label
        themeSelect.appendChild(option)
      })

      themeSelect.addEventListener('change', (e) => {
        this.data.theme = e.target.value
        this.renderMermaid()
      })

      themeWrapper.appendChild(themeSelect)
      controlsWrapper.appendChild(themeWrapper)

      // 示例按钮
      const exampleBtn = document.createElement('button')
      exampleBtn.classList.add('ce-mermaid__example-btn')
      exampleBtn.textContent = '插入示例'
      exampleBtn.addEventListener('click', () => {
        this.insertExample()
      })
      controlsWrapper.appendChild(exampleBtn)

      this.wrapper.appendChild(controlsWrapper)
    } else {
      // 只读模式：显示主题标签
      const themeLabel = document.createElement('div')
      themeLabel.classList.add('ce-mermaid__theme-label')
      themeLabel.style.cssText = 'font-size: 0.75rem; color: #6b7280; font-family: monospace; margin-bottom: 0.5rem;'
      const themes = { default: '默认', dark: '深色', forest: '森林', neutral: '中性' }
      themeLabel.textContent = `主题: ${themes[this.data.theme] || this.data.theme}`
      this.wrapper.appendChild(themeLabel)
    }

    // 编辑区域（编辑模式）
    if (!this.readOnly) {
      this.textarea = document.createElement('textarea')
      this.textarea.classList.add('ce-mermaid__textarea')
      // 确保代码已经解码（textarea.value 会自动处理，但为了保险起见）
      this.textarea.value = this.data.code
      this.textarea.placeholder = '输入 Mermaid 图表代码...\n\n示例：\ngraph TD\n    A[开始] --> B{判断}\n    B -->|是| C[执行A]\n    B -->|否| D[执行B]\n    C --> E[结束]\n    D --> E'

      this.textarea.addEventListener('input', () => {
        this.data.code = this.textarea.value
        this.debounceRender()
      })

      this.wrapper.appendChild(this.textarea)
    }

    // 预览区域
    this.preview = document.createElement('div')
    this.preview.classList.add('ce-mermaid__preview')
    this.preview.id = this.mermaidId

    this.wrapper.appendChild(this.preview)

    // 初始化 Mermaid 并渲染
    this.initMermaid()

    return this.wrapper
  }

  initMermaid() {
    // Mermaid 已通过 npm 包在 main.js 中加载
    if (typeof window === 'undefined') return

    // 如果 Mermaid 已加载，直接使用
    if (window.mermaid) {
      this.renderMermaid()
      return
    }

    // 如果 Mermaid 还未加载（理论上不应该发生），等待加载
    const checkMermaid = setInterval(() => {
      if (window.mermaid) {
        clearInterval(checkMermaid)
        this.renderMermaid()
      }
    }, 100)

    // 设置超时，避免无限等待
    setTimeout(() => {
      clearInterval(checkMermaid)
      if (!window.mermaid) {
        console.error('Mermaid.js failed to load')
      }
    }, 5000)
  }

  renderMermaid() {
    if (!this.preview) {
      return
    }

    // 如果没有代码，显示提示信息（不显示错误）
    if (!this.data.code || !this.data.code.trim()) {
      this.preview.innerHTML = '<div class="ce-mermaid__empty">请输入 Mermaid 图表代码</div>'
      return
    }

    if (!window.mermaid) {
      this.preview.innerHTML = '<div class="ce-mermaid__loading">Mermaid 库加载中...</div>'
      return
    }

    try {
      // 初始化 Mermaid（设置主题）
      window.mermaid.initialize({
        startOnLoad: false,
        theme: this.data.theme,
        securityLevel: 'loose',
        flowchart: {
          useMaxWidth: true,
          htmlLabels: true,
        },
      })

      // 清空预览区域
      this.preview.innerHTML = ''

      // 创建新的容器用于渲染
      const container = document.createElement('div')
      container.id = `${this.mermaidId}-container`
      this.preview.appendChild(container)

      // 使用 mermaid.render 方法渲染（兼容不同版本）
      const renderId = `${this.mermaidId}-${Date.now()}`

      // 检查 Mermaid 版本
      if (window.mermaid.render && typeof window.mermaid.render === 'function') {
        // Mermaid 10.x 版本
        window.mermaid.render(renderId, this.data.code).then((result) => {
          if (result && result.svg) {
            container.innerHTML = result.svg
            const svg = container.querySelector('svg')
            if (svg) {
              svg.style.maxWidth = '100%'
              svg.style.height = 'auto'
            }
            // 清除之前的错误提示（包括 Mermaid 自动生成的）
            this.clearErrorMessages()
            this.clearMermaidErrorMessages()
          } else {
            throw new Error('渲染结果无效')
          }
        }).catch((err) => {
          console.error('Mermaid render error:', err)
          // 清除 Mermaid 自动生成的错误提示
          this.clearMermaidErrorMessages()
          // 只在有实际代码时才显示错误
          if (this.data.code && this.data.code.trim()) {
            const errorMsg = err.message || '请检查 Mermaid 代码语法'
            // 如果已经有错误提示，更新它；否则创建新的
            let errorDiv = this.preview.querySelector('.ce-mermaid__error')
            if (!errorDiv) {
              errorDiv = document.createElement('div')
              errorDiv.className = 'ce-mermaid__error'
              this.preview.appendChild(errorDiv)
            }
            errorDiv.textContent = `语法错误: ${errorMsg}`
          }
        })
      } else if (window.mermaid.mermaidAPI && window.mermaid.mermaidAPI.render) {
        // Mermaid 9.x 或更早版本
        window.mermaid.mermaidAPI.render(renderId, this.data.code, (svgCode) => {
          if (svgCode) {
            container.innerHTML = svgCode
            const svg = container.querySelector('svg')
            if (svg) {
              svg.style.maxWidth = '100%'
              svg.style.height = 'auto'
            }
          } else {
            throw new Error('渲染失败')
          }
        })
      } else {
        // 尝试直接使用 mermaid.parse 和手动渲染
        window.mermaid.parse(this.data.code).then(() => {
          // 如果解析成功，尝试渲染
          const tempDiv = document.createElement('div')
          tempDiv.className = 'mermaid'
          tempDiv.textContent = this.data.code
          container.appendChild(tempDiv)

          // 触发 Mermaid 自动渲染
          if (window.mermaid.init) {
            window.mermaid.init(undefined, container)
          } else if (window.mermaid.run) {
            window.mermaid.run({
              nodes: [container],
              suppressErrors: true,
            })
          }
          // 清除 Mermaid 自动生成的错误提示
          setTimeout(() => {
            this.clearMermaidErrorMessages()
          }, 100)
        }).catch((err) => {
          console.error('Mermaid parse error:', err)
          // 清除 Mermaid 自动生成的错误提示
          this.clearMermaidErrorMessages()
          // 只在有实际代码时才显示错误
          if (this.data.code && this.data.code.trim()) {
            const errorMsg = err.message || '请检查 Mermaid 代码语法'
            let errorDiv = this.preview.querySelector('.ce-mermaid__error')
            if (!errorDiv) {
              errorDiv = document.createElement('div')
              errorDiv.className = 'ce-mermaid__error'
              this.preview.appendChild(errorDiv)
            }
            errorDiv.textContent = `语法错误: ${errorMsg}`
          }
        })
      }
    } catch (err) {
      console.error('Mermaid error:', err)
      // 清除 Mermaid 自动生成的错误提示
      this.clearMermaidErrorMessages()
      // 只在有实际代码时才显示错误
      if (this.data.code && this.data.code.trim()) {
        const errorMsg = err.message || '渲染失败'
        let errorDiv = this.preview.querySelector('.ce-mermaid__error')
        if (!errorDiv) {
          errorDiv = document.createElement('div')
          errorDiv.className = 'ce-mermaid__error'
          this.preview.appendChild(errorDiv)
        }
        errorDiv.textContent = `错误: ${errorMsg}`
      }
    }
  }

  // 清除 Mermaid 自动生成的错误提示（ID 格式：dmermaid-xxx）
  clearMermaidErrorMessages() {
    if (!this.preview) return

    // 查找所有以 dmermaid- 开头的 ID 的元素（Mermaid 自动生成的错误提示）
    const mermaidErrors = this.preview.querySelectorAll('[id^="dmermaid-"]')
    mermaidErrors.forEach(el => {
      el.remove()
    })

    // 也查找可能的其他 Mermaid 错误元素（根据实际 DOM 结构调整）
    const errorElements = this.preview.querySelectorAll('[class*="error"], [class*="Error"]')
    errorElements.forEach(el => {
      // 只移除 Mermaid 自动生成的错误，保留我们自定义的
      if (!el.classList.contains('ce-mermaid__error')) {
        const id = el.id || ''
        if (id.startsWith('dmermaid-') || id.includes('mermaid-error')) {
          el.remove()
        }
      }
    })
  }

  // 清除所有错误提示（包括自定义的）
  clearErrorMessages() {
    if (!this.preview) return

    // 清除自定义错误提示
    const errorDiv = this.preview.querySelector('.ce-mermaid__error')
    if (errorDiv) {
      errorDiv.remove()
    }

    // 清除 Mermaid 自动生成的错误提示
    this.clearMermaidErrorMessages()
  }

  debounceRender() {
    if (this.renderTimeout) {
      clearTimeout(this.renderTimeout)
    }
    // 如果代码为空，立即清除错误提示
    if (!this.data.code || !this.data.code.trim()) {
      if (this.preview) {
        this.clearErrorMessages()
        this.preview.innerHTML = '<div class="ce-mermaid__empty">请输入 Mermaid 图表代码</div>'
      }
      return
    }
    this.renderTimeout = setTimeout(() => {
      this.renderMermaid()
    }, 500)
  }

  insertExample() {
    if (!this.textarea) return

    const examples = [
      {
        name: '流程图',
        code: `graph TD
    A[开始] --> B{判断条件}
    B -->|是| C[执行操作A]
    B -->|否| D[执行操作B]
    C --> E[结束]
    D --> E`
      },
      {
        name: '序列图',
        code: `sequenceDiagram
    participant A as 用户
    participant B as 系统
    participant C as 数据库
    
    A->>B: 发送请求
    B->>C: 查询数据
    C-->>B: 返回结果
    B-->>A: 响应数据`
      },
      {
        name: '甘特图',
        code: `gantt
    title 项目进度
    dateFormat YYYY-MM-DD
    section 阶段1
    任务1 :a1, 2024-01-01, 30d
    任务2 :a2, after a1, 20d
    section 阶段2
    任务3 :a3, 2024-02-01, 30d`
      },
      {
        name: '饼图',
        code: `pie title 数据分布
    "类型A" : 42.1
    "类型B" : 30.2
    "类型C" : 27.7`
      },
      {
        name: '状态图',
        code: `stateDiagram-v2
    [*] --> 状态1
    状态1 --> 状态2
    状态2 --> 状态3
    状态3 --> [*]`
      },
    ]

    // 如果当前有内容，插入序列图示例，否则插入流程图示例
    const example = this.data.code.trim() ? examples[1] : examples[0]
    this.textarea.value = example.code
    this.data.code = example.code
    this.renderMermaid()
    this.textarea.focus()
  }

  save() {
    // 确保保存的代码没有被 HTML 转义
    let code = this.data.code || ''
    if (this.textarea) {
      // 从 textarea 直接获取值，避免 HTML 转义
      code = this.textarea.value
    }
    return {
      code: code,
      theme: this.data.theme,
    }
  }

  // 如果 Editor.js 调用 merge 方法更新数据，也需要解码
  merge(data) {
    if (data && data.code && typeof data.code === 'string') {
      this.data.code = this.decodeHtmlEntities(data.code)
      // 如果 textarea 存在，更新其值
      if (this.textarea) {
        this.textarea.value = this.data.code
      }
      // 重新渲染
      this.renderMermaid()
    }
    if (data && data.theme) {
      this.data.theme = data.theme
    }
  }

  static get sanitize() {
    return {
      code: {},
      theme: {},
    }
  }

  static validate(savedData) {
    if (!savedData || !savedData.code || savedData.code.trim() === '') {
      return false
    }
    return true
  }
}
