import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const routes = [
  {
    path: '/',
    name: 'Home',
    component: () => import('@/views/Home.vue'),
    meta: { requiresAuth: false }, // 允许未登录访问
  },
  {
    path: '/login',
    name: 'Login',
    component: () => import('@/views/Login.vue'),
    meta: { requiresGuest: true },
  },
  {
    path: '/register',
    name: 'Register',
    component: () => import('@/views/Register.vue'),
    meta: { requiresGuest: true },
  },
  {
    path: '/memo/create',
    name: 'MemoCreate',
    component: () => import('@/views/MemoCreate.vue'),
    meta: { requiresAuth: false }, // 允许未登录访问
  },
  {
    path: '/memos/:id',
    name: 'MemoDetail',
    component: () => import('@/views/MemoDetail.vue'),
    meta: { requiresAuth: false }, // 允许未登录访问
  },
  {
    path: '/pages',
    name: 'Pages',
    component: () => import('@/views/Pages.vue'),
    meta: { requiresAuth: false }, // 允许未登录访问（但只能看到公开页面）
  },
  {
    path: '/pages/:id',
    name: 'PageEdit',
    component: () => import('@/views/PageEdit.vue'),
    meta: { requiresAuth: false }, // 允许未登录访问（混合模式）
  },
  {
    path: '/pages/new',
    name: 'PageNew',
    component: () => import('@/views/PageEdit.vue'),
    meta: { requiresAuth: false }, // 允许未登录访问
  },
  {
    path: '/s/:share_id',
    name: 'PageShare',
    component: () => import('@/views/PageShare.vue'),
  },
  {
    path: '/search',
    name: 'Search',
    component: () => import('@/views/Search.vue'),
    meta: { requiresAuth: false },
  },
  {
    path: '/trash',
    name: 'Trash',
    component: () => import('@/views/Trash.vue'),
    meta: { requiresAuth: true },
  },
  {
    path: '/tags/:name/timeline',
    name: 'TagTimeline',
    component: () => import('@/views/TagTimeline.vue'),
    meta: { requiresAuth: true },
  },
  {
    path: '/tags',
    name: 'Tags',
    component: () => import('@/views/Tags.vue'),
    meta: { requiresAuth: true },
  },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

// 路由守卫
router.beforeEach((to, from, next) => {
  const authStore = useAuthStore()
  
  // 需要登录的路由
  if (to.meta.requiresAuth && !authStore.isAuthenticated) {
    next({ name: 'Login', query: { redirect: to.fullPath } })
  } 
  // 需要未登录的路由（登录后跳转首页）
  else if (to.meta.requiresGuest && authStore.isAuthenticated) {
    next({ name: 'Home' })
  } 
  // 其他路由允许访问（包括未登录用户）
  else {
    next()
  }
})

export default router
