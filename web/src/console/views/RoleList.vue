<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '@/api'
import { buildTree } from '@/tree'
import PermTree from '../components/PermTree.vue'

const list = ref([])
const reports = ref([])
const dsnList = ref([])
const loading = ref(false)
const dialog = ref(false)

const reportTree = computed(() => buildTree(reports.value))

// 某数据源的权限资源串: default 源固定 dsn.default, 其余 dsn.<id>
function dsnRes(d) { return (d.name === 'default' || !d.id) ? 'dsn.default' : `dsn.${d.id}` }
// 显示用列表: 主库 default 不在 dsn 表 (listDsn 不含它), 这里补一条置顶供单独授权。
const dsnRows = computed(() => [
  { id: 0, name: 'default', driver: '主库' },
  ...dsnList.value
])
// 配了全局 dsn 只读/读写即覆盖所有源, 此时无需逐个勾
const dsnAllReadable = computed(() => (form.perms['dsn'] || '').includes('r'))
// 配了「全部报表」(Rr/Rrw) 即递归覆盖所有报表, 此时无需按文件夹/报表逐个授权
const reportAllGranted = computed(() => !!(form.perms['report'] || ''))

// form.perms: { 资源串 -> mode字符串 }
const form = reactive({ id: 0, name: '', parent_id: 0, remark: '', perms: {} })

async function load() {
  loading.value = true
  try {
    [list.value, reports.value, dsnList.value] = await Promise.all([
      api.listRoles(), api.listReports().catch(() => []), api.listDsn().catch(() => [])
    ])
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    loading.value = false
  }
}

function openNew() {
  Object.assign(form, { id: 0, name: '', parent_id: 0, remark: '', perms: {} })
  dialog.value = true
}
function openEdit(row) {
  let perms = {}
  try { perms = JSON.parse(row.resource || '{}') } catch { perms = {} }
  Object.assign(form, { id: row.id, name: row.name, parent_id: row.parent_id, remark: row.remark, perms })
  dialog.value = true
}

// 权限快捷设置: mode 为 ''(无) / 'r' / 'rw'; 资源支持 Rr 递归 (报表总开关)
function setPerm(res, mode) {
  if (!mode) delete form.perms[res]
  else form.perms[res] = mode
}
function permOf(res) { return form.perms[res] || '' }

async function save() {
  try {
    const body = {
      name: form.name, parent_id: Number(form.parent_id) || 0,
      resource: JSON.stringify(form.perms), remark: form.remark
    }
    if (form.id) await api.updateRole(form.id, body)
    else await api.createRole(body)
    dialog.value = false
    ElMessage.success('已保存')
    load()
  } catch (e) {
    ElMessage.error(e.message)
  }
}

async function remove(row) {
  await ElMessageBox.confirm(`确认删除角色「${row.name}」?`, '提示', { type: 'warning' })
  await api.deleteRole(row.id)
  ElMessage.success('已删除')
  load()
}

onMounted(load)
</script>

<template>
  <div>
    <div class="toolbar">
      <h2>角色管理</h2>
      <el-button type="primary" @click="openNew">+ 新建角色</el-button>
    </div>
    <el-card shadow="never">
      <el-table :data="list" v-loading="loading">
        <el-table-column prop="id" label="ID" width="70" />
        <el-table-column prop="name" label="角色名" width="160" />
        <el-table-column label="父角色" width="120">
          <template #default="{ row }">
            {{ list.find(r => r.id === row.parent_id)?.name || '-' }}
          </template>
        </el-table-column>
        <el-table-column label="权限摘要">
          <template #default="{ row }"><span class="mono">{{ row.resource }}</span></template>
        </el-table-column>
        <el-table-column prop="remark" label="备注" />
        <el-table-column label="操作" width="140">
          <template #default="{ row }">
            <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
            <el-button link type="danger" @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog v-model="dialog" :title="form.id ? '编辑角色' : '新建角色'" width="640px">
      <el-form label-width="90px">
        <el-form-item label="角色名"><el-input v-model="form.name" /></el-form-item>
        <el-form-item label="父角色">
          <el-select v-model="form.parent_id" style="width:100%" clearable placeholder="无 (继承父角色权限)">
            <el-option label="无" :value="0" />
            <el-option v-for="r in list.filter(r => r.id !== form.id)" :key="r.id" :label="r.name" :value="r.id" />
          </el-select>
        </el-form-item>

        <el-divider content-position="left">数据源权限</el-divider>
        <el-form-item label="全部数据源">
          <el-radio-group :model-value="permOf('dsn')" @update:model-value="v => setPerm('dsn', v)">
            <el-radio value="">不授予</el-radio>
            <el-radio value="r">全部只读</el-radio>
            <el-radio value="rw">全部读写</el-radio>
          </el-radio-group>
          <div class="hint">「全部只读」即可用所有数据源; 也可在下方按单个数据源精细控制。「读写」含管理数据源 (增删改连接)。</div>
        </el-form-item>
        <el-form-item label="按数据源">
          <div v-if="dsnAllReadable" class="muted">已授予「全部数据源」, 无需逐个设置。</div>
          <div v-else-if="dsnRows.length" class="report-grid">
            <div v-for="d in dsnRows" :key="d.id || d.name" class="report-row">
              <span class="rp-name">{{ d.name }} <small class="muted">({{ d.driver }})</small></span>
              <el-radio-group :model-value="permOf(dsnRes(d))" @update:model-value="v => setPerm(dsnRes(d), v)" size="small">
                <el-radio value="">无</el-radio>
                <el-radio value="r">只读</el-radio>
              </el-radio-group>
            </div>
          </div>
          <div v-else class="muted">暂无数据源</div>
          <div class="hint">只读 = 可用该数据源开发/运行报表 (探 schema、写 SQL); 数据源的增删改由「全部读写」控制。</div>
        </el-form-item>

        <el-divider content-position="left">报表权限</el-divider>
        <el-form-item label="管理报表">
          <el-radio-group :model-value="permOf('report.manage')" @update:model-value="v => setPerm('report.manage', v)">
            <el-radio value="">无</el-radio>
            <el-radio value="rw">可建/改/删</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="全部报表">
          <el-radio-group :model-value="permOf('report')" @update:model-value="v => setPerm('report', v)">
            <el-radio value="">不授予</el-radio>
            <el-radio value="Rr">全部可读</el-radio>
            <el-radio value="Rrw">全部可读写</el-radio>
          </el-radio-group>
          <div class="hint">递归授予所有报表; 也可在下方按单个报表精细控制。</div>
        </el-form-item>
        <el-form-item label="按文件夹/报表">
          <div v-if="reportAllGranted" class="muted">已授予「全部报表」, 无需逐个设置。</div>
          <div v-else class="report-grid">
            <PermTree v-if="reportTree.length" :nodes="reportTree" :perms="form.perms" />
            <div v-else class="muted">暂无报表</div>
          </div>
          <div class="hint">文件夹选「整夹可读」会递归覆盖其下所有子报表 (R 递归)。</div>
        </el-form-item>

        <el-form-item label="备注"><el-input v-model="form.remark" /></el-form-item>
        <el-form-item label="权限 JSON">
          <el-input :model-value="JSON.stringify(form.perms)" type="textarea" :rows="2" readonly class="mono" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialog = false">取消</el-button>
        <el-button type="primary" @click="save">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.toolbar { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; }
.toolbar h2 { margin: 0; }
.mono { font-family: Monaco, monospace; font-size: 12px; color: var(--el-text-color-regular); }
.hint { font-size: 12px; color: var(--el-text-color-secondary); margin-top: 4px; }
.report-grid { width: 100%; max-height: 220px; overflow-y: auto; border: 1px solid var(--el-border-color-lighter); border-radius: 6px; padding: 6px; }
.report-row { display: flex; align-items: center; justify-content: space-between; padding: 4px 8px; }
.rp-name { font-size: 13px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.muted { color: var(--el-text-color-secondary); font-size: 12px; padding: 8px; }
</style>
