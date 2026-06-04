package db

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "github.com/mattn/go-sqlite3"
)

type Database struct {
	db     *sql.DB
	mu     sync.Mutex
	dbPath string
}

func resolveDBPath() string {
	// 1. Resolve binary directory
	exe, err := os.Executable()
	var binDir string
	if err == nil {
		binDir = filepath.Dir(exe)
		if strings.Contains(exe, "go-build") || strings.Contains(binDir, "Temp") {
			binDir = ""
		}
	}

	// 2. Check for portable triggers in executable directory
	if binDir != "" {
		portableLock := filepath.Join(binDir, "portable.dat")
		dbPortablePath := filepath.Join(binDir, "habit.db")
		
		_, errLock := os.Stat(portableLock)
		_, errDb := os.Stat(dbPortablePath)
		
		if errLock == nil || errDb == nil {
			log.Printf("[Database] Portable Mode: Using database in executable directory: %s", dbPortablePath)
			return dbPortablePath
		}
	}

	// 3. Check working directory override
	if cwd, err := os.Getwd(); err == nil {
		dbCwdPath := filepath.Join(cwd, "habit.db")
		if _, err := os.Stat(dbCwdPath); err == nil {
			log.Printf("[Database] CWD Mode: Using database in working directory: %s", dbCwdPath)
			return dbCwdPath
		}
	}

	// 4. Default global fallback
	var configDir string
	if home, err := os.UserHomeDir(); err == nil {
		configDir = filepath.Join(home, ".config", "zen-typo")
	} else {
		cwd, _ := os.Getwd()
		configDir = cwd
	}

	_ = os.MkdirAll(configDir, 0755)
	dbConfigPath := filepath.Join(configDir, "habit.db")
	log.Printf("[Database] Standard Mode: Using database in config directory: %s", dbConfigPath)
	return dbConfigPath
}

func Open() (*Database, error) {
	dbPath := resolveDBPath()
	db, err := sql.Open("sqlite3", dbPath+"?_journal=WAL&_sync=NORMAL")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	d := &Database{db: db, dbPath: dbPath}
	if err := d.initSchema(); err != nil {
		db.Close()
		return nil, err
	}

	return d, nil
}

func (d *Database) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.db != nil {
		d.db.Close()
	}
}

func (d *Database) initSchema() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	queries := []string{
		`CREATE TABLE IF NOT EXISTS word_frequency (
			word TEXT PRIMARY KEY,
			frequency INTEGER DEFAULT 0
		);`,
		`CREATE TABLE IF NOT EXISTS bigrams (
			word1 TEXT,
			word2 TEXT,
			frequency INTEGER DEFAULT 0,
			PRIMARY KEY (word1, word2)
		);`,
	}

	for _, q := range queries {
		if _, err := d.db.Exec(q); err != nil {
			return fmt.Errorf("schema init failed: %w", err)
		}
	}
	return nil
}

func (d *Database) IncrementWord(word string) {
	word = strings.ToLower(strings.TrimSpace(word))
	if len(word) == 0 {
		return
	}

	go func() {
		d.mu.Lock()
		defer d.mu.Unlock()

		_, err := d.db.Exec(`
			INSERT INTO word_frequency (word, frequency)
			VALUES (?, 1)
			ON CONFLICT(word) DO UPDATE SET frequency = frequency + 1;
		`, word)
		if err != nil {
			log.Printf("Failed to increment word in DB: %v", err)
		}
	}()
}

func (d *Database) IncrementBigram(word1, word2 string) {
	word1 = strings.ToLower(strings.TrimSpace(word1))
	word2 = strings.ToLower(strings.TrimSpace(word2))
	if len(word1) == 0 || len(word2) == 0 {
		return
	}

	go func() {
		d.mu.Lock()
		defer d.mu.Unlock()

		_, err := d.db.Exec(`
			INSERT INTO bigrams (word1, word2, frequency)
			VALUES (?, ?, 1)
			ON CONFLICT(word1, word2) DO UPDATE SET frequency = frequency + 1;
		`, word1, word2)
		if err != nil {
			log.Printf("Failed to increment bigram in DB: %v", err)
		}
	}()
}

func (d *Database) LoadWordFrequencies() (map[string]int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	rows, err := d.db.Query("SELECT word, frequency FROM word_frequency")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	freqs := make(map[string]int64)
	for rows.Next() {
		var word string
		var freq int64
		if err := rows.Scan(&word, &freq); err != nil {
			return nil, err
		}
		freqs[word] = freq
	}
	return freqs, nil
}

func (d *Database) LoadBigrams() (map[string]map[string]int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	rows, err := d.db.Query("SELECT word1, word2, frequency FROM bigrams")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	bigrams := make(map[string]map[string]int64)
	for rows.Next() {
		var w1, w2 string
		var freq int64
		if err := rows.Scan(&w1, &w2, &freq); err != nil {
			return nil, err
		}
		if _, exists := bigrams[w1]; !exists {
			bigrams[w1] = make(map[string]int64)
		}
		bigrams[w1][w2] = freq
	}
	return bigrams, nil
}
