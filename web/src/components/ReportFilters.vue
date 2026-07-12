<script setup>
import { watch } from 'vue'
import { t } from '@/locale'

// 依据引擎返回的 filters 渲染交互输入控件, 通过 v-model 双向绑定 params。
const props = defineProps({
  filters: { type: Array, default: () => [] },
  modelValue: { type: Object, default: () => ({}) },
  loading: { type: Boolean, default: false }
})
const emit = defineEmits(['update:modelValue', 'run'])
let cascadeTimer

function set(name, val) {
  const next = { ...props.modelValue, [name]: val }
  // 沿依赖图清空全部下游过滤器，例如 province -> city -> district。
  const cleared = new Set()
	// 源节点预先标记为已访问，循环依赖 A -> B -> A 时不能清空用户刚设置的 A。
	const visited = new Set([name])
  const queue = [name]
  while (queue.length) {
    const parent = queue.shift()
    for (const f of props.filters) {
	  if (!visited.has(f.name) && (f.depends_on || []).includes(parent)) {
		visited.add(f.name)
        cleared.add(f.name)
        next[f.name] = ''
        queue.push(f.name)
      }
    }
  }
  emit('update:modelValue', next)
	if (cleared.size) {
	  clearTimeout(cascadeTimer)
	  cascadeTimer = setTimeout(() => emit('run'), 250)
	}
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
		<label>{{ f.label }}<span v-if="f.constraints?.required" class="required"> *</span></label>
		<small v-if="f.truncated" class="truncated">选项仅显示前 {{ f.row_limit }} 条</small>
        <el-select
          v-if="f.type === 'enum'"
          :model-value="f.multiple ? enumToArray(modelValue[f.name]) : (modelValue[f.name] || '')"
          @update:model-value="v => setEnum(f.name, v)"
          :multiple="!!f.multiple" :collapse-tags="!!f.multiple" collapse-tags-tooltip
          :placeholder="t('all')" clearable :style="{ width: f.multiple ? '220px' : '160px' }">
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
          :range-separator="t('rangeSep')" :start-placeholder="t('start')" :end-placeholder="t('end')"
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
		  :type="f.type === 'number' ? 'number' : 'text'"
		  :min="f.constraints?.min" :max="f.constraints?.max"
		  :minlength="f.constraints?.min_length" :maxlength="f.constraints?.max_length"
		  style="width: 160px" />
      </div>
      <el-button type="primary" :loading="loading" @click="emit('run')">{{ t('query') }}</el-button>
    </div>
  </div>
</template>

<style scoped>
.filters { margin-bottom: 20px; padding-bottom: 16px; border-bottom: 1px solid var(--el-border-color-lighter); }
.filter-row { display: flex; flex-wrap: wrap; gap: 16px; align-items: flex-end; }
.filter-item { display: flex; flex-direction: column; gap: 4px; }
.filter-item label { font-size: 12px; color: var(--el-text-color-secondary); }
.required { color: var(--el-color-danger); }
.truncated { color: var(--el-color-warning); font-size: 11px; }

/* 移动端: 控件固定宽 (160~360px) 会超出窗口, 改为每项占满整行, 输入控件随之 100%。 */
@media (max-width: 640px) {
  .filter-row { gap: 12px; }
  .filter-item { width: 100%; }
  .filter-item :deep(.el-select),
  .filter-item :deep(.el-input),
  .filter-item :deep(.el-date-editor) { width: 100% !important; }
  .filter-row > .el-button { width: 100%; }
}
</style>
