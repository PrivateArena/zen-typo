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
static Display* recordDisplay = NULL;

static int startRecord(char* displayName) {
    Display* ctrlDisplay = XOpenDisplay(displayName);
    if (!ctrlDisplay) {
        return 0;
    }
    
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
    range->device_events.last = KeyRelease;
    
    XRecordClientSpec spec = XRecordAllClients;
    ctx = XRecordCreateContext(ctrlDisplay, 0, &spec, 1, &range, 1);
    XFree(range);
    
    if (!ctx) {
        XCloseDisplay(ctrlDisplay);
        XCloseDisplay(recordDisplay);
        return 0;
    }
    
    XSync(ctrlDisplay, False);
    
    if (!XRecordEnableContextAsync(recordDisplay, ctx, recordCallback, NULL)) {
        XRecordFreeContext(ctrlDisplay, ctx);
        XCloseDisplay(ctrlDisplay);
        XCloseDisplay(recordDisplay);
        return 0;
    }
    
    // Process replies loop (blocks)
    while (recordDisplay) {
        XRecordProcessReplies(recordDisplay);
    }
    
    return 1;
}

static void stopRecord(char* displayName) {
    if (ctx) {
        Display* ctrlDisplay = XOpenDisplay(displayName);
        if (ctrlDisplay) {
            XRecordDisableContext(ctrlDisplay, ctx);
            XRecordFreeContext(ctrlDisplay, ctx);
            XCloseDisplay(ctrlDisplay);
        }
        ctx = 0;
    }
    if (recordDisplay) {
        XCloseDisplay(recordDisplay);
        recordDisplay = NULL;
    }
}

static int getCtrlKeycode(char* displayName) {
    Display* d = XOpenDisplay(displayName);
    if (!d) return 37; // fallback
    int kc = XKeysymToKeycode(d, XK_Control_L);
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
	triggerCallback func()
	ctrlKeycode     int
	lastPressTime   time.Time
	ctrlPressed     bool
	mu              sync.Mutex
)

//export goKeyCallback
func goKeyCallback(keycode C.int, isPress C.int) {
	mu.Lock()
	defer mu.Unlock()

	targetKey := int(keycode) == ctrlKeycode

	if targetKey {
		if isPress == 1 {
			if !ctrlPressed {
				ctrlPressed = true
				now := time.Now()
				if now.Sub(lastPressTime) < 300*time.Millisecond {
					if triggerCallback != nil {
						// Trigger the callback in a separate goroutine
						go triggerCallback()
					}
					// Reset timer to prevent triple-taps from triggering twice
					lastPressTime = time.Time{}
				} else {
					lastPressTime = now
				}
			}
		} else {
			ctrlPressed = false
		}
	} else {
		// If any other key is pressed/released, reset the double-Ctrl sequence
		if isPress == 1 {
			lastPressTime = time.Time{}
		}
	}
}

// Listen starts monitoring for double-Ctrl hotkey triggers asynchronously.
func Listen(callback func()) {
	triggerCallback = callback

	displayStr := os.Getenv("DISPLAY")
	if displayStr == "" {
		displayStr = ":0"
	}
	cDisplayStr := C.CString(displayStr)
	ctrlKeycode = int(C.getCtrlKeycode(cDisplayStr))

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

