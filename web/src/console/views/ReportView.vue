<script setup>
import { ref, reactive, computed, watch, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { VideoPlay, VideoPause } from '@element-plus/icons-vue'
import { api } from '@/api'
import { copyText } from '@/clipboard'
import { canManageReports } from '@/perm'
import { paramsToQuery, queryToParams, sameQuery } from '@/params'
import { useAutoRefresh } from '@/autorefresh'
import ReportFilters from '@/components/ReportFilters.vue'
import ReportBlocks from '@/components/ReportBlocks.vue'
import TimingTooltip from '@/components/TimingTooltip.vue'

const canManage = computed(() => canManageReports())

const props = defineProps({ id: { type: String, default: '' } })
const route = useRoute()
const router = useRouter()

const report = reactive({ id: 0, name: '', is_public: false, share_token: '' })
const result = ref(null)
const params = ref({})
const loading = ref(false)

// 分享弹窗
const shareDialog = ref(false)
const shareToggling = ref(false)

async function load() {
  const r = await api.getReport(Number(props.id))
  report.id = r.id
  report.name = r.name
  report.is_public = r.is_public
  report.share_token = r.share_token || ''
  // 初始过滤值来自 URL query, 方便分享链接直接复现
  params.value = queryToParams(route.query)
  await run()
}

// 自动刷新倒计时: 报表设置了间隔时, 倒计时归零自动重查 (旁路缓存), 可手动暂停。
const autoRefresh = useAutoRefresh(() => run(true))

async function run(force = false) {
  loading.value = true
  try {
    // force=true (点击"刷新") 旁路查询缓存。
    const p = force === true ? { ...params.value, _nocache: '1' } : params.value
    result.value = await api.runReport(Number(props.id), p)
    autoRefresh.arm(result.value?.auto_refresh)
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    loading.value = false
  }
}

// 过滤值变化 -> 实时同步到 URL query (replace, 不污染历史)
watch(params, (p) => {
  const q = paramsToQuery(p)
  if (!sameQuery(q, route.query)) {
    router.replace({ query: q })
  }
}, { deep: true })

// 公开分享链接 (带当前过滤参数)
const shareUrl = () => {
  if (!report.share_token) return ''
  const base = location.origin + location.pathname.replace(/[^/]*$/, '') + 'view.html'
  const q = new URLSearchParams(paramsToQuery(params.value)).toString()
  return `${base}#/${report.share_token}` + (q ? `?${q}` : '')
}

function openShare() { shareDialog.value = true }

async function toggleShare(enable) {
  shareToggling.value = true
  try {
    const d = await api.setShare(report.id, enable)
    report.is_public = d.is_public
    if (d.share_token) report.share_token = d.share_token
    ElMessage.success(enable ? '已开启公开分享' : '已关闭分享')
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    shareToggling.value = false
  }
}

async function copyShareUrl() {
  if (await copyText(shareUrl())) {
    ElMessage.success('链接已复制')
  } else {
    ElMessage.error('复制失败')
  }
}

function openStandalone() {
  if (report.is_public && report.share_token) window.open(shareUrl(), '_blank')
}

// 切换不同报表 (侧边栏点击) 时重新加载
watch(() => props.id, load)
onMounted(load)
</script>

<template>
  <div>
    <div class="toolbar">
      <div class="title-wrap">
        <h2 class="title">{{ report.name }}</h2>
        <TimingTooltip :timing="result?.timing" scope="report" />
      </div>
      <div class="actions">
        <el-button :loading="loading" @click="run(true)">刷新</el-button>
        <el-button v-if="autoRefresh.enabled.value" type="warning" plain
          :icon="autoRefresh.paused.value ? VideoPlay : VideoPause" @click="autoRefresh.toggle()">
          {{ autoRefresh.paused.value ? '已暂停' : `${autoRefresh.seconds.value} 秒后刷新` }}
        </el-button>
        <el-button v-if="canManage" @click="openShare">
          分享<el-tag v-if="report.is_public" type="success" size="small" effect="plain" style="margin-left:6px">已开启</el-tag>
        </el-button>
        <el-button v-if="canManage" type="primary" @click="router.push(`/reports/${report.id}/edit`)">编辑</el-button>
      </div>
    </div>

    <div class="sheet" v-loading="loading">
      <ReportFilters v-model="params" :filters="result?.filters || []" :loading="loading" @run="run" />
      <ReportBlocks v-if="result" :blocks="result.blocks" />
    </div>

    <!-- 分享弹窗 -->
    <el-dialog v-model="shareDialog" title="公开分享" width="560px">
      <div class="share-row">
        <span>开启公开访问</span>
        <el-switch :model-value="report.is_public" :loading="shareToggling"
          @update:model-value="toggleShare" />
      </div>
      <p class="share-hint">
        开启后, 任何持有下方链接的人无需登录即可查看该报表 (可调过滤器)。关闭即失效。
      </p>
      <template v-if="report.is_public && report.share_token">
        <el-input :model-value="shareUrl()" readonly>
          <template #append>
            <el-button @click="copyShareUrl">复制</el-button>
          </template>
        </el-input>
        <div class="share-actions">
          <el-button link type="primary" @click="openStandalone">在新窗口打开</el-button>
        </div>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.toolbar { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; }
.title-wrap { display: inline-flex; align-items: center; gap: 6px; min-width: 0; }
.title { margin: 0; font-size: 18px; }
.actions { display: flex; gap: 8px; }
.sheet { background: var(--el-bg-color); border-radius: 8px; padding: 24px; min-height: 200px; }
.share-row { display: flex; align-items: center; justify-content: space-between; margin-bottom: 8px; }
.share-hint { font-size: 12px; color: var(--el-text-color-secondary); line-height: 1.6; margin: 0 0 14px; }
.share-actions { margin-top: 8px; text-align: right; }
</style>
