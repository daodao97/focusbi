// 暗黑模式: 支持 auto (跟随系统) / light / dark 三态, 持久化到 localStorage。
// Element Plus 2.x 暗色靠给 <html> 加 .dark 类 + 引入 dark/css-vars.css 实现。
import { ref, computed } from 'vue'

const KEY = 'focusbi_theme'           // 'auto' | 'light' | 'dark'
const mode = ref(localStorage.getItem(KEY) || 'auto')

const media = window.matchMedia ? window.matchMedia('(prefers-color-scheme: dark)') : null

// 当前是否暗色 (auto 时取系统)
const isDark = computed(() => mode.value === 'dark' || (mode.value === 'auto' && !!media && media.matches))

// 把 isDark 应用到 <html>.dark
function apply() {
  document.documentElement.classList.toggle('dark', isDark.value)
}

// auto 模式下跟随系统变化
if (media) {
  media.addEventListener('change', () => { if (mode.value === 'auto') apply() })
}

function setMode(m) {
  mode.value = m
  localStorage.setItem(KEY, m)
  apply()
}

// 点击循环切换: auto -> light -> dark -> auto
function cycle() {
  setMode(mode.value === 'auto' ? 'light' : mode.value === 'light' ? 'dark' : 'auto')
}

// 应用启动时立即应用一次 (避免首屏闪烁)
apply()

export function useTheme() {
  return { mode, isDark, setMode, cycle }
}
