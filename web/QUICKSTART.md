# 快速启动指南

## 1. 安装依赖

```bash
cd web
npm install
```

## 2. 配置环境变量（可选）

创建 `.env` 文件（如果后端不在 localhost:8080）：

```env
VITE_API_BASE_URL=http://localhost:8080
```

## 3. 启动开发服务器

```bash
npm run dev
```

前端将在 `http://localhost:3000` 启动。

## 4. 确保后端服务运行

后端服务需要在 `http://localhost:8080` 运行。

## 功能说明

### 已实现的页面

1. **登录/注册** (`/login`, `/register`)
   - 用户认证
   - Token 自动管理

2. **首页** (`/`)
   - 时间轴展示（Memos + Pages）
   - 加载更多
   - 快速创建入口

3. **创建 Memo** (`/memo/create`)
   - 文本输入
   - 图片上传（自动压缩）
   - 离线支持

4. **编辑页面** (`/pages/:id`, `/pages/new`)
   - Editor.js 编辑器
   - 标题和封面设置
   - 分享功能

5. **分享页面** (`/s/:share_id`)
   - 只读模式
   - 公开访问

## 核心特性

✅ Token 自动刷新  
✅ 图片压缩（500KB 以内）  
✅ 离线存储和自动同步  
✅ Editor.js 集成  
✅ 响应式设计（Tailwind CSS）
