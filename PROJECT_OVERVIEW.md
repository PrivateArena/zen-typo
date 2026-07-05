# Zen‑Typo — Project Architecture

## Purpose

Desktop typing enhancement tool for Linux/X11. Intercepts keystrokes, tracks
words in real time, provides autocorrect (spell check + fuzzy correction) and
predictive text (next‑word / sentence completion) via an on‑screen overlay.
Learns user word habits through an SQLite DB.

---

## File Tree

```
.
├── main.go                          # Entry point — wires all subsystems
├── pkg/
│   ├── config/config.go             # Configuration loading / defaulting
│   ├── db/db.go                     # SQLite persistence (word/bigram freqs)
│   ├── hotkey/listener.go           # X11 hotkey capture (double‑Ctrl/Alt/Shift)
│   ├── inject/
│   │   ├── autocorrect.go           # Backspace‑and‑type replacement
│   │   └── paste.go                 # Clipboard paste (Ctrl+V)
│   ├── input/tracker.go             # Key event → word fragment state machine
│   ├── suggest/
│   │   ├── predictor.go             # Predictor interface
│   │   ├── engine.go                # Spell engine (dict + edit‑distance + fuzzy)
│   │   ├── ngram.go                 # In‑memory bigram/trigram engine
│   │   ├── ngram_sqlite.go          # SQLite‑backed n‑gram predictor
│   │   ├── lazy_predictor.go        # Deferred‑load wrapper (hot‑swap to ONNX)
│   │   ├── onnx_predictor.go        # GPT‑2 / ONNX neural predictor
│   │   └── onnx_stub.go             # Stub when ONNX build tag not set
│   └── ui/
│       ├── overlay.go               # Full popup overlay (spell/predict UI)
│       └── strip.go                 # Minimal inline suggestion bar
└── tools/
    ├── setup_ngram_db/main.go       # Corpus download + SQLite n‑gram builder
    └── setup_onnx/main.go           # ONNX runtime + model download
```

---

## Component Roles & Key Exports

### `main.go` — Orchestrator (483 loc)
- `main()` — Loads config, opens DB, creates Engine + Predictor, starts UI,
  hooks hotkeys, enters event loop
- `buildPredictor()` — Constructs `LazyPredictor` wrapping
  `SQLitePredictor` + `BuiltinPredictor`, kicks off ONNX background load
- `hydrateFromDB()` — Feeds persisted word/bigram freqs into Engine
- `onTextChanged()` — Central event: runs autocorrect + updates overlay
- `onCommit()` — Persists typed word transitions to DB + predictor
- `onTrackerFragment()` — Feeds partial fragment to predictor, updates overlay
- `onTrackerSpace()` — Triggers autocorrect replacement on space key

### `pkg/config/config.go` — Configuration (224 loc)
- `Config` struct — All user‑facing settings (execlude list, UI colours,
  prediction flags, key mappings)
- `Load()` — Reads JSON from `~/.config/zen-typo/config.json`, creates default
  if absent
- `ResolvePath()` — Resolves relative paths relative to config directory

### `pkg/db/db.go` — SQLite Persistence (221 loc)
- `Database` struct — Wraps `*sql.DB`
- `Open()` / `Close()` — Connect/disconnect
- `IncrementWord()` / `IncrementBigram()` — Record a typed word or transition
- `LoadWordFrequencies()` / `LoadBigrams()` — Hydrate engine at startup
- `ResetHabits()` — Forget learned frequencies

### `pkg/hotkey/listener.go` — X11 Hotkey Monitor (234 loc)
- `Listen()` / `Stop()` — Async loop detecting double‑tap Ctrl/Alt/Shift
- `SetKeyEventCallback()` — Register callback for every key event
- `Suppress()` — Temporarily suppress events (avoids autocorrect loops)

### `pkg/inject/autocorrect.go` — Text Replacement (164 loc)
- `ReplaceWord()` — Backspace N chars then paste correction (via clipboard)
- `GetActiveWindowClass()` — Query X11 `_NET_WM_PID` → `WM_CLASS`
- `ShouldIgnoreAutocorrect()` — Skip if active window in ignore list
- `TypeString()` — Paste arbitrary text (clipboard fallback)

### `pkg/inject/paste.go` — Clipboard Paste (69 loc)
- `Paste()` — Copy text → clipboard → X11 `Ctrl+V` emulation

### `pkg/input/tracker.go` — Word Tracking State Machine (245 loc)
- `WordTracker` struct — Tracks fragment, word history, modifier state
- `Feed(keyEvent)` — Process raw X11 key press (alpha, space, backspace, etc.)
- `Fragment()` — Current partial word (thread‑safe)
- `LastWord()` — Most recently completed word

### `pkg/suggest/predictor.go` — Predictor Interface (20 loc)
- `Predictor` interface — `PredictNextContext`, `PredictSentences`,
  `Starters`, `Close`

### `pkg/suggest/engine.go` — Spell Engine (489 loc)
- `Engine` struct — Dictionary + edit‑distance + fuzzy model
- `LoadDictionary()` / `LoadCustomWords()` — Populate word set
- `Suggest()` — Ranked completions + corrections for a fragment
- `BestCorrection()` / `FuzzyCorrection()` — Find nearest dictionary word
- `TrainFuzzyModel()` — Background goroutine for keyboard‑distance training
- `MatchCasing()` — Preserve UPPER/Title case on correction

### `pkg/suggest/ngram.go` — In‑Memory N‑gram (440 loc)
- `BigramEngine` struct — Frequency maps for bigrams + trigrams
- `PredictNextContext()` — Given word history, predicts next word
- `Starters()` — Common sentence‑opening words

### `pkg/suggest/ngram_sqlite.go` — SQLite N‑gram (255 loc)
- `SQLitePredictor` struct — Queries pre‑built SQLite bigram/trigram tables
- `NewSQLitePredictor()` — Opens DB, falls back to builtin on error
- `AddUserTransition()` — Records user bigram transitions
- `BuiltinPredictor` — Small in‑memory fallback for unknown words

### `pkg/suggest/lazy_predictor.go` — Lazy / Deferred Predictor (93 loc)
- `LazyPredictor` struct — Wraps a primary `Predictor`; until background
  ONNX loading completes, delegates to a `fallback` predictor
- `IsReady()` — Whether primary predictor has loaded
- `AddUserTransition()` — Fans out to both fallback and (once ready) primary

### `pkg/suggest/onnx_predictor.go` — ONNX Neural Predictor (534 loc)
- `OnnxPredictor` struct — GPT‑2 ONNX model runner
- `PredictNextContext()` — Top‑5 word continuations via transformer inference
- `PredictSentences()` — Generates full‑sentence completions
- `tokenize()` / `decodeTokens()` / `topKWords()` — GPT‑2 subword pipeline
- `loadGPT2Vocab()` — Loads `vocab.json`

### `pkg/suggest/onnx_stub.go` — ONNX Stub (24 loc)
- Build‑tag‑gated alternative: `NewOnnxPredictor()` returns nil; every method
  is a no‑op. Used when the `!onnx` build tag is active.

### `pkg/ui/overlay.go` — Popup Overlay (465 loc)
- `UIController` struct — X11 window with custom rendering
- `Start()` — Creates window, enters event loop
- `Show()` / `Hide()` — Toggle visibility
- `UpdateCandidates()` / `UpdateSentences()` — Push new data from tracker
- `handleKeyPress()` — Arrow keys to select, Enter to commit
- `renderLocked()` — OpenGL‑style custom drawing (rects, borders, text)
- `loadTTFFont()` — Loads custom font for rendering

### `pkg/ui/strip.go` — Inline Suggestion Bar (206 loc)
- `Strip` struct — Overlay at top of screen showing 3‑5 candidates
- `NewStrip()` — Create window, position at screen top
- `UpdateCandidates()` — Refresh displayed candidates
- `handleClick()` / `handleMouseClick()` — Click‑to‑select
- `SelectCandidate()` — External selection (from hotkey)

### `tools/setup_ngram_db/main.go` — N‑gram DB Builder (330 loc)
- Downloads or imports plain‑text corpus, tokenizes, builds bigram/trigram
  frequency tables, bulk‑inserts into SQLite

### `tools/setup_onnx/main.go` — ONNX Setup Tool (203 loc)
- Downloads GPT‑2 ONNX model + ONNX Runtime shared library,
  verifies integrity

---

## Architecture Diagram (Dependency Flow)

```
┌───────────────────────────────────────────────────────────────────┐
│                         main.go                                   │
│  Loads Config, opens DB, creates Engine, builds Predictor         │
│  wires Hotkey → Tracker → Engine → UI                             │
└────┬──────┬──────┬──────┬──────┬──────┬──────┬──────┬────────────┘
     │      │      │      │      │      │      │      │
     ▼      ▼      ▼      ▼      ▼      ▼      ▼      ▼
  ┌────┐ ┌────┐ ┌──────┐ ┌────┐ ┌──────┐ ┌────┐ ┌────┐ ┌────┐
  │cfg │ │ db │ │engine│ │trkr│ │pred. │ │ui  │ │hk  │ │inj │
  │    │ │    │ │      │ │    │ │      │ │    │ │    │ │    │
  │Load│ │Open│ │NewEng│ │New │ │build │ │Str │ │Set │ │Rpl │
  │Res │ │Clse│ │Load* │ │Feed│ │Pred. │ │Strt│ │Cal │ │Pste│
  └────┘ └────┘ └──────┘ └────┘ └──────┘ └────┘ └────┘ └────┘
                    │             │
                    │             ├── LazyPredictor (wraps)
                    │             │    ├── SQLitePredictor ←─ tools/setup_ngram_db
                    │             │    ├── BuiltinPredictor
                    │             │    └── OnnxPredictor  ←─ tools/setup_onnx
                    │             │
                    │             └── Predictor interface
                    │                  (predictor.go)
                    │
                    └── BigramEngine (ngram.go)
                         in‑memory bigram/trigram

EVENT LOOP:
  X11 event → Hotkey.Listen()
              → KeyEventCallback (SetKeyEventCallback)
                → WordTracker.Feed()
                  → onTrackerFragment / onTrackerSpace / onTextChanged
                    → Engine.FuzzyCorrection / Suggest
                    → LazyPredictor.PredictNextContext
                    → UIController.UpdateCandidates / .Show
                    → inject.ReplaceWord (on autocorrect)
                    → db.IncrementWord / IncrementBigram (on commit)
```

---

## Key Architectural Patterns

### 1. Strategy Pattern — Predictor Interface
`predictor.go` defines `Predictor` interface with `PredictNextContext`,
`PredictSentences`, `Starters`, `Close`. Four implementations:
- `SQLitePredictor` (SQLite‑backed n‑gram)
- `BigramEngine` (in‑memory n‑gram)
- `BuiltinPredictor` (fallback, wraps in‑memory maps)
- `OnnxPredictor` (neural)

### 2. Proxy / Deferred Loading — LazyPredictor
`LazyPredictor` wraps a "primary" predictor. When primary is not yet loaded
(ONNX loads in background goroutine), all calls go to a `fallback` predictor.
Once `IsReady()` returns true, the primary takes over.

### 3. Event‑Driven Wiring via Callbacks
`SetKeyEventCallback` is the single integration point. `main.go` registers
`WordTracker.Feed` as the callback. The tracker then invokes registered
notifiers (`onTrackerFragment`, `onTrackerSpace`, `onTextChanged`) defined in
`main.go`, which form the "controller" layer.

### 4. Build‑Tag Stub
`onnx_stub.go` uses `//go:build !onnx` to replace the full ONNX predictor
with a no‑op implementation when the ONNX runtime is unavailable.

### 5. Embedding the Spell Engine
`Engine` is not behind an interface — it is used directly from `main.go`
for synchronous spell‑checking and autocorrect decisions. Only *prediction*
uses the `Predictor` interface.

### 6. State Machine — WordTracker
`WordTracker.Feed()` implements a character‑level state machine:
- `alpha` → accumulate fragment
- `space` → commit word, fire autocorrect
- `backspace` → shorten fragment
- `modifier` → track for shortcuts
- `non‑alpha` → punctuation passes through

### 7. Thread Safety
- `WordTracker` uses `sync.RWMutex` on `Fragment()` / `LastWord()` reads
- `UIController` / `Strip` use mutex‑locked render cycles
- `Database` inherits SQLite connection safety
- `LazyPredictor.AddUserTransition` is safe for concurrent calls

### 8. Configuration Cascade
`Load()` → tries CWD config, then `~/.config/zen-typo/config.json`,
then executable directory. Writes defaults to first writable location.

### 9. X11‑Native UI
No toolkit dependency. Overlay and strip create raw X11 windows with
`XCreateWindow`, render via `XRender` / `Xft` / `XDrawString`, and
process events through `XNextEvent` loops.

---

## Connection Map (How Components Wire Together)

| main.go function        | Calls Into                                        | Called By / Trigger          |
|-------------------------|---------------------------------------------------|------------------------------|
| `main()`                | config.Load, db.Open, engine.NewEngine,           | `go run`                     |
|                         | buildPredictor, hydrateFromDB, ui.Start,          |                              |
|                         | hotkey.SetKeyEventCallback, hotkey.Listen         |                              |
| `buildPredictor()`      | NewSQLitePredictor, NewBuiltinPredictor,          | `main()`                     |
|                         | NewLazyPredictor, NewOnnxPredictor (goroutine)    |                              |
| `onTrackerFragment()`   | Predictor.PredictNextContext,                     | WordTracker fragment notify  |
|                         | ui.UpdateCandidates                               |                              |
| `onTrackerSpace()`      | Engine.FuzzyCorrection, MatchCasing,              | WordTracker space notify     |
|                         | inject.ReplaceWord, DB.IncrementWord              |                              |
| `onTextChanged()`       | Engine.Suggest, Predictor.PredictNextContext,     | overlay.handleKeyPress       |
|                         | ui.UpdateCandidates, UpdateSentences              |                              |
| `onCommit()`            | DB.IncrementBigram, Predictor.AddUserTransition   | overlay.applySuggestion      |
| `onHide()`              | ui overlay Hide                                   | ESC key                      |

| Component Pair            | Direction | Mechanism                      |
|---------------------------|-----------|--------------------------------|
| Hotkey → Tracker          | callback  | `SetKeyEventCallback(Feed)`    |
| Tracker → main handlers   | notify    | Registered closures            |
| main → Engine             | direct    | Method calls                   |
| main → Predictor          | interface | `Predictor` interface          |
| main → DB                 | direct    | `Database` struct methods      |
| main → UI (overlay/strip) | direct    | Method calls                   |
| main → Inject             | direct    | `ReplaceWord`, `TypeString`    |
| Engine → Config           | direct    | `Config` fields for tuning     |
| LazyPredictor → SQLite/   | interface | Delegates to wrapped Predictor |
|                Builtin/   |           |                                |
|                Onnx       |           |                                |
