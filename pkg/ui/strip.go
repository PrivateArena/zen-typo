package ui

import (
	"image"
	"log"
	"sync"

	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil"
	"github.com/jezek/xgbutil/xevent"
	"github.com/jezek/xgbutil/xgraphics"
	"github.com/jezek/xgbutil/xwindow"
)

type Strip struct {
	X           *xgbutil.XUtil
	win         *xwindow.Window
	candidates  []string
	fragmentLen int
	chipBoxes   []image.Rectangle
	visible     bool
	mu          sync.Mutex

	OnSelect func(word string, fragLen int)
}

func NewStrip() (*Strip, error) {
	xu, err := getX()
	if err != nil {
		return nil, err
	}

	win, err := xwindow.Generate(xu)
	if err != nil {
		return nil, err
	}

	sw, sh := screenDimensions()
	winWidth, winHeight := 520, 44
	x := (sw - winWidth) / 2
	y := sh - 60

	err = win.CreateChecked(
		xu.RootWin(),
		x, y,
		winWidth, winHeight,
		xproto.CwBackPixel|xproto.CwOverrideRedirect|xproto.CwEventMask,
		0x12121C, // background color
		1,        // override redirect
		xproto.EventMaskExposure|xproto.EventMaskButtonPress,
	)
	if err != nil {
		return nil, err
	}

	s := &Strip{
		X:   xu,
		win: win,
	}

	// Connect exposure handler
	xevent.ExposeFun(func(X *xgbutil.XUtil, ev xevent.ExposeEvent) {
		s.mu.Lock()
		s.renderLocked()
		s.mu.Unlock()
	}).Connect(xu, win.Id)

	// Connect button press handler (click)
	xevent.ButtonPressFun(func(X *xgbutil.XUtil, ev xevent.ButtonPressEvent) {
		s.mu.Lock()
		x := int(ev.EventX)
		s.handleMouseClick(x)
		s.mu.Unlock()
	}).Connect(xu, win.Id)

	return s, nil
}

func (s *Strip) UpdateCandidates(candidates []string, fragmentLen int) {
	s.mu.Lock()
	s.candidates = candidates
	s.fragmentLen = fragmentLen
	if len(candidates) > 0 {
		s.visible = true
		s.renderLocked()
	} else {
		s.visible = false
		s.win.Unmap()
	}
	s.mu.Unlock()
}

func (s *Strip) Hide() {
	s.mu.Lock()
	s.visible = false
	s.win.Unmap()
	s.mu.Unlock()
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

func (s *Strip) SelectCandidate(idx int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if idx >= 0 && idx < len(s.candidates) {
		s.handleClick(idx)
		return true
	}
	return false
}

func (s *Strip) handleMouseClick(x int) {
	for i, r := range s.chipBoxes {
		if x >= r.Min.X && x <= r.Max.X {
			s.handleClick(i)
			break
		}
	}
}

func (s *Strip) renderLocked() {
	if !s.visible || len(s.candidates) == 0 {
		s.win.Unmap()
		return
	}

	winWidth, winHeight := 520, 44
	img := xgraphics.New(s.X, image.Rect(0, 0, winWidth, winHeight))

	// Background: rgba(18, 18, 28, 0.92)
	bg := xgraphics.BGRA{R: 0x12, G: 0x12, B: 0x1C, A: 0xFF}
	img.For(func(x, y int) xgraphics.BGRA {
		return bg
	})

	// Border: rgba(130, 100, 255, 0.35)
	borderColor := xgraphics.BGRA{R: 130, G: 100, B: 255, A: 0xFF}
	drawBorder(img, 0, 0, winWidth, winHeight, 1, borderColor)

	font, err := loadTTFFont()
	if err != nil {
		log.Printf("[Strip] Render error loading font: %v", err)
		return
	}

	s.chipBoxes = nil
	xOffset := 10

	for i, cand := range s.candidates {
		if i >= 5 {
			break
		}
		chipText := cand
		w, h := xgraphics.TextMaxExtents(font, 14, chipText)
		boxWidth := w + 35
		boxHeight := h + 8

		rect := image.Rect(xOffset, 8, xOffset+boxWidth, 8+boxHeight)
		s.chipBoxes = append(s.chipBoxes, rect)

		// Chip background
		chipBg := xgraphics.BGRA{R: 80, G: 60, B: 160, A: 0xFF}
		drawRect(img, rect.Min.X, rect.Min.Y, rect.Max.X, rect.Max.Y, chipBg)

		// Draw selection index in pink, candidate in light-purple
		indexText := cand
		pinkColor := xgraphics.BGRA{R: 0xFF, G: 0x00, B: 0x7F, A: 0xFF}
		whiteColor := xgraphics.BGRA{R: 0xE8, G: 0xE0, B: 0xFF, A: 0xFF}

		// Text: "1" in pink, " cand" in white
		ix, _, _ := img.Text(xOffset+10, 8+4, pinkColor, 14, font, string('1'+rune(i)))
		_, _, _ = img.Text(ix+4, 8+4, whiteColor, 14, font, " "+indexText)

		xOffset += boxWidth + 6
	}

	_ = img.CreatePixmap()
	img.XDraw()
	img.XExpPaint(s.win.Id, 0, 0)
	img.Destroy()

	s.win.Map()
}

func screenDimensions() (int, int) {
	xu, err := getX()
	if err != nil {
		return 1920, 1080
	}
	setup := xproto.Setup(xu.Conn())
	if setup == nil || len(setup.Roots) == 0 {
		return 1920, 1080
	}
	root := setup.DefaultScreen(xu.Conn())
	return int(root.WidthInPixels), int(root.HeightInPixels)
}
