//go:build onnx

package suggest

// OnnxPredictor is the real ONNX-backed predictor compiled only when
// the "onnx" build tag is set: go build -tags onnx .
//
// Prerequisites:
//   1. Install ONNX Runtime shared library:
//      wget https://github.com/microsoft/onnxruntime/releases/download/v1.17.3/onnxruntime-linux-x64-1.17.3.tgz
//      tar -xf onnxruntime-linux-x64-1.17.3.tgz
//      sudo cp onnxruntime-linux-x64-1.17.3/lib/libonnxruntime.so* /usr/local/lib/
//      sudo ldconfig
//
//   2. Add Go dep (once):
//      go get github.com/yalue/onnxruntime_go
//      go get github.com/sugarme/tokenizer
//
//   3. Download the model + vocab:
//      go run ./tools/setup_onnx/

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	ort "github.com/yalue/onnxruntime_go"
)

// OnnxPredictor uses a quantized DistilGPT-2 ONNX model for next-word
// prediction. It tokenizes input using GPT-2 BPE, runs inference, and
// returns the top-5 most probable next words (whole words only, not subwords).
type OnnxPredictor struct {
	session    *ort.DynamicAdvancedSession
	vocab      map[string]int32 // token string → id
	revVocab   map[int32]string // id → token string
	vocabSize  int
	builtin    *BigramEngine
	maxContext int // max tokens to pass to model (keep last N)
}

// NewOnnxPredictor loads the ONNX model and GPT-2 vocabulary.
func NewOnnxPredictor(modelPath, vocabPath string) (*OnnxPredictor, error) {
	// Initialize ONNX Runtime environment
	// Look for libonnxruntime.so in the executable's directory first, then current working directory
	libPath := "libonnxruntime.so"
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		p := filepath.Join(exeDir, "libonnxruntime.so")
		if _, err := os.Stat(p); err == nil {
			libPath = p
		}
	}
	if _, err := os.Stat(libPath); err != nil {
		if _, err := os.Stat("./libonnxruntime.so"); err == nil {
			libPath = "./libonnxruntime.so"
		}
	}
	ort.SetSharedLibraryPath(libPath)
	if err := ort.InitializeEnvironment(); err != nil {
		return nil, fmt.Errorf("onnxruntime init: %w", err)
	}

	// Load vocabulary
	vocab, revVocab, err := loadGPT2Vocab(vocabPath)
	if err != nil {
		return nil, fmt.Errorf("load vocab: %w", err)
	}

	// Create dynamic session (variable sequence length)
	session, err := ort.NewDynamicAdvancedSession(
		modelPath,
		[]string{"input_ids", "attention_mask"},
		[]string{"logits"},
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("create onnx session: %w", err)
	}

	log.Printf("[OnnxPredictor] Loaded model from %s (vocab size=%d)", modelPath, len(vocab))
	return &OnnxPredictor{
		session:    session,
		vocab:      vocab,
		revVocab:   revVocab,
		vocabSize:  len(vocab),
		builtin:    NewBigramEngine(),
		maxContext: 64,
	}, nil
}

// PredictNextContext encodes the word context, runs GPT-2 inference, and
// returns the top-5 whole-word continuations.
func (p *OnnxPredictor) PredictNextContext(words []string) []string {
	if len(words) == 0 {
		return p.builtin.Starters()
	}

	// Build input text: join last N words (space-prefixed for GPT-2)
	contextWords := words
	if len(contextWords) > 8 {
		contextWords = contextWords[len(contextWords)-8:]
	}
	text := strings.Join(contextWords, " ")

	// Tokenize using simple whitespace+vocab lookup (GPT-2 BPE simplified)
	inputIDs, err := p.tokenize(text)
	if err != nil || len(inputIDs) == 0 {
		return p.builtin.PredictNextContext(words)
	}

	// Clamp to max context
	if len(inputIDs) > p.maxContext {
		inputIDs = inputIDs[len(inputIDs)-p.maxContext:]
	}

	seqLen := int64(len(inputIDs))

	// Convert inputIDs to int64 for the ONNX model
	inputIDs64 := make([]int64, len(inputIDs))
	for i, id := range inputIDs {
		inputIDs64[i] = int64(id)
	}

	// Create input tensor [1, seqLen]
	inputTensor, err := ort.NewTensor(
		ort.NewShape(1, seqLen),
		inputIDs64,
	)
	if err != nil {
		return p.builtin.PredictNextContext(words)
	}
	defer inputTensor.Destroy()

	// Create attention mask tensor [1, seqLen] containing all 1s
	attentionMask := make([]int64, seqLen)
	for i := range attentionMask {
		attentionMask[i] = 1
	}
	maskTensor, err := ort.NewTensor(
		ort.NewShape(1, seqLen),
		attentionMask,
	)
	if err != nil {
		return p.builtin.PredictNextContext(words)
	}
	defer maskTensor.Destroy()

	// Output: [1, seqLen, vocabSize]
	outputTensor, err := ort.NewEmptyTensor[float32](
		ort.NewShape(1, seqLen, int64(p.vocabSize)),
	)
	if err != nil {
		return p.builtin.PredictNextContext(words)
	}
	defer outputTensor.Destroy()

	// Run inference
	if err := p.session.Run(
		[]ort.ArbitraryTensor{inputTensor, maskTensor},
		[]ort.ArbitraryTensor{outputTensor},
	); err != nil {
		log.Printf("[OnnxPredictor] inference error: %v", err)
		return p.builtin.PredictNextContext(words)
	}

	// Extract logits for the LAST token position: [vocabSize]
	data := outputTensor.GetData()
	offset := int((seqLen - 1) * int64(p.vocabSize))
	if offset+p.vocabSize > len(data) {
		return p.builtin.PredictNextContext(words)
	}
	logits := data[offset : offset+p.vocabSize]

	lastWord := strings.ToLower(strings.TrimSpace(words[len(words)-1]))
	isSentenceEnd := strings.HasSuffix(lastWord, ".") || strings.HasSuffix(lastWord, "?") || strings.HasSuffix(lastWord, "!")

	return p.topKWords(logits, 5, isSentenceEnd)
}

// tokenize converts text to GPT-2 token IDs using a simplified BPE approach.
// Each word is looked up with and without a leading space (Ġ prefix in GPT-2).
func (p *OnnxPredictor) tokenize(text string) ([]int32, error) {
	words := strings.Fields(text)
	var ids []int32
	for i, w := range words {
		key := w
		if i > 0 {
			key = "Ġ" + w // GPT-2 encodes spaces as Ġ (U+0120)
		}
		if id, ok := p.vocab[key]; ok {
			ids = append(ids, id)
		} else {
			// Character-level fallback for unknown tokens
			for _, ch := range w {
				chKey := string(ch)
				if i > 0 && len(ids) == 0 {
					chKey = "Ġ" + chKey
				}
				if id, ok := p.vocab[chKey]; ok {
					ids = append(ids, id)
				}
			}
		}
	}
	return ids, nil
}

// topKWords picks the top-k token IDs by logit, filters for whole-word tokens
// (those starting with Ġ in GPT-2), and converts to lowercase word strings.
func (p *OnnxPredictor) topKWords(logits []float32, k int, blendStarters bool) []string {
	type entry struct {
		id    int
		score float32
	}

	// Softmax-normalised top-100 candidates
	candidates := make([]entry, 0, 100)

	// Find max logit for numerical stability
	maxL := float32(math.Inf(-1))
	for _, l := range logits {
		if l > maxL {
			maxL = l
		}
	}

	var sumExp float64
	exps := make([]float64, len(logits))
	for i, l := range logits {
		e := math.Exp(float64(l - maxL))
		exps[i] = e
		sumExp += e
	}

	for i, e := range exps {
		candidates = append(candidates, entry{i, float32(e / sumExp)})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	seen := make(map[string]bool)
	var result []string

	for _, c := range candidates {
		if len(result) >= k {
			break
		}
		tok, ok := p.revVocab[int32(c.id)]
		if !ok {
			continue
		}
		// GPT-2 uses Ġ (U+0120) to mark tokens that start a new word
		if !strings.HasPrefix(tok, "Ġ") {
			continue
		}
		word := strings.ToLower(strings.TrimPrefix(tok, "Ġ"))
		if len(word) < 2 || seen[word] {
			continue
		}
		seen[word] = true
		result = append(result, word)
	}

	// Blend in builtin starters if we didn't get enough and we are starting a new sentence
	if len(result) < k && blendStarters {
		for _, w := range p.builtin.Starters() {
			if !seen[w] {
				result = append(result, w)
				seen[w] = true
			}
			if len(result) >= k {
				break
			}
		}
	}
	return result
}

func (p *OnnxPredictor) Starters() []string { return p.builtin.Starters() }

func (p *OnnxPredictor) Close() error {
	if p.session != nil {
		p.session.Destroy()
	}
	return nil
}

// loadGPT2Vocab reads vocab.json and builds bidirectional maps.
// vocab.json format: {"token": id, ...}
func loadGPT2Vocab(path string) (map[string]int32, map[int32]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	raw := make(map[string]int32)
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("parse vocab.json: %w", err)
	}
	rev := make(map[int32]string, len(raw))
	for tok, id := range raw {
		rev[id] = tok
	}
	return raw, rev, nil
}
