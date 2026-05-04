<template>
  <div class="chat-page">
    <aside class="sidebar">
      <div class="sidebar-header">
        <span>会话列表</span>
        <button class="new-chat-btn" @click="createNewSession">新建会话</button>
      </div>

      <ul class="session-list">
        <li
          v-for="session in sessions"
          :key="session.id"
          :class="['session-item', { active: currentSessionId === session.id }]"
          @click="switchSession(session.id)"
        >
          {{ session.name || `会话 ${session.id}` }}
        </li>
      </ul>
    </aside>

    <section class="chat-panel">
      <header class="toolbar">
        <button class="ghost-btn" @click="$router.push('/menu')">返回菜单</button>
        <button class="ghost-btn" @click="syncHistory" :disabled="!currentSessionId || tempSession">同步历史</button>

        <label class="inline-label" for="modelType">模型</label>
        <select id="modelType" v-model="selectedModel" class="select-input">
          <option value="1">OpenAI 对话</option>
          <option value="2">OpenAI RAG</option>
          <option value="3">OpenAI MCP</option>
        </select>
        <label class="inline-label" for="kbId">知识库</label>
        <input
          id="kbId"
          v-model.trim="selectedKBID"
          class="select-input"
          placeholder="default"
        />

        <label class="checkbox-label" for="streamingMode">
          <input id="streamingMode" type="checkbox" v-model="isStreaming" />
          流式响应
        </label>

        <button class="ghost-btn" @click="triggerFileUpload" :disabled="uploading">上传文档</button>
        <input
          ref="fileInput"
          type="file"
          accept=".md,.txt,text/markdown,text/plain"
          style="display: none"
          @change="handleFileUpload"
        />
      </header>

      <main class="messages" ref="messagesRef">
        <div
          v-for="(message, index) in currentMessages"
          :key="index"
          :class="['msg', isUserMessage(message) ? 'msg-user' : 'msg-ai']"
        >
          <div class="msg-head">
            <b>{{ isUserMessage(message) ? '你' : 'AI' }}</b>
            <button
              v-if="!isUserMessage(message)"
              class="tts-btn"
              @click="playTTS(message.content)"
            >
              朗读
            </button>
            <span
              v-if="message.meta && message.meta.status === 'streaming'"
              class="streaming"
            >
              生成中...
            </span>
          </div>
          <div class="msg-content" v-html="renderMarkdown(message.content)"></div>
        </div>
      </main>

      <footer class="composer">
        <textarea
          ref="messageInput"
          v-model="inputMessage"
          placeholder="请输入你的问题..."
          :disabled="loading"
          rows="1"
          @keydown.enter.exact.prevent="sendMessage"
        ></textarea>

        <button
          class="send-btn"
          type="button"
          :disabled="!inputMessage.trim() || loading"
          @click="sendMessage"
        >
          {{ loading ? '发送中...' : '发送' }}
        </button>
      </footer>
    </section>
  </div>
</template>

<script>
import { ref, nextTick, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import api from '../utils/api'

export default {
  name: 'AIChat',
  setup() {
    const sessions = ref({})
    const currentSessionId = ref(null)
    const tempSession = ref(false)
    const currentMessages = ref([])
    const inputMessage = ref('')
    const loading = ref(false)
    const messagesRef = ref(null)
    const messageInput = ref(null)
    const selectedModel = ref('1')
    const selectedKBID = ref('default')
    const isStreaming = ref(false)
    const uploading = ref(false)
    const fileInput = ref(null)

    const renderMarkdown = (text) => {
      if (!text && text !== '') return ''
      return String(text)
        .replace(/\*\*(.*?)\*\*/g, '<strong>$1</strong>')
        .replace(/\*(.*?)\*/g, '<em>$1</em>')
        .replace(/`(.*?)`/g, '<code>$1</code>')
        .replace(/\n/g, '<br>')
    }

    const isUserMessage = (message) => {
      if (!message) return false
      if (typeof message.is_user === 'boolean') return message.is_user
      if (typeof message.isUser === 'boolean') return message.isUser
      const role = String(message.role || '').toLowerCase()
      return role === 'user' || role === 'human'
    }

    const toRoleMessage = (item) => ({
      role: isUserMessage(item) ? 'user' : 'assistant',
      content: item?.content || ''
    })

    const scrollToBottom = () => {
      if (!messagesRef.value) return
      try {
        messagesRef.value.scrollTop = messagesRef.value.scrollHeight
      } catch (e) {
        // ignore
      }
    }

    const playTTS = async (text) => {
      try {
        const createResponse = await api.post('/AI/chat/tts', { text })
        if (!(createResponse.data && createResponse.data.status_code === 1000 && createResponse.data.task_id)) {
          ElMessage.error('无法创建语音合成任务')
          return
        }

        const taskId = createResponse.data.task_id
        await new Promise((resolve) => setTimeout(resolve, 5000))

        const maxAttempts = 30
        const pollInterval = 2000
        let attempts = 0

        const pollResult = async () => {
          const queryResponse = await api.get('/AI/chat/tts/query', { params: { task_id: taskId } })

          if (queryResponse.data && queryResponse.data.status_code === 1000) {
            const taskStatus = queryResponse.data.task_status
            if (taskStatus === 'Success' && queryResponse.data.task_result) {
              const audio = new Audio(queryResponse.data.task_result)
              audio.play()
              return true
            }
            if (taskStatus === 'Running' || taskStatus === 'Created') {
              attempts += 1
              if (attempts < maxAttempts) {
                await new Promise((resolve) => setTimeout(resolve, pollInterval))
                return pollResult()
              }
              ElMessage.error('语音合成超时')
              return true
            }

            ElMessage.error('语音合成失败')
            return true
          }

          attempts += 1
          if (attempts < maxAttempts) {
            await new Promise((resolve) => setTimeout(resolve, pollInterval))
            return pollResult()
          }

          ElMessage.error('语音合成超时')
          return true
        }

        await pollResult()
      } catch (error) {
        console.error('TTS error:', error)
        ElMessage.error('请求语音接口失败')
      }
    }

    const loadSessions = async () => {
      try {
        const response = await api.get('/AI/chat/sessions')
        if (response.data && response.data.status_code === 1000 && Array.isArray(response.data.sessions)) {
          const sessionMap = {}
          response.data.sessions.forEach((s) => {
            const sid = String(s.sessionId)
            sessionMap[sid] = {
              id: sid,
              name: s.name || `会话 ${sid}`,
              messages: []
            }
          })
          sessions.value = sessionMap
        }
      } catch (error) {
        console.error('Load sessions error:', error)
      }
    }

    const createNewSession = () => {
      currentSessionId.value = 'temp'
      tempSession.value = true
      currentMessages.value = []
      nextTick(() => {
        if (messageInput.value) messageInput.value.focus()
      })
    }

    const switchSession = async (sessionId) => {
      if (!sessionId) return
      currentSessionId.value = String(sessionId)
      tempSession.value = false

      if (!sessions.value[sessionId].messages || sessions.value[sessionId].messages.length === 0) {
        try {
          const response = await api.post('/AI/chat/history', { sessionId: currentSessionId.value })
          if (response.data && response.data.status_code === 1000 && Array.isArray(response.data.history)) {
            sessions.value[sessionId].messages = response.data.history.map(toRoleMessage)
          }
        } catch (err) {
          console.error('Load history error:', err)
        }
      }

      currentMessages.value = [...(sessions.value[sessionId].messages || [])]
      await nextTick()
      scrollToBottom()
    }

    const syncHistory = async () => {
      if (!currentSessionId.value || tempSession.value) {
        ElMessage.warning('请先选择已有会话再同步')
        return
      }

      try {
        const response = await api.post('/AI/chat/history', { sessionId: currentSessionId.value })
        if (response.data && response.data.status_code === 1000 && Array.isArray(response.data.history)) {
          const messages = response.data.history.map(toRoleMessage)
          sessions.value[currentSessionId.value].messages = messages
          currentMessages.value = [...messages]
          await nextTick()
          scrollToBottom()
        } else {
          ElMessage.error('无法获取历史数据')
        }
      } catch (err) {
        console.error('Sync history error:', err)
        ElMessage.error('请求历史数据失败')
      }
    }

    const sendMessage = async () => {
      if (!inputMessage.value || !inputMessage.value.trim()) {
        ElMessage.warning('请输入消息内容')
        return
      }

      if (!tempSession.value && (!currentSessionId.value || !sessions.value[currentSessionId.value])) {
        tempSession.value = true
      }

      const userMessage = {
        role: 'user',
        content: inputMessage.value
      }

      const currentInput = inputMessage.value
      inputMessage.value = ''

      currentMessages.value.push(userMessage)
      await nextTick()
      scrollToBottom()

      try {
        loading.value = true
        if (isStreaming.value) {
          await handleStreaming(currentInput)
        } else {
          await handleNormal(currentInput)
        }
      } catch (err) {
        console.error('Send message error:', err)
        ElMessage.error('发送失败，请重试')

        if (
          !tempSession.value &&
          currentSessionId.value &&
          sessions.value[currentSessionId.value] &&
          sessions.value[currentSessionId.value].messages
        ) {
          const sessionArr = sessions.value[currentSessionId.value].messages
          if (sessionArr && sessionArr.length) sessionArr.pop()
        }
        currentMessages.value.pop()
      } finally {
        if (!isStreaming.value) loading.value = false
        await nextTick()
        scrollToBottom()
      }
    }

    async function handleStreaming(question) {
      const aiMessage = {
        role: 'assistant',
        content: '',
        meta: { status: 'streaming' }
      }

      const aiMessageIndex = currentMessages.value.length
      currentMessages.value.push(aiMessage)

      const useNewSession = tempSession.value || !currentSessionId.value || !sessions.value[currentSessionId.value]
      if (useNewSession) tempSession.value = true

      if (!useNewSession && sessions.value[currentSessionId.value]) {
        if (!sessions.value[currentSessionId.value].messages) sessions.value[currentSessionId.value].messages = []
        sessions.value[currentSessionId.value].messages.push({ role: 'assistant', content: '' })
      }

      const url = useNewSession ? '/api/AI/chat/send-stream-new-session' : '/api/AI/chat/send-stream'
      const headers = {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${localStorage.getItem('token') || ''}`
      }
      const body = useNewSession
        ? { question, modelType: selectedModel.value, kbId: selectedKBID.value || 'default' }
        : { question, modelType: selectedModel.value, sessionId: currentSessionId.value, kbId: selectedKBID.value || 'default' }

      try {
        const response = await fetch(url, {
          method: 'POST',
          headers,
          body: JSON.stringify(body)
        })

        if (!response.ok) {
          loading.value = false
          throw new Error('Network response was not ok')
        }

        const reader = response.body.getReader()
        const decoder = new TextDecoder()
        let buffer = ''

        // eslint-disable-next-line no-constant-condition
        while (true) {
          const { done, value } = await reader.read()
          if (done) break

          const chunk = decoder.decode(value, { stream: true })
          buffer += chunk

          const lines = buffer.split('\n')
          buffer = lines.pop() || ''

          for (const line of lines) {
            const trimmedLine = line.trim()
            if (!trimmedLine || !trimmedLine.startsWith('data:')) continue

            const data = trimmedLine.slice(5).trim()
            if (data === '[DONE]') {
              loading.value = false
              currentMessages.value[aiMessageIndex].meta = { status: 'done' }
              currentMessages.value = [...currentMessages.value]
            } else if (data.startsWith('{')) {
              try {
                const parsed = JSON.parse(data)
                if (parsed.sessionId) {
                  const newSid = String(parsed.sessionId)
                  if (tempSession.value) {
                    sessions.value[newSid] = {
                      id: newSid,
                      name: '新会话',
                      messages: [...currentMessages.value]
                    }
                    currentSessionId.value = newSid
                    tempSession.value = false
                  }
                }
              } catch (e) {
                currentMessages.value[aiMessageIndex].content += data
              }
            } else {
              currentMessages.value[aiMessageIndex].content += data
            }

            currentMessages.value = [...currentMessages.value]
            await new Promise((resolve) => {
              requestAnimationFrame(() => {
                scrollToBottom()
                resolve()
              })
            })
          }
        }

        loading.value = false
        currentMessages.value[aiMessageIndex].meta = { status: 'done' }
        currentMessages.value = [...currentMessages.value]

        if (!tempSession.value && currentSessionId.value && sessions.value[currentSessionId.value]) {
          const sessMsgs = sessions.value[currentSessionId.value].messages
          if (Array.isArray(sessMsgs) && sessMsgs.length) {
            const lastIndex = sessMsgs.length - 1
            if (sessMsgs[lastIndex] && sessMsgs[lastIndex].role === 'assistant') {
              sessMsgs[lastIndex].content = currentMessages.value[aiMessageIndex].content
            }
          }
        }
      } catch (err) {
        console.error('Stream error:', err)
        loading.value = false
        currentMessages.value[aiMessageIndex].meta = { status: 'error' }
        currentMessages.value = [...currentMessages.value]
        ElMessage.error('流式传输出错')
      }
    }

    async function handleNormal(question) {
      const useNewSession = tempSession.value || !currentSessionId.value || !sessions.value[currentSessionId.value]

      if (useNewSession) {
        tempSession.value = true

        const response = await api.post('/AI/chat/send-new-session', {
          question,
          modelType: selectedModel.value,
          kbId: selectedKBID.value || 'default'
        })

        if (response.data && response.data.status_code === 1000) {
          const sessionId = String(response.data.sessionId)
          const aiMessage = {
            role: 'assistant',
            content: response.data.Information || ''
          }

          sessions.value[sessionId] = {
            id: sessionId,
            name: '新会话',
            messages: [{ role: 'user', content: question }, aiMessage]
          }
          currentSessionId.value = sessionId
          tempSession.value = false
          currentMessages.value = [...sessions.value[sessionId].messages]
        } else {
          ElMessage.error(response.data?.status_msg || '发送失败')
          currentMessages.value.pop()
        }
      } else {
        if (!Array.isArray(sessions.value[currentSessionId.value].messages)) {
          sessions.value[currentSessionId.value].messages = []
        }

        const sessionMsgs = sessions.value[currentSessionId.value].messages
        sessionMsgs.push({ role: 'user', content: question })

        const response = await api.post('/AI/chat/send', {
          question,
          modelType: selectedModel.value,
          sessionId: currentSessionId.value,
          kbId: selectedKBID.value || 'default'
        })

        if (response.data && response.data.status_code === 1000) {
          const aiMessage = { role: 'assistant', content: response.data.Information || '' }
          sessionMsgs.push(aiMessage)
          currentMessages.value = [...sessionMsgs]
        } else {
          ElMessage.error(response.data?.status_msg || '发送失败')
          sessionMsgs.pop()
          currentMessages.value.pop()
        }
      }
    }

    const triggerFileUpload = () => {
      if (fileInput.value) fileInput.value.click()
    }

    const handleFileUpload = async (event) => {
      const file = event.target.files[0]
      if (!file) return

      const fileName = file.name.toLowerCase()
      if (!fileName.endsWith('.md') && !fileName.endsWith('.txt')) {
        ElMessage.error('只允许上传 .md 或 .txt 文件')
        if (fileInput.value) fileInput.value.value = ''
        return
      }

      try {
        uploading.value = true
        const formData = new FormData()
        formData.append('file', file)
        formData.append('kbId', selectedKBID.value || 'default')

        const response = await api.post('/file/upload', formData, {
          headers: { 'Content-Type': 'multipart/form-data' }
        })

        if (response.data && response.data.status_code === 1000) {
          ElMessage.success('文件上传成功')
        } else {
          ElMessage.error(response.data?.status_msg || '上传失败')
        }
      } catch (error) {
        console.error('File upload error:', error)
        ElMessage.error('文件上传失败')
      } finally {
        uploading.value = false
        if (fileInput.value) fileInput.value.value = ''
      }
    }

    onMounted(() => {
      loadSessions()
    })

    return {
      sessions: computed(() => Object.values(sessions.value)),
      currentSessionId,
      tempSession,
      currentMessages,
      inputMessage,
      loading,
      messagesRef,
      messageInput,
      selectedModel,
      selectedKBID,
      isStreaming,
      uploading,
      fileInput,
      renderMarkdown,
      isUserMessage,
      playTTS,
      createNewSession,
      switchSession,
      syncHistory,
      sendMessage,
      triggerFileUpload,
      handleFileUpload
    }
  }
}
</script>

<style scoped>
.chat-page {
  height: 100vh;
  height: 100dvh;
  display: grid;
  grid-template-columns: 260px 1fr;
  background: #f6f8fb;
  overflow: hidden;
}

.sidebar {
  background: #fff;
  border-right: 1px solid #e5e7eb;
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
}

.sidebar-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px;
  border-bottom: 1px solid #e5e7eb;
  font-weight: 600;
  color: #0f172a;
  flex-shrink: 0;
}

.new-chat-btn {
  border: 1px solid #cbd5e1;
  background: #fff;
  color: #334155;
  border-radius: 8px;
  padding: 6px 10px;
  cursor: pointer;
}

.session-list {
  list-style: none;
  margin: 0;
  padding: 8px;
  overflow-y: auto;
  overscroll-behavior: contain;
  flex: 1;
  min-height: 0;
}

.session-item {
  padding: 10px 12px;
  border-radius: 8px;
  cursor: pointer;
  color: #334155;
  margin-bottom: 4px;
  border: 1px solid transparent;
}

.session-item:hover {
  background: #f8fafc;
  border-color: #e2e8f0;
}

.session-item.active {
  background: #eff6ff;
  color: #1d4ed8;
  border-color: #bfdbfe;
}

.chat-panel {
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
}

.toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  padding: 12px 14px;
  border-bottom: 1px solid #e5e7eb;
  background: #fff;
}

.ghost-btn {
  border: 1px solid #d1d5db;
  background: #fff;
  color: #334155;
  border-radius: 8px;
  padding: 7px 12px;
  cursor: pointer;
}

.ghost-btn:disabled {
  color: #94a3b8;
  cursor: not-allowed;
}

.inline-label {
  color: #475569;
}

.select-input {
  border: 1px solid #d1d5db;
  border-radius: 8px;
  padding: 7px 10px;
  background: #fff;
}

.checkbox-label {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: #475569;
}

.messages {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  overscroll-behavior: contain;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.msg {
  max-width: 72%;
  border-radius: 12px;
  padding: 10px 12px;
  line-height: 1.65;
}

.msg-user {
  align-self: flex-end;
  background: #2563eb;
  color: #fff;
}

.msg-ai {
  align-self: flex-start;
  background: #fff;
  color: #111827;
  border: 1px solid #e5e7eb;
}

.msg-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}

.tts-btn {
  border: none;
  background: #10b981;
  color: #fff;
  border-radius: 6px;
  padding: 2px 8px;
  cursor: pointer;
  font-size: 12px;
}

.streaming {
  color: #94a3b8;
  font-size: 12px;
}

.msg-content {
  white-space: pre-wrap;
  word-break: break-word;
}

.composer {
  border-top: 1px solid #e5e7eb;
  background: #fff;
  padding: 12px 14px;
  position: relative;
}

.composer textarea {
  width: 100%;
  resize: none;
  border: 1px solid #d1d5db;
  border-radius: 10px;
  padding: 10px 12px;
  min-height: 44px;
  max-height: 180px;
  outline: none;
  font-size: 14px;
  line-height: 1.6;
}

.composer textarea:focus {
  border-color: #93c5fd;
  box-shadow: 0 0 0 3px rgba(59, 130, 246, 0.12);
}

.send-btn {
  position: absolute;
  right: 24px;
  bottom: 20px;
  border: none;
  background: #2563eb;
  color: #fff;
  border-radius: 8px;
  padding: 9px 14px;
  cursor: pointer;
}

.send-btn:disabled {
  background: #cbd5e1;
  cursor: not-allowed;
}

@media (max-width: 960px) {
  .chat-page {
    grid-template-columns: 1fr;
  }

  .sidebar {
    border-right: none;
    border-bottom: 1px solid #e5e7eb;
    max-height: 220px;
    flex-shrink: 0;
  }

  .msg {
    max-width: 90%;
  }
}
</style>
