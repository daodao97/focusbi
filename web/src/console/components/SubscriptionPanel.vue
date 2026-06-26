<script setup>
// 报表订阅推送管理 (报表设置弹窗内的 Tab): 列出 / 新增 / 编辑 / 删除 / 测试 该报表的订阅。
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '@/api'
import SubscriptionForm from './SubscriptionForm.vue'

const props = defineProps({
  reportId: { type: Number, required: true },
  filters: { type: Array, default: () => [] }, // 报表过滤器, 用于填固定参数
  content: { type: String, default: '' },      // 报表内容, 用于条件配置拉 blocks
  dsn: { type: String, default: 'default' }
})

const list = ref([])
const loading = ref(false)
const formVisible = ref(false)
const editing = ref(null)
const testing = ref(0)

async function load() {
  loading.value = true
  try {
    list.value = await api.listSubscriptions(props.reportId)
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    loading.value = false
  }
}

function openCreate() { editing.value = null; formVisible.value = true }
function openEdit(row) { editing.value = row; formVisible.value = true }

async function remove(row) {
  try {
    await ElMessageBox.confirm(`删除订阅「${row.name || row.cron}」?`, '确认', { type: 'warning' })
  } catch { return }
  try {
    await api.deleteSubscription(props.reportId, row.id)
    await load()
  } catch (e) {
    ElMessage.error(e.message)
  }
}

async function test(row) {
  testing.value = row.id
  try {
    const r = await api.testSubscription(props.reportId, row.id)
    if (r && r.triggered === false) ElMessage.info(r.message || '条件未命中, 未推送')
    else ElMessage.success('已推送, 请到群里查看')
  } catch (e) {
    ElMessage.error('推送失败: ' + e.message)
  } finally {
    testing.value = 0
  }
}

const channelLabel = (c) => (c === 'wework' ? '企业微信' : '飞书')

onMounted(load)
</script>

<template>
  <div v-loading="loading">
    <div class="bar">
      <el-button type="primary" size="small" @click="openCreate">新增订阅</el-button>
    </div>

    <el-table :data="list" size="small" empty-text="暂无订阅">
      <el-table-column prop="name" label="名称" min-width="100">
        <template #default="{ row }">{{ row.name || '—' }}</template>
      </el-table-column>
      <el-table-column prop="cron" label="cron" width="110" />
      <el-table-column label="渠道" width="80">
        <template #default="{ row }">{{ channelLabel(row.channel) }}</template>
      </el-table-column>
      <el-table-column label="状态" width="70">
        <template #default="{ row }">
          <el-tag :type="row.enabled ? 'success' : 'info'" size="small" effect="plain">
            {{ row.enabled ? '启用' : '停用' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="上次结果" min-width="120">
        <template #default="{ row }">
          <span :class="['last', { err: row.last_status && row.last_status !== 'ok' }]">
            {{ row.last_status || '—' }}
          </span>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="170" align="right">
        <template #default="{ row }">
          <el-button link size="small" :loading="testing === row.id" @click="test(row)">测试</el-button>
          <el-button link size="small" @click="openEdit(row)">编辑</el-button>
          <el-button link size="small" type="danger" @click="remove(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <SubscriptionForm v-model="formVisible" :report-id="reportId" :filters="filters"
      :content="content" :dsn="dsn" :edit="editing" @saved="load" />
  </div>
</template>

<style scoped>
.bar { margin-bottom: 10px; }
.last { font-size: 12px; color: var(--el-text-color-secondary); }
.last.err { color: var(--el-color-danger); }
</style>
