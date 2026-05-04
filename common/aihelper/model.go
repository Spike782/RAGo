package aihelper

import (
	"ai-chat/common/rag"
	"ai-chat/config"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

type StreamCallback func(msg string)

type AIModel interface {
	GenerateResponse(ctx context.Context, messages []*schema.Message) (*schema.Message, error)
	StreamResponse(ctx context.Context, messages []*schema.Message, cb StreamCallback) (string, error)
	GetModelType() string
}

type RetrievalTraceProvider interface {
	GetLastRetrievalTrace() *rag.RetrievalTrace
}

type OpenAIModel struct {
	llm model.ToolCallingChatModel
}

func NewOpenAIModel(ctx context.Context) (*OpenAIModel, error) {
	key := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	conf := config.GetConfig()
	modelName, baseURL := resolveModelConfig("1", conf.RagModelConfig.RagChatModelName, conf.RagModelConfig.RagBaseUrl)

	if key == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is empty")
	}
	if modelName == "" {
		return nil, fmt.Errorf("OPENAI model name is empty")
	}
	if baseURL == "" {
		return nil, fmt.Errorf("OPENAI base URL is empty")
	}

	llm, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		BaseURL: baseURL,
		Model:   modelName,
		APIKey:  key,
	})
	if err != nil {
		return nil, fmt.Errorf("create openai model failed: %w", err)
	}
	return &OpenAIModel{llm: llm}, nil
}

func (o *OpenAIModel) StreamResponse(ctx context.Context, messages []*schema.Message, cb StreamCallback) (string, error) {
	stream, err := o.llm.Stream(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("openai stream failed: %w", err)
	}
	defer stream.Close()

	var fullResp strings.Builder
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("openai stream recv failed: %w", err)
		}
		if len(msg.Content) == 0 {
			continue
		}
		fullResp.WriteString(msg.Content)
		if cb != nil {
			cb(msg.Content)
		}
	}
	return fullResp.String(), nil
}

func (o *OpenAIModel) GenerateResponse(ctx context.Context, messages []*schema.Message) (*schema.Message, error) {
	resp, err := o.llm.Generate(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("openai generate failed: %w", err)
	}
	return resp, nil
}

func (o *OpenAIModel) GetModelType() string { return "1" }

type AliRAGModel struct {
	llm       model.ToolCallingChatModel
	email     string
	kbID      string
	traceMu   sync.RWMutex
	lastTrace *rag.RetrievalTrace
}

func NewAliRAGModel(ctx context.Context, email, kbID string) (*AliRAGModel, error) {
	key := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	conf := config.GetConfig()
	modelName, baseURL := resolveModelConfig("2", conf.RagModelConfig.RagChatModelName, conf.RagModelConfig.RagBaseUrl)

	llm, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		BaseURL: baseURL,
		Model:   modelName,
		APIKey:  key,
	})
	if err != nil {
		return nil, fmt.Errorf("create rag model failed: %w", err)
	}
	return &AliRAGModel{
		llm:   llm,
		email: email,
		kbID:  rag.NormalizeKnowledgeBaseID(kbID),
	}, nil
}

func (o *AliRAGModel) GenerateResponse(ctx context.Context, messages []*schema.Message) (*schema.Message, error) {
	if len(messages) == 0 {
		return nil, fmt.Errorf("no messages provided")
	}
	query := messages[len(messages)-1].Content
	start := time.Now()

	ragQuery, err := rag.NewRAGQuery(ctx, o.email, o.kbID)
	if err != nil {
		o.setLastRetrievalTrace(rag.BuildRetrievalErrorTrace(query, o.kbID, "", 0, time.Since(start), err))
		log.Printf("create RAG query failed: %v", err)
		return &schema.Message{
			Role:    schema.Assistant,
			Content: o.noKnowledgeAnswer(query),
		}, nil
	}

	docs, err := ragQuery.RetrieveDocuments(ctx, query)
	if err != nil {
		o.setLastRetrievalTrace(rag.BuildRetrievalErrorTrace(query, ragQuery.KnowledgeBaseID(), ragQuery.StoreName(), ragQuery.TopK(), time.Since(start), err))
		log.Printf("retrieve rag docs failed: %v", err)
		return &schema.Message{
			Role:    schema.Assistant,
			Content: o.noKnowledgeAnswer(query),
		}, nil
	}
	o.setLastRetrievalTrace(rag.BuildRetrievalTrace(query, ragQuery.KnowledgeBaseID(), ragQuery.StoreName(), ragQuery.TopK(), docs, time.Since(start)))
	if len(docs) == 0 {
		return &schema.Message{
			Role:    schema.Assistant,
			Content: o.noKnowledgeAnswer(query),
		}, nil
	}

	ragPrompt := rag.BuildRAGPrompt(query, docs)
	ragMessages := cloneMessagesWithLastUserPrompt(messages, ragPrompt)
	resp, err := o.llm.Generate(ctx, ragMessages)
	if err != nil {
		return nil, fmt.Errorf("rag generate failed: %w", err)
	}
	return resp, nil
}

func (o *AliRAGModel) StreamResponse(ctx context.Context, messages []*schema.Message, cb StreamCallback) (string, error) {
	if len(messages) == 0 {
		return "", fmt.Errorf("no messages provided")
	}
	query := messages[len(messages)-1].Content
	start := time.Now()

	ragQuery, err := rag.NewRAGQuery(ctx, o.email, o.kbID)
	if err != nil {
		o.setLastRetrievalTrace(rag.BuildRetrievalErrorTrace(query, o.kbID, "", 0, time.Since(start), err))
		log.Printf("create RAG query failed: %v", err)
		text := o.noKnowledgeAnswer(query)
		if cb != nil {
			cb(text)
		}
		return text, nil
	}

	docs, err := ragQuery.RetrieveDocuments(ctx, query)
	if err != nil {
		o.setLastRetrievalTrace(rag.BuildRetrievalErrorTrace(query, ragQuery.KnowledgeBaseID(), ragQuery.StoreName(), ragQuery.TopK(), time.Since(start), err))
		log.Printf("retrieve rag docs failed: %v", err)
		text := o.noKnowledgeAnswer(query)
		if cb != nil {
			cb(text)
		}
		return text, nil
	}
	o.setLastRetrievalTrace(rag.BuildRetrievalTrace(query, ragQuery.KnowledgeBaseID(), ragQuery.StoreName(), ragQuery.TopK(), docs, time.Since(start)))
	if len(docs) == 0 {
		text := o.noKnowledgeAnswer(query)
		if cb != nil {
			cb(text)
		}
		return text, nil
	}

	ragPrompt := rag.BuildRAGPrompt(query, docs)
	ragMessages := cloneMessagesWithLastUserPrompt(messages, ragPrompt)

	stream, err := o.llm.Stream(ctx, ragMessages)
	if err != nil {
		return "", fmt.Errorf("rag stream failed: %w", err)
	}
	defer stream.Close()

	var fullResp strings.Builder
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("rag stream recv failed: %w", err)
		}
		if len(msg.Content) == 0 {
			continue
		}
		fullResp.WriteString(msg.Content)
		if cb != nil {
			cb(msg.Content)
		}
	}
	return fullResp.String(), nil
}

func (o *AliRAGModel) streamWithoutRAG(ctx context.Context, messages []*schema.Message, cb StreamCallback) (string, error) {
	stream, err := o.llm.Stream(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("stream without rag failed: %w", err)
	}
	defer stream.Close()

	var fullResp strings.Builder
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("stream without rag recv failed: %w", err)
		}
		if len(msg.Content) == 0 {
			continue
		}
		fullResp.WriteString(msg.Content)
		if cb != nil {
			cb(msg.Content)
		}
	}
	return fullResp.String(), nil
}

func (o *AliRAGModel) GetModelType() string { return "2" }

func (o *AliRAGModel) noKnowledgeAnswer(query string) string {
	kb := strings.TrimSpace(o.kbID)
	if kb == "" {
		kb = "default"
	}
	q := strings.TrimSpace(query)
	if q == "" {
		q = "（空问题）"
	}
	return fmt.Sprintf("未在知识库 %s 检索到可用文档片段，无法基于文档回答。请确认文档已上传并完成索引后重试。问题：%s", kb, q)
}

func (o *AliRAGModel) setLastRetrievalTrace(trace *rag.RetrievalTrace) {
	o.traceMu.Lock()
	defer o.traceMu.Unlock()
	o.lastTrace = cloneRetrievalTrace(trace)
}

func (o *AliRAGModel) GetLastRetrievalTrace() *rag.RetrievalTrace {
	o.traceMu.RLock()
	defer o.traceMu.RUnlock()
	return cloneRetrievalTrace(o.lastTrace)
}

func cloneRetrievalTrace(trace *rag.RetrievalTrace) *rag.RetrievalTrace {
	if trace == nil {
		return nil
	}
	out := *trace
	if len(trace.Sources) > 0 {
		out.Sources = make([]rag.RetrievedSource, len(trace.Sources))
		copy(out.Sources, trace.Sources)
	}
	return &out
}

type MCPModel struct {
	llm        model.ToolCallingChatModel
	mcpClient  *client.Client
	email      string
	mcpBaseURL string
}

func NewMCPModel(ctx context.Context, email string) (*MCPModel, error) {
	key := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	conf := config.GetConfig()
	modelName, baseURL := resolveModelConfig("3", conf.RagModelConfig.RagChatModelName, conf.RagModelConfig.RagBaseUrl)

	llm, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		BaseURL: baseURL,
		Model:   modelName,
		APIKey:  key,
	})
	if err != nil {
		return nil, fmt.Errorf("create mcp model failed: %w", err)
	}

	return &MCPModel{
		llm:        llm,
		email:      email,
		mcpBaseURL: buildMCPBaseURL(os.Getenv("MCP_BASE_URL")),
	}, nil
}

func (m *MCPModel) getMCPClient(ctx context.Context) (*client.Client, error) {
	if m.mcpClient != nil {
		return m.mcpClient, nil
	}

	httpTransport, err := transport.NewStreamableHTTP(m.mcpBaseURL)
	if err != nil {
		return nil, fmt.Errorf("create mcp transport failed: %w", err)
	}
	m.mcpClient = client.NewClient(httpTransport)

	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "MCP-Go AIHelper Client",
		Version: "1.0.0",
	}
	initRequest.Params.Capabilities = mcp.ClientCapabilities{}
	if _, err := m.mcpClient.Initialize(ctx, initRequest); err != nil {
		return nil, fmt.Errorf("mcp initialize failed: %w", err)
	}
	return m.mcpClient, nil
}

func (m *MCPModel) GenerateResponse(ctx context.Context, messages []*schema.Message) (*schema.Message, error) {
	if len(messages) == 0 {
		return nil, fmt.Errorf("no messages provided")
	}

	query := messages[len(messages)-1].Content
	observations, directAnswer, err := m.runReActLoop(ctx, messages, query)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(directAnswer) != "" {
		return &schema.Message{Role: schema.Assistant, Content: directAnswer}, nil
	}
	return m.buildFinalAnswerByObservations(ctx, messages, query, observations)
}

func (m *MCPModel) StreamResponse(ctx context.Context, messages []*schema.Message, cb StreamCallback) (string, error) {
	if len(messages) == 0 {
		return "", fmt.Errorf("no messages provided")
	}

	query := messages[len(messages)-1].Content
	observations, directAnswer, err := m.runReActLoop(ctx, messages, query)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(directAnswer) != "" {
		m.streamTextChunks(directAnswer, cb)
		return directAnswer, nil
	}
	return m.streamFinalAnswerByObservations(ctx, messages, query, observations, cb)
}

type AIToolCall struct {
	IsToolCall bool                   `json:"isToolCall"`
	ToolName   string                 `json:"toolName"`
	Args       map[string]interface{} `json:"args"`
}

func (m *MCPModel) buildToolFailureText(query, toolName string, err error) string {
	name := strings.TrimSpace(toolName)
	if name == "" {
		name = "tool"
	}
	_ = query
	log.Printf("tool call failed (%s): %v", name, err)
	return fmt.Sprintf("%s call failed, please retry later.", name)
}

func (m *MCPModel) streamTextChunks(text string, cb StreamCallback) {
	if cb == nil {
		return
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return
	}
	runes := []rune(trimmed)
	const chunkSize = 20
	for i := 0; i < len(runes); i += chunkSize {
		end := i + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		cb(string(runes[i:end]))
	}
}

func (m *MCPModel) buildFirstPrompt(query string) string {
	return fmt.Sprintf(`You are an assistant that can call MCP tools.

Available tools:
- get_weather: get current weather. args: {"city":"..."}
- get_weather_forecast: get forecast. args: {"city":"...", "days": 1-7}
- web_search: search web pages. args: {"query":"...", "limit": 1-10}
- web_fetch: fetch page content by URL. args: {"url":"https://...", "max_chars": 200-8000}
- translate_text: translate text. args: {"text":"...", "source_lang":"auto|zh|en|ja...", "target_lang":"zh|en|ja..."}

Rules:
1. If a tool is needed, return ONLY JSON in this exact schema:
{"isToolCall": true, "toolName": "<one_of_tools>", "args": {...}}
2. If no tool is needed, return a normal natural-language answer.
3. Do not wrap JSON in explanations.
4. For weather forecast requests (future/multi-day), use get_weather_forecast.
5. For search/link finding requests, use web_search.
6. For translation requests, use translate_text.

User question: %s`, query)
}

func (m *MCPModel) buildSecondPrompt(query, toolName string, args map[string]interface{}, toolResult string) string {
	return fmt.Sprintf(`You are an assistant that has received MCP tool results.

Tool name: %s
Tool args: %v
Tool result: %s

User question: %s

Please provide the final answer to the user based on the tool result.`, toolName, args, toolResult, query)
}

func (m *MCPModel) parseAIResponse(response string) (*AIToolCall, error) {
	raw := strings.TrimSpace(response)
	if raw == "" {
		return &AIToolCall{IsToolCall: false}, nil
	}

	if toolCall, ok := m.tryParseToolCallJSON(raw); ok {
		return toolCall, nil
	}
	if jsonBlock := extractFirstJSONObject(raw); jsonBlock != "" {
		if toolCall, ok := m.tryParseToolCallJSON(jsonBlock); ok {
			return toolCall, nil
		}
	}

	if city := m.extractCityFromResponse(raw); city != "" {
		return &AIToolCall{
			IsToolCall: true,
			ToolName:   "get_weather",
			Args:       map[string]interface{}{"city": city},
		}, nil
	}

	return &AIToolCall{IsToolCall: false}, nil
}

func (m *MCPModel) tryParseToolCallJSON(raw string) (*AIToolCall, bool) {
	var toolCall AIToolCall
	if err := json.Unmarshal([]byte(raw), &toolCall); err != nil {
		return nil, false
	}

	if toolCall.IsToolCall && toolCall.ToolName == "" && strings.Contains(raw, "get_weather") {
		toolCall.ToolName = "get_weather"
	}
	if toolCall.IsToolCall && toolCall.ToolName == "" && strings.Contains(raw, "translate_text") {
		toolCall.ToolName = "translate_text"
	}
	if toolCall.IsToolCall && toolCall.ToolName == "" {
		return nil, false
	}
	if toolCall.Args == nil {
		toolCall.Args = map[string]interface{}{}
	}
	toolCall.ToolName = strings.TrimSpace(toolCall.ToolName)
	if !isSupportedMCPTool(toolCall.ToolName) {
		return nil, false
	}
	return &toolCall, true
}

func isSupportedMCPTool(name string) bool {
	switch strings.TrimSpace(name) {
	case "get_weather", "get_weather_forecast", "web_search", "web_fetch", "translate_text":
		return true
	default:
		return false
	}
}

func extractFirstJSONObject(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```") {
		text = strings.TrimPrefix(text, "```json")
		text = strings.TrimPrefix(text, "```JSON")
		text = strings.TrimPrefix(text, "```")
		text = strings.TrimSuffix(text, "```")
		text = strings.TrimSpace(text)
	}

	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start == -1 || end == -1 || end <= start {
		return ""
	}
	return text[start : end+1]
}

func buildMCPBaseURL(raw string) string {
	addr := strings.TrimSpace(raw)
	if addr == "" {
		return "http://127.0.0.1:8082/mcp"
	}
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		addr = strings.TrimRight(addr, "/")
		if strings.HasSuffix(addr, "/mcp") {
			return addr
		}
		return addr + "/mcp"
	}
	if strings.HasPrefix(addr, ":") {
		return "http://127.0.0.1" + addr + "/mcp"
	}
	addr = strings.TrimRight(addr, "/")
	if strings.HasSuffix(addr, "/mcp") {
		return "http://" + addr
	}
	return "http://" + addr + "/mcp"
}

func (m *MCPModel) callMCPTool(ctx context.Context, client *client.Client, toolName string, args map[string]interface{}) (string, error) {
	callToolRequest := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      toolName,
			Arguments: args,
		},
	}

	result, err := client.CallTool(ctx, callToolRequest)
	if err != nil {
		return "", fmt.Errorf("mcp tool call failed: %w", err)
	}

	var text strings.Builder
	for _, content := range result.Content {
		if textContent, ok := content.(mcp.TextContent); ok {
			if t := strings.TrimSpace(textContent.Text); t != "" {
				text.WriteString(t)
				text.WriteString("\n")
			}
			continue
		}
		raw, marshalErr := json.Marshal(content)
		if marshalErr == nil && len(raw) > 0 && string(raw) != "null" {
			text.Write(raw)
			text.WriteString("\n")
		}
	}

	out := strings.TrimSpace(text.String())
	if out == "" {
		raw, _ := json.Marshal(result.Content)
		return "", fmt.Errorf("mcp tool returned empty content: %s", string(raw))
	}
	return out, nil
}

func (m *MCPModel) extractCityFromResponse(response string) string {
	raw := strings.TrimSpace(response)
	if raw == "" {
		return ""
	}

	if toolCall, ok := m.tryParseToolCallJSON(raw); ok {
		if city, ok := toolCall.Args["city"].(string); ok {
			return strings.TrimSpace(city)
		}
	}
	if jsonBlock := extractFirstJSONObject(raw); jsonBlock != "" {
		if toolCall, ok := m.tryParseToolCallJSON(jsonBlock); ok {
			if city, ok := toolCall.Args["city"].(string); ok {
				return strings.TrimSpace(city)
			}
		}
	}

	patterns := []string{
		`(?i)"city"\s*[:=]\s*"([^"]+)"`,
		`(?i)city\s*[:=]\s*([^\s,}]+)`,
	}
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(raw)
		if len(matches) >= 2 {
			city := strings.TrimSpace(matches[1])
			city = strings.Trim(city, `"'`)
			if city != "" {
				return city
			}
		}
	}
	return ""
}

func (m *MCPModel) inferToolCallFromQuery(query string) (*AIToolCall, bool) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, false
	}
	qLower := strings.ToLower(q)

	if looksLikeTranslationRequest(qLower) {
		text, sourceLang, targetLang := parseTranslationIntent(q)
		if strings.TrimSpace(text) == "" {
			text = q
		}
		return &AIToolCall{
			IsToolCall: true,
			ToolName:   "translate_text",
			Args: map[string]interface{}{
				"text":        text,
				"source_lang": sourceLang,
				"target_lang": targetLang,
			},
		}, true
	}

	if urlText := extractFirstURL(q); urlText != "" {
		return &AIToolCall{
			IsToolCall: true,
			ToolName:   "web_fetch",
			Args: map[string]interface{}{
				"url":       urlText,
				"max_chars": 4000,
			},
		}, true
	}

	if containsAny(qLower, []string{"search", "link", "website", "官网", "搜索", "链接"}) {
		return &AIToolCall{
			IsToolCall: true,
			ToolName:   "web_search",
			Args: map[string]interface{}{
				"query": q,
				"limit": 5,
			},
		}, true
	}

	if containsAny(qLower, []string{"weather", "forecast", "temperature", "天气", "气温", "温度", "预报"}) {
		city := extractCityFromQuery(q)
		if city == "" || isUnsupportedLocation(city) {
			return nil, false
		}

		if containsAny(qLower, []string{"forecast", "tomorrow", "future", "预报", "明天", "后天"}) || regexp.MustCompile(`(?i)\d+\s*(day|days|天)`).MatchString(q) {
			days := extractForecastDays(q)
			return &AIToolCall{
				IsToolCall: true,
				ToolName:   "get_weather_forecast",
				Args: map[string]interface{}{
					"city": city,
					"days": days,
				},
			}, true
		}

		return &AIToolCall{
			IsToolCall: true,
			ToolName:   "get_weather",
			Args: map[string]interface{}{
				"city": city,
			},
		}, true
	}

	return nil, false
}

func looksLikeTranslationRequest(qLower string) bool {
	keywords := []string{"translate", "translation", "翻译", "译成", "翻成"}
	for _, k := range keywords {
		if strings.Contains(qLower, strings.ToLower(k)) {
			return true
		}
	}
	return false
}

func parseTranslationIntent(query string) (text, sourceLang, targetLang string) {
	q := strings.TrimSpace(query)
	sourceLang = "auto"
	targetLang = "zh"

	if m := regexp.MustCompile(`(?i)\bfrom\s+([a-z]{2,12})\s+to\s+([a-z]{2,12})\b`).FindStringSubmatch(q); len(m) >= 3 {
		sourceLang = normalizeLangAlias(m[1], sourceLang)
		targetLang = normalizeLangAlias(m[2], targetLang)
	}

	lower := strings.ToLower(q)
	if strings.Contains(lower, " to english") || strings.Contains(lower, " into english") {
		targetLang = "en"
	}
	if strings.Contains(lower, " to chinese") || strings.Contains(lower, " into chinese") {
		targetLang = "zh"
	}
	if strings.Contains(lower, " to japanese") || strings.Contains(lower, " into japanese") {
		targetLang = "ja"
	}
	if strings.Contains(lower, " to korean") || strings.Contains(lower, " into korean") {
		targetLang = "ko"
	}
	if strings.Contains(lower, "翻译成英文") {
		targetLang = "en"
	}
	if strings.Contains(lower, "翻译成中文") {
		targetLang = "zh"
	}

	if m := regexp.MustCompile(`["“](.+?)["”]`).FindStringSubmatch(q); len(m) >= 2 {
		text = strings.TrimSpace(m[1])
	}
	if text == "" {
		if m := regexp.MustCompile(`(?i)translate\s+(.+?)\s+(?:to|into)\s+[a-z]+`).FindStringSubmatch(q); len(m) >= 2 {
			text = strings.TrimSpace(m[1])
		}
	}
	if text == "" {
		text = q
	}
	return text, sourceLang, targetLang
}

func normalizeLangAlias(raw, fallback string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "", "auto":
		return fallback
	case "zh", "chinese", "中文":
		return "zh"
	case "en", "english", "英文":
		return "en"
	case "ja", "japanese", "日文":
		return "ja"
	case "ko", "korean", "韩文":
		return "ko"
	case "fr", "french":
		return "fr"
	case "de", "german":
		return "de"
	case "es", "spanish":
		return "es"
	default:
		return v
	}
}

func extractFirstURL(text string) string {
	re := regexp.MustCompile(`https?://[^\s]+`)
	if m := re.FindString(text); m != "" {
		return strings.TrimSpace(m)
	}
	return ""
}

func containsAny(text string, words []string) bool {
	for _, w := range words {
		if strings.Contains(text, strings.ToLower(w)) {
			return true
		}
	}
	return false
}

func extractCityFromQuery(query string) string {
	q := strings.TrimSpace(query)
	if q == "" {
		return ""
	}

	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bweather\s+in\s+([a-zA-Z\s]{2,40})`),
		regexp.MustCompile(`(?i)\bforecast\s+for\s+([a-zA-Z\s]{2,40})`),
		regexp.MustCompile(`([\p{Han}]{2,20})(?:天气|气温|温度|预报)`),
		regexp.MustCompile(`([A-Za-z][A-Za-z\s]{1,30})\s+(?:weather|forecast)`),
	}
	for _, re := range patterns {
		if m := re.FindStringSubmatch(q); len(m) >= 2 {
			city := strings.TrimSpace(m[1])
			city = strings.Trim(city, ",.?!，。！？ ")
			if city != "" {
				return city
			}
		}
	}
	return ""
}

func extractForecastDays(query string) int {
	reList := []*regexp.Regexp{
		regexp.MustCompile(`(?i)(\d+)\s*(day|days)`),
		regexp.MustCompile(`(\d+)\s*天`),
	}
	for _, re := range reList {
		if m := re.FindStringSubmatch(query); len(m) >= 2 {
			if n, err := strconv.Atoi(strings.TrimSpace(m[1])); err == nil {
				if n < 1 {
					return 1
				}
				if n > 7 {
					return 7
				}
				return n
			}
		}
	}
	if strings.Contains(query, "明天") && strings.Contains(query, "后天") {
		return 3
	}
	return 3
}

func isUnsupportedLocation(city string) bool {
	s := strings.ToLower(strings.TrimSpace(city))
	keywords := []string{
		"火星", "月球", "木星", "土星", "水星", "金星", "天王星", "海王星", "冥王星", "太阳",
		"mars", "moon", "jupiter", "saturn", "mercury", "venus", "uranus", "neptune", "pluto", "sun",
	}
	for _, k := range keywords {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

func (m *MCPModel) GetModelType() string { return "3" }

func (m *MCPModel) Close() {
	if m.mcpClient != nil {
		m.mcpClient.Close()
	}
}
