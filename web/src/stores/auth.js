import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import apiClient from '@/utils/api'

export const useAuthStore = defineStore('auth', () => {
  const accessToken = ref(localStorage.getItem('access_token') || '')
  const refreshToken = ref(localStorage.getItem('refresh_token') || '')
  const user = ref(null)

  const isAuthenticated = computed(() => !!accessToken.value)

  function setTokens(access, refresh) {
    accessToken.value = access
    refreshToken.value = refresh
    localStorage.setItem('access_token', access)
    localStorage.setItem('refresh_token', refresh)
  }

  function clearTokens() {
    accessToken.value = ''
    refreshToken.value = ''
    localStorage.removeItem('access_token')
    localStorage.removeItem('refresh_token')
    user.value = null
  }

  async function login(email, password) {
    try {
      const response = await apiClient.post('/auth/login', {
        email,
        password,
      })
      setTokens(response.data.access_token, response.data.refresh_token)
      return response.data
    } catch (error) {
      throw error
    }
  }

  async function register(email, password) {
    try {
      const response = await apiClient.post('/auth/register', {
        email,
        password,
      })
      return response.data
    } catch (error) {
      throw error
    }
  }

  async function logout() {
    clearTokens()
    apiClient.setAccessToken('')
  }

  return {
    accessToken,
    refreshToken,
    user,
    isAuthenticated,
    login,
    register,
    logout,
    setTokens,
    clearTokens,
  }
})
