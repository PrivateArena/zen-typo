package suggest

import (
	"sort"
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
		"nice":    {"to", "meeting", "job"},
		"to":      {"meet", "be", "do", "hearing", "see", "the", "you"},
		"meet":    {"you"},
		"thank":   {"you"},
		"you":     {"are", "can", "will", "do", "have", "know", "think", "very"},
		"very":    {"much"},
		"look":    {"forward", "at", "for", "like", "up"},
		"forward": {"to"},
		"how":     {"are", "about", "do", "to", "many"},
		"are":     {"you"},
		"let":     {"me", "us", "go", "him", "her"},
		"me":      {"know"},
		"know":    {"if"},
		"would":   {"be", "like", "you", "love", "have"},
		"like":    {"to"},
		"dont":    {"know", "think", "have", "want", "like"},
		"feel":    {"free", "like", "good", "better"},
		"free":    {"to"},
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

	getTopNext := func(w string) []bigramEntry {
		cands := make(map[string]int64)
		if nexts, exists := b.transitions[w]; exists {
			for next, weight := range nexts {
				cands[next] = weight
			}
		}
		if nexts, exists := b.userTransitions[w]; exists {
			for next, weight := range nexts {
				cands[next] += weight * 10
			}
		}
		var sorted []bigramEntry
		for next, weight := range cands {
			sorted = append(sorted, bigramEntry{word: next, weight: weight})
		}
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].weight > sorted[j].weight
		})
		return sorted
	}

	primary := getTopNext(lastWord)
	if len(primary) == 0 {
		return nil
	}

	var suggestions []string
	seen := make(map[string]bool)

	addSuggestion := func(s string) {
		if !seen[s] && len(suggestions) < 5 {
			seen[s] = true
			suggestions = append(suggestions, s)
		}
	}

	for _, p := range primary {
		addSuggestion(p.word)

		secondLevel := getTopNext(p.word)
		if len(secondLevel) > 0 {
			next2 := secondLevel[0].word
			phrase2 := p.word + " " + next2
			addSuggestion(phrase2)

			thirdLevel := getTopNext(next2)
			if len(thirdLevel) > 0 {
				next3 := thirdLevel[0].word
				phrase3 := phrase2 + " " + next3
				addSuggestion(phrase3)
			}
		}
	}

	for _, p := range primary {
		addSuggestion(p.word)
	}

	return suggestions
}

type bigramEntry struct {
	word   string
	weight int64
}
