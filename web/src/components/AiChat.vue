<script setup>
import { ref, nextTick } from 'vue'
import { ElMessage } from 'element-plus'
import { Markdown } from 'vue-stream-markdown'
import 'vue-stream-markdown/index.css'
import 'vue-stream-markdown/theme.css'
import { api } from '@/api'
import { canReadDsnById } from '@/perm'

// AI 对话面板: 拿当前模板 content, 用户输入指令, 调用后端返回待确认修改。
// 通过级联选择器 (数据源 -> 库 -> 表) 选相关表, 把表结构作为上下文一并发给 AI。
const props = defineProps({
  content: { type: String, default: '' },
  dsnList: { type: Array, default: () => [] },
  defaultDsn: { type: String, default: 'default' }
})
const emit = defineEmits(['update'])

const log = ref([{ role: 'ai', text: '描述你想要的修改, 我会生成待确认的模板变更。\n可先选择相关表 (数据源 → 库 → 表), 我会参考其字段生成 SQL。' }])
const input = ref('')
const busy = ref(false)
const logEl = ref(null)
const history = ref([])
const pending = ref(null)
const pendingInstruction = ref('')
const streamText = ref('')
const toolUsed = ref(false)

// 级联选中值: 数组的数组, 每项形如 [dsn, db, table]
const picked = ref([])

// 级联配置: 多选 + 懒加载
const cascaderProps = {
  multiple: true,
  lazy: true,
  lazyLoad: async (node, resolve) => {
    const { level, value, pathValues } = node
    try {
      if (level === 0) {
        // 第一层: 数据源 (实时从后端拉取, 不依赖外部传入的 dsnList 时机)。
        // listDsn 已按权限过滤; default 单独按 dsn.default 权限决定是否显示。
        const head = canReadDsnById('default') ? ['default'] : []
        let names = head
        try {
          const list = await api.listDsn()
          names = [...head, ...list.map(d => d.name)]
        } catch {
          // 拉取失败时退回到 props 传入的列表
          names = [...head, ...props.dsnList.map(d => d.name)]
        }
        resolve(names.map(n => ({ value: n, label: n, leaf: false })))
      } else if (level === 1) {
        // 第二层: 库
        const dbs = await api.listDatabases(value)
        resolve(dbs.map(db => ({ value: db, label: db, leaf: false })))
      } else if (level === 2) {
        // 第三层: 表 (叶子)
        const [dsn, db] = pathValues
        const tables = await api.listTables(dsn, db)
        resolve(tables.map(t => ({ value: t, label: t, leaf: true })))
      } else {
        resolve([])
      }
    } catch (e) {
      ElMessage.error('加载失败: ' + e.message)
      resolve([])
    }
  }
}

// 把级联路径 [dsn,db,table] 转成引用对象 (含 @库.表 标签)
function pathToRef(p) {
  const [dsn, db, table] = p
  const label = db ? `${db}.${table}` : table
  return { dsn, db, table, label }
}

// 引用去重 key
function refKey(r) { return `${r.dsn}|${r.db}|${r.table}` }

// 汇总整个对话历史中所有 @表引用 (去重), 拼成结构上下文喂给 AI。
async function buildSchema() {
  const seen = new Set()
  const refs = []
  for (const m of log.value) {
    for (const r of (m.refs || [])) {
      const k = refKey(r)
      if (!seen.has(k)) { seen.add(k); refs.push(r) }
    }
  }
  if (!refs.length) return ''
  const parts = []
  for (const r of refs) {
    try {
      const cols = await api.listColumns(r.dsn, r.db, r.table)
      const lines = cols.map(c => `  ${c.name} ${c.type}${c.comment ? ' -- ' + c.comment : ''}`)
      parts.push(`表 ${r.label} (@dsn=${r.dsn}):\n${lines.join('\n')}`)
    } catch {
      parts.push(`表 ${r.label}: (字段获取失败)`)
    }
  }
  return parts.join('\n\n')
}

async function scrollDown() {
  await nextTick()
  if (logEl.value) logEl.value.scrollTop = logEl.value.scrollHeight
}

async function send() {
  const text = input.value.trim()
  if ((!text && !picked.value.length) || busy.value) return
  pending.value = null
  pendingInstruction.value = ''
  streamText.value = ''
  toolUsed.value = false

  // 本次选中的表 -> 引用; 文本前缀拼上 @库.表 字样
  const refs = (picked.value || []).filter(p => p[2]).map(pathToRef)
  const mention = refs.map(r => '@' + r.label).join(' ')
  const fullText = mention ? (text ? `${mention}\n${text}` : mention) : text

  log.value.push({ role: 'user', text: fullText, refs })
  input.value = ''
  picked.value = []        // 清空级联器, 方便下次选择
  scrollDown()

  // 仅 @表无指令: 只登记到上下文, 不调用 AI
  if (!text) {
    log.value.push({ role: 'ai', text: `已记住 ${refs.length} 张表, 后续指令会参考它们的结构。` })
    scrollDown()
    return
  }

  busy.value = true
  try {
    const schema = await buildSchema() // 汇总全历史 @表
    await api.aiModifyStream(props.content, text, schema, history.value, (evt) => {
      if (evt.event === 'delta') {
        streamText.value += evt.data?.text || ''
        nextTick(scrollDown)
      } else if (evt.event === 'tool_call') {
        toolUsed.value = !!evt.data?.used
      } else if (evt.event === 'proposal') {
        pending.value = evt.data
        pendingInstruction.value = text
        log.value.push({ role: 'ai', text: '已生成修改建议，请确认后应用到模板。' })
      } else if (evt.event === 'status') {
        log.value.push({ role: 'ai', text: evt.data?.text || '处理中…' })
      }
    })
  } catch (e) {
    log.value.push({ role: 'ai', text: '出错: ' + e.message })
  } finally {
    busy.value = false
    scrollDown()
  }
}

function applyPending() {
  if (!pending.value) return
  emit('update', pending.value.content)
  history.value.push({ instruction: pendingInstruction.value, template: pending.value.content })
  log.value.push({ role: 'ai', text: '已应用修改到左侧模板。' })
  pending.value = null
  pendingInstruction.value = ''
  streamText.value = ''
  toolUsed.value = false
  scrollDown()
}

function discardPending() {
  pending.value = null
  pendingInstruction.value = ''
  streamText.value = ''
  toolUsed.value = false
  log.value.push({ role: 'ai', text: '已放弃本次修改建议。' })
  scrollDown()
}

// 消息正文: 去掉开头的 @提及行 (已用标签展示), 避免重复
function msgBody(m) {
  if (!m.refs || !m.refs.length) return m.text
  const mention = m.refs.map(r => '@' + r.label).join(' ')
  let t = m.text
  if (t.startsWith(mention)) t = t.slice(mention.length).replace(/^\n/, '')
  return t
}

// Enter 发送, Shift+Enter 换行
function onKeydown(e) {
  if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
    e.preventDefault()
    send()
  }
}
</script>

<template>
  <div class="ai-chat">
    <div class="head">AI 助手</div>

    <div ref="logEl" class="log">
      <div v-for="(m, i) in log" :key="i" :class="['msg', m.role]">
        <div v-if="m.refs && m.refs.length" class="msg-refs">
          <el-tag v-for="r in m.refs" :key="r.label" size="small" type="info" effect="plain" class="ref-tag">
            @{{ r.label }}
          </el-tag>
        </div>
        <Markdown
          v-if="m.role === 'ai' && msgBody(m)"
          :content="msgBody(m)"
          mode="static"
          :controls="false"
          :previewers="false"
          class="msg-markdown" />
        <span v-else-if="msgBody(m)">{{ msgBody(m) }}</span>
      </div>
    </div>

    <div v-if="streamText" class="stream-text">
      <div class="stream-title">说明</div>
      <Markdown
        :content="streamText"
        mode="streaming"
        :controls="false"
        :previewers="false"
        class="stream-markdown-view" />
    </div>

    <div v-if="pending" class="proposal">
      <div class="proposal-head">
        <span>待确认修改</span>
        <el-tag size="small" :type="toolUsed ? 'success' : 'warning'" effect="plain">
          {{ toolUsed ? 'tool call' : '文本解析' }}
        </el-tag>
      </div>
      <el-tabs type="border-card" class="proposal-tabs">
        <el-tab-pane label="Patch">
          <pre class="proposal-code">{{ pending.patch || streamText }}</pre>
        </el-tab-pane>
        <el-tab-pane label="新模板">
          <pre class="proposal-code">{{ pending.content }}</pre>
        </el-tab-pane>
      </el-tabs>
      <div class="proposal-actions">
        <el-button size="small" @click="discardPending">放弃</el-button>
        <el-button size="small" type="primary" @click="applyPending">应用到模板</el-button>
      </div>
    </div>

    <!-- 级联选择: 数据源 -> 库 -> 表 -->
    <div class="ctx">
      <el-cascader
        v-model="picked" :props="cascaderProps" size="small"
        placeholder="选择相关表 (数据源 → 库 → 表), 发送后以 @表 形式带入"
        clearable filterable collapse-tags collapse-tags-tooltip
        :show-all-levels="true"
        popper-class="ai-cascader-pop" style="width: 100%" />
      <div v-if="picked.length" class="ctx-hint">
        本次将带入 {{ picked.filter(p => p[2]).length }} 张表 (@提及), 发送后清空
      </div>
    </div>

    <div class="input">
      <el-input
        v-model="input" type="textarea" :rows="3" resize="none"
        placeholder="输入修改指令… (Enter 发送, Shift+Enter 换行)"
        :disabled="busy" @keydown="onKeydown" />
      <el-button type="primary" :loading="busy" @click="send">发送</el-button>
    </div>
  </div>
</template>

<style scoped>
.ai-chat { display: flex; flex-direction: column; height: 100%; min-height: 460px; }
.head { font-weight: 600; margin-bottom: 8px; color: var(--el-text-color-primary); }
.log { flex: 1; overflow-y: auto; padding-right: 4px; min-height: 120px; }
.msg { padding: 8px 10px; border-radius: 8px; margin-bottom: 8px; font-size: 13px; line-height: 1.5; white-space: pre-wrap; color: var(--el-text-color-primary); }
.msg.user { background: var(--el-color-primary-light-9); }
.msg.ai { background: var(--el-fill-color-light); }
.msg-markdown { font-size: 13px; line-height: 1.5; white-space: normal; }
.msg-markdown :deep(p), .stream-markdown-view :deep(p) { margin: 0 0 6px; }
.msg-markdown :deep(p:last-child), .stream-markdown-view :deep(p:last-child) { margin-bottom: 0; }
.msg-markdown :deep(ul), .msg-markdown :deep(ol), .stream-markdown-view :deep(ul), .stream-markdown-view :deep(ol) { margin: 4px 0 6px; padding-left: 20px; }
.msg-markdown :deep(pre), .stream-markdown-view :deep(pre) { margin: 6px 0; max-width: 100%; overflow: auto; border-radius: 6px; }
.msg-markdown :deep(code), .stream-markdown-view :deep(code) { font-family: Monaco, Consolas, monospace; font-size: 12px; }
.msg-refs { display: flex; flex-wrap: wrap; gap: 4px; margin-bottom: 6px; }
.ref-tag { font-family: Monaco, monospace; }
.stream-text { border: 1px solid var(--el-border-color-lighter); border-radius: 8px; padding: 8px; margin-top: 8px; background: var(--el-fill-color-extra-light); }
.stream-title { font-size: 12px; color: var(--el-text-color-secondary); margin-bottom: 4px; }
.stream-markdown-view { font-size: 13px; line-height: 1.45; white-space: normal; }
.proposal { border: 1px solid var(--el-border-color); border-radius: 8px; padding: 8px; margin-top: 8px; background: var(--el-bg-color); }
.proposal-head { display: flex; align-items: center; justify-content: space-between; font-size: 13px; font-weight: 600; margin-bottom: 8px; }
.proposal-tabs { --el-tabs-header-height: 32px; }
.proposal-code { margin: 0; max-height: 220px; overflow: auto; white-space: pre-wrap; word-break: break-word; font-family: Monaco, Consolas, monospace; font-size: 12px; line-height: 1.45; }
.proposal-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 8px; }
.ctx { margin-top: 8px; }
.ctx-hint { font-size: 12px; color: var(--el-text-color-secondary); margin-top: 4px; }
.input { display: flex; gap: 8px; margin-top: 8px; align-items: flex-end; }
.input :deep(.el-textarea) { flex: 1; }
</style>

<style>
/* 级联弹层向上展开, 避免在底部被截断 */
.ai-cascader-pop { margin-bottom: 6px; }
</style>
