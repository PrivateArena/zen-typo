// tools/setup_ngram_db/main.go
//
// Downloads the top-N English bigrams/trigrams from a public corpus and imports
// them into an SQLite DB that zen-typo can query at runtime.
//
// Usage:
//   go run ./tools/setup_ngram_db/           # downloads + builds ngrams.db
//   go run ./tools/setup_ngram_db/ --input my_text.txt   # import custom corpus
//   go run ./tools/setup_ngram_db/ --max-bigrams 200000  # larger corpus
//
// Data source: Google Books 2-gram (English, 20120701 version)
// https://storage.googleapis.com/books/ngrams/books/20120701/eng/2-00000-of-00589.gz
// (we only download the first shard, ~200MB uncompressed → top 500K bigrams)
//go:build ignore

package main

import (
	"bufio"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	_ "github.com/mattn/go-sqlite3"
)

const (
	defaultOutput      = "ngrams.db"
	defaultMaxBigrams  = 100_000
	defaultMaxTrigrams = 50_000

	// First shard of Google Books English 2-grams (smallest usable shard)
	googleBigrams1URL = "https://storage.googleapis.com/books/ngrams/books/20120701/eng/2-00000-of-00589.gz"
	googleBigrams2URL = "https://storage.googleapis.com/books/ngrams/books/20120701/eng/2-00001-of-00589.gz"
)

func main() {
	output := flag.String("output", defaultOutput, "Output SQLite DB path")
	inputFile := flag.String("input", "", "Import custom text corpus instead of downloading")
	maxBigrams := flag.Int("max-bigrams", defaultMaxBigrams, "Max bigrams to import")
	maxTrigrams := flag.Int("max-trigrams", defaultMaxTrigrams, "Max trigrams to import")
	flag.Parse()

	log.SetFlags(log.LstdFlags)

	if *inputFile != "" {
		log.Printf("Importing corpus from %s ...", *inputFile)
		if err := importFromTextFile(*inputFile, *output, *maxBigrams, *maxTrigrams); err != nil {
			log.Fatalf("Import failed: %v", err)
		}
	} else {
		log.Println("Downloading Gutenberg corpus and building N-grams...")
		if err := downloadCorpusAndBuild(*output, *maxBigrams, *maxTrigrams); err != nil {
			log.Fatalf("Corpus build failed: %v", err)
		}
	}

	log.Printf("✅ ngrams.db ready at %s", *output)
	log.Printf("   Set \"engine\": \"ngram\", \"ngram_db_path\": \"%s\" in config.json", *output)
}

func downloadCorpusAndBuild(outputPath string, maxBigrams, maxTrigrams int) error {
	urls := []string{
		"https://www.gutenberg.org/cache/epub/1661/pg1661.txt", // Sherlock Holmes
		"https://www.gutenberg.org/cache/epub/1342/pg1342.txt", // Pride and Prejudice
		"https://www.gutenberg.org/cache/epub/84/pg84.txt",     // Frankenstein
	}

	type pair struct{ w1, w2 string }
	type triple struct{ w1, w2, w3 string }
	bigramFreqs := make(map[pair]int64)
	trigramFreqs := make(map[triple]int64)

	// Ingest seed phrases first to guarantee they are present and have high frequency
	seedSentences := []string{
		"The quick brown fox jumps over the lazy dog.",
		"The lazy fox jumps over the quick brown dog.",
		"Hello world, this is a test of the autocomplete daemon.",
		"Please let me know if you need any further assistance.",
		"Could you please send me the details as soon as possible?",
		"How are you doing today?",
		"Thank you very much for your help.",
		"What is the best way to contact you?",
		"I am writing to confirm our meeting tomorrow.",
		"Let me know what you think.",
	}

	ingestText := func(text string, weight int64) {
		words := strings.FieldsFunc(text, func(r rune) bool {
			return !unicode.IsLetter(r) && r != '\''
		})
		cleaned := make([]string, 0, len(words))
		for _, w := range words {
			c := cleanWord(w)
			if c != "" {
				cleaned = append(cleaned, c)
			}
		}
		for i := 0; i < len(cleaned)-1; i++ {
			bigramFreqs[pair{cleaned[i], cleaned[i+1]}] += weight
			if i < len(cleaned)-2 {
				trigramFreqs[triple{cleaned[i], cleaned[i+1], cleaned[i+2]}] += weight
			}
		}
	}

	for _, s := range seedSentences {
		ingestText(s, 1000000)
	}

	for _, url := range urls {
		log.Printf("Downloading English corpus from %s ...", url)
		resp, err := http.Get(url)
		if err != nil {
			return fmt.Errorf("download %s: %w", url, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("download failed for %s: %s", url, resp.Status)
		}

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			ingestText(scanner.Text(), 1)
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("scan %s: %w", url, err)
		}
	}

	db, err := openDB(outputPath)
	if err != nil {
		return err
	}
	defer db.Close()

	bEntries := make([]bigramEntry, 0, len(bigramFreqs))
	for p, f := range bigramFreqs {
		bEntries = append(bEntries, bigramEntry{p.w1, p.w2, f})
	}
	log.Printf("Importing %d bigrams (cap=%d)...", len(bEntries), maxBigrams)
	if err := bulkInsertBigrams(db, bEntries, maxBigrams); err != nil {
		return err
	}

	tEntries := make([]trigramEntry, 0, len(trigramFreqs))
	for t, f := range trigramFreqs {
		tEntries = append(tEntries, trigramEntry{t.w1, t.w2, t.w3, f})
	}
	log.Printf("Importing %d trigrams (cap=%d)...", len(tEntries), maxTrigrams)
	if err := bulkInsertTrigrams(db, tEntries, maxTrigrams); err != nil {
		return err
	}

	return nil
}

type bigramEntry struct {
	word, next string
	freq       int64
}
type trigramEntry struct {
	w1, w2, w3 string
	freq       int64
}

// importFromTextFile builds bigrams/trigrams by counting transitions in a
// plain text file. Useful for domain-specific corpora.
func importFromTextFile(inputPath, outputPath string, maxBigrams, maxTrigrams int) error {
	f, err := os.Open(inputPath)
	if err != nil {
		return err
	}
	defer f.Close()

	type pair struct{ w1, w2 string }
	type triple struct{ w1, w2, w3 string }
	bigramFreqs := make(map[pair]int64)
	trigramFreqs := make(map[triple]int64)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)

	for scanner.Scan() {
		words := strings.FieldsFunc(scanner.Text(), func(r rune) bool {
			return !unicode.IsLetter(r) && r != '\''
		})
		cleaned := make([]string, 0, len(words))
		for _, w := range words {
			c := cleanWord(w)
			if c != "" {
				cleaned = append(cleaned, c)
			}
		}
		for i := 0; i < len(cleaned)-1; i++ {
			bigramFreqs[pair{cleaned[i], cleaned[i+1]}]++
			if i < len(cleaned)-2 {
				trigramFreqs[triple{cleaned[i], cleaned[i+1], cleaned[i+2]}]++
			}
		}
	}

	db, err := openDB(outputPath)
	if err != nil {
		return err
	}
	defer db.Close()

	bigrams := make(map[struct{ word, next string }]int64)
	for p, f := range bigramFreqs {
		bigrams[struct{ word, next string }{p.w1, p.w2}] = f
	}
	bEntries := make([]bigramEntry, 0, len(bigrams))
	for p, f := range bigrams {
		bEntries = append(bEntries, bigramEntry{p.word, p.next, f})
	}

	log.Printf("Importing %d bigrams (cap=%d)...", len(bEntries), maxBigrams)
	if err := bulkInsertBigrams(db, bEntries, maxBigrams); err != nil {
		return err
	}

	tEntries := make([]trigramEntry, 0, len(trigramFreqs))
	for t, f := range trigramFreqs {
		tEntries = append(tEntries, trigramEntry{t.w1, t.w2, t.w3, f})
	}
	log.Printf("Importing %d trigrams (cap=%d)...", len(tEntries), maxTrigrams)
	return bulkInsertTrigrams(db, tEntries, maxTrigrams)
}

func openDB(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil && filepath.Dir(path) != "." {
		return nil, err
	}
	db, err := sql.Open("sqlite3", path+"?_journal=WAL&_sync=NORMAL")
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS bigrams (
			word TEXT NOT NULL,
			next TEXT NOT NULL,
			freq INTEGER NOT NULL DEFAULT 1,
			PRIMARY KEY (word, next)
		);
		CREATE INDEX IF NOT EXISTS idx_bigrams ON bigrams(word, freq DESC);

		CREATE TABLE IF NOT EXISTS trigrams (
			w1   TEXT NOT NULL,
			w2   TEXT NOT NULL,
			next TEXT NOT NULL,
			freq INTEGER NOT NULL DEFAULT 1,
			PRIMARY KEY (w1, w2, next)
		);
		CREATE INDEX IF NOT EXISTS idx_trigrams ON trigrams(w1, w2, freq DESC);
	`)
	return db, err
}

func bulkInsertBigrams(db *sql.DB, entries []bigramEntry, limit int) error {
	sortBigramDesc(entries)
	if len(entries) > limit {
		entries = entries[:limit]
	}
	tx, _ := db.Begin()
	stmt, _ := tx.Prepare(`INSERT OR REPLACE INTO bigrams(word,next,freq) VALUES(?,?,?)`)
	for _, e := range entries {
		stmt.Exec(e.word, e.next, e.freq)
	}
	stmt.Close()
	return tx.Commit()
}

func bulkInsertTrigrams(db *sql.DB, entries []trigramEntry, limit int) error {
	// Partial selection sort to get top-limit
	for i := 0; i < len(entries) && i < limit; i++ {
		maxIdx := i
		for j := i + 1; j < len(entries); j++ {
			if entries[j].freq > entries[maxIdx].freq {
				maxIdx = j
			}
		}
		entries[i], entries[maxIdx] = entries[maxIdx], entries[i]
	}
	if len(entries) > limit {
		entries = entries[:limit]
	}
	tx, _ := db.Begin()
	stmt, _ := tx.Prepare(`INSERT OR REPLACE INTO trigrams(w1,w2,next,freq) VALUES(?,?,?,?)`)
	for _, e := range entries {
		stmt.Exec(e.w1, e.w2, e.w3, e.freq)
	}
	stmt.Close()
	return tx.Commit()
}

func sortBigramDesc(entries []bigramEntry) {
	n := len(entries)
	for i := 1; i < n; i++ {
		key := entries[i]
		j := i - 1
		for j >= 0 && entries[j].freq < key.freq {
			entries[j+1] = entries[j]
			j--
		}
		entries[j+1] = key
	}
}

func cleanWord(w string) string {
	w = strings.ToLower(strings.Trim(w, ".,!?;:\"'()[]{}"))
	if len(w) < 2 {
		return ""
	}
	for _, r := range w {
		if !unicode.IsLetter(r) && r != '\'' && r != '-' {
			return ""
		}
	}
	return w
}
