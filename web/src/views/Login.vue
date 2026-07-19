<template>
  <div class="login-container">
    <main class="login-panel">
      <div class="brand-lockup">
        <img src="@/assets/logo.png" alt="Beagle Signal" />
        <div><strong>Beagle Signal</strong><span>管理控制台</span></div>
      </div>
      <h1 class="login-title">{{ t('login.title') }}</h1>
      <el-form
        ref="formRef"
        :model="form"
        :rules="rules"
        label-position="top"
        @submit.prevent="handleLogin"
      >
        <el-form-item :label="t('login.username')" prop="username">
          <el-input
            v-model="form.username"
            :placeholder="t('login.usernamePlaceholder')"
            size="large"
          >
            <template #prefix>
              <el-icon><User /></el-icon>
            </template>
          </el-input>
        </el-form-item>
        <el-form-item :label="t('login.password')" prop="password">
          <el-input
            v-model="form.password"
            type="password"
            :placeholder="t('login.passwordPlaceholder')"
            size="large"
            @keyup.enter="handleLogin"
          >
            <template #prefix>
              <el-icon><Lock /></el-icon>
            </template>
          </el-input>
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            size="large"
            :loading="loading"
            style="width: 100%"
            @click="handleLogin"
          >
            {{ t('login.login') }}
          </el-button>
        </el-form-item>
        <el-form-item>
          <el-button
            size="large"
            style="width: 100%"
            @click="handleDownloadClient"
          >
            <el-icon style="margin-right: 8px"><Download /></el-icon>
            下载客户端
          </el-button>
        </el-form-item>
      </el-form>
    </main>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const { t } = useI18n()
const authStore = useAuthStore()

const formRef = ref<FormInstance>()
const loading = ref(false)

const form = reactive({
  username: '',
  password: ''
})

const rules: FormRules = {
  username: [{ required: true, message: t('login.usernameRequired'), trigger: 'blur' }],
  password: [{ required: true, message: t('login.passwordRequired'), trigger: 'blur' }]
}

const handleLogin = async () => {
  if (!formRef.value) return
  
  await formRef.value.validate(async (valid) => {
    if (valid) {
      loading.value = true
      try {
        const success = await authStore.login(form)
        if (success) {
          ElMessage.success(t('login.loginSuccess'))
          router.push('/')
        } else {
          ElMessage.error(t('login.loginFailed'))
        }
      } catch (error) {
        ElMessage.error(t('login.loginFailed'))
      } finally {
        loading.value = false
      }
    }
  })
}

const handleDownloadClient = () => {
  router.push('/download')
}


</script>

<style scoped>
.login-container {
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: var(--bg-page);
}

.login-panel {
  width: min(100%, 400px);
  padding: 30px 32px 24px;
  background: #fff;
  border: 1px solid var(--border-light);
  border-top: 3px solid var(--primary-color);
  border-radius: 6px;
  box-shadow: 0 14px 36px rgba(34, 45, 67, 0.08);
}

.brand-lockup {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 28px;
}

.brand-lockup img {
  width: 42px;
  height: 42px;
  object-fit: contain;
}

.brand-lockup strong,
.brand-lockup span {
  display: block;
}

.brand-lockup strong {
  color: var(--text-primary);
  font-size: 17px;
  line-height: 22px;
}

.brand-lockup span {
  margin-top: 2px;
  color: var(--text-secondary);
  font-size: 12px;
}

.login-title {
  margin: 0 0 22px;
  color: var(--text-primary);
  font-size: 20px;
  line-height: 28px;
}

.login-panel :deep(.el-form-item:last-child) {
  margin-bottom: 0;
}

@media (max-width: 480px) {
  .login-container { padding: 16px; align-items: flex-start; padding-top: 10vh; }
  .login-panel { padding: 26px 22px 20px; }
}
</style>
