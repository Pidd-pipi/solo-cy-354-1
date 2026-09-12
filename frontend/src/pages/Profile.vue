<template>
  <div class="page">
    <h2>个人中心</h2>
    <template v-if="authStore.user">
      <el-card class="profile-card">
        <div class="profile-head">
          <el-avatar :size="64" :src="authStore.user.avatar || ''">{{ authStore.user.nickname.slice(0, 1) }}</el-avatar>
          <div class="profile-info">
            <h3>{{ authStore.user.nickname }}</h3>
            <p>{{ authStore.user.phone }} · {{ roleLabel(authStore.user.role) }} · {{ authStore.user.campus }}</p>
          </div>
          <div class="credit-box">
            <div class="credit-label">信誉分</div>
            <div class="credit-value">{{ authStore.user.credit_score }}</div>
            <div class="credit-level">{{ creditLevel(authStore.user.credit_score) }}</div>
          </div>
        </div>
      </el-card>
      <el-card class="section">
        <template #header>🚩 我的举报</template>
        <el-table :data="reports" empty-text="暂无举报记录">
          <el-table-column prop="id" label="ID" width="70" />
          <el-table-column prop="product_title" label="被举报商品" min-width="140" />
          <el-table-column label="原因" width="130">
            <template #default="{ row }">
              <el-tag size="small">{{ reportReasonLabel(row.reason) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="状态" width="160">
            <template #default="{ row }">
              <el-tag :type="reportStatusType(row.status) as any" size="small">{{ reportStatusLabel(row.status) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="处理结果" min-width="160">
            <template #default="{ row }">{{ row.handle_result || '—' }}</template>
          </el-table-column>
          <el-table-column label="提交时间" width="150">
            <template #default="{ row }">{{ formatDateTime(row.created_at) }}</template>
          </el-table-column>
          <el-table-column label="处理时间" width="150">
            <template #default="{ row }">{{ row.handled_at ? formatDateTime(row.handled_at) : '—' }}</template>
          </el-table-column>
        </el-table>
      </el-card>
      <el-card class="section">
        <template #header>⭐ 收到的评价</template>
        <el-table :data="reviews">
          <el-table-column prop="id" label="ID" width="80" />
          <el-table-column label="评价">
            <template #default="{ row }">
              <el-tag size="small">{{ ratingLabel(row.rating) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="content" label="内容" />
          <el-table-column label="时间">
            <template #default="{ row }">{{ formatDateTime(row.created_at) }}</template>
          </el-table-column>
        </el-table>
      </el-card>
    </template>
    <el-empty v-else description="请先登录">
      <el-button type="primary" @click="$router.push('/login')">去登录</el-button>
    </el-empty>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useAuthStore } from '../stores/authStore'
import { roleLabel } from '../constants/user'
import { ratingLabel } from '../constants/trade'
import { reportReasonLabel, reportStatusLabel, reportStatusType } from '../constants/report'
import { listMyReviews } from '../api/review'
import { listMyReports } from '../api/report'
import { formatDateTime } from '../utils/dateFormat'
import type { Review, ReportView } from '../types'

const authStore = useAuthStore()
const reviews = ref<Review[]>([])
const reports = ref<ReportView[]>([])

function creditLevel(score: number): string {
  if (score >= 200) return '极佳'
  if (score >= 150) return '优秀'
  if (score >= 100) return '良好'
  if (score >= 60) return '一般'
  return '待提升'
}

onMounted(async () => {
  if (!authStore.token) return
  const [reviewRes, reportRes] = await Promise.all([listMyReviews(), listMyReports()])
  reviews.value = reviewRes.data
  reports.value = reportRes.data
})
</script>

<style scoped>
.profile-card {
  margin-bottom: 16px;
}
.profile-head {
  display: flex;
  align-items: center;
  gap: 16px;
}
.profile-info h3 {
  margin: 0;
}
.profile-info p {
  margin: 4px 0 0;
  color: #909399;
  font-size: 13px;
}
.credit-box {
  margin-left: auto;
  text-align: center;
}
.credit-label {
  color: #909399;
  font-size: 12px;
}
.credit-value {
  font-size: 28px;
  font-weight: 700;
  color: #e6a23c;
}
.credit-level {
  font-size: 12px;
  color: #909399;
}
.section {
  margin-bottom: 16px;
}
</style>
