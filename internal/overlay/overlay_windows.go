//go:build windows

// Package overlay runs the native always-on-top overlay window for
// gorgon-session.
//
// The overlay is a compact, semi-transparent WebView2 window (the Edge
// runtime built into Windows 10/11 — a system component, not a third-party
// app) that renders the app's own web UI in overlay mode
// (http://127.0.0.1:7777/?overlay=1). It is always-on-top, excluded from the
// taskbar, doesn't steal focus on open, and can be toggled into click-through
// mode so mouse input passes through to the game underneath. Clicking an
// input field activates the window normally so typing works.
//
// Controls:
//   - Ctrl+F9 toggles click-through (the window visibly dims between the
//     configured normal and click-through opacities so the state is obvious).
//   - Esc closes the overlay.
//   - Drag the grip bar (⠿) in the top bar to move the window.
//   - The web page can close or toggle the overlay via the bound JS functions
//     window.overlayClose() and window.overlayToggleClickThrough() (both
//     return Promises).
//
// Window opacity, click-through defaults, screen corner, and web UI
// theme/accent are read from the app's /api/config at startup (see
// config.OverlaySettings); on any error the historical hardcoded appearance
// (98%/78% opacity, bottom-right) is used.
package overlay

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"github.com/jchv/go-webview2"
	"github.com/michalbasisty/gorgon-session/internal/config"
)

// ── Win32 plumbing (user32.dll; the webview2 lib handles the rest) ──────
var (
	user32 = syscall.NewLazyDLL("user32.dll")

	procGetWindowLongPtrW     = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW     = user32.NewProc("SetWindowLongPtrW")
	procSetLayeredWindowAttrs = user32.NewProc("SetLayeredWindowAttributes")
	procRegisterHotKey        = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey      = user32.NewProc("UnregisterHotKey")
	procSetWindowPos          = user32.NewProc("SetWindowPos")
	procSystemParametersInfoW = user32.NewProc("SystemParametersInfoW")
	procPostMessageW          = user32.NewProc("PostMessageW")
	procSendMessageW          = user32.NewProc("SendMessageW")
	procReleaseCapture        = user32.NewProc("ReleaseCapture")
	procCallWindowProcW       = user32.NewProc("CallWindowProcW")
	procDefWindowProcW        = user32.NewProc("DefWindowProcW")
	procGetCursorPos          = user32.NewProc("GetCursorPos")
	procGetWindowRect         = user32.NewProc("GetWindowRect")
	procEnumChildWindows      = user32.NewProc("EnumChildWindows")
)

// ── Win32 constants ─────────────────────────────────────────────────────
const (
	wmClose        = 0x0010
	wmHotKey       = 0x0312
	wmNcHitTest    = 0x0084
	wmNcLDown      = 0x00A1 // WM_NCLBUTTONDOWN
	wmExitSizeMove = 0x0232 // WM_EXITSIZEMOVE

	// htCaption tells Windows the click landed on the (imaginary) title bar,
	// starting the native move loop — how the borderless window drags.
	htCaption = 2

	// Resize codes returned from WM_NCHITTEST for a borderless window edge.
	htLeft       = 10
	htRight      = 11
	htTop        = 12
	htTopLeft    = 13
	htTopRight   = 14
	htBottom     = 15
	htBottomLeft = 16
	htBottomRight = 17

	// resizeMargin is the pixel-wide zone at each window edge where the cursor
	// triggers native resize (instead of passing through to the web content).
	resizeMargin = 8

	wsExTopmost     = 0x00000008
	wsExLayered     = 0x00080000
	wsExToolWindow  = 0x00000080
	wsExNoActivate  = 0x08000000
	wsExTransparent = 0x00000020

	swpNoSize     = 0x0001
	swpNoMove     = 0x0002
	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010

	// hwndTopmost is the HWND_TOPMOST insert-after value for SetWindowPos.
	// Setting the WS_EX_TOPMOST style bit alone does not change z-order.
	hwndTopmost = ^uintptr(0)

	lwaAlpha = 0x00000002

	modControl = 0x0002
	vkF9       = 0x78
	vkEscape   = 0x1B

	hotkeyClickThrough = 1
	hotkeyClose        = 2

	spiGetWorkArea = 0x0030

	winW = 460
	winH = 760
)

// GWLP_WNDPROC, GWL_STYLE and GWL_EXSTYLE are negative indices, so they need
// typed int32 vars (untyped negative constants don't convert to uintptr).
var (
	gwlWndProc = int32(-4)
	gwlStyle   = int32(-16)
	gwlExStyle = int32(-20)

	// WS_CAPTION|WS_SYSMENU|WS_THICKFRAME|WS_MINIMIZEBOX|WS_MAXIMIZEBOX —
	// the frame bits of WS_OVERLAPPEDWINDOW that the webview2 lib creates.
	// Cleared after creation so the overlay is borderless (no native title
	// bar; the web top bar is the drag region instead).
	wsOverlappedWindow = uintptr(0x00CF0000)

	// htTransparent is the WM_NCHITTEST result that passes mouse input to the
	// window underneath (= LRESULT -1; ^uintptr(0) is the all-bits-set form,
	// since constant conversion of a negative value to uintptr is an error).
	htTransparent = ^uintptr(0)

	// Callbacks (syscall callbacks cannot be closures). Assigned in Run —
	// package-level init would be a cycle, since the procs reference each
	// other through toggleClickThrough/subclass.
	overlayWndProcCB uintptr
	enumChildProcCB  uintptr
)

type rect struct{ Left, Top, Right, Bottom int32 }
type point struct{ X, Y int32 }

// overlay holds the state shared between Run and the wndproc callback.
type overlay struct {
	hwnd            uintptr
	clickThrough    bool
	alphaNormal     uintptr // normal-mode window alpha (0..255)
	alphaClickThru  uintptr // click-through-mode window alpha (0..255)
	resizeAfter     func(cw, ch int) // called on WM_EXITSIZEMOVE to sync the controller
}

// theOverlay routes the wndproc callback (callbacks cannot be closures).
var theOverlay *overlay

// subclassed maps every subclassed HWND (the top window plus its WebView2
// children) to the window procedure it replaced, so messages can be forwarded
// with CallWindowProcW. Only touched from the message-loop thread.
var subclassed = map[uintptr]uintptr{}

// subclass replaces hwnd's window procedure with overlayWndProc, remembering
// the previous one for forwarding. No-op if already subclassed.
func subclass(hwnd uintptr) {
	if _, ok := subclassed[hwnd]; ok {
		return
	}
	old, _, _ := procGetWindowLongPtrW.Call(hwnd, uintptr(gwlWndProc))
	subclassed[hwnd] = old
	procSetWindowLongPtrW.Call(hwnd, uintptr(gwlWndProc), overlayWndProcCB)
}

func enumChildProc(hwnd, _ uintptr) uintptr {
	subclass(hwnd)
	return 1 // keep enumerating
}

// overlayWndProc is installed on the top window AND all its children. While
// click-through is enabled, WM_NCHITTEST returns HTTRANSPARENT so every mouse
// message passes to whatever is underneath (the game). On the top-level window
// (non-click-through) a narrow edge zone returns resize hit-codes so the user
// can drag any border to resize the borderless window. The drag gesture itself
// is driven from the web page's JS (the menu bars call overlayStartDrag) so
// that buttons in the bars remain clickable.
func overlayWndProc(hwnd, uMsg, wParam, lParam uintptr) uintptr {
	o := theOverlay
	switch uMsg {
	case wmNcHitTest:
		if o == nil {
			break
		}
		if o.clickThrough {
			return uintptr(htTransparent)
		}
		if hwnd == o.hwnd {
			var pt point
			procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
			var wr rect
			procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&wr)))
			x := pt.X - wr.Left
			y := pt.Y - wr.Top
			ww := wr.Right - wr.Left
			wh := wr.Bottom - wr.Top
			m := int32(resizeMargin)
			// Corners
			if y <= m && x <= m {
				return uintptr(htTopLeft)
			}
			if y <= m && x >= ww-m {
				return uintptr(htTopRight)
			}
			if y >= wh-m && x <= m {
				return uintptr(htBottomLeft)
			}
			if y >= wh-m && x >= ww-m {
				return uintptr(htBottomRight)
			}
			// Single edges
			if x <= m {
				return uintptr(htLeft)
			}
			if x >= ww-m {
				return uintptr(htRight)
			}
			if y <= m {
				return uintptr(htTop)
			}
			if y >= wh-m {
				return uintptr(htBottom)
			}
		}

	case wmExitSizeMove:
		// Native edge-resize just ended; sync the WebView2 controller to the
		// new window size (the controller does not auto-follow borderless
		// resize).
		if o != nil && hwnd == o.hwnd && o.resizeAfter != nil {
			var wr rect
			procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&wr)))
			o.resizeAfter(int(wr.Right-wr.Left), int(wr.Bottom-wr.Top))
			return 0
		}

	case wmHotKey:
		if o != nil && hwnd == o.hwnd {
			switch wParam {
			case hotkeyClickThrough:
				o.toggleClickThrough()
			case hotkeyClose:
				procPostMessageW.Call(hwnd, wmClose, 0, 0)
			}
			return 0
		}

	case wmClose:
		if o != nil && hwnd == o.hwnd {
			o.close()
			return 0
		}
	}

	old, ok := subclassed[hwnd]
	if ok && old != 0 {
		r, _, _ := procCallWindowProcW.Call(old, hwnd, uMsg, wParam, lParam)
		return r
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uMsg, wParam, lParam)
	return r
}

// close restores the original window procedures, unregisters the hotkeys, then
// forwards WM_CLOSE to the webview2 lib's own wndproc, which destroys the
// window and terminates the message loop.
func (o *overlay) close() {
	procUnregisterHotKey.Call(o.hwnd, hotkeyClickThrough)
	procUnregisterHotKey.Call(o.hwnd, hotkeyClose)

	old := subclassed[o.hwnd]
	for hwnd, p := range subclassed {
		if p != 0 {
			procSetWindowLongPtrW.Call(hwnd, uintptr(gwlWndProc), p)
		}
		delete(subclassed, hwnd)
	}
	if old != 0 {
		procCallWindowProcW.Call(old, o.hwnd, wmClose, 0, 0)
	}
}

// startDrag starts a native window-move operation (the borderless equivalent
// of dragging the title bar): Windows enters its move loop from the current
// cursor position. Called from the web page's drag-grip mousedown.
func (o *overlay) startDrag() {
	procReleaseCapture.Call()
	procSendMessageW.Call(o.hwnd, wmNcLDown, htCaption, 0)
}

// toggleClickThrough flips click-through and makes the state visible: the
// window dims between the configured normal and click-through alphas. The
// WS_EX_TRANSPARENT bit is toggled too as belt-and-suspenders; the
// HTTRANSPARENT wndproc is the real mechanism (WS_EX_TRANSPARENT alone does
// not work across processes).
func (o *overlay) toggleClickThrough() {
	o.clickThrough = !o.clickThrough

	cur, _, _ := procGetWindowLongPtrW.Call(o.hwnd, uintptr(gwlExStyle))
	alpha := o.alphaNormal
	if o.clickThrough {
		alpha = o.alphaClickThru
		cur |= wsExTransparent
	} else {
		cur &^= wsExTransparent
	}
	procSetWindowLongPtrW.Call(o.hwnd, uintptr(gwlExStyle), cur)
	procSetLayeredWindowAttrs.Call(o.hwnd, 0, alpha, lwaAlpha)

	// Subclass any WebView2 child windows created since the last pass (the
	// renderer host appears lazily once content starts rendering).
	procEnumChildWindows.Call(o.hwnd, enumChildProcCB, 0)
}

// Run blocks in the webview2 message loop until the overlay is closed
// (Esc / WM_CLOSE / window.overlayClose()). serverURL is the base URL of the
// app's own HTTP API; the overlay renders its web UI in overlay mode.
func Run(serverURL string) error {
	if serverURL == "" {
		serverURL = "http://127.0.0.1:7777"
	}
	runtime.LockOSThread() // the window and message loop are thread-affine
	defer runtime.UnlockOSThread()

	// One-time registration of the Win32 callbacks (see var block above).
	overlayWndProcCB = syscall.NewCallback(overlayWndProc)
	enumChildProcCB = syscall.NewCallback(enumChildProc)

	w := webview2.New(false) // debug=false: no context menu, no devtools
	if w == nil {
		return errors.New("overlay: webview2 window creation failed (WebView2 runtime unavailable?)")
	}
	defer w.Destroy()

	hwnd := uintptr(w.Window())
	o := &overlay{hwnd: hwnd}
	theOverlay = o
	defer func() { theOverlay = nil }()

	// Pull the overlay appearance from the app's config. On ANY failure the
	// historical hardcoded defaults (98%/78% opacity, bottom-right, dark)
	// stand in — the overlay never crashes over a missing server.
	ov := config.Default().Overlay
	var cfgResp struct {
		Overlay config.OverlaySettings `json:"overlay"`
	}
	if err := getJSON(serverURL+"/api/config", &cfgResp); err == nil && cfgResp.Overlay.Opacity != 0 {
		ov = cfgResp.Overlay
	}
	if ov.Position == "" {
		ov.Position = "bottom-right"
	}
	if ov.Theme == "" {
		ov.Theme = "dark"
	}
	o.alphaNormal = alphaFromPercent(ov.Opacity)
	o.alphaClickThru = alphaFromPercent(ov.ClickThroughOpacity)

	w.SetSize(winW, winH, webview2.HintNone)

	// Borderless: strip the native title bar (WS_CAPTION etc. from
	// WS_OVERLAPPEDWINDOW). Must run after SetSize, which re-applies
	// WS_THICKFRAME|WS_MAXIMIZEBOX.
	curStyle, _, _ := procGetWindowLongPtrW.Call(hwnd, uintptr(gwlStyle))
	procSetWindowLongPtrW.Call(hwnd, uintptr(gwlStyle), curStyle &^ wsOverlappedWindow)

	// SetSize's AdjustWindowRect inflated the window to make room for the
	// frame; the frame is gone now, so shrink the window back to the
	// requested size (the browser controller was already sized inside
	// SetSize, so it fills the window exactly after the shrink).
	procSetWindowPos.Call(hwnd, 0, 0, 0, uintptr(winW), uintptr(winH),
		swpNoZOrder|swpNoActivate|swpNoMove)

	// Called on WM_EXITSIZEMOVE after the user finishes a native edge-resize:
	// sync the WebView2 controller to the new borderless window size.
	o.resizeAfter = func(cw, ch int) {
		if cw < 320 {
			cw = 320
		}
		if ch < 400 {
			ch = 400
		}
		w.SetSize(cw, ch, webview2.HintNone)
		cur, _, _ := procGetWindowLongPtrW.Call(hwnd, uintptr(gwlStyle))
		procSetWindowLongPtrW.Call(hwnd, uintptr(gwlStyle), cur &^ wsOverlappedWindow)
		procSetWindowPos.Call(hwnd, 0, 0, 0, uintptr(cw), uintptr(ch),
			swpNoZOrder|swpNoActivate|swpNoMove)
	}

	// Always-on-top, no taskbar entry, layered (alpha). Deliberately NOT
	// WS_EX_NOACTIVATE: that would block keyboard focus, so inputs in the
	// overlay (search boxes, settings) could never be typed into. Opening
	// still doesn't steal focus (all SetWindowPos calls use SWP_NOACTIVATE);
	// clicking an input simply activates the window like any other app.
	cur, _, _ := procGetWindowLongPtrW.Call(hwnd, uintptr(gwlExStyle))
	procSetWindowLongPtrW.Call(hwnd, uintptr(gwlExStyle),
		cur|wsExTopmost|wsExToolWindow|wsExLayered)
	procSetLayeredWindowAttrs.Call(hwnd, 0, o.alphaNormal, lwaAlpha)

	// Dock to the chosen corner of the primary work area, 12px margin — the
	// same spot as the old GDI HUD by default. The HWND_TOPMOST insert-after
	// is what actually makes the window topmost.
	var wa rect
	procSystemParametersInfoW.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&wa)), 0)
	x, y := cornerPos(ov.Position, wa)
	procSetWindowPos.Call(hwnd, hwndTopmost, uintptr(x), uintptr(y), 0, 0,
		swpNoSize|swpNoActivate)

	// Subclass the top window and its children (WebView2 creates child
	// windows that otherwise eat mouse input in click-through mode).
	subclass(hwnd)
	procEnumChildWindows.Call(hwnd, enumChildProcCB, 0)

	// Hotkeys. Failures (key in use elsewhere) are non-fatal.
	procRegisterHotKey.Call(hwnd, hotkeyClickThrough, modControl, vkF9)
	procRegisterHotKey.Call(hwnd, hotkeyClose, 0, vkEscape)

	// Bind Go functions into the page as window.overlayClose() /
	// window.overlayToggleClickThrough(). Must happen before Navigate so the
	// injected shim runs on the new document.
	_ = w.Bind("overlayClose", func() { procPostMessageW.Call(hwnd, wmClose, 0, 0) })
	_ = w.Bind("overlayToggleClickThrough", func() { o.toggleClickThrough() })
	_ = w.Bind("overlayStartDrag", func() { o.startDrag() })
	// JS-driven resize: SetSize resizes the native window AND the WebView2
	// controller together. SetSize inflates for the frame (AdjustWindowRect)
	// and re-applies WS_THICKFRAME|WS_MAXIMIZEBOX, so the borderless strip
	// runs again + the window shrinks back to the requested size (the
	// controller already matches from inside SetSize).
	_ = w.Bind("overlayResize", func(width, height int) {
		if width < 320 {
			width = 320
		}
		if height < 400 {
			height = 400
		}
		w.SetSize(width, height, webview2.HintNone)
		cur, _, _ := procGetWindowLongPtrW.Call(hwnd, uintptr(gwlStyle))
		procSetWindowLongPtrW.Call(hwnd, uintptr(gwlStyle), cur &^ wsOverlappedWindow)
		procSetWindowPos.Call(hwnd, 0, 0, 0, uintptr(width), uintptr(height),
			swpNoZOrder|swpNoActivate|swpNoMove)
	})

	// Start in click-through mode via the same toggle path so the exstyle,
	// alpha, and hotkey state stay consistent with a manual toggle.
	if ov.ClickThroughByDefault {
		o.toggleClickThrough()
	}

	if os.Getenv("GORGON_OVERLAY_SMOKE") == "1" {
		// Smoke mode: close ~2.5s after showing (allows WebView2 init) so
		// CI/terminal runs exit cleanly. Verifies create+run+close don't crash.
		go func() {
			time.Sleep(2500 * time.Millisecond)
			procPostMessageW.Call(hwnd, wmClose, 0, 0)
		}()
	}

	nav := serverURL + "/?overlay=1&theme=" + ov.Theme
	if ov.AccentColor != "" {
		nav += "&accent=" + url.QueryEscape(ov.AccentColor)
	}
	w.Navigate(nav)
	w.Run() // blocks until the window is closed

	// Belt and suspenders: the hotkeys die with the window anyway.
	procUnregisterHotKey.Call(hwnd, hotkeyClickThrough)
	procUnregisterHotKey.Call(hwnd, hotkeyClose)
	return nil
}

// getJSON fetches url and decodes its JSON body into v. Any failure returns
// an error so callers can fall back to defaults.
func getJSON(url string, v any) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("overlay: GET %s: status %d", url, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// alphaFromPercent maps an opacity percent (30..100) to a window alpha byte:
// alpha = 255 * percent / 100, clamped so the window never becomes invisible.
func alphaFromPercent(pct int) uintptr {
	if pct < 30 {
		pct = 30
	}
	if pct > 100 {
		pct = 100
	}
	return uintptr(255 * pct / 100)
}

// cornerPos returns the top-left corner of a winW x winH window docked to the
// requested corner of the primary work area with a 12px margin. Unknown
// positions fall back to bottom-right (the historical behavior).
func cornerPos(pos string, wa rect) (int32, int32) {
	const margin = 12
	right := wa.Left + (wa.Right - wa.Left) - winW - margin
	bottom := wa.Bottom - winH - margin
	switch pos {
	case "top-left":
		return wa.Left + margin, wa.Top + margin
	case "top-right":
		return right, wa.Top + margin
	case "bottom-left":
		return wa.Left + margin, bottom
	default: // "bottom-right" and anything unknown
		return right, bottom
	}
}
