// 复制文本到剪贴板, 兼容 HTTP 环境。
//
// navigator.clipboard 仅在安全上下文 (HTTPS / localhost) 可用; 很多内网 HTTP 部署下
// 它是 undefined, 直接调用会失败 —— 看起来像"假复制"。这里优先用它, 失败/不可用时
// 回退到 document.execCommand('copy') (老 API, HTTP 下仍可用)。
// 返回 true 表示成功。
export async function copyText(text) {
  text = String(text ?? '')
  // 1) 现代 API (安全上下文)
  if (navigator.clipboard && window.isSecureContext) {
    try {
      await navigator.clipboard.writeText(text)
      return true
    } catch {
      // 落到下面的兜底
    }
  }
  // 2) execCommand 兜底 (HTTP 可用): 临时 textarea + 选中 + copy
  try {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.setAttribute('readonly', '')
    // 移出视口, 避免页面跳动/闪烁
    ta.style.position = 'fixed'
    ta.style.top = '-9999px'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    ta.setSelectionRange(0, ta.value.length)
    const ok = document.execCommand('copy')
    document.body.removeChild(ta)
    return ok
  } catch {
    return false
  }
}
