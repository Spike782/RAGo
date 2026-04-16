<template>
  <div class="auth-page">
    <el-card class="auth-card" shadow="never">
      <template #header>
        <div class="auth-title">注册</div>
      </template>

      <el-form
        ref="registerFormRef"
        :model="registerForm"
        :rules="registerRules"
        label-width="90px"
      >
        <el-form-item label="邮箱" prop="email">
          <el-input
            v-model="registerForm.email"
            type="email"
            placeholder="请输入邮箱"
            clearable
          />
        </el-form-item>

        <el-form-item label="验证码" prop="captcha">
          <el-row :gutter="10" style="width: 100%">
            <el-col :span="15">
              <el-input
                v-model="registerForm.captcha"
                placeholder="请输入验证码"
                clearable
              />
            </el-col>
            <el-col :span="9">
              <el-button
                type="primary"
                :loading="codeLoading"
                :disabled="countdown > 0"
                @click="sendCode"
                class="full-btn"
              >
                {{ countdown > 0 ? `${countdown}s` : '发送验证码' }}
              </el-button>
            </el-col>
          </el-row>
        </el-form-item>

        <el-form-item label="密码" prop="password">
          <el-input
            v-model="registerForm.password"
            type="password"
            show-password
            placeholder="请输入密码"
            clearable
          />
        </el-form-item>

        <el-form-item label="确认密码" prop="confirmPassword">
          <el-input
            v-model="registerForm.confirmPassword"
            type="password"
            show-password
            placeholder="请再次输入密码"
            clearable
          />
        </el-form-item>

        <el-form-item>
          <el-button
            type="primary"
            :loading="loading"
            @click="handleRegister"
            class="full-btn"
          >
            注册
          </el-button>
        </el-form-item>

        <el-form-item>
          <el-button
            link
            type="primary"
            @click="$router.push('/login')"
            class="full-btn"
          >
            已有账号？去登录
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>
  </div>
</template>

<script>
import { ref, reactive } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import api from '../utils/api'

export default {
  name: 'RegisterView',
  setup() {
    const router = useRouter()
    const registerFormRef = ref()
    const loading = ref(false)
    const codeLoading = ref(false)
    const countdown = ref(0)

    const registerForm = reactive({
      email: '',
      captcha: '',
      password: '',
      confirmPassword: ''
    })

    const validateConfirmPassword = (rule, value, callback) => {
      if (value !== registerForm.password) {
        callback(new Error('两次输入密码不一致'))
      } else {
        callback()
      }
    }

    const registerRules = {
      email: [
        { required: true, message: '请输入邮箱', trigger: 'blur' },
        { type: 'email', message: '请输入正确的邮箱格式', trigger: 'blur' }
      ],
      captcha: [{ required: true, message: '请输入验证码', trigger: 'blur' }],
      password: [
        { required: true, message: '请输入密码', trigger: 'blur' },
        { min: 6, message: '密码长度不能少于 6 位', trigger: 'blur' }
      ],
      confirmPassword: [
        { required: true, message: '请确认密码', trigger: 'blur' },
        { validator: validateConfirmPassword, trigger: 'blur' }
      ]
    }

    const sendCode = async () => {
      if (!registerForm.email) {
        ElMessage.warning('请先输入邮箱')
        return
      }

      try {
        codeLoading.value = true
        const response = await api.post('/user/captcha', { email: registerForm.email })

        if (response.data.status_code === 1000) {
          ElMessage.success('验证码发送成功')
          countdown.value = 60
          const timer = setInterval(() => {
            countdown.value -= 1
            if (countdown.value <= 0) clearInterval(timer)
          }, 1000)
        } else {
          ElMessage.error(response.data.status_msg || '验证码发送失败')
        }
      } catch (error) {
        console.error('Send code error:', error)
        ElMessage.error('验证码发送失败，请重试')
      } finally {
        codeLoading.value = false
      }
    }

    const handleRegister = async () => {
      try {
        await registerFormRef.value.validate()
        loading.value = true

        const response = await api.post('/user/register', {
          email: registerForm.email,
          captcha: registerForm.captcha,
          password: registerForm.password
        })

        if (response.data.status_code === 1000) {
          ElMessage.success('注册成功，请登录')
          router.push('/login')
        } else {
          ElMessage.error(response.data.status_msg || '注册失败')
        }
      } catch (error) {
        console.error('Register error:', error)
        ElMessage.error('注册失败，请重试')
      } finally {
        loading.value = false
      }
    }

    return {
      registerFormRef,
      loading,
      codeLoading,
      countdown,
      registerForm,
      registerRules,
      sendCode,
      handleRegister
    }
  }
}
</script>

<style scoped>
.auth-page {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: linear-gradient(160deg, #eef3ff 0%, #f8fafc 100%);
}

.auth-card {
  width: 100%;
  max-width: 460px;
  border-radius: 14px;
  border: 1px solid #e5e7eb;
}

.auth-title {
  text-align: center;
  font-size: 24px;
  font-weight: 600;
  color: #111827;
}

.full-btn {
  width: 100%;
}
</style>
