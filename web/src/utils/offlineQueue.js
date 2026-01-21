/**
 * 离线队列管理
 * 用于在离线状态下保存 Memo，网络恢复后自动同步
 */
const OFFLINE_QUEUE_KEY = 'offline_memo_queue'

export class OfflineQueue {
  /**
   * 添加 Memo 到离线队列
   * @param {Object} memo - Memo 数据 { content, images: File[], source }
   */
  static add(memo) {
    const queue = this.getQueue()
    queue.push({
      ...memo,
      timestamp: Date.now(),
      retryCount: 0,
    })
    localStorage.setItem(OFFLINE_QUEUE_KEY, JSON.stringify(queue))
  }

  /**
   * 获取离线队列
   * @returns {Array} 队列数组
   */
  static getQueue() {
    const data = localStorage.getItem(OFFLINE_QUEUE_KEY)
    return data ? JSON.parse(data) : []
  }

  /**
   * 清空离线队列
   */
  static clear() {
    localStorage.removeItem(OFFLINE_QUEUE_KEY)
  }

  /**
   * 处理离线队列，尝试同步到服务器
   * @param {Object} apiClient - API 客户端实例
   * @returns {Promise<void>}
   */
  static async processQueue(apiClient) {
    const queue = this.getQueue()
    if (queue.length === 0) {
      return
    }

    const failed = []

    for (const item of queue) {
      try {
        // 上传图片（如果有）
        const imageUrls = []
        if (item.images && item.images.length > 0) {
          for (const imageFile of item.images) {
            const formData = new FormData()
            formData.append('image', imageFile)
            
            const uploadRes = await apiClient.post('/api/v1/upload', formData, {
              headers: {
                'Content-Type': 'multipart/form-data',
              },
            })
            
            if (uploadRes.data.success === 1) {
              imageUrls.push(uploadRes.data.file.url)
            }
          }
        }

        // 创建 Memo
        const memoRes = await apiClient.post('/api/v1/memos', {
          content: item.content,
          images: imageUrls,
          source: item.source || 'mobile',
        })

        if (memoRes.status === 201) {
          console.log('离线 Memo 同步成功:', item)
        } else {
          failed.push(item)
        }
      } catch (error) {
        console.error('离线 Memo 同步失败:', error)
        item.retryCount = (item.retryCount || 0) + 1
        // 重试次数小于 3 次时保留
        if (item.retryCount < 3) {
          failed.push(item)
        }
      }
    }

    // 更新队列
    if (failed.length > 0) {
      localStorage.setItem(OFFLINE_QUEUE_KEY, JSON.stringify(failed))
    } else {
      this.clear()
    }
  }
}

// 监听网络状态，自动处理队列
if (typeof window !== 'undefined' && 'serviceWorker' in navigator) {
  window.addEventListener('online', () => {
    import('@/utils/api').then(({ default: apiClient }) => {
      OfflineQueue.processQueue(apiClient)
    })
  })
}
