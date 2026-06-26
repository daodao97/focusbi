// 全局权限状态: 缓存当前用户的 is_admin 与已编译的 resources, 提供 can() 判断。
// 判断逻辑与后端 internal/auth 引擎一致: R 递归 / * 通配 / r·w 读写。
import { reactive } from 'vue'
import { api } from '@/api'

export const perm = reactive({
  loaded: false,
  isAdmin: false,
  resources: {} // { "report": "Rr", "report.5": "rw", "dsn": "r", ... }
})

// 拉取当前用户权限 (登录后 / 应用初始化时调用)。
export async function loadPerm() {
  try {
    const me = await api.me()
    perm.isAdmin = !!me.is_admin
    perm.resources = me.resources || {}
    perm.user = me
  } catch {
    perm.isAdmin = false
    perm.resources = {}
    perm.user = null
  } finally {
    perm.loaded = true
  }
  return perm
}

export function clearPerm() {
  perm.loaded = false
  perm.isAdmin = false
  perm.resources = {}
  perm.user = null
}

// mode 串是否包含 want 的所有字符 (R 区分大小写: 递归标记)
function hasMode(mode, want) {
  for (const c of want) if (!mode.includes(c)) return false
  return true
}

// can(resource, mode): 当前用户对 resource 是否有 mode 权限。
// 资源串按 "." 分段匹配: 精确命中 / 祖先节点带 R 递归 / * 通配。
export function can(resource, mode = 'r') {
  if (perm.isAdmin) return true
  const res = String(resource).toLowerCase()
  const segs = res.split('.')

  // 逐段构造前缀, 命中规则:
  //   - 完整资源精确命中 (任意模式满足)
  //   - 某祖先前缀带 R 且模式满足 (递归覆盖)
  //   - 顶层 * 通配
  if (perm.resources['*'] && hasMode(perm.resources['*'], mode)) return true

  for (let i = 0; i < segs.length; i++) {
    const prefix = segs.slice(0, i + 1).join('.')
    const m = perm.resources[prefix]
    if (!m) continue
    const isLast = i === segs.length - 1
    if ((isLast || hasMode(m, 'R')) && hasMode(m, mode)) return true
  }
  return false
}

// 便捷判断
export const canManageReports = () => can('report.manage', 'rw')
export const canWriteDsn = () => can('dsn', 'rw')
// 全局数据源读 (覆盖所有源)
export const canReadAnyDsn = () => can('dsn', 'r')
// 某具体数据源是否可读: 全局 dsn:r 覆盖一切; 否则按 dsn.<id> / dsn.default 判定。
export const canReadDsnById = (id) => {
  if (can('dsn', 'r')) return true
  const res = (!id || id === 'default') ? 'dsn.default' : `dsn.${id}`
  return can(res, 'r')
}
