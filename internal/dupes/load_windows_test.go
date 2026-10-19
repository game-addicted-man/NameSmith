//go:build windows

package dupes

import (
	"bytes"
	"image/png"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// The Windows photo reader must see photos the way Go's decoders do, so a
// photo hashes the same whichever reader handled it.
func TestWICMatchesGo(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	l := NewLoader()
	defer l.Close()
	if l.(*wicLoader).factory == nil {
		t.Fatal("WIC is not available")
	}
	dir := t.TempDir()
	base := photo(3, 900, 600)
	jpg := filepath.Join(dir, "사진.jpg")
	writeFile(t, jpg, encodeJPEG(t, base, 90), time.Time{})
	var b bytes.Buffer
	png.Encode(&b, Shrink(base, 450))
	pngPath := filepath.Join(dir, "그림.png")
	writeFile(t, pngPath, b.Bytes(), time.Time{})

	for _, p := range []string{jpg, pngPath} {
		gw, w, h, err := l.(*wicLoader).pixelsGrid(p)
		if err != nil {
			t.Fatalf("%s: WIC failed: %v", filepath.Base(p), err)
		}
		gg, gow, goh, err := goLoader{}.Grid(p)
		if err != nil {
			t.Fatal(err)
		}
		if w != gow || h != goh {
			t.Errorf("%s: WIC size %dx%d, Go %dx%d", filepath.Base(p), w, h, gow, goh)
		}
		hw, hg := HashOf(&gw), HashOf(&gg)
		f, c := hw.Distances(hg)
		if !LevelStrict.Match(f, c) {
			t.Errorf("%s: WIC and Go hashes differ: fine %d coarse %d", filepath.Base(p), f, c)
		}
		t.Logf("%s: fine %d coarse %d", filepath.Base(p), f, c)

		img, tw, th, err := l.Thumb(p, 200)
		if err != nil || tw != gow || th != goh {
			t.Fatalf("%s: thumb %v %dx%d", filepath.Base(p), err, tw, th)
		}
		if bb := img.Bounds(); bb.Dx() > 200 || bb.Dy() > 200 || bb.Dx() < 100 {
			t.Errorf("%s: thumb size %v", filepath.Base(p), bb)
		}
	}
	if _, _, _, err := l.Grid(filepath.Join(dir, "없는 파일.jpg")); err == nil {
		t.Error("missing file should fail")
	}
}
