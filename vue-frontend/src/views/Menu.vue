<template>
  <div class="menu-page">
    <header class="menu-header">
      <h1>AI 应用平台</h1>
      <el-button type="danger" plain @click="handleLogout">退出登录</el-button>
    </header>

    <main class="menu-main">
      <div class="menu-grid">
        <el-card class="menu-item" shadow="never" @click="$router.push('/ai-chat')">
          <div class="card-content">
            <el-icon size="40" color="#2563eb"><ChatDotRound /></el-icon>
            <h3>AI 聊天</h3>
            <p>文本对话、RAG 与 MCP 工具调用</p>
          </div>
        </el-card>

        <el-card class="menu-item" shadow="never" @click="$router.push('/image-recognition')">
          <div class="card-content">
            <el-icon size="40" color="#16a34a"><Camera /></el-icon>
            <h3>图像识别</h3>
            <p>上传图片并查看识别结果</p>
          </div>
        </el-card>
      </div>
    </main>
  </div>
</template>

<script>
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ChatDotRound, Camera } from '@element-plus/icons-vue'

export default {
  name: 'MenuView',
  components: {
    ChatDotRound,
    Camera
  },
  setup() {
    const router = useRouter()

    const handleLogout = async () => {
      try {
        await ElMessageBox.confirm('确定要退出登录吗？', '提示', {
          confirmButtonText: '确定',
          cancelButtonText: '取消',
          type: 'warning'
        })
        localStorage.removeItem('token')
        ElMessage.success('已退出登录')
        router.push('/login')
      } catch {
        // User canceled.
      }
    }

    return {
      handleLogout
    }
  }
}
</script>

<style scoped>
.menu-page {
  min-height: 100vh;
  background: #f6f8fb;
  padding: 24px;
}

.menu-header {
  max-width: 980px;
  margin: 0 auto;
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 24px;
}

.menu-header h1 {
  font-size: 28px;
  color: #111827;
}

.menu-main {
  max-width: 980px;
  margin: 0 auto;
}

.menu-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  gap: 18px;
}

.menu-item {
  border-radius: 14px;
  border: 1px solid #e5e7eb;
  cursor: pointer;
  transition: all 0.2s ease;
}

.menu-item:hover {
  border-color: #cbd5e1;
  transform: translateY(-2px);
  box-shadow: 0 10px 24px rgba(15, 23, 42, 0.08);
}

.card-content {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 10px;
  padding: 8px;
}

.card-content h3 {
  font-size: 20px;
  color: #111827;
}

.card-content p {
  color: #64748b;
}
</style>
