# Zen-Typo

A system-wide autocomplete and next-word prediction daemon for **Linux X11**.
Operates completely out-of-band from IBus/Fcitx — no input-method framework required.
100 % offline, zero cloud dependency, sub-millisecond suggestion latency.

---

## Quick Start

```bash
# Build (standard, no ONNX)
go build -o zen-typo .

# Optional: build the corpus database (~100 K bigrams from Project Gutenberg)
go run ./tools/setup_ngram_db/

# Optional: download DistilGPT-2 ONNX model (~82 MB) for sentence completions
go run ./tools/setup_onnx/

# Run
./zen-typo
```

**Trigger**: press `Ctrl` twice in quick succession (< 300 ms) from any application.

### Keyboard Controls (overlay)

| Key | Action |
|---|---|
| `Tab` | Apply first word candidate |
| `↑` / `↓` | Cycle word candidates |
| `Shift+↑` / `Shift+↓` | Cycle sentence candidates |
| `Shift+Tab` | Apply highlighted sentence candidate |
| `Enter` | Paste current text as-is |
| `Escape` | Close overlay |

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│  User types anywhere (X11)                                      │
└────────────────────┬────────────────────────────────────────────┘
                     │ XRecord (kernel fd, 0 % CPU idle)
                     ▼
             pkg/hotkey · Listener
               Double-Ctrl (<300 ms)
                     │
                     ▼
             pkg/ui · Pure X11 Overlay ──────────► pkg/inject · XTest Ctrl+V
               7 pre-alloc slots
               3 sentence slots
                     │
                     │ commit text
                     │
          ┌──────────┴──────────┐                    │
          │ onTextChanged       │ onCommit            │
          ▼                     ▼                    │
   pkg/suggest                 habit.db (WAL)        │
   ┌──────────────┐  word/bigram learning ──────────►│
   │ Engine       │  (goroutine, non-blocking)
   │  SymSpell    │
   │  QWERTY dist │
   │  Prefix scan │
   └──────┬───────┘
          │ Predictor interface
   ┌──────┴────────────────────────────────┐
   │  builtin   │  ngram (default)  │ onnx │
   │  BigramEng │  SQLitePredictor  │ GPT2 │
   │  (seeded)  │  bigrams+trigrams │ ONNX │
   └────────────┴───────────────────┴──────┘
                         ▲
                    ngrams.db (corpus)
                    habit.db  (personal)
```

---

## Core Components

### `pkg/hotkey` — Global Key Listener
- Uses **XRecord** via a CGo wrapper over `libX11` + `libXtst`.
- Opens two X11 connections: one for `XRecordCreateContext`, one blocking on
  `XRecordEnableContext` (parks on the kernel socket — true 0 % CPU idle).
- Fires the double-Ctrl callback when two `Control_L` presses arrive within **300 ms**
  and no other key is pressed between them.

### `pkg/ui` — Pure X11 Overlay & Strip
- **Window type**: Borderless `OverrideRedirect` window — bypasses window manager decoration and focus policies.
- **Drawing**: Rendered directly via the `xgraphics` package to draw text, borders, and candidate boxes onto custom pixmaps.
- **Fonts**: Automatically resolves standard system TTF fonts (e.g., DejaVuSans, LiberationSans) at runtime.
- **Input handling**: Bypasses the complex GTK widget toolkit, listening to raw KeyPress and ButtonPress events directly on the X11 connection, with event dispatching handled via `xgbutil/xevent`.
- **Performance**: 0 % CPU idle overhead, no CGo bindings for UI, and builds in a fraction of a second.

### `pkg/suggest` — Spelling + Prediction Engine

#### Spell / Completion Engine (`engine.go`)
- **SymSpell** deletion-dictionary algorithm: pre-computed deletions at add-time,
  `O(1)` lookup per candidate.
- **QWERTY-aware edit distance**: substitution cost for keyboard-adjacent keys is
  `0.5` (half penalty) instead of `1.0`.
- **Exponential distance decay**: candidate score = `freq × (1 / 250^editDist)` —
  a distance-2 correction is 62 500× weaker than an exact match.
- **Prefix completion**: linear penalty `freq / (1 + suffix_length)`, boosted by
  `+1e9` to always rank above typo corrections of the same word.

#### Predictor Interface (`predictor.go`)
Three interchangeable backends selected via `config.json`:

| Engine | Key | Notes |
|---|---|---|
| `BuiltinPredictor` | `"builtin"` | In-memory seed bigrams, no files needed |
| `SQLitePredictor` | `"ngram"` | Gutenberg corpus, bigrams + trigrams, prepared stmts |
| `OnnxPredictor` | `"onnx"` | DistilGPT-2, build tag `onnx`, sentence completions |

`SQLitePredictor` query priority: **trigram ×3** → **bigram ×1** → builtin fallback
(gap-fill only). Gracefully degrades to builtin when `ngrams.db` is absent.

`OnnxPredictor` requires:
```bash
go build -tags onnx .
# and libonnxruntime.so alongside the binary
```

### `pkg/db` — Habit Learning Database (`habit.db`)
- SQLite in **WAL** mode with `PRAGMA synchronous = NORMAL`.
- Stores per-word frequencies and user bigram transitions.
- All writes (`IncrementWord`, `IncrementBigram`) run in background goroutines —
  disk I/O never blocks a UI frame.
- Loaded at startup into the spelling `Engine` (`UpdateUserFrequency`) and into
  the active `Predictor` (`AddUserTransition`) for personalised ranking.

### `pkg/inject` — Text Injection (`paste.go`)
1. Copies committed text to the system clipboard (`atotto/clipboard`).
2. Waits 150 ms for the previously-active window to regain focus.
3. Simulates `Ctrl+V` via **XTest** (`FakeInput` key events) to paste into the
   target application.

### `pkg/config` — Configuration (`config.json`)
Auto-created next to the binary on first run. Key fields:

```json
{
  "engine": "ngram",
  "ngram_db_path": "ngrams.db",
  "onnx_model_path": "model/distilgpt2.onnx",
  "onnx_vocab_path": "model/vocab.json",
  "max_suggestions": 5,
  "enable_sentence_suggestions": true,
  "max_sentence_suggestions": 3
}
```

Path resolution order: absolute path → relative to executable directory (portable mode).

---

## Suggestion Pipeline (per keystroke)

```
onTextChanged(text)
 ├─ text == ""            → Predictor.Starters()
 ├─ text ends with " "   → Predictor.PredictNextContext(words)
 └─ mid-word fragment
     ├─ Engine.Suggest(lastWord)          [SymSpell + prefix]
     ├─ Predictor.PredictNextContext      [prior context, prefix-filtered]
     └─ merge (context first, then spell) → UpdateCandidates()

 └─ if EnableSentenceSuggestions && words > 0
       goroutine → Predictor.PredictSentences(words)
                  → UpdateSentences()   [seq-guarded]
```

---

## Portability & Data Locations

Resolution order for `habit.db` (and all data files):

1. **Portable mode**: `portable.dat` or `habit.db` found next to the binary →
   all files locked to that directory.
2. **CWD**: binary not portable, checks working directory.
3. **User config dir**: `~/.config/zen-typo/habit.db`.

`config.json` always resolves to the executable directory.

---

## Tools

| Tool | Purpose |
|---|---|
| `tools/setup_ngram_db` | Downloads Project Gutenberg books, builds `ngrams.db` with 100 K+ bigrams |
| `tools/setup_onnx` | Downloads `libonnxruntime.so` v1.25.0 + DistilGPT-2 ONNX model |

---

## Dependencies

| Package | Role |
|---|---|
| `jezek/xgb` + `xgbutil` | X11 protocol (UI window, drawing, text, XTest injection) |
| `mattn/go-sqlite3` | SQLite driver (habit.db + ngrams.db) |
| `atotto/clipboard` | System clipboard write |
| `yalue/onnxruntime_go` | ONNX Runtime binding (build tag `onnx` only) |
| `libX11` + `libXtst` | XRecord (CGo, hotkey listener) |

---

## Architectural Rationale

| Decision | Why |
|---|---|
| XRecord over `xdotool`/evdev polling | Kernel-fd blocking = true 0 % CPU idle |
| Pure X11 OverrideRedirect Tooltips | 100% CGo-free UI compile, instant build, extremely lightweight |
| `Predictor` interface (builtin/ngram/onnx) | Swappable backends without touching UI or learning code |
| SQLite WAL + async goroutines | Disk writes never add latency to suggestion rendering |
| Build tag `onnx` gating CGo | Standard `go build .` works with zero ONNX runtime on the host |
| Exponential decay base 250 | Distance-1 corrections rank 250× below exact matches — typo fixes never hijack prefix completions |
