<template>
  <div class="min-h-screen flex items-center justify-center bg-white">
    <div class="w-full max-w-sm">
      <!-- Logo/标题区域 - 极客风格 -->
      <div class="text-center mb-12">
        <h1 class="text-3xl font-bold text-gray-900 mb-2 tracking-tight">
          Notion-Lite
        </h1>
        <p class="text-sm text-gray-500 font-mono">
          轻量级笔记应用
        </p>
      </div>

      <!-- 登录表单 - 扁平化设计 -->
      <form @submit.prevent="handleLogin" class="space-y-6">
        <div class="space-y-4">
          <div>
            <input
              v-model="email"
              type="email"
              required
              placeholder="邮箱"
              class="input text-sm"
              autocomplete="email"
            />
          </div>
          <div>
            <input
              v-model="password"
              type="password"
              required
              placeholder="密码"
              class="input text-sm"
              autocomplete="current-password"
            />
          </div>
        </div>

        <!-- 错误提示 -->
        <div v-if="error" class="text-xs text-red-600 font-mono">
          {{ error }}
        </div>

        <!-- 登录按钮 -->
        <button
          type="submit"
          :disabled="loading"
          class="btn btn-primary w-full"
        >
            <span v-if="!loading">登录</span>
            <span v-else class="flex items-center gap-2">
              <svg class="animate-spin h-4 w-4" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
              </svg>
              登录中...
            </span>
        </button>

        <!-- 注册链接 -->
        <div class="text-center">
          <router-link
            to="/register"
            class="text-xs text-gray-500 hover:text-gray-900 transition-colors font-mono"
          >
            → 注册
          </router-link>
        </div>
      </form>

      <!-- 底部信息 -->
      <div class="mt-12 text-center">
        <p class="text-xs text-gray-400 font-mono">
          v1.0.0
        </p>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()

const email = ref('')
const password = ref('')
const loading = ref(false)
const error = ref('')

async function handleLogin() {
  error.value = ''
  loading.value = true

  try {
    await authStore.login(email.value, password.value)
    const redirect = route.query.redirect || '/'
    router.push(redirect)
  } catch (err) {
    error.value = err.response?.data?.error || '登录失败'
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
/* 极客风格的输入框焦点效果 */
.input:focus {
  border-bottom-color: #000;
  border-bottom-width: 2px;
  padding-bottom: calc(0.5rem - 1px);
}
</style>
