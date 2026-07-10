import DOMPurify from 'dompurify'

// 报表作者可提供说明性富文本, 但不能注入脚本、表单或可执行嵌入内容。
export function sanitizeReportHtml(html) {
  return DOMPurify.sanitize(String(html || ''), {
    USE_PROFILES: { html: true },
    FORBID_TAGS: ['script', 'style', 'iframe', 'object', 'embed', 'form', 'input', 'button', 'textarea', 'select', 'meta', 'link', 'base'],
    FORBID_ATTR: ['srcdoc'],
    ALLOW_UNKNOWN_PROTOCOLS: false
  })
}

const safeLinkProtocols = new Set(['http:', 'https:', 'mailto:', 'tel:'])

// 列链接允许站内相对地址和常见外部协议; javascript:/data:/blob: 一律拒绝。
export function sanitizeReportHref(href) {
  const value = String(href || '').trim()
  if (!value) return ''
  try {
    const parsed = new URL(value, window.location.origin)
    return safeLinkProtocols.has(parsed.protocol) ? value : ''
  } catch {
    return ''
  }
}
