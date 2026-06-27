<script setup>
import { InfoFilled } from '@element-plus/icons-vue'

defineProps({
  timing: { type: Object, default: null },
  scope: { type: String, default: 'block' }
})

function formatMs(ms) {
  if (ms === undefined || ms === null) return '-'
  const n = Number(ms)
  if (!Number.isFinite(n)) return '-'
  if (n < 1000) return `${n} ms`
  return `${(n / 1000).toFixed(n < 10000 ? 2 : 1)} s`
}
</script>

<template>
  <el-tooltip v-if="timing" placement="top" effect="dark">
    <template #content>
      <div class="timing-tip">
        <template v-if="scope === 'report'">
          <div>总计: {{ formatMs(timing.total_ms) }}</div>
          <div>解析区块: {{ timing.parsed_blocks || 0 }}</div>
          <div>输出区块: {{ timing.output_blocks || 0 }}</div>
        </template>
        <template v-else>
          <div>总计: {{ formatMs(timing.total_ms) }}</div>
          <div>解析: {{ formatMs(timing.parse_ms) }}</div>
          <div>执行: {{ formatMs(timing.exec_ms) }}</div>
          <div v-if="timing.dsn">数据源: {{ timing.dsn }}</div>
          <div v-if="timing.rows || timing.columns">
            结果: {{ timing.rows || 0 }} 行 / {{ timing.columns || 0 }} 列
          </div>
          <div v-if="timing.sql_len">SQL: {{ timing.sql_len }} 字符</div>
          <div v-if="timing.produced_blocks">产出: {{ timing.produced_blocks }} 块</div>
          <div v-if="timing.error" class="timing-error">{{ timing.error }}</div>
        </template>
      </div>
    </template>
    <el-icon class="timing-icon" tabindex="0"><InfoFilled /></el-icon>
  </el-tooltip>
</template>

<style scoped>
.timing-icon {
  font-size: 13px;
  color: var(--el-text-color-placeholder);
  cursor: help;
  outline: none;
  vertical-align: -2px;
}
.timing-icon:hover,
.timing-icon:focus { color: var(--el-color-primary); }
.timing-tip { line-height: 1.7; white-space: nowrap; }
.timing-error { max-width: 360px; white-space: normal; color: var(--el-color-danger-light-5); }
</style>
