package suggest

import (
	"sort"
	"strings"
	"sync"
)

// BigramEngine stores bigram and trigram transition data for next-word prediction.
// It supports both seed (built-in) and user-learned transitions.
type BigramEngine struct {
	mu sync.RWMutex

	// bigrams: word → {next: weight}
	transitions     map[string]map[string]int64
	userTransitions map[string]map[string]int64

	// trigrams: "word1 word2" → {next: weight}
	trigramSeeds    map[string]map[string]int64

	// starters: common sentence-starting words, shown on empty input
	starters []string
}

func NewBigramEngine() *BigramEngine {
	b := &BigramEngine{
		transitions:     make(map[string]map[string]int64),
		userTransitions: make(map[string]map[string]int64),
		trigramSeeds:    make(map[string]map[string]int64),
	}
	b.loadCommonBigrams()
	b.loadCommonTrigrams()
	b.starters = []string{
		"i", "the", "please", "thank", "could", "would", "let", "just", "it", "we",
		"can", "what", "how", "if", "i'm", "good", "great",
	}
	return b
}

func addBigram(m map[string]map[string]int64, word, next string, weight int64) {
	if m[word] == nil {
		m[word] = make(map[string]int64)
	}
	if m[word][next] < weight {
		m[word][next] = weight
	}
}

func addTrigram(m map[string]map[string]int64, w1, w2, next string, weight int64) {
	key := w1 + " " + w2
	if m[key] == nil {
		m[key] = make(map[string]int64)
	}
	if m[key][next] < weight {
		m[key][next] = weight
	}
}

func (b *BigramEngine) loadCommonBigrams() {
	type pair struct{ w, n string }
	pairs := []struct {
		word  string
		nexts []string
	}{
		// ── Pronouns ──────────────────────────────────────────────────────────
		{"i", []string{"would", "am", "think", "want", "have", "can", "know", "will", "need", "feel", "believe", "hope", "appreciate", "understand", "agree"}},
		{"i'm", []string{"not", "sorry", "happy", "glad", "sure", "available", "working", "looking", "interested", "ready"}},
		{"i'll", []string{"be", "have", "get", "make", "send", "check", "look", "try", "let", "do"}},
		{"i've", []string{"been", "got", "seen", "done", "sent", "checked", "found", "noticed", "had", "heard"}},
		{"you", []string{"are", "can", "will", "do", "have", "know", "think", "need", "should", "want", "might"}},
		{"we", []string{"can", "will", "are", "have", "need", "do", "should", "could", "would", "want", "appreciate"}},
		{"they", []string{"are", "will", "can", "have", "do", "need", "should", "want", "might", "were"}},
		{"it", []string{"is", "was", "would", "seems", "has", "can", "will", "looks", "appears", "should", "might"}},
		{"this", []string{"is", "was", "will", "has", "means", "way", "could", "should", "looks", "helps", "might"}},
		{"that", []string{"is", "was", "you", "they", "we", "would", "could", "might", "will", "means", "should"}},

		// ── Modal verbs ───────────────────────────────────────────────────────
		{"would", []string{"be", "like", "you", "love", "have", "appreciate", "prefer", "suggest", "recommend", "help"}},
		{"could", []string{"you", "be", "have", "not", "do", "please", "possibly", "help", "check", "look"}},
		{"should", []string{"be", "we", "have", "you", "do", "not", "also", "probably", "definitely", "consider"}},
		{"can", []string{"you", "be", "do", "have", "not", "see", "also", "help", "check", "please"}},
		{"will", []string{"be", "have", "do", "not", "also", "send", "provide", "let", "need", "make"}},
		{"might", []string{"be", "have", "not", "want", "need", "also", "work", "help", "take", "consider"}},

		// ── Common verbs ──────────────────────────────────────────────────────
		{"please", []string{"let", "find", "send", "check", "help", "do", "note", "see", "confirm", "provide", "advise", "feel", "review"}},
		{"thank", []string{"you", "fully"}},
		{"thanks", []string{"for", "again", "a", "in", "to", "so", "very"}},
		{"let", []string{"me", "us", "go", "him", "her", "know", "them"}},
		{"look", []string{"forward", "at", "for", "like", "up", "into", "through", "at"}},
		{"feel", []string{"free", "like", "good", "better", "comfortable", "confident"}},
		{"do", []string{"you", "not", "it", "have", "so", "let", "need", "think", "want", "know"}},
		{"don't", []string{"know", "think", "have", "want", "like", "worry", "forget", "hesitate", "need"}},
		{"know", []string{"that", "what", "how", "if", "when", "where", "why", "you", "the"}},
		{"think", []string{"that", "about", "it", "we", "you", "this", "of", "the", "so"}},
		{"get", []string{"back", "to", "the", "a", "your", "in", "it", "done", "this", "an"}},
		{"make", []string{"sure", "it", "the", "a", "this", "that", "an", "sense", "your"}},
		{"see", []string{"you", "the", "if", "that", "what", "how", "it", "this"}},
		{"use", []string{"the", "a", "this", "it", "your", "our", "an", "these"}},
		{"take", []string{"a", "the", "some", "this", "care", "time", "note", "place"}},
		{"keep", []string{"in", "the", "a", "up", "me", "us", "this", "that", "going"}},
		{"send", []string{"you", "me", "the", "a", "it", "them", "this", "an", "your"}},
		{"need", []string{"to", "a", "the", "your", "some", "more", "help", "this", "that", "it"}},
		{"want", []string{"to", "a", "the", "you", "more", "this", "some", "that", "your"}},
		{"work", []string{"on", "with", "for", "together", "out", "well", "in", "as", "at"}},
		{"provide", []string{"the", "a", "more", "an", "your", "some", "feedback", "details", "information"}},
		{"share", []string{"the", "a", "your", "this", "some", "it", "more", "that"}},
		{"check", []string{"the", "if", "your", "out", "this", "that", "it", "in", "on"}},
		{"follow", []string{"up", "the", "this", "that", "your", "our", "through"}},
		{"find", []string{"the", "a", "out", "it", "your", "that", "this", "some", "an"}},
		{"help", []string{"you", "me", "us", "with", "the", "to", "this", "that", "them"}},
		{"update", []string{"you", "the", "me", "us", "your", "this", "on", "it", "our"}},
		{"review", []string{"the", "your", "this", "that", "it", "our", "an", "these"}},
		{"discuss", []string{"the", "this", "that", "it", "further", "with", "our", "your"}},
		{"schedule", []string{"a", "the", "this", "that", "an", "some", "our", "your"}},
		{"confirm", []string{"the", "that", "your", "this", "it", "receipt", "whether", "if"}},
		{"reach", []string{"out", "you", "me", "us", "the", "to", "back"}},

		// ── Greetings / courtesy ───────────────────────────────────────────────
		{"good", []string{"morning", "afternoon", "evening", "job", "idea", "luck", "news", "point", "question", "to", "day", "time"}},
		{"nice", []string{"to", "meeting", "job", "work", "try"}},
		{"great", []string{"job", "work", "idea", "question", "point", "to", "thank", "news"}},
		{"hello", []string{"there", "everyone", "all", "team", "world"}},
		{"hi", []string{"there", "everyone", "all", "team"}},
		{"dear", []string{"all", "team", "sir", "madam", "mr", "ms", "dr"}},
		{"best", []string{"regards", "wishes", "of", "practice", "option", "approach"}},
		{"kind", []string{"regards", "of"}},
		{"sincerely", []string{"yours", "apologize"}},
		{"regards", []string{"to", "from"}},

		// ── Connectors / discourse ──────────────────────────────────────────────
		{"how", []string{"are", "about", "do", "to", "many", "can", "is", "this", "we", "would", "does"}},
		{"what", []string{"is", "are", "do", "about", "if", "you", "was", "the", "would", "can", "does", "should"}},
		{"when", []string{"you", "we", "the", "is", "are", "will", "do", "can", "it", "would", "does"}},
		{"where", []string{"you", "we", "the", "is", "are", "do", "can", "would", "should"}},
		{"why", []string{"is", "are", "do", "you", "we", "the", "would", "can", "not", "should"}},
		{"if", []string{"you", "we", "the", "it", "there", "this", "that", "possible", "not", "needed", "any", "so"}},
		{"as", []string{"a", "the", "soon", "well", "much", "many", "per", "of", "an", "discussed"}},
		{"in", []string{"the", "a", "this", "order", "addition", "case", "terms", "fact", "general", "particular", "response"}},
		{"on", []string{"the", "a", "this", "that", "behalf", "time", "your", "top"}},
		{"at", []string{"the", "a", "this", "that", "your", "our", "least"}},
		{"for", []string{"the", "a", "your", "this", "that", "more", "further", "any", "now", "example"}},
		{"with", []string{"the", "a", "your", "this", "that", "our", "me", "us", "him", "her", "them"}},
		{"and", []string{"the", "a", "i", "we", "you", "it", "this", "that", "also", "then", "so"}},
		{"but", []string{"i", "we", "you", "it", "the", "this", "that", "also", "not", "if", "please"}},
		{"however", []string{"i", "we", "you", "the", "this", "that", "it", "there", "please"}},
		{"also", []string{"i", "the", "a", "you", "we", "please", "note", "want", "need"}},
		{"just", []string{"want", "need", "checking", "wanted", "a", "to", "let", "following", "wondering", "reached"}},
		{"still", []string{"waiting", "looking", "working", "need", "have", "want", "trying", "in", "not"}},
		{"already", []string{"have", "been", "done", "sent", "checked", "received", "completed", "started"}},
		{"yet", []string{"to", "another", "not", "again", "a"}},
		{"now", []string{"that", "i", "we", "you", "the", "a", "is", "are", "it", "to", "let", "please"}},
		{"then", []string{"we", "i", "you", "the", "it", "please", "let", "that", "can"}},
		{"so", []string{"that", "we", "i", "you", "the", "it", "please", "what", "far", "much"}},
		{"therefore", []string{"i", "we", "the", "it", "you", "this", "please"}},
		{"furthermore", []string{"i", "we", "the", "it", "you", "this", "please"}},
		{"meanwhile", []string{"i", "we", "the", "you", "it", "please"}},
		{"additionally", []string{"i", "we", "the", "it", "you", "please"}},
		{"otherwise", []string{"i", "we", "the", "it", "you", "please", "this", "we'll"}},
		{"regarding", []string{"the", "your", "this", "that", "our", "a", "an"}},

		// ── Business / professional phrases ──────────────────────────────────
		{"looking", []string{"forward", "at", "for", "into", "to", "good"}},
		{"forward", []string{"to", "this", "that"}},
		{"soon", []string{"as", "possible", "the", "a", "to", "this"}},
		{"possible", []string{"to", "solution", "option", "date", "time"}},
		{"possible", []string{"to", "for", "way"}},
		{"issue", []string{"with", "is", "has", "in", "that"}},
		{"matter", []string{"at", "of", "how", "with", "that"}},
		{"team", []string{"to", "will", "can", "has", "is", "that", "should"}},
		{"meeting", []string{"to", "on", "at", "in", "will", "with", "that", "scheduled"}},
		{"update", []string{"on", "about", "you", "me", "the", "this", "our", "your"}},
		{"feedback", []string{"on", "about", "from", "is", "would", "for", "your"}},
		{"progress", []string{"on", "update", "report", "has", "is", "made", "so", "of"}},
		{"attached", []string{"please", "is", "are", "herewith", "to"}},
		{"attached", []string{"please", "find", "is", "are"}},
		{"below", []string{"is", "are", "please", "the", "you", "i"}},
		{"above", []string{"is", "are", "please", "the", "mentioned", "all"}},
		{"kindly", []string{"please", "find", "note", "advise", "let", "confirm", "review", "share"}},
		{"urgent", []string{"matter", "issue", "request", "please", "action", "attention", "basis"}},
		{"priority", []string{"to", "of", "one", "this", "is"}},
		{"deadline", []string{"is", "for", "on", "of", "to", "this"}},
		{"asap", []string{"please", "to", "the", "i", "we"}},
	}

	for _, p := range pairs {
		b.transitions[p.word] = make(map[string]int64)
		weight := int64(200)
		for _, next := range p.nexts {
			b.transitions[p.word][next] = weight
			weight -= 10
			if weight < 20 {
				weight = 20
			}
		}
	}
}

func (b *BigramEngine) loadCommonTrigrams() {
	type trigram struct {
		w1, w2, next string
		weight       int64
	}
	seeds := []trigram{
		// "looking forward to" — primary phrase
		{"looking", "forward", "to", 500},
		{"looking", "forward", "hearing", 300},
		{"looking", "forward", "working", 300},
		{"looking", "forward", "meeting", 300},
		// "as soon as"
		{"as", "soon", "as", 500},
		{"as", "soon", "possible", 300},
		{"as", "soon", "the", 200},
		// "let me know" — primary
		{"let", "me", "know", 500},
		{"let", "me", "check", 300},
		{"let", "me", "look", 300},
		{"let", "me", "see", 300},
		{"let", "me", "try", 300},
		// "please let me"
		{"please", "let", "me", 300},
		{"please", "find", "attached", 300},
		{"please", "feel", "free", 300},
		{"please", "let", "know", 300},
		// "in order to"
		{"in", "order", "to", 300},
		{"in", "addition", "to", 300},
		{"in", "case", "you", 300},
		{"in", "terms", "of", 300},
		{"in", "fact", "the", 300},
		{"in", "general", "the", 300},
		{"in", "particular", "the", 300},
		{"in", "response", "to", 300},
		// "as well as"
		{"as", "well", "as", 300},
		{"as", "per", "our", 300},
		{"as", "per", "the", 300},
		{"as", "discussed", "in", 300},
		{"as", "mentioned", "in", 300},
		{"as", "mentioned", "above", 300},
		// "thank you for"
		{"thank", "you", "for", 500},
		{"thank", "you", "so", 300},
		{"thank", "you", "very", 300},
		// "I would like"
		{"i", "would", "like", 500},
		{"i", "would", "appreciate", 300},
		{"i", "would", "suggest", 300},
		{"i", "would", "recommend", 300},
		{"i", "would", "love", 300},
		{"i", "am", "writing", 400},
		{"i", "am", "available", 300},
		{"i", "am", "happy", 300},
		{"i", "am", "glad", 300},
		{"i", "am", "sorry", 300},
		{"i", "will", "be", 400},
		{"i", "will", "send", 300},
		{"i", "will", "check", 300},
		{"i", "will", "let", 300},
		{"i", "will", "follow", 300},
		{"i", "have", "sent", 300},
		{"i", "have", "checked", 300},
		{"i", "have", "been", 400},
		{"i", "have", "received", 300},
		{"i", "have", "reviewed", 300},
		// "we would like"
		{"we", "would", "like", 500},
		{"we", "would", "appreciate", 300},
		{"we", "would", "be", 300},
		{"we", "are", "happy", 300},
		{"we", "are", "glad", 300},
		{"we", "are", "looking", 300},
		{"we", "are", "pleased", 300},
		{"we", "can", "help", 300},
		{"we", "can", "discuss", 300},
		{"we", "can", "schedule", 300},
		{"we", "need", "to", 300},
		// "could you please"
		{"could", "you", "please", 500},
		{"could", "you", "kindly", 300},
		{"could", "you", "check", 300},
		{"could", "you", "review", 300},
		{"could", "you", "send", 300},
		{"could", "you", "confirm", 300},
		// "would you like"
		{"would", "you", "like", 500},
		{"would", "you", "be", 300},
		{"would", "you", "mind", 300},
		{"would", "you", "prefer", 300},
		// "hope you are"
		{"hope", "you", "are", 300},
		{"hope", "you", "have", 300},
		// "feel free to"
		{"feel", "free", "to", 300},
		// "do not hesitate"
		{"do", "not", "hesitate", 300},
		{"do", "not", "forget", 300},
		{"do", "not", "worry", 300},
		// "at your earliest"
		{"at", "your", "earliest", 300},
		{"at", "your", "convenience", 300},
		// "on behalf of"
		{"on", "behalf", "of", 300},
		{"on", "top", "of", 300},
		// "for your reference"
		{"for", "your", "reference", 300},
		{"for", "your", "information", 300},
		{"for", "your", "review", 300},
		{"for", "further", "information", 300},
		{"for", "further", "assistance", 300},
		// "just wanted to"
		{"just", "wanted", "to", 300},
		{"just", "checking", "in", 300},
		{"just", "following", "up", 300},
		{"just", "reached", "out", 300},
		// "good morning everyone"
		{"good", "morning", "everyone", 300},
		{"good", "morning", "team", 300},
		{"good", "afternoon", "everyone", 300},
		{"good", "afternoon", "team", 300},
		// "nice to meet"
		{"nice", "to", "meet", 500},
		{"nice", "to", "see", 300},
		{"nice", "to", "hear", 300},
		// "great to hear"
		{"great", "to", "hear", 400},
		{"great", "to", "see", 300},
		{"great", "to", "meet", 300},
		{"great", "job", "on", 300},
		{"great", "work", "on", 300},
	}

	for _, t := range seeds {
		addTrigram(b.trigramSeeds, t.w1, t.w2, t.next, t.weight)
	}
}

// AddUserTransition records a user-learned bigram transition.
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

// PredictNext predicts the next word given the last word only (compat shim).
func (b *BigramEngine) PredictNext(lastWord string) []string {
	return b.PredictNextContext([]string{lastWord})
}

// PredictNextContext predicts the next word using up to last 2 words of context.
// - If words is empty, returns common sentence starters.
// - If words has 2+, checks trigrams for "words[-2] words[-1]" first.
// - Falls back to bigrams on "words[-1]" if trigram has no matches.
func (b *BigramEngine) PredictNextContext(words []string) []string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if len(words) == 0 {
		return b.starters
	}

	candidates := make(map[string]int64)

	lastWord := strings.ToLower(strings.TrimSpace(words[len(words)-1]))

	// 1. Trigram: "word[-2] word[-1]" → next (higher weight)
	if len(words) >= 2 {
		w2 := strings.ToLower(strings.TrimSpace(words[len(words)-2]))
		key := w2 + " " + lastWord
		if nexts, ok := b.trigramSeeds[key]; ok {
			for next, weight := range nexts {
				candidates[next] += weight // trigram weight is highest
			}
		}
	}

	// 2. Bigram: lastWord → next
	if nexts, ok := b.transitions[lastWord]; ok {
		for next, weight := range nexts {
			candidates[next] += weight
		}
	}

	// 3. User-learned bigrams (highest personal priority multiplier)
	if nexts, ok := b.userTransitions[lastWord]; ok {
		for next, weight := range nexts {
			candidates[next] += weight * 10
		}
	}

	// 4. Fallback: if still empty, return starters
	if len(candidates) == 0 {
		return b.starters[:min5(5, len(b.starters))]
	}

	// Sort and return top 5
	type entry struct {
		word   string
		weight int64
	}
	sorted := make([]entry, 0, len(candidates))
	for w, weight := range candidates {
		sorted = append(sorted, entry{w, weight})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].weight > sorted[j].weight
	})

	result := make([]string, 0, 5)
	for i := 0; i < len(sorted) && i < 5; i++ {
		result = append(result, sorted[i].word)
	}
	return result
}

// Starters returns common sentence starters (for empty input).
func (b *BigramEngine) Starters() []string {
	return b.starters
}

func min5(a, b int) int {
	if a < b {
		return a
	}
	return b
}
