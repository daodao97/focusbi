import { ref, onUnmounted } from 'vue'

// 自动刷新倒计时 (移植自 dataddy):
// 报表设置了 auto_refresh 间隔时, 每秒递减一个倒计时, 归零即调用 refreshFn 重查并重置;
// 可手动暂停/恢复 (暂停时秒数冻结)。refreshFn 通常是页面的 run(true) (旁路缓存)。
//
//   const { enabled, seconds, paused, toggle, arm, clear } = useAutoRefresh(() => run(true))
//   // 每次成功 run 之后调用 arm(result.auto_refresh) 重新拉起倒计时
//
// arm 以"间隔秒数"为参数: <=0 关闭 (enabled=false); >0 启动并把 seconds 重置为该值。
export function useAutoRefresh(refreshFn) {
  const enabled = ref(false) // 是否配置了自动刷新
  const seconds = ref(0)     // 倒计时剩余秒
  const paused = ref(false)  // 用户是否手动暂停
  let timer = null
  let interval = 0

  function clear() {
    if (timer) { clearInterval(timer); timer = null }
  }

  function tick() {
    if (paused.value) return // 暂停: 冻结秒数
    if (seconds.value > 0) {
      seconds.value--
    }
    if (seconds.value <= 0) {
      seconds.value = interval // 先重置, 避免 refresh 期间重复触发
      refreshFn()
    }
  }

  // arm(intervalSec): 启动/重置倒计时。每次 run 成功后调用。
  function arm(intervalSec) {
    const n = Math.max(0, Math.floor(Number(intervalSec) || 0))
    clear()
    interval = n
    enabled.value = n > 0
    if (!enabled.value) {
      seconds.value = 0
      return
    }
    seconds.value = n
    timer = setInterval(tick, 1000)
  }

  function toggle() {
    if (!enabled.value) return
    paused.value = !paused.value
  }

  onUnmounted(clear)

  return { enabled, seconds, paused, toggle, arm, clear }
}
