// 把扁平的报表列表 (含 parent_id / type) 组装成树。
// 每个节点: { ...report, children: [] }。根节点 parent_id=0。

export function buildTree(list) {
  const byId = new Map()
  for (const r of list) byId.set(r.id, { ...r, children: [] })

  const roots = []
  for (const node of byId.values()) {
    const parent = node.parent_id && byId.get(node.parent_id)
    if (parent) parent.children.push(node)
    else roots.push(node)
  }

  // 排序: 文件夹在前, 再按 sort, 再按 name
  const sortNodes = (nodes) => {
    nodes.sort((a, b) => {
      const af = a.type === 'folder' ? 0 : 1
      const bf = b.type === 'folder' ? 0 : 1
      if (af !== bf) return af - bf
      if ((a.sort || 0) !== (b.sort || 0)) return (a.sort || 0) - (b.sort || 0)
      return (a.name || '').localeCompare(b.name || '')
    })
    for (const n of nodes) if (n.children.length) sortNodes(n.children)
  }
  sortNodes(roots)
  return roots
}

// 只取文件夹, 组成树 (用于"所属文件夹"选择器 / 角色权限树)。
export function buildFolderTree(list) {
  return pruneToFolders(buildTree(list))
}
function pruneToFolders(nodes) {
  return nodes
    .filter(n => n.type === 'folder')
    .map(n => ({ ...n, children: pruneToFolders(n.children) }))
}

// el-cascader / el-tree-select 需要的 {value,label,children} 形态 (文件夹)。
export function folderOptions(list) {
  const conv = (nodes) => nodes.map(n => ({
    value: n.id, label: n.name,
    children: n.children.length ? conv(n.children) : undefined
  }))
  return conv(buildFolderTree(list))
}
