<template>
  <div class="page">
    <h2>商品广场</h2>
    <el-form inline class="filters">
      <el-form-item label="分类">
        <el-select v-model="query.category" clearable placeholder="全部分类" style="width: 160px">
          <el-option v-for="c in PRODUCT_CATEGORIES" :key="c.value" :label="c.label" :value="c.value" />
        </el-select>
      </el-form-item>
      <el-form-item label="校区">
        <el-input v-model="query.campus" placeholder="输入校区" style="width: 140px" clearable />
      </el-form-item>
      <el-form-item label="关键词">
        <el-input v-model="query.keyword" placeholder="搜索标题/描述" style="width: 180px" clearable />
      </el-form-item>
      <el-form-item>
        <el-button type="primary" @click="load">搜索</el-button>
      </el-form-item>
    </el-form>
    <el-row :gutter="16">
      <el-col v-for="p in products" :key="p.id" :span="6" class="col">
        <ProductCard :product="p" @detail="showDetail" @buy="buy" @chat="chat" />
      </el-col>
    </el-row>
    <el-empty v-if="!loading && products.length === 0" description="暂无商品" />
    <el-dialog v-model="detailVisible" :title="current?.title" width="520px">
      <el-descriptions :column="2" border v-if="current">
        <el-descriptions-item label="分类">{{ categoryLabel(current.category) }}</el-descriptions-item>
        <el-descriptions-item label="成色">{{ current.condition }}</el-descriptions-item>
        <el-descriptions-item label="校区">{{ current.campus }}</el-descriptions-item>
        <el-descriptions-item label="交易地点">{{ current.trade_location }}</el-descriptions-item>
        <el-descriptions-item label="价格">¥{{ current.price.toFixed(2) }}</el-descriptions-item>
        <el-descriptions-item label="状态">{{ productStatusLabel(current.status) }}</el-descriptions-item>
        <el-descriptions-item label="描述" :span="2">{{ current.description }}</el-descriptions-item>
      </el-descriptions>
      <template #footer>
        <el-tag v-if="current && reportedIds.has(current.id)" type="warning" effect="plain">已举报，等待处理</el-tag>
        <el-button
          v-if="current && authStore.token && authStore.user?.id !== current.seller_id && current.status !== 'removed'"
          type="danger" plain :disabled="reportedIds.has(current.id)" @click="openReport"
        >举报</el-button>
      </template>
    </el-dialog>
    <ReportDialog ref="reportDialogRef" :product="current" @submitted="onReported" />
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import ProductCard from '../components/common/ProductCard.vue'
import ReportDialog from '../components/common/ReportDialog.vue'
import { PRODUCT_CATEGORIES, categoryLabel, productStatusLabel } from '../constants/product'
import { useProducts } from '../hooks/useProducts'
import { createTradeOrder } from '../api/tradeOrder'
import { createConversation } from '../api/conversation'
import { listMyReports } from '../api/report'
import type { Product } from '../types'
import { useAuthStore } from '../stores/authStore'
import { useRouter } from 'vue-router'

const { products, loading, load } = useProducts()
const query = reactive<{ category?: string; campus?: string; keyword?: string }>({})
const detailVisible = ref(false)
const current = ref<Product | null>(null)
const authStore = useAuthStore()
const router = useRouter()
const reportDialogRef = ref<InstanceType<typeof ReportDialog> | null>(null)
// 我提交的、仍处于待处理状态的举报（同一商品仅保留一条待处理举报）
const reportedIds = ref<Set<number>>(new Set())

function showDetail(p: Product) {
  current.value = p
  detailVisible.value = true
}

function openReport() {
  reportDialogRef.value?.open()
}

function onReported() {
  if (current.value) {
    reportedIds.value.add(current.value.id)
  }
}

async function loadMyPendingReports() {
  if (!authStore.token) return
  try {
    const res = await listMyReports()
    reportedIds.value = new Set(res.data.filter((r) => r.status === 'pending').map((r) => r.product_id))
  } catch {
    // 未登录或加载失败时不影响商品浏览
  }
}

async function buy(p: Product) {
  if (!authStore.token) {
    ElMessage.warning('请先登录')
    router.push('/login')
    return
  }
  await createTradeOrder(p.id)
  ElMessage.success('已下单，等待卖家确认')
}

async function chat(p: Product) {
  if (!authStore.token) {
    ElMessage.warning('请先登录')
    router.push('/login')
    return
  }
  await createConversation(p.id)
  ElMessage.success('已发起私信')
  router.push('/messages')
}

onMounted(() => {
  load()
  loadMyPendingReports()
})
</script>

<style scoped>
.filters {
  margin-bottom: 8px;
}
.col {
  margin-bottom: 16px;
}
</style>
