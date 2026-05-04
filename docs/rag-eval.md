# RAG 性能评测说明

本项目提供了批量 RAG 评测工具：`cmd/rag-eval`。

## 评测内容

- 通过 `/api/v1/AI/chat/retrieve-debug` 统计检索成功率与错误情况
- 检索时延
  - 客户端请求时延
  - 服务端 `retrieval.durationMs`
- 检索质量
  - `Hit@K`
  - 平均 `Recall@K`
  - `MRR`
- 通过 `/api/v1/AI/chat/send-new-session`（`modelType=2`）统计回答时延
- 可选与 `modelType=1` 做基线时延对比

## 输入 CSV 格式

必填表头：

- `query`

可选表头：

- `kb_id`（或 `kbId`）
- `expected_docs`（或 `expectedDocs`、`expected`）
- `top_k`（或 `topK`）

`expected_docs` 支持分隔符：`;`、`,`、`|`

示例文件：`cmd/rag-eval/sample_cases.csv`

## 运行方式

```powershell
$env:RAG_EVAL_TOKEN="your_jwt_token"
go run ./cmd/rag-eval `
  -input ./cmd/rag-eval/sample_cases.csv `
  -base_url http://127.0.0.1:8080 `
  -kb_id default `
  -top_k 5 `
  -run_answer=true `
  -compare_baseline=true
```

## 说明

- 相关接口需要 JWT 鉴权。
- 当 `-run_answer=true` 时，工具会调用 `send-new-session`，并在数据库中写入会话/消息。
- 如果只做检索评测，请使用 `-run_answer=false`。
