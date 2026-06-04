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
	// An unknown word should fall back gracefully (starters or empty, not panic)
	results := b.PredictNextContext([]string{"xyzzyqqqq"})
	// Should not panic; may return starters or empty slice
	_ = results
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
	// Any near-word should return at least one result (engine is functional)
	results := e.Suggest("teh")
	if len(results) == 0 {
		t.Error("Suggest('teh') returned no results; engine may be broken")
	}
}
