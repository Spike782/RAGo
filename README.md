# RAGo AI Chat（Gin + Eino + RAG + MCP）

一个基于 Go 的 AI Agent 对话平台后端，支持多轮对话、RAG 检索增强、MCP 工具调用、SSE 流式输出，并通过 Redis / RabbitMQ / Qdrant 提升并发与稳定性。

## 功能特性

- 多模型对话（统一 AIHelper 工厂）
  - `modelType=1`：基础 LLM 对话
  - `modelType=2`：RAG 对话
  - `modelType=3`：MCP 工具增强对话（ReAct）
- SSE 流式响应
  - 新建会话流式：`/api/v1/AI/chat/send-stream-new-session`
  - 已有会话流式：`/api/v1/AI/chat/send-stream`
- RAG 文档流程
  - 文件上传 -> 异步索引 -> 检索问答
  - 支持混合检索（向量 + 词法）和 Rerank
  - 向量存储支持 Redis / Qdrant（由配置切换）
- 异步任务与持久化
  - RabbitMQ 消费聊天消息与文件索引任务
  - MySQL 持久化用户/会话/消息
- Redis 缓存与状态
  - 验证码缓存、模型配置缓存、在线状态、会话历史缓存
  - 索引状态查询（`pending/indexing/ready/failed`）
- 限流与鉴权
  - JWT 鉴权
  - 按登录用户限流

## 技术栈

- Web 框架：Gin
- Agent / 模型编排：Eino
- ORM：GORM + MySQL
- 缓存：Redis（可用 Redis Stack）
- 消息队列：RabbitMQ
- 向量库：Qdrant（可选 Redis 向量）
- 前端：Vue3 + Element Plus（`vue-frontend`）

## 项目结构

```text
.
├─main.go                    # 后端入口
├─config/                    # TOML 配置
├─router/                    # 路由注册
├─controller/                # HTTP 控制器
├─service/                   # 业务逻辑
├─dao/                       # 数据访问层
├─model/                     # 数据模型
├─common/
│  ├─aihelper/               # 模型工厂、会话上下文、RAG/MCP模型
│  ├─rag/                    # 检索、分块、向量化、trace、rerank
│  ├─redis/                  # 缓存、在线状态、索引状态
│  ├─rabbitmq/               # 队列生产消费
│  └─mcp/                    # MCP server/client
├─cmd/
│  ├─rag-index/              # 手动索引工具
│  └─rag-eval/               # 评测工具
├─uploads/                   # 上传文档目录（按用户/知识库分层）
└─vue-frontend/              # 前端工程
```

## 运行前准备

建议本地先启动这些依赖：

- MySQL（默认 `127.0.0.1:3306`）
- Redis / Redis Stack（默认 `127.0.0.1:6380`）
- RabbitMQ（默认 `127.0.0.1:5672`）
- Qdrant（当 `ragModelConfig.vectorStore = "qdrant"` 时需要，默认 `127.0.0.1:6333`）

## 配置说明

配置文件：`config/config.toml`

重点配置分组：

- `mainConfig`：服务监听地址与端口
- `mysqlConfig`：MySQL 连接
- `redisConfig`：Redis 连接
- `rabbitmqConfig`：MQ 连接与重试
- `jwtConfig`：JWT 密钥与有效期
- `rateLimitConfig`：限流窗口与阈值
- `onlineConfig`：在线心跳 TTL
- `ragModelConfig`：
  - `embeddingModel` / `chatModelName`
  - `baseUrl`（聊天模型 OpenAI 兼容地址）
  - `embeddingBaseUrl`（可单独配置 embedding 端点）
  - `embeddingApiKey`（可单独配置 embedding key）
  - `vectorStore`：`redis` 或 `qdrant`
  - `topK` / `chunkSize` / `chunkOverlap`
  - `hybridEnabled` + 权重
  - `rerankEnabled` + 权重
  - `qdrantUrl` / `qdrantApiKey`

## 环境变量

项目中常用到的环境变量：

- `OPENAI_API_KEY`：LLM API Key（基础对话、RAG 对话、MCP 中部分工具会读取）
- `OPENAI_BASE_URL`：LLM OpenAI 兼容地址（可选）
- `OPENAI_MODEL_NAME`：默认模型名（可选）
- `OPENAI_EMBEDDING_API_KEY`：embedding key（可选，未设置时回退到 `OPENAI_API_KEY`）
- `QDRANT_API_KEY`：Qdrant Key（可选）
- `MCP_BASE_URL`：MCP 服务地址（可选）

示例（PowerShell）：

```powershell
$env:OPENAI_API_KEY="your_api_key"
$env:OPENAI_BASE_URL="https://api.openai-proxy.org/v1"
```

## 启动后端

```powershell
go mod tidy
go run .
```

服务默认启动在：`http://127.0.0.1:8080`

首次启动会自动执行 GORM AutoMigrate，创建/更新：

- `users`
- `sessions`
- `messages`

## 启动前端（可选）

```powershell
cd vue-frontend
npm install
npm run serve
```

## 主要接口

基路径：`/api/v1`

### 用户

- `POST /user/captcha`：发送验证码
- `POST /user/register`：注册
- `POST /user/login`：登录（返回 JWT）
- `GET /user/online-status`：在线状态（需 JWT）

### 对话（需 JWT）

- `GET /AI/chat/sessions`：会话列表
- `POST /AI/chat/send-new-session`：新建会话并提问
- `POST /AI/chat/send`：已有会话提问
- `POST /AI/chat/history`：会话历史
- `POST /AI/chat/retrieve-debug`：仅查看检索结果与 trace
- `POST /AI/chat/send-stream-new-session`：流式新会话
- `POST /AI/chat/send-stream`：流式已有会话

### 文件（需 JWT）

- `POST /file/upload`：上传知识库文件（form-data：`file`，可选 `kbId`）
- `GET /file/index-status?documentId=...`：查询索引状态

## RAG 使用流程（不含评测）

1. 登录获取 JWT。
2. 调用 `POST /api/v1/file/upload` 上传 `.md/.txt`（可传 `kbId`，默认 `default`）。
3. 轮询 `GET /api/v1/file/index-status`，直到 `status=ready`。
4. 聊天请求使用 `modelType="2"`，并传相同 `kbId`。
5. 如需排查检索，调用 `POST /api/v1/AI/chat/retrieve-debug` 查看 `retrieval` trace。

注意：

- 知识库 ID 在内部会规范化（例如连字符可能转为下划线），上传与提问请保持同一 `kbId`。
- 当 `vectorStore=qdrant` 且切换了 embedding 维度时，需要重建对应集合索引，避免维度不一致报错。

## 手动索引工具（可选）

如果你希望跳过上传接口，直接离线索引本地知识库文件，可使用：

```powershell
go run ./cmd/rag-index -user <user_email_or_id> -kb_id <kb_id> -pattern "*.md"
```

目录约定：`uploads/<user>/<kb>/`

## MCP 工具服务（可选）

启动 MCP Server：

```powershell
go run ./common/mcp -mode server -http-addr :8082
```

本地调用 MCP 工具（client 模式）：

```powershell
go run ./common/mcp -mode client -http-addr :8082 -tool web_search -query "warframe ash"
```

## 常见问题

- 登录总是“用户名或密码错误”
  - 检查是否先完成验证码注册、数据库是否连对、密码是否被前后端二次处理。
- RAG 回复提示未检索到文档
  - 检查 `kbId` 是否一致、索引状态是否 `ready`、向量库连接是否正常。
- Qdrant 报 `Vector dimension error`
  - embedding 维度与已建集合不一致，需要删除旧集合后重新索引。

## 安全建议

- 不要把真实密钥、数据库密码写入仓库。
- `config/config.toml` 建议仅保留模板值，在部署环境通过安全配置注入真实凭据。



