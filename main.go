package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"zen-typo/pkg/db"
	"zen-typo/pkg/hotkey"
	"zen-typo/pkg/inject"
	"zen-typo/pkg/suggest"
	"zen-typo/pkg/ui"
)

var (
	suggestEngine *suggest.Engine
	bigramEngine  *suggest.BigramEngine
	database      *db.Database
	uiCtrl        *ui.UIController
)

func main() {
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Println("Starting zen-typo Autocomplete daemon...")

	// 1. Initialize SQLite Database
	var err error
	database, err = db.Open()
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	// 2. Initialize Suggestion & Bigram engines
	suggestEngine = suggest.NewEngine()
	bigramEngine = suggest.NewBigramEngine()

	// 3. Hydrate engines with learned habits from DB
	hydrateFromDB()

	// 4. Initialize UI
	uiCtrl, err = ui.Start(onTextChanged, onCommit, onHide)
	if err != nil {
		log.Fatalf("Failed to start UI: %v", err)
	}

	// 5. Start Hotkey Listener
	hotkey.Listen(func() {
		if uiCtrl.IsVisible() {
			log.Println("Double-Ctrl detected while visible! Committing current entry...")
			text := uiCtrl.GetText()
			uiCtrl.Hide()
			onCommit(text)
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

func hydrateFromDB() {
	// Load word frequencies
	freqs, err := database.LoadWordFrequencies()
	if err == nil {
		for w, freq := range freqs {
			suggestEngine.UpdateUserFrequency(w, freq)
		}
		log.Printf("Loaded %d learned words from database.", len(freqs))
	} else {
		log.Printf("Failed to load learned words: %v", err)
	}

	// Load bigram frequencies
	bigrams, err := database.LoadBigrams()
	if err == nil {
		count := 0
		for w1, nexts := range bigrams {
			for w2, freq := range nexts {
				bigramEngine.AddUserTransition(w1, w2, freq)
				count++
			}
		}
		log.Printf("Loaded %d learned bigram transitions from database.", count)
	} else {
		log.Printf("Failed to load learned bigrams: %v", err)
	}
}

func onTextChanged(text string) {
	if len(text) == 0 {
		uiCtrl.UpdateCandidates(nil)
		return
	}

	// Check if the text ends with space
	if strings.HasSuffix(text, " ") {
		// Suggest next words
		words := strings.Fields(text)
		if len(words) > 0 {
			lastWord := words[len(words)-1]
			suggestions := bigramEngine.PredictNext(lastWord)
			uiCtrl.UpdateCandidates(suggestions)
		} else {
			uiCtrl.UpdateCandidates(nil)
		}
	} else {
		// Suggest spelling correction / word completion
		words := strings.Fields(text)
		if len(words) > 0 {
			lastFragment := words[len(words)-1]
			suggestions := suggestEngine.Suggest(lastFragment)
			uiCtrl.UpdateCandidates(suggestions)
		} else {
			uiCtrl.UpdateCandidates(nil)
		}
	}
}

func onCommit(text string) {
	text = strings.TrimSpace(text)
	if len(text) == 0 {
		return
	}

	log.Printf("Committing: %q", text)

	// Perform passive habit learning in the background
	go func() {
		words := strings.Fields(strings.ToLower(text))
		if len(words) == 0 {
			return
		}

		// 1. Learn individual words
		for _, w := range words {
			// Strip standard punctuation marks for word learning
			wClean := strings.Trim(w, ".,?!;:\"'()[]")
			if len(wClean) > 0 {
				suggestEngine.UpdateUserFrequency(wClean, 1)
				database.IncrementWord(wClean)
			}
		}

		// 2. Learn transitions (bigrams)
		for i := 0; i < len(words)-1; i++ {
			w1 := strings.Trim(words[i], ".,?!;:\"'()[]")
			w2 := strings.Trim(words[i+1], ".,?!;:\"'()[]")
			if len(w1) > 0 && len(w2) > 0 {
				bigramEngine.AddUserTransition(w1, w2, 1)
				database.IncrementBigram(w1, w2)
			}
		}
	}()

	// Inject text back into active window
	go func() {
		// Small delay to ensure the window has hidden and focus is restored
		time.Sleep(100 * time.Millisecond)
		err := inject.Paste(text + " ")
		if err != nil {
			log.Printf("Text injection error: %v", err)
		}
	}()
}

func onHide() {
	log.Println("Overlay hidden.")
}
