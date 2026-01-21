/**
 * 自定义 Code Tool，支持语言选择和语法高亮
 * 基于 @editorjs/code 扩展
 * 使用 Prism.js 进行语法高亮（通过 CDN 加载）
 */
import Code from '@editorjs/code'

export default class CodeTool extends Code {
  static get toolbox() {
    return {
      title: '代码',
      icon: '<svg width="20" height="20" viewBox="0 0 20 20" xmlns="http://www.w3.org/2000/svg"><path d="M13.8 10.9c-.14-.14-.14-.37 0-.52l4.5-4.5c.14-.14.37-.14.52 0 .14.14.14.37 0 .52L14.33 10.9c-.14.14-.37.14-.52 0zm-7.6 0c-.14.14-.37.14-.52 0L1.18 6.4c-.14-.14-.14-.37 0-.52.14-.14.37-.14.52 0l4.5 4.5c.14.14.14.37 0 .52z"/></svg>'
    }
  }

  static get isReadOnlySupported() {
    return true
  }

  constructor({ data, api, readOnly, config }) {
    // 确保 config 存在，提供默认 placeholder
    const toolConfig = config || {}
    if (!toolConfig.placeholder) {
      toolConfig.placeholder = '输入代码...'
    }
    super({ data, api, readOnly, config: toolConfig })

    // 支持的语言列表
    this.languages = {
      'javascript': 'JavaScript',
      'typescript': 'TypeScript',
      'python': 'Python',
      'java': 'Java',
      'c': 'C',
      'cpp': 'C++',
      'csharp': 'C#',
      'go': 'Go',
      'rust': 'Rust',
      'php': 'PHP',
      'ruby': 'Ruby',
      'swift': 'Swift',
      'kotlin': 'Kotlin',
      'sql': 'SQL',
      'bash': 'Bash/Shell',
      'json': 'JSON',
      'markdown': 'Markdown',
      'html': 'HTML',
      'css': 'CSS',
      'scss': 'SCSS',
      'yaml': 'YAML',
      'xml': 'XML',
    }

    // 当前选择的语言（从 data.language 或 data.mode 获取）
    this.currentLanguage = (data?.language || data?.mode || 'javascript')
  }

  render() {
    // 先调用父类的 render 方法获取基础代码块
    const codeBlock = super.render()
    if (!codeBlock) {
      return document.createElement('div')
    }

    const wrapper = document.createElement('div')
    wrapper.classList.add('ce-code-wrapper')

    // 语言选择器（编辑模式）
    if (!this.readOnly) {
      const selectWrapper = document.createElement('div')
      selectWrapper.classList.add('ce-code__select-wrapper')

      const label = document.createElement('span')
      label.textContent = '语言: '
      label.style.cssText = 'font-size: 0.75rem; color: #6b7280; font-family: monospace; margin-right: 0.5rem;'
      selectWrapper.appendChild(label)

      const select = document.createElement('select')
      select.classList.add('ce-code__select')
      select.value = this.currentLanguage

      // 添加语言选项
      Object.entries(this.languages).forEach(([code, name]) => {
        const option = document.createElement('option')
        option.value = code
        option.textContent = name
        select.appendChild(option)
      })

      select.addEventListener('change', (e) => {
        this.currentLanguage = e.target.value
        this.updateHighlight()
      })

      selectWrapper.appendChild(select)
      wrapper.appendChild(selectWrapper)

      // 编辑模式：创建叠加层容器（编辑 + 高亮预览）
      const editorContainer = document.createElement('div')
      editorContainer.classList.add('ce-code__editor-container')

      // 获取父类的 textarea
      const textarea = codeBlock.querySelector('textarea')
      if (textarea) {
        this.textarea = textarea

        // 创建高亮预览层
        const highlightLayer = document.createElement('pre')
        highlightLayer.classList.add('ce-code__highlight-layer')
        const codeElement = document.createElement('code')
        codeElement.classList.add(`language-${this.currentLanguage}`)
        highlightLayer.appendChild(codeElement)
        this.highlightLayer = highlightLayer
        this.highlightCode = codeElement

        // 设置 textarea 样式，使其透明并覆盖在高亮层上
        textarea.classList.add('ce-code__textarea-overlay')

        // 确保样式完全一致
        this.syncStyles(textarea, highlightLayer)

        // 同步滚动（双向同步）
        textarea.addEventListener('scroll', () => {
          highlightLayer.scrollTop = textarea.scrollTop
          highlightLayer.scrollLeft = textarea.scrollLeft
        })

        // 监听输入变化，实时更新高亮
        textarea.addEventListener('input', () => {
          this.updateHighlight()
          // 确保滚动位置同步
          highlightLayer.scrollTop = textarea.scrollTop
          highlightLayer.scrollLeft = textarea.scrollLeft
        })

        // 监听窗口大小变化，重新同步样式
        window.addEventListener('resize', () => {
          this.syncStyles(textarea, highlightLayer)
        })

        // 将高亮层和 textarea 添加到容器
        editorContainer.appendChild(highlightLayer)
        editorContainer.appendChild(textarea)

        // 将容器添加到 wrapper
        wrapper.appendChild(editorContainer)

        // 初始更新高亮
        this.updateHighlight()
      } else {
        // 如果没有 textarea，使用原始代码块
        wrapper.appendChild(codeBlock)
      }
    } else {
      // 只读模式：显示语言标签
      const langLabel = document.createElement('div')
      langLabel.classList.add('ce-code__lang-label')
      langLabel.style.cssText = 'font-size: 0.75rem; color: #6b7280; font-family: monospace; margin-bottom: 0.5rem;'
      langLabel.textContent = `语言: ${this.languages[this.currentLanguage] || this.currentLanguage}`
      wrapper.appendChild(langLabel)

      // 只读模式：直接创建 pre/code 结构，而不是使用父类的 codeBlock（可能是 disabled textarea）
      const pre = document.createElement('pre')
      pre.className = 'ce-code-pre'
      const code = document.createElement('code')
      code.className = `language-${this.currentLanguage}`

      // 从 data 中获取代码内容
      const codeText = this.data?.code || ''
      code.textContent = codeText

      pre.appendChild(code)
      wrapper.appendChild(pre)

      // 为 wrapper 添加 data-language 属性，方便后续高亮
      wrapper.setAttribute('data-language', this.currentLanguage)

      // 保存引用以便后续高亮
      this.wrapperPre = pre
      this.wrapperCode = code
    }

    // 保存 wrapper 引用以便后续使用
    this.wrapper = wrapper

    // 应用语法高亮（只读模式）
    if (this.readOnly) {
      // 延迟应用，确保 DOM 已完全渲染，并且 Prism.js 已加载
      setTimeout(() => {
        this.applySyntaxHighlight()
      }, 300)
    }

    return wrapper
  }

  applySyntaxHighlight() {
    if (typeof window === 'undefined') return

    // 只读模式：直接对 pre/code 标签应用语法高亮
    if (this.readOnly) {
      // 使用更长的延迟，确保 Prism.js 已完全加载
      setTimeout(() => {
        if (!this.wrapper) return

        const Prism = window.Prism
        if (!Prism) {
          console.warn('CodeTool: Prism.js not loaded yet, retrying...')
          // 如果 Prism 还没加载，再等一会儿
          setTimeout(() => this.applySyntaxHighlight(), 500)
          return
        }

        // 使用保存的引用，或查找 pre/code 元素
        const pre = this.wrapperPre || this.wrapper.querySelector('pre')
        const code = this.wrapperCode || (pre ? pre.querySelector('code') : null)

        if (!pre || !code) {
          console.warn('CodeTool: pre or code element not found', {
            hasPre: !!pre,
            hasCode: !!code,
            wrapperHTML: this.wrapper.innerHTML.substring(0, 200)
          })
          return
        }

        // 从 wrapper 的 data-language 属性获取语言
        const dataLang = this.wrapper.getAttribute('data-language')
        const targetLanguage = dataLang || this.currentLanguage

        // 检查语言是否已加载
        let language = Prism.languages[targetLanguage]
        let languageId = targetLanguage

        // 如果语言未加载，尝试使用备用语言
        if (!language || typeof language !== 'object') {
          console.warn(`CodeTool: Language "${targetLanguage}" not loaded, trying fallbacks`)
          if (Prism.languages.javascript && typeof Prism.languages.javascript === 'object') {
            language = Prism.languages.javascript
            languageId = 'javascript'
          } else if (Prism.languages.markup && typeof Prism.languages.markup === 'object') {
            language = Prism.languages.markup
            languageId = 'markup'
          } else {
            // 如果都没有，至少设置类名
            code.className = `language-${targetLanguage}`
            console.warn(`CodeTool: No fallback language available for ${targetLanguage}`)
            return
          }
        }

        code.className = `language-${languageId}`

        // 使用 highlightElement 方法（更安全）
        if (Prism.highlightElement) {
          try {
            Prism.highlightElement(code)
            console.log('CodeTool: Syntax highlight applied successfully', { language: languageId, hasTokens: code.querySelectorAll('.token').length > 0 })
          } catch (err) {
            console.warn('CodeTool: Prism highlightElement error:', err)
            // 如果 highlightElement 失败，尝试手动高亮
            try {
              const codeText = code.textContent || ''
              const highlighted = Prism.highlight(codeText, language, languageId)
              code.innerHTML = highlighted
              console.log('CodeTool: Manual highlight applied', { language: languageId })
            } catch (highlightErr) {
              console.warn('CodeTool: Prism manual highlight error:', highlightErr)
            }
          }
        } else if (Prism.highlight) {
          // 如果没有 highlightElement，使用 highlight
          try {
            const codeText = code.textContent || ''
            const highlighted = Prism.highlight(codeText, language, languageId)
            code.innerHTML = highlighted
            console.log('CodeTool: Highlight applied using Prism.highlight', { language: languageId })
          } catch (err) {
            console.warn('CodeTool: Prism highlight error:', err)
          }
        } else {
          console.warn('CodeTool: Prism.highlightElement and Prism.highlight not available')
        }
      }, 300)
    }
  }

  // 同步 textarea 和 highlightLayer 的样式
  syncStyles(textarea, highlightLayer) {
    // 获取 textarea 的计算样式
    const textareaStyle = window.getComputedStyle(textarea)

    // 同步关键样式属性，确保完全对齐
    highlightLayer.style.padding = textareaStyle.padding
    highlightLayer.style.margin = textareaStyle.margin
    highlightLayer.style.fontSize = textareaStyle.fontSize
    highlightLayer.style.fontFamily = textareaStyle.fontFamily
    highlightLayer.style.lineHeight = textareaStyle.lineHeight
    highlightLayer.style.letterSpacing = textareaStyle.letterSpacing
    highlightLayer.style.wordSpacing = textareaStyle.wordSpacing
    highlightLayer.style.tabSize = textareaStyle.tabSize || '2'
    highlightLayer.style.width = textareaStyle.width
    highlightLayer.style.height = textareaStyle.height

    // 确保 code 元素也使用相同的样式
    if (this.highlightCode) {
      this.highlightCode.style.fontSize = textareaStyle.fontSize
      this.highlightCode.style.fontFamily = textareaStyle.fontFamily
      this.highlightCode.style.lineHeight = textareaStyle.lineHeight
      this.highlightCode.style.letterSpacing = textareaStyle.letterSpacing
      this.highlightCode.style.wordSpacing = textareaStyle.wordSpacing
    }
  }

  // 更新语法高亮（编辑模式）
  updateHighlight() {
    if (!this.highlightCode || !this.textarea) {
      console.warn('CodeTool: highlightCode or textarea not available')
      return
    }

    // 等待 Prism 加载（最多等待 2 秒）
    const checkPrism = (attempts = 0) => {
      const Prism = window.Prism

      if (!Prism || !Prism.highlight) {
        if (attempts < 20) {
          // 每 100ms 检查一次，最多检查 20 次（2 秒）
          setTimeout(() => checkPrism(attempts + 1), 100)
          return
        } else {
          // Prism 未加载，显示原始代码
          console.warn('CodeTool: Prism.js not loaded after 2 seconds')
          this.highlightCode.textContent = this.textarea.value || ' '
          this.highlightCode.className = `language-${this.currentLanguage}`
          return
        }
      }

      // Prism 已加载，执行高亮
      this.doHighlight(Prism)
    }

    checkPrism()
  }

  // 执行实际的语法高亮
  doHighlight(Prism) {
    const code = this.textarea.value || ''

    try {
      // 检查语言是否已加载
      let language = Prism.languages[this.currentLanguage]
      let languageId = this.currentLanguage

      // 如果语言未加载，尝试使用备用语言
      if (!language || typeof language !== 'object') {
        console.warn(`CodeTool: Language "${this.currentLanguage}" not loaded, trying fallbacks`)

        // 尝试 javascript
        if (Prism.languages.javascript && typeof Prism.languages.javascript === 'object') {
          language = Prism.languages.javascript
          languageId = 'javascript'
          console.log('CodeTool: Using javascript as fallback')
        }
        // 尝试 markup
        else if (Prism.languages.markup && typeof Prism.languages.markup === 'object') {
          language = Prism.languages.markup
          languageId = 'markup'
          console.log('CodeTool: Using markup as fallback')
        }
        // 尝试 plaintext
        else if (Prism.languages.plaintext && typeof Prism.languages.plaintext === 'object') {
          language = Prism.languages.plaintext
          languageId = 'plaintext'
          console.log('CodeTool: Using plaintext as fallback')
        }
        // 如果都没有，直接显示原始代码，不使用高亮
        else {
          console.warn('CodeTool: No valid language found, displaying plain text')
          this.highlightCode.textContent = code
          this.highlightCode.className = `language-${this.currentLanguage}`
          return
        }
      }

      // 确保 language 是有效的对象
      if (!language || typeof language !== 'object') {
        console.warn('CodeTool: Invalid language definition, displaying plain text')
        this.highlightCode.textContent = code
        this.highlightCode.className = `language-${this.currentLanguage}`
        return
      }

      // 对于某些语言（如 PHP），需要检查依赖是否已加载
      // PHP 依赖于 markup-templating
      if (languageId === 'php' && (!Prism.languages['markup-templating'] || typeof Prism.languages['markup-templating'] !== 'object')) {
        console.warn('CodeTool: PHP requires markup-templating, but it is not loaded. Using fallback.')
        // 尝试使用 javascript 作为备用
        if (Prism.languages.javascript && typeof Prism.languages.javascript === 'object') {
          language = Prism.languages.javascript
          languageId = 'javascript'
        } else {
          // 如果 javascript 也不可用，显示原始代码
          this.highlightCode.textContent = code
          this.highlightCode.className = `language-${this.currentLanguage}`
          return
        }
      }

      // 使用 Prism.highlight 方法高亮代码
      // 对于有依赖的语言，确保依赖已加载
      let highlighted
      try {
        highlighted = Prism.highlight(code, language, languageId)
      } catch (highlightErr) {
        // 如果高亮失败，可能是依赖问题，尝试使用备用语言
        console.warn('CodeTool: Highlight failed, trying fallback:', highlightErr)
        if (languageId !== 'javascript' && Prism.languages.javascript && typeof Prism.languages.javascript === 'object') {
          try {
            highlighted = Prism.highlight(code, Prism.languages.javascript, 'javascript')
            languageId = 'javascript'
          } catch (fallbackErr) {
            console.error('CodeTool: Fallback highlight also failed:', fallbackErr)
            this.highlightCode.textContent = code
            this.highlightCode.className = `language-${this.currentLanguage}`
            return
          }
        } else {
          // 如果已经是 javascript 或没有备用，显示原始代码
          this.highlightCode.textContent = code
          this.highlightCode.className = `language-${this.currentLanguage}`
          return
        }
      }

      // 更新高亮层
      this.highlightCode.className = `language-${languageId}`

      // 确保高亮结果不为空
      if (!highlighted || highlighted.trim() === '') {
        console.warn('CodeTool: Highlight result is empty, using plain text')
        this.highlightCode.textContent = code
      } else {
        // 设置高亮后的 HTML
        this.highlightCode.innerHTML = highlighted
        console.log('CodeTool: Highlight successful', {
          language: languageId,
          codeLength: code.length,
          highlightedLength: highlighted.length,
          hasTokens: highlighted.includes('token')
        })

        // 调试：检查是否有 token 元素
        const tokens = this.highlightCode.querySelectorAll('.token, [class*="token"]')
        console.log('CodeTool: Token count:', tokens.length)
      }

      // 确保样式同步
      if (this.highlightLayer) {
        this.syncStyles(this.textarea, this.highlightLayer)
      }
    } catch (err) {
      console.error('CodeTool: Prism highlight error:', err)
      // 如果高亮失败，显示原始代码
      this.highlightCode.textContent = code
      this.highlightCode.className = `language-${this.currentLanguage}`
    }
  }

  // 重写 save 方法，包含语言信息
  save(blockContent) {
    const content = blockContent || this.wrapper
    // 优先使用保存的 textarea 引用，否则查找
    const textarea = this.textarea || content?.querySelector('textarea')
    const code = textarea ? textarea.value : ''

    return {
      code: code,
      language: this.currentLanguage,
    }
  }

  // 重写 merge 方法，处理数据合并
  merge(data) {
    if (data && (data.language || data.mode)) {
      this.currentLanguage = data.language || data.mode
    }
    if (super.merge) {
      super.merge(data)
    }
  }

  // 重写 validate 方法
  static validate(savedData) {
    if (!savedData || !savedData.code || savedData.code.trim() === '') {
      return false
    }
    return true
  }

  static get sanitize() {
    return {
      code: {
        br: true,
      },
      language: {},
    }
  }
}
