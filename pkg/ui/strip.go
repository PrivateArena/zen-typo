package ui

import (
	"fmt"
	"log"

	"github.com/gotk3/gotk3/gdk"
	"github.com/gotk3/gotk3/glib"
	"github.com/gotk3/gotk3/gtk"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil"
)

const stripSlots = 5

// StripSlot holds a single candidate chip in the ambient strip.
type stripSlot struct {
	box   *gtk.EventBox
	label *gtk.Label
}

// Strip is a slim, always-visible, focus-less GTK window that shows word
// candidates as the user types anywhere on the desktop. Clicking a chip calls
// OnSelect with the chosen word and the length of the current fragment so the
// caller can inject a replacement.
type Strip struct {
	window *gtk.Window
	slots  [stripSlots]*stripSlot

	candidates  []string
	fragmentLen int // length of current fragment (for ReplaceWord)

	// OnSelect is called (in a goroutine) when the user clicks a chip.
	// word = selected candidate, fragLen = current fragment length.
	OnSelect func(word string, fragLen int)
}

// NewStrip creates the ambient strip window. Must be called on the GTK thread.
func NewStrip() (*Strip, error) {
	win, err := gtk.WindowNew(gtk.WINDOW_TOPLEVEL)
	if err != nil {
		return nil, fmt.Errorf("strip: create window: %w", err)
	}

	win.SetTitle("zen-typo-strip")
	win.SetDecorated(false)
	win.SetSkipPagerHint(true)
	win.SetSkipTaskbarHint(true)
	win.SetKeepAbove(true)
	win.SetAcceptFocus(false) // CRITICAL: never steal focus
	win.SetTypeHint(gdk.WINDOW_TYPE_HINT_TOOLTIP)
	win.SetDefaultSize(520, 44)

	// Position: bottom-centre of primary screen using xproto (no GDK Screen API needed)
	sw, sh := screenDimensions()
	win.Move((sw-520)/2, sh-60)

	// Outer box
	outer, err := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 6)
	if err != nil {
		return nil, err
	}
	outer.SetMarginStart(10)
	outer.SetMarginEnd(10)
	outer.SetMarginTop(6)
	outer.SetMarginBottom(6)
	win.Add(outer)

	s := &Strip{window: win}

	// Pre-allocate chip slots
	for i := 0; i < stripSlots; i++ {
		box, err := gtk.EventBoxNew()
		if err != nil {
			return nil, err
		}
		box.SetName("strip-chip")

		lbl, err := gtk.LabelNew("")
		if err != nil {
			return nil, err
		}
		lbl.SetName("strip-label")
		box.Add(lbl)

		idx := i
		box.Connect("button-press-event", func() {
			s.handleClick(idx)
		})

		outer.PackStart(box, false, false, 0)
		s.slots[i] = &stripSlot{box: box, label: lbl}
	}

	// Apply CSS
	applyStripCSS(win)

	win.ShowAll()
	// Start hidden; will show once first candidates arrive
	win.Hide()

	return s, nil
}

func applyStripCSS(win *gtk.Window) {
	provider, err := gtk.CssProviderNew()
	if err != nil {
		log.Printf("[Strip] CSS provider error: %v", err)
		return
	}
	css := `
window {
	background: rgba(18, 18, 28, 0.92);
	border-radius: 22px;
	border: 1px solid rgba(130, 100, 255, 0.35);
}
#strip-chip {
	background: rgba(80, 60, 160, 0.35);
	border-radius: 16px;
	padding: 4px 14px;
	margin: 2px;
	transition: background 120ms ease;
}
#strip-chip:hover {
	background: rgba(120, 90, 220, 0.65);
}
#strip-label {
	color: #e8e0ff;
	font-family: 'Inter', 'Roboto', sans-serif;
	font-size: 23px;
	font-weight: 500;
}
`
	if err := provider.LoadFromData(css); err != nil {
		log.Printf("[Strip] CSS load error: %v", err)
		return
	}
	screen, err := gdk.ScreenGetDefault()
	if err != nil {
		return
	}
	gtk.AddProviderForScreen(screen, provider, gtk.STYLE_PROVIDER_PRIORITY_APPLICATION)
}

// UpdateCandidates refreshes the strip chips. Safe to call from any goroutine.
func (s *Strip) UpdateCandidates(candidates []string, fragmentLen int) {
	glib.IdleAdd(func() {
		s.fragmentLen = fragmentLen
		s.candidates = candidates

		for i := 0; i < stripSlots; i++ {
			slot := s.slots[i]
			if i < len(candidates) {
				slot.label.SetText(candidates[i])
				slot.box.SetVisible(true)
			} else {
				slot.label.SetText("")
				slot.box.SetVisible(false)
			}
		}

		if len(candidates) > 0 {
			s.window.ShowAll()
		} else {
			s.window.Hide()
		}
	})
}

// Hide hides the strip. Safe to call from any goroutine.
func (s *Strip) Hide() {
	glib.IdleAdd(func() { s.window.Hide() })
}

func (s *Strip) handleClick(idx int) {
	if idx >= len(s.candidates) {
		return
	}
	word := s.candidates[idx]
	flen := s.fragmentLen
	if cb := s.OnSelect; cb != nil {
		go cb(word, flen)
	}
}

// screenDimensions returns the width and height of the default X11 screen.
// Falls back to 1920×1080 if the X connection cannot be established.
func screenDimensions() (int, int) {
	xu, err := xgbutil.NewConn()
	if err != nil {
		return 1920, 1080
	}
	defer xu.Conn().Close()

	setup := xproto.Setup(xu.Conn())
	if setup == nil || len(setup.Roots) == 0 {
		return 1920, 1080
	}
	root := setup.DefaultScreen(xu.Conn())
	return int(root.WidthInPixels), int(root.HeightInPixels)
}
