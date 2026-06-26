<script setup>
import { ref, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import ReportEditor from '../components/ReportEditor.vue'

const props = defineProps({ id: { type: String, default: '' } })
const router = useRouter()
const route = useRoute()

const parentId = ref(0)

function loadParent() {
  if (!props.id && route.query.parent) parentId.value = Number(route.query.parent)
}

function onSaved(id, action) {
  // 仅发布后跳查看页; 保存草稿留在编辑器继续改 (新建后补上 id 以便后续走更新)
  if (action === 'publish') {
    router.push(`/reports/${id}`)
  } else if (!props.id && id) {
    router.replace(`/reports/${id}/edit`)
  }
}
function onCancel() {
  // 始终走前端路由跳到明确目标, 避免 router.back() 命中外部历史导致整页刷新:
  // 编辑已有报表 -> 其查看页; 新建 -> 报表列表。
  if (props.id) {
    router.push(`/reports/${props.id}`)
  } else {
    router.push('/reports')
  }
}

onMounted(loadParent)
</script>

<template>
  <ReportEditor
    :id="props.id" :parent-id="parentId"
    show-back @saved="onSaved" @back="onCancel" />
</template>
