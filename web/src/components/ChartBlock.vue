<script setup>
import { ref, shallowRef, onMounted, onBeforeUnmount, watch, nextTick } from 'vue'
import * as echarts from 'echarts'

// chart: 引擎返回的 ChartConfig; rows: 数据行
const props = defineProps({
  chart: { type: Object, required: true },
  rows: { type: Array, default: () => [] }
})

const el = ref(null)
const inst = shallowRef(null)

function num(v) {
  const n = Number(v)
  return Number.isFinite(n) ? n : 0
}

// 分类+数值: pie / funnel 的 {name,value} 数据。
function nameValueData(c, rows) {
  return rows.map(r => ({ name: r[c.name], value: num(r[c.value]) }))
}

function buildOption() {
  const c = props.chart
  const rows = props.rows || []

  switch (c.type) {
    case 'pie':
      return {
        tooltip: { trigger: 'item' },
        legend: { bottom: 0 },
        series: [{ type: 'pie', radius: ['40%', '65%'], data: nameValueData(c, rows) }]
      }

    case 'funnel':
      return {
        tooltip: { trigger: 'item', formatter: '{b}: {c}' },
        legend: { bottom: 0 },
        series: [{ type: 'funnel', left: '10%', right: '10%', sort: 'descending', data: nameValueData(c, rows) }]
      }

    case 'scatter':
      return {
        tooltip: { trigger: 'item' },
        grid: { left: 56, right: 24, top: 36, bottom: 48, containLabel: true },
        xAxis: { type: 'value', name: c.x, scale: true },
        yAxis: { type: 'value', name: c.y, scale: true },
        series: [{ type: 'scatter', data: rows.map(r => [num(r[c.x]), num(r[c.y])]) }]
      }

    case 'gauge': {
      const last = rows[rows.length - 1] || {}
      return {
        tooltip: { formatter: '{b}: {c}' },
        series: [{
          type: 'gauge',
          progress: { show: true },
          detail: { valueAnimation: true, formatter: '{value}' },
          data: [{ name: c.value, value: num(last[c.value]) }]
        }]
      }
    }

    case 'radar': {
      const dims = c.series || []
      // 每行一组指标值, 各维度的最大值用于雷达量程。
      const max = {}
      for (const d of dims) max[d] = Math.max(1, ...rows.map(r => num(r[d])))
      return {
        tooltip: { trigger: 'item' },
        legend: { bottom: 0 },
        radar: { indicator: dims.map(d => ({ name: d, max: max[d] })) },
        series: [{
          type: 'radar',
          data: rows.map((r, i) => ({ name: r[c.x] || ('系列' + (i + 1)), value: dims.map(d => num(r[d])) }))
        }]
      }
    }

    default: {
      // line / bar / area: 类目轴 + 多数值序列。
      const isBar = c.type === 'bar'
      const isArea = c.type === 'area'
      const x = rows.map(r => r[c.x])
      const series = (c.series || []).map(s => ({
        name: s,
        type: isBar ? 'bar' : 'line',
        smooth: !isBar,
        areaStyle: isArea ? {} : undefined,
        stack: c.stack ? 'total' : undefined,
        data: rows.map(r => num(r[s]))
      }))
      return {
        tooltip: { trigger: 'axis' },
        legend: { data: c.series || [] },
        grid: { left: 56, right: 24, top: 36, bottom: 48, containLabel: true },
        xAxis: { type: 'category', data: x, boundaryGap: isBar },
        yAxis: { type: 'value' },
        series
      }
    }
  }
}

function render() {
  if (!inst.value) return
  inst.value.setOption(buildOption(), true)
}

function resize() { inst.value && inst.value.resize() }

onMounted(async () => {
  await nextTick()
  inst.value = echarts.init(el.value)
  render()
  window.addEventListener('resize', resize)
})

onBeforeUnmount(() => {
  window.removeEventListener('resize', resize)
  inst.value && inst.value.dispose()
})

watch(() => [props.chart, props.rows], render, { deep: true })
</script>

<template>
  <div ref="el" class="chart-block"></div>
</template>

<style scoped>
.chart-block { width: 100%; height: 340px; }
</style>
