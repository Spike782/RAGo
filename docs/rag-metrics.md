# RAG 指标实践手册

## 1. 自动索引流水线

- 上传接口：`POST /api/v1/file/upload`
- 状态接口：`GET /api/v1/file/index-status?documentId=<doc_id>`
- 状态值：
  - `pending`：任务已接收，等待队列处理
  - `indexing`：工作进程正在构建向量索引
  - `ready`：向量索引可用于检索
  - `failed`：索引失败，查看 `message`

## 2. 核心检索指标

- `Hit@K`：Top-K 中是否至少命中 1 个期望文档
- `Recall@K`：Top-K 命中的期望文档占比
- `MRR`：第一个相关结果的倒数排名

使用示例：

```powershell
go run ./cmd/rag-eval `
  -token "$token" `
  -input ./cmd/rag-eval/sample_cases.csv `
  -base_url http://127.0.0.1:8080 `
  -run_answer=true `
  -compare_baseline=true
```

## 3. 建议 KPI 目标

- 检索质量：
  - `Hit@5 >= 0.80`
  - `MRR >= 0.70`
- 可靠性：
  - 索引任务成功率 `>= 99%`
  - 失败任务可通过 `index-status` 看到非空 `message`
- 时延：
  - 检索 p95 `< 4s`
  - 端到端回答 p95 `< 8s`

## 4. 演示检查清单

1. 上传 `.md/.txt` 文件。
2. 保存返回的 `documentId`。
3. 轮询 `GET /file/index-status`。
4. 等待 `status=ready`。
5. 使用 modelType `2`（RAG）提问。
6. 运行 `rag-eval`，展示 `Hit@K/Recall@K/MRR + 时延`。
