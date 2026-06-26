// 过滤器参数与 URL query 之间的序列化/反序列化, 用于分享与复现报表筛选状态。

// 过滤值 -> query 对象 (丢弃空值)
export function paramsToQuery(params) {
  const q = {}
  for (const [k, v] of Object.entries(params || {})) {
    if (v !== undefined && v !== null && v !== '') q[k] = v
  }
  return q
}

// query 对象 -> 过滤值 (值统一转字符串; 数组取首项)
export function queryToParams(query, exclude = []) {
  const p = {}
  for (const [k, v] of Object.entries(query || {})) {
    if (exclude.includes(k)) continue
    p[k] = Array.isArray(v) ? String(v[0] ?? '') : String(v ?? '')
  }
  return p
}

// 两个 query 对象是否等价 (避免无意义的路由跳转)
export function sameQuery(a, b) {
  const ka = Object.keys(a || {})
  const kb = Object.keys(b || {})
  if (ka.length !== kb.length) return false
  return ka.every(k => String(a[k]) === String(b[k]))
}
