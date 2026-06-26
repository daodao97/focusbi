<script setup>
// 报表发布版本历史抽屉: 列出历次发布, 可查看某版本内容、回滚到草稿。
import { ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '@/api'
import SqlEditor from '@/components/SqlEditor.vue'

const props = defineProps({
  modelValue: { type: Boolean, default: false }, // 抽屉可见
  reportId: { type: Number, default: 0 }
})
const emit = defineEmits(['update:modelValue', 'rollback'])

const list = ref([])
const loading = ref(false)
const viewing = ref(null)   // 当前查看的版本 {id, content, ...}
const rollingBack = ref(0)

function close() { emit('update:modelValue', false) }

async function load() {
  loading.value = true
  viewing.value = null
  try {
    list.value = await api.listReportVersions(props.reportId)
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    loading.value = false
  }
}

async function viewVersion(v) {
  try {
    viewing.value = await api.getReportVersion(props.reportId, v.id)
  } catch (e) {
    ElMessage.error(e.message)
  }
}

async function rollback(v) {
  try {
    await ElMessageBox.confirm('回滚会把该版本内容载入当前草稿 (不直接上线, 需再发布)。继续?', '回滚确认', { type: 'warning' })
  } catch { return }
  rollingBack.value = v.id
  try {
    await api.rollbackReport(props.reportId, v.id)
    const full = viewing.value && viewing.value.id === v.id ? viewing.value : await api.getReportVersion(props.reportId, v.id)
    emit('rollback', full.content)   // 通知编辑器载入草稿缓冲
    ElMessage.success('已回滚到草稿, 确认后请发布')
    close()
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    rollingBack.value = 0
  }
}

function fmtTime(t) {
  return t ? String(t).replace('T', ' ').slice(0, 19) : ''
}

// 打开时加载
watch(() => props.modelValue, (open) => { if (open && props.reportId) load() })
</script>

<template>
  <el-drawer :model-value="modelValue" @update:model-value="emit('update:modelValue', $event)"
    title="发布版本历史" direction="rtl" size="60%" append-to-body :destroy-on-close="true">
    <div v-loading="loading" class="wrap">
      <el-empty v-if="!list.length" description="暂无发布记录 (发布后产生版本)" />
      <div v-else class="cols">
        <!-- 版本列表 -->
        <el-table :data="list" size="small" highlight-current-row style="max-width:340px"
          @current-change="viewVersion">
          <el-table-column label="时间" min-width="140">
            <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
          </el-table-column>
          <el-table-column prop="user_nick" label="发布人" width="90">
            <template #default="{ row }">{{ row.user_nick || '—' }}</template>
          </el-table-column>
          <el-table-column label="操作" width="80" align="right">
            <template #default="{ row }">
              <el-button link type="primary" size="small" :loading="rollingBack === row.id"
                @click.stop="rollback(row)">回滚</el-button>
            </template>
          </el-table-column>
        </el-table>

        <!-- 选中版本内容预览 -->
        <div class="preview">
          <div v-if="!viewing" class="hint">点击左侧版本查看内容</div>
          <SqlEditor v-else :model-value="viewing.content || ''" readonly height="100%" />
        </div>
      </div>
    </div>
  </el-drawer>
</template>

<style scoped>
.wrap { height: 100%; }
.cols { display: flex; gap: 12px; height: 100%; }
.preview { flex: 1; min-width: 0; border: 1px solid var(--el-border-color); border-radius: 6px; overflow: hidden; }
.hint { color: var(--el-text-color-secondary); font-size: 13px; padding: 16px; }
</style>
