package suggest

import (
	"strings"
	"sync"
)

type BigramEngine struct {
	mu           sync.RWMutex
	transitions  map[string]map[string]int64
	userTransitions map[string]map[string]int64
}

func NewBigramEngine() *BigramEngine {
	b := &BigramEngine{
		transitions:     make(map[string]map[string]int64),
		userTransitions: make(map[string]map[string]int64),
	}
	b.loadCommonBigrams()
	return b
}

func (b *BigramEngine) loadCommonBigrams() {
	common := map[string][]string{
		"how":    {"are", "about", "do", "to", "many"},
		"what":   {"is", "are", "do", "about", "if", "you"},
		"thank":  {"you", "fully", "s"},
		"you":    {"are", "can", "will", "do", "have", "know", "think"},
		"i":      {"would", "am", "think", "want", "have", "can", "know", "will"},
		"it":     {"is", "was", "would", "seems", "has", "can"},
		"this":   {"is", "was", "will", "has", "means", "way"},
		"that":   {"is", "was", "you", "they", "we", "would"},
		"we":     {"can", "will", "are", "have", "need", "do", "should"},
		"they":   {"are", "will", "can", "have", "do", "need"},
		"nice":   {"to", "meeting", "job"},
		"good":   {"morning", "afternoon", "evening", "job", "idea", "luck"},
		"please": {"let", "find", "send", "check", "help", "do"},
		"would":  {"be", "like", "you", "love", "have"},
		"could":  {"you", "be", "have", "not", "do"},
		"should": {"be", "we", "have", "you", "do"},
		"let":    {"me", "us", "go", "him", "her"},
		"look":   {"forward", "at", "for", "like", "up"},
		"feel":   {"free", "like", "good", "better"},
		"do":     {"you", "not", "it", "have", "so"},
		"dont":   {"know", "think", "have", "want", "like"},
		"can":    {"you", "be", "do", "have", "not", "see"},
	}

	for word, nexts := range common {
		b.transitions[word] = make(map[string]int64)
		weight := int64(100)
		for _, next := range nexts {
			b.transitions[word][next] = weight
			weight -= 15
		}
	}
}

func (b *BigramEngine) AddUserTransition(word, next string, weight int64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	word = strings.ToLower(word)
	next = strings.ToLower(next)

	if _, exists := b.userTransitions[word]; !exists {
		b.userTransitions[word] = make(map[string]int64)
	}
	b.userTransitions[word][next] += weight
}

func (b *BigramEngine) PredictNext(lastWord string) []string {
	lastWord = strings.TrimSpace(strings.ToLower(lastWord))
	if len(lastWord) == 0 {
		return nil
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	candidates := make(map[string]int64)

	// Add built-in bigrams
	if nexts, exists := b.transitions[lastWord]; exists {
		for next, weight := range nexts {
			candidates[next] = weight
		}
	}

	// Add user-learned transitions with higher weight multiplier
	if nexts, exists := b.userTransitions[lastWord]; exists {
		for next, weight := range nexts {
			candidates[next] += weight * 10
		}
	}

	// Sort by weight
	var sorted []bigramEntry
	for next, weight := range candidates {
		sorted = append(sorted, bigramEntry{word: next, weight: weight})
	}

	sortSlice(sorted)

	var result []string
	for i := 0; i < len(sorted) && i < 5; i++ {
		result = append(result, sorted[i].word)
	}

	return result
}

type bigramEntry struct {
	word   string
	weight int64
}

func sortSlice(slice []bigramEntry) {
	// Standard bubble-sort or simple selection sort to keep it dependency-free and easy
	for i := 0; i < len(slice); i++ {
		for j := i + 1; j < len(slice); j++ {
			if slice[i].weight < slice[j].weight {
				slice[i], slice[j] = slice[j], slice[i]
			}
		}
	}
}
