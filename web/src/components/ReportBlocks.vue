<script setup>
import { ref, defineAsyncComponent } from 'vue'
import { ElMessage } from 'element-plus'
import { InfoFilled } from '@element-plus/icons-vue'
import { copyText } from '@/clipboard'
import TimingTooltip from './TimingTooltip.vue'
// ChartBlock(ECharts ~1MB) 与 SqlEditor(Monaco ~3MB) 体积大且仅按需出现,
// 用异步组件拆分成独立 chunk: 无图表/不看 SQL 的页面不会加载它们。
const ChartBlock = defineAsyncComponent(() => import('./ChartBlock.vue'))
const KpiBlock = defineAsyncComponent(() => import('./KpiBlock.vue'))
const SqlEditor = defineAsyncComponent(() => import('./SqlEditor.vue'))
// markdown 区块用 vue-stream-markdown 渲染 (与 AI 对话同款); 异步加载, 无 markdown 区块不拉。
const Markdown = defineAsyncComponent(async () => {
  await import('vue-stream-markdown/index.css')
  await import('vue-stream-markdown/theme.css')
  return (await import('vue-stream-markdown')).Markdown
})

// 渲染引擎返回的 blocks: 表格 / 图表 / markdown。
defineProps({
  blocks: { type: Array, default: () => [] }
})

// SQL 模态框
const sqlDialog = ref(false)
const sqlText = ref('')
const sqlTitle = ref('')

function showSql(b) {
  sqlText.value = b.sql || ''
  sqlTitle.value = b.title || b.id || ''
  sqlDialog.value = true
}

async function copySql() {
  if (await copyText(sqlText.value)) {
    ElMessage.success('已复制 SQL')
  } else {
    ElMessage.error('复制失败')
  }
}

// 把 href 模板里的 {field} 替换为当前行的值
function cellHref(col, row) {
  const tpl = col.config && col.config.href
  if (!tpl) return ''
  return String(tpl).replace(/\{(\w+)\}/g, (_, k) => encodeURIComponent(row[k] ?? ''))
}
function cellTooltip(col) {
  return (col.config && col.config.tooltip) || ''
}

// 单元格标签 (cell_attrs): 取某列某行的标签配置, 无则返回 null。
function cellTag(b, col, rowIndex) {
  const colMap = b.cell_attrs && b.cell_attrs[col.name]
  return (colMap && colMap[String(rowIndex)]) || null
}
// 该列是否配置了 tag (任意行命中即需走自定义渲染)
function colHasTag(b, col) {
  return !!(b.cell_attrs && b.cell_attrs[col.name])
}
// 是否需要自定义单元格渲染 (链接 / 提示 / 标签)
function colNeedsSlot(b, col) {
  return colHasTag(b, col) || !!(col.config && (col.config.href || col.config.tooltip))
}

// 行级样式 (row_attrs): el-table row-class-name 回调。
function makeRowClass(b) {
  if (!b.row_attrs) return undefined
  return ({ rowIndex }) => {
    const a = b.row_attrs[String(rowIndex)]
    return (a && a.class) || ''
  }
}

// 单元格展示值: 标签文本优先于原值 (导出时与界面一致)。
function displayValue(b, col, rowIndex, row) {
  const tag = cellTag(b, col, rowIndex)
  if (tag && tag.text) return tag.text
  const v = row[col.name]
  return v === undefined || v === null ? '' : v
}

// 把单个字段转义为 CSV 字段 (含逗号/引号/换行时加引号)。
function csvField(v) {
  const s = String(v)
  return /[",\n\r]/.test(s) ? '"' + s.replace(/"/g, '""') + '"' : s
}

// 导出当前表格块为 CSV (含表头与可选汇总行)。带 UTF-8 BOM, Excel 可直接打开。
function exportCsv(b) {
  const cols = b.columns || []
  if (!cols.length || !b.rows || !b.rows.length) {
    ElMessage.warning('无数据可导出')
    return
  }
  const lines = [cols.map(c => csvField(c.header)).join(',')]
  b.rows.forEach((row, i) => {
    lines.push(cols.map(c => csvField(displayValue(b, c, i, row))).join(','))
  })
  if (b.summary && Object.keys(b.summary).length) {
    lines.push(cols.map((c, i) => csvField(summaryCell(b, 'sum', c, i))).join(','))
  }
  if (b.average && Object.keys(b.average).length) {
    lines.push(cols.map((c, i) => csvField(summaryCell(b, 'avg', c, i))).join(','))
  }
  const blob = new Blob(['﻿' + lines.join('\r\n')], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = (b.title || b.id || 'report') + '.csv'
  a.click()
  URL.revokeObjectURL(url)
}

// merge_cell: 为相邻、同列且值相同的单元格生成 rowspan (el-table span-method)。
// 返回每个 block 的 span-method 闭包。
function makeSpanMethod(b) {
  const mergeCols = b.merge_cell || []
  if (!mergeCols.length) return undefined
  // 预计算: 对每个需合并的列, 记录每行应跨多少行 (0 表示被上一行合并掉)
  const spanMap = {} // colName -> array of rowspan
  for (const name of mergeCols) {
    const spans = new Array(b.rows.length).fill(1)
    let i = 0
    while (i < b.rows.length) {
      let j = i + 1
      while (j < b.rows.length && b.rows[j][name] === b.rows[i][name] &&
             // 仅当前置合并列也相同才继续 (避免跨组错误合并)
             mergeColsMatch(b, mergeCols, name, i, j)) {
        j++
      }
      spans[i] = j - i
      for (let k = i + 1; k < j; k++) spans[k] = 0
      i = j
    }
    spanMap[name] = spans
  }
  return ({ column, rowIndex }) => {
    const name = column.property
    if (!spanMap[name]) return { rowspan: 1, colspan: 1 }
    const rs = spanMap[name][rowIndex]
    return rs === 0 ? { rowspan: 0, colspan: 0 } : { rowspan: rs, colspan: 1 }
  }
}
// 合并某列时, 要求它前面的合并列在 i/j 两行也相同 (层级合并)
function mergeColsMatch(b, mergeCols, name, i, j) {
  for (const c of mergeCols) {
    if (c === name) break
    if (b.rows[i][c] !== b.rows[j][c]) return false
  }
  return true
}

// 是否显示合计/平均行
function hasSummary(b) {
  return (b.summary && Object.keys(b.summary).length) || (b.average && Object.keys(b.average).length)
}

// el-table 的合计行渲染: 第一列放标签, 其余列取 summary/average 值。
// el-table 只支持单条合计行, 这里优先显示 summary, 再显示 average 时用第二个表脚不便,
// 故改为在表格下方单独渲染汇总条。
function summaryCell(b, type, col, idx) {
  const data = type === 'sum' ? b.summary : b.average
  if (!data) return ''
  if (idx === 0 && data[col.name] === undefined) {
    return type === 'sum' ? '合计' : '平均'
  }
  const v = data[col.name]
  return v === undefined || v === null ? '' : v
}

</script>

<template>
  <div>
    <section v-for="b in blocks" :key="b.id" class="block">
      <!-- markdown/raw 无 CSV/SQL 按钮, 无标题时整个 header 留空, 直接隐藏 -->
      <div v-if="!((b.type === 'markdown' || b.type === 'raw') && !b.title)" class="block-header">
        <span class="block-title">{{ b.title || b.id }}</span>
        <TimingTooltip :timing="b.timing" />
        <span v-if="b.subtitle" class="block-subtitle">{{ b.subtitle }}</span>
        <span class="spacer" />
        <el-button v-if="b.type === 'table' && b.rows && b.rows.length" link type="primary" size="small"
          @click="exportCsv(b)">
          导出 CSV
        </el-button>
        <el-button v-if="b.sql" link type="primary" size="small" @click="showSql(b)">
          查看 SQL
        </el-button>
      </div>

      <el-alert v-if="b.error" type="error" :closable="false" :title="b.error" />

      <!-- markdown 区块: 渲染 markdown 语法 (标题/列表/表格/代码等) -->
      <Markdown v-else-if="b.type === 'markdown'" :content="b.markdown || ''"
        mode="static" :controls="false" :previewers="false" class="md-body" />

      <!-- raw 区块: 原样输出, 不转 markdown (语义即此) -->
      <pre v-else-if="b.type === 'raw'" class="md">{{ b.markdown }}</pre>

      <template v-else>
        <el-alert v-if="b.notice" :title="b.notice" type="info" :closable="false" class="notice" />

        <KpiBlock v-if="b.kpi && b.kpi.items && b.kpi.items.length && b.rows && b.rows.length"
          :kpi="b.kpi" :rows="b.rows" />

        <ChartBlock v-if="b.chart && b.rows && b.rows.length" :chart="b.chart" :rows="b.rows" />

        <template v-if="!b.invisible">
          <el-table v-if="b.rows && b.rows.length" :data="b.rows" stripe size="small" max-height="480"
            :span-method="makeSpanMethod(b)" :row-class-name="makeRowClass(b)">
            <el-table-column
              v-for="c in b.columns" :key="c.name"
              :prop="c.name" :label="c.header" show-overflow-tooltip min-width="120">
              <template v-if="colNeedsSlot(b, c)" #default="{ row, $index }">
                <el-tag v-if="cellTag(b, c, $index)" :type="cellTag(b, c, $index).type || 'info'"
                  :effect="cellTag(b, c, $index).plain ? 'plain' : 'light'" size="small"
                  :title="cellTooltip(c)">{{ cellTag(b, c, $index).text || row[c.name] }}</el-tag>
                <a v-else-if="c.config && c.config.href" :href="cellHref(c, row)" target="_blank" class="cell-link"
                  :title="cellTooltip(c)">{{ row[c.name] }}</a>
                <span v-else :title="cellTooltip(c)">{{ row[c.name] }}</span>
              </template>
              <template v-if="c.config && c.config.tooltip" #header>
                <span :title="c.config.tooltip">{{ c.header }} <el-icon class="hint-icon"><InfoFilled /></el-icon></span>
              </template>
            </el-table-column>
          </el-table>
          <el-empty v-else description="无数据" :image-size="60" />

          <!-- 合计 / 平均 汇总条 -->
          <div v-if="hasSummary(b)" class="summary">
            <div v-if="b.summary && Object.keys(b.summary).length" class="sum-row">
              <span v-for="(c, i) in b.columns" :key="c.name" class="sum-cell">
                {{ summaryCell(b, 'sum', c, i) }}
              </span>
            </div>
            <div v-if="b.average && Object.keys(b.average).length" class="sum-row">
              <span v-for="(c, i) in b.columns" :key="c.name" class="sum-cell">
                {{ summaryCell(b, 'avg', c, i) }}
              </span>
            </div>
          </div>
        </template>
      </template>
    </section>

    <!-- SQL 抽屉: 右侧弹出, 只读 Monaco 展示, 带高亮 -->
    <el-drawer v-model="sqlDialog" :title="`实际执行 SQL · ${sqlTitle}`" direction="rtl" size="50%"
      append-to-body :destroy-on-close="true">
      <SqlEditor v-if="sqlDialog" :model-value="sqlText" readonly height="100%" />
      <template #footer>
        <el-button type="primary" @click="copySql">复制 SQL</el-button>
        <el-button @click="sqlDialog = false">关闭</el-button>
      </template>
    </el-drawer>
  </div>
</template>

<style scoped>
.block { margin-bottom: 28px; }
.block-header {
  display: flex;
  align-items: baseline;
  gap: 8px;
  margin-bottom: 10px;
  padding-bottom: 6px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}
.block-title { font-weight: 600; font-size: 14px; color: var(--el-text-color-primary); }
.block-subtitle { font-size: 12px; color: var(--el-text-color-secondary); }
.cell-link { color: var(--el-color-primary); text-decoration: none; }
.cell-link:hover { text-decoration: underline; }

/* 行级样式预设 (row_tag class): 用 EP 语义色变量, 自动适配明暗 */
:deep(.el-table .row-success > td.el-table__cell) { background: var(--el-color-success-light-9); }
:deep(.el-table .row-warning > td.el-table__cell) { background: var(--el-color-warning-light-9); }
:deep(.el-table .row-danger > td.el-table__cell) { background: var(--el-color-danger-light-9); }
:deep(.el-table .row-info > td.el-table__cell) { background: var(--el-color-info-light-9); }
:deep(.el-table .row-muted > td.el-table__cell) { color: var(--el-text-color-secondary); }
:deep(.el-table .row-bold > td.el-table__cell) { font-weight: 600; }
.hint-icon { font-size: 12px; color: var(--el-text-color-placeholder); vertical-align: -1px; }
.spacer { flex: 1; }
.notice { margin-bottom: 10px; }
.md { white-space: pre-wrap; font-family: inherit; margin: 0; color: var(--el-text-color-primary); }
/* markdown 区块: 渲染后的排版 (标题/列表/表格等); 基础样式来自 vue-stream-markdown 主题 */
.md-body { font-size: 14px; line-height: 1.7; color: var(--el-text-color-primary); }
.md-body :deep(table) { border-collapse: collapse; }
.md-body :deep(th), .md-body :deep(td) { border: 1px solid var(--el-border-color-light); padding: 6px 12px; }

/* 合计 / 平均 汇总条: 与表格列对齐的简易行 */
.summary { margin-top: 4px; border-top: 2px solid var(--el-border-color); }
.sum-row { display: flex; background: var(--el-fill-color-light); font-size: 12px; }
.sum-cell {
  flex: 1;
  min-width: 120px;
  padding: 8px 10px;
  font-weight: 600;
  color: var(--el-text-color-regular);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
</style>
