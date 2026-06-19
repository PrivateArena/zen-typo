# Engineering Learnings & Failure Solutions

This document serves as a persistent record of architectural mistakes, failure modes, and their proven solutions within the `zen-typo` engine. Refer to these rules to avoid regressions during future development.

---

## 1. X11 Input Simulation (Dropped/Merged Events)

### The Failure
When injecting raw keystrokes via `xgb/xtest.FakeInput`, sending multiple events (such as multiple Backspaces followed by replacement characters) causes characters to double-type or fail to delete (e.g., typing `"wwant"` instead of `"want"`).

### The Cause
1. **Physical Key Interleaving:** If simulated typing is too slow (e.g., due to sleeping 10ms between Press and Release and calling `c.Sync()`), the user's physical key actions (like releasing the Space bar or typing the next key) will interleave between the simulated KeyPress and KeyRelease. This corrupts the modifier/key state queue in the X server or the target application's event loop.
2. **Atomic Pair Requirement:** A simulated keypress and keyrelease for the same key must be sent together in the same batch (without intermediate `Sync()` or delays) so that the X server processes them atomically, preventing any physical key event from interleaving between them.

### The Solution
* **Atomic Press/Release Pairs:** Send `KeyPress` and `KeyRelease` for a key sequentially with zero delay and no intermediate `Sync()`.
* **Short Inter-Key Delay:** Use a small delay (e.g., `5ms`) only *after* releasing a key and before starting the next character.
* **Sync at the End:** Call `c.Sync()` once at the very end of the sequence to commit all queued events to the X server.

```go
// Correct pattern for robust keypress simulation:
xtest.FakeInput(c, xproto.KeyPress, keycode, 0, 0, 0, 0, 0)
xtest.FakeInput(c, xproto.KeyRelease, keycode, 0, 0, 0, 0, 0)
time.Sleep(5 * time.Millisecond)
```

---

## 2. Autocorrect Modifiers State Contamination

### The Failure
During rapid typing, the user may hold down modifier keys physically (e.g., `Shift`, `Alt`, `Control`). If simulated typing is injected while these keys are pressed, the X server will combine them (e.g., typing capital letters, running window manager shortcuts, or triggering copy-paste).

### The Solution
Before executing any simulated keystroke loop:
1. Query and resolve the keycodes for `Alt_L`, `Alt_R`, `Control_L`, `Control_R`, `Shift_L`, `Shift_R`, `Super_L`, and `Super_R`.
2. Explicitly send simulated `KeyRelease` events for all of these modifiers.
3. Call `c.Sync()` to release them.

---

## 3. Path Resolution in Development Hot-Reloaders

### The Failure
When running the daemon via development tools like `air` or `go run`, the executable is compiled and run from a temporary directory (e.g., `/tmp` or `/tmp/bin`). The binary-relative path resolution defaults to looking for `config.json` and `ngrams.db` in `/tmp`, fails, and initializes with an empty configuration.

### The Solution
* **Prioritize Current Working Directory (CWD):** The loader must check the CWD first.
* **Filter Temporary Directories:** Add helper functions to identify if the current executable path contains substring patterns like `/tmp`, `go-build`, or `Temp` and ignore them.

---

## 4. Keypress Suppression Gates & Simulation Latencies

### The Failure
Autocompletion triggers simulated keystrokes, which the XRecord listener intercepts. If not suppressed, these synthetic events will feed back into the word tracker, triggering infinite loops or double-triggering autocorrect.
* If suppression duration is too high (`400ms-500ms`), it blocks subsequent real keystrokes from the user.
* If suppression is too low or not synchronized, it leaks letters.
* If events are sent too close together (e.g. without any delay or less than 5ms), target applications' event loops may merge or drop KeyPress/KeyRelease events.
* Backspaces trigger heavier UI updates in target editors (layout re-calculations, autocompletion widget updates) than regular character insertions, making them highly susceptible to being dropped if fired too quickly.

### The Solution
* Set the suppression window to exactly `250ms` to cover the entire replacement sequence.
* Introduce a `30ms` settlement delay at the start of replacement to allow physical keyboard state to settle.
* Use a `5ms` KeyPress-to-KeyRelease delay and `5ms` spacing for typing regular characters.
* Use a larger `15ms` KeyPress-to-KeyRelease delay and `15ms` spacing for backspaces to accommodate editor layout and spellcheck latency.
* Maintain a `50ms` window transition pause after the backspacing loop.
* Ensure that the suppress timer `suppressUntil` is checked atomically inside the raw keycallback.
