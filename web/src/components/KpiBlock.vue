<script setup>
import { computed } from 'vue'

// kpi: 引擎返回的 KpiConfig ({items}); rows: 数据行 (与表格同源)
const props = defineProps({
  kpi: { type: Object, required: true },
  rows: { type: Array, default: () => [] }
})

function num(v) {
  const n = Number(String(v ?? '').replace(/,/g, '').replace(/%$/, ''))
  return Number.isFinite(n) ? n : 0
}

function addThousands(s) {
  const neg = s.startsWith('-')
  if (neg) s = s.slice(1)
  const [int, frac] = s.split('.')
  const grouped = int.replace(/\B(?=(\d{3})+(?!\d))/g, ',')
  return (neg ? '-' : '') + grouped + (frac != null ? '.' + frac : '')
}

// 与后端 transform.go:formatCellValue 口径一致 (money/number/integer/percent)。
function fmt(v, format) {
  if (v == null || v === '') return '-'
  const n = Number(String(v).replace(/,/g, ''))
  if (!Number.isFinite(n)) return String(v)
  switch ((format || '').toLowerCase()) {
    case 'money':
    case 'currency':
      return addThousands(n.toFixed(2))
    case 'number':
      return addThousands(String(n))
    case 'integer':
    case 'int':
      return addThousands(n.toFixed(0))
    case 'percent':
    case 'percentage':
      return (n * 100).toFixed(2) + '%'
    default:
      return String(v)
  }
}

const last = computed(() => props.rows[props.rows.length - 1] || {})

// 每张卡片的展示模型: 值 / 同环比 / sparkline 点。
const cards = computed(() => (props.kpi.items || []).map(it => {
  const cur = last.value[it.value]
  const card = {
    label: it.label,
    value: fmt(cur, it.format),
    unit: it.unit || '',
    compare: null,
    points: null
  }

  // 同环比: (当前 - 基准)/基准。基准 0 → +100%; 当前缺失 → 数据缺失。(对齐 fluctuations.go)
  if (it.compare) {
    const base = last.value[it.compare]
    if (cur == null || cur === '') {
      card.compare = { text: '数据缺失', cls: 'kpi-flat' }
    } else {
      const c = num(cur), b = num(base)
      const diff = b === 0 ? 1 : (c - b) / b
      const pct = Math.round(diff * 100)
      card.compare = {
        text: (pct >= 0 ? '▲ +' : '▼ ') + pct + '%',
        cls: pct > 0 ? 'kpi-up' : pct < 0 ? 'kpi-down' : 'kpi-flat'
      }
    }
  }

  // sparkline: 取 trend 列整列, 归一化到 0..1 的 SVG polyline 坐标点。
  if (it.trend) {
    const ys = props.rows.map(r => num(r[it.trend]))
    if (ys.length >= 2) {
      const min = Math.min(...ys), max = Math.max(...ys)
      const span = max - min || 1
      const W = 120, H = 28
      card.points = ys.map((y, i) =>
        `${(i / (ys.length - 1) * W).toFixed(1)},${(H - (y - min) / span * H).toFixed(1)}`
      ).join(' ')
    }
  }
  return card
}))
</script>

<template>
  <div class="kpi-block">
    <div v-for="(c, i) in cards" :key="i" class="kpi-card">
      <div class="kpi-label">{{ c.label }}</div>
      <div class="kpi-value">{{ c.value }}<span v-if="c.unit" class="kpi-unit">{{ c.unit }}</span></div>
      <div v-if="c.compare" class="kpi-compare" :class="c.compare.cls">{{ c.compare.text }}</div>
      <svg v-if="c.points" class="kpi-spark" viewBox="0 0 120 28" preserveAspectRatio="none">
        <polyline :points="c.points" fill="none" stroke="currentColor" stroke-width="1.5" />
      </svg>
    </div>
  </div>
</template>

<style scoped>
.kpi-block {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  margin-bottom: 12px;
}
.kpi-card {
  flex: 1 1 160px;
  min-width: 160px;
  padding: 14px 16px;
  border: 1px solid var(--el-border-color-lighter, #ebeef5);
  border-radius: 8px;
  background: var(--el-bg-color, #fff);
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.04);
}
.kpi-label {
  font-size: 13px;
  color: var(--el-text-color-secondary, #909399);
  margin-bottom: 6px;
}
.kpi-value {
  font-size: 26px;
  font-weight: 600;
  line-height: 1.2;
  color: var(--el-text-color-primary, #303133);
}
.kpi-unit {
  font-size: 14px;
  font-weight: 400;
  margin-left: 4px;
  color: var(--el-text-color-secondary, #909399);
}
.kpi-compare {
  font-size: 13px;
  margin-top: 4px;
}
.kpi-up { color: var(--el-color-success, #67c23a); }
.kpi-down { color: var(--el-color-danger, #f56c6c); }
.kpi-flat { color: var(--el-text-color-secondary, #909399); }
.kpi-spark {
  width: 100%;
  height: 28px;
  margin-top: 8px;
  color: var(--el-color-primary, #409eff);
}
</style>
