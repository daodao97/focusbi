<script setup>
// 可复用的报表编辑器: SQL 编辑 + AI 面板 + 预览切换。
// 由路由编辑页 (ReportEdit) 与列表主从工作区 (ReportList) 共用。
// 保存/发布成功后 emit('saved', id, action), action='save'|'publish', 不自行跳转, 交给宿主决定。
// 宿主据 action 决定是否跳转: 保存草稿应留在编辑器, 发布可跳查看页。
import { ref, reactive, watch, onMounted, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { api } from '@/api'
import { canReadDsnById } from '@/perm'
import AiChat from '@/components/AiChat.vue'
import SqlEditor from '@/components/SqlEditor.vue'
import ReportFilters from '@/components/ReportFilters.vue'
import ReportBlocks from '@/components/ReportBlocks.vue'
import DocDrawer from '@/components/DocDrawer.vue'
import SchedulePanel from './SchedulePanel.vue'
import VersionDrawer from './VersionDrawer.vue'
import { Reading, Setting } from '@element-plus/icons-vue'

const props = defineProps({
  id: { type: [Number, String], default: 0 }, // 报表 id, 0/空 表示新建
  parentId: { type: Number, default: 0 },      // 新建时预置文件夹
  showHeader: { type: Boolean, default: true },      // 是否显示自带工具栏 (标题+预览/保存)
  showBack: { type: Boolean, default: false }        // 是否显示取消/返回按钮 (路由全屏页用)
})
const emit = defineEmits(['saved', 'back'])

const DEFAULT_CONTENT = `\${range|日期|-7 days,today|date_range}

-- @id=示例
-- @chart=__auto__
SELECT 1 AS x, 10 AS y;`

const report = reactive({ id: 0, name: '新报表', dsn: 'default', content: DEFAULT_CONTENT, parent_id: 0 })
const dsnList = ref([])
const params = ref({})
const preview = ref(null)
const previewing = ref(false)
const loading = ref(true) // 初次加载报表数据时的整体过渡 (默认 true, load 完成置 false)
const mode = ref('edit') // edit | preview
const docOpen = ref(false)
const settingsOpen = ref(false)     // 报表设置模态框
const settingsTab = ref('general')  // 设置弹窗当前 Tab
const autoRefresh = ref(0)          // 报表级自动刷新间隔 (秒); 0 关闭。存于 report.settings
const prependContent = ref('')      // 页面顶部注入的原始 HTML; 存于 report.settings
const publishedContent = ref('')    // 已发布版 content (用于判断草稿是否有未发布改动)
const savedSnapshot = ref('')       // 上次保存的可保存状态快照 (判断草稿是否有未保存改动)
const publishing = ref(false)
const versionOpen = ref(false)      // 版本历史抽屉

// 回滚: 把历史版本内容载入草稿缓冲 (后端已写入 dev_content, 这里同步前端)
function onRollback(content) {
  report.content = content
  // 草稿与已发布版必然不同, 提示用户去发布; draftDirty/dirty 自动反映
}
// report.content 在编辑器里是"开发版草稿"缓冲; 已发布版单独存 publishedContent。
// 发布按钮: 当前草稿与已发布版不同时可点 (有未发布改动)。
const dirty = computed(() => report.content !== publishedContent.value)
// 可保存状态快照: content + 元信息任一变化即视为有未保存改动。
function editSnapshot() {
  return JSON.stringify({ c: report.content, n: report.name, d: report.dsn, r: autoRefresh.value, p: prependContent.value })
}
// 保存草稿按钮: 当前编辑状态与上次保存不同时可点。
const draftDirty = computed(() => editSnapshot() !== savedSnapshot.value)
// 报表名不允许为空 (保存/发布的前置条件)。
const nameValid = computed(() => report.name.trim() !== '')
// 发布按钮为何不可点 (供 tooltip 解释, 避免误以为是权限/bug)。可点时为空串。
const publishHint = computed(() => {
  if (!nameValid.value) return '请先填写报表名称'
  if (!dirty.value) return '当前草稿与已发布版一致, 无需发布'
  return ''
})

// 从 settings JSON 串解析出页面级配置
function parseSettings(raw) {
  if (!raw) return {}
  try { return JSON.parse(raw) || {} } catch { return {} }
}

async function load() {
  loading.value = true
  try {
    dsnList.value = await api.listDsn().catch(() => [])
    if (props.id) {
      const r = await api.getReport(Number(props.id))
      Object.assign(report, r)
      // 编辑器编辑开发版草稿; 旧数据 dev_content 为空时回退到 content
      report.content = (r.dev_content != null && r.dev_content !== '') ? r.dev_content : (r.content || '')
      publishedContent.value = r.content || ''
      const s = parseSettings(r.settings)
      autoRefresh.value = Number(s.auto_refresh) || 0
      prependContent.value = s.prepend_content || ''
    } else {
      // 重置为新建态
      Object.assign(report, { id: 0, name: '新报表', dsn: 'default', content: DEFAULT_CONTENT, parent_id: props.parentId || 0, settings: '' })
      publishedContent.value = '' // 新报表未发布
      autoRefresh.value = 0
      prependContent.value = ''
    }
    mode.value = 'edit'
    preview.value = null
    params.value = {}
    savedSnapshot.value = editSnapshot() // 载入即"已保存"基线
  } finally {
    loading.value = false
  }
}

async function doPreview() {
  previewing.value = true
  try {
    preview.value = await api.previewReport({ dsn: report.dsn, content: report.content, params: params.value })
    mode.value = 'preview'
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    previewing.value = false
  }
}

async function rerun() {
  previewing.value = true
  try {
    preview.value = await api.previewReport({ dsn: report.dsn, content: report.content, params: params.value })
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    previewing.value = false
  }
}

function cancelPreview() { mode.value = 'edit' }

// 保存 = 存开发版草稿 (dev_content), 不影响线上发布版。
// silent=true 时不 emit/不提示 (供 publish 内部复用)。
async function save(silent = false) {
  if (!report.name.trim()) { ElMessage.warning('请输入报表名称'); return }
  try {
    const body = { name: report.name, dsn: report.dsn, dev_content: report.content, parent_id: report.parent_id || 0, type: 'report', settings: buildSettings() }
    let id = report.id
    if (id) {
      await api.updateReport(id, body)
    } else {
      const d = await api.createReport(body)
      id = d.id
      report.id = id
    }
    savedSnapshot.value = editSnapshot() // 保存成功 -> 重置"已保存"基线, draftDirty 归零
    if (!silent) {
      ElMessage.success('已保存')
      // action='save': 仅存草稿, 宿主不应跳转 (留在编辑器继续改)
      emit('saved', id, 'save')
    }
    return id
  } catch (e) {
    ElMessage.error(e.message)
  }
}

// 发布 = 把当前草稿同步为发布版, 对查看者生效。先保存再发布。
async function publish() {
  publishing.value = true
  try {
    const id = await save(true) // 静默保存, 不触发跳转
    if (!id) return
    await api.publishReport(id)
    publishedContent.value = report.content // 草稿即发布版, dirty 归零
    ElMessage.success('已发布')
    // action='publish': 已上线, 宿主可跳转到查看页
    emit('saved', id, 'publish')
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    publishing.value = false
  }
}

function onAiUpdate(content) { report.content = content }

// 合并 autoRefresh / prependContent 进 settings JSON, 保留其他已有键 (前向兼容)。
function buildSettings() {
  let obj = {}
  try { obj = report.settings ? JSON.parse(report.settings) : {} } catch { obj = {} }
  const n = Math.max(0, Math.floor(Number(autoRefresh.value) || 0))
  if (n > 0) obj.auto_refresh = n
  else delete obj.auto_refresh
  const html = (prependContent.value || '').trim()
  if (html) obj.prepend_content = html
  else delete obj.prepend_content
  report.settings = JSON.stringify(obj)
  return report.settings
}

// id / parentId 变化 (宿主切换选中报表) 时重新加载
watch(() => [props.id, props.parentId], load)
onMounted(load)

defineExpose({ save, reload: load })
</script>

<template>
  <div class="editor-page" v-loading="loading" element-loading-text="加载中…">
    <div v-if="showHeader && !loading" class="toolbar">
      <div class="left">
        <span class="title">{{ report.name }}</span>
        <el-tag v-if="mode === 'preview'" size="small" type="info" style="margin-left:8px">预览中</el-tag>
        <el-tag v-else-if="dirty" size="small" type="warning" effect="plain" style="margin-left:8px">未发布草稿</el-tag>
      </div>
      <div class="actions">
        <el-button text @click="docOpen = true">
          <el-icon><Reading /></el-icon>
          <span>开发文档</span>
        </el-button>
        <el-button v-if="report.id" text @click="versionOpen = true">历史版本</el-button>
        <template v-if="mode === 'edit'">
          <el-button :loading="previewing" @click="doPreview">预览</el-button>
          <el-button :disabled="!draftDirty || !nameValid" @click="save">保存</el-button>
          <el-tooltip :content="publishHint" :disabled="!publishHint" placement="top">
            <span>
              <el-button type="primary" :disabled="!dirty || !nameValid" :loading="publishing"
                :style="(!dirty || !nameValid) ? 'pointer-events:none' : ''" @click="publish">发布</el-button>
            </span>
          </el-tooltip>
        </template>
        <template v-else>
          <el-button @click="cancelPreview">取消预览</el-button>
          <el-button :disabled="!draftDirty || !nameValid" @click="save">保存</el-button>
          <el-tooltip :content="publishHint" :disabled="!publishHint" placement="top">
            <span>
              <el-button type="primary" :disabled="!dirty || !nameValid" :loading="publishing"
                :style="(!dirty || !nameValid) ? 'pointer-events:none' : ''" @click="publish">发布</el-button>
            </span>
          </el-tooltip>
        </template>
        <el-button v-if="showBack" @click="emit('back')">取消</el-button>
      </div>
    </div>

    <DocDrawer v-model="docOpen" />
    <VersionDrawer v-model="versionOpen" :report-id="report.id" @rollback="onRollback" />

    <!-- 编辑模式: 编辑器 + AI 面板 -->
    <el-row v-show="mode === 'edit' && !loading" :gutter="16" class="edit-row">
      <el-col :span="15" class="edit-col">
        <el-card shadow="never" class="fill" body-class="card-body">
          <div class="meta">
            <el-input v-model="report.name" placeholder="报表名称 (必填)" style="flex:1"
              :class="{ 'name-error': !nameValid }" />
            <el-select v-model="report.dsn" style="width:160px">
              <el-option v-if="canReadDsnById('default')" label="default" value="default" />
              <el-option v-for="d in dsnList" :key="d.id" :label="d.name" :value="d.name" />
            </el-select>
            <el-tooltip content="报表设置" placement="top">
              <el-button :icon="Setting" @click="settingsOpen = true" />
            </el-tooltip>
          </div>
          <SqlEditor v-model="report.content" height="100%" class="editor-fill" @save="save" />
        </el-card>
      </el-col>

      <el-col :span="9" class="edit-col">
        <el-card shadow="never" class="fill chat-card" body-class="card-body">
          <AiChat :content="report.content" :dsn-list="dsnList" :default-dsn="report.dsn"
            @update="onAiUpdate" />
        </el-card>
      </el-col>
    </el-row>

    <!-- 预览模式 -->
    <div v-if="mode === 'preview' && !loading" class="sheet" v-loading="previewing">
      <ReportFilters v-model="params" :filters="preview?.filters || []" :loading="previewing" @run="rerun" />
      <!-- 页面级顶部 HTML 预览 (取编辑器当前值, 预览接口不回传 settings) -->
      <div v-if="prependContent.trim()" class="prepend" v-html="prependContent"></div>
      <ReportBlocks v-if="preview" :blocks="preview.blocks" />
    </div>

    <!-- 报表设置: 承载 report.settings 的页面级配置 + 定时任务 -->
    <el-dialog v-model="settingsOpen" title="报表设置" width="720px" append-to-body>
      <el-tabs v-model="settingsTab">
        <el-tab-pane label="常规" name="general">
          <el-form label-width="110px" label-position="right">
            <el-form-item label="自动刷新">
              <el-input v-model.number="autoRefresh" type="number" :min="0" style="width:160px">
                <template #append>秒</template>
              </el-input>
              <div class="form-hint">报表加载后每隔 N 秒自动刷新 (旁路缓存), 0 关闭。</div>
            </el-form-item>
            <el-form-item label="顶部 HTML">
              <el-input v-model="prependContent" type="textarea" :rows="4"
                placeholder="<div class='alert'>说明文字</div>" />
              <div class="form-hint">在报表顶部注入一段原始 HTML (说明/提示/链接), 直接渲染。留空关闭。</div>
            </el-form-item>
          </el-form>
        </el-tab-pane>
        <el-tab-pane label="定时任务" name="schedule">
          <SchedulePanel v-if="report.id" :report-id="report.id" :filters="preview?.filters || []"
            :content="report.content" :dsn="report.dsn" />
          <el-alert v-else type="info" :closable="false"
            title="请先保存报表后再配置定时任务" />
        </el-tab-pane>
      </el-tabs>
      <template #footer>
        <el-button @click="settingsOpen = false">关闭</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.editor-page { display: flex; flex-direction: column; height: 100%; min-height: 0; }
.prepend { margin-bottom: 16px; }
.toolbar { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; flex: none; }
.left { display: flex; align-items: center; gap: 8px; min-width: 0; }
.title { font-weight: 600; }
.actions { display: flex; gap: 8px; }

/* 移动端: 标题与按钮组换行, 标题截断, 按钮可换行不挤压 */
@media (max-width: 768px) {
  .toolbar { flex-wrap: wrap; gap: 8px; }
  .left { flex: 1 1 100%; }
  .title { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .actions { flex-wrap: wrap; }
}

.edit-row { flex: 1; min-height: 0; }
.edit-col { height: 100%; }
.fill { height: 100%; display: flex; flex-direction: column; }
.fill :deep(.card-body) { flex: 1; min-height: 0; display: flex; flex-direction: column; padding: 16px; }

.meta { display: flex; gap: 12px; margin-bottom: 12px; flex: none; align-items: center; }
.form-hint { font-size: 12px; color: var(--el-text-color-secondary); line-height: 1.5; margin-top: 4px; }
/* 报表名为空时高亮输入框边框 */
.name-error :deep(.el-input__wrapper) { box-shadow: 0 0 0 1px var(--el-color-danger) inset; }
.editor-fill { flex: 1; min-height: 0; }
.chat-card :deep(.card-body) { min-height: 0; }

.sheet { background: var(--el-bg-color); border-radius: 8px; padding: 24px; min-height: 200px; overflow-y: auto; }
</style>
