<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Document, Folder, MoreFilled, Hide } from '@element-plus/icons-vue'
import { api } from '@/api'
import { buildTree, folderOptions } from '@/tree'
import { canManageReports } from '@/perm'
import ReportTreeMenu from '../components/ReportTreeMenu.vue'
import ReportEditor from '../components/ReportEditor.vue'

const router = useRouter()
const flat = ref([])
const loading = ref(false)
const treeRef = ref(null)
const canManage = computed(() => canManageReports())

const tree = computed(() => buildTree(flat.value))
const folderOpts = computed(() => folderOptions(flat.value))

// 拖拽放置规则: inner(放进节点内部) 仅当目标是文件夹; prev/next(同级排序) 始终允许。
function allowDrop(draggingNode, dropNode, type) {
  if (type === 'inner') return dropNode.data.type === 'folder'
  return true
}

// 拖拽结束: 计算受影响节点的新 parent_id + sort, 持久化。
async function onDrop(draggingNode, dropNode, dropType) {
  // 落点所在的父节点 id: inner -> dropNode 自身; prev/next -> dropNode 的父
  let parentId = 0
  if (dropType === 'inner') {
    parentId = dropNode.data.id
  } else {
    parentId = dropNode.parent && dropNode.parent.data ? (dropNode.parent.data.id || 0) : 0
  }

  // 取该父级下当前的子节点顺序 (拖拽后 el-tree 已更新 data), 重排 sort
  const siblings = parentId === 0
    ? (treeRef.value?.data || [])
    : (findNode(treeRef.value?.data || [], parentId)?.children || [])

  const items = siblings.map((n, i) => ({ id: n.id, parent_id: parentId, sort: i }))
  try {
    await api.reorderReports(items)
    await load() // 重新拉取, 保证 flat 与服务端一致
  } catch (e) {
    ElMessage.error('移动失败: ' + e.message)
    await load() // 回滚到服务端状态
  }
}

// 在树里按 id 找节点
function findNode(nodes, id) {
  for (const n of nodes) {
    if (n.id === id) return n
    if (n.children) {
      const f = findNode(n.children, id)
      if (f) return f
    }
  }
  return null
}

// 右侧编辑器状态: selected = 报表 id (编辑已有); 'new' = 新建; 0 = 空
const selected = ref(0)
const newParent = ref(0)
const editorKey = ref(0) // 强制刷新编辑器 (新建/切换)

async function load() {
  loading.value = true
  try {
    flat.value = await api.listReports()
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    loading.value = false
  }
}

// 左树点击报表:
//   有编辑权 -> 右侧进编辑器; 只读 -> 跳查看页
function selectReport(id) {
  if (!canManage.value) {
    router.push(`/reports/${id}`)
    return
  }
  selected.value = id
  newParent.value = 0
  editorKey.value++
}
function newReportIn(folderId) {
  selected.value = 'new'
  newParent.value = folderId || 0
  editorKey.value++
}

function onSaved() {
  load()           // 刷新左树 (名称/层级可能变); 提示由编辑器内部给出, 不重复
}

async function remove(node) {
  const msg = node.type === 'folder' ? `确认删除文件夹「${node.name}」? (需先清空)` : `确认删除报表「${node.name}」?`
  await ElMessageBox.confirm(msg, '提示', { type: 'warning' })
  try {
    await api.deleteReport(node.id)
    if (selected.value === node.id) selected.value = 0
    ElMessage.success('已删除')
    load()
  } catch (e) {
    ElMessage.error(e.message)
  }
}

function viewReport(id) { router.push(`/reports/${id}`) }

// ---- 文件夹 / 移动 弹窗 ----
const folderDialog = ref(false)
const folderForm = reactive({ id: 0, name: '', parent_id: 0 })
function newFolder() { Object.assign(folderForm, { id: 0, name: '', parent_id: 0 }); folderDialog.value = true }
function renameFolder(node) { Object.assign(folderForm, { id: node.id, name: node.name, parent_id: node.parent_id }); folderDialog.value = true }
async function saveFolder() {
  if (!folderForm.name.trim()) { ElMessage.warning('请输入名称'); return }
  try {
    if (folderForm.id) {
      await api.updateReport(folderForm.id, { name: folderForm.name, type: 'folder', parent_id: folderForm.parent_id || 0, content: '' })
    } else {
      await api.createFolder(folderForm.name, folderForm.parent_id || 0)
    }
    folderDialog.value = false
    load()
  } catch (e) { ElMessage.error(e.message) }
}

const moveDialog = ref(false)
const moveForm = reactive({ id: 0, name: '', type: 'report', parent_id: 0, dsn: '', content: '' })
function openMove(node) {
  Object.assign(moveForm, { id: node.id, name: node.name, type: node.type, parent_id: node.parent_id, dsn: node.dsn, content: node.content })
  moveDialog.value = true
}
async function saveMove() {
  try {
    const body = { name: moveForm.name, type: moveForm.type, parent_id: moveForm.parent_id || 0, dsn: moveForm.dsn }
    if (moveForm.type !== 'folder') body.content = moveForm.content
    await api.updateReport(moveForm.id, body)
    moveDialog.value = false
    load()
  } catch (e) { ElMessage.error(e.message) }
}
const moveTargets = computed(() => excludeSelf(folderOpts.value, moveForm.id))
function excludeSelf(opts, id) {
  return opts.filter(o => o.value !== id).map(o => ({ ...o, children: o.children ? excludeSelf(o.children, id) : undefined }))
}

onMounted(load)
</script>

<template>
  <div class="workspace">
    <!-- 左: 文件树 (名称 + 操作) -->
    <aside class="tree-pane" v-loading="loading">
      <div class="tree-head">
        <span>报表</span>
        <div v-if="canManage" class="head-actions">
          <el-button link size="small" @click="newFolder">+文件夹</el-button>
          <el-button link type="primary" size="small" @click="newReportIn(0)">+报表</el-button>
        </div>
      </div>
      <el-tree
        ref="treeRef"
        :data="tree" node-key="id" default-expand-all :expand-on-click-node="false"
        :draggable="canManage" :allow-drop="allowDrop" @node-drop="onDrop"
        :props="{ label: 'name', children: 'children' }">
        <template #default="{ node, data }">
          <span class="tree-node" :class="{ active: selected === data.id }"
            @click="data.type === 'folder' ? null : selectReport(data.id)">
            <span class="node-label">
              <el-icon class="node-icon">
                <Folder v-if="data.type === 'folder'" />
                <Document v-else />
              </el-icon>
              <span>{{ data.name }}</span>
              <el-tooltip v-if="data.type !== 'folder' && data.visible === false" content="已隐藏 (不在侧边菜单显示)" placement="top">
                <el-icon class="hidden-icon"><Hide /></el-icon>
              </el-tooltip>
            </span>
            <span class="node-ops" @click.stop>
              <template v-if="data.type === 'folder'">
                <el-dropdown v-if="canManage" trigger="click" @command="cmd => cmd(data)">
                  <el-icon class="more"><MoreFilled /></el-icon>
                  <template #dropdown>
                    <el-dropdown-menu>
                      <el-dropdown-item :command="d => newReportIn(d.id)">新建报表</el-dropdown-item>
                      <el-dropdown-item :command="renameFolder">重命名</el-dropdown-item>
                      <el-dropdown-item :command="openMove">移动</el-dropdown-item>
                      <el-dropdown-item :command="remove" divided>删除</el-dropdown-item>
                    </el-dropdown-menu>
                  </template>
                </el-dropdown>
              </template>
              <template v-else>
                <el-dropdown trigger="click" @command="cmd => cmd(data)">
                  <el-icon class="more"><MoreFilled /></el-icon>
                  <template #dropdown>
                    <el-dropdown-menu>
                      <el-dropdown-item :command="d => viewReport(d.id)">查看</el-dropdown-item>
                      <template v-if="canManage">
                        <el-dropdown-item :command="openMove">移动</el-dropdown-item>
                        <el-dropdown-item :command="remove" divided>删除</el-dropdown-item>
                      </template>
                    </el-dropdown-menu>
                  </template>
                </el-dropdown>
              </template>
            </span>
          </span>
        </template>
      </el-tree>
    </aside>

    <!-- 右: 编辑器 -->
    <section class="editor-pane">
      <ReportEditor v-if="selected" :key="editorKey"
        :id="selected === 'new' ? 0 : selected" :parent-id="newParent"
        @saved="onSaved" />
      <el-empty v-else :description="canManage ? '从左侧选择报表编辑, 或点「+报表」新建' : '从左侧选择报表查看'" />
    </section>

    <!-- 文件夹弹窗 -->
    <el-dialog v-model="folderDialog" :title="folderForm.id ? '重命名文件夹' : '新建文件夹'" width="460px">
      <el-form label-width="90px">
        <el-form-item label="名称"><el-input v-model="folderForm.name" /></el-form-item>
        <el-form-item label="所属文件夹">
          <el-tree-select v-model="folderForm.parent_id" :data="folderOpts" check-strictly placeholder="根目录" clearable style="width:100%" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="folderDialog = false">取消</el-button>
        <el-button type="primary" @click="saveFolder">保存</el-button>
      </template>
    </el-dialog>

    <!-- 移动弹窗 -->
    <el-dialog v-model="moveDialog" title="移动到" width="460px">
      <el-form label-width="90px">
        <el-form-item label="目标文件夹">
          <el-tree-select v-model="moveForm.parent_id" :data="moveTargets" check-strictly placeholder="根目录" clearable style="width:100%" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="moveDialog = false">取消</el-button>
        <el-button type="primary" @click="saveMove">移动</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.workspace { display: flex; gap: 16px; height: 100%; min-height: 0; }
.tree-pane { width: 280px; flex: none; background: var(--el-bg-color); border-radius: 8px; padding: 12px; overflow-y: auto; }
.tree-head { display: flex; align-items: center; justify-content: space-between; padding: 4px 6px 10px; font-weight: 600; border-bottom: 1px solid var(--el-border-color-lighter); margin-bottom: 6px; }
.head-actions { display: flex; gap: 4px; }
.tree-node { display: flex; align-items: center; justify-content: space-between; width: 100%; padding-right: 6px; }
.tree-node.active .node-label { color: var(--el-color-primary); font-weight: 600; }
.node-label { display: inline-flex; align-items: center; gap: 6px; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.node-label span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.node-icon { flex: none; color: var(--el-text-color-secondary); }
.hidden-icon { flex: none; color: var(--el-text-color-secondary); font-size: 13px; }
.node-ops { opacity: 0; transition: opacity .15s; }
.tree-node:hover .node-ops { opacity: 1; }
.more { cursor: pointer; color: var(--el-text-color-secondary); }
.editor-pane { flex: 1; min-width: 0; min-height: 0; }
.editor-pane :deep(.el-empty) { height: 100%; }
</style>
