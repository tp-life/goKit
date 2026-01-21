/**
 * Prism.js 初始化
 * 导入所有需要的语言组件并挂载到 window 对象
 */

import Prism from 'prismjs'
import 'prismjs/themes/prism-tomorrow.css'

// 导入基础语言（必须先加载）
import 'prismjs/components/prism-markup'
import 'prismjs/components/prism-markup-templating'
import 'prismjs/components/prism-css'
import 'prismjs/components/prism-clike'

// 导入所有支持的语言
import 'prismjs/components/prism-javascript'
import 'prismjs/components/prism-typescript'
import 'prismjs/components/prism-python'
import 'prismjs/components/prism-java'
import 'prismjs/components/prism-c'
import 'prismjs/components/prism-cpp'
import 'prismjs/components/prism-csharp'
import 'prismjs/components/prism-go'
import 'prismjs/components/prism-rust'
import 'prismjs/components/prism-php'
import 'prismjs/components/prism-ruby'
import 'prismjs/components/prism-swift'
import 'prismjs/components/prism-kotlin'
import 'prismjs/components/prism-sql'
import 'prismjs/components/prism-bash'
import 'prismjs/components/prism-json'
import 'prismjs/components/prism-markdown'
// 注意：HTML 已经包含在 prism-markup 中，不需要单独导入
import 'prismjs/components/prism-scss'
import 'prismjs/components/prism-yaml'
// 注意：XML 已经包含在 prism-markup 中，不需要单独导入

// 将 Prism 挂载到 window 对象，以便 CodeTool.js 等组件可以使用
if (typeof window !== 'undefined') {
  window.Prism = Prism
}

export default Prism
