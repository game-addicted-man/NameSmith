//go:build windows

package dupes

import (
	"fmt"
	"image"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Photos are read with WIC (Windows Imaging Component): it is fast (JPEGs
// are decoded straight at a small size) and reads every format Windows can
// show — including HEIC and WebP when their Windows extensions are installed.
// Go's own decoders are the fallback.

var (
	modOle32             = windows.NewLazySystemDLL("ole32.dll")
	procCoCreateInstance = modOle32.NewProc("CoCreateInstance")

	clsidWICImagingFactory = windows.GUID{Data1: 0xcacaf262, Data2: 0x9370, Data3: 0x4615, Data4: [8]byte{0xa1, 0x3b, 0x9f, 0x55, 0x39, 0xda, 0x4c, 0x0a}}
	iidIWICImagingFactory  = windows.GUID{Data1: 0xec5ec8a9, Data2: 0xc395, Data3: 0x4314, Data4: [8]byte{0x9c, 0x77, 0x54, 0xd7, 0xa9, 0x35, 0xff, 0x70}}
	guidPixelFormatPBGRA   = windows.GUID{Data1: 0x6fddc324, Data2: 0x4e03, Data3: 0x4bfe, Data4: [8]byte{0xb1, 0x85, 0x3d, 0x77, 0x76, 0x8d, 0xc9, 0x10}}
)

const (
	genericRead          = 0x80000000
	wicCacheOnDemand     = 0
	wicInterpolationFant = 3
)

// vtable slots
const (
	slotRelease = 2
	// IWICImagingFactory
	slotCreateDecoderFromFilename = 3
	slotCreateFormatConverter     = 10
	slotCreateBitmapScaler        = 11
	// IWICBitmapDecoder
	slotGetFrame = 13
	// IWICBitmapSource
	slotGetSize    = 3
	slotCopyPixels = 7
	// IWICBitmapScaler / IWICFormatConverter
	slotInitialize = 8
)

type comObj struct {
	vtbl *[64]uintptr
}

// call invokes a COM method. uintptrescapes keeps pointer arguments on the
// heap and alive during the call.
//
//go:uintptrescapes
func (o *comObj) call(slot int, args ...uintptr) uint32 {
	all := make([]uintptr, 0, len(args)+1)
	all = append(all, uintptr(unsafe.Pointer(o)))
	all = append(all, args...)
	r, _, _ := syscall.SyscallN(o.vtbl[slot], all...)
	return uint32(r)
}

func (o *comObj) release() {
	if o != nil {
		o.call(slotRelease)
	}
}

type wicLoader struct {
	factory  *comObj
	comInit  bool
	fallback goLoader
}

// NewLoader returns the photo reader for this system. The calling goroutine
// must stay on its OS thread (runtime.LockOSThread) until Close.
func NewLoader() Loader {
	l := &wicLoader{}
	if err := windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED); err == nil {
		l.comInit = true
	}
	var f *comObj
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidWICImagingFactory)), 0, 1, // CLSCTX_INPROC_SERVER
		uintptr(unsafe.Pointer(&iidIWICImagingFactory)), uintptr(unsafe.Pointer(&f)))
	if uint32(hr) == 0 && f != nil {
		l.factory = f
	}
	return l
}

func (l *wicLoader) Close() {
	l.factory.release()
	l.factory = nil
	if l.comInit {
		windows.CoUninitialize()
		l.comInit = false
	}
}

func isJPEG(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg", ".jpe", ".jfif":
		return true
	}
	return false
}

// pixels decodes path at tw×th (or, when fit > 0, at the largest size that
// fits in fit×fit) as premultiplied BGRA. It returns the stored size too.
func (l *wicLoader) pixels(path string, tw, th, fit int) (buf []byte, outW, outH, w, h int, err error) {
	if l.factory == nil {
		return nil, 0, 0, 0, 0, ErrNotPicture
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, 0, 0, 0, 0, err
	}
	var dec, frame *comObj
	if hr := l.factory.call(slotCreateDecoderFromFilename, uintptr(unsafe.Pointer(p)), 0, genericRead, wicCacheOnDemand, uintptr(unsafe.Pointer(&dec))); hr != 0 || dec == nil {
		return nil, 0, 0, 0, 0, fmt.Errorf("%w (WIC 0x%08X)", ErrNotPicture, hr)
	}
	defer dec.release()
	if hr := dec.call(slotGetFrame, 0, uintptr(unsafe.Pointer(&frame))); hr != 0 || frame == nil {
		return nil, 0, 0, 0, 0, fmt.Errorf("%w (WIC 0x%08X)", ErrNotPicture, hr)
	}
	defer frame.release()
	var w32, h32 uint32
	if hr := frame.call(slotGetSize, uintptr(unsafe.Pointer(&w32)), uintptr(unsafe.Pointer(&h32))); hr != 0 || w32 == 0 || h32 == 0 {
		return nil, 0, 0, 0, 0, ErrNotPicture
	}
	w, h = int(w32), int(h32)
	if fit > 0 {
		tw, th = w, h
		if w > fit || h > fit {
			if w >= h {
				tw, th = fit, max(1, h*fit/w)
			} else {
				tw, th = max(1, w*fit/h), fit
			}
		}
	}

	// JPEGs are scaled first (the decoder then skips most of the work);
	// other formats are converted first so transparency is averaged right.
	orders := []bool{true, false}
	if !isJPEG(path) {
		orders = []bool{false, true}
	}
	var lastHR uint32
	for _, scaleFirst := range orders {
		var buf []byte
		buf, lastHR = l.run(frame, tw, th, scaleFirst)
		if lastHR == 0 {
			return buf, tw, th, w, h, nil
		}
	}
	return nil, 0, 0, w, h, fmt.Errorf("%w (WIC 0x%08X)", ErrNotPicture, lastHR)
}

func (l *wicLoader) run(frame *comObj, tw, th int, scaleFirst bool) ([]byte, uint32) {
	var scaler, conv *comObj
	if hr := l.factory.call(slotCreateBitmapScaler, uintptr(unsafe.Pointer(&scaler))); hr != 0 || scaler == nil {
		return nil, hr
	}
	defer scaler.release()
	if hr := l.factory.call(slotCreateFormatConverter, uintptr(unsafe.Pointer(&conv))); hr != 0 || conv == nil {
		return nil, hr
	}
	defer conv.release()

	initScaler := func(src *comObj) uint32 {
		return scaler.call(slotInitialize, uintptr(unsafe.Pointer(src)), uintptr(tw), uintptr(th), wicInterpolationFant)
	}
	initConv := func(src *comObj) uint32 {
		// source, format, dither none, no palette, alpha threshold 0.0, custom palette
		return conv.call(slotInitialize, uintptr(unsafe.Pointer(src)), uintptr(unsafe.Pointer(&guidPixelFormatPBGRA)), 0, 0, 0, 0)
	}
	var last *comObj
	if scaleFirst {
		if hr := initScaler(frame); hr != 0 {
			return nil, hr
		}
		if hr := initConv(scaler); hr != 0 {
			return nil, hr
		}
		last = conv
	} else {
		if hr := initConv(frame); hr != 0 {
			return nil, hr
		}
		if hr := initScaler(conv); hr != 0 {
			return nil, hr
		}
		last = scaler
	}
	stride := tw * 4
	buf := make([]byte, stride*th)
	if hr := last.call(slotCopyPixels, 0, uintptr(stride), uintptr(len(buf)), uintptr(unsafe.Pointer(&buf[0]))); hr != 0 {
		return nil, hr
	}
	return buf, 0
}

func (l *wicLoader) Grid(path string) (Grid, int, int, error) {
	g, w, h, err := l.pixelsGrid(path)
	if err != nil {
		return l.fallback.Grid(path)
	}
	return g, w, h, nil
}

// gridDecodeSize: WIC decodes the photo at about this size (JPEGs are
// decoded straight at a fraction of their size), and the grid is averaged
// from that in Go. Asking WIC for 17×17 directly is not safe: some scalers
// pick single pixels instead of averaging.
const gridDecodeSize = 320

// pixelsGrid is Grid through WIC only (no fallback).
func (l *wicLoader) pixelsGrid(path string) (Grid, int, int, error) {
	buf, tw, th, w, h, err := l.pixels(path, 0, 0, gridDecodeSize)
	if err != nil {
		return Grid{}, 0, 0, err
	}
	return gridFromPBGRA(buf, tw, th), w, h, nil
}

// gridFromPBGRA averages premultiplied BGRA pixels (on white) into a grid.
func gridFromPBGRA(buf []byte, w, h int) Grid {
	var g Grid
	var cnt [GridSize][GridSize]float32
	for y := 0; y < h; y++ {
		cy := y * GridSize / h
		row := buf[y*w*4:]
		for x := 0; x < w; x++ {
			p := row[x*4 : x*4+4]
			cx := x * GridSize / w
			g[cy][cx] += 0.114*float32(p[0]) + 0.587*float32(p[1]) + 0.299*float32(p[2]) + float32(255-p[3])
			cnt[cy][cx]++
		}
	}
	for y := range g {
		for x := range g[y] {
			if cnt[y][x] > 0 {
				g[y][x] /= cnt[y][x]
			} else {
				g[y][x] = 255
			}
		}
	}
	return g
}

func (l *wicLoader) Thumb(path string, maxSide int) (image.Image, int, int, error) {
	buf, tw, th, w, h, err := l.pixels(path, 0, 0, maxSide)
	if err != nil {
		return l.fallback.Thumb(path, maxSide)
	}
	img := image.NewRGBA(image.Rect(0, 0, tw, th))
	for i := 0; i+3 < len(buf); i += 4 {
		white := 255 - buf[i+3]
		img.Pix[i+0] = buf[i+2] + white
		img.Pix[i+1] = buf[i+1] + white
		img.Pix[i+2] = buf[i+0] + white
		img.Pix[i+3] = 255
	}
	return img, w, h, nil
}
