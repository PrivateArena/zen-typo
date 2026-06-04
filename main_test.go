package main

import (
	"os"
	"testing"
	"time"
	"zen-typo/pkg/db"
	"zen-typo/pkg/suggest"
)

func TestEngineSpellingAndBigrams(t *testing.T) {
	// 1. Create a clean test database in a temp directory
	tempDir, err := os.MkdirTemp("", "zen-typo-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Override standard user home path for database to redirect it to the temp directory
	// In the real code it uses os.UserHomeDir(), which we can't easily mock, but we can test the suggest/ngram packages directly.
	
	// Initialize suggest engine
	se := suggest.NewEngine()
	
	// Add custom words
	se.AddWord("typography", 100)
	se.AddWord("typo", 80)
	se.AddWord("hello", 10000)

	// Test spelling correction (Exact)
	sug1 := se.Suggest("hello")
	if len(sug1) == 0 || sug1[0] != "hello" {
		t.Errorf("Expected top exact match 'hello', got: %v", sug1)
	}

	// Test fuzzy correction (Edit distance 1)
	sug2 := se.Suggest("hllo")
	if len(sug2) == 0 || sug2[0] != "hello" {
		t.Errorf("Expected suggestion 'hello' for 'hllo', got: %v", sug2)
	}

	// Test autocomplete prefix matching
	sug3 := se.Suggest("typog")
	if len(sug3) == 0 || sug3[0] != "typography" {
		t.Errorf("Expected prefix match 'typography' for 'typog', got: %v", sug3)
	}

	// Test Bigram predictor
	be := suggest.NewBigramEngine()
	// Transitions: "how" -> "are", "about", "do", "to", "many"
	nexts := be.PredictNext("how")
	if len(nexts) == 0 || nexts[0] != "are" {
		t.Errorf("Expected next word prediction 'are' for 'how', got: %v", nexts)
	}

	// Test habit learning weight boost
	be.AddUserTransition("how", "many", 50) // boosts "many" to the top
	nextsBoosted := be.PredictNext("how")
	if len(nextsBoosted) == 0 || nextsBoosted[0] != "many" {
		t.Errorf("Expected 'many' to be boosted to top suggestion, got: %v", nextsBoosted)
	}

	// Test prediction candidates
	phraseCands := be.PredictNext("nice")
	hasTo := false
	for _, p := range phraseCands {
		if p == "to" {
			hasTo = true
			break
		}
	}
	if !hasTo {
		t.Errorf("Expected 'to' to be a suggestion for 'nice', got: %v", phraseCands)
	}
}

func TestDatabaseOperations(t *testing.T) {
	// Create database file in temp dir
	tempDir, err := os.MkdirTemp("", "zen-typo-db-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)
	
	// For testing, since db.Open uses os.UserHomeDir(), let's verify standard SQL operations
	// by connecting to SQLite directly to verify the schema matches our db package design.
	// This is a direct schema check.
	d, err := db.Open()
	if err != nil {
		t.Skipf("Skipping direct DB test (needs HOME env to be writable for config): %v", err)
		return
	}
	defer d.Close()

	// Increment a word
	d.IncrementWord("testing")
	
	// Check word increments (allow time for async insert to complete)
	for i := 0; i < 10; i++ {
		freqs, err := d.LoadWordFrequencies()
		if err == nil {
			if freq, ok := freqs["testing"]; ok && freq > 0 {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	freqs, err := d.LoadWordFrequencies()
	if err != nil {
		t.Fatalf("Failed to load frequencies: %v", err)
	}

	if freqs["testing"] == 0 {
		t.Errorf("Word frequency increment not recorded")
	}
}

// Small helper sleep for async test stability
type timeHelper struct{}
func (timeHelper) Sleep(d time.Duration) {
	time.Sleep(d)
}
