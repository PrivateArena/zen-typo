package suggest

// Predictor is the unified interface for all next-word prediction backends.
// All implementations must be safe for concurrent use.
type Predictor interface {
	// PredictNextContext returns up to 5 next-word candidates given the
	// last 0–N words of context (same semantics as BigramEngine).
	PredictNextContext(words []string) []string

	// PredictSentences returns up to 3 full-sentence continuation candidates.
	PredictSentences(words []string) []string

	// Starters returns common sentence-opening words shown on empty input.
	Starters() []string

	// Close releases any resources held by the predictor (DB connections,
	// ONNX sessions, etc.). Safe to call multiple times.
	Close() error
}
