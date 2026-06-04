package suggest

import (
	"math"
	"sort"
	"strings"
	"sync"
)

type WordInfo struct {
	Word      string
	Frequency int64
	UserFreq  int64 // Passively learned user frequency
}

type Engine struct {
	mu          sync.RWMutex
	dictionary  map[string]*WordInfo
	deletes     map[string][]string
	maxDistance int
}

var qwertyCoords = map[rune][2]float64{
	'q': {0, 0}, 'w': {1, 0}, 'e': {2, 0}, 'r': {3, 0}, 't': {4, 0}, 'y': {5, 0}, 'u': {6, 0}, 'i': {7, 0}, 'o': {8, 0}, 'p': {9, 0},
	'a': {0.2, 1}, 's': {1.2, 1}, 'd': {2.2, 1}, 'f': {3.2, 1}, 'g': {4.2, 1}, 'h': {5.2, 1}, 'j': {6.2, 1}, 'k': {7.2, 1}, 'l': {8.2, 1},
	'z': {0.5, 2}, 'x': {1.5, 2}, 'c': {2.5, 2}, 'v': {3.5, 2}, 'b': {4.5, 2}, 'n': {5.5, 2}, 'm': {6.5, 2},
}

func NewEngine() *Engine {
	return &Engine{
		dictionary:  make(map[string]*WordInfo),
		deletes:     make(map[string][]string),
		maxDistance: 2,
	}
}

func isAlpha(s string) bool {
	for _, r := range s {
		if (r < 'a' || r > 'z') && r != '\'' && r != '-' {
			return false
		}
	}
	return true
}

func (e *Engine) AddWord(word string, freq int64) {
	word = strings.ToLower(word)
	e.mu.Lock()
	defer e.mu.Unlock()

	info, exists := e.dictionary[word]
	if exists {
		info.Frequency = freq
		return
	}

	info = &WordInfo{Word: word, Frequency: freq}
	e.dictionary[word] = info

	// Generate deletions for SymSpell
	deletes := e.getDeletes(word, e.maxDistance)
	for del := range deletes {
		e.deletes[del] = append(e.deletes[del], word)
	}
}

// UpdateUserFrequency boosts a word's score through passive learning
func (e *Engine) UpdateUserFrequency(word string, inc int64) {
	word = strings.ToLower(word)
	e.mu.Lock()
	defer e.mu.Unlock()

	info, exists := e.dictionary[word]
	if exists {
		info.UserFreq += inc
	} else {
		info = &WordInfo{Word: word, Frequency: 5, UserFreq: inc}
		e.dictionary[word] = info
		deletes := e.getDeletes(word, e.maxDistance)
		for del := range deletes {
			e.deletes[del] = append(e.deletes[del], word)
		}
	}
}

func (e *Engine) getDeletes(word string, maxDist int) map[string]bool {
	results := make(map[string]bool)
	var queue []string
	queue = append(queue, word)
	results[word] = true

	for d := 1; d <= maxDist; d++ {
		var nextQueue []string
		for _, q := range queue {
			if len(q) <= 1 {
				continue
			}
			for i := 0; i < len(q); i++ {
				del := q[:i] + q[i+1:]
				if !results[del] {
					results[del] = true
					nextQueue = append(nextQueue, del)
				}
			}
		}
		queue = nextQueue
	}
	return results
}

type Candidate struct {
	Word  string
	Score float64
}

// Suggest returns spell check and completions ranked by frequency and keyboard distance
func (e *Engine) Suggest(input string) []string {
	input = strings.TrimSpace(strings.ToLower(input))
	if len(input) == 0 {
		return nil
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	candidatesMap := make(map[string]float64)

	// 1. Exact match priority seeding
	if info, exists := e.dictionary[input]; exists {
		totalFreq := info.Frequency + info.UserFreq*5
		candidatesMap[input] = float64(totalFreq)
	}

	// 2. Generate input deletions for SymSpell matching
	inputDeletes := e.getDeletes(input, e.maxDistance)
	for del := range inputDeletes {
		words, matched := e.deletes[del]
		if matched {
			for _, w := range words {
				if _, exists := candidatesMap[w]; !exists {
					dist := e.editDistance(input, w)
					if dist <= float64(e.maxDistance) {
						info := e.dictionary[w]
						totalFreq := info.Frequency + info.UserFreq*5
						// Distance penalty multiplier (heavy exponential decay)
						penalty := 1.0
						if dist > 0 {
							penalty = 1.0 / math.Pow(250.0, dist)
						}

						// Calculate candidate score
						candidatesMap[w] = float64(totalFreq) * penalty
					}
				}
			}
		}
	}

	// 3. Prefix auto-completion search
	if len(input) >= 1 {
		for w, info := range e.dictionary {
			if strings.HasPrefix(w, input) && len(w) > len(input) {
				dist := len(w) - len(input)
				if dist <= 6 { // max completion gap (e.g. typog -> typography)
					totalFreq := info.Frequency + info.UserFreq*5
					// Linear prefix penalty to prioritize completions over typos
					score := float64(totalFreq) / (1.0 + float64(dist))
					if existingScore, exists := candidatesMap[w]; !exists || score > existingScore {
						candidatesMap[w] = score
					}
				}
			}
		}
	}

	// Sort candidates
	var sorted []Candidate
	for w, s := range candidatesMap {
		// Boost prefix matches/completions to rank above typo corrections
		if strings.HasPrefix(strings.ToLower(w), strings.ToLower(input)) {
			s += 1e9
		}
		sorted = append(sorted, Candidate{Word: w, Score: s})
	}

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Score > sorted[j].Score
	})

	var result []string
	for i := 0; i < len(sorted) && len(result) < 5; i++ {
		// Exclude suggestions that are exactly equal to the typed input
		if strings.EqualFold(sorted[i].Word, input) {
			continue
		}
		result = append(result, sorted[i].Word)
	}

	return result
}

func (e *Engine) IsValidWord(word string) bool {
	word = strings.ToLower(word)
	e.mu.RLock()
	defer e.mu.RUnlock()
	_, exists := e.dictionary[word]
	return exists
}

// BestCorrection returns the single best spell correction for input and its
// edit distance. Returns ("", 0, false) when the word is already valid, too
// short, or no candidate is found within maxDistance.
func (e *Engine) BestCorrection(input string) (word string, dist float64, found bool) {
	input = strings.TrimSpace(strings.ToLower(input))
	if len(input) < 3 {
		return "", 0, false
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	if _, exists := e.dictionary[input]; exists {
		return "", 0, false // already valid
	}

	type cand struct {
		word string
		dist float64
		freq int64
	}
	var best *cand

	for del := range e.getDeletes(input, e.maxDistance) {
		for _, w := range e.deletes[del] {
			d := e.editDistance(input, w)
			if d > float64(e.maxDistance) {
				continue
			}
			info := e.dictionary[w]
			freq := info.Frequency + info.UserFreq*5
			if best == nil || d < best.dist || (d == best.dist && freq > best.freq) {
				best = &cand{word: w, dist: d, freq: freq}
			}
		}
	}

	if best == nil {
		return "", 0, false
	}
	return best.word, best.dist, true
}

func (e *Engine) editDistance(s1, s2 string) float64 {
	r1, r2 := []rune(s1), []rune(s2)
	len1, len2 := len(r1), len(r2)

	// Create distance matrix
	matrix := make([][]float64, len1+1)
	for i := range matrix {
		matrix[i] = make([]float64, len2+1)
		matrix[i][0] = float64(i)
	}
	for j := 0; j <= len2; j++ {
		matrix[0][j] = float64(j)
	}

	for i := 1; i <= len1; i++ {
		for j := 1; j <= len2; j++ {
			if r1[i-1] == r2[j-1] {
				matrix[i][j] = matrix[i-1][j-1]
			} else {
				subCost := 1.0
				// QWERTY keyboard distance optimization
				if e.isKeyboardAdjacent(r1[i-1], r2[j-1]) {
					subCost = 0.5
				}
				matrix[i][j] = minFloat(
					matrix[i-1][j]+1.0,       // Deletion
					matrix[i][j-1]+1.0,       // Insertion
					matrix[i-1][j-1]+subCost, // Substitution
				)
			}
		}
	}
	return matrix[len1][len2]
}

func (e *Engine) isKeyboardAdjacent(r1, r2 rune) bool {
	c1, ok1 := qwertyCoords[r1]
	c2, ok2 := qwertyCoords[r2]
	if !ok1 || !ok2 {
		return false
	}
	dx := c1[0] - c2[0]
	dy := c1[1] - c2[1]
	dist := math.Sqrt(dx*dx + dy*dy)
	return dist <= 1.5
}

func minFloat(a, b, c float64) float64 {
	if a < b && a < c {
		return a
	}
	if b < c {
		return b
	}
	return c
}
