<template>
  <div class="page-wrap">
    <aside class="left-panel">
      <div class="panel-title">图像识别</div>
      <div class="panel-subtitle">上传图片并查看 AI 返回结果</div>
    </aside>

    <section class="main-panel">
      <div class="top-bar">
        <button class="back-btn" @click="$router.push('/menu')">返回菜单</button>
        <h2>AI 图像识别</h2>
      </div>

      <div class="chat-messages" ref="chatContainerRef">
        <div
          v-for="(message, index) in messages"
          :key="index"
          :class="['message', message.role === 'user' ? 'user-message' : 'ai-message']"
        >
          <div class="message-header">
            <b>{{ message.role === 'user' ? '你' : 'AI' }}:</b>
          </div>
          <div class="message-content">
            <span>{{ message.content }}</span>
            <img v-if="message.imageUrl" :src="message.imageUrl" alt="上传图片" />
          </div>
        </div>
      </div>

      <div class="upload-bar">
        <form class="upload-form" @submit.prevent="handleSubmit">
          <input
            ref="fileInputRef"
            type="file"
            accept="image/*"
            required
            @change="handleFileSelect"
          />
          <button type="submit" :disabled="!selectedFile">发送图片</button>
        </form>
      </div>
    </section>
  </div>
</template>

<script>
import { ref, nextTick } from 'vue'
import api from '../utils/api'

export default {
  name: 'ImageRecognition',
  setup() {
    const messages = ref([])
    const selectedFile = ref(null)
    const fileInputRef = ref(null)
    const chatContainerRef = ref(null)

    const handleFileSelect = (event) => {
      selectedFile.value = event.target.files[0]
    }

    const scrollToBottom = () => {
      if (chatContainerRef.value) {
        chatContainerRef.value.scrollTop = chatContainerRef.value.scrollHeight
      }
    }

    const handleSubmit = async () => {
      if (!selectedFile.value) return

      const file = selectedFile.value
      const imageUrl = URL.createObjectURL(file)

      messages.value.push({
        role: 'user',
        content: `已上传图片：${file.name}`,
        imageUrl
      })

      await nextTick()
      scrollToBottom()

      const formData = new FormData()
      formData.append('image', file)

      try {
        const response = await api.post('/image/recognize', formData, {
          headers: { 'Content-Type': 'multipart/form-data' }
        })

        if (response.data && response.data.class_name) {
          messages.value.push({
            role: 'assistant',
            content: `识别结果：${response.data.class_name}`
          })
        } else {
          messages.value.push({
            role: 'assistant',
            content: `[错误] ${response.data.status_msg || '识别失败'}`
          })
        }
      } catch (error) {
        console.error('Upload error:', error)
        messages.value.push({
          role: 'assistant',
          content: `[错误] 上传失败：${error.message}`
        })
      } finally {
        URL.revokeObjectURL(imageUrl)
        await nextTick()
        scrollToBottom()

        selectedFile.value = null
        if (fileInputRef.value) fileInputRef.value.value = ''
      }
    }

    return {
      messages,
      selectedFile,
      fileInputRef,
      chatContainerRef,
      handleFileSelect,
      handleSubmit
    }
  }
}
</script>

<style scoped>
.page-wrap {
  min-height: 100vh;
  display: grid;
  grid-template-columns: 240px 1fr;
  background: #f6f8fb;
}

.left-panel {
  border-right: 1px solid #e5e7eb;
  background: #fff;
  padding: 24px;
}

.panel-title {
  font-size: 20px;
  font-weight: 600;
  color: #111827;
}

.panel-subtitle {
  margin-top: 8px;
  color: #64748b;
  line-height: 1.6;
}

.main-panel {
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.top-bar {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 14px 18px;
  border-bottom: 1px solid #e5e7eb;
  background: #fff;
}

.top-bar h2 {
  font-size: 18px;
  color: #111827;
}

.back-btn {
  border: 1px solid #d1d5db;
  background: #fff;
  color: #334155;
  border-radius: 8px;
  padding: 7px 12px;
  cursor: pointer;
}

.chat-messages {
  flex: 1;
  overflow-y: auto;
  padding: 18px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.message {
  max-width: 70%;
  border-radius: 12px;
  padding: 10px 12px;
  line-height: 1.6;
}

.user-message {
  align-self: flex-end;
  background: #2563eb;
  color: #fff;
}

.ai-message {
  align-self: flex-start;
  background: #fff;
  color: #111827;
  border: 1px solid #e5e7eb;
}

.message-header {
  margin-bottom: 4px;
}

.message-content {
  white-space: pre-wrap;
  word-break: break-word;
}

.message-content img {
  max-width: 260px;
  margin-top: 8px;
  border-radius: 10px;
  border: 1px solid #e5e7eb;
}

.upload-bar {
  border-top: 1px solid #e5e7eb;
  padding: 14px 18px;
  background: #fff;
}

.upload-form {
  display: flex;
  gap: 10px;
}

.upload-form input[type='file'] {
  flex: 1;
}

.upload-form button {
  border: none;
  background: #2563eb;
  color: #fff;
  border-radius: 8px;
  padding: 10px 14px;
  cursor: pointer;
}

.upload-form button:disabled {
  background: #cbd5e1;
  cursor: not-allowed;
}

@media (max-width: 900px) {
  .page-wrap {
    grid-template-columns: 1fr;
  }

  .left-panel {
    border-right: none;
    border-bottom: 1px solid #e5e7eb;
  }
}
</style>
