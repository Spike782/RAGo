package aihelper

import (
	"ai-chat/config"
	"context"
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/cloudwego/eino/schema"
)

const defaultReActMaxSteps = 4

func (m *MCPModel) reactMaxSteps() int {
	steps := config.GetConfig().RagModelConfig.ReactMaxSteps
	if steps <= 0 {
		return defaultReActMaxSteps
	}
	if steps > 8 {
		return 8
	}
	return steps
}

func (m *MCPModel) runReActLoop(ctx context.Context, messages []*schema.Message, query string) ([]string, string, error) {
	observations := make([]string, 0, 4)
	maxSteps := m.reactMaxSteps()

	for step := 1; step <= maxSteps; step++ {
		planPrompt := m.buildReActStepPrompt(query, observations, step, maxSteps)
		stepMessages := cloneMessagesWithLastUserPrompt(messages, planPrompt)

		planResp, err := m.llm.Generate(ctx, stepMessages)
		if err != nil {
			return observations, "", fmt.Errorf("mcp react planning failed at step %d: %w", step, err)
		}
		rawDecision := strings.TrimSpace(planResp.Content)
		if rawDecision == "" {
			return observations, "", nil
		}

		toolCall, err := m.parseAIResponse(rawDecision)
		if err != nil {
			log.Printf("parse react step response failed (step=%d): %v", step, err)
			if len(observations) == 0 {
				return observations, rawDecision, nil
			}
			return observations, "", nil
		}

		if !toolCall.IsToolCall {
			if inferred, ok := m.inferToolCallFromQuery(query); ok {
				toolCall = inferred
			}
		}

		if !toolCall.IsToolCall {
			return observations, rawDecision, nil
		}

		mcpClient, err := m.getMCPClient(ctx)
		if err != nil {
			log.Printf("MCP client error at step=%d: %v", step, err)
			return observations, m.buildToolFailureText(query, toolCall.ToolName, err), nil
		}

		toolResult, err := m.callMCPTool(ctx, mcpClient, toolCall.ToolName, toolCall.Args)
		if err != nil {
			log.Printf("MCP tool call failed at step=%d: %v", step, err)
			observation := fmt.Sprintf("Step %d tool call failed: name=%s args=%v err=%v", step, toolCall.ToolName, toolCall.Args, err)
			observations = append(observations, observation)
			continue
		}

		observation := fmt.Sprintf("Step %d observation: tool=%s args=%v result=%s", step, toolCall.ToolName, toolCall.Args, toolResult)
		observations = append(observations, observation)
	}

	return observations, "", nil
}

func (m *MCPModel) buildFinalAnswerByObservations(ctx context.Context, messages []*schema.Message, query string, observations []string) (*schema.Message, error) {
	finalPrompt := m.buildReActFinalPrompt(query, observations)
	finalMessages := cloneMessagesWithLastUserPrompt(messages, finalPrompt)
	finalResp, err := m.llm.Generate(ctx, finalMessages)
	if err != nil {
		return nil, fmt.Errorf("mcp final generate failed: %w", err)
	}
	return finalResp, nil
}

func (m *MCPModel) streamFinalAnswerByObservations(ctx context.Context, messages []*schema.Message, query string, observations []string, cb StreamCallback) (string, error) {
	finalPrompt := m.buildReActFinalPrompt(query, observations)
	finalMessages := cloneMessagesWithLastUserPrompt(messages, finalPrompt)

	stream, err := m.llm.Stream(ctx, finalMessages)
	if err != nil {
		return "", fmt.Errorf("mcp final stream failed: %w", err)
	}
	defer stream.Close()

	var finalResp strings.Builder
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("mcp final stream recv failed: %w", err)
		}
		if len(msg.Content) == 0 {
			continue
		}
		finalResp.WriteString(msg.Content)
		if cb != nil {
			cb(msg.Content)
		}
	}
	return finalResp.String(), nil
}

func (m *MCPModel) buildReActStepPrompt(query string, observations []string, step, maxSteps int) string {
	var obsBuilder strings.Builder
	for _, item := range observations {
		obsBuilder.WriteString("- ")
		obsBuilder.WriteString(item)
		obsBuilder.WriteString("\n")
	}
	if obsBuilder.Len() == 0 {
		obsBuilder.WriteString("- (none)\n")
	}

	return fmt.Sprintf(`You are an assistant with MCP tools, following a ReAct process.

Available tools:
- get_weather: {"city":"..."}
- get_weather_forecast: {"city":"...", "days": 1-7}
- web_search: {"query":"...", "limit": 1-10}
- web_fetch: {"url":"https://...", "max_chars": 200-8000}
- translate_text: {"text":"...", "source_lang":"auto|zh|en|ja...", "target_lang":"zh|en|ja..."}

Current step: %d/%d
User question: %s
Observations so far:
%s

If another tool is needed, return ONLY JSON:
{"isToolCall": true, "toolName": "<tool>", "args": {...}}

If no more tools are needed, return the final answer in natural language.`,
		step, maxSteps, query, obsBuilder.String())
}

func (m *MCPModel) buildReActFinalPrompt(query string, observations []string) string {
	var obsBuilder strings.Builder
	for i, item := range observations {
		obsBuilder.WriteString(fmt.Sprintf("%d. %s\n", i+1, item))
	}
	if obsBuilder.Len() == 0 {
		obsBuilder.WriteString("No external tool observations.\n")
	}

	return fmt.Sprintf(`You are an assistant finalizing an answer based on tool observations.

User question: %s
Tool observations:
%s

Please provide a concise, accurate, and user-facing final answer.`,
		query, obsBuilder.String())
}

func cloneMessagesWithLastUserPrompt(messages []*schema.Message, prompt string) []*schema.Message {
	cloned := make([]*schema.Message, len(messages))
	copy(cloned, messages)
	if len(cloned) == 0 {
		return cloned
	}
	cloned[len(cloned)-1] = &schema.Message{
		Role:    schema.User,
		Content: prompt,
	}
	return cloned
}
