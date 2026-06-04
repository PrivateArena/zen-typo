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

package main

import (
	"bufio"
	"compress/gzip"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	_ "github.com/mattn/go-sqlite3"
)

const (
	defaultOutput     = "ngrams.db"
	defaultMaxBigrams = 100_000
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
		log.Println("Downloading Google Books N-gram corpus (shard 0)...")
		log.Println("This is ~200MB download. Use Ctrl-C to cancel.")
		if err := downloadAndImport(*output, *maxBigrams); err != nil {
			log.Fatalf("Download failed: %v\n\nAlternative: provide your own corpus with --input", err)
		}
	}

	log.Printf("✅ ngrams.db ready at %s", *output)
	log.Printf("   Set \"engine\": \"ngram\", \"ngram_db_path\": \"%s\" in config.json", *output)
}

// downloadAndImport fetches Google Books bigrams and imports top N.
func downloadAndImport(outputPath string, maxBigrams int) error {
	tmpFile, err := os.CreateTemp("", "ngrams-*.gz")
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	log.Printf("Downloading %s ...", googleBigrams1URL)
	resp, err := http.Get(googleBigrams1URL)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	written, err := io.Copy(tmpFile, resp.Body)
	if err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	log.Printf("Downloaded %.1f MB", float64(written)/1e6)

	if _, err := tmpFile.Seek(0, 0); err != nil {
		return err
	}

	gr, err := gzip.NewReader(tmpFile)
	if err != nil {
		return fmt.Errorf("gzip open: %w", err)
	}
	defer gr.Close()

	return importGoogleBigramsReader(gr, outputPath, maxBigrams)
}

// bigramEntry and trigramEntry are shared types for frequency tracking.
type bigramEntry struct {
	word, next string
	freq       int64
}
type trigramEntry struct {
	w1, w2, w3 string
	freq       int64
}

//   ngram TAB year TAB match_count TAB volume_count
// We aggregate across all years (sum match_counts).
func importGoogleBigramsReader(r io.Reader, outputPath string, limit int) error {
	db, err := openDB(outputPath)
	if err != nil {
		return err
	}
	defer db.Close()

	type pair struct{ word, next string }
	freqs := make(map[pair]int64)

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)

	log.Println("Parsing bigrams...")
	lines := 0
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.SplitN(line, "\t", 4)
		if len(fields) < 3 {
			continue
		}
		ngram := fields[0]
		count, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil {
			continue
		}

		parts := strings.Fields(ngram)
		if len(parts) != 2 {
			continue
		}

		w1 := cleanWord(parts[0])
		w2 := cleanWord(parts[1])
		if w1 == "" || w2 == "" {
			continue
		}

		freqs[pair{w1, w2}] += count
		lines++
		if lines%500_000 == 0 {
			log.Printf("  Parsed %dK bigram lines...", lines/1000)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan: %w", err)
	}

	bEntries := make([]bigramEntry, 0, len(freqs))
	for p, f := range freqs {
		bEntries = append(bEntries, bigramEntry{p.word, p.next, f})
	}

	log.Printf("Parsed %d unique bigram pairs. Importing top %d...", len(bEntries), limit)
	return bulkInsertBigrams(db, bEntries, limit)
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
