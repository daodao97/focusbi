<script setup>
// 定时任务新增/编辑表单 (弹窗), 被 SchedulePanel (报表设置内) 与 ScheduleList (全局管理页) 共用。
//
// 两种用法:
//   - 固定报表 (Panel): 传 report-id + filters, 不显示报表选择器。
//   - 选报表 (List):    传 selectable + reports, 顶部出现报表下拉, 选定后拉该报表 filters。
import { ref, reactive, watch, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { api } from '@/api'
import ReportFilters from '@/components/ReportFilters.vue'

const props = defineProps({
  modelValue: { type: Boolean, default: false },  // 弹窗可见
  reportId: { type: Number, default: 0 },          // 固定报表 id (Panel 用)
  filters: { type: Array, default: () => [] },     // 固定报表的过滤器 (Panel 用)
  content: { type: String, default: '' },          // 固定报表内容 (Panel 用, 拉 blocks 做条件配置)
  dsn: { type: String, default: 'default' },       // 固定报表数据源 (Panel 用)
  selectable: { type: Boolean, default: false },   // 是否显示报表选择器 (List 用)
  reports: { type: Array, default: () => [] },     // 可选报表 [{id,name}] (List 用)
  edit: { type: Object, default: null }            // 非空=编辑, 传入任务行
})
const emit = defineEmits(['update:modelValue', 'saved'])

const ops = ['>', '>=', '<', '<=', '=', '!=']
const aggs = [
  { label: '任一行', value: 'any' },
  { label: '首行', value: 'first' },
  { label: '求和', value: 'sum' },
  { label: '最大', value: 'max' },
  { label: '最小', value: 'min' },
  { label: '行数', value: 'count' }
]

const cronPresets = [
  { label: '每天 9:00', value: '0 9 * * *' },
  { label: '每周一 9:00', value: '0 9 * * 1' },
  { label: '每月 1 号 9:00', value: '0 9 1 * *' },
  { label: '每小时', value: '0 * * * *' },
  { label: '每分钟 (测试)', value: '* * * * *' }
]

const blank = () => ({
  id: 0, report_id: props.reportId || 0, name: '', cron: '0 9 * * *',
  action: 'webhook', // none 只跑不推 / webhook 推群机器人
  channel: 'lark', webhook: '', params: {}, enabled: true,
  // 阈值告警
  alarm: false, condition: { block: '', column: '', agg: 'any', op: '<', value: '', silence_minutes: 0 }
})
const form = reactive(blank())
const saving = ref(false)
const localFilters = ref([])       // 选报表模式下动态拉取的 filters
const blocks = ref([])             // 报表区块 (用于条件的区块/列下拉)
const visible = computed({ get: () => props.modelValue, set: v => emit('update:modelValue', v) })
const effectiveFilters = computed(() => (props.selectable ? localFilters.value : props.filters))

// 当前条件选中区块的列 (只挑表格区块的列)
const tableBlocks = computed(() => blocks.value.filter(b => b.type === 'table'))
const condColumns = computed(() => {
  const b = blocks.value.find(x => x.id === form.condition.block) || tableBlocks.value[0]
  return (b && b.columns) || []
})

// 打开时按 edit 初始化
watch(() => props.modelValue, async (open) => {
  if (!open) return
  blocks.value = []
  if (props.edit) {
    // 列表里的 webhook 是脱敏值; 拉取单条任务取明文回填, 失败则留空 (留空保留原地址)
    Object.assign(form, blank(), props.edit, { params: { ...(props.edit.params || {}) } })
    try {
      const full = await api.getSchedule(props.edit.report_id, props.edit.id)
      form.webhook = full.webhook || ''
      if (full.params) form.params = { ...full.params }
      if (full.condition && full.condition.column) {
        form.alarm = true
        form.condition = { block: '', agg: 'any', op: '<', value: '', silence_minutes: 0, ...full.condition }
      }
    } catch {
      form.webhook = ''
    }
  } else {
    Object.assign(form, blank())
  }
  if (props.selectable) {
    if (form.report_id) loadPreview(form.report_id)
  } else {
    loadPreviewContent(props.content, props.dsn)
  }
})

// 选报表模式: 选定报表后拉其 filters + blocks (复用 preview 接口解析模板)
async function loadPreview(rid) {
  const r = (props.reports || []).find(x => x.id === rid)
  if (!r) { localFilters.value = []; blocks.value = []; return }
  await loadPreviewContent(r.content || '', r.dsn || 'default')
}
async function loadPreviewContent(content, dsn) {
  localFilters.value = []
  blocks.value = []
  if (!content) return
  try {
    const res = await api.previewReport({ dsn: dsn || 'default', content, params: {} })
    localFilters.value = res.filters || []
    blocks.value = res.blocks || []
  } catch {
    localFilters.value = []
    blocks.value = []
  }
}
function onPickReport(rid) {
  form.report_id = rid
  form.params = {}
  form.condition = { block: '', column: '', agg: 'any', op: '<', value: '', silence_minutes: 0 }
  loadPreview(rid)
}

async function submit() {
  if (props.selectable && !form.report_id) { ElMessage.warning('请选择报表'); return }
  if (!form.cron.trim()) { ElMessage.warning('请填写 cron 表达式'); return }
  const needWebhook = form.action !== 'none'
  if (needWebhook && !form.id && !form.webhook.trim()) { ElMessage.warning('请填写 webhook 地址'); return }
  // none 动作不推送, 阈值告警无意义, 忽略
  if (needWebhook && form.alarm) {
    if (!form.condition.column && form.condition.agg !== 'count') { ElMessage.warning('请选择条件列'); return }
    if (form.condition.value === '') { ElMessage.warning('请填写条件阈值'); return }
  }
  saving.value = true
  try {
    const rid = form.report_id || props.reportId
    // none 动作不推送: 不下发 condition; 否则 alarm 开启才下发 condition。
    const condition = (form.action !== 'none' && form.alarm)
      ? { block: form.condition.block, column: form.condition.column, agg: form.condition.agg, op: form.condition.op, value: String(form.condition.value), silence_minutes: Number(form.condition.silence_minutes) || 0 }
      : null
    const body = { name: form.name, cron: form.cron, action: form.action, channel: form.channel, webhook: form.webhook, params: form.params, enabled: form.enabled, condition }
    if (form.id) await api.updateSchedule(rid, form.id, body)
    else await api.createSchedule(rid, body)
    ElMessage.success('已保存')
    visible.value = false
    emit('saved')
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <el-dialog v-model="visible" :title="form.id ? '编辑任务' : '新增任务'" width="560px" append-to-body>
    <el-form label-width="92px" label-position="right">
      <el-form-item v-if="selectable" label="报表">
        <el-select :model-value="form.report_id || ''" placeholder="选择报表" filterable
          style="width:100%" :disabled="!!form.id" @update:model-value="onPickReport">
          <el-option v-for="r in reports" :key="r.id" :label="r.name" :value="r.id" />
        </el-select>
      </el-form-item>
      <el-form-item label="名称">
        <el-input v-model="form.name" placeholder="如: 销售日报" />
      </el-form-item>
      <el-form-item label="定时">
        <div class="cron-row">
          <el-select :model-value="''" placeholder="常用预设" style="width:140px"
            @update:model-value="v => v && (form.cron = v)">
            <el-option v-for="p in cronPresets" :key="p.value" :label="p.label" :value="p.value" />
          </el-select>
          <el-input v-model="form.cron" placeholder="cron: 分 时 日 月 周" style="flex:1" />
        </div>
        <div class="form-hint">标准 5 段 cron (分 时 日 月 周), 如 0 9 * * * = 每天 9:00。</div>
      </el-form-item>
      <el-form-item label="动作">
        <el-radio-group v-model="form.action">
          <el-radio value="webhook">推送群机器人</el-radio>
          <el-radio value="none">只跑不推</el-radio>
        </el-radio-group>
        <div class="form-hint">只跑不推: 到点执行报表 (刷新缓存 / 预热), 不发任何通知。</div>
      </el-form-item>
      <template v-if="form.action !== 'none'">
        <el-form-item label="渠道">
          <el-radio-group v-model="form.channel">
            <el-radio value="lark">飞书</el-radio>
            <el-radio value="wework">企业微信</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="Webhook">
          <el-input v-model="form.webhook" type="textarea" :rows="2"
            placeholder="粘贴群机器人 webhook 完整地址" />
        </el-form-item>
      </template>
      <el-form-item v-if="effectiveFilters.length" label="固定参数">
        <ReportFilters v-model="form.params" :filters="effectiveFilters" />
        <div class="form-hint">任务按这些参数跑报表; 留默认即按过滤器默认值。</div>
      </el-form-item>
      <el-form-item v-if="form.action !== 'none'" label="触发条件">
        <el-switch v-model="form.alarm" active-text="仅满足条件时推送" inline-prompt />
        <div class="form-hint">关闭=定时必推; 开启=跑完报表命中条件才推 (阈值告警)。</div>
      </el-form-item>
      <template v-if="form.action !== 'none' && form.alarm">
        <el-form-item label="区块">
          <el-select v-model="form.condition.block" placeholder="首个表格区块" clearable style="width:100%">
            <el-option v-for="b in tableBlocks" :key="b.id" :label="b.title || b.id" :value="b.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="条件">
          <div class="cond-row">
            <el-select v-model="form.condition.agg" style="width:96px">
              <el-option v-for="a in aggs" :key="a.value" :label="a.label" :value="a.value" />
            </el-select>
            <el-select v-if="form.condition.agg !== 'count'" v-model="form.condition.column"
              placeholder="列" filterable style="width:130px">
              <el-option v-for="c in condColumns" :key="c.name" :label="c.header || c.name" :value="c.name" />
            </el-select>
            <el-select v-model="form.condition.op" style="width:72px">
              <el-option v-for="o in ops" :key="o" :label="o" :value="o" />
            </el-select>
            <el-input v-model="form.condition.value" placeholder="阈值" style="flex:1;min-width:80px" />
          </div>
          <div class="form-hint">如「任一行 gmv &lt; 10000」或「sum 金额 &lt; 5000」时触发推送。</div>
        </el-form-item>
        <el-form-item label="静默期">
          <el-input-number v-model="form.condition.silence_minutes" :min="0" :step="10" style="width:130px" />
          <div class="form-hint">分钟。推送一次告警后, 静默期内再命中不重复推送; 0 = 每次命中都推。</div>
        </el-form-item>
      </template>
      <el-form-item label="启用">
        <el-switch v-model="form.enabled" />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button type="primary" :loading="saving" @click="submit">保存</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.cron-row { display: flex; gap: 8px; width: 100%; }
.cond-row { display: flex; gap: 8px; width: 100%; flex-wrap: wrap; }
.form-hint { font-size: 12px; color: var(--el-text-color-secondary); line-height: 1.5; margin-top: 4px; }
</style>
