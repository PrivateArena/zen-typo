package config

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
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

	// EnableSentenceSuggestions enables full-sentence completions (Option 2/ONNX only).
	EnableSentenceSuggestions bool `json:"enable_sentence_suggestions"`

	// MaxSentenceSuggestions controls how many sentence candidates are shown (1–3).
	MaxSentenceSuggestions int `json:"max_sentence_suggestions"`

	// EnableStrip shows the ambient always-on tray strip while the user types.
	EnableStrip bool `json:"enable_strip"`

	// StripSelectModifier controls the modifier key used to select candidates from the strip: "alt" | "super" | "ctrl" | "ctrl+alt" | "none"
	StripSelectModifier string `json:"strip_select_modifier"`

	// EnableAutocorrect silently replaces high-confidence typos when Space is pressed.
	EnableAutocorrect bool `json:"enable_autocorrect"`

	// AutocorrectMaxDist is the maximum edit distance accepted for silent correction.
	// 0.5 = keyboard-adjacent single char; 1.0 = any single edit. Default: 1.0.
	AutocorrectMaxDist float64 `json:"autocorrect_max_dist"`

	// DictPath is the path to a system or custom plain-text dictionary.
	// Loaded at startup to prevent autocorrecting valid words.
	DictPath string `json:"dict_path"`

	// AutocorrectIgnoreApps lists window classes or instance names to skip autocorrect in.
	AutocorrectIgnoreApps []string `json:"autocorrect_ignore_apps"`

	// CustomWords lists user-specified valid words that should never be autocorrected.
	CustomWords []string `json:"custom_words"`

	// LazyOnnx loads the ONNX predictor in the background so startup is instant.
	LazyOnnx bool `json:"lazy_onnx"`
}

// Default returns a config with sensible out-of-the-box values.
func Default() *Config {
	return &Config{
		Engine:                    EngineBuiltin,
		NgramDBPath:               "ngrams.db",
		OnnxModelPath:             "model/distilgpt2.onnx",
		OnnxVocabPath:             "model/vocab.json",
		OnnxMergesPath:            "model/merges.txt",
		MaxSuggestions:            5,
		EnableSentenceSuggestions: true,
		MaxSentenceSuggestions:    3,
		EnableStrip:               true,
		StripSelectModifier:       "alt",
		EnableAutocorrect:         true,
		AutocorrectMaxDist:        1.0,
		DictPath:                  "/usr/share/dict/words",
		AutocorrectIgnoreApps:     []string{"terminal", "kitty", "alacritty", "konsole"},
		CustomWords:               []string{},
		LazyOnnx:                  true,
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

	if cfg.MaxSentenceSuggestions < 1 {
		cfg.MaxSentenceSuggestions = 1
	}
	if cfg.MaxSentenceSuggestions > 3 {
		cfg.MaxSentenceSuggestions = 3
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

func containsTemp(path string) bool {
	lower := strings.ToLower(path)
	return strings.Contains(lower, "/tmp") || strings.Contains(lower, "temp") || strings.Contains(lower, "go-build")
}

// resolveConfigPath returns the path to config.json, preferring the
// current working directory first, and then the executable directory (excluding temp/build dirs).
func resolveConfigPath() string {
	// 1. Try CWD first
	cwd, err := os.Getwd()
	if err == nil {
		p := filepath.Join(cwd, "config.json")
		if _, err2 := os.Stat(p); err2 == nil {
			return p
		}
	}

	// 2. Try Executable dir (excluding temporary build paths)
	exe, err := os.Executable()
	if err == nil && !containsTemp(exe) {
		p := filepath.Join(filepath.Dir(exe), "config.json")
		if _, err2 := os.Stat(p); err2 == nil {
			return p
		}
	}

	// 3. Fallback to CWD default
	if err == nil {
		return filepath.Join(cwd, "config.json")
	}
	return "config.json"
}

// ResolvePath resolves a config path relative to the config file directory
// if it's not already absolute, skipping temp directories.
func (c *Config) ResolvePath(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	// Try CWD first if it contains the target file
	cwd, err := os.Getwd()
	if err == nil {
		path := filepath.Join(cwd, p)
		if _, err2 := os.Stat(path); err2 == nil {
			return path
		}
	}

	exe, err := os.Executable()
	if err == nil && !containsTemp(exe) {
		return filepath.Join(filepath.Dir(exe), p)
	}

	if err == nil {
		return filepath.Join(cwd, p)
	}
	return p
}
