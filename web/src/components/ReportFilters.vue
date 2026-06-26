<script setup>
import { watch } from 'vue'

// 依据引擎返回的 filters 渲染交互输入控件, 通过 v-model 双向绑定 params。
const props = defineProps({
  filters: { type: Array, default: () => [] },
  modelValue: { type: Object, default: () => ({}) },
  loading: { type: Boolean, default: false }
})
const emit = defineEmits(['update:modelValue', 'run'])

function set(name, val) {
  emit('update:modelValue', { ...props.modelValue, [name]: val })
}

// date_range / time_range 在 params 里以 "from,to" 字符串保存,
// 而 el-date-picker(daterange) 用 [from, to] 数组, 这里做双向转换。
function rangeToArray(val) {
  if (!val) return []
  const parts = String(val).split(',').map(s => s.trim())
  return parts.length === 2 && parts[0] && parts[1] ? parts : []
}
function setRange(name, arr) {
  const val = arr && arr.length === 2 ? `${arr[0]},${arr[1]}` : ''
  set(name, val)
}

// enum 多选: params 里以逗号串保存, el-select(multiple) 用数组, 这里双向转换。
function enumToArray(val) {
  if (!val) return []
  return String(val).split(',').map(s => s.trim()).filter(Boolean)
}
function setEnum(name, val) {
  // 多选给数组 -> 拼逗号串; 单选给标量 -> 原样
  set(name, Array.isArray(val) ? val.join(',') : val)
}

// 把后端的 PHP 风格 format (Y-m / Y-m-d / Y-m-d H:i:s) 转成 Element Plus 的
// value-format (dayjs token), 并据此推断单/范围选择器的 type。
function phpToDayjs(fmt) {
  if (!fmt) return ''
  return fmt
    .replace(/Y/g, 'YYYY').replace(/y/g, 'YY')
    .replace(/m/g, 'MM').replace(/n/g, 'M')
    .replace(/d/g, 'DD').replace(/j/g, 'D')
    .replace(/H/g, 'HH').replace(/i/g, 'mm').replace(/s/g, 'ss')
}
// 判断 format 是否精确到"月" (只有年月, 无日)
function isMonthOnly(fmt) {
  return /Y/.test(fmt) && /m/i.test(fmt) && !/[dj]/.test(fmt)
}
function hasTime(fmt) {
  return /[His]/.test(fmt)
}

// 单值日期选择器配置
function dateConf(f) {
  const fmt = f.format || (f.type === 'time' ? 'Y-m-d H:i:s' : 'Y-m-d')
  const vf = phpToDayjs(fmt)
  if (isMonthOnly(fmt)) return { type: 'month', valueFormat: vf, width: '160px' }
  if (hasTime(fmt)) return { type: 'datetime', valueFormat: vf, width: '200px' }
  return { type: 'date', valueFormat: vf, width: '160px' }
}
// 范围日期选择器配置
function rangeConf(f) {
  const fmt = f.format || (f.type === 'time_range' ? 'Y-m-d H:i:s' : 'Y-m-d')
  const vf = phpToDayjs(fmt)
  if (isMonthOnly(fmt)) return { type: 'monthrange', valueFormat: vf, width: '280px' }
  if (hasTime(fmt)) return { type: 'datetimerange', valueFormat: vf, width: '360px' }
  return { type: 'daterange', valueFormat: vf, width: '280px' }
}

// filters 变化时, 用后端解析后的默认值 (resolved) 回填尚未设置的过滤项,
// 让输入控件首次渲染即带默认数据。
watch(
  () => props.filters,
  (filters) => {
    if (!filters || !filters.length) return
    const next = { ...props.modelValue }
    let changed = false
    for (const f of filters) {
      if (next[f.name] === undefined || next[f.name] === '') {
        const def = f.resolved ?? f.default ?? ''
        if (def !== '') {
          next[f.name] = def
          changed = true
        }
      }
    }
    if (changed) emit('update:modelValue', next)
  },
  { immediate: true, deep: true }
)
</script>

<template>
  <div v-if="filters.length" class="filters">
    <div class="filter-row">
      <div v-for="f in filters" :key="f.name" class="filter-item">
        <label>{{ f.label }}</label>
        <el-select
          v-if="f.type === 'enum'"
          :model-value="f.multiple ? enumToArray(modelValue[f.name]) : (modelValue[f.name] || '')"
          @update:model-value="v => setEnum(f.name, v)"
          :multiple="!!f.multiple" :collapse-tags="!!f.multiple" collapse-tags-tooltip
          placeholder="全部" clearable :style="{ width: f.multiple ? '220px' : '160px' }">
          <el-option v-for="o in f.options" :key="o.value" :label="o.label" :value="o.value" />
        </el-select>

        <el-switch
          v-else-if="f.type === 'bool'"
          :model-value="modelValue[f.name] === '1'"
          @update:model-value="v => set(f.name, v ? '1' : '')" />

        <el-date-picker
          v-else-if="f.type === 'date_range' || f.type === 'time_range'"
          :model-value="rangeToArray(modelValue[f.name])"
          @update:model-value="v => setRange(f.name, v)"
          :type="rangeConf(f).type" :value-format="rangeConf(f).valueFormat"
          range-separator="至" start-placeholder="开始" end-placeholder="结束"
          unlink-panels :style="{ width: rangeConf(f).width }" />

        <el-date-picker
          v-else-if="f.type === 'date' || f.type === 'time'"
          :model-value="modelValue[f.name] || ''"
          @update:model-value="v => set(f.name, v)"
          :type="dateConf(f).type" :value-format="dateConf(f).valueFormat"
          :style="{ width: dateConf(f).width }" />

        <el-input
          v-else
          :model-value="modelValue[f.name] || ''"
          @update:model-value="v => set(f.name, v)"
          :type="f.type === 'number' ? 'number' : 'text'" style="width: 160px" />
      </div>
      <el-button type="primary" :loading="loading" @click="emit('run')">查询</el-button>
    </div>
  </div>
</template>

<style scoped>
.filters { margin-bottom: 20px; padding-bottom: 16px; border-bottom: 1px solid var(--el-border-color-lighter); }
.filter-row { display: flex; flex-wrap: wrap; gap: 16px; align-items: flex-end; }
.filter-item { display: flex; flex-direction: column; gap: 4px; }
.filter-item label { font-size: 12px; color: var(--el-text-color-secondary); }
</style>
