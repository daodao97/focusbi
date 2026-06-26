<script setup>
import { Folder } from '@element-plus/icons-vue'

// 递归渲染报表树到侧边栏: 文件夹 -> el-sub-menu, 报表 -> el-menu-item。
defineProps({
  nodes: { type: Array, default: () => [] }
})
</script>

<template>
  <template v-for="node in nodes" :key="node.id">
    <!-- 文件夹: 可展开子菜单 -->
    <el-sub-menu v-if="node.type === 'folder'" :index="`folder-${node.id}`">
      <template #title>
        <span class="folder-title"><el-icon><Folder /></el-icon><span>{{ node.name }}</span></span>
      </template>
      <ReportTreeMenu :nodes="node.children" />
      <el-menu-item v-if="!node.children.length" :index="`empty-${node.id}`" disabled class="empty-folder">
        (空文件夹)
      </el-menu-item>
    </el-sub-menu>

    <!-- 报表: 叶子 -->
    <el-menu-item v-else :index="`/reports/${node.id}`" class="report-item">
      <span class="report-name">{{ node.name }}</span>
    </el-menu-item>
  </template>
</template>

<style scoped>
.report-item { font-size: 13px; }
.report-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.folder-title { display: inline-flex; align-items: center; gap: 6px; font-size: 13px; }
.empty-folder { font-size: 12px; color: var(--el-text-color-secondary) !important; }
</style>
