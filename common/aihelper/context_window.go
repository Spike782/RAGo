package aihelper

import (
	"ai-chat/config"
	"ai-chat/model"
)

const (
	defaultContextMaxTokens  = 16384
	defaultReservedOutTokens = 2048
	defaultMinContextMsgNum  = 6
)

type TokenUsage struct {
	TotalTokens     int
	InputBudget     int
	UsedTokens      int
	DroppedMessages int
	Trimmed         bool
}

func trimModelMessagesForInput(messages []*model.Message) ([]*model.Message, TokenUsage) {
	budget := getInputTokenBudget()
	minKeep := getMinContextMessages()

	tokenByMsg := make([]int, len(messages))
	total := 0
	for i, msg := range messages {
		tok := estimateModelMessageTokens(msg)
		tokenByMsg[i] = tok
		total += tok
	}

	usage := TokenUsage{
		TotalTokens: total,
		InputBudget: budget,
		UsedTokens:  total,
	}

	if total <= budget || len(messages) == 0 {
		return messages, usage
	}

	if minKeep < 1 {
		minKeep = 1
	}
	if minKeep > len(messages) {
		minKeep = len(messages)
	}

	selected := make(map[int]struct{}, len(messages))
	used := 0

	mandatoryStart := len(messages) - minKeep
	for i := mandatoryStart; i < len(messages); i++ {
		selected[i] = struct{}{}
		used += tokenByMsg[i]
	}

	for i := mandatoryStart - 1; i >= 0; i-- {
		if used+tokenByMsg[i] > budget {
			continue
		}
		selected[i] = struct{}{}
		used += tokenByMsg[i]
	}

	trimmed := make([]*model.Message, 0, len(selected))
	for i := 0; i < len(messages); i++ {
		if _, ok := selected[i]; ok {
			trimmed = append(trimmed, messages[i])
		}
	}

	usage.UsedTokens = used
	usage.DroppedMessages = len(messages) - len(trimmed)
	usage.Trimmed = usage.DroppedMessages > 0

	return trimmed, usage
}

func estimateModelMessageTokens(msg *model.Message) int {
	if msg == nil {
		return 0
	}
	roleToken := 4
	contentToken := estimateTextTokens(msg.Content)
	return roleToken + contentToken
}

func estimateTextTokens(text string) int {
	if text == "" {
		return 1
	}

	asciiCount := 0
	nonASCII := 0
	for _, r := range text {
		if r <= 127 {
			asciiCount++
		} else {
			nonASCII++
		}
	}

	// Rough estimator:
	// - ASCII chars ~ 4 chars/token
	// - CJK and non-ASCII chars ~ 1 char/token
	tokens := (asciiCount / 4) + nonASCII + 1
	if tokens < 1 {
		tokens = 1
	}
	return tokens
}

func getInputTokenBudget() int {
	cfg := config.GetConfig().RagModelConfig
	maxTokens := cfg.ContextMaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultContextMaxTokens
	}

	reservedOut := cfg.ReservedOutputTok
	if reservedOut <= 0 {
		reservedOut = defaultReservedOutTokens
	}

	// Keep at least 25% for input if reserved output is too aggressive.
	maxReserved := maxTokens * 3 / 4
	if reservedOut > maxReserved {
		reservedOut = maxReserved
	}

	inputBudget := maxTokens - reservedOut
	if inputBudget < 512 {
		inputBudget = 512
	}
	return inputBudget
}

func getMinContextMessages() int {
	n := config.GetConfig().RagModelConfig.MinContextMsgNum
	if n <= 0 {
		return defaultMinContextMsgNum
	}
	return n
}
