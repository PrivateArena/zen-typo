package ui

import (
	"fmt"
	"log"
	"runtime"
	"strings"
	"sync"

	"github.com/gotk3/gotk3/gdk"
	"github.com/gotk3/gotk3/glib"
	"github.com/gotk3/gotk3/gtk"
)

const maxSlots = 7

// candidateSlot is a pre-allocated, permanently-live widget pair.
// We NEVER call Destroy() on these — that's the entire point.
type candidateSlot struct {
	box   *gtk.EventBox
	label *gtk.Label
}

type UIController struct {
	window       *gtk.Window
	entry        *gtk.Entry
	candidateBox *gtk.Box
	slots        [maxSlots]*candidateSlot // pre-allocated, never destroyed

	candidates    []string
	selectedIndex int
	visible       bool
	currentText   string

	OnTextChanged func(text string)
	OnCommit      func(text string)
	OnHide        func()

	mu sync.Mutex
}

var controller *UIController

func Start(onTextChanged func(text string), onCommit func(text string), onHide func()) (*UIController, error) {
	runtime.LockOSThread()

	gtk.Init(nil)

	win, err := gtk.WindowNew(gtk.WINDOW_TOPLEVEL)
	if err != nil {
		return nil, fmt.Errorf("failed to create window: %w", err)
	}

	win.SetTitle("zen-typo")
	win.SetDecorated(false)
	win.SetKeepAbove(true)
	win.SetSkipPagerHint(true)
	win.SetSkipTaskbarHint(true)
	win.SetDefaultSize(800, 240)
	win.SetPosition(gtk.WIN_POS_CENTER)

	mainBox, err := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 14)
	if err != nil {
		return nil, err
	}
	mainBox.SetMarginStart(25)
	mainBox.SetMarginEnd(25)
	mainBox.SetMarginTop(20)
	mainBox.SetMarginBottom(20)
	win.Add(mainBox)

	entry, err := gtk.EntryNew()
	if err != nil {
		return nil, err
	}
	entry.SetPlaceholderText("Type your word or sentence here...")
	mainBox.PackStart(entry, false, false, 0)

	candidateBox, err := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 8)
	if err != nil {
		return nil, err
	}
	mainBox.PackStart(candidateBox, true, true, 0)

	tipsLabel, err := gtk.LabelNew("Tab: Apply & predict next  |  ↑↓: Cycle candidates  |  Enter: Paste  |  Esc: Close")
	if err != nil {
		return nil, err
	}
	tipsLabel.SetHAlign(gtk.ALIGN_START)
	tipsLabel.SetVAlign(gtk.ALIGN_CENTER)
	tipsLabel.SetName("tips-label")
	mainBox.PackEnd(tipsLabel, false, false, 0)

	controller = &UIController{
		window:        win,
		entry:         entry,
		candidateBox:  candidateBox,
		OnTextChanged: onTextChanged,
		OnCommit:      onCommit,
		OnHide:        onHide,
	}

	// Pre-allocate candidate slots — created once, never destroyed.
	// This is the core fix: zero widget.Destroy() calls = zero focus theft.
	for i := 0; i < maxSlots; i++ {
		slot, err := newCandidateSlot(controller, i)
		if err != nil {
			return nil, fmt.Errorf("failed to create candidate slot %d: %w", i, err)
		}
		controller.slots[i] = slot
		candidateBox.PackStart(slot.box, false, false, 0)
	}

	applyCSS()

	// "changed" signal fires automatically when entry.SetText is called,
	// so applySuggestionAndPredict does NOT need to call OnTextChanged explicitly.
	entry.Connect("changed", func() {
		text, _ := entry.GetText()
		controller.mu.Lock()
		controller.currentText = text
		controller.mu.Unlock()
		if controller.OnTextChanged != nil {
			controller.OnTextChanged(text)
		}
	})

	win.Connect("key-press-event", func(w *gtk.Window, event *gdk.Event) bool {
		keyEvent := gdk.EventKeyNewFromEvent(event)
		keyval := keyEvent.KeyVal()

		switch keyval {
		case gdk.KEY_Escape:
			controller.Hide()
			if controller.OnHide != nil {
				controller.OnHide()
			}
			return true

		case gdk.KEY_Return, gdk.KEY_KP_Enter:
			text, _ := entry.GetText()
			if controller.selectedIndex >= 0 && controller.selectedIndex < len(controller.candidates) {
				words := strings.Fields(text)
				if len(words) > 0 {
					words[len(words)-1] = controller.candidates[controller.selectedIndex]
					text = strings.Join(words, " ") + " "
				}
			}
			if controller.OnCommit != nil {
				controller.OnCommit(text)
			}
			controller.Hide()
			return true

		case gdk.KEY_Tab:
			controller.mu.Lock()
			idx := controller.selectedIndex
			if idx < 0 && len(controller.candidates) > 0 {
				idx = 0
			}
			controller.mu.Unlock()
			if idx >= 0 && idx < len(controller.candidates) {
				controller.applySuggestion(idx)
			}
			return true

		case gdk.KEY_ISO_Left_Tab:
			controller.mu.Lock()
			if len(controller.candidates) > 0 {
				controller.selectedIndex--
				if controller.selectedIndex < 0 {
					controller.selectedIndex = len(controller.candidates) - 1
				}
				idx := controller.selectedIndex
				controller.mu.Unlock()
				controller.applySuggestion(idx)
			} else {
				controller.mu.Unlock()
			}
			return true

		case gdk.KEY_Down:
			if len(controller.candidates) > 0 {
				controller.mu.Lock()
				controller.selectedIndex++
				if controller.selectedIndex >= len(controller.candidates) {
					controller.selectedIndex = 0
				}
				controller.mu.Unlock()
				controller.renderCandidates()
			}
			return true

		case gdk.KEY_Up:
			if len(controller.candidates) > 0 {
				controller.mu.Lock()
				controller.selectedIndex--
				if controller.selectedIndex < 0 {
					controller.selectedIndex = len(controller.candidates) - 1
				}
				controller.mu.Unlock()
				controller.renderCandidates()
			}
			return true
		}

		return false
	})

	win.Connect("delete-event", func() bool {
		controller.Hide()
		if controller.OnHide != nil {
			controller.OnHide()
		}
		return true
	})

	go func() {
		gtk.Main()
	}()

	return controller, nil
}

// newCandidateSlot creates a permanent EventBox+Label slot for candidate display.
func newCandidateSlot(c *UIController, idx int) (*candidateSlot, error) {
	box, err := gtk.EventBoxNew()
	if err != nil {
		return nil, err
	}
	// EventBox must NOT steal keyboard focus
	box.SetCanFocus(false)

	label, err := gtk.LabelNew("")
	if err != nil {
		return nil, err
	}
	label.SetCanFocus(false)
	label.SetMarginStart(4)
	label.SetMarginEnd(4)

	ctx, err := label.GetStyleContext()
	if err == nil {
		ctx.AddClass("candidate-label")
	}

	box.Add(label)

	// Click handler — capture idx at slot-creation time
	slotIdx := idx
	box.Connect("button-press-event", func() bool {
		c.applySuggestion(slotIdx)
		return true
	})

	// Hidden by default
	box.Hide()

	return &candidateSlot{box: box, label: label}, nil
}

func applyCSS() {
	cssProvider, err := gtk.CssProviderNew()
	if err != nil {
		log.Printf("Failed to create CSS provider: %v", err)
		return
	}

	css := `
		window {
			background-color: #161624;
			border: 3px solid #00f0ff;
			border-radius: 12px;
		}
		entry, entry text {
			background-color: #ffffff;
			color: #000000;
			font-size: 26px;
			padding: 10px 16px;
			caret-color: #ff007f;
		}
		entry {
			border: 1px solid #ff007f;
			border-radius: 8px;
		}
		entry:focus, entry text:focus {
			border: 2px solid #00f0ff;
		}
		#tips-label {
			font-size: 14px;
			color: #7b7b99;
			margin-top: 8px;
		}
		.candidate-label {
			font-size: 20px;
			color: #cfcfdb;
			background-color: #21253b;
			padding: 10px 20px;
			border-radius: 6px;
			border: 1px solid #333957;
		}
		.candidate-label.selected {
			background-color: #ff007f;
			color: #ffffff;
			border: 1px solid #ff007f;
			font-weight: bold;
		}
	`

	err = cssProvider.LoadFromData(css)
	if err != nil {
		log.Printf("Failed to load CSS: %v", err)
		return
	}

	screen, err := gdk.ScreenGetDefault()
	if err == nil {
		gtk.AddProviderForScreen(screen, cssProvider, gtk.STYLE_PROVIDER_PRIORITY_APPLICATION)
	}
}

func (c *UIController) Show() {
	c.mu.Lock()
	c.visible = true
	c.currentText = ""
	c.mu.Unlock()

	glib.IdleAdd(func() {
		c.window.ShowAll()
		c.entry.SetText("")
		c.window.Present()
		c.entry.GrabFocus()
		c.mu.Lock()
		c.candidates = nil
		c.selectedIndex = -1
		c.mu.Unlock()
		c.renderCandidates()
	})
}

func (c *UIController) Hide() {
	c.mu.Lock()
	c.visible = false
	c.mu.Unlock()

	glib.IdleAdd(func() {
		c.window.Hide()
	})
}

func (c *UIController) IsVisible() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.visible
}

func (c *UIController) GetText() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.currentText
}

func (c *UIController) UpdateCandidates(candidates []string) {
	c.mu.Lock()
	c.candidates = candidates
	c.selectedIndex = -1
	c.mu.Unlock()

	glib.IdleAdd(func() {
		c.renderCandidates()
	})
}

// renderCandidates updates the pre-allocated slot pool in-place.
// No widgets are ever created or destroyed here — focus is never disrupted.
func (c *UIController) renderCandidates() {
	c.mu.Lock()
	candidates := make([]string, len(c.candidates))
	copy(candidates, c.candidates)
	selectedIdx := c.selectedIndex
	c.mu.Unlock()

	for i, slot := range c.slots {
		if i < len(candidates) {
			slot.label.SetText(candidates[i])

			ctx, err := slot.label.GetStyleContext()
			if err == nil {
				if i == selectedIdx {
					ctx.AddClass("selected")
				} else {
					ctx.RemoveClass("selected")
				}
			}
			slot.box.ShowAll()
		} else {
			slot.box.Hide()
		}
	}
}

// applySuggestion applies a candidate via entry.SetText.
// The "changed" signal on entry fires automatically and calls OnTextChanged,
// so we do NOT call OnTextChanged explicitly (that would cause a double render).
func (c *UIController) applySuggestion(index int) {
	c.mu.Lock()
	if index < 0 || index >= len(c.candidates) {
		c.mu.Unlock()
		return
	}
	candidate := c.candidates[index]
	c.mu.Unlock()

	glib.IdleAdd(func() {
		text, _ := c.entry.GetText()
		words := strings.Fields(text)
		var newText string
		if strings.HasSuffix(text, " ") || len(words) == 0 {
			newText = strings.TrimRight(text, " ") + " " + candidate + " "
		} else {
			words[len(words)-1] = candidate
			newText = strings.Join(words, " ") + " "
		}
		// SetText fires the "changed" signal → onTextChanged → UpdateCandidates.
		c.entry.SetText(newText)
		c.entry.SetPosition(-1)
	})
}
