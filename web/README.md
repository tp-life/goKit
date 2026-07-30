# GoKit Web 管理后台

GoKit 后端（`/api/v1`）的管理控制台前端。「编辑部纸张 × 墨色 × 朱砂」视觉风格，
基于 Ant Design Pro 组件体系。

## 技术栈

- Vite 5 + React 18 + TypeScript（pnpm 管理依赖）
- antd v5 + @ant-design/pro-components（ProLayout / ProTable / ProForm）+ @ant-design/icons
- react-router-dom v6、dayjs（中文 locale）
- 字体：@fontsource 的 Fraunces / Noto Serif SC（标题）、Noto Sans SC（正文）、JetBrains Mono（数字与 ID）

## 目录结构

```
src/
├── api/          # request 封装（响应壳/401/token）+ 各模块 API 与 TS 类型（对齐 openapi.yaml）
├── components/   # Stamp 状态印章等小组件
├── layouts/      # AdminLayout（ProLayout 墨色侧边栏 + 刊头）
├── pages/        # login / dashboard / users / roles / depts / menus / logs / profile
├── store/        # auth context（token、profile、usePerms/hasPerm）
├── utils/        # notify、format、tree 转换
├── theme.ts      # antd v5 theme token / components 覆盖
└── global.css    # CSS 变量、纸张噪点纹理、硬阴影卡片、登录页与工作台样式
```

## 使用

```bash
pnpm install   # 安装依赖
pnpm dev       # 开发（默认 http://localhost:5173）
pnpm build     # 类型检查 + 生产构建（tsc --noEmit && vite build）
pnpm preview   # 预览构建产物
```

> 本机 pnpm 版本若要求 Node ≥ 22.13，可用 `npm exec --package=pnpm@9 -- pnpm <cmd>` 代替。

## 后端联调

- 接口 base URL 为 `/api/v1`；后端默认监听 `:8080`，且未配置 CORS。
- 开发环境已通过 `vite.config.ts` 的 proxy 将 `/api` 转发到 `http://localhost:8080`，
  因此只需先后启动后端（`go run ./cmd/server` 或 `make run`）与 `pnpm dev` 即可联调。
- 生产部署时需由反向代理（如 nginx）把 `/api` 转发到后端服务。

## 约定

- 统一响应壳 `{code, message, data}`：`code !== 0` 时全局 `message.error` 并 reject；
  401（`40100`）自动清 token 并跳转登录页。
- token 存于 `localStorage`（key：`gokit_token`），所有请求自动携带 `Authorization: Bearer`。
- 按钮级权限：`usePerms().hasPerm('system:user:create')`（超级管理员恒为 true）。
