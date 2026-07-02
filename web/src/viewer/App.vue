<script setup>
import { ref, reactive, watch, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { VideoPlay, VideoPause } from '@element-plus/icons-vue'
import { api } from '@/api'
import { paramsToQuery, queryToParams, sameQuery } from '@/params'
import { useAutoRefresh } from '@/autorefresh'
import ReportFilters from '@/components/ReportFilters.vue'
import ReportBlocks from '@/components/ReportBlocks.vue'
import TimingTooltip from '@/components/TimingTooltip.vue'

// 独立报表查看页 (公开分享, 类似 dataddy /open), 无需登录。
// 形如 view.html#<share_token>?from_month=2026-06-01&to_month=2026-06-30
// hash 承载不可枚举的 share_token 与过滤参数, 便于分享带筛选条件的公开链接。
function parseHash() {
  let h = location.hash.replace(/^#\/?/, '')
  let query = {}
  const qi = h.indexOf('?')
  let token = h
  if (qi >= 0) {
    token = h.slice(0, qi)
    const sp = new URLSearchParams(h.slice(qi + 1))
    for (const [k, v] of sp.entries()) query[k] = v
  }
  return { token: token.trim(), query }
}

const { token, query: initialQuery } = parseHash()
const report = reactive({ name: '' })
const result = ref(null)
const params = ref(queryToParams(initialQuery))
const loading = ref(false)
const notFound = ref(false)

async function load() {
  if (!token) { notFound.value = true; return }
  try {
    const r = await api.publicGetReport(token)
    report.name = r.name
  } catch {
    notFound.value = true
    return
  }
  run()
}

// 自动刷新倒计时: 报表设置了间隔时, 倒计时归零自动重查 (旁路缓存), 可手动暂停。
const autoRefresh = useAutoRefresh(() => run(true))

async function run(force = false) {
  loading.value = true
  try {
    // force=true (点击"刷新") 旁路查询缓存; 不污染同步到 hash 的 params。
    const p = force === true ? { ...params.value, _nocache: '1' } : params.value
    result.value = await api.publicRunReport(token, p)
    autoRefresh.arm(result.value?.auto_refresh)
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    loading.value = false
  }
}

// 过滤值变化 -> 同步回 hash query (不触发刷新)
watch(params, (p) => {
  const q = paramsToQuery(p)
  const sp = new URLSearchParams(q).toString()
  const newHash = `#/${token}` + (sp ? `?${sp}` : '')
  if (location.hash !== newHash) {
    history.replaceState(null, '', newHash)
  }
}, { deep: true })

onMounted(load)
</script>

<template>
  <div class="viewer">
    <header class="top">
      <span class="brand">FocusBI</span>
      <div v-if="report.name" class="title-wrap">
        <h1>{{ report.name }}</h1>
        <TimingTooltip :timing="result?.timing" scope="report" />
      </div>
      <el-button v-if="token" :loading="loading" size="small" @click="run(true)">刷新</el-button>
      <el-button v-if="autoRefresh.enabled.value" size="small" type="warning" plain
        :icon="autoRefresh.paused.value ? VideoPlay : VideoPause" @click="autoRefresh.toggle()">
        {{ autoRefresh.paused.value ? '已暂停' : `${autoRefresh.seconds.value} 秒后刷新` }}
      </el-button>
    </header>

    <main class="content">
      <el-empty v-if="notFound" description="分享链接无效或已关闭" />
      <div v-else class="sheet" v-loading="loading">
        <ReportFilters v-model="params" :filters="result?.filters || []" :loading="loading" @run="run" />
        <!-- 页面级顶部 HTML (report.settings.prepend_content); 由报表作者撰写, 直接渲染 -->
        <div v-if="result?.prepend_content" class="prepend" v-html="result.prepend_content"></div>
        <ReportBlocks v-if="result" :blocks="result.blocks" />
      </div>
    </main>
  </div>
</template>

<style scoped>
.viewer { min-height: 100vh; background: var(--el-bg-color-page); }
.top { display: flex; align-items: center; gap: 16px; padding: 14px 24px; background: var(--el-bg-color); border-bottom: 1px solid var(--el-border-color-light); }
.brand { font-weight: 600; color: var(--el-text-color-primary); }
.title-wrap { display: inline-flex; align-items: center; gap: 6px; flex: 1; min-width: 0; }
.top h1 { font-size: 16px; margin: 0; color: var(--el-text-color-primary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.content { max-width: 1200px; margin: 0 auto; padding: 20px; }
.sheet { background: var(--el-bg-color); border-radius: 8px; padding: 24px; }
/* 长报表切换时 spinner 默认居中在很高的 sheet 内 (落在屏幕外)。用 sticky 让它在 sheet 内
   水平居中 (避免 fixed 按整个视口居中), 纵向随滚动贴住视口中央保持可见。 */
.sheet :deep(.el-loading-spinner) { position: sticky; top: 50%; }
.prepend { margin-bottom: 16px; }

/* 移动端: 收紧内边距, 顶栏允许换行, 内容区表格可横向滚动而非撑破布局。 */
@media (max-width: 640px) {
  .top { flex-wrap: wrap; gap: 8px; padding: 10px 14px; }
  .content { padding: 12px; }
  .sheet { padding: 14px; border-radius: 6px; }
}
</style>
