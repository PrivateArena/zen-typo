package config

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
)

// Engine constants for the "engine" config field.
const (
	EngineBuiltin = "builtin" // in-memory seed bigrams/trigrams (default, no files needed)
	EngineNgram   = "ngram"   // SQLite corpus bigram/trigram DB (Option 1)
	EngineOnnx    = "onnx"    // DistilGPT-2 via ONNX Runtime (Option 2, requires -tags onnx)
)

// Config is the root configuration structure persisted to config.json.
type Config struct {
	// Engine selects the prediction backend: "builtin" | "ngram" | "onnx"
	Engine string `json:"engine"`

	// NgramDBPath is the path to the SQLite bigram/trigram corpus DB.
	// Resolved relative to the executable directory if not absolute.
	// Create with: go run ./tools/setup_ngram_db/
	NgramDBPath string `json:"ngram_db_path"`

	// OnnxModelPath is the path to the DistilGPT-2 ONNX model file.
	// Resolved relative to the executable directory if not absolute.
	// Download with: go run ./tools/setup_onnx/
	OnnxModelPath string `json:"onnx_model_path"`

	// OnnxVocabPath is the path to the GPT-2 vocab.json file.
	OnnxVocabPath string `json:"onnx_vocab_path"`

	// OnnxMergesPath is the path to the GPT-2 merges.txt file.
	OnnxMergesPath string `json:"onnx_merges_path"`

	// MaxSuggestions controls how many candidates are shown (1–7).
	MaxSuggestions int `json:"max_suggestions"`
}

// Default returns a config with sensible out-of-the-box values.
func Default() *Config {
	return &Config{
		Engine:         EngineBuiltin,
		NgramDBPath:    "ngrams.db",
		OnnxModelPath:  "model/distilgpt2.onnx",
		OnnxVocabPath:  "model/vocab.json",
		OnnxMergesPath: "model/merges.txt",
		MaxSuggestions: 5,
	}
}

// Load reads config.json from the executable directory or working directory.
// If the file doesn't exist, a default config is written and returned.
func Load() *Config {
	path := resolveConfigPath()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := Default()
			if writeErr := save(path, cfg); writeErr != nil {
				log.Printf("[Config] Could not write default config: %v", writeErr)
			} else {
				log.Printf("[Config] Created default config at %s", path)
			}
			return cfg
		}
		log.Printf("[Config] Could not read config: %v — using defaults", err)
		return Default()
	}

	cfg := Default()
	if err := json.Unmarshal(data, cfg); err != nil {
		log.Printf("[Config] Malformed config.json: %v — using defaults", err)
		return Default()
	}

	// Clamp max suggestions to valid range
	if cfg.MaxSuggestions < 1 {
		cfg.MaxSuggestions = 1
	}
	if cfg.MaxSuggestions > 7 {
		cfg.MaxSuggestions = 7
	}

	log.Printf("[Config] Loaded (engine=%s)", cfg.Engine)
	return cfg
}

func save(path string, cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// resolveConfigPath returns the path to config.json, preferring the
// executable directory (portable mode) over the working directory.
func resolveConfigPath() string {
	exe, err := os.Executable()
	if err == nil {
		p := filepath.Join(filepath.Dir(exe), "config.json")
		if _, err2 := os.Stat(p); err2 == nil {
			return p // found next to executable
		}
		// Will be created next to executable (portable mode)
		return p
	}
	return "config.json"
}

// ResolvePath resolves a config path relative to the executable directory
// if it's not already absolute.
func (c *Config) ResolvePath(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	exe, err := os.Executable()
	if err != nil {
		return p
	}
	return filepath.Join(filepath.Dir(exe), p)
}
