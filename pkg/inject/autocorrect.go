package inject

import (
	"fmt"
	"strings"
	"time"

	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
	"github.com/jezek/xgbutil"
	"github.com/jezek/xgbutil/keybind"
)

func getAtom(xu *xgbutil.XUtil, name string) (xproto.Atom, error) {
	reply, err := xproto.InternAtom(xu.Conn(), false, uint16(len(name)), name).Reply()
	if err != nil {
		return 0, err
	}
	return reply.Atom, nil
}

// GetActiveWindowClass queries X11 for the current active window's class name(s).
func GetActiveWindowClass() (string, error) {
	xu, err := xgbutil.NewConn()
	if err != nil {
		return "", err
	}
	defer xu.Conn().Close()

	setup := xproto.Setup(xu.Conn())
	if setup == nil || len(setup.Roots) == 0 {
		return "", fmt.Errorf("no roots in X setup")
	}
	rootWin := setup.DefaultScreen(xu.Conn()).Root

	activeWinAtom, err := getAtom(xu, "_NET_ACTIVE_WINDOW")
	if err != nil {
		return "", err
	}
	reply, err := xproto.GetProperty(xu.Conn(), false, rootWin, activeWinAtom, xproto.GetPropertyTypeAny, 0, 1).Reply()
	if err != nil {
		return "", err
	}
	if reply == nil || reply.Format != 32 || len(reply.Value) < 4 {
		return "", fmt.Errorf("invalid _NET_ACTIVE_WINDOW property")
	}

	winID := xproto.Window(uint32(reply.Value[0]) | uint32(reply.Value[1])<<8 | uint32(reply.Value[2])<<16 | uint32(reply.Value[3])<<24)
	if winID == 0 {
		return "", fmt.Errorf("no active window")
	}

	wmClassAtom, err := getAtom(xu, "WM_CLASS")
	if err != nil {
		return "", err
	}
	classReply, err := xproto.GetProperty(xu.Conn(), false, winID, wmClassAtom, xproto.GetPropertyTypeAny, 0, 100).Reply()
	if err != nil {
		return "", err
	}
	if classReply == nil || len(classReply.Value) == 0 {
		return "", fmt.Errorf("invalid WM_CLASS property")
	}

	// WM_CLASS contains null-terminated strings
	// Format is typically: instance_name \0 class_name \0
	parts := strings.Split(string(classReply.Value), "\x00")
	var classes []string
	for _, p := range parts {
		if p != "" {
			classes = append(classes, p)
		}
	}
	return strings.Join(classes, ", "), nil
}

// ShouldIgnoreAutocorrect checks if any active window class name matches the ignore list.
func ShouldIgnoreAutocorrect(classesStr string, ignoreList []string) bool {
	if len(ignoreList) == 0 {
		return false
	}
	classes := strings.Split(classesStr, ", ")
	for _, cls := range classes {
		clsLower := strings.ToLower(strings.TrimSpace(cls))
		for _, ignorePattern := range ignoreList {
			ignoreLower := strings.ToLower(strings.TrimSpace(ignorePattern))
			if strings.Contains(clsLower, ignoreLower) || strings.Contains(ignoreLower, clsLower) {
				return true
			}
		}
	}
	return false
}

// ReplaceWord backspaces `wordLen+1` characters (word + the space that committed
// it) then types `replacement` followed by a space. Used by the silent
// autocorrect feature.
func ReplaceWord(wordLen int, replacement string) error {
	xu, err := xgbutil.NewConn()
	if err != nil {
		return err
	}
	defer xu.Conn().Close()

	keybind.Initialize(xu)
	if err := xtest.Init(xu.Conn()); err != nil {
		return err
	}
	c := xu.Conn()

	// Resolve backspace keycode
	_, bsKcs, _ := keybind.ParseString(xu, "BackSpace")
	var bsKC byte = 22
	if len(bsKcs) > 0 {
		bsKC = byte(bsKcs[0])
	}

	// Backspace over: word characters + 1 space
	total := wordLen + 1
	for i := 0; i < total; i++ {
		xtest.FakeInput(c, xproto.KeyPress, bsKC, 0, 0, 0, 0, 0)
		xtest.FakeInput(c, xproto.KeyRelease, bsKC, 0, 0, 0, 0, 0)
		time.Sleep(4 * time.Millisecond)
	}

	// Type replacement + space using clipboard to avoid per-char keycode lookup
	// on non-US layouts. Re-use Paste() helper which handles clipboard+Ctrl+V.
	return Paste(replacement + " ")
}
