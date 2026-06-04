package inject

import (
	"fmt"
	"time"

	"github.com/atotto/clipboard"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
	"github.com/jezek/xgbutil"
	"github.com/jezek/xgbutil/keybind"
)

// Paste copies the text to the clipboard and simulates Ctrl+V to paste it.
func Paste(text string) error {
	// 1. Copy text to system clipboard
	if err := clipboard.WriteAll(text); err != nil {
		return fmt.Errorf("failed to copy text to clipboard: %w", err)
	}

	// 2. Wait a moment for window focus to return to the target application
	time.Sleep(150 * time.Millisecond)

	// 3. Connect to X server and simulate Ctrl+V
	xu, err := xgbutil.NewConn()
	if err != nil {
		// Fallback: If we're on a pure Wayland environment without X11/XWayland,
		// we cannot simulate keystrokes this way. The user can still press Ctrl+V manually.
		return fmt.Errorf("X11 connection failed (manual paste fallback): %w", err)
	}
	defer xu.Conn().Close()

	keybind.Initialize(xu)
	if err := xtest.Init(xu.Conn()); err != nil {
		return fmt.Errorf("failed to initialize xtest: %w", err)
	}

	c := xu.Conn()

	// Get left Control keycode
	_, kcs, err := keybind.ParseString(xu, "Control_L")
	var ctrlKC byte = 37 // standard fallback
	if err == nil && len(kcs) > 0 {
		ctrlKC = byte(kcs[0])
	}

	// Get 'v' keycode
	_, vKcs, err := keybind.ParseString(xu, "v")
	var vKC byte = 55 // standard fallback
	if err == nil && len(vKcs) > 0 {
		vKC = byte(vKcs[0])
	}

	// Press Ctrl
	xtest.FakeInput(c, xproto.KeyPress, ctrlKC, 0, 0, 0, 0, 0)
	time.Sleep(10 * time.Millisecond)

	// Press and release 'v'
	xtest.FakeInput(c, xproto.KeyPress, vKC, 0, 0, 0, 0, 0)
	time.Sleep(10 * time.Millisecond)
	xtest.FakeInput(c, xproto.KeyRelease, vKC, 0, 0, 0, 0, 0)
	time.Sleep(10 * time.Millisecond)

	// Release Ctrl
	xtest.FakeInput(c, xproto.KeyRelease, ctrlKC, 0, 0, 0, 0, 0)

	return nil
}
