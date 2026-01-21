/**
 * Mermaid.js 初始化
 * 导入 Mermaid 并挂载到 window 对象
 */

import mermaid from 'mermaid'

// 注意：Mermaid 10.x 版本的 CSS 已经内嵌在 JS 中，不需要单独导入 CSS
// 如果需要自定义样式，可以在 style.css 中添加

// 初始化 Mermaid 默认配置
mermaid.initialize({
  startOnLoad: false, // 不自动渲染，由组件手动控制
  theme: 'default',
  securityLevel: 'loose',
  flowchart: {
    useMaxWidth: true,
    htmlLabels: true,
  },
})

// 将 Mermaid 挂载到 window 对象，以便 MermaidTool.js 等组件可以使用
if (typeof window !== 'undefined') {
  window.mermaid = mermaid
}

export default mermaid
