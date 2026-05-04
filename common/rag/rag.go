package rag

import (
	"ai-chat/config"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	embeddingArk "github.com/cloudwego/eino-ext/components/embedding/ark"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/schema"
)

const (
	defaultChunkSize       = 800
	defaultChunkOverlap    = 120
	defaultTopK            = 5
	defaultKnowledgeBaseID = "default"
)

type IndexedDocument struct {
	KnowledgeBaseID string
	FileName        string
	FilePath        string
	IndexName       string
}

type RAGIndexer struct {
	indexName    string
	embedding    embedding.Embedder
	store        VectorStore
	chunkSize    int
	chunkOverlap int
}

type RAGQuery struct {
	knowledgeBaseID string
	documents       []IndexedDocument
	embedding       embedding.Embedder
	store           VectorStore
	topK            int
}

type vectorQueryRetriever interface {
	RetrieveByVector(ctx context.Context, filename string, vector []float64, topK int) ([]*schema.Document, error)
}

func NormalizeKnowledgeBaseID(kbID string) string {
	kbID = strings.TrimSpace(strings.ToLower(kbID))
	if kbID == "" {
		return defaultKnowledgeBaseID
	}

	var b strings.Builder
	for _, r := range kbID {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '_', r == '-', r == '.':
			b.WriteRune('_')
		}
	}

	out := strings.Trim(b.String(), "_")
	if out == "" {
		return defaultKnowledgeBaseID
	}
	return out
}

func BuildDocumentIndexName(kbID, fileName string) string {
	kb := NormalizeKnowledgeBaseID(kbID)
	name := strings.ToLower(strings.TrimSpace(filepath.Base(fileName)))
	var b strings.Builder
	for _, r := range name {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '_', r == '-', r == '.':
			b.WriteRune('_')
		}
	}
	docName := strings.Trim(b.String(), "_")
	if docName == "" {
		docName = "doc"
	}
	return fmt.Sprintf("%s__%s", kb, docName)
}

func UserKnowledgeBaseDir(userEmail, kbID string) string {
	return filepath.Join("uploads", userEmail, NormalizeKnowledgeBaseID(kbID))
}

func DiscoverKnowledgeBaseDocuments(userEmail, kbID string) ([]IndexedDocument, error) {
	normalizedKBID := NormalizeKnowledgeBaseID(kbID)
	candidates := []string{normalizedKBID}
	if normalizedKBID != defaultKnowledgeBaseID {
		candidates = append(candidates, defaultKnowledgeBaseID)
	}

	var lastErr error
	for _, candidateKBID := range candidates {
		userDir := UserKnowledgeBaseDir(userEmail, candidateKBID)
		files, err := os.ReadDir(userDir)
		if err != nil {
			lastErr = err
			continue
		}

		docs := make([]IndexedDocument, 0, len(files))
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			fileName := f.Name()
			docs = append(docs, IndexedDocument{
				KnowledgeBaseID: candidateKBID,
				FileName:        fileName,
				FilePath:        filepath.Join(userDir, fileName),
				IndexName:       BuildDocumentIndexName(candidateKBID, fileName),
			})
		}

		if len(docs) == 0 {
			continue
		}

		sort.Slice(docs, func(i, j int) bool {
			return docs[i].FileName < docs[j].FileName
		})
		return docs, nil
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, nil
}

func NewRAGIndexer(indexName, embeddingModel string) (*RAGIndexer, error) {
	ctx := context.Background()
	cfg := config.GetConfig()

	embedder, err := newEmbedder(ctx, embeddingModel)
	if err != nil {
		return nil, err
	}

	store := NewVectorStore()
	if err := store.EnsureIndex(ctx, indexName, cfg.RagModelConfig.RagDimension); err != nil {
		return nil, fmt.Errorf("ensure %s index failed: %w", store.Name(), err)
	}

	chunkSize, chunkOverlap := normalizeChunkConfig(cfg.RagModelConfig.RagChunkSize, cfg.RagModelConfig.RagChunkOverlap)
	return &RAGIndexer{
		indexName:    indexName,
		embedding:    embedder,
		store:        store,
		chunkSize:    chunkSize,
		chunkOverlap: chunkOverlap,
	}, nil
}

func (r *RAGIndexer) IndexFile(ctx context.Context, filePath string) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	text := strings.TrimSpace(string(content))
	if text == "" {
		return fmt.Errorf("file is empty")
	}

	chunks := splitTextIntoChunks(text, r.chunkSize, r.chunkOverlap)
	if len(chunks) == 0 {
		chunks = []string{text}
	}

	docs := make([]*schema.Document, 0, len(chunks))
	for i, chunk := range chunks {
		docs = append(docs, &schema.Document{
			ID:      fmt.Sprintf("chunk_%d", i+1),
			Content: chunk,
			MetaData: map[string]any{
				"source":      filePath,
				"index_name":  r.indexName,
				"chunk_index": i + 1,
				"chunk_total": len(chunks),
			},
		})
	}

	if err := r.store.IndexDocuments(ctx, r.indexName, docs, r.embedding); err != nil {
		return fmt.Errorf("index file via %s failed: %w", r.store.Name(), err)
	}
	return nil
}

func DeleteIndex(ctx context.Context, indexName string) error {
	store := NewVectorStore()
	if err := store.DeleteIndex(ctx, indexName); err != nil {
		return fmt.Errorf("delete %s index failed: %w", store.Name(), err)
	}
	return nil
}

func NewRAGQuery(ctx context.Context, userEmail, kbID string) (*RAGQuery, error) {
	return NewRAGQueryWithTopK(ctx, userEmail, kbID, 0)
}

func NewRAGQueryWithTopK(ctx context.Context, userEmail, kbID string, topK int) (*RAGQuery, error) {
	cfg := config.GetConfig()
	embedder, err := newEmbedder(ctx, cfg.RagModelConfig.RagEmbeddingModel)
	if err != nil {
		return nil, err
	}

	kbID = NormalizeKnowledgeBaseID(kbID)
	docs, err := DiscoverKnowledgeBaseDocuments(userEmail, kbID)
	if err != nil || len(docs) == 0 {
		return nil, fmt.Errorf("no uploaded file found for user=%s kb=%s", userEmail, kbID)
	}

	resolvedTopK := cfg.RagModelConfig.RagTopK
	if resolvedTopK <= 0 {
		resolvedTopK = defaultTopK
	}
	if topK > 0 {
		resolvedTopK = topK
	}
	if resolvedTopK < 1 {
		resolvedTopK = 1
	}
	if resolvedTopK > 20 {
		resolvedTopK = 20
	}

	return &RAGQuery{
		knowledgeBaseID: docs[0].KnowledgeBaseID,
		documents:       docs,
		embedding:       embedder,
		store:           NewVectorStore(),
		topK:            resolvedTopK,
	}, nil
}

func (r *RAGQuery) RetrieveDocuments(ctx context.Context, query string) ([]*schema.Document, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	all := make([]*schema.Document, 0, len(r.documents)*r.topK)
	var (
		mu      sync.Mutex
		lastErr error
	)

	var cachedVector []float64
	vectorRetriever, useVector := r.store.(vectorQueryRetriever)
	if useVector {
		vectors, err := r.embedding.EmbedStrings(ctx, []string{query})
		if err == nil && len(vectors) > 0 {
			cachedVector = vectors[0]
		} else if err != nil {
			return nil, fmt.Errorf("embed query failed: %w", err)
		}
	}

	maxWorkers := 12
	if len(r.documents) < maxWorkers {
		maxWorkers = len(r.documents)
	}
	if maxWorkers < 1 {
		maxWorkers = 1
	}
	sem := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup

	for _, ref := range r.documents {
		ref := ref
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			var (
				docs []*schema.Document
				err  error
			)
			if useVector && len(cachedVector) > 0 {
				docs, err = vectorRetriever.RetrieveByVector(ctx, ref.IndexName, cachedVector, r.topK)
			} else {
				docs, err = r.store.Retrieve(ctx, ref.IndexName, query, r.topK, r.embedding)
			}
			if err != nil {
				mu.Lock()
				lastErr = err
				mu.Unlock()
				return
			}

			local := make([]*schema.Document, 0, len(docs))
			for _, doc := range docs {
				if doc.MetaData == nil {
					doc.MetaData = map[string]any{}
				}
				doc.MetaData["kb_id"] = ref.KnowledgeBaseID
				doc.MetaData["file_name"] = ref.FileName
				doc.MetaData["index_name"] = ref.IndexName
				if _, ok := doc.MetaData["source"]; !ok {
					doc.MetaData["source"] = ref.FilePath
				}
				local = append(local, doc)
			}

			mu.Lock()
			all = append(all, local...)
			mu.Unlock()
		}()
	}
	wg.Wait()

	if len(all) == 0 && lastErr != nil {
		return nil, fmt.Errorf("failed to retrieve documents from %s: %w", r.store.Name(), lastErr)
	}

	cfg := config.GetConfig().RagModelConfig
	if cfg.RagHybridEnabled {
		applyHybridScores(query, all, cfg.RagHybridVecW, cfg.RagHybridLexW)
	}

	sort.SliceStable(all, func(i, j int) bool {
		return docRelevanceScore(all[i]) > docRelevanceScore(all[j])
	})
	if cfg.RagRerankEnabled {
		applyRerankScores(
			query,
			all,
			r.topK,
			cfg.RagRerankTopN,
			cfg.RagRerankBaseW,
			cfg.RagRerankLexW,
			cfg.RagRerankPosW,
		)
		sort.SliceStable(all, func(i, j int) bool {
			return docRelevanceScore(all[i]) > docRelevanceScore(all[j])
		})
	}

	if len(all) > r.topK {
		all = all[:r.topK]
	}
	return all, nil
}

func (r *RAGQuery) StoreName() string {
	return r.store.Name()
}

func (r *RAGQuery) TopK() int {
	return r.topK
}

func (r *RAGQuery) KnowledgeBaseID() string {
	return r.knowledgeBaseID
}

func BuildRAGPrompt(query string, docs []*schema.Document) string {
	if len(docs) == 0 {
		return query
	}

	var builder strings.Builder
	for i, doc := range docs {
		label := sourceLabel(doc.MetaData)
		builder.WriteString(fmt.Sprintf("[Document %d | source: %s]: %s\n\n", i+1, label, doc.Content))
	}

	return fmt.Sprintf(`Answer the user's question based on the following reference content.
If the references are insufficient, explicitly say you cannot find enough information.

References:
%s
User question: %s

Please provide a concise and accurate answer.`, builder.String(), query)
}

func newEmbedder(ctx context.Context, embeddingModel string) (embedding.Embedder, error) {
	cfg := config.GetConfig()
	baseURL := strings.TrimSpace(cfg.RagModelConfig.RagEmbeddingURL)
	if baseURL == "" {
		baseURL = strings.TrimSpace(cfg.RagModelConfig.RagBaseUrl)
	}
	apiKey := strings.TrimSpace(cfg.RagModelConfig.RagEmbeddingKey)
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("OPENAI_EMBEDDING_API_KEY"))
	}
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	}
	if apiKey == "" && isLocalEmbeddingURL(baseURL) {
		// Some local OpenAI-compatible servers (e.g. Ollama) do not require API key.
		apiKey = "local-embedding"
	}
	if apiKey == "" {
		return nil, fmt.Errorf("embedding api key is empty: set embeddingApiKey / OPENAI_EMBEDDING_API_KEY / OPENAI_API_KEY")
	}
	embedConfig := &embeddingArk.EmbeddingConfig{
		BaseURL: baseURL,
		APIKey:  apiKey,
		Model:   embeddingModel,
	}
	embedder, err := embeddingArk.NewEmbedder(ctx, embedConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create embedder: %w", err)
	}
	return embedder, nil
}

func isLocalEmbeddingURL(url string) bool {
	u := strings.ToLower(strings.TrimSpace(url))
	return strings.Contains(u, "127.0.0.1") || strings.Contains(u, "localhost")
}

func normalizeChunkConfig(size, overlap int) (int, int) {
	if size <= 0 {
		size = defaultChunkSize
	}
	if overlap < 0 {
		overlap = 0
	}
	if overlap >= size {
		overlap = size / 4
	}
	return size, overlap
}

func splitTextIntoChunks(text string, chunkSize, chunkOverlap int) []string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 0 {
		return nil
	}

	chunkSize, chunkOverlap = normalizeChunkConfig(chunkSize, chunkOverlap)
	step := chunkSize - chunkOverlap
	if step <= 0 {
		step = chunkSize
	}

	chunks := make([]string, 0, (len(runes)/step)+1)
	start := 0

	for start < len(runes) {
		end := start + chunkSize
		if end > len(runes) {
			end = len(runes)
		}

		split := findSplitPosition(runes, start, end)
		if split <= start {
			split = end
		}

		chunk := strings.TrimSpace(string(runes[start:split]))
		if chunk != "" {
			chunks = append(chunks, chunk)
		}

		if split >= len(runes) {
			break
		}

		next := split - chunkOverlap
		if next <= start {
			next = start + step
		}
		start = next
	}

	return chunks
}

func findSplitPosition(runes []rune, start, end int) int {
	if end >= len(runes) {
		return end
	}

	const window = 120
	min := start
	if end-window > min {
		min = end - window
	}

	for i := end - 1; i >= min; i-- {
		if isBoundaryRune(runes[i]) {
			return i + 1
		}
	}
	return end
}

func isBoundaryRune(r rune) bool {
	if unicode.IsSpace(r) {
		return true
	}
	switch r {
	case '.', ',', '!', '?', ';', ':', '，', '。', '；', '：', '！', '？':
		return true
	default:
		return false
	}
}

func sourceLabel(meta map[string]any) string {
	if meta == nil {
		return "-"
	}
	fileName := metaString(meta, "file_name")
	if fileName == "" {
		fileName = filepath.Base(metaString(meta, "source"))
	}
	if fileName == "" {
		fileName = "unknown"
	}

	chunkIndex := metaNumberString(meta, "chunk_index")
	chunkTotal := metaNumberString(meta, "chunk_total")
	if chunkIndex != "" && chunkTotal != "" {
		return fmt.Sprintf("%s chunk %s/%s", fileName, chunkIndex, chunkTotal)
	}
	return fileName
}

func docRelevanceScore(doc *schema.Document) float64 {
	if doc == nil || doc.MetaData == nil {
		return 0
	}
	if score, ok := metaFloat(doc.MetaData, "rerank_score"); ok {
		return score
	}
	if score, ok := metaFloat(doc.MetaData, "hybrid_score"); ok {
		return score
	}
	if score, ok := metaFloat(doc.MetaData, "score"); ok {
		return score
	}
	if distance, ok := metaFloat(doc.MetaData, "distance"); ok {
		return 1 - distance
	}
	return 0
}

func metaString(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	v, ok := meta[key]
	if !ok {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

func applyHybridScores(query string, docs []*schema.Document, vecWeight, lexWeight float64) {
	if len(docs) == 0 {
		return
	}
	vecWeight, lexWeight = normalizeHybridWeights(vecWeight, lexWeight)

	vectorRaw := make([]float64, len(docs))
	lexicalRaw := make([]float64, len(docs))
	for i, doc := range docs {
		vectorRaw[i] = rawVectorScore(doc)
		lexicalRaw[i] = lexicalMatchScore(query, doc)
	}

	vectorNorm := minMaxNormalize(vectorRaw)
	lexicalNorm := minMaxNormalize(lexicalRaw)
	for i, doc := range docs {
		if doc == nil {
			continue
		}
		if doc.MetaData == nil {
			doc.MetaData = map[string]any{}
		}

		hybrid := vecWeight*vectorNorm[i] + lexWeight*lexicalNorm[i]
		doc.MetaData["vector_score_raw"] = vectorRaw[i]
		doc.MetaData["vector_score"] = vectorNorm[i]
		doc.MetaData["lexical_score"] = lexicalNorm[i]
		doc.MetaData["hybrid_score"] = hybrid
		doc.MetaData["score"] = hybrid
	}
}

func applyRerankScores(query string, docs []*schema.Document, topK, rerankTopN int, baseWeight, lexWeight, posWeight float64) {
	if len(docs) == 0 {
		return
	}

	candidateN := resolveRerankTopN(len(docs), topK, rerankTopN)
	if candidateN <= 0 {
		return
	}
	baseWeight, lexWeight, posWeight = normalizeRerankWeights(baseWeight, lexWeight, posWeight)

	baseRaw := make([]float64, candidateN)
	lexRaw := make([]float64, candidateN)
	posRaw := make([]float64, candidateN)
	for i := 0; i < candidateN; i++ {
		doc := docs[i]
		baseRaw[i] = docRelevanceScore(doc)
		lexRaw[i] = rerankLexicalScore(query, doc)
		posRaw[i] = rerankPositionScore(doc)
	}

	baseNorm := minMaxNormalize(baseRaw)
	lexNorm := minMaxNormalize(lexRaw)
	posNorm := minMaxNormalize(posRaw)
	for i := 0; i < candidateN; i++ {
		doc := docs[i]
		if doc == nil {
			continue
		}
		if doc.MetaData == nil {
			doc.MetaData = map[string]any{}
		}

		rerank := baseWeight*baseNorm[i] + lexWeight*lexNorm[i] + posWeight*posNorm[i]
		doc.MetaData["rerank_base_score"] = baseNorm[i]
		doc.MetaData["rerank_lexical_score"] = lexNorm[i]
		doc.MetaData["rerank_position_score"] = posNorm[i]
		doc.MetaData["rerank_score"] = rerank
		doc.MetaData["score"] = rerank
	}

	// Restrict final topK selection to reranked candidate set.
	for i := candidateN; i < len(docs); i++ {
		doc := docs[i]
		if doc == nil {
			continue
		}
		if doc.MetaData == nil {
			doc.MetaData = map[string]any{}
		}
		doc.MetaData["rerank_score"] = -1.0
	}
}

func resolveRerankTopN(total, topK, configured int) int {
	if total <= 0 {
		return 0
	}
	n := configured
	if n <= 0 {
		n = topK * 4
	}
	if n < topK {
		n = topK
	}
	if n > total {
		n = total
	}
	return n
}

func normalizeRerankWeights(baseWeight, lexWeight, posWeight float64) (float64, float64, float64) {
	if baseWeight < 0 {
		baseWeight = 0
	}
	if lexWeight < 0 {
		lexWeight = 0
	}
	if posWeight < 0 {
		posWeight = 0
	}
	total := baseWeight + lexWeight + posWeight
	if total == 0 {
		return 0.65, 0.25, 0.10
	}
	return baseWeight / total, lexWeight / total, posWeight / total
}

func rerankLexicalScore(query string, doc *schema.Document) float64 {
	base := lexicalMatchScore(query, doc)
	if doc == nil {
		return base
	}
	queryNorm := normalizeForMatch(query)
	if queryNorm == "" {
		return base
	}

	contentNorm := normalizeForMatch(doc.Content + " " + metaString(doc.MetaData, "file_name"))
	if strings.Contains(contentNorm, queryNorm) {
		base += 0.2
	}
	if base > 1 {
		base = 1
	}
	return base
}

func rerankPositionScore(doc *schema.Document) float64 {
	if doc == nil || doc.MetaData == nil {
		return 0
	}
	chunkIndex, ok := metaFloat(doc.MetaData, "chunk_index")
	if !ok || chunkIndex <= 0 {
		return 0.5
	}
	return 1.0 / chunkIndex
}

func normalizeHybridWeights(vecWeight, lexWeight float64) (float64, float64) {
	if vecWeight < 0 {
		vecWeight = 0
	}
	if lexWeight < 0 {
		lexWeight = 0
	}
	if vecWeight == 0 && lexWeight == 0 {
		return 0.7, 0.3
	}
	total := vecWeight + lexWeight
	if total == 0 {
		return 0.7, 0.3
	}
	return vecWeight / total, lexWeight / total
}

func minMaxNormalize(values []float64) []float64 {
	out := make([]float64, len(values))
	if len(values) == 0 {
		return out
	}

	minV := values[0]
	maxV := values[0]
	for _, v := range values {
		if v < minV {
			minV = v
		}
		if v > maxV {
			maxV = v
		}
	}

	if maxV <= minV {
		for i := range out {
			out[i] = 1
		}
		return out
	}

	scale := maxV - minV
	for i, v := range values {
		out[i] = (v - minV) / scale
	}
	return out
}

func rawVectorScore(doc *schema.Document) float64 {
	if doc == nil || doc.MetaData == nil {
		return 0
	}
	if score, ok := metaFloat(doc.MetaData, "score"); ok {
		return score
	}
	if distance, ok := metaFloat(doc.MetaData, "distance"); ok {
		return 1 - distance
	}
	return 0
}

func lexicalMatchScore(query string, doc *schema.Document) float64 {
	if doc == nil {
		return 0
	}
	terms := buildLexicalTerms(query)
	if len(terms) == 0 {
		return 0
	}

	content := normalizeForMatch(doc.Content + " " + metaString(doc.MetaData, "file_name"))
	if content == "" {
		return 0
	}

	hit := 0
	for _, term := range terms {
		if strings.Contains(content, term) {
			hit++
		}
	}
	return float64(hit) / float64(len(terms))
}

func buildLexicalTerms(query string) []string {
	normalized := normalizeForMatch(query)
	if normalized == "" {
		return nil
	}

	termSet := make(map[string]struct{}, 16)
	parts := strings.Fields(normalized)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		addTerm(part, termSet)
		if containsHan(part) {
			for _, bg := range hanBigrams(part) {
				addTerm(bg, termSet)
			}
		}
	}

	out := make([]string, 0, len(termSet))
	for t := range termSet {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func addTerm(term string, set map[string]struct{}) {
	term = strings.TrimSpace(term)
	if term == "" {
		return
	}
	if len([]rune(term)) < 2 && !containsHan(term) {
		return
	}
	set[term] = struct{}{}
}

func normalizeForMatch(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(text)) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), unicode.Is(unicode.Han, r):
			b.WriteRune(r)
		case unicode.IsSpace(r):
			b.WriteRune(' ')
		default:
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func containsHan(text string) bool {
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func hanBigrams(text string) []string {
	runes := []rune(text)
	if len(runes) < 2 {
		return nil
	}
	out := make([]string, 0, len(runes)-1)
	for i := 0; i < len(runes)-1; i++ {
		if unicode.Is(unicode.Han, runes[i]) || unicode.Is(unicode.Han, runes[i+1]) {
			out = append(out, string(runes[i:i+2]))
		}
	}
	return out
}

func metaNumberString(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	v, ok := meta[key]
	if !ok {
		return ""
	}
	switch t := v.(type) {
	case int:
		return strconv.Itoa(t)
	case int32:
		return strconv.Itoa(int(t))
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatInt(int64(t), 10)
	case string:
		return strings.TrimSpace(t)
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

func metaFloat(meta map[string]any, key string) (float64, bool) {
	if meta == nil {
		return 0, false
	}
	v, ok := meta[key]
	if !ok {
		return 0, false
	}
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int32:
		return float64(t), true
	case int64:
		return float64(t), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}
