<script setup>
import { computed, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Check } from '@element-plus/icons-vue'
import { api } from '@/api'

const loading = ref(false)
const saving = ref(false)
const ready = ref(false)
const mode = ref('off')
const allowlist = ref('')
const queryTimeout = ref('3m')
const queryConcurrency = ref(8)
const scriptTimeout = ref('3m')
const scheduleEnabled = ref(true)
const publicShareEnabled = ref(true)
const sources = ref({})
const baseline = ref({})

const settingKeys = [
  'engine.query_timeout',
  'engine.query_concurrency',
  'engine.script_timeout',
  'schedule.enabled',
  'security.public_share_enabled',
  'engine.script_fetch',
]

const effectiveFetch = computed(() => {
  if (mode.value !== 'allowlist') return mode.value
  return allowlist.value
    .split(/[\n,]+/)
    .map(v => v.trim())
    .filter(Boolean)
    .join(',')
})

const dynamicCount = computed(() => Object.values(sources.value).filter(v => v === 'database').length)
const defaultCount = computed(() => settingKeys.length - dynamicCount.value)
const payload = computed(() => ({
  script_fetch: effectiveFetch.value,
  query_timeout: queryTimeout.value,
  query_concurrency: queryConcurrency.value,
  script_timeout: scriptTimeout.value,
  schedule_enabled: scheduleEnabled.value,
  public_share_enabled: publicShareEnabled.value,
}))
const changedPayload = computed(() => Object.fromEntries(
  Object.entries(payload.value).filter(([key, value]) => value !== baseline.value[key])
))
const dirty = computed(() => ready.value && Object.keys(changedPayload.value).length > 0)

function applyFetch(value) {
  const normalized = String(value || 'off').trim()
  if (normalized === 'off' || normalized === 'on') {
    mode.value = normalized
    allowlist.value = ''
  } else {
    mode.value = 'allowlist'
    allowlist.value = normalized.split(',').map(v => v.trim()).filter(Boolean).join('\n')
  }
}

function applySettings(data) {
  applyFetch(data.script_fetch)
  queryTimeout.value = data.query_timeout || '3m'
  queryConcurrency.value = Number(data.query_concurrency) || 8
  scriptTimeout.value = data.script_timeout || '3m'
  scheduleEnabled.value = data.schedule_enabled !== false
  publicShareEnabled.value = data.public_share_enabled !== false
  sources.value = data.sources || {}
}

function sourceLabel(key) {
  return sources.value[key] === 'database' ? '动态配置' : '默认值'
}

function isDynamic(key) {
  return sources.value[key] === 'database'
}

async function load() {
  loading.value = true
  ready.value = false
  try {
    applySettings(await api.getSystemSettings())
    baseline.value = { ...payload.value }
    ready.value = true
  } catch (e) {
    ElMessage.error(e.message || '加载失败')
  } finally {
    loading.value = false
  }
}

async function save() {
  if (mode.value === 'allowlist' && !effectiveFetch.value) {
    ElMessage.warning('请至少填写一个允许访问的 URL')
    return
  }
  saving.value = true
  try {
    const data = await api.updateSystemSettings(changedPayload.value)
    applySettings(data)
    baseline.value = { ...payload.value }
    ElMessage.success('系统设置已生效')
  } catch (e) {
    ElMessage.error(e.message || '保存失败')
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="settings-page" v-loading="loading">
    <header class="page-head">
      <div>
        <h2>系统设置</h2>
        <div class="source-summary">
          <span><i class="source-dot dynamic"></i>{{ dynamicCount }} 项动态配置</span>
          <span><i class="source-dot"></i>{{ defaultCount }} 项默认值</span>
        </div>
      </div>
      <el-button type="primary" :icon="Check" :loading="saving" :disabled="!dirty" @click="save">
        保存更改
      </el-button>
    </header>

    <main class="settings-content">
      <section class="settings-section">
        <div class="section-head">
          <span class="section-index">01</span>
          <h3>执行引擎</h3>
        </div>
        <div class="setting-list">
          <div class="setting-row">
            <div class="setting-name">
              <span>SQL 查询超时</span>
              <small :class="{ dynamic: isDynamic('engine.query_timeout') }">
                <i class="source-dot"></i>{{ sourceLabel('engine.query_timeout') }}
              </small>
            </div>
            <div class="setting-control short-control">
              <el-input v-model.trim="queryTimeout" placeholder="3m" />
            </div>
          </div>
          <div class="setting-row">
            <div class="setting-name">
              <span>SQL 并发数</span>
              <small :class="{ dynamic: isDynamic('engine.query_concurrency') }">
                <i class="source-dot"></i>{{ sourceLabel('engine.query_concurrency') }}
              </small>
            </div>
            <div class="setting-control short-control">
              <el-input-number v-model="queryConcurrency" :min="1" :max="64" controls-position="right" />
            </div>
          </div>
          <div class="setting-row">
            <div class="setting-name">
              <span>脚本执行超时</span>
              <small :class="{ dynamic: isDynamic('engine.script_timeout') }">
                <i class="source-dot"></i>{{ sourceLabel('engine.script_timeout') }}
              </small>
            </div>
            <div class="setting-control short-control">
              <el-input v-model.trim="scriptTimeout" placeholder="3m" />
            </div>
          </div>
        </div>
      </section>

      <section class="settings-section">
        <div class="section-head">
          <span class="section-index">02</span>
          <h3>功能开关</h3>
        </div>
        <div class="setting-list">
          <div class="setting-row">
            <div class="setting-name">
              <span>定时任务调度</span>
              <small :class="{ dynamic: isDynamic('schedule.enabled') }">
                <i class="source-dot"></i>{{ sourceLabel('schedule.enabled') }}
              </small>
            </div>
            <div class="setting-control switch-control">
              <el-switch v-model="scheduleEnabled" inline-prompt active-text="开" inactive-text="关" />
            </div>
          </div>
          <div class="setting-row">
            <div class="setting-name">
              <span>公开链接分享</span>
              <small :class="{ dynamic: isDynamic('security.public_share_enabled') }">
                <i class="source-dot"></i>{{ sourceLabel('security.public_share_enabled') }}
              </small>
            </div>
            <div class="setting-control switch-control">
              <el-switch v-model="publicShareEnabled" inline-prompt active-text="开" inactive-text="关" />
            </div>
          </div>
        </div>
      </section>

      <section class="settings-section">
        <div class="section-head">
          <span class="section-index">03</span>
          <h3>脚本网络访问</h3>
        </div>
        <div class="setting-list">
          <div class="setting-row setting-row-top">
            <div class="setting-name">
              <span>fetch()</span>
              <small :class="{ dynamic: isDynamic('engine.script_fetch') }">
                <i class="source-dot"></i>{{ sourceLabel('engine.script_fetch') }}
              </small>
            </div>
            <div class="setting-control network-control">
              <el-radio-group v-model="mode">
                <el-radio-button value="off">禁用</el-radio-button>
                <el-radio-button value="on">仅公网</el-radio-button>
                <el-radio-button value="allowlist">URL 白名单</el-radio-button>
              </el-radio-group>
              <el-input v-if="mode === 'allowlist'" v-model="allowlist" type="textarea" :rows="6"
                placeholder="https://api.example.com/v1&#10;http://10.0.0.20:8080/api" />
            </div>
          </div>
        </div>
      </section>
    </main>
  </div>
</template>

<style scoped>
.settings-page { width: 100%; padding: 4px; box-sizing: border-box; }
.page-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 24px; margin-bottom: 28px; }
.page-head h2 { margin: 0 0 8px; font-size: 24px; line-height: 32px; letter-spacing: 0; }
.source-summary { display: flex; align-items: center; flex-wrap: wrap; gap: 16px; color: var(--el-text-color-secondary); font-size: 12px; }
.source-summary span { display: inline-flex; align-items: center; gap: 6px; }
.source-dot { display: inline-block; width: 6px; height: 6px; flex: none; border-radius: 50%; background: var(--el-border-color-darker); }
.source-dot.dynamic,
.setting-name small.dynamic .source-dot { background: var(--el-color-primary); }
.settings-section { display: grid; grid-template-columns: 150px minmax(0, 1fr); border-top: 1px solid var(--el-border-color); }
.settings-section:last-child { border-bottom: 1px solid var(--el-border-color); }
.section-head { display: flex; align-items: baseline; gap: 12px; padding: 22px 20px 22px 0; }
.section-head h3 { margin: 0; font-size: 15px; line-height: 22px; letter-spacing: 0; }
.section-index { color: var(--el-text-color-placeholder); font-size: 11px; font-variant-numeric: tabular-nums; }
.setting-list { min-width: 0; border-left: 1px solid var(--el-border-color-light); }
.setting-row { display: grid; grid-template-columns: minmax(190px, 34%) minmax(0, 1fr); align-items: center; min-height: 76px; padding: 14px 0 14px 28px; box-sizing: border-box; }
.setting-row + .setting-row { border-top: 1px solid var(--el-border-color-lighter); }
.setting-row-top { align-items: start; padding-top: 22px; padding-bottom: 22px; }
.setting-name { display: flex; flex-direction: column; align-items: flex-start; gap: 5px; min-width: 0; padding-right: 20px; color: var(--el-text-color-primary); font-size: 14px; line-height: 20px; }
.setting-name small { display: inline-flex; align-items: center; gap: 6px; color: var(--el-text-color-placeholder); font-size: 11px; line-height: 16px; }
.setting-name small.dynamic { color: var(--el-color-primary); }
.setting-control { min-width: 0; }
.short-control { width: min(100%, 280px); }
.short-control :deep(.el-input-number) { width: 100%; }
.switch-control { display: flex; align-items: center; min-height: 32px; }
.switch-control :deep(.el-switch) { --el-switch-on-color: var(--el-color-primary); }
.network-control { display: flex; flex-direction: column; align-items: flex-start; gap: 14px; width: min(100%, 560px); }
.network-control :deep(.el-textarea__inner) { resize: vertical; min-height: 128px !important; }
@media (max-width: 760px) {
  .settings-page { padding: 0; }
  .page-head { align-items: center; margin-bottom: 22px; }
  .page-head h2 { font-size: 21px; line-height: 28px; }
  .settings-section { display: block; }
  .section-head { padding: 18px 0 10px; }
  .setting-list { border-left: 0; }
  .setting-row { grid-template-columns: minmax(130px, 42%) minmax(0, 1fr); padding-left: 0; min-height: 70px; }
  .setting-row-top { display: block; }
  .setting-row-top .setting-name { margin-bottom: 14px; }
  .short-control { width: 100%; }
  .network-control { width: 100%; }
}
@media (max-width: 480px) {
  .page-head { align-items: flex-start; gap: 12px; }
  .page-head .el-button { padding-left: 12px; padding-right: 12px; }
  .source-summary { display: grid; gap: 3px; }
  .setting-row { display: block; padding: 16px 0; }
  .setting-name { flex-direction: row; align-items: center; justify-content: space-between; gap: 12px; padding-right: 0; margin-bottom: 10px; }
  .setting-name small { white-space: nowrap; }
  :deep(.el-radio-group) { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); width: 100%; }
  :deep(.el-radio-button__inner) { width: 100%; padding-left: 8px; padding-right: 8px; }
}
</style>
