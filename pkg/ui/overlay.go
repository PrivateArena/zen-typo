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

type UIController struct {
	window        *gtk.Window
	entry         *gtk.Entry
	candidateBox  *gtk.Box
	candidates    []string
	selectedIndex int
	visible       bool
	currentText   string

	OnTextChanged func(text string)
	OnCommit      func(text string, acceptedSuggestion bool)
	OnHide        func()

	mu sync.Mutex
}

var controller *UIController

func Start(onTextChanged func(text string), onCommit func(text string, acceptedSuggestion bool), onHide func()) (*UIController, error) {
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
	win.SetDefaultSize(650, 110)
	win.SetPosition(gtk.WIN_POS_CENTER)

	// Box layout with more spacing
	mainBox, err := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 6)
	if err != nil {
		return nil, err
	}
	mainBox.SetMarginStart(15)
	mainBox.SetMarginEnd(15)
	mainBox.SetMarginTop(10)
	mainBox.SetMarginBottom(10)
	win.Add(mainBox)

	// Text input field
	entry, err := gtk.EntryNew()
	if err != nil {
		return nil, err
	}
	entry.SetPlaceholderText("Type your word or sentence here...")
	mainBox.PackStart(entry, false, false, 0)

	// Candidates list layout
	candidateBox, err := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 8)
	if err != nil {
		return nil, err
	}
	mainBox.PackStart(candidateBox, true, true, 0)

	// Status/tips label
	tipsLabel, err := gtk.LabelNew("Tab / Up/Down: Cycle | Esc: Close | Enter: Commit")
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

	// Apply CSS Styling
	applyCSS()

	// Connect signals
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
			acceptedSuggestion := false
			if controller.selectedIndex >= 0 && controller.selectedIndex < len(controller.candidates) {
				// Commit the selected candidate instead of raw text if one is highlighted
				words := strings.Fields(text)
				if len(words) > 0 {
					words[len(words)-1] = controller.candidates[controller.selectedIndex]
					text = strings.Join(words, " ") + " "
				}
				acceptedSuggestion = true
			}
			if controller.OnCommit != nil {
				controller.OnCommit(text, acceptedSuggestion)
			}
			controller.Hide()
			return true

		case gdk.KEY_Tab, gdk.KEY_Down:
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

		case gdk.KEY_ISO_Left_Tab, gdk.KEY_Up:
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

	// Do not destroy on delete, just hide
	win.Connect("delete-event", func() bool {
		controller.Hide()
		if controller.OnHide != nil {
			controller.OnHide()
		}
		return true
	})

	// Start GTK main loop in a background thread to prevent blocking
	go func() {
		gtk.Main()
	}()

	return controller, nil
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
			font-size: 18px;
			padding: 6px 12px;
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
			font-size: 10px;
			color: #7b7b99;
			margin-top: 4px;
		}
		.candidate-label {
			font-size: 14px;
			color: #cfcfdb;
			background-color: #21253b;
			padding: 5px 12px;
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

		// Read primary selection to pre-populate text
		initialText := ""
		if clipboard, err := gtk.ClipboardGet(gdk.SELECTION_PRIMARY); err == nil {
			if text, err := clipboard.WaitForText(); err == nil {
				initialText = strings.TrimSpace(text)
			}
		}

		c.entry.SetText(initialText)
		c.window.Present()
		c.entry.GrabFocus()
		c.entry.SetPosition(-1) // Move cursor to the end of initialText

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

func (c *UIController) renderCandidates() {
	// Clear old children
	children := c.candidateBox.GetChildren()
	children.Foreach(func(item interface{}) {
		if widget, ok := item.(*gtk.Widget); ok {
			widget.Destroy()
		}
	})

	c.mu.Lock()
	defer c.mu.Unlock()

	for i, cand := range c.candidates {
		label, err := gtk.LabelNew(cand)
		if err != nil {
			continue
		}
		label.SetMarginStart(4)
		label.SetMarginEnd(4)
		
		ctx, err := label.GetStyleContext()
		if err == nil {
			ctx.AddClass("candidate-label")
			if i == c.selectedIndex {
				ctx.AddClass("selected")
			}
		}

		// Click to apply suggestion
		idx := i
		label.Connect("button-press-event", func() {
			c.applySuggestion(idx)
		})

		c.candidateBox.PackStart(label, false, false, 0)
	}
	c.candidateBox.ShowAll()
}

func (c *UIController) applySuggestion(index int) {
	if index < 0 || index >= len(c.candidates) {
		return
	}
	glib.IdleAdd(func() {
		text, _ := c.entry.GetText()
		words := strings.Fields(text)
		if len(words) > 0 {
			words[len(words)-1] = c.candidates[index]
			c.entry.SetText(strings.Join(words, " ") + " ")
			c.entry.SetPosition(-1) // Move cursor to end
		}
	})
}
