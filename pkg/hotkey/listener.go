package hotkey

/*
#cgo LDFLAGS: -lX11 -lXtst

#include <X11/Xlib.h>
#include <X11/extensions/record.h>
#include <X11/keysym.h>
#include <stdlib.h>

extern void goKeyCallback(int keycode, int isPress);

static void recordCallback(XPointer userData, XRecordInterceptData* data) {
    if (data->category == XRecordFromServer) {
        unsigned char* eventData = data->data;
        int type = eventData[0] & 0x7F;
        int keycode = eventData[1];

        if (type == KeyPress) {
            goKeyCallback(keycode, 1);
        } else if (type == KeyRelease) {
            goKeyCallback(keycode, 0);
        }
    }
    XRecordFreeData(data);
}

static XRecordContext ctx = 0;
static Display*      recordDisplay = NULL;

static int startRecord(char* displayName) {
    // ctrlDisplay: used for XRecordCreateContext + XSync only.
    // Closed by startRecord() itself after XRecordEnableContext returns.
    Display* ctrlDisplay = XOpenDisplay(displayName);
    if (!ctrlDisplay) return 0;

    recordDisplay = XOpenDisplay(displayName);
    if (!recordDisplay) {
        XCloseDisplay(ctrlDisplay);
        return 0;
    }

    XRecordRange* range = XRecordAllocRange();
    if (!range) {
        XCloseDisplay(ctrlDisplay);
        XCloseDisplay(recordDisplay);
        return 0;
    }

    range->device_events.first = KeyPress;
    range->device_events.last  = KeyRelease;

    XRecordClientSpec spec = XRecordAllClients;
    ctx = XRecordCreateContext(ctrlDisplay, 0, &spec, 1, &range, 1);
    XFree(range);

    if (!ctx) {
        XCloseDisplay(ctrlDisplay);
        XCloseDisplay(recordDisplay);
        return 0;
    }

    XSync(ctrlDisplay, False);

    // ── BLOCKING CALL ─────────────────────────────────────────────────────────
    // XRecordEnableContext (synchronous) parks on the kernel X11 socket fd.
    // CPU usage: 0% when no key is pressed.
    // Returns only after stopRecord() calls XRecordDisableContext() on its own
    // separate connection (stopCtrl). ctx is freed by stopRecord() before we
    // resume here, so we must NOT touch ctx after this point.
    // ──────────────────────────────────────────────────────────────────────────
    XRecordEnableContext(recordDisplay, ctx, recordCallback, NULL);

    // Cleanup after unblocked — stopRecord has already freed ctx.
    XCloseDisplay(recordDisplay);
    recordDisplay = NULL;
    XCloseDisplay(ctrlDisplay);
    return 1;
}

static void stopRecord(char* displayName) {
    if (ctx) {
        // Open a short-lived control connection to send the disable command
        // while startRecord's goroutine is still blocked inside XRecordEnableContext.
        Display* stopCtrl = XOpenDisplay(displayName);
        if (stopCtrl) {
            XRecordDisableContext(stopCtrl, ctx);
            XSync(stopCtrl, False);
            XRecordFreeContext(stopCtrl, ctx);
            XCloseDisplay(stopCtrl);
        }
        ctx = 0;
        // recordDisplay is closed by startRecord() after Enable returns.
    }
}

static int getKeysymKeycode(char* displayName, KeySym keysym) {
    Display* d = XOpenDisplay(displayName);
    if (!d) return 0;
    int kc = XKeysymToKeycode(d, keysym);
    XCloseDisplay(d);
    return kc;
}
*/
import "C"
import (
	"os"
	"sync"
	"time"
	"unsafe"
)

var (
	triggerCtrlCallback  func()
	triggerAltCallback   func()
	triggerShiftCallback func()
	keyEventCallback     func(keycode int, isPress bool) // raw feed for word tracker

	ctrlKeycode  int
	altKeycode   int
	shiftKeycode int

	lastCtrlPressTime  time.Time
	lastAltPressTime   time.Time
	lastShiftPressTime time.Time

	ctrlPressed  bool
	altPressed   bool
	shiftPressed bool

	mu            sync.Mutex
	suppressUntil time.Time // ignore synthetic XTest events during injection
)

//export goKeyCallback
func goKeyCallback(keycode C.int, isPress C.int) {
	// Raw feed — called before the double-modifier lock so the tracker
	// can process every key independently. Skipped during injection.
	if cb := keyEventCallback; cb != nil {
		mu.Lock()
		suppressed := time.Now().Before(suppressUntil)
		mu.Unlock()
		kc := int(keycode)
		// Always allow modifier keys to bypass suppression so their state in the tracker stays in sync
		isModifier := kc == 37 || kc == 105 || kc == 50 || kc == 62 || kc == 64 || kc == 108 || kc == 133 || kc == 134
		if !suppressed || isModifier {
			cb(kc, isPress == 1)
		}
	}

	mu.Lock()
	defer mu.Unlock()

	kc := int(keycode)

	handleDoubleTap := func(targetKey bool, pressed *bool, lastPressTime *time.Time, onTrigger func()) {
		if targetKey {
			if isPress == 1 {
				if !*pressed {
					*pressed = true
					now := time.Now()
					if now.Sub(*lastPressTime) < 300*time.Millisecond {
						if onTrigger != nil {
							go onTrigger()
						}
						*lastPressTime = time.Time{}
					} else {
						*lastPressTime = now
					}
				}
			} else {
				*pressed = false
			}
		} else {
			if isPress == 1 {
				*lastPressTime = time.Time{}
			}
		}
	}

	handleDoubleTap(kc == ctrlKeycode, &ctrlPressed, &lastCtrlPressTime, triggerCtrlCallback)
	handleDoubleTap(kc == altKeycode, &altPressed, &lastAltPressTime, triggerAltCallback)
	handleDoubleTap(kc == shiftKeycode, &shiftPressed, &lastShiftPressTime, triggerShiftCallback)
}

// SetKeyEventCallback registers a callback that receives every raw key event.
// Call before Listen. Safe to set once at startup.
func SetKeyEventCallback(cb func(keycode int, isPress bool)) {
	keyEventCallback = cb
}

// Suppress blocks the raw key callback for the given duration.
// Call this before injecting synthetic keystrokes to prevent autocorrect loops.
func Suppress(d time.Duration) {
	mu.Lock()
	suppressUntil = time.Now().Add(d)
	mu.Unlock()
}

// Listen starts monitoring for double-Ctrl, double-Alt, and double-Shift triggers asynchronously.
func Listen(onCtrl, onAlt, onShift func()) {
	triggerCtrlCallback = onCtrl
	triggerAltCallback = onAlt
	triggerShiftCallback = onShift

	displayStr := os.Getenv("DISPLAY")
	if displayStr == "" {
		displayStr = ":0"
	}
	cDisplayStr := C.CString(displayStr)
	ctrlKeycode = int(C.getKeysymKeycode(cDisplayStr, C.XK_Control_L))
	altKeycode = int(C.getKeysymKeycode(cDisplayStr, C.XK_Alt_L))
	shiftKeycode = int(C.getKeysymKeycode(cDisplayStr, C.XK_Shift_L))

	go func() {
		defer C.free(unsafe.Pointer(cDisplayStr))
		C.startRecord(cDisplayStr)
	}()
}

// Stop stops the hotkey monitoring.
func Stop() {
	displayStr := os.Getenv("DISPLAY")
	if displayStr == "" {
		displayStr = ":0"
	}
	cDisplayStr := C.CString(displayStr)
	defer C.free(unsafe.Pointer(cDisplayStr))

	C.stopRecord(cDisplayStr)
}
