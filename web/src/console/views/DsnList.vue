<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '@/api'
import { canWriteDsn } from '@/perm'

const canWrite = computed(() => canWriteDsn())
const list = ref([])
const loading = ref(false)
const dialog = ref(false)
const testing = ref(false)
function emptyForm() {
  return {
    id: 0, name: '', driver: 'mysql', dsn: '', remark: '',
    ssh_enabled: false, ssh_host: '', ssh_port: 22, ssh_user: '',
    ssh_auth: 'password', ssh_password: '', ssh_key: '', ssh_key_passphrase: ''
  }
}
const form = reactive(emptyForm())

// 不同驱动的连接串示例
const DSN_PLACEHOLDER = {
  mysql: 'user:pass@tcp(127.0.0.1:3306)/db?charset=utf8mb4&parseTime=true',
  postgres: 'postgres://user:pass@127.0.0.1:5432/db?sslmode=disable',
  sqlite: '/path/to/data.db  (或 file:data.db?cache=shared)'
}
const dsnPlaceholder = computed(() => DSN_PLACEHOLDER[form.driver] || '')
// SSH 隧道仅对 mysql 有效
const sshSupported = computed(() => form.driver === 'mysql')

// 构造提交给后端的完整 payload
// 敏感字段 (连接串密码段 / SSH 密码 / 私钥 / 口令) 从 listDsn 拿到的是 **** 脱敏占位;
// 未改动则原样回传, 后端按 id 用库中原值补回, 不会把凭据覆盖成 ****。
function buildPayload() {
  return {
    id: form.id, name: form.name, driver: form.driver, dsn: form.dsn, remark: form.remark,
    ssh_enabled: sshSupported.value && form.ssh_enabled,
    ssh_host: form.ssh_host, ssh_port: Number(form.ssh_port) || 22, ssh_user: form.ssh_user,
    ssh_auth: form.ssh_auth, ssh_password: form.ssh_password,
    ssh_key: form.ssh_key, ssh_key_passphrase: form.ssh_key_passphrase
  }
}

async function load() {
  loading.value = true
  try {
    list.value = await api.listDsn()
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    loading.value = false
  }
}

function mask(s) {
  return s ? s.replace(/:[^:@/]*@/, ':****@') : ''
}

function openNew() {
  Object.assign(form, emptyForm())
  dialog.value = true
}
function openEdit(row) {
  Object.assign(form, emptyForm(), row)
  dialog.value = true
}

async function save() {
  try {
    const body = buildPayload()
    if (form.id) await api.updateDsn(form.id, body)
    else await api.createDsn(body)
    dialog.value = false
    ElMessage.success('已保存')
    load()
  } catch (e) {
    ElMessage.error(e.message)
  }
}

async function test() {
  testing.value = true
  try {
    await api.testDsn(buildPayload())
    ElMessage.success('连接成功')
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    testing.value = false
  }
}

async function remove(row) {
  await ElMessageBox.confirm(`确认删除数据源「${row.name}」?`, '提示', { type: 'warning' })
  await api.deleteDsn(row.id)
  ElMessage.success('已删除')
  load()
}

onMounted(load)
</script>

<template>
  <div>
    <div class="toolbar">
      <h2>数据源</h2>
      <el-button v-if="canWrite" type="primary" @click="openNew">+ 新建数据源</el-button>
    </div>
    <el-card shadow="never">
      <el-table :data="list" v-loading="loading">
        <el-table-column prop="name" label="名称" width="160" />
        <el-table-column prop="driver" label="驱动" width="100" />
        <el-table-column label="连接串">
          <template #default="{ row }">
            <span class="mono">{{ mask(row.dsn) }}</span>
            <el-tag v-if="row.ssh_enabled" size="small" type="success" effect="plain" style="margin-left:6px">
              SSH {{ row.ssh_user }}@{{ row.ssh_host }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="remark" label="备注" />
        <el-table-column v-if="canWrite" label="操作" width="160">
          <template #default="{ row }">
            <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
            <el-button link type="danger" @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog v-model="dialog" :title="form.id ? '编辑数据源' : '新建数据源'" width="560px">
      <el-form label-width="80px">
        <el-form-item label="名称">
          <el-input v-model="form.name" placeholder="例如 sales_db" />
        </el-form-item>
        <el-form-item label="驱动">
          <el-select v-model="form.driver" style="width:100%">
            <el-option label="MySQL" value="mysql" />
            <el-option label="PostgreSQL" value="postgres" />
            <el-option label="SQLite" value="sqlite" />
          </el-select>
        </el-form-item>
        <el-form-item label="连接串">
          <el-input v-model="form.dsn" type="textarea" :rows="2" :placeholder="dsnPlaceholder" />
          <div v-if="sshSupported && form.ssh_enabled" class="hint">
            连接串中的 host 为「从跳板机视角」可达的数据库地址 (常用 127.0.0.1:3306)。
          </div>
        </el-form-item>

        <!-- SSH 隧道 (仅 mysql) -->
        <template v-if="sshSupported">
          <el-form-item label="SSH 隧道">
            <el-switch v-model="form.ssh_enabled" />
          </el-form-item>
          <template v-if="form.ssh_enabled">
            <el-form-item label="SSH 主机">
              <div class="row">
                <el-input v-model="form.ssh_host" placeholder="跳板机地址" style="flex:1" />
                <el-input v-model.number="form.ssh_port" placeholder="端口" style="width:100px" />
              </div>
            </el-form-item>
            <el-form-item label="SSH 用户">
              <el-input v-model="form.ssh_user" placeholder="例如 root" />
            </el-form-item>
            <el-form-item label="认证方式">
              <el-radio-group v-model="form.ssh_auth">
                <el-radio value="password">密码</el-radio>
                <el-radio value="key">私钥</el-radio>
              </el-radio-group>
            </el-form-item>
            <el-form-item v-if="form.ssh_auth === 'password'" label="SSH 密码">
              <el-input v-model="form.ssh_password" type="password" show-password autocomplete="new-password" />
            </el-form-item>
            <template v-else>
              <el-form-item label="私钥">
                <el-input v-model="form.ssh_key" type="textarea" :rows="4"
                  placeholder="-----BEGIN OPENSSH PRIVATE KEY-----&#10;...&#10;-----END OPENSSH PRIVATE KEY-----" />
              </el-form-item>
              <el-form-item label="私钥口令">
                <el-input v-model="form.ssh_key_passphrase" type="password" show-password
                  placeholder="无口令可留空" autocomplete="new-password" />
              </el-form-item>
            </template>
          </template>
        </template>

        <el-form-item label="备注">
          <el-input v-model="form.remark" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button :loading="testing" @click="test">测试连接</el-button>
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
.row { display: flex; gap: 8px; width: 100%; }
.hint { font-size: 12px; color: var(--el-color-warning); margin-top: 4px; line-height: 1.4; }
</style>
