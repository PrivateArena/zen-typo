package suggest

import (
	"testing"
)

func TestStartersReturnedOnEmptyContext(t *testing.T) {
	b := NewBigramEngine()
	got := b.PredictNextContext([]string{})
	if len(got) == 0 {
		t.Fatal("PredictNextContext([]) should return starters, got nothing")
	}
}

func TestBigramLookup(t *testing.T) {
	b := NewBigramEngine()
	results := b.PredictNextContext([]string{"thank"})
	if len(results) == 0 {
		t.Fatal("no predictions for 'thank'")
	}
	// "you" should be the top prediction after "thank"
	if results[0] != "you" {
		t.Errorf("expected 'you' as top prediction after 'thank', got %q", results[0])
	}
}

func TestTrigramBeatsUnigram(t *testing.T) {
	b := NewBigramEngine()
	// "looking forward" → trigram should produce "to" as top
	results := b.PredictNextContext([]string{"looking", "forward"})
	if len(results) == 0 {
		t.Fatal("no predictions for 'looking forward'")
	}
	if results[0] != "to" {
		t.Errorf("expected trigram 'to' as top for 'looking forward', got %q", results[0])
	}
}

func TestUserTransitionOutranksSeeds(t *testing.T) {
	b := NewBigramEngine()
	// Add a very high-weight user transition
	b.AddUserTransition("hello", "zennnn", 999)
	results := b.PredictNextContext([]string{"hello"})
	if len(results) == 0 {
		t.Fatal("no predictions after AddUserTransition")
	}
	if results[0] != "zennnn" {
		t.Errorf("user transition should outrank seeds: got %q", results[0])
	}
}

func TestPredictNextContextFallbackToStarters(t *testing.T) {
	b := NewBigramEngine()
	// A normal unknown word should NOT return starters (should return nil/empty)
	resultsNormal := b.PredictNextContext([]string{"xyzzyqqqq"})
	if len(resultsNormal) > 0 {
		t.Errorf("Expected empty/nil next-word predictions for unknown normal word, got %v", resultsNormal)
	}

	// An unknown word ending with sentence-ending punctuation should return starters
	resultsEnd := b.PredictNextContext([]string{"xyzzyqqqq."})
	if len(resultsEnd) == 0 {
		t.Fatal("Expected starters to be returned for sentence-ending context, got nothing")
	}
	if resultsEnd[0] != "i" {
		t.Errorf("Expected first starter to be 'i', got %q", resultsEnd[0])
	}
}

func TestResultsCappedAtFive(t *testing.T) {
	b := NewBigramEngine()
	results := b.PredictNextContext([]string{"i"})
	if len(results) > 5 {
		t.Errorf("expected at most 5 results, got %d", len(results))
	}
}

func TestLetMeKnowTrigram(t *testing.T) {
	b := NewBigramEngine()
	results := b.PredictNextContext([]string{"let", "me"})
	if len(results) == 0 {
		t.Fatal("no predictions for 'let me'")
	}
	// "know" should be the top trigram result
	if results[0] != "know" {
		t.Errorf("expected 'know' for 'let me', got %q", results[0])
	}
}

func TestCouldYouPleaseTrigram(t *testing.T) {
	b := NewBigramEngine()
	results := b.PredictNextContext([]string{"could", "you"})
	if len(results) == 0 {
		t.Fatal("no predictions for 'could you'")
	}
	if results[0] != "please" {
		t.Errorf("expected 'please' for 'could you', got %q", results[0])
	}
}

// Engine tests

func TestEngineEmptyInput(t *testing.T) {
	e := NewEngine()
	got := e.Suggest("")
	if got != nil {
		t.Errorf("expected nil for empty input, got %v", got)
	}
}

func TestEnginePrefixCompletion(t *testing.T) {
	e := NewEngine()
	e.AddWord("thank", 100)
	e.AddWord("that", 200)
	e.AddWord("than", 150)
	results := e.Suggest("tha")
	found := false
	for _, r := range results {
		if r == "thank" || r == "that" || r == "than" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("prefix 'tha' should complete to thank/that/than, got %v", results)
	}
}

func TestEngineSpellingReturnsResults(t *testing.T) {
	e := NewEngine()
	e.AddWord("the", 1000)
	// Any near-word should return at least one result (engine is functional)
	results := e.Suggest("teh")
	if len(results) == 0 {
		t.Error("Suggest('teh') returned no results; engine may be broken")
	}
}

func TestEnginePrefixPriority(t *testing.T) {
	e := NewEngine()
	// Manually inject words to control frequency and distance precisely
	e.AddWord("we", 50000)      // very high frequency typo candidate for "wh"
	e.AddWord("who", 50)       // low frequency prefix match candidate for "wh"
	e.AddWord("why", 50)       // low frequency prefix match candidate for "wh"

	results := e.Suggest("wh")
	if len(results) == 0 {
		t.Fatal("expected suggestions for 'wh'")
	}
	// "who" and "why" must rank before "we" because they are prefix completions
	if results[0] == "we" {
		t.Errorf("expected prefix completion to rank before typo correction 'we', got: %v", results)
	}
}
