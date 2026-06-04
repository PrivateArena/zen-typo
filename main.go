package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"zen-typo/pkg/config"
	"zen-typo/pkg/db"
	"zen-typo/pkg/hotkey"
	"zen-typo/pkg/inject"
	"zen-typo/pkg/suggest"
	"zen-typo/pkg/ui"
)

var (
	suggestEngine *suggest.Engine
	predictor     suggest.Predictor // active next-word predictor (builtin|ngram|onnx)
	database      *db.Database
	uiCtrl        *ui.UIController
	cfg           *config.Config
)

func main() {
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Println("Starting zen-typo Autocomplete daemon...")

	// 1. Load configuration
	cfg = config.Load()

	// 2. Initialize SQLite habit DB
	var err error
	database, err = db.Open()
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	// 3. Initialize spelling engine
	suggestEngine = suggest.NewEngine()
	dbPath := cfg.ResolvePath(cfg.NgramDBPath)
	if err := suggest.LoadCorpusWordFrequencies(dbPath, suggestEngine); err != nil {
		log.Printf("[Main] SQLite corpus hydration warning: %v (database might be empty or missing)", err)
	}

	// 4. Initialize next-word predictor from config
	predictor = buildPredictor(cfg)
	defer predictor.Close()

	// 5. Hydrate spelling + predictor with learned habits from habit DB
	hydrateFromDB()

	// 6. Initialize UI
	uiCtrl, err = ui.Start(onTextChanged, func(text string) { onCommit(text, false) }, onHide)
	if err != nil {
		log.Fatalf("Failed to start UI: %v", err)
	}

	// 7. Start Hotkey Listener
	hotkey.Listen(func() {
		if uiCtrl.IsVisible() {
			log.Println("Double-Ctrl detected while visible! Committing current entry...")
			text := uiCtrl.GetText()
			uiCtrl.Hide()
			onCommit(text, false)
		} else {
			log.Println("Double-Ctrl detected! Showing overlay...")
			uiCtrl.Show()
		}
	})
	defer hotkey.Stop()

	// Handle OS shutdown signals gracefully
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down zen-typo...")
}

// buildPredictor constructs the active Predictor based on config.Engine.
func buildPredictor(cfg *config.Config) suggest.Predictor {
	switch cfg.Engine {
	case config.EngineNgram:
		dbPath := cfg.ResolvePath(cfg.NgramDBPath)
		p, err := suggest.NewSQLitePredictor(dbPath)
		if err != nil {
			log.Printf("[Predictor] SQLite init error: %v — falling back to builtin", err)
			return suggest.NewBuiltinPredictor()
		}
		return p

	case config.EngineOnnx:
		modelPath := cfg.ResolvePath(cfg.OnnxModelPath)
		vocabPath := cfg.ResolvePath(cfg.OnnxVocabPath)
		p, err := suggest.NewOnnxPredictor(modelPath, vocabPath)
		if err != nil {
			log.Printf("[Predictor] ONNX init error: %v — falling back to builtin", err)
			return suggest.NewBuiltinPredictor()
		}
		return p

	default: // "builtin" or unrecognised
		if cfg.Engine != config.EngineBuiltin {
			log.Printf("[Predictor] Unknown engine %q — using builtin", cfg.Engine)
		}
		return suggest.NewBuiltinPredictor()
	}
}

func hydrateFromDB() {
	// Load word frequencies into spelling engine
	freqs, err := database.LoadWordFrequencies()
	if err == nil {
		for w, freq := range freqs {
			suggestEngine.UpdateUserFrequency(w, freq)
		}
		log.Printf("Loaded %d learned words from database.", len(freqs))
	} else {
		log.Printf("Failed to load learned words: %v", err)
	}

	// Load bigram transitions into the predictor (if it's builtin-backed)
	bigrams, err := database.LoadBigrams()
	if err == nil {
		count := 0
		for w1, nexts := range bigrams {
			for w2, freq := range nexts {
				// BuiltinPredictor and SQLitePredictor both embed a BigramEngine
				// that accepts user transitions; propagate if the predictor can.
				if bp, ok := predictor.(interface {
					AddUserTransition(word, next string, weight int64)
				}); ok {
					bp.AddUserTransition(w1, w2, freq)
				}
				count++
			}
		}
		log.Printf("Loaded %d learned bigram transitions from database.", count)
	} else {
		log.Printf("Failed to load learned bigrams: %v", err)
	}
}

func onTextChanged(text string) {
	words := strings.Fields(text)

	// Empty input → show sentence starters
	if len(text) == 0 {
		uiCtrl.UpdateCandidates(predictor.Starters())
		return
	}

	// Text ends with space → predict next word using full context
	if strings.HasSuffix(text, " ") {
		if len(words) > 0 {
			uiCtrl.UpdateCandidates(predictor.PredictNextContext(words))
		} else {
			uiCtrl.UpdateCandidates(predictor.Starters())
		}
		return
	}

	// Mid-word: spell-correct/prefix-complete the last fragment.
	// Blend up to 2 next-word predictions (from prior context) with spell suggestions.
	if len(words) > 0 {
		lastFrag := words[len(words)-1]
		spellSugs := suggestEngine.Suggest(lastFrag)

		var merged []string
		seen := make(map[string]bool)

		// Only blend context predictions when there are actual prior words and they match prefix
		if len(words) >= 2 {
			prefix := strings.ToLower(lastFrag)
			for _, s := range predictor.PredictNextContext(words[:len(words)-1]) {
				if strings.HasPrefix(strings.ToLower(s), prefix) {
					if !seen[s] {
						merged = append(merged, s)
						seen[s] = true
					}
					if len(merged) >= 2 {
						break
					}
				}
			}
		}

		for _, s := range spellSugs {
			if !seen[s] {
				merged = append(merged, s)
				seen[s] = true
			}
			if len(merged) >= cfg.MaxSuggestions {
				break
			}
		}
		uiCtrl.UpdateCandidates(merged)
	} else {
		uiCtrl.UpdateCandidates(nil)
	}
}

func onCommit(text string, acceptedSuggestion bool) {
	text = strings.TrimSpace(text)
	if len(text) == 0 {
		return
	}

	log.Printf("Committing (acceptedSuggestion=%t): %q", acceptedSuggestion, text)

	// Passive habit learning — runs in background, never blocks typing
	go func() {
		words := strings.Fields(strings.ToLower(text))
		if len(words) == 0 {
			return
		}

		for _, w := range words {
			wClean := strings.Trim(w, ".,?!;:\"'()[]")
			if len(wClean) > 0 {
				if acceptedSuggestion || suggestEngine.IsValidWord(wClean) {
					suggestEngine.UpdateUserFrequency(wClean, 1)
					database.IncrementWord(wClean)
				}
			}
		}

		for i := 0; i < len(words)-1; i++ {
			w1 := strings.Trim(words[i], ".,?!;:\"'()[]")
			w2 := strings.Trim(words[i+1], ".,?!;:\"'()[]")
			if len(w1) > 0 && len(w2) > 0 {
				if acceptedSuggestion || (suggestEngine.IsValidWord(w1) && suggestEngine.IsValidWord(w2)) {
					// Always feed user transitions back into the builtin engine fallback
					if bp, ok := predictor.(interface {
						AddUserTransition(w, n string, freq int64)
					}); ok {
						bp.AddUserTransition(w1, w2, 1)
					}
					database.IncrementBigram(w1, w2)
				}
			}
		}
	}()

	// Inject text back into the previously-active window
	go func() {
		time.Sleep(100 * time.Millisecond)
		if err := inject.Paste(text + " "); err != nil {
			log.Printf("Text injection error: %v", err)
		}
	}()
}

func onHide() {
	log.Println("Overlay hidden.")
}
