<template>
  <el-dialog v-model="visible" title="编辑 Agent" width="620px" destroy-on-close>
    <template v-if="resource">
      <p class="dialog-subtitle mono">{{ resource.stable_key }}</p>
      <el-form label-position="top">
        <el-form-item label="显示名称" required>
          <el-input v-model="form.displayName" maxlength="100" />
          <span class="field-help">用于列表和详情页展示，不影响稳定标识、部署身份和访问域名。</span>
        </el-form-item>
        <el-form-item label="Agent 域名标识" required :error="domainError">
          <el-input v-model="form.domainLabel" class="mono" maxlength="63" @input="normalizeDomainLabel" />
          <span class="field-help">小写字母、数字或连字符，首尾必须为字母或数字。</span>
        </el-form-item>
        <el-form-item label="生效后的域名命名空间">
          <el-input :model-value="newNamespace" class="mono" disabled />
        </el-form-item>
      </el-form>

      <template v-if="domainChanged">
        <el-alert
          class="domain-warning"
          title="旧域名将立即失效"
          description="新命名空间生效后不保留别名、兼容期或 fallback，现有客户端必须改用新域名。"
          type="warning"
          show-icon
          :closable="false"
        />
        <div class="impact-grid">
          <div><span>旧命名空间</span><strong class="mono">{{ oldNamespace }}</strong></div>
          <div><span>新命名空间</span><strong class="mono">{{ newNamespace }}</strong></div>
          <div><span>受影响资源域名</span><strong>{{ affectedDomainCount }} 个</strong></div>
          <div><span>活动会话</span><strong>{{ activeSessionCount }} 个</strong></div>
        </div>
        <el-form label-position="top">
          <el-form-item label="变更原因" required>
            <el-input v-model="form.reason" type="textarea" :rows="3" maxlength="500" show-word-limit placeholder="说明修改 Agent 域名的原因" />
          </el-form-item>
          <el-form-item label="再次输入新的域名标识" required>
            <el-input v-model="form.confirmDomainLabel" class="mono" autocomplete="off" @input="normalizeConfirmation" />
          </el-form-item>
        </el-form>
      </template>
    </template>

    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button type="primary" :loading="loading" :disabled="!formValid" @click="submit">保存</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import type { TechnicalResource } from '@/api/providerSupply'

const props = withDefaults(defineProps<{
  modelValue: boolean
  resource?: TechnicalResource
  affectedDomainCount?: number
  activeSessionCount?: number
  loading?: boolean
}>(), {
  resource: undefined,
  affectedDomainCount: 0,
  activeSessionCount: 0,
  loading: false,
})

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  submit: [value: { displayName: string; domainLabel: string; reason: string }]
}>()

const form = reactive({ displayName: '', domainLabel: '', reason: '', confirmDomainLabel: '' })
const visible = computed({ get: () => props.modelValue, set: value => emit('update:modelValue', value) })
const currentDomainLabel = computed(() => props.resource?.domain_label || '')
const providerNamespace = computed(() => {
  const namespace = props.resource?.domain_namespace || ''
  const prefix = `${currentDomainLabel.value}.`
  return namespace.startsWith(prefix) ? namespace.slice(prefix.length) : ''
})
const normalizedDomainLabel = computed(() => form.domainLabel.trim().toLowerCase())
const oldNamespace = computed(() => `*.${props.resource?.domain_namespace || currentDomainLabel.value}.beagle`)
const newNamespace = computed(() => `*.${normalizedDomainLabel.value || '...'}${providerNamespace.value ? `.${providerNamespace.value}` : ''}.beagle`)
const domainValid = computed(() => /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(normalizedDomainLabel.value))
const domainError = computed(() => form.domainLabel && !domainValid.value ? '域名标识格式不正确' : '')
const displayName = computed(() => form.displayName.trim())
const displayNameChanged = computed(() => displayName.value !== (props.resource?.display_name || props.resource?.domain_label || ''))
const domainChanged = computed(() => normalizedDomainLabel.value !== currentDomainLabel.value)
const riskConfirmed = computed(() => !domainChanged.value || (
  !!form.reason.trim() && form.confirmDomainLabel.trim().toLowerCase() === normalizedDomainLabel.value
))
const formValid = computed(() => !!displayName.value && domainValid.value && (displayNameChanged.value || domainChanged.value) && riskConfirmed.value && !props.loading)

watch(() => [props.modelValue, props.resource?.id], ([open]) => {
  if (!open || !props.resource) return
  form.displayName = props.resource.display_name || props.resource.domain_label
  form.domainLabel = props.resource.domain_label
  form.reason = ''
  form.confirmDomainLabel = ''
}, { immediate: true })

const normalizeDomainLabel = (value: string) => { form.domainLabel = value.trim().toLowerCase() }
const normalizeConfirmation = (value: string) => { form.confirmDomainLabel = value.trim().toLowerCase() }
const submit = () => {
  if (!formValid.value) return
  emit('submit', { displayName: displayName.value, domainLabel: normalizedDomainLabel.value, reason: form.reason.trim() })
}
</script>

<style scoped>
.dialog-subtitle { margin: -10px 0 16px; color: var(--text-secondary); font-size: 11px; }
.field-help { display: block; margin-top: 6px; color: var(--text-secondary); font-size: 11px; }
.domain-warning { margin-bottom: 14px; }
.impact-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); margin-bottom: 16px; overflow: hidden; border: 1px solid var(--border-light); border-radius: 6px; background: var(--border-light); gap: 1px; }
.impact-grid > div { min-width: 0; padding: 11px 13px; background: #f8fafc; }
.impact-grid span, .impact-grid strong { display: block; }
.impact-grid span { margin-bottom: 5px; color: var(--text-secondary); font-size: 10px; }
.impact-grid strong { overflow-wrap: anywhere; font-size: 12px; }
.mono { font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', monospace; }
</style>
