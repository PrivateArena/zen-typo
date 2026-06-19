package ui

import (
	"fmt"
	"image"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/BurntSushi/freetype-go/freetype/truetype"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil"
	"github.com/jezek/xgbutil/keybind"
	"github.com/jezek/xgbutil/xevent"
	"github.com/jezek/xgbutil/xgraphics"
	"github.com/jezek/xgbutil/xwindow"
)

type UIController struct {
	X                     *xgbutil.XUtil
	win                   *xwindow.Window
	visible               bool
	currentText           string
	candidates            []string
	selectedIndex         int
	sentences             []string
	selectedSentenceIndex int

	OnTextChanged func(text string)
	OnCommit      func(text string)
	OnHide        func()

	mu sync.Mutex
}

var (
	X   *xgbutil.XUtil
	xmu sync.Mutex
)

func getX() (*xgbutil.XUtil, error) {
	xmu.Lock()
	defer xmu.Unlock()
	if X != nil {
		return X, nil
	}
	var err error
	X, err = xgbutil.NewConn()
	if err != nil {
		return nil, err
	}
	keybind.Initialize(X)
	go xevent.Main(X)
	return X, nil
}

func loadTTFFont() (*truetype.Font, error) {
	paths := []string{
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
		"/usr/share/fonts/truetype/liberation/LiberationSans-Regular.ttf",
		"/usr/share/fonts/TTF/DejaVuSans.ttf",
		"/usr/share/fonts/truetype/freefont/FreeSans.ttf",
	}
	var fontBytes []byte
	var err error
	for _, p := range paths {
		fontBytes, err = os.ReadFile(p)
		if err == nil {
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("could not find any system TTF font: %w", err)
	}
	return truetype.Parse(fontBytes)
}

func Start(onTextChanged func(text string), onCommit func(text string), onHide func()) (*UIController, error) {
	xu, err := getX()
	if err != nil {
		return nil, err
	}

	win, err := xwindow.Generate(xu)
	if err != nil {
		return nil, err
	}

	sw, sh := screenDimensions()
	winWidth, winHeight := 800, 360
	x := (sw - winWidth) / 2
	y := (sh - winHeight) / 2

	err = win.CreateChecked(
		xu.RootWin(),
		x, y,
		winWidth, winHeight,
		xproto.CwBackPixel|xproto.CwOverrideRedirect|xproto.CwEventMask,
		0x161624, // background color
		1,        // override redirect
		xproto.EventMaskExposure|xproto.EventMaskKeyPress,
	)
	if err != nil {
		return nil, err
	}

	c := &UIController{
		X:             xu,
		win:           win,
		OnTextChanged: onTextChanged,
		OnCommit:      onCommit,
		OnHide:        onHide,
	}

	// Connect exposure handler
	xevent.ExposeFun(func(X *xgbutil.XUtil, ev xevent.ExposeEvent) {
		c.mu.Lock()
		c.renderLocked()
		c.mu.Unlock()
	}).Connect(xu, win.Id)

	// Connect key press handler
	xevent.KeyPressFun(func(X *xgbutil.XUtil, ev xevent.KeyPressEvent) {
		keyStr := keybind.LookupString(X, ev.State, ev.Detail)
		isShift := (ev.State & xproto.ModMaskShift) != 0
		c.handleKeyPress(keyStr, isShift)
	}).Connect(xu, win.Id)

	return c, nil
}

func (c *UIController) Show() {
	c.mu.Lock()
	c.visible = true
	c.currentText = ""
	c.candidates = nil
	c.selectedIndex = -1
	c.sentences = nil
	c.selectedSentenceIndex = -1
	c.win.Map()
	c.win.Focus()
	c.renderLocked()
	c.mu.Unlock()
}

func (c *UIController) Hide() {
	c.mu.Lock()
	c.hideLocked()
	c.mu.Unlock()
}

func (c *UIController) hideLocked() {
	c.visible = false
	c.win.Unmap()
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
	c.renderLocked()
	c.mu.Unlock()
}

func (c *UIController) UpdateSentences(sentences []string) {
	c.mu.Lock()
	c.sentences = sentences
	c.selectedSentenceIndex = -1
	c.renderLocked()
	c.mu.Unlock()
}

func (c *UIController) handleKeyPress(keyStr string, isShift bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.visible {
		return
	}

	switch keyStr {
	case "Escape":
		c.hideLocked()
		if c.OnHide != nil {
			go c.OnHide()
		}
	case "Return", "KP_Enter":
		text := c.currentText
		if c.selectedIndex >= 0 && c.selectedIndex < len(c.candidates) {
			words := strings.Fields(text)
			if len(words) > 0 {
				words[len(words)-1] = c.candidates[c.selectedIndex]
				text = strings.Join(words, " ") + " "
			}
		}
		c.hideLocked()
		if c.OnCommit != nil {
			go c.OnCommit(text)
		}
	case "Tab":
		if isShift {
			idx := c.selectedSentenceIndex
			if idx < 0 && len(c.sentences) > 0 {
				idx = 0
			}
			if idx >= 0 && idx < len(c.sentences) {
				c.applySentenceLocked(idx)
			}
		} else {
			idx := c.selectedIndex
			if idx < 0 && len(c.candidates) > 0 {
				idx = 0
			}
			if idx >= 0 && idx < len(c.candidates) {
				c.applySuggestionLocked(idx)
			}
		}
	case "ISO_Left_Tab":
		idx := c.selectedSentenceIndex
		if idx < 0 && len(c.sentences) > 0 {
			idx = 0
		}
		if idx >= 0 && idx < len(c.sentences) {
			c.applySentenceLocked(idx)
		}
	case "BackSpace":
		if len(c.currentText) > 0 {
			runes := []rune(c.currentText)
			c.currentText = string(runes[:len(runes)-1])
			if c.OnTextChanged != nil {
				go c.OnTextChanged(c.currentText)
			}
			c.renderLocked()
		}
	case "Down":
		if isShift {
			if len(c.sentences) > 0 {
				c.selectedSentenceIndex++
				if c.selectedSentenceIndex >= len(c.sentences) {
					c.selectedSentenceIndex = 0
				}
				c.renderLocked()
			}
		} else {
			if len(c.candidates) > 0 {
				c.selectedIndex++
				if c.selectedIndex >= len(c.candidates) {
					c.selectedIndex = 0
				}
				c.renderLocked()
			}
		}
	case "Up":
		if isShift {
			if len(c.sentences) > 0 {
				c.selectedSentenceIndex--
				if c.selectedSentenceIndex < 0 {
					c.selectedSentenceIndex = len(c.sentences) - 1
				}
				c.renderLocked()
			}
		} else {
			if len(c.candidates) > 0 {
				c.selectedIndex--
				if c.selectedIndex < 0 {
					c.selectedIndex = len(c.candidates) - 1
				}
				c.renderLocked()
			}
		}
	case "space":
		c.currentText += " "
		if c.OnTextChanged != nil {
			go c.OnTextChanged(c.currentText)
		}
		c.renderLocked()
	default:
		runes := []rune(keyStr)
		if len(runes) == 1 && runes[0] >= 32 && runes[0] < 127 {
			c.currentText += keyStr
			if c.OnTextChanged != nil {
				go c.OnTextChanged(c.currentText)
			}
			c.renderLocked()
		}
	}
}

func (c *UIController) applySuggestionLocked(index int) {
	if index < 0 || index >= len(c.candidates) {
		return
	}
	candidate := c.candidates[index]
	words := strings.Fields(c.currentText)
	var newText string
	if strings.HasSuffix(c.currentText, " ") || len(words) == 0 {
		newText = strings.TrimRight(c.currentText, " ") + " " + candidate + " "
	} else {
		words[len(words)-1] = candidate
		newText = strings.Join(words, " ") + " "
	}
	c.currentText = newText
	if c.OnTextChanged != nil {
		go c.OnTextChanged(newText)
	}
	c.renderLocked()
}

func (c *UIController) applySentenceLocked(index int) {
	if index < 0 || index >= len(c.sentences) {
		return
	}
	sentence := c.sentences[index]
	var newText string
	if strings.HasSuffix(c.currentText, " ") || c.currentText == "" {
		newText = c.currentText + sentence + " "
	} else {
		newText = c.currentText + " " + sentence + " "
	}
	c.currentText = newText
	if c.OnTextChanged != nil {
		go c.OnTextChanged(newText)
	}
	c.renderLocked()
}

func (c *UIController) renderLocked() {
	if !c.visible {
		return
	}

	winWidth, winHeight := 800, 360
	img := xgraphics.New(c.X, image.Rect(0, 0, winWidth, winHeight))

	// Background
	bg := xgraphics.BGRA{R: 0x16, G: 0x16, B: 0x24, A: 0xFF}
	img.For(func(x, y int) xgraphics.BGRA {
		return bg
	})

	// Border
	borderColor := xgraphics.BGRA{R: 0x00, G: 0xF0, B: 0xFF, A: 0xFF}
	drawBorder(img, 0, 0, winWidth, winHeight, 3, borderColor)

	// Font
	font, err := loadTTFFont()
	if err != nil {
		log.Printf("[UI] Render error loading font: %v", err)
		return
	}

	// 1. Draw input box
	inputBg := xgraphics.BGRA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	inputBorder := xgraphics.BGRA{R: 0xFF, G: 0x00, B: 0x7F, A: 0xFF}
	drawRect(img, 25, 20, 775, 75, inputBg)
	drawBorder(img, 25, 20, 775, 75, 1, inputBorder)

	// Draw text in input box
	inputText := c.currentText
	textColor := xgraphics.BGRA{R: 0x00, G: 0x00, B: 0x00, A: 0xFF}
	if inputText == "" {
		inputText = "Type your word or sentence here..."
		textColor = xgraphics.BGRA{R: 0x88, G: 0x88, B: 0x88, A: 0xFF}
	}
	_, _, _ = img.Text(40, 32, textColor, 24, font, inputText)

	// 2. Draw candidates row (y = 95 to 145)
	xOffset := 25
	for i, cand := range c.candidates {
		if i >= 7 {
			break
		}
		candText := fmt.Sprintf("%d: %s", i+1, cand)
		w, h := xgraphics.TextMaxExtents(font, 18, candText)
		boxWidth := w + 30
		boxHeight := h + 16

		var boxBg, boxText xgraphics.BGRA
		if i == c.selectedIndex {
			boxBg = xgraphics.BGRA{R: 0xFF, G: 0x00, B: 0x7F, A: 0xFF}
			boxText = xgraphics.BGRA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
		} else {
			boxBg = xgraphics.BGRA{R: 0x21, G: 0x25, B: 0x3B, A: 0xFF}
			boxText = xgraphics.BGRA{R: 0xCF, G: 0xCF, B: 0xDB, A: 0xFF}
		}

		drawRect(img, xOffset, 95, xOffset+boxWidth, 95+boxHeight, boxBg)
		drawBorder(img, xOffset, 95, xOffset+boxWidth, 95+boxHeight, 1, xgraphics.BGRA{R: 0x33, G: 0x39, B: 0x57, A: 0xFF})
		_, _, _ = img.Text(xOffset+15, 95+8, boxText, 18, font, candText)

		xOffset += boxWidth + 10
	}

	// 3. Draw sentences (y = 160 to 280)
	yOffset := 160
	for i, sent := range c.sentences {
		if i >= 3 {
			break
		}
		w, h := xgraphics.TextMaxExtents(font, 16, sent)
		boxWidth := w + 30
		boxHeight := h + 12

		var boxBg, boxText xgraphics.BGRA
		if i == c.selectedSentenceIndex {
			boxBg = xgraphics.BGRA{R: 0x00, G: 0xF0, B: 0xFF, A: 0xFF}
			boxText = xgraphics.BGRA{R: 0x00, G: 0x00, B: 0x00, A: 0xFF}
		} else {
			boxBg = xgraphics.BGRA{R: 0x1E, G: 0x1E, B: 0x2F, A: 0xFF}
			boxText = xgraphics.BGRA{R: 0xA3, G: 0xA3, B: 0xC2, A: 0xFF}
		}

		drawRect(img, 25, yOffset, 25+boxWidth, yOffset+boxHeight, boxBg)
		drawBorder(img, 25, yOffset, 25+boxWidth, yOffset+boxHeight, 1, xgraphics.BGRA{R: 0x2E, G: 0x2E, B: 0x4A, A: 0xFF})
		_, _, _ = img.Text(40, yOffset+6, boxText, 16, font, sent)

		yOffset += boxHeight + 8
	}

	// 4. Draw tips label
	tipsText := "Tab: Apply word  |  Up/Down: Cycle words  |  Shift+Up/Down: Cycle sentences  |  Enter: Paste"
	tipsColor := xgraphics.BGRA{R: 0x7B, G: 0x7B, B: 0x99, A: 0xFF}
	_, _, _ = img.Text(25, 320, tipsColor, 12, font, tipsText)

	// Draw on window
	_ = img.CreatePixmap()
	img.XDraw()
	img.XExpPaint(c.win.Id, 0, 0)
	img.Destroy()
}

func drawRect(img *xgraphics.Image, x1, y1, x2, y2 int, clr xgraphics.BGRA) {
	for y := y1; y < y2; y++ {
		if y < 0 || y >= img.Bounds().Max.Y {
			continue
		}
		for x := x1; x < x2; x++ {
			if x < 0 || x >= img.Bounds().Max.X {
				continue
			}
			img.SetBGRA(x, y, clr)
		}
	}
}

func drawBorder(img *xgraphics.Image, x1, y1, x2, y2 int, thickness int, clr xgraphics.BGRA) {
	drawRect(img, x1, y1, x2, y1+thickness, clr)
	drawRect(img, x1, y2-thickness, x2, y2, clr)
	drawRect(img, x1, y1, x1+thickness, y2, clr)
	drawRect(img, x2-thickness, y1, x2, y2, clr)
}
