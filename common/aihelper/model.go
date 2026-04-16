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

// OpenAI base chat model (modelType=1).
type OpenAIModel struct {
	llm model.ToolCallingChatModel
}

func NewOpenAIModel(ctx context.Context) (*OpenAIModel, error) {
	key := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	modelName := strings.TrimSpace(os.Getenv("OPENAI_MODEL_NAME"))
	baseURL := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL"))
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("OPEN_AI_BASE_URL"))
	}

	conf := config.GetConfig()
	if modelName == "" {
		modelName = strings.TrimSpace(conf.RagModelConfig.RagChatModelName)
	}
	if baseURL == "" {
		baseURL = strings.TrimSpace(conf.RagModelConfig.RagBaseUrl)
	}

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
		return nil, fmt.Errorf("create openai model failed: %v", err)
	}
	return &OpenAIModel{llm: llm}, nil
}

func (o *OpenAIModel) StreamResponse(ctx context.Context, messages []*schema.Message, cb StreamCallback) (string, error) {
	stream, err := o.llm.Stream(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("openai stream failed: %v", err)
	}
	defer stream.Close()

	var fullResp strings.Builder
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("openai stream recv failed: %v", err)
		}
		if len(msg.Content) > 0 {
			fullResp.WriteString(msg.Content)
			cb(msg.Content)
		}
	}
	return fullResp.String(), nil
}

func (o *OpenAIModel) GenerateResponse(ctx context.Context, messages []*schema.Message) (*schema.Message, error) {
	resp, err := o.llm.Generate(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("openai stream failed: %v", err)
	}
	return resp, nil
}

func (o *OpenAIModel) GetModelType() string { return "1" }

// RAG model (modelType=2), fallback to normal chat if retrieval fails.
type AliRAGModel struct {
	llm   model.ToolCallingChatModel
	email string
}

func NewAliRAGModel(ctx context.Context, email string) (*AliRAGModel, error) {
	key := os.Getenv("OPENAI_API_KEY")
	conf := config.GetConfig()
	modelName := conf.RagModelConfig.RagChatModelName
	baseURL := conf.RagModelConfig.RagBaseUrl

	llm, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		BaseURL: baseURL,
		Model:   modelName,
		APIKey:  key,
	})
	if err != nil {
		return nil, fmt.Errorf("create ali rag model failed: %v", err)
	}
	return &AliRAGModel{
		llm:   llm,
		email: email,
	}, nil
}

func (o *AliRAGModel) GenerateResponse(ctx context.Context, messages []*schema.Message) (*schema.Message, error) {
	ragQuery, err := rag.NewRAGQuery(ctx, o.email)
	if err != nil {
		log.Printf("Failed to create RAG query (user may not have uploaded file): %v", err)
		resp, err := o.llm.Generate(ctx, messages)
		if err != nil {
			return nil, fmt.Errorf("ali rag generate failed: %v", err)
		}
		return resp, nil
	}

	if len(messages) == 0 {
		return nil, fmt.Errorf("no messages provided")
	}
	lastMessage := messages[len(messages)-1]
	query := lastMessage.Content

	docs, err := ragQuery.RetrieveDocuments(ctx, query)
	if err != nil {
		log.Printf("Failed to retrieve documents: %v", err)
		resp, err := o.llm.Generate(ctx, messages)
		if err != nil {
			return nil, fmt.Errorf("ali rag generate failed: %v", err)
		}
		return resp, nil
	}

	ragPrompt := rag.BuildRAGPrompt(query, docs)

	ragMessages := make([]*schema.Message, len(messages))
	copy(ragMessages, messages)
	ragMessages[len(ragMessages)-1] = &schema.Message{
		Role:    schema.User,
		Content: ragPrompt,
	}

	resp, err := o.llm.Generate(ctx, ragMessages)
	if err != nil {
		return nil, fmt.Errorf("ali rag generate failed: %v", err)
	}
	return resp, nil
}

func (o *AliRAGModel) StreamResponse(ctx context.Context, messages []*schema.Message, cb StreamCallback) (string, error) {
	ragQuery, err := rag.NewRAGQuery(ctx, o.email)
	if err != nil {
		log.Printf("Failed to create RAG query (user may not have uploaded file): %v", err)
		return o.streamWithoutRAG(ctx, messages, cb)
	}

	if len(messages) == 0 {
		return "", fmt.Errorf("no messages provided")
	}
	lastMessage := messages[len(messages)-1]
	query := lastMessage.Content

	docs, err := ragQuery.RetrieveDocuments(ctx, query)
	if err != nil {
		log.Printf("Failed to retrieve documents: %v", err)
		return o.streamWithoutRAG(ctx, messages, cb)
	}

	ragPrompt := rag.BuildRAGPrompt(query, docs)

	ragMessages := make([]*schema.Message, len(messages))
	copy(ragMessages, messages)
	ragMessages[len(ragMessages)-1] = &schema.Message{
		Role:    schema.User,
		Content: ragPrompt,
	}

	stream, err := o.llm.Stream(ctx, ragMessages)
	if err != nil {
		return "", fmt.Errorf("ali rag stream failed: %v", err)
	}
	defer stream.Close()

	var fullResp strings.Builder

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("ali rag stream recv failed: %v", err)
		}
		if len(msg.Content) > 0 {
			fullResp.WriteString(msg.Content)
			cb(msg.Content)
		}
	}

	return fullResp.String(), nil
}

func (o *AliRAGModel) streamWithoutRAG(ctx context.Context, messages []*schema.Message, cb StreamCallback) (string, error) {
	stream, err := o.llm.Stream(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("ali rag stream failed: %v", err)
	}
	defer stream.Close()

	var fullResp strings.Builder

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("ali rag stream recv failed: %v", err)
		}
		if len(msg.Content) > 0 {
			fullResp.WriteString(msg.Content)
			cb(msg.Content)
		}
	}

	return fullResp.String(), nil
}

func (o *AliRAGModel) GetModelType() string { return "2" }

// MCP model (modelType=3): decide tool call first, then generate final answer.
type MCPModel struct {
	llm        model.ToolCallingChatModel
	mcpClient  *client.Client
	email      string
	mcpBaseURL string
}

func NewMCPModel(ctx context.Context, email string) (*MCPModel, error) {
	key := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	modelName := strings.TrimSpace(os.Getenv("OPENAI_MODEL_NAME"))
	baseURL := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL"))
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("OPEN_AI_BASE_URL"))
	}

	conf := config.GetConfig()
	if modelName == "" {
		modelName = strings.TrimSpace(conf.RagModelConfig.RagChatModelName)
	}
	if baseURL == "" {
		baseURL = strings.TrimSpace(conf.RagModelConfig.RagBaseUrl)
	}

	llm, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		BaseURL: baseURL,
		Model:   modelName,
		APIKey:  key,
	})
	if err != nil {
		return nil, fmt.Errorf("create mcp model failed: %v", err)
	}

	mcpBaseURL := buildMCPBaseURL(os.Getenv("MCP_BASE_URL"))

	return &MCPModel{
		llm:        llm,
		mcpBaseURL: mcpBaseURL,
		email:      email,
	}, nil
}

func (m *MCPModel) getMCPClient(ctx context.Context) (*client.Client, error) {
	if m.mcpClient == nil {
		httpTransport, err := transport.NewStreamableHTTP(m.mcpBaseURL)
		if err != nil {
			return nil, fmt.Errorf("create mcp transport failed: %v", err)
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
			return nil, fmt.Errorf("mcp client initialize failed: %v", err)
		}
	}
	return m.mcpClient, nil
}

func (m *MCPModel) GenerateResponse(ctx context.Context, messages []*schema.Message) (*schema.Message, error) {
	if len(messages) == 0 {
		return nil, fmt.Errorf("no messages provided")
	}

	lastMessage := messages[len(messages)-1]
	query := lastMessage.Content

	firstPrompt := m.buildFirstPrompt(query)
	firstMessages := make([]*schema.Message, len(messages))
	copy(firstMessages, messages)
	firstMessages[len(firstMessages)-1] = &schema.Message{
		Role:    schema.User,
		Content: firstPrompt,
	}

	firstResp, err := m.llm.Generate(ctx, firstMessages)
	if err != nil {
		return nil, fmt.Errorf("mcp first generate failed: %v", err)
	}
	log.Println("first resp is ", firstResp)
	aiResult := firstResp.Content
	toolCall, err := m.parseAIResponse(aiResult)
	if err != nil {
		log.Printf("Failed to parse AI response: %v", err)
		return firstResp, nil
	}

	if !toolCall.IsToolCall {
		if inferred, ok := m.inferToolCallFromQuery(query); ok {
			toolCall = inferred
		}
	}

	if !toolCall.IsToolCall {
		log.Println("toolCall IsToolCall is false ", firstResp)
		return firstResp, nil
	}
	log.Println("toolCall IsToolCall is true ", firstResp)
	mcpClient, err := m.getMCPClient(ctx)
	if err != nil {
		log.Printf("MCP client error: %v", err)
		return &schema.Message{
			Role:    schema.Assistant,
			Content: m.buildToolFailureText(query, toolCall.ToolName, err),
		}, nil
	}

	toolResult, err := m.callMCPTool(ctx, mcpClient, toolCall.ToolName, toolCall.Args)
	if err != nil {
		log.Printf("MCP tool call failed: %v", err)
		return &schema.Message{
			Role:    schema.Assistant,
			Content: m.buildToolFailureText(query, toolCall.ToolName, err),
		}, nil
	}

	secondPrompt := m.buildSecondPrompt(query, toolCall.ToolName, toolCall.Args, toolResult)
	secondMessages := make([]*schema.Message, len(messages))
	copy(secondMessages, messages)
	secondMessages[len(secondMessages)-1] = &schema.Message{
		Role:    schema.User,
		Content: secondPrompt,
	}

	finalResp, err := m.llm.Generate(ctx, secondMessages)

	if err != nil {
		return nil, fmt.Errorf("mcp second generate failed: %v", err)
	}
	log.Println("final response:", finalResp)
	return finalResp, nil
}

func (m *MCPModel) StreamResponse(ctx context.Context, messages []*schema.Message, cb StreamCallback) (string, error) {
	if len(messages) == 0 {
		return "", fmt.Errorf("no messages provided")
	}

	lastMessage := messages[len(messages)-1]
	query := lastMessage.Content

	firstPrompt := m.buildFirstPrompt(query)
	firstMessages := make([]*schema.Message, len(messages))
	copy(firstMessages, messages)
	firstMessages[len(firstMessages)-1] = &schema.Message{
		Role:    schema.User,
		Content: firstPrompt,
	}

	firstResp, err := m.llm.Generate(ctx, firstMessages)
	if err != nil {
		return "", fmt.Errorf("mcp first generate failed: %v", err)
	}

	aiResult := firstResp.Content
	toolCall, err := m.parseAIResponse(aiResult)
	if err != nil {
		log.Printf("Failed to parse AI response: %v", err)
		return aiResult, nil
	}

	if !toolCall.IsToolCall {
		if inferred, ok := m.inferToolCallFromQuery(query); ok {
			toolCall = inferred
		}
	}

	if !toolCall.IsToolCall {
		return aiResult, nil
	}

	mcpClient, err := m.getMCPClient(ctx)
	if err != nil {
		log.Printf("MCP client error: %v", err)
		fallback := m.buildToolFailureText(query, toolCall.ToolName, err)
		m.streamTextChunks(fallback, cb)
		return fallback, nil
	}

	toolResult, err := m.callMCPTool(ctx, mcpClient, toolCall.ToolName, toolCall.Args)
	if err != nil {
		log.Printf("MCP tool call failed: %v", err)
		fallback := m.buildToolFailureText(query, toolCall.ToolName, err)
		m.streamTextChunks(fallback, cb)
		return fallback, nil
	}

	secondPrompt := m.buildSecondPrompt(query, toolCall.ToolName, toolCall.Args, toolResult)
	secondMessages := make([]*schema.Message, len(messages))
	copy(secondMessages, messages)
	secondMessages[len(secondMessages)-1] = &schema.Message{
		Role:    schema.User,
		Content: secondPrompt,
	}

	stream, err := m.llm.Stream(ctx, secondMessages)
	if err != nil {
		return "", fmt.Errorf("mcp second stream failed: %v", err)
	}
	defer stream.Close()

	var finalResp strings.Builder

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("mcp second stream recv failed: %v", err)
		}
		if len(msg.Content) > 0 {
			finalResp.WriteString(msg.Content)
			cb(msg.Content)
		}
	}

	return finalResp.String(), nil
}

type AIToolCall struct {
	IsToolCall bool                   `json:"isToolCall"`
	ToolName   string                 `json:"toolName"`
	Args       map[string]interface{} `json:"args"`
}

func (m *MCPModel) buildToolFailureText(query, toolName string, err error) string {
	name := strings.TrimSpace(toolName)
	if name == "" {
		name = "工具"
	}
	_ = query
	log.Printf("tool call failed (%s): %v", name, err)
	return fmt.Sprintf("%s调用失败，天气服务暂时不可用，请稍后重试。", name)
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

Rules:
1. If a tool is needed, return ONLY JSON in this exact schema:
{"isToolCall": true, "toolName": "<one_of_tools>", "args": {...}}
2. If no tool is needed, return a normal natural-language answer.
3. Do not wrap JSON in explanations.
4. For weather forecast requests (future/multi-day), use get_weather_forecast.
5. For search/link finding requests, use web_search.
6. For non-Earth locations (e.g., Mars), do not call weather tools; answer directly that this service only supports Earth cities.

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

	// Parse strict JSON first.
	if toolCall, ok := m.tryParseToolCallJSON(raw); ok {
		return toolCall, nil
	}

	// Handle markdown-wrapped JSON or mixed text.
	if jsonBlock := extractFirstJSONObject(raw); jsonBlock != "" {
		if toolCall, ok := m.tryParseToolCallJSON(jsonBlock); ok {
			return toolCall, nil
		}
	}

	// Fallback: if city can be extracted, trigger weather tool.
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
	case "get_weather", "get_weather_forecast", "web_search", "web_fetch":
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
		return "", fmt.Errorf("mcp tool call failed: %v", err)
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

	// Extract city from tool-call JSON.
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
		`(?i)"city"\s*[:=]\s*([^\s,}]+)`,
		`(?i)city\s*[:=]\s*["']?([^"'\s,}]+)`,
	}
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(raw)
		if len(matches) >= 2 {
			city := strings.TrimSpace(matches[1])
			if city != "" {
				return strings.Trim(city, `"'`)
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

	if containsAny(qLower, []string{"搜索", "搜一下", "官网", "链接", "link", "search"}) {
		return &AIToolCall{
			IsToolCall: true,
			ToolName:   "web_search",
			Args: map[string]interface{}{
				"query": q,
				"limit": 5,
			},
		}, true
	}

	if containsAny(qLower, []string{"天气", "气温", "温度", "weather", "forecast", "预报"}) {
		city := extractCityFromQuery(q)
		if city == "" || isUnsupportedLocation(city) {
			return nil, false
		}

		if containsAny(qLower, []string{"未来", "明天", "后天", "forecast", "预报"}) || regexp.MustCompile(`\d+\s*天`).MatchString(qLower) {
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

	// Common "city + weather" forms in Chinese and English.
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`([\p{Han}A-Za-z]+?)未来\d*天`),
		regexp.MustCompile(`([\p{Han}A-Za-z]+?)(今天|明天|后天|现在).{0,6}(天气|气温|温度|weather|forecast|预报)`),
		regexp.MustCompile(`([\p{Han}A-Za-z]+?)(天气|气温|温度|weather|forecast|预报)`),
	}
	for _, re := range patterns {
		if m := re.FindStringSubmatch(q); len(m) >= 2 {
			city := strings.TrimSpace(m[1])
			city = strings.Trim(city, "，。,.?？:：!！")
			if city != "" {
				return city
			}
		}
	}
	return ""
}

func extractForecastDays(query string) int {
	re := regexp.MustCompile(`(\d+)\s*天`)
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
