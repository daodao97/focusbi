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

function buildOption() {
  const c = props.chart
  const rows = props.rows || []

  if (c.type === 'pie') {
    return {
      tooltip: { trigger: 'item' },
      legend: { bottom: 0 },
      series: [{
        type: 'pie',
        radius: ['40%', '65%'],
        data: rows.map(r => ({ name: r[c.name], value: num(r[c.value]) }))
      }]
    }
  }

  const x = rows.map(r => r[c.x])
  const series = (c.series || []).map(s => ({
    name: s,
    type: c.type === 'bar' ? 'bar' : 'line',
    smooth: c.type !== 'bar',
    data: rows.map(r => num(r[s]))
  }))
  return {
    tooltip: { trigger: 'axis' },
    legend: { data: c.series || [] },
    grid: { left: 56, right: 24, top: 36, bottom: 48, containLabel: true },
    xAxis: { type: 'category', data: x, boundaryGap: c.type === 'bar' },
    yAxis: { type: 'value' },
    series
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
