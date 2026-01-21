# 前端对接指南

本文档说明如何在前端（移动端 PWA 和 PC 端 Web）中对接 Notion-Lite 后端 API。

## 📱 移动端 PWA 对接

### 1. Token 自动刷新拦截器

实现一个 HTTP 拦截器，自动处理 Token 刷新：

```javascript
// utils/api.js
class ApiClient {
  constructor() {
    this.baseURL = 'http://localhost:8080';
    this.accessToken = localStorage.getItem('access_token');
    this.refreshToken = localStorage.getItem('refresh_token');
  }

  async request(url, options = {}) {
    const headers = {
      'Content-Type': 'application/json',
      ...options.headers,
    };

    if (this.accessToken) {
      headers['Authorization'] = `Bearer ${this.accessToken}`;
    }

    try {
      const response = await fetch(`${this.baseURL}${url}`, {
        ...options,
        headers,
      });

      // Token 过期，尝试刷新
      if (response.status === 401 && this.refreshToken) {
        const refreshed = await this.refreshAccessToken();
        if (refreshed) {
          // 重试原请求
          headers['Authorization'] = `Bearer ${this.accessToken}`;
          return fetch(`${this.baseURL}${url}`, {
            ...options,
            headers,
          });
        }
      }

      return response;
    } catch (error) {
      console.error('API request failed:', error);
      throw error;
    }
  }

  async refreshAccessToken() {
    try {
      const response = await fetch(`${this.baseURL}/auth/refresh`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          refresh_token: this.refreshToken,
        }),
      });

      if (response.ok) {
        const data = await response.json();
        this.accessToken = data.access_token;
        this.refreshToken = data.refresh_token;
        localStorage.setItem('access_token', data.access_token);
        localStorage.setItem('refresh_token', data.refresh_token);
        return true;
      }
    } catch (error) {
      console.error('Token refresh failed:', error);
      // 刷新失败，跳转到登录页
      this.logout();
    }
    return false;
  }

  logout() {
    localStorage.removeItem('access_token');
    localStorage.removeItem('refresh_token');
    this.accessToken = null;
    this.refreshToken = null;
    // 跳转到登录页
    window.location.href = '/login';
  }
}

export const apiClient = new ApiClient();
```

### 2. 图片压缩

在上传前压缩图片至 500KB 以内：

```javascript
// utils/imageCompress.js
export async function compressImage(file, maxSizeKB = 500) {
  return new Promise((resolve) => {
    const reader = new FileReader();
    reader.readAsDataURL(file);
    reader.onload = (event) => {
      const img = new Image();
      img.src = event.target.result;
      img.onload = () => {
        const canvas = document.createElement('canvas');
        let width = img.width;
        let height = img.height;
        let quality = 0.9;

        // 计算压缩后的尺寸
        const maxDimension = 1920;
        if (width > maxDimension || height > maxDimension) {
          if (width > height) {
            height = (height * maxDimension) / width;
            width = maxDimension;
          } else {
            width = (width * maxDimension) / height;
            height = maxDimension;
          }
        }

        canvas.width = width;
        canvas.height = height;

        const ctx = canvas.getContext('2d');
        ctx.drawImage(img, 0, 0, width, height);

        // 逐步降低质量直到文件大小符合要求
        const compress = () => {
          canvas.toBlob(
            (blob) => {
              const sizeKB = blob.size / 1024;
              if (sizeKB <= maxSizeKB || quality <= 0.1) {
                resolve(blob);
              } else {
                quality -= 0.1;
                compress();
              }
            },
            'image/jpeg',
            quality
          );
        };
        compress();
      };
    };
  });
}
```

### 3. 离线存储与重试

使用 LocalStorage 实现离线存储和自动重试：

```javascript
// utils/offlineQueue.js
const OFFLINE_QUEUE_KEY = 'offline_memo_queue';

export class OfflineQueue {
  static add(memo) {
    const queue = this.getQueue();
    queue.push({
      ...memo,
      timestamp: Date.now(),
      retryCount: 0,
    });
    localStorage.setItem(OFFLINE_QUEUE_KEY, JSON.stringify(queue));
  }

  static getQueue() {
    const data = localStorage.getItem(OFFLINE_QUEUE_KEY);
    return data ? JSON.parse(data) : [];
  }

  static clear() {
    localStorage.removeItem(OFFLINE_QUEUE_KEY);
  }

  static async processQueue(apiClient) {
    const queue = this.getQueue();
    const failed = [];

    for (const item of queue) {
      try {
        // 上传图片（如果有）
        const imageUrls = [];
        if (item.images && item.images.length > 0) {
          for (const imageFile of item.images) {
            const formData = new FormData();
            formData.append('image', imageFile);
            const uploadRes = await apiClient.request('/api/v1/upload', {
              method: 'POST',
              body: formData,
            });
            if (uploadRes.ok) {
              const uploadData = await uploadRes.json();
              imageUrls.push(uploadData.file.url);
            }
          }
        }

        // 创建 Memo
        const memoRes = await apiClient.request('/api/v1/memos', {
          method: 'POST',
          body: JSON.stringify({
            content: item.content,
            images: imageUrls,
            source: 'mobile',
          }),
        });

        if (memoRes.ok) {
          console.log('Offline memo synced:', item);
        } else {
          failed.push(item);
        }
      } catch (error) {
        console.error('Failed to sync offline memo:', error);
        item.retryCount++;
        if (item.retryCount < 3) {
          failed.push(item);
        }
      }
    }

    if (failed.length > 0) {
      localStorage.setItem(OFFLINE_QUEUE_KEY, JSON.stringify(failed));
    } else {
      this.clear();
    }
  }
}

// 在应用启动时检查网络并处理队列
if ('serviceWorker' in navigator) {
  window.addEventListener('online', () => {
    OfflineQueue.processQueue(apiClient);
  });
}
```

### 4. 创建 Memo 示例

```javascript
// pages/MemoCreate.vue (Vue 3)
import { apiClient } from '@/utils/api';
import { compressImage } from '@/utils/imageCompress';
import { OfflineQueue } from '@/utils/offlineQueue';

export default {
  data() {
    return {
      content: '',
      images: [],
      uploading: false,
    };
  },
  methods: {
    async handleImageSelect(event) {
      const files = Array.from(event.target.files);
      for (const file of files) {
        const compressed = await compressImage(file);
        this.images.push(compressed);
      }
    },
    async submitMemo() {
      this.uploading = true;
      try {
        // 检查网络状态
        if (!navigator.onLine) {
          // 离线模式：存入队列
          OfflineQueue.add({
            content: this.content,
            images: this.images,
          });
          this.$message.success('已保存到本地，网络恢复后自动同步');
          return;
        }

        // 在线模式：直接上传
        const imageUrls = [];
        for (const image of this.images) {
          const formData = new FormData();
          formData.append('image', image);
          const uploadRes = await apiClient.request('/api/v1/upload', {
            method: 'POST',
            body: formData,
          });
          if (uploadRes.ok) {
            const data = await uploadRes.json();
            imageUrls.push(data.file.url);
          }
        }

        const memoRes = await apiClient.request('/api/v1/memos', {
          method: 'POST',
          body: JSON.stringify({
            content: this.content,
            images: imageUrls,
            source: 'mobile',
          }),
        });

        if (memoRes.ok) {
          this.$message.success('创建成功');
          this.content = '';
          this.images = [];
        }
      } catch (error) {
        this.$message.error('创建失败，已保存到本地');
        OfflineQueue.add({
          content: this.content,
          images: this.images,
        });
      } finally {
        this.uploading = false;
      }
    },
  },
};
```

---

## 💻 PC 端 Editor.js 对接

### 1. 安装 Editor.js

```bash
npm install @editorjs/editorjs
npm install @editorjs/header
npm install @editorjs/list
npm install @editorjs/image
```

### 2. 配置 Editor.js

```javascript
// components/Editor.vue
import EditorJS from '@editorjs/editorjs';
import Header from '@editorjs/header';
import List from '@editorjs/list';
import Image from '@editorjs/image';
import { apiClient } from '@/utils/api';

export default {
  props: {
    initialData: {
      type: Object,
      default: null,
    },
    readOnly: {
      type: Boolean,
      default: false,
    },
  },
  data() {
    return {
      editor: null,
    };
  },
  mounted() {
    this.initEditor();
  },
  beforeUnmount() {
    if (this.editor) {
      this.editor.destroy();
    }
  },
  methods: {
    initEditor() {
      this.editor = new EditorJS({
        holder: 'editorjs',
        readOnly: this.readOnly,
        data: this.initialData,
        tools: {
          header: {
            class: Header,
            config: {
              levels: [1, 2, 3],
            },
          },
          list: {
            class: List,
            inlineToolbar: true,
          },
          image: {
            class: Image,
            config: {
              endpoints: {
                byFile: 'http://localhost:8080/api/v1/upload',
              },
              field: 'image',
              types: 'image/*',
              // 请求头必须携带 JWT Token
              additionalRequestHeaders: {
                Authorization: `Bearer ${apiClient.accessToken}`,
              },
            },
          },
        },
        onChange: async () => {
          // 自动保存（可选）
          // await this.save();
        },
      });
    },
    async save() {
      const outputData = await this.editor.save();
      return outputData;
    },
  },
};
```

### 3. 创建/编辑页面

```javascript
// pages/PageEdit.vue
<template>
  <div>
    <input v-model="title" placeholder="页面标题" />
    <input v-model="cover" placeholder="封面URL" />
    <Editor
      ref="editor"
      :initial-data="pageData"
      :read-only="isReadOnly"
    />
    <button @click="handleSave">保存</button>
  </div>
</template>

<script>
import Editor from '@/components/Editor.vue';
import { apiClient } from '@/utils/api';

export default {
  components: { Editor },
  data() {
    return {
      pageId: null,
      title: '',
      cover: '',
      pageData: null,
      isReadOnly: false,
    };
  },
  async mounted() {
    // 检查是否是分享页
    const shareId = this.$route.params.share_id;
    if (shareId) {
      await this.loadSharedPage(shareId);
      this.isReadOnly = true; // 分享页只读
    } else {
      this.pageId = this.$route.params.id;
      if (this.pageId) {
        await this.loadPage();
      }
    }
  },
  methods: {
    async loadPage() {
      const res = await apiClient.request(`/api/v1/pages/${this.pageId}`);
      if (res.ok) {
        const page = await res.json();
        this.title = page.title;
        this.cover = page.cover;
        this.pageData = {
          blocks: page.blocks,
          time: new Date(page.created_at).getTime(),
          version: '2.0',
        };
      }
    },
    async loadSharedPage(shareId) {
      const res = await fetch(
        `http://localhost:8080/api/v1/public/pages/${shareId}`
      );
      if (res.ok) {
        const page = await res.json();
        this.title = page.title;
        this.cover = page.cover;
        this.pageData = {
          blocks: page.blocks,
          time: new Date(page.created_at).getTime(),
          version: '2.0',
        };
      }
    },
    async handleSave() {
      const outputData = await this.$refs.editor.save();
      const res = await apiClient.request('/api/v1/pages', {
        method: 'POST',
        body: JSON.stringify({
          id: this.pageId || undefined,
          title: this.title,
          cover: this.cover,
          blocks: outputData,
        }),
      });
      if (res.ok) {
        const data = await res.json();
        this.$message.success('保存成功');
        if (!this.pageId) {
          this.$router.push(`/pages/${data.id}`);
        }
      }
    },
  },
};
</script>
```

### 4. 只读模式切换

在分享页路由中，自动设置为只读模式：

```javascript
// router/index.js
{
  path: '/s/:share_id',
  component: () => import('@/pages/PageEdit.vue'),
  meta: { readOnly: true },
}
```

---

## 🔗 API 接口总结

### 认证接口
- `POST /auth/login` - 登录
- `POST /auth/refresh` - 刷新 Token
- `POST /auth/register` - 注册

### 上传接口
- `POST /api/v1/upload` - 图片上传（Editor.js 格式）

### Memo 接口
- `POST /api/v1/memos` - 创建闪念

### Page 接口
- `POST /api/v1/pages` - 创建/更新页面
- `GET /api/v1/pages/:id` - 获取页面（混合模式）
- `GET /api/v1/public/pages/:share_id` - 获取公开页面
- `POST /api/v1/pages/:id/share` - 开启/关闭分享

### Timeline 接口
- `GET /api/v1/timeline?limit=20&offset=0` - 获取时间轴

---

## 📝 注意事项

1. **Token 存储**: Access Token 存内存，Refresh Token 存 LocalStorage
2. **图片压缩**: 移动端上传前必须压缩至 500KB 以内
3. **离线支持**: 使用 LocalStorage 队列，网络恢复后自动重试
4. **Editor.js 配置**: Image Tool 必须配置 `additionalRequestHeaders` 携带 JWT
5. **只读模式**: 分享页自动设置 `readOnly: true`，隐藏编辑工具栏
