import { ref } from 'vue'

export function useWebSocket() {
  let ws = null
  let messageHandler = null
  const isConnected = ref(false)

  const connect = (onMessage) => {
    if (ws && ws.readyState === WebSocket.OPEN) {
      return
    }

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const wsUrl = `${protocol}//${window.location.host}/ws/comparison`
    
    ws = new WebSocket(wsUrl)

    ws.onopen = () => {
      isConnected.value = true
      console.log('WebSocket connected')
    }

    ws.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data)
        if (onMessage) {
          onMessage(data)
        }
        if (messageHandler) {
          messageHandler(data)
        }
      } catch (error) {
        console.error('Failed to parse WebSocket message:', error)
      }
    }

    ws.onerror = (error) => {
      console.error('WebSocket error:', error)
      isConnected.value = false
    }

    ws.onclose = () => {
      isConnected.value = false
      console.log('WebSocket disconnected')
      // 自动重连
      setTimeout(() => {
        if (messageHandler) {
          connect(messageHandler)
        }
      }, 3000)
    }

    messageHandler = onMessage
  }

  const disconnect = () => {
    if (ws) {
      ws.close()
      ws = null
      messageHandler = null
      isConnected.value = false
    }
  }

  return {
    connect,
    disconnect,
    isConnected,
  }
}
