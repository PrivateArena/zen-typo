package inject

import (
	"time"

	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
	"github.com/jezek/xgbutil"
	"github.com/jezek/xgbutil/keybind"
)

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
