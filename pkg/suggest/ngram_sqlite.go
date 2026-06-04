package suggest

import (
	"database/sql"
	"fmt"
	"log"
	"sort"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

// SQLitePredictor implements Predictor using a pre-built bigram/trigram corpus DB.
// Schema (created by tools/setup_ngram_db):
//
//	bigrams(word TEXT, next TEXT, freq INTEGER, PRIMARY KEY(word,next))
//	trigrams(w1 TEXT, w2 TEXT, next TEXT, freq INTEGER, PRIMARY KEY(w1,w2,next))
//
// Falls back to the builtin BigramEngine for any word not covered by the corpus.
type SQLitePredictor struct {
	db      *sql.DB
	builtin *BigramEngine

	stmtBigram  *sql.Stmt
	stmtTrigram *sql.Stmt
}

// NewSQLitePredictor opens the corpus DB at dbPath.
// If the DB file cannot be opened (e.g. first run before setup), it falls back
// to the builtin engine and logs a warning rather than crashing.
func NewSQLitePredictor(dbPath string) (*SQLitePredictor, error) {
	builtin := NewBigramEngine()

	db, err := sql.Open("sqlite3", dbPath+"?mode=ro&_journal=OFF&_sync=OFF&cache=shared")
	if err != nil {
		log.Printf("[SQLitePredictor] Cannot open %s: %v — using builtin fallback", dbPath, err)
		return &SQLitePredictor{builtin: builtin}, nil
	}

	// Verify the file is actually accessible
	if pingErr := db.Ping(); pingErr != nil {
		db.Close()
		log.Printf("[SQLitePredictor] %s not accessible: %v — using builtin fallback", dbPath, pingErr)
		return &SQLitePredictor{builtin: builtin}, nil
	}

	// Prepare statements for hot-path queries
	stmtBigram, err := db.Prepare(
		`SELECT next, freq FROM bigrams WHERE word=? ORDER BY freq DESC LIMIT 7`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("prepare bigram stmt: %w", err)
	}

	stmtTrigram, err := db.Prepare(
		`SELECT next, freq FROM trigrams WHERE w1=? AND w2=? ORDER BY freq DESC LIMIT 7`)
	if err != nil {
		stmtBigram.Close()
		db.Close()
		return nil, fmt.Errorf("prepare trigram stmt: %w", err)
	}

	log.Printf("[SQLitePredictor] Corpus DB loaded from %s", dbPath)
	return &SQLitePredictor{
		db:          db,
		builtin:     builtin,
		stmtBigram:  stmtBigram,
		stmtTrigram: stmtTrigram,
	}, nil
}

// PredictNextContext queries trigrams first, merges with bigrams, and falls
// back to the builtin engine for coverage on unknown words.
func (p *SQLitePredictor) PredictNextContext(words []string) []string {
	if len(words) == 0 {
		return p.builtin.Starters()
	}

	// If DB is unavailable, delegate fully to builtin
	if p.db == nil {
		return p.builtin.PredictNextContext(words)
	}

	scores := make(map[string]int64)
	lastWord := strings.ToLower(strings.TrimSpace(words[len(words)-1]))

	// 1. Trigram query (higher weight ×3)
	if len(words) >= 2 {
		w2 := strings.ToLower(strings.TrimSpace(words[len(words)-2]))
		rows, err := p.stmtTrigram.Query(w2, lastWord)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var next string
				var freq int64
				if rows.Scan(&next, &freq) == nil {
					scores[next] += freq * 3
				}
			}
		}
	}

	// 2. Bigram query
	rows, err := p.stmtBigram.Query(lastWord)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var next string
			var freq int64
			if rows.Scan(&next, &freq) == nil {
				scores[next] += freq
			}
		}
	}

	// 3. Builtin fallback blended in (lower weight ×0.1) for coverage
	for _, w := range p.builtin.PredictNextContext(words) {
		if _, exists := scores[w]; !exists {
			scores[w] = 1 // only fill gaps, don't override corpus
		}
	}

	if len(scores) == 0 {
		if len(lastWord) > 0 {
			lastChar := lastWord[len(lastWord)-1]
			if lastChar == '.' || lastChar == '?' || lastChar == '!' {
				return p.builtin.Starters()
			}
		}
		return nil
	}

	return rankTop5(scores)
}

func (p *SQLitePredictor) Starters() []string {
	return p.builtin.Starters()
}

func (p *SQLitePredictor) PredictSentences(words []string) []string {
	return nil
}

func (p *SQLitePredictor) Close() error {
	if p.stmtBigram != nil {
		p.stmtBigram.Close()
	}
	if p.stmtTrigram != nil {
		p.stmtTrigram.Close()
	}
	if p.db != nil {
		return p.db.Close()
	}
	return nil
}

// rankTop5 sorts score map and returns top 5 word strings.
func rankTop5(scores map[string]int64) []string {
	type entry struct {
		word  string
		score int64
	}
	sorted := make([]entry, 0, len(scores))
	for w, s := range scores {
		sorted = append(sorted, entry{w, s})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].score > sorted[j].score
	})
	result := make([]string, 0, 5)
	for i := 0; i < len(sorted) && i < 5; i++ {
		result = append(result, sorted[i].word)
	}
	return result
}

// BuiltinPredictor wraps BigramEngine to satisfy the Predictor interface.
type BuiltinPredictor struct {
	engine *BigramEngine
}

func NewBuiltinPredictor() *BuiltinPredictor {
	return &BuiltinPredictor{engine: NewBigramEngine()}
}

func (p *BuiltinPredictor) PredictNextContext(words []string) []string {
	return p.engine.PredictNextContext(words)
}
func (p *BuiltinPredictor) PredictSentences(words []string) []string {
	return nil
}
func (p *BuiltinPredictor) Starters() []string { return p.engine.Starters() }
func (p *BuiltinPredictor) Close() error        { return nil }

func LoadCorpusWordFrequencies(dbPath string, engine *Engine) error {
	db, err := sql.Open("sqlite3", dbPath+"?mode=ro&_journal=OFF&_sync=OFF&cache=shared")
	if err != nil {
		return err
	}
	defer db.Close()

	// Query sum of frequencies for each word in the bigrams table (both first and second words)
	rows, err := db.Query(`
		SELECT w, SUM(f) as total_freq FROM (
			SELECT word as w, freq as f FROM bigrams
			UNION ALL
			SELECT next as w, freq as f FROM bigrams
		)
		GROUP BY w
		ORDER BY total_freq DESC
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var word string
		var freq int64
		if err := rows.Scan(&word, &freq); err == nil {
			engine.AddWord(word, freq)
			count++
		}
	}
	log.Printf("[Corpus] Hydrated spelling engine with %d words from %s", count, dbPath)
	return nil
}
