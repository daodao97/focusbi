<script setup>
import { useRoute, useRouter } from 'vue-router'
import { ref, computed, watch, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Fold, Expand, SwitchButton, DataAnalysis, Document, Coin, User, Setting, Bell, Key, Moon, Sunny } from '@element-plus/icons-vue'
import { api, clearToken, getToken } from '@/api'
import { buildTree } from '@/tree'
import { perm, loadPerm, clearPerm, canManageReports } from '@/perm'
import { useTheme } from '@/theme'
import ReportTreeMenu from './components/ReportTreeMenu.vue'

const { mode: themeMode, isDark, cycle: cycleTheme } = useTheme()
// 图标用日月样式: 当前暗色显示月亮, 亮色显示太阳 (反映实际外观)
const themeIcon = computed(() => isDark.value ? Moon : Sunny)
const themeLabel = computed(() => ({ auto: '跟随系统', light: '浅色', dark: '深色' }[themeMode.value] || '主题'))

const route = useRoute()
const router = useRouter()
const reports = ref([])

// 侧边栏收起状态 (记住偏好); 移动端无保存偏好时默认收起 (230px 侧栏会挤垮窄屏)。
const asidePref = localStorage.getItem('focusbi_aside_collapsed')
const collapsed = ref(asidePref === '1' || (asidePref === null && window.innerWidth <= 768))
function toggleAside() {
  collapsed.value = !collapsed.value
  localStorage.setItem('focusbi_aside_collapsed', collapsed.value ? '1' : '0')
}

const isLoginPage = computed(() => route.name === 'login')

// 组装报表树 (文件夹 + 报表)
const reportTree = computed(() => buildTree(reports.value))

async function loadReports() {
  if (isLoginPage.value || !getToken()) return
  reports.value = await api.listReports().catch(() => [])
}

async function loadMe() {
  if (isLoginPage.value || !getToken()) return
  await loadPerm()
}

const isAdmin = computed(() => perm.isAdmin)
const canManageSub = computed(() => canManageReports())
const userName = computed(() => perm.user?.nick || perm.user?.username || '')

// 折叠态显示的顶层导航 (图标 + 跳转)
const navItems = computed(() => {
  const items = [
    { path: '/reports', label: '报表', icon: DataAnalysis },
    { path: '/dsn', label: '数据源', icon: Coin },
    { path: '/tokens', label: 'MCP 令牌', icon: Key }
  ]
  if (canManageSub.value) {
    items.push({ path: '/subscriptions', label: '订阅管理', icon: Bell })
  }
  if (isAdmin.value) {
    items.push({ path: '/users', label: '用户管理', icon: User })
    items.push({ path: '/roles', label: '角色管理', icon: Setting })
  }
  return items
})

function goNav(path) { router.push(path) }
function navActive(path) {
  if (path === '/reports') return route.path.startsWith('/reports')
  return route.path.startsWith(path)
}

// 当前选中项高亮
const active = computed(() => {
  if (route.path.startsWith('/dsn')) return '/dsn'
  if (route.path.startsWith('/subscriptions')) return '/subscriptions'
  if (route.path.startsWith('/users')) return '/users'
  if (route.path.startsWith('/roles')) return '/roles'
  if ((route.name === 'report-view' || route.name === 'report-edit') && route.params.id) {
    return `/reports/${route.params.id}`
  }
  if (route.name === 'report-new') return '/reports/new'
  return '/reports'
})

async function logout() {
  await api.logout().catch(() => {})
  clearToken()
  clearPerm()
  ElMessage.success('已退出')
  router.push({ name: 'login' })
}

watch(() => route.fullPath, () => { loadReports(); if (!perm.loaded) loadMe() })
onMounted(() => { loadReports(); loadMe() })
</script>

<template>
  <!-- 登录页: 不套后台框架 -->
  <router-view v-if="isLoginPage" />

  <el-container v-else class="layout">
    <!-- 收起态: 窄栏, 顶层导航图标 -->
    <div v-if="collapsed" class="aside-mini">
      <el-icon class="mini-logo"><DataAnalysis /></el-icon>
      <el-tooltip content="展开侧边栏" placement="right">
        <el-icon class="mini-btn" @click="toggleAside"><Expand /></el-icon>
      </el-tooltip>
      <div class="mini-nav">
        <el-tooltip v-for="it in navItems" :key="it.path" :content="it.label" placement="right">
          <el-icon class="mini-nav-item" :class="{ active: navActive(it.path) }" @click="goNav(it.path)">
            <component :is="it.icon" />
          </el-icon>
        </el-tooltip>
      </div>
      <el-tooltip :content="`主题: ${themeLabel}`" placement="right">
        <el-icon class="mini-btn mini-theme" @click="cycleTheme"><component :is="themeIcon" /></el-icon>
      </el-tooltip>
      <el-tooltip content="退出" placement="right">
        <el-icon class="mini-btn" @click="logout"><SwitchButton /></el-icon>
      </el-tooltip>
    </div>

    <!-- 展开态 -->
    <el-aside v-else width="230px" class="aside">
      <div class="logo">
        <span class="logo-brand"><el-icon><DataAnalysis /></el-icon><span>FocusBI</span></span>
        <el-tooltip content="收起侧边栏" placement="right">
          <el-icon class="collapse-btn" @click="toggleAside"><Fold /></el-icon>
        </el-tooltip>
      </div>
      <el-menu :default-active="active" router unique-opened>
        <el-sub-menu index="reports-group">
          <template #title><el-icon><DataAnalysis /></el-icon><span>报表</span></template>
          <ReportTreeMenu :nodes="reportTree" />
        </el-sub-menu>
        <el-menu-item index="/reports"><el-icon><Document /></el-icon><span>全部报表</span></el-menu-item>
        <el-menu-item index="/dsn"><el-icon><Coin /></el-icon><span>数据源</span></el-menu-item>
        <el-menu-item index="/tokens"><el-icon><Key /></el-icon><span>MCP 令牌</span></el-menu-item>
        <el-menu-item v-if="canManageSub" index="/subscriptions"><el-icon><Bell /></el-icon><span>订阅管理</span></el-menu-item>
        <template v-if="isAdmin">
          <el-menu-item index="/users"><el-icon><User /></el-icon><span>用户管理</span></el-menu-item>
          <el-menu-item index="/roles"><el-icon><Setting /></el-icon><span>角色管理</span></el-menu-item>
        </template>
      </el-menu>
      <div class="user-bar">
        <span class="uname">{{ userName }}</span>
        <div class="bar-actions">
          <el-tooltip :content="`主题: ${themeLabel}`" placement="top">
            <el-icon class="bar-btn" @click="cycleTheme"><component :is="themeIcon" /></el-icon>
          </el-tooltip>
          <el-tooltip content="退出" placement="top">
            <el-icon class="bar-btn" @click="logout"><SwitchButton /></el-icon>
          </el-tooltip>
        </div>
      </div>
    </el-aside>
    <el-main class="main">
      <router-view />
    </el-main>
  </el-container>
</template>

<style scoped>
.layout { height: 100vh; }
.aside { background: #1f2430; color: #fff; overflow-y: auto; display: flex; flex-direction: column; }
.logo { font-size: 17px; font-weight: 600; padding: 18px 20px; color: #fff; display: flex; align-items: center; justify-content: space-between; }
.logo-brand { display: inline-flex; align-items: center; gap: 8px; }
.collapse-btn { cursor: pointer; color: #aeb4c0; font-size: 18px; }
.collapse-btn:hover { color: #fff; }

/* 收起态窄栏 */
.aside-mini { width: 56px; flex: none; background: #1f2430; color: #fff; display: flex; flex-direction: column; align-items: center; padding: 14px 8px; box-sizing: border-box; gap: 10px; }
.mini-logo { width: 36px; height: 36px; display: flex; align-items: center; justify-content: center; font-size: 20px; line-height: 1; }
.mini-nav { width: 100%; display: flex; flex: 1; flex-direction: column; align-items: center; gap: 8px; padding-top: 6px; }
.mini-btn,
.mini-nav-item { width: 36px; height: 36px; flex: none; display: inline-flex; align-items: center; justify-content: center; border-radius: 8px; color: #aeb4c0; font-size: 18px; cursor: pointer; transition: background-color .15s ease, color .15s ease; }
.mini-btn:hover,
.mini-nav-item:hover { background: #2c3342; color: #fff; }
.mini-nav-item.active { background: #3a72ff; color: #fff; }
.mini-theme { margin-top: auto; }
.aside :deep(.el-menu) { background: transparent; border-right: none; flex: 1; }
.aside :deep(.el-menu-item),
.aside :deep(.el-sub-menu__title) { color: #aeb4c0; }
.aside :deep(.el-menu-item.is-active) { color: #fff; background: #3a72ff; }
.aside :deep(.el-menu-item:hover),
.aside :deep(.el-sub-menu__title:hover) { background: #2c3342; color: #fff; }
.report-item { font-size: 13px; }
.report-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.user-bar { padding: 12px 20px; border-top: 1px solid #313846; display: flex; align-items: center; justify-content: space-between; font-size: 13px; color: #aeb4c0; }
.uname { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.bar-actions { display: flex; align-items: center; gap: 6px; flex: none; }
.bar-btn { width: 30px; height: 30px; display: inline-flex; align-items: center; justify-content: center; border-radius: 6px; color: #aeb4c0; font-size: 17px; cursor: pointer; transition: background-color .15s ease, color .15s ease; }
.bar-btn:hover { background: #2c3342; color: #fff; }
/* 内容区背景: EP 页面背景变量, 亮色浅灰 / 暗色自动变深 */
.main { background: var(--el-bg-color-page); padding: 20px; overflow-y: auto; }
</style>
