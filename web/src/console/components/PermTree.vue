<script setup>
import { Document, Folder } from '@element-plus/icons-vue'

// 递归渲染报表树的权限选择。
//   文件夹 -> 资源串 report.{id}, 选项: 无 / Rr(整个文件夹可读) / Rrw(可读写)
//            (R 递归: 后端按祖先链让其覆盖所有子报表)
//   报表   -> 资源串 report.{id}, 选项: 无 / r / rw
// perms 为 { 资源串 -> mode } 对象 (双向: 通过 get/set 函数读写)。
defineProps({
  nodes: { type: Array, default: () => [] },
  perms: { type: Object, required: true },
  depth: { type: Number, default: 0 }
})

function res(id) { return 'report.' + id }
</script>

<template>
  <div>
    <div v-for="node in nodes" :key="node.id" class="perm-node">
      <div class="row" :style="{ paddingLeft: depth * 18 + 'px' }">
        <span class="name">
          <el-icon class="node-icon">
            <Folder v-if="node.type === 'folder'" />
            <Document v-else />
          </el-icon>
          <span>{{ node.name }}</span>
        </span>
        <el-radio-group v-if="node.type === 'folder'" size="small"
          :model-value="perms[res(node.id)] || ''"
          @update:model-value="v => v ? perms[res(node.id)] = v : delete perms[res(node.id)]">
          <el-radio-button value="">无</el-radio-button>
          <el-radio-button value="Rr">整夹可读</el-radio-button>
          <el-radio-button value="Rrw">可读写</el-radio-button>
        </el-radio-group>
        <el-radio-group v-else size="small"
          :model-value="perms[res(node.id)] || ''"
          @update:model-value="v => v ? perms[res(node.id)] = v : delete perms[res(node.id)]">
          <el-radio-button value="">无</el-radio-button>
          <el-radio-button value="r">读</el-radio-button>
          <el-radio-button value="rw">读写</el-radio-button>
        </el-radio-group>
      </div>
      <PermTree v-if="node.children && node.children.length"
        :nodes="node.children" :perms="perms" :depth="depth + 1" />
    </div>
  </div>
</template>

<style scoped>
.perm-node { width: 100%; }
.row { display: flex; align-items: center; justify-content: space-between; padding: 5px 8px; }
.name { display: inline-flex; align-items: center; gap: 6px; min-width: 0; font-size: 13px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.node-icon { flex: none; color: var(--el-text-color-secondary); }
</style>
