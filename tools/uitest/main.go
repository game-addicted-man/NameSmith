//go:build windows

// Command uitest drives NameSmith on a real Windows desktop. CI runs it after
// every build: it makes sample files, types into the window, renames them,
// checks the result on disk, undoes it, and saves screenshots along the way.
//
//	go run ./tools/uitest -exe dist\NameSmith.exe -out dist
package main

import (
	"errors"
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                 = windows.NewLazySystemDLL("user32.dll")
	gdi32                  = windows.NewLazySystemDLL("gdi32.dll")
	procFindWindow         = user32.NewProc("FindWindowW")
	procEnumChildWindows   = user32.NewProc("EnumChildWindows")
	procGetClassName       = user32.NewProc("GetClassNameW")
	procGetWindowText      = user32.NewProc("GetWindowTextW")
	procIsWindowVisible    = user32.NewProc("IsWindowVisible")
	procIsWindowEnabled    = user32.NewProc("IsWindowEnabled")
	procSendMessage        = user32.NewProc("SendMessageW")
	procPostMessage        = user32.NewProc("PostMessageW")
	procGetWindowRect      = user32.NewProc("GetWindowRect")
	procGetSystemMetrics   = user32.NewProc("GetSystemMetrics")
	procGetDC              = user32.NewProc("GetDC")
	procReleaseDC          = user32.NewProc("ReleaseDC")
	procSetProcessDPIAware = user32.NewProc("SetProcessDPIAware")
	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBm = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject       = gdi32.NewProc("SelectObject")
	procBitBlt             = gdi32.NewProc("BitBlt")
	procGetDIBits          = gdi32.NewProc("GetDIBits")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procDeleteDC           = gdi32.NewProc("DeleteDC")
)

const title = "NameSmith - 파일 이름 일괄 변경기"

type control struct {
	h       uintptr
	class   string
	text    string
	visible bool
	enabled bool
}

func main() {
	exe := flag.String("exe", `dist\NameSmith.exe`, "NameSmith.exe to test")
	out := flag.String("out", "dist", "folder for screenshots")
	flag.Parse()
	procSetProcessDPIAware.Call()

	if err := run(*exe, *out); err != nil {
		fmt.Println("FAIL:", err)
		shot(filepath.Join(*out, "uitest-failure.png"), 0)
		os.Exit(1)
	}
	fmt.Println("PASS")
}

func run(exe, out string) error {
	dir := filepath.Join(os.TempDir(), "NameSmith 테스트 폴더")
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var original []string
	for i := 1; i <= 30; i++ {
		original = append(original, fmt.Sprintf("IMG_%04d.jpg", 1000+i))
	}
	original = append(original, "보고서 - 복사본.txt")
	for _, n := range original {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(n), 0o644); err != nil {
			return err
		}
	}

	cmd := exec.Command(exe, dir)
	if err := cmd.Start(); err != nil {
		return err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	defer func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
		}
	}()

	var mw uintptr
	if err := waitFor(20*time.Second, func() bool { mw = findWindow(); return mw != 0 }); err != nil {
		return errors.New("main window did not appear")
	}
	time.Sleep(2 * time.Second)
	shot(filepath.Join(out, "screenshot-1-start.png"), mw)

	// Type a new name; the section switches itself on.
	edit, err := editAfter(mw, "이름을 통째로")
	if err != nil {
		return err
	}
	setText(edit, "사진")
	if err := clickButton(mw, "4. 번호 붙이기"); err != nil {
		return err
	}
	wantButton := fmt.Sprintf("이름 바꾸기 (%d)", len(original))
	if err := waitFor(5*time.Second, func() bool { b := find(mw, "Button", wantButton); return b != nil && b.enabled }); err != nil {
		return fmt.Errorf("rename button never showed %q", wantButton)
	}
	time.Sleep(500 * time.Millisecond)
	shot(filepath.Join(out, "screenshot-2-preview.png"), mw)

	if err := clickButton(mw, wantButton); err != nil {
		return err
	}
	var expected []string
	for i := 1; i <= 30; i++ {
		expected = append(expected, fmt.Sprintf("사진_%03d.jpg", i))
	}
	expected = append(expected, "사진_031.txt")
	if err := waitFor(10*time.Second, func() bool { return sameNames(dir, expected) }); err != nil {
		return fmt.Errorf("files were not renamed as expected; folder has %v", listNames(dir))
	}
	time.Sleep(time.Second)
	shot(filepath.Join(out, "screenshot-3-renamed.png"), mw)
	fmt.Println("rename OK:", len(expected), "files")

	if err := clickButton(mw, "되돌리기"); err != nil {
		return err
	}
	if err := waitFor(10*time.Second, func() bool { return sameNames(dir, original) }); err != nil {
		return fmt.Errorf("undo did not restore the names; folder has %v", listNames(dir))
	}
	time.Sleep(time.Second)
	shot(filepath.Join(out, "screenshot-4-undone.png"), mw)
	fmt.Println("undo OK")

	procPostMessage.Call(mw, 0x0010, 0, 0) // WM_CLOSE
	select {
	case err := <-exited:
		if err != nil {
			return fmt.Errorf("NameSmith exited with %v", err)
		}
	case <-time.After(10 * time.Second):
		return errors.New("NameSmith did not close")
	}
	fmt.Println("close OK")
	return nil
}

func waitFor(d time.Duration, ok func() bool) error {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if ok() {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("timeout")
}

func findWindow() uintptr {
	t, _ := syscall.UTF16PtrFromString(title)
	h, _, _ := procFindWindow.Call(0, uintptr(unsafe.Pointer(t)))
	return h
}

func children(parent uintptr) []control {
	var out []control
	cb := syscall.NewCallback(func(h, _ uintptr) uintptr {
		cls := make([]uint16, 128)
		procGetClassName.Call(h, uintptr(unsafe.Pointer(&cls[0])), 128)
		txt := make([]uint16, 512)
		procGetWindowText.Call(h, uintptr(unsafe.Pointer(&txt[0])), 512)
		v, _, _ := procIsWindowVisible.Call(h)
		e, _, _ := procIsWindowEnabled.Call(h)
		out = append(out, control{h, syscall.UTF16ToString(cls), syscall.UTF16ToString(txt), v != 0, e != 0})
		return 1
	})
	procEnumChildWindows.Call(parent, cb, 0)
	return out
}

func find(parent uintptr, class, text string) *control {
	for _, c := range children(parent) {
		if c.visible && c.class == class && c.text == text {
			c := c
			return &c
		}
	}
	return nil
}

// editAfter returns the first Edit control created after the button with
// the given text (child windows are listed in creation order).
func editAfter(parent uintptr, buttonText string) (uintptr, error) {
	seen := false
	for _, c := range children(parent) {
		if c.class == "Button" && c.text == buttonText {
			seen = true
			continue
		}
		if seen && c.class == "Edit" && c.visible {
			return c.h, nil
		}
	}
	return 0, fmt.Errorf("no text box after %q", buttonText)
}

func setText(h uintptr, s string) {
	p, _ := syscall.UTF16PtrFromString(s)
	procSendMessage.Call(h, 0x000C, 0, uintptr(unsafe.Pointer(p))) // WM_SETTEXT
}

func clickButton(parent uintptr, text string) error {
	b := find(parent, "Button", text)
	if b == nil {
		return fmt.Errorf("button %q not found", text)
	}
	if !b.enabled {
		return fmt.Errorf("button %q is disabled", text)
	}
	procSendMessage.Call(b.h, 0x00F5, 0, 0) // BM_CLICK
	return nil
}

func listNames(dir string) []string {
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

func sameNames(dir string, want []string) bool {
	got := listNames(dir)
	w := append([]string{}, want...)
	sort.Strings(w)
	return strings.Join(got, "|") == strings.Join(w, "|")
}

// shot saves the screen (or just window h when h != 0) as a PNG.
func shot(path string, h uintptr) {
	x, y := int32(0), int32(0)
	sw, _, _ := procGetSystemMetrics.Call(0) // SM_CXSCREEN
	sh, _, _ := procGetSystemMetrics.Call(1) // SM_CYSCREEN
	w, hh := int32(sw), int32(sh)
	if h != 0 {
		var r [4]int32
		procGetWindowRect.Call(h, uintptr(unsafe.Pointer(&r)))
		x, y, w, hh = r[0], r[1], r[2]-r[0], r[3]-r[1]
		if x < 0 {
			w += x
			x = 0
		}
		if y < 0 {
			hh += y
			y = 0
		}
	}
	if w <= 0 || hh <= 0 {
		return
	}
	screen, _, _ := procGetDC.Call(0)
	defer procReleaseDC.Call(0, screen)
	mem, _, _ := procCreateCompatibleDC.Call(screen)
	defer procDeleteDC.Call(mem)
	bmp, _, _ := procCreateCompatibleBm.Call(screen, uintptr(w), uintptr(hh))
	defer procDeleteObject.Call(bmp)
	old, _, _ := procSelectObject.Call(mem, bmp)
	procBitBlt.Call(mem, 0, 0, uintptr(w), uintptr(hh), screen, uintptr(x), uintptr(y), 0x00CC0020|0x40000000)
	procSelectObject.Call(mem, old)

	type bitmapInfoHeader struct {
		Size                                     uint32
		Width, Height                            int32
		Planes, BitCount                         uint16
		Compression, SizeImage                   uint32
		XPelsPerMeter, YPelsPerMeter             int32
		ClrUsed, ClrImportant                    uint32
	}
	bi := bitmapInfoHeader{Size: 40, Width: w, Height: -hh, Planes: 1, BitCount: 32}
	buf := make([]byte, int(w)*int(hh)*4)
	procGetDIBits.Call(mem, bmp, 0, uintptr(hh), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bi)), 0)
	img := image.NewRGBA(image.Rect(0, 0, int(w), int(hh)))
	for i := 0; i < len(buf); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = buf[i+2], buf[i+1], buf[i], 255
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()
	_ = png.Encode(f, img)
}
