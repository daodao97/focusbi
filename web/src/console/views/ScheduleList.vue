<script setup>
// 任务管理页 (需报表写权限): 列出用户可写报表的定时任务, 可启停/编辑/删除/测试/新增。
import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '@/api'
import { canWriteAnyReport, canWriteReport } from '@/perm'
import ScheduleForm from '../components/ScheduleForm.vue'

const canManage = computed(() => canWriteAnyReport())
const list = ref([])
const reports = ref([])     // 可选报表 (新增时选)
const loading = ref(false)
const formVisible = ref(false)
const editing = ref(null)
const testing = ref(0)

async function load() {
  loading.value = true
  try {
    list.value = await api.listAllSchedules()
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    loading.value = false
  }
}

async function loadReports() {
  try {
    const all = await api.listReports()
    // 新建任务只能选用户有写权限的报表 (与后端 report.{id}:w 一致)。
    const parents = {}
    for (const x of all || []) parents[x.id] = x.parent_id
    reports.value = (all || []).filter(r => r.type !== 'folder' && canWriteReport(r.id, parents))
  } catch {
    reports.value = []
  }
}

function openCreate() { editing.value = null; formVisible.value = true }
function openEdit(row) { editing.value = row; formVisible.value = true }

async function toggle(row) {
  try {
    await api.updateSchedule(row.report_id, row.id, { enabled: !row.enabled })
    row.enabled = !row.enabled
  } catch (e) {
    ElMessage.error(e.message)
  }
}

async function remove(row) {
  try {
    await ElMessageBox.confirm(`删除「${row.report_name || ''} · ${row.name || row.cron}」?`, '确认', { type: 'warning' })
  } catch { return }
  try {
    await api.deleteSchedule(row.report_id, row.id)
    await load()
  } catch (e) {
    ElMessage.error(e.message)
  }
}

async function test(row) {
  testing.value = row.id
  try {
    const r = await api.testSchedule(row.report_id, row.id)
    if (r && r.triggered === false) ElMessage.info(r.message || '条件未命中, 未推送')
    else if (row.action === 'none') ElMessage.success('已执行 (只跑不推)')
    else ElMessage.success('已推送, 请到群里查看')
  } catch (e) {
    ElMessage.error('执行失败: ' + e.message)
  } finally {
    testing.value = 0
  }
}

const channelLabel = (c) => (c === 'wework' ? '企业微信' : '飞书')

onMounted(() => { load(); loadReports() })
</script>

<template>
  <div class="page">
    <div class="head">
      <div>
        <h2 class="title">任务管理</h2>
        <p class="desc">全站报表的定时任务推送 (飞书 / 企业微信)。到点自动跑报表并推送结果。</p>
      </div>
      <el-button v-if="canManage" type="primary" @click="openCreate">新增任务</el-button>
    </div>

    <el-table v-loading="loading" :data="list" empty-text="暂无任务" border>
      <el-table-column prop="report_name" label="报表" min-width="140">
        <template #default="{ row }">{{ row.report_name || `#${row.report_id}` }}</template>
      </el-table-column>
      <el-table-column prop="name" label="任务名" min-width="120">
        <template #default="{ row }">{{ row.name || '—' }}</template>
      </el-table-column>
      <el-table-column prop="cron" label="cron" width="120" />
      <el-table-column label="类型" width="80">
        <template #default="{ row }">
          <el-tag :type="row.action === 'none' ? '' : (row.condition ? 'warning' : 'info')" size="small" effect="plain">
            {{ row.action === 'none' ? '只跑' : (row.condition ? '告警' : '定时') }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="渠道" width="90">
        <template #default="{ row }">{{ row.action === 'none' ? '—' : channelLabel(row.channel) }}</template>
      </el-table-column>
      <el-table-column label="启用" width="70">
        <template #default="{ row }">
          <el-switch :model-value="row.enabled" :disabled="!canManage" @click="toggle(row)" />
        </template>
      </el-table-column>
      <el-table-column label="上次结果" min-width="140">
        <template #default="{ row }">
          <span :class="['last', { err: row.last_status && row.last_status !== 'ok' }]">
            {{ row.last_status || '—' }}
          </span>
        </template>
      </el-table-column>
      <el-table-column label="上次运行" width="160">
        <template #default="{ row }">{{ row.last_run_at || '—' }}</template>
      </el-table-column>
      <el-table-column v-if="canManage" label="操作" width="180" align="right" fixed="right">
        <template #default="{ row }">
          <el-button link size="small" :loading="testing === row.id" @click="test(row)">测试</el-button>
          <el-button link size="small" @click="openEdit(row)">编辑</el-button>
          <el-button link size="small" type="danger" @click="remove(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <ScheduleForm v-model="formVisible" selectable :reports="reports" :edit="editing" @saved="load" />
  </div>
</template>

<style scoped>
.page { padding: 4px 2px; }
.head { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 16px; }
.title { margin: 0 0 4px; font-size: 18px; }
.desc { margin: 0; font-size: 13px; color: var(--el-text-color-secondary); }
.last { font-size: 12px; color: var(--el-text-color-regular); }
.last.err { color: var(--el-color-danger); }
</style>
