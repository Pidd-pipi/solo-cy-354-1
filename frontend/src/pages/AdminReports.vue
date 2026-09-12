<template>
  <div class="page">
    <h2>举报处理</h2>
    <el-radio-group v-model="statusFilter" class="filter" @change="load">
      <el-radio-button label="">全部</el-radio-button>
      <el-radio-button label="pending">待处理</el-radio-button>
      <el-radio-button label="removed">已下架</el-radio-button>
      <el-radio-button label="rejected">已驳回</el-radio-button>
    </el-radio-group>
    <el-table :data="reports" v-loading="loading" empty-text="暂无举报记录">
      <el-table-column prop="id" label="ID" width="70" />
      <el-table-column label="商品" min-width="140">
        <template #default="{ row }">
          <el-link type="primary" @click="showProduct(row.product_id)">{{ row.product_title || `商品#${row.product_id}` }}</el-link>
          <el-tag size="small" class="ml8" :type="productStatusType(row.product_status) as any">{{ productStatusLabel(row.product_status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="举报原因" width="140">
        <template #default="{ row }">
          <el-tag size="small">{{ reportReasonLabel(row.reason) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="detail" label="补充说明" min-width="160" show-overflow-tooltip />
      <el-table-column prop="reporter_name" label="举报者" width="110" />
      <el-table-column label="状态" width="100">
        <template #default="{ row }">
          <el-tag :type="reportStatusType(row.status) as any" size="small">{{ reportStatusLabel(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="处理人/时间" width="170">
        <template #default="{ row }">
          <div v-if="row.handler_id">{{ row.handler_name || `管理员#${row.handler_id}` }}</div>
          <div v-if="row.handled_at" class="time">{{ formatDateTime(row.handled_at) }}</div>
          <span v-else>—</span>
        </template>
      </el-table-column>
      <el-table-column prop="handle_result" label="处理结果" min-width="150" show-overflow-tooltip />
      <el-table-column label="操作" width="170" fixed="right">
        <template #default="{ row }">
          <template v-if="row.status === 'pending'">
            <el-button size="small" type="danger" @click="handle(row, 'remove')">下架商品</el-button>
            <el-button size="small" @click="handle(row, 'reject')">驳回</el-button>
          </template>
          <span v-else class="done">已处理</span>
        </template>
      </el-table-column>
    </el-table>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useRouter } from 'vue-router'
import { listReports, handleReport } from '../api/report'
import { reportReasonLabel, reportStatusLabel, reportStatusType } from '../constants/report'
import { productStatusLabel, productStatusType } from '../constants/product'
import { formatDateTime } from '../utils/dateFormat'
import type { ReportView } from '../types'

const router = useRouter()
const reports = ref<ReportView[]>([])
const statusFilter = ref('pending')
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const res = await listReports(statusFilter.value)
    reports.value = res.data
  } finally {
    loading.value = false
  }
}

async function handle(row: ReportView, action: 'remove' | 'reject') {
  const title = action === 'remove'
    ? `确认下架「${row.product_title}」？下架后该商品不再接受下单或私信。`
    : '确认驳回该举报？'
  let note = ''
  try {
    const { value } = await ElMessageBox.prompt(title, action === 'remove' ? '下架商品（举报成立）' : '驳回举报', {
      confirmButtonText: '确认',
      cancelButtonText: '取消',
      inputType: 'textarea',
      inputPlaceholder: '处理备注（选填，最多 200 字）',
      inputValidator: (v: string) => (v || '').length <= 200 || '备注最多 200 字',
    })
    note = value || ''
  } catch {
    return
  }
  await handleReport(row.id, action, note)
  ElMessage.success(action === 'remove' ? '商品已下架，举报处理完成' : '举报已驳回')
  load()
}

function showProduct(id: number) {
  router.push({ path: '/products', query: { highlight: id } })
}

onMounted(load)
</script>

<style scoped>
.filter {
  margin-bottom: 16px;
}
.ml8 {
  margin-left: 8px;
}
.time {
  color: #909399;
  font-size: 12px;
}
.done {
  color: #909399;
  font-size: 12px;
}
</style>
