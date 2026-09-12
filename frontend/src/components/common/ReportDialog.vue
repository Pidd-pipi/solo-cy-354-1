<template>
  <el-dialog v-model="visible" title="举报商品" width="460px" @closed="reset">
    <el-form label-position="top">
      <el-form-item label="举报商品">
        <el-text>{{ product?.title }}</el-text>
      </el-form-item>
      <el-form-item label="举报原因" required>
        <el-select v-model="reason" placeholder="请选择举报原因" style="width: 100%">
          <el-option v-for="r in REPORT_REASONS" :key="r.value" :label="r.label" :value="r.value" />
        </el-select>
      </el-form-item>
      <el-form-item label="补充说明">
        <el-input v-model="detail" type="textarea" :rows="4" maxlength="500" show-word-limit
          placeholder="请描述具体情况（选填，最多 500 字）" />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button type="danger" :loading="submitting" @click="submit">提交举报</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import { createReport } from '../../api/report'
import { REPORT_REASONS } from '../../constants/report'
import type { Product } from '../../types'

const props = defineProps<{ product: Product | null }>()
const emit = defineEmits<{ (e: 'submitted'): void }>()

const visible = ref(false)
const reason = ref('')
const detail = ref('')
const submitting = ref(false)

function open() {
  visible.value = true
}

function reset() {
  reason.value = ''
  detail.value = ''
  submitting.value = false
}

async function submit() {
  if (!props.product) return
  if (!reason.value) {
    ElMessage.warning('请选择举报原因')
    return
  }
  submitting.value = true
  try {
    await createReport({ product_id: props.product.id, reason: reason.value, detail: detail.value })
    ElMessage.success('举报已提交，请等待管理员处理')
    visible.value = false
    emit('submitted')
  } finally {
    submitting.value = false
  }
}

defineExpose({ open })
</script>
