# Web UI 开发指南

## 开发环境设置

1. 安装依赖：
```bash
cd web
npm install
```

2. 启动开发服务器：
```bash
npm run dev
```

开发服务器会在 `http://localhost:3000` 启动，并自动代理 API 请求到后端。

## 构建生产版本

```bash
npm run build
```

构建产物会输出到 `web/dist/` 目录，然后会被 Go 的 embed 功能嵌入到二进制文件中。

## 项目结构

```
web/
├── index.html              # 入口 HTML
├── vite.config.js         # Vite 配置
├── tailwind.config.js     # Tailwind CSS 配置
├── postcss.config.js      # PostCSS 配置
├── package.json           # 依赖配置
└── src/
    ├── main.js            # 入口文件
    ├── App.vue            # 主组件
    ├── style.css          # 全局样式
    ├── components/        # Vue 组件
    │   ├── ComparisonTable.vue
    │   ├── ArbitragePanel.vue
    │   ├── ConfigPanel.vue
    │   └── StatusIndicator.vue
    └── composables/       # 组合式函数
        ├── useWebSocket.js
        └── useAPI.js
```

## 功能说明

### 1. 价格对比表格
- 实时显示多交易所价格
- 显示价格差异和费率差异
- 套利机会标识

### 2. 套利机会面板
- 筛选不同级别的套利机会
- 显示预期收益率
- 按时间排序

### 3. 配置面板
- 自定义套利阈值
- 实时更新配置

### 4. WebSocket 实时推送
- 自动连接 WebSocket
- 实时接收对比数据更新
- 自动重连机制

## 注意事项

1. **构建顺序**：在构建 Go 应用之前，必须先构建前端：
   ```bash
   cd web && npm run build
   cd .. && go build ./cmd/server
   ```

2. **开发模式**：开发时可以使用 `npm run dev` 独立运行前端，通过代理连接后端。

3. **生产部署**：生产环境使用 embed 方式，前端文件会被嵌入到 Go 二进制文件中。
