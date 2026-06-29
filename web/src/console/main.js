import { createApp } from 'vue'
import { createRouter, createWebHashHistory } from 'vue-router'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import 'element-plus/theme-chalk/dark/css-vars.css'
import '@/theme'
import '@/styles.css'
import App from './App.vue'
import { getToken, setUnauthorizedHandler } from '@/api'
import { elementLocale } from '@/locale'

const routes = [
  { path: '/login', name: 'login', component: () => import('./views/Login.vue'), meta: { public: true } },
  { path: '/', redirect: '/reports' },
  { path: '/reports', name: 'reports', component: () => import('./views/ReportList.vue') },
  { path: '/reports/new', name: 'report-new', component: () => import('./views/ReportEdit.vue') },
  { path: '/reports/:id', name: 'report-view', component: () => import('./views/ReportView.vue'), props: true },
  { path: '/reports/:id/edit', name: 'report-edit', component: () => import('./views/ReportEdit.vue'), props: true },
  { path: '/dsn', name: 'dsn', component: () => import('./views/DsnList.vue') },
  { path: '/tokens', name: 'tokens', component: () => import('./views/TokenList.vue') },
  { path: '/schedules', name: 'schedules', component: () => import('./views/ScheduleList.vue') },
  { path: '/users', name: 'users', component: () => import('./views/UserList.vue') },
  { path: '/roles', name: 'roles', component: () => import('./views/RoleList.vue') }
]

const router = createRouter({ history: createWebHashHistory(), routes })

// 路由守卫: 未登录跳登录页
router.beforeEach((to) => {
  if (to.meta.public) return true
  if (!getToken()) return { name: 'login', query: { redirect: to.fullPath } }
  return true
})

// 401 时跳登录
setUnauthorizedHandler(() => {
  if (router.currentRoute.value.name !== 'login') {
    router.push({ name: 'login' })
  }
})

createApp(App).use(router).use(ElementPlus, { locale: elementLocale }).mount('#app')
