// Package suggest provides the lazy predictor wrapper.
package suggest

import (
	"log"
	"sync"
)

// LazyPredictor wraps two Predictor instances: a fast fallback that is used
// immediately, and a primary (e.g. ONNX) that is loaded in the background.
// Once the primary is ready it is swapped in atomically. All methods are safe
// for concurrent use.
type LazyPredictor struct {
	mu      sync.RWMutex
	current Predictor
	primary Predictor // nil until loaded
}

// NewLazyPredictor starts loading the primary predictor in a goroutine.
// Until it is ready every call is served by fallback.
func NewLazyPredictor(fallback Predictor, load func() (Predictor, error)) *LazyPredictor {
	lp := &LazyPredictor{current: fallback}
	go func() {
		p, err := load()
		if err != nil {
			log.Printf("[LazyPredictor] Primary load failed: %v — staying on fallback", err)
			return
		}
		lp.mu.Lock()
		lp.primary = p
		lp.current = p
		lp.mu.Unlock()
		log.Printf("[LazyPredictor] Primary predictor ready")
	}()
	return lp
}

func (lp *LazyPredictor) PredictNextContext(words []string) []string {
	lp.mu.RLock()
	defer lp.mu.RUnlock()
	return lp.current.PredictNextContext(words)
}

func (lp *LazyPredictor) PredictSentences(words []string) []string {
	lp.mu.RLock()
	defer lp.mu.RUnlock()
	return lp.current.PredictSentences(words)
}

func (lp *LazyPredictor) Starters() []string {
	lp.mu.RLock()
	defer lp.mu.RUnlock()
	return lp.current.Starters()
}

func (lp *LazyPredictor) Close() error {
	lp.mu.Lock()
	defer lp.mu.Unlock()
	if lp.primary != nil && lp.primary != lp.current {
		_ = lp.primary.Close()
	}
	return lp.current.Close()
}

// IsReady reports whether the primary predictor has loaded.
func (lp *LazyPredictor) IsReady() bool {
	lp.mu.RLock()
	defer lp.mu.RUnlock()
	return lp.primary != nil
}

func (lp *LazyPredictor) AddUserTransition(word, next string, weight int64) {
	lp.mu.Lock()
	defer lp.mu.Unlock()

	// Helper function to forward transition to a predictor
	forward := func(p Predictor) {
		if p == nil {
			return
		}
		if bp, ok := p.(interface {
			AddUserTransition(word, next string, weight int64)
		}); ok {
			bp.AddUserTransition(word, next, weight)
		}
	}

	forward(lp.current)
	if lp.primary != lp.current {
		forward(lp.primary)
	}
}
