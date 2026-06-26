<script setup>
import { ref, watch } from 'vue'
import { marked } from 'marked'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  src: { type: String, default: 'SYNTAX.md' },              // 文档路径 (站点根下的 .md)
  title: { type: String, default: '开发文档 · 报表模板语法' } // 抽屉标题
})
const emit = defineEmits(['update:modelValue'])

const html = ref('')
const loading = ref(false)
const loaded = ref(false)

marked.setOptions({ gfm: true, breaks: false })

async function loadDoc() {
  if (loaded.value) return
  loading.value = true
  try {
    const res = await fetch(props.src, { headers: { Accept: 'text/markdown' } })
    if (!res.ok) throw new Error('HTTP ' + res.status)
    const md = await res.text()
    html.value = marked.parse(md)
    loaded.value = true
  } catch (e) {
    html.value = `<p style="color:#c0392b">文档加载失败: ${e.message}</p>`
  } finally {
    loading.value = false
  }
}

// 抽屉打开时按需加载
watch(() => props.modelValue, (open) => { if (open) loadDoc() })
</script>

<template>
  <el-drawer
    :model-value="modelValue" @update:model-value="v => emit('update:modelValue', v)"
    :title="title" direction="rtl" size="46%">
    <div v-loading="loading" class="doc-body markdown-body" v-html="html"></div>
  </el-drawer>
</template>

<style scoped>
.doc-body { padding: 0 4px 32px; font-size: 14px; line-height: 1.7; color: var(--el-text-color-primary); }
</style>

<style>
/* markdown 友好排版 (非 scoped, 作用于 v-html 内容) */
.markdown-body h1 { font-size: 22px; margin: 18px 0 12px; padding-bottom: 8px; border-bottom: 1px solid var(--el-border-color-lighter); }
.markdown-body h2 { font-size: 18px; margin: 22px 0 10px; padding-bottom: 6px; border-bottom: 1px solid var(--el-border-color-lighter); }
.markdown-body h3 { font-size: 15px; margin: 18px 0 8px; }
.markdown-body p { margin: 8px 0; }
.markdown-body ul, .markdown-body ol { padding-left: 22px; margin: 8px 0; }
.markdown-body li { margin: 4px 0; }
.markdown-body code {
  background: var(--el-fill-color-light); padding: 2px 5px; border-radius: 4px;
  font-family: "SF Mono", Monaco, Consolas, monospace; font-size: 12.5px; color: var(--el-color-danger);
}
.markdown-body pre {
  background: var(--el-fill-color-light); padding: 14px 16px; border-radius: 8px; overflow-x: auto;
  border: 1px solid var(--el-border-color-lighter); margin: 12px 0;
}
.markdown-body pre code { background: none; padding: 0; color: var(--el-text-color-primary); font-size: 12.5px; }
.markdown-body table { border-collapse: collapse; width: 100%; margin: 12px 0; font-size: 13px; }
.markdown-body th, .markdown-body td { border: 1px solid var(--el-border-color-light); padding: 6px 12px; text-align: left; }
.markdown-body th { background: var(--el-fill-color-light); font-weight: 600; }
.markdown-body tr:nth-child(2n) { background: var(--el-fill-color-lighter); }
.markdown-body blockquote { margin: 12px 0; padding: 0 14px; color: var(--el-text-color-secondary); border-left: 3px solid var(--el-border-color-light); }
.markdown-body a { color: var(--el-color-primary); text-decoration: none; }
.markdown-body a:hover { text-decoration: underline; }
.markdown-body hr { border: none; border-top: 1px solid var(--el-border-color-lighter); margin: 20px 0; }
</style>
