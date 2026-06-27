<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '@/api'
import { copyText } from '@/clipboard'
import DocDrawer from '@/components/DocDrawer.vue'

const list = ref([])
const loading = ref(false)
const docOpen = ref(false)   // MCP 设置文档抽屉

// 创建弹窗
const dialog = ref(false)
const form = ref({ name: '', expire_days: 0 })
const creating = ref(false)

// 新建后一次性展示明文
const plainDialog = ref(false)
const plainToken = ref('')

async function load() {
  loading.value = true
  try {
    list.value = await api.listApiTokens()
  } catch (e) {
    ElMessage.error(e.message || '加载失败')
  } finally {
    loading.value = false
  }
}

function openCreate() {
  form.value = { name: '', expire_days: 0 }
  dialog.value = true
}

async function submit() {
  if (!form.value.name.trim()) {
    ElMessage.warning('请填写令牌名称')
    return
  }
  creating.value = true
  try {
    const res = await api.createApiToken(form.value.name.trim(), Number(form.value.expire_days) || 0)
    dialog.value = false
    plainToken.value = res.token
    plainDialog.value = true   // 明文仅此一次, 弹窗提示复制
    await load()
  } catch (e) {
    ElMessage.error(e.message || '创建失败')
  } finally {
    creating.value = false
  }
}

async function copyPlain() {
  if (await copyText(plainToken.value)) {
    ElMessage.success('已复制到剪贴板')
  } else {
    ElMessage.warning('复制失败, 请手动选择复制')
  }
}

async function remove(row) {
  try {
    await ElMessageBox.confirm(`确认删除令牌「${row.name || row.token_prefix}」? 使用它的客户端将立即失效。`, '删除确认', { type: 'warning' })
  } catch { return }
  try {
    await api.deleteApiToken(row.id)
    ElMessage.success('已删除')
    await load()
  } catch (e) {
    ElMessage.error(e.message || '删除失败')
  }
}

function fmt(t) { return t ? new Date(t).toLocaleString() : '—' }

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="head">
      <div>
        <h2>MCP 令牌</h2>
        <p class="hint">用于 MCP 等程序化访问 (如在 Codex / Claude Code 中开发报表)。令牌继承你本人的权限。</p>
      </div>
      <div class="actions">
        <el-button @click="docOpen = true">MCP 设置文档</el-button>
        <el-button type="primary" @click="openCreate">生成令牌</el-button>
      </div>
    </div>

    <el-table :data="list" v-loading="loading" stripe>
      <el-table-column prop="name" label="名称" min-width="160" />
      <el-table-column prop="token_prefix" label="前缀" width="160" />
      <el-table-column label="创建时间" width="180">
        <template #default="{ row }">{{ fmt(row.created_at) }}</template>
      </el-table-column>
      <el-table-column label="上次使用" width="180">
        <template #default="{ row }">{{ fmt(row.last_used_at) }}</template>
      </el-table-column>
      <el-table-column label="过期" width="180">
        <template #default="{ row }">{{ row.expires_at ? fmt(row.expires_at) : '永不' }}</template>
      </el-table-column>
      <el-table-column label="操作" width="100">
        <template #default="{ row }">
          <el-button link type="danger" @click="remove(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <!-- 创建弹窗 -->
    <el-dialog v-model="dialog" title="生成 MCP 令牌" width="460px">
      <el-form label-width="90px">
        <el-form-item label="名称">
          <el-input v-model="form.name" placeholder="如: 我的 Claude Code" />
        </el-form-item>
        <el-form-item label="有效期">
          <el-input v-model.number="form.expire_days" type="number" :min="0" style="width:160px">
            <template #append>天</template>
          </el-input>
          <div class="hint">0 表示永不过期。</div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialog = false">取消</el-button>
        <el-button type="primary" :loading="creating" @click="submit">生成</el-button>
      </template>
    </el-dialog>

    <!-- 明文一次性展示 -->
    <el-dialog v-model="plainDialog" title="令牌已生成" width="560px" :close-on-click-modal="false">
      <el-alert type="warning" :closable="false" show-icon
        title="请立即复制保存。出于安全, 此明文只显示这一次, 关闭后无法再次查看。" style="margin-bottom:12px" />
      <el-input :model-value="plainToken" readonly type="textarea" :rows="2" />
      <template #footer>
        <el-button @click="copyPlain">复制</el-button>
        <el-button type="primary" @click="plainDialog = false">我已保存</el-button>
      </template>
    </el-dialog>

    <!-- MCP 设置文档 (Codex / Claude Code 等接入说明) -->
    <DocDrawer v-model="docOpen" src="MCP.md" title="在 AI 工具中开发报表 · MCP 设置" />
  </div>
</template>

<style scoped>
.page { padding: 4px; }
.head { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 16px; }
.head h2 { margin: 0 0 4px; }
.hint { color: var(--el-text-color-secondary); font-size: 12px; margin: 0; }
.actions { display: flex; gap: 8px; flex: none; }
</style>
