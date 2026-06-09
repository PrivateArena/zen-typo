// Package input tracks the current word fragment being typed from raw X11
// key events supplied by pkg/hotkey. It uses a static US-QWERTY keycode map
// (covers the vast majority of Linux X11 users). Non-letter characters act as
// word boundaries that flush the current fragment.
package input

import (
	"strings"
	"sync"
)

// keycodeToChar maps X11 Linux keycodes to lowercase runes for the US-QWERTY
// layout. Only letter + apostrophe/hyphen keys are tracked; everything else is
// treated as a word boundary.
var keycodeToChar = map[int]rune{
	// Top row: QWERTYUIOP
	24: 'q', 25: 'w', 26: 'e', 27: 'r', 28: 't',
	29: 'y', 30: 'u', 31: 'i', 32: 'o', 33: 'p',
	// Home row: ASDFGHJKL
	38: 'a', 39: 's', 40: 'd', 41: 'f', 42: 'g',
	43: 'h', 44: 'j', 45: 'k', 46: 'l',
	// Bottom row: ZXCVBNM
	52: 'z', 53: 'x', 54: 'c', 55: 'v', 56: 'b', 57: 'n', 58: 'm',
	// Apostrophe (key between L and Enter on US layout)
	48: '\'',
}

const (
	kcBackspace = 22
	kcSpace     = 65
	kcReturn    = 36
	kcTab       = 23
	kcEscape    = 9
	kcCtrlL     = 37
	kcCtrlR     = 105
	kcAltL      = 64
	kcAltR      = 108
	kcShiftL    = 50
	kcShiftR    = 62
	kcSuper     = 133
)

// WordTracker maintains the current word fragment and a short word history
// from raw X11 key events. All callbacks are called from the XRecord goroutine;
// implementations must be non-blocking (e.g. post to a channel or glib.IdleAdd).
type WordTracker struct {
	mu sync.Mutex

	fragment       string   // characters typed since last word boundary
	words          []string // last N committed words (context)
	altPressed     bool     // tracks Alt state for hotkeys
	ctrlPressed    bool     // tracks Ctrl state for hotkeys
	superPressed   bool     // tracks Super state for hotkeys
	SelectModifier string   // "alt" | "ctrl" | "super" | "ctrl+alt" | "none"

	// OnFragment is called on every fragment change with (fragment, wordContext).
	OnFragment func(fragment string, words []string)
	// OnSpace is called when Space is pressed: (committedWord, wordContext before it).
	// The space has already been delivered to the target app.
	OnSpace func(word string, words []string)
	// OnBoundary is called on any non-space word boundary (Enter, Escape, etc.).
	OnBoundary func()
	// OnChooseCandidate is called when selection hotkey is pressed. Returns true if candidate selection succeeded.
	OnChooseCandidate func(index int) bool
}

func NewWordTracker() *WordTracker {
	return &WordTracker{
		SelectModifier: "alt", // default
	}
}

// Feed processes a single raw key event. Call this from hotkey.SetKeyEventCallback.
func (t *WordTracker) Feed(keycode int, isPress bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	switch keycode {
	case kcAltL, kcAltR:
		t.altPressed = isPress
		return
	case kcCtrlL, kcCtrlR:
		t.ctrlPressed = isPress
		return
	case kcSuper:
		t.superPressed = isPress
		return
	}

	if !isPress {
		return // only care about presses for non-modifiers
	}

	// Hotkey selection from 1 to 5 (keycodes 10 to 14)
	if keycode >= 10 && keycode <= 14 {
		var modifierMatch bool
		switch t.SelectModifier {
		case "alt":
			modifierMatch = t.altPressed && !t.ctrlPressed && !t.superPressed
		case "ctrl":
			modifierMatch = t.ctrlPressed && !t.altPressed && !t.superPressed
		case "super":
			modifierMatch = t.superPressed && !t.altPressed && !t.ctrlPressed
		case "ctrl+alt":
			modifierMatch = t.ctrlPressed && t.altPressed && !t.superPressed
		case "none":
			modifierMatch = false
		default: // fallback to alt
			modifierMatch = t.altPressed && !t.ctrlPressed && !t.superPressed
		}

		if modifierMatch && t.OnChooseCandidate != nil {
			idx := keycode - 10
			if t.OnChooseCandidate(idx) {
				return // handled and consumed!
			}
		}
	}

	switch keycode {
	case kcShiftL, kcShiftR:
		return // modifiers alone don't affect the fragment

	case kcBackspace:
		if len(t.fragment) > 0 {
			runes := []rune(t.fragment)
			t.fragment = string(runes[:len(runes)-1])
			t.notify()
		}

	case kcSpace:
		word := strings.TrimSpace(t.fragment)
		ctx := append([]string(nil), t.words...)
		t.fragment = ""
		if len(word) > 0 {
			t.words = append(t.words, word)
			if len(t.words) > 6 {
				t.words = t.words[len(t.words)-6:]
			}
		}
		if cb := t.OnSpace; cb != nil && len(word) > 0 {
			go cb(word, ctx)
		}
		t.notify()

	case kcReturn, kcTab, kcEscape:
		t.fragment = ""
		t.words = nil
		if cb := t.OnBoundary; cb != nil {
			go cb()
		}
		t.notify()

	default:
		if ch, ok := keycodeToChar[keycode]; ok {
			t.fragment += string(ch)
			t.notify()
		} else {
			// Non-letter printable (digit, punctuation): treat as boundary
			t.fragment = ""
			t.notify()
		}
	}
}

// Reset clears the fragment, word history, and modifier states.
func (t *WordTracker) Reset() {
	t.mu.Lock()
	t.fragment = ""
	t.words = nil
	t.altPressed = false
	t.ctrlPressed = false
	t.superPressed = false
	t.mu.Unlock()
}

// Fragment returns the current word fragment (safe for concurrent reads).
func (t *WordTracker) Fragment() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.fragment
}

func (t *WordTracker) notify() {
	if cb := t.OnFragment; cb != nil {
		frag := t.fragment
		ctx := append([]string(nil), t.words...)
		go cb(frag, ctx)
	}
}
