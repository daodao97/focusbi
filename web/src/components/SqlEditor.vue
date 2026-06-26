<script setup>
import { ref, shallowRef, onMounted, onBeforeUnmount, watch } from 'vue'
import { useTheme } from '@/theme'
// edcore.main: 编辑器核心 + 全部编辑功能/快捷键 (查找替换、注释、多光标、折叠、
// 命令面板 F1、跳转行 Ctrl+G 等), 但不含各语言服务, 避免打包体积膨胀。
import * as monaco from 'monaco-editor/esm/vs/editor/edcore.main'
import 'monaco-editor/esm/vs/basic-languages/sql/sql.contribution'
import EditorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker'

// Monaco 在 Vite 下需要指定 worker。报表模板只用到 sql 高亮, 用基础 editor worker 即可。
self.MonacoEnvironment = {
  getWorker() {
    return new EditorWorker()
  }
}

// MySQL 方言: monaco 内置 sql 语言, 这里补充常用 MySQL 关键字/函数的自动补全。
const MYSQL_KEYWORDS = [
  'SELECT', 'FROM', 'WHERE', 'GROUP BY', 'ORDER BY', 'HAVING', 'LIMIT', 'OFFSET',
  'JOIN', 'LEFT JOIN', 'RIGHT JOIN', 'INNER JOIN', 'ON', 'AS', 'AND', 'OR', 'NOT',
  'IN', 'LIKE', 'BETWEEN', 'IS NULL', 'IS NOT NULL', 'DISTINCT', 'UNION', 'UNION ALL',
  'INSERT INTO', 'VALUES', 'UPDATE', 'SET', 'DELETE', 'WITH', 'CASE', 'WHEN', 'THEN',
  'ELSE', 'END', 'ASC', 'DESC'
]
const MYSQL_FUNCS = [
  'COUNT', 'SUM', 'AVG', 'MAX', 'MIN', 'IFNULL', 'COALESCE', 'CONCAT', 'CONCAT_WS',
  'DATE_FORMAT', 'DATE_SUB', 'DATE_ADD', 'NOW', 'CURDATE', 'UNIX_TIMESTAMP', 'FROM_UNIXTIME',
  'ROUND', 'FLOOR', 'CEIL', 'ABS', 'IF', 'GROUP_CONCAT', 'SUBSTRING', 'LOWER', 'UPPER', 'CAST'
]

// 显式注册 SQL 的注释配置, 保证 Cmd/Ctrl+/ 行注释、Shift+Alt+A 块注释立即可用
// (不依赖 sql.js 的懒加载时机)。
let langConfigured = false
function registerSqlLangConfig() {
  if (langConfigured) return
  langConfigured = true
  monaco.languages.setLanguageConfiguration('sql', {
    comments: { lineComment: '--', blockComment: ['/*', '*/'] },
    brackets: [['(', ')'], ['[', ']'], ['{', '}']],
    autoClosingPairs: [
      { open: '(', close: ')' },
      { open: '[', close: ']' },
      { open: "'", close: "'" },
      { open: '"', close: '"' },
      { open: '`', close: '`' }
    ]
  })
}

let completionProvider = null
function registerMysqlCompletion() {
  if (completionProvider) return
  completionProvider = monaco.languages.registerCompletionItemProvider('sql', {
    provideCompletionItems(model, position) {
      const word = model.getWordUntilPosition(position)
      const range = {
        startLineNumber: position.lineNumber,
        endLineNumber: position.lineNumber,
        startColumn: word.startColumn,
        endColumn: word.endColumn
      }
      const suggestions = [
        ...MYSQL_KEYWORDS.map(k => ({
          label: k, kind: monaco.languages.CompletionItemKind.Keyword, insertText: k, range
        })),
        ...MYSQL_FUNCS.map(f => ({
          label: f, kind: monaco.languages.CompletionItemKind.Function,
          insertText: `${f}()`, range
        }))
      ]
      return { suggestions }
    }
  })
}

const props = defineProps({
  modelValue: { type: String, default: '' },
  readonly: { type: Boolean, default: false },
  height: { type: String, default: '420px' }
})
const emit = defineEmits(['update:modelValue', 'save'])

const el = ref(null)
const editor = shallowRef(null)

// Monaco 有独立主题系统 (不吃 CSS 变量), 跟随全局暗黑: vs-dark / vs。
const { isDark } = useTheme()
const monacoTheme = () => (isDark.value ? 'vs-dark' : 'vs')

onMounted(() => {
  registerSqlLangConfig()
  registerMysqlCompletion()
  editor.value = monaco.editor.create(el.value, {
    value: props.modelValue,
    language: 'sql',
    theme: monacoTheme(),
    automaticLayout: true,
    fontSize: 13,
    fontFamily: 'SF Mono, Monaco, Consolas, monospace',
    minimap: { enabled: false },
    scrollBeyondLastLine: false,
    tabSize: 2,
    lineNumbers: 'on',
    renderLineHighlight: 'line',
    wordWrap: 'on',
    readOnly: props.readonly,
    // 只读时隐藏光标行高亮与右键改写类菜单, 更像"查看器"
    domReadOnly: props.readonly,
    // 启用常用编辑能力
    folding: true,
    multiCursorModifier: 'alt',
    formatOnPaste: !props.readonly,
    find: { addExtraSpaceOnTop: false }
  })
  if (!props.readonly) {
    editor.value.onDidChangeModelContent(() => {
      const v = editor.value.getValue()
      if (v !== props.modelValue) emit('update:modelValue', v)
    })

    // Cmd/Ctrl+S: 保存 (交给父组件处理), 阻止浏览器默认保存
    editor.value.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => {
      emit('save')
    })
    // Shift+Alt+F: 格式化文档 (内置 action)
    editor.value.addCommand(
      monaco.KeyMod.Shift | monaco.KeyMod.Alt | monaco.KeyCode.KeyF,
      () => editor.value.getAction('editor.action.formatDocument')?.run()
    )
  }
})

onBeforeUnmount(() => {
  editor.value && editor.value.dispose()
})

// 全局暗黑切换时更新 Monaco 主题 (setTheme 是全局作用, 影响所有编辑器实例)
watch(isDark, () => {
  if (editor.value) monaco.editor.setTheme(monacoTheme())
})

// 外部 (如 AI 修改) 更新 content 时同步到编辑器
watch(() => props.modelValue, (v) => {
  if (editor.value && v !== editor.value.getValue()) {
    editor.value.setValue(v || '')
  }
})
</script>

<template>
  <div ref="el" class="sql-editor" :style="{ height }"></div>
</template>

<style scoped>
.sql-editor {
  width: 100%;
  border: 1px solid var(--el-border-color);
  border-radius: 4px;
  overflow: hidden;
}
</style>
