package rag

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
)

type RetrievedSource struct {
	DocumentID string `json:"documentId,omitempty"`
	FileName   string `json:"fileName,omitempty"`
	IndexName  string `json:"indexName,omitempty"`
	Chunk      string `json:"chunk,omitempty"`
	Score      string `json:"score,omitempty"`
	Content    string `json:"content,omitempty"`
	Snippet    string `json:"snippet,omitempty"`
}

type RetrievalTrace struct {
	Enabled         bool              `json:"enabled"`
	Query           string            `json:"query,omitempty"`
	KnowledgeBaseID string            `json:"kbId,omitempty"`
	Store           string            `json:"store,omitempty"`
	TopK            int               `json:"topK,omitempty"`
	Retrieved       int               `json:"retrieved,omitempty"`
	DurationMS      int64             `json:"durationMs,omitempty"`
	Sources         []RetrievedSource `json:"sources,omitempty"`
	Error           string            `json:"error,omitempty"`
}

func BuildRetrievalTrace(query, kbID, store string, topK int, docs []*schema.Document, duration time.Duration) *RetrievalTrace {
	trace := &RetrievalTrace{
		Enabled:         true,
		Query:           strings.TrimSpace(query),
		KnowledgeBaseID: NormalizeKnowledgeBaseID(kbID),
		Store:           strings.TrimSpace(store),
		TopK:            topK,
		DurationMS:      duration.Milliseconds(),
		Sources:         buildSources(docs),
	}
	trace.Retrieved = len(trace.Sources)
	return trace
}

func BuildRetrievalErrorTrace(query, kbID, store string, topK int, duration time.Duration, err error) *RetrievalTrace {
	trace := &RetrievalTrace{
		Enabled:         true,
		Query:           strings.TrimSpace(query),
		KnowledgeBaseID: NormalizeKnowledgeBaseID(kbID),
		Store:           strings.TrimSpace(store),
		TopK:            topK,
		DurationMS:      duration.Milliseconds(),
	}
	if err != nil {
		trace.Error = err.Error()
	}
	return trace
}

func buildSources(docs []*schema.Document) []RetrievedSource {
	out := make([]RetrievedSource, 0, len(docs))
	for _, doc := range docs {
		if doc == nil {
			continue
		}

		scoreText := ""
		if score, ok := metaFloat(doc.MetaData, "score"); ok {
			scoreText = formatScore(score)
		} else if distance, ok := metaFloat(doc.MetaData, "distance"); ok {
			scoreText = formatScore(1 - distance)
		}

		chunk := ""
		idx := metaNumberString(doc.MetaData, "chunk_index")
		total := metaNumberString(doc.MetaData, "chunk_total")
		if idx != "" && total != "" {
			chunk = fmt.Sprintf("%s/%s", idx, total)
		}

		out = append(out, RetrievedSource{
			DocumentID: strings.TrimSpace(doc.ID),
			FileName:   metaString(doc.MetaData, "file_name"),
			IndexName:  metaString(doc.MetaData, "index_name"),
			Chunk:      chunk,
			Score:      scoreText,
			Content:    strings.TrimSpace(doc.Content),
			Snippet:    truncateRunes(strings.TrimSpace(doc.Content), 220),
		})
	}
	return out
}

func formatScore(score float64) string {
	if math.IsNaN(score) || math.IsInf(score, 0) {
		return ""
	}
	return fmt.Sprintf("%.4f", score)
}

func truncateRunes(text string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(text)
	if len(r) <= max {
		return text
	}
	return string(r[:max]) + "..."
}
