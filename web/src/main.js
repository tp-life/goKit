import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import './style.css'
// 初始化 Prism.js（导入所有语言组件并挂载到 window）
import './utils/prism.js'
// 初始化 Mermaid.js（导入并挂载到 window）
import './utils/mermaid.js'

const app = createApp(App)

app.use(createPinia())
app.use(router)

app.mount('#app')
