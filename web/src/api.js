// 统一的后端接口封装。后端约定: { code: 0, data } 成功; { code: 1, msg } 失败。
const BASE = (import.meta.env.VITE_BASE_API || '/api')

const TOKEN_KEY = 'focusbi_token'
export function getToken() { return localStorage.getItem(TOKEN_KEY) || '' }
export function setToken(t) { t ? localStorage.setItem(TOKEN_KEY, t) : localStorage.removeItem(TOKEN_KEY) }
export function clearToken() { localStorage.removeItem(TOKEN_KEY) }

// 401 回调: 由应用注入 (跳登录页)。
let onUnauthorized = null
export function setUnauthorizedHandler(fn) { onUnauthorized = fn }

async function request(method, url, body) {
  const headers = { 'Content-Type': 'application/json' }
  const token = getToken()
  if (token) headers['Authorization'] = 'Bearer ' + token

  const opt = { method, headers }
  if (body !== undefined) opt.body = JSON.stringify(body)
  const res = await fetch(BASE + url, opt)

  // 仅当"携带 token 的请求"被拒, 才判定为会话失效并跳登录。
  // 登录/注册等本就无 token 的请求, 401 是业务错误 (如密码错误), 应透传真实提示。
  if (res.status === 401 && token) {
    clearToken()
    if (onUnauthorized) onUnauthorized()
    throw new Error('登录已失效')
  }

  let json
  try {
    json = await res.json()
  } catch {
    throw new Error(`HTTP ${res.status}`)
  }
  if (json.code !== 0) throw new Error(json.msg || `HTTP ${res.status}`)
  return json.data
}

async function streamRequest(method, url, body, onEvent) {
  const headers = { 'Content-Type': 'application/json' }
  const token = getToken()
  if (token) headers['Authorization'] = 'Bearer ' + token

  const opt = { method, headers }
  if (body !== undefined) opt.body = JSON.stringify(body)
  const res = await fetch(BASE + url, opt)
  if (res.status === 401 && token) {
    clearToken()
    if (onUnauthorized) onUnauthorized()
    throw new Error('登录已失效')
  }
  if (!res.ok || !res.body) throw new Error(`HTTP ${res.status}`)

  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  while (true) {
    const { value, done } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })
    let idx
    while ((idx = buffer.indexOf('\n\n')) >= 0) {
      const block = buffer.slice(0, idx)
      buffer = buffer.slice(idx + 2)
      const evt = parseSSEBlock(block)
      if (evt && onEvent) onEvent(evt)
      if (evt?.event === 'error') throw new Error(evt.data?.message || 'AI 请求失败')
    }
  }
  buffer += decoder.decode()
  const evt = parseSSEBlock(buffer)
  if (evt && onEvent) onEvent(evt)
  if (evt?.event === 'error') throw new Error(evt.data?.message || 'AI 请求失败')
}

function parseSSEBlock(block) {
  const lines = block.split(/\r?\n/)
  let event = 'message'
  const data = []
  for (const line of lines) {
    if (line.startsWith('event:')) event = line.slice(6).trim()
    else if (line.startsWith('data:')) data.push(line.slice(5).trimStart())
  }
  if (!data.length) return null
  try {
    return { event, data: JSON.parse(data.join('\n')) }
  } catch {
    return { event, data: data.join('\n') }
  }
}

export const api = {
  // 认证
  bootstrap: () => request('GET', '/auth/bootstrap'),
  register: (username, password, turnstileToken = '') => request('POST', '/auth/register', { username, password, turnstile_token: turnstileToken }),
  login: (username, password, turnstileToken = '') => request('POST', '/auth/login', { username, password, turnstile_token: turnstileToken }),
  me: () => request('GET', '/auth/me'),
  logout: () => request('POST', '/auth/logout'),

  // 用户管理 (admin)
  listUsers: () => request('GET', '/user'),
  createUser: (u) => request('POST', '/user', u),
  updateUser: (id, u) => request('PUT', `/user/${id}`, u),
  deleteUser: (id) => request('DELETE', `/user/${id}`),

  // 角色管理 (admin)
  listRoles: () => request('GET', '/role'),
  createRole: (r) => request('POST', '/role', r),
  updateRole: (id, r) => request('PUT', `/role/${id}`, r),
  deleteRole: (id) => request('DELETE', `/role/${id}`),

  // 数据源
  listDsn: () => request('GET', '/dsn'),
  createDsn: (d) => request('POST', '/dsn', d),
  updateDsn: (id, d) => request('PUT', `/dsn/${id}`, d),
  deleteDsn: (id) => request('DELETE', `/dsn/${id}`),
  testDsn: (d) => request('POST', '/dsn/test', d),
  listDatabases: (name) => request('GET', `/dsn/${encodeURIComponent(name)}/databases`),
  listTables: (name, db) => request('GET', `/dsn/${encodeURIComponent(name)}/tables${db ? `?db=${encodeURIComponent(db)}` : ''}`),
  listColumns: (name, db, table) => request('GET', `/dsn/${encodeURIComponent(name)}/columns?db=${encodeURIComponent(db || '')}&table=${encodeURIComponent(table)}`),

  // 报表 (含文件夹: type='folder')
  listReports: () => request('GET', '/report'),
  getReport: (id) => request('GET', `/report/${id}`),
  createReport: (r) => request('POST', '/report', r),
  updateReport: (id, r) => request('PUT', `/report/${id}`, r),
  publishReport: (id) => request('POST', `/report/${id}/publish`),
  listReportVersions: (id) => request('GET', `/report/${id}/version`),
  getReportVersion: (id, vid) => request('GET', `/report/${id}/version/${vid}`),
  rollbackReport: (id, vid) => request('POST', `/report/${id}/version/${vid}/rollback`),
  deleteReport: (id) => request('DELETE', `/report/${id}`),
  // 文件夹便捷封装
  createFolder: (name, parent_id = 0) => request('POST', '/report', { name, type: 'folder', parent_id }),
  // 拖拽排序/移动: items=[{id,parent_id,sort}]
  reorderReports: (items) => request('POST', '/report/reorder', { items }),

  // 执行 / 预览 / AI
  runReport: (id, params) => request('POST', `/report/${id}/run`, { params }),
  previewReport: (payload) => request('POST', '/report/preview', payload),
  aiModify: (content, instruction, schema, history = []) => request('POST', '/report/ai', { content, instruction, schema, history }),
  aiModifyStream: (content, instruction, schema, history = [], onEvent) =>
    streamRequest('POST', '/report/ai/stream', { content, instruction, schema, history }, onEvent),

  // 分享 (管理端开关)
  setShare: (id, enable) => request('POST', `/report/${id}/share`, { enable }),

  // 定时订阅推送 (飞书/企微)
  listAllSubscriptions: () => request('GET', '/report/subscriptions'),
  listSubscriptions: (rid) => request('GET', `/report/${rid}/subscription`),
  getSubscription: (rid, sid) => request('GET', `/report/${rid}/subscription/${sid}`),
  createSubscription: (rid, s) => request('POST', `/report/${rid}/subscription`, s),
  updateSubscription: (rid, sid, s) => request('PUT', `/report/${rid}/subscription/${sid}`, s),
  deleteSubscription: (rid, sid) => request('DELETE', `/report/${rid}/subscription/${sid}`),
  testSubscription: (rid, sid) => request('POST', `/report/${rid}/subscription/${sid}/test`),

  // API Token (供 MCP 等程序化访问; 明文仅创建时返回一次)
  listApiTokens: () => request('GET', '/token'),
  createApiToken: (name, expireDays = 0) => request('POST', '/token', { name, expire_days: expireDays }),
  deleteApiToken: (id) => request('DELETE', `/token/${id}`),

  // 公开访问 (无需登录, 凭 share_token)
  publicGetReport: (token) => request('GET', `/public/report/${encodeURIComponent(token)}`),
  publicRunReport: (token, params) => request('POST', `/public/report/${encodeURIComponent(token)}/run`, { params })
}
