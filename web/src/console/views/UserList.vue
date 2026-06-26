<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { api } from '@/api'

// 生成一个 16 位强随机密码 (大小写字母 + 数字 + 符号), 用 crypto 取随机。
function genPassword(len = 16) {
  const charset = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789!@#$%^&*'
  const arr = new Uint32Array(len)
  crypto.getRandomValues(arr)
  return Array.from(arr, n => charset[n % charset.length]).join('')
}

const list = ref([])
const roles = ref([])
const loading = ref(false)
const dialog = ref(false)
const form = reactive({ id: 0, username: '', password: '', nick: '', roleIds: [], is_admin: false, email: '' })

const roleName = (id) => roles.value.find(r => r.id === id)?.name || id

// 生成随机密码并填入表单, 顺带复制到剪贴板 (新建用户后好转交)。
async function fillRandomPassword() {
  const pwd = genPassword()
  form.password = pwd
  try {
    await navigator.clipboard.writeText(pwd)
    ElMessage.success('已生成并复制到剪贴板')
  } catch {
    ElMessage.success('已生成随机密码')
  }
}

async function load() {
  loading.value = true
  try {
    [list.value, roles.value] = await Promise.all([api.listUsers(), api.listRoles()])
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    loading.value = false
  }
}

function openNew() {
  Object.assign(form, { id: 0, username: '', password: '', nick: '', roleIds: [], is_admin: false, email: '' })
  dialog.value = true
}
function openEdit(row) {
  Object.assign(form, {
    id: row.id, username: row.username, password: '', nick: row.nick,
    roleIds: (row.roles || '').split(',').filter(Boolean).map(Number),
    is_admin: row.is_admin, email: row.email
  })
  dialog.value = true
}

async function save() {
  try {
    const body = {
      username: form.username, password: form.password, nick: form.nick,
      roles: form.roleIds.join(','), is_admin: form.is_admin, email: form.email
    }
    if (form.id) await api.updateUser(form.id, body)
    else await api.createUser(body)
    dialog.value = false
    ElMessage.success('已保存')
    load()
  } catch (e) {
    ElMessage.error(e.message)
  }
}

async function remove(row) {
  await ElMessageBox.confirm(`确认删除用户「${row.username}」?`, '提示', { type: 'warning' })
  await api.deleteUser(row.id)
  ElMessage.success('已删除')
  load()
}

onMounted(load)
</script>

<template>
  <div>
    <div class="toolbar">
      <h2>用户管理</h2>
      <el-button type="primary" @click="openNew">+ 新建用户</el-button>
    </div>
    <el-card shadow="never">
      <el-table :data="list" v-loading="loading">
        <el-table-column prop="id" label="ID" width="70" />
        <el-table-column prop="username" label="用户名" width="140" />
        <el-table-column prop="nick" label="昵称" width="140" />
        <el-table-column label="角色">
          <template #default="{ row }">
            <el-tag v-if="row.is_admin" type="danger" size="small">超级管理员</el-tag>
            <template v-else>
              <el-tag v-for="id in (row.roles || '').split(',').filter(Boolean)" :key="id"
                size="small" class="role-tag">{{ roleName(Number(id)) }}</el-tag>
              <span v-if="!row.roles" class="muted">无</span>
            </template>
          </template>
        </el-table-column>
        <el-table-column prop="email" label="邮箱" />
        <el-table-column label="操作" width="140">
          <template #default="{ row }">
            <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
            <el-button link type="danger" @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog v-model="dialog" :title="form.id ? '编辑用户' : '新建用户'" width="520px">
      <el-form label-width="80px">
        <el-form-item label="用户名">
          <el-input v-model="form.username" :disabled="!!form.id" />
        </el-form-item>
        <el-form-item label="密码">
          <el-input v-model="form.password" type="password" show-password
            :placeholder="form.id ? '留空则不修改' : '必填'" autocomplete="new-password">
            <template #append>
              <el-tooltip content="生成随机密码 (并复制)" placement="top">
                <el-button :icon="Refresh" @click="fillRandomPassword" />
              </el-tooltip>
            </template>
          </el-input>
        </el-form-item>
        <el-form-item label="昵称"><el-input v-model="form.nick" /></el-form-item>
        <el-form-item label="角色">
          <el-select v-model="form.roleIds" multiple style="width:100%" :disabled="form.is_admin"
            placeholder="选择角色">
            <el-option v-for="r in roles" :key="r.id" :label="r.name" :value="r.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="超级管理员">
          <el-switch v-model="form.is_admin" />
          <span class="muted" style="margin-left:8px">开启后拥有全部权限, 忽略角色</span>
        </el-form-item>
        <el-form-item label="邮箱"><el-input v-model="form.email" /></el-form-item>
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
.role-tag { margin-right: 4px; }
.muted { color: var(--el-text-color-secondary); font-size: 12px; }
</style>
