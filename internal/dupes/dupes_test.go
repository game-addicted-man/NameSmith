package dupes

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// photo draws a deterministic picture that behaves like a photo: smooth
// light, a few soft shapes and grain.
func photo(seed int64, w, h int) *image.RGBA {
	r := rand.New(rand.NewSource(seed))
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	type blob struct{ x, y, rad, cr, cg, cb float64 }
	var blobs []blob
	for i := 0; i < 7; i++ {
		blobs = append(blobs, blob{r.Float64(), r.Float64(), 0.08 + r.Float64()*0.25, r.Float64() * 255, r.Float64() * 255, r.Float64() * 255})
	}
	gx, gy := r.Float64()*2-1, r.Float64()*2-1
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			fx, fy := float64(x)/float64(w), float64(y)/float64(h)
			base := 110 + 60*(gx*fx+gy*fy)
			cr, cg, cb := base, base*0.9, base*0.8
			for _, b := range blobs {
				d := math.Hypot(fx-b.x, (fy-b.y)*float64(h)/float64(w))
				if t := 1 - d/b.rad; t > 0 {
					t = t * t
					cr += (b.cr - cr) * t
					cg += (b.cg - cg) * t
					cb += (b.cb - cb) * t
				}
			}
			n := (r.Float64() - 0.5) * 12
			img.Set(x, y, color.RGBA{clamp(cr + n), clamp(cg + n), clamp(cb + n), 255})
		}
	}
	return img
}

func clamp(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

func encodeJPEG(t *testing.T, img image.Image, q int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: q}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func writeFile(t *testing.T, path string, data []byte, mod time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if !mod.IsZero() {
		os.Chtimes(path, mod, mod)
	}
}

func hashOf(img image.Image) Hash {
	g := GridOf(img)
	return HashOf(&g)
}

func brighter(img image.Image, f float64) image.Image {
	b := img.Bounds()
	out := image.NewRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			out.Set(x, y, color.RGBA{clamp(float64(r>>8) * f), clamp(float64(g>>8) * f), clamp(float64(bl>>8) * f), 255})
		}
	}
	return out
}

func TestHashLevels(t *testing.T) {
	base := photo(1, 640, 480)
	h0 := hashOf(base)
	decode := func(b []byte) image.Image {
		img, err := jpeg.Decode(bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		return img
	}
	copies := map[string]image.Image{
		"re-saved q40": decode(encodeJPEG(t, base, 40)),
		"half size":    Shrink(base, 320),
		"small":        Shrink(base, 200),
		"brighter":     brighter(base, 1.12),
	}
	for name, img := range copies {
		f, c := hashOf(img).Distances(h0)
		if !LevelStrict.Match(f, c) {
			t.Errorf("%s: not matched at strict (fine %d, coarse %d)", name, f, c)
		}
		if s := Similarity(f, c); s < 90 {
			t.Errorf("%s: similarity %d%%", name, s)
		}
	}
	for seed := int64(2); seed < 12; seed++ {
		f, c := hashOf(photo(seed, 640, 480)).Distances(h0)
		if LevelLoose.Match(f, c) {
			t.Errorf("different photo %d matched at loose (fine %d, coarse %d)", seed, f, c)
		}
	}
	if Similarity(0, 0) != 100 || Similarity(1, 0) != 99 || Similarity(FineBits, CoarseBits) != 0 {
		t.Error("Similarity bounds")
	}
}

func TestOrient(t *testing.T) {
	// 170×102: both sides are multiples of 17, so grids line up exactly
	base := photo(5, 170, 102)
	g := GridOf(base)
	for o := 1; o <= 8; o++ {
		rot := OrientImage(base, o)
		want := GridOf(rot)
		got := Orient(g, o)
		for y := range got {
			for x := range got[y] {
				if math.Abs(float64(got[y][x]-want[y][x])) > 0.5 {
					t.Fatalf("orientation %d: cell %d,%d = %.1f, want %.1f", o, x, y, got[y][x], want[y][x])
				}
			}
		}
		w, h := OrientedSize(170, 102, o)
		if b := rot.Bounds(); b.Dx() != w || b.Dy() != h {
			t.Errorf("orientation %d: size %v, OrientedSize %dx%d", o, b, w, h)
		}
	}
}

func TestCopyPenalty(t *testing.T) {
	for name, want := range map[string]int{
		"보고서.docx":             0,
		"보고서 - 복사본.docx":       2,
		"사진 (2).jpg":           1,
		"Copy of plan.pdf":     2,
		"IMG_1234.JPG":         0,
		"보고서 - 복사본 (3).docx":   3,
		"copyright notice.txt": 0,
	} {
		if got := copyPenalty(name); got != want {
			t.Errorf("copyPenalty(%q) = %d, want %d", name, got, want)
		}
	}
}

func TestFolderPenalty(t *testing.T) {
	for dir, want := range map[string]int{
		`C:\Users\me\Pictures\2024`: 0,
		`C:\Users\me\Downloads`:     1,
		`D:\백업\2024`:                1,
		`D:\사진 백업`:                  1,
		`D:\photos_backup`:          1,
		`D:\templates`:              0,
		`C:\Users\me\Documents\카카오톡 받은 파일`: 1,
		`D:\Backup_2023\photos`:            1,
		`D:\oldies`:                        0,
	} {
		if got := folderPenalty(dir); got != want {
			t.Errorf("folderPenalty(%q) = %d, want %d", dir, got, want)
		}
	}
}

func TestKeepRules(t *testing.T) {
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	a := &File{Path: `C:\a\보고서.docx`, Name: "보고서.docx", Size: 10, Created: t0.Add(2 * time.Hour), Modified: t0, Root: 1}
	b := &File{Path: `C:\b\보고서 - 복사본.docx`, Name: "보고서 - 복사본.docx", Size: 10, Created: t0, Modified: t0.Add(time.Hour), Root: 0}
	c := &File{Path: `C:\c\보고서 (2).docx`, Name: "보고서 (2).docx", Size: 10, Created: t0.Add(time.Hour), Modified: t0.Add(2 * time.Hour), Root: 2}
	for _, f := range []*File{a, b, c} {
		f.hasSum = true
	}
	g := &Group{Kind: GroupExact, Files: []*File{b, c, a}}
	check := func(rule KeepRule, want *File) {
		t.Helper()
		g.ChooseKeep(rule, 3)
		if g.Keep() != want {
			t.Errorf("rule %d kept %s, want %s", rule, g.Keep().Name, want.Name)
		}
		if len(g.Files) != 3 {
			t.Fatal("files lost")
		}
	}
	check(KeepRecommended, a) // the name without "복사본"/"(2)"
	check(KeepOldest, a)
	check(KeepNewest, c)
	check(KeepFirstFolder, b)

	big := &File{Name: "big.jpg", Path: "big.jpg", W: 4000, H: 3000, Size: 3 << 20}
	small := &File{Name: "small.jpg", Path: "small.jpg", W: 1280, H: 960, Size: 4 << 20}
	sg := &Group{Kind: GroupSimilar, Files: []*File{small, big}}
	sg.ChooseKeep(KeepRecommended, 1)
	if sg.Keep() != big {
		t.Error("similar photos: the larger picture should be kept")
	}
	if sg.Reclaimable() != small.Size {
		t.Error("Reclaimable")
	}
}

func scanOpts(roots ...string) Options {
	return Options{Roots: roots, Recursive: true, Exact: true, Workers: 2}
}

func groupNames(groups []*Group) []string {
	var out []string
	for _, g := range groups {
		var names []string
		for _, f := range g.Files {
			names = append(names, f.Name)
		}
		out = append(out, strings.Join(names, ","))
	}
	sort.Strings(out)
	return out
}

func TestScanExact(t *testing.T) {
	root := t.TempDir()
	big := bytes.Repeat([]byte("0123456789abcdef"), 8<<10) // 128 KB
	bigOther := append([]byte{}, big...)
	bigOther[len(bigOther)/2] ^= 1 // same start, same end, different middle
	writeFile(t, filepath.Join(root, "a.txt"), []byte("hello"), time.Time{})
	writeFile(t, filepath.Join(root, "sub", "a - 복사본.txt"), []byte("hello"), time.Time{})
	writeFile(t, filepath.Join(root, "sub", "deeper", "a (2).txt"), []byte("hello"), time.Time{})
	writeFile(t, filepath.Join(root, "other.txt"), []byte("world"), time.Time{}) // same size, different
	writeFile(t, filepath.Join(root, "big.bin"), big, time.Time{})
	writeFile(t, filepath.Join(root, "sub", "big copy.bin"), big, time.Time{})
	writeFile(t, filepath.Join(root, "almost.bin"), bigOther, time.Time{})
	writeFile(t, filepath.Join(root, "empty1"), nil, time.Time{})
	writeFile(t, filepath.Join(root, "empty2"), nil, time.Time{})
	writeFile(t, filepath.Join(root, ".hidden.txt"), []byte("hello"), time.Time{})
	hide(t, filepath.Join(root, ".hidden.txt"))

	var phases []Phase
	res, err := Scan(context.Background(), scanOpts(root, filepath.Join(root, "sub")), func(p Progress) { phases = append(phases, p.Phase) })
	if err != nil {
		t.Fatal(err)
	}
	got := groupNames(res.Groups)
	want := []string{"a.txt,a - 복사본.txt,a (2).txt", "big.bin,big copy.bin"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("groups:\n got %q\nwant %q", got, want)
	}
	if res.Files != 9 { // the hidden file is skipped; "sub" (given twice) is counted once
		t.Errorf("files = %d", res.Files)
	}
	if res.Skipped.Hidden != 1 {
		t.Errorf("hidden = %d", res.Skipped.Hidden)
	}
	if res.Groups[0].Keep().Name != "big.bin" {
		t.Errorf("biggest saving first; got %s", res.Groups[0].Keep().Name)
	}
	if len(phases) == 0 || phases[0] != PhaseList {
		t.Errorf("progress phases %v", phases)
	}

	// not recursive: only the top folder
	opt := scanOpts(root)
	opt.Recursive = false
	res, _ = Scan(context.Background(), opt, nil)
	if len(res.Groups) != 0 {
		t.Errorf("non-recursive found %v", groupNames(res.Groups))
	}

	// only one kind of file
	opt = scanOpts(root)
	opt.Kind = KindDocuments
	res, _ = Scan(context.Background(), opt, nil)
	if got := groupNames(res.Groups); len(got) != 1 || !strings.HasPrefix(got[0], "a.txt") {
		t.Errorf("documents only: %v", got)
	}

	// cancel
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Scan(ctx, scanOpts(root), nil); err == nil {
		t.Error("cancelled scan should fail")
	}
}

type countingLoader struct {
	Loader
	n *atomic.Int64
}

func (c countingLoader) Grid(p string) (Grid, int, int, error) {
	c.n.Add(1)
	return c.Loader.Grid(p)
}

func TestScanSimilar(t *testing.T) {
	var reads atomic.Int64
	old := newLoader
	newLoader = func() Loader { return countingLoader{old(), &reads} }
	defer func() { newLoader = old }()

	root := t.TempDir()
	base := photo(7, 800, 600)
	orig := encodeJPEG(t, base, 92)
	t0 := time.Now().Add(-48 * time.Hour)
	writeFile(t, filepath.Join(root, "여행", "IMG_0001.jpg"), orig, t0)
	writeFile(t, filepath.Join(root, "카톡", "KakaoTalk_0001.jpg"), encodeJPEG(t, Shrink(base, 400), 70), t0.Add(time.Hour))
	writeFile(t, filepath.Join(root, "백업", "IMG_0001.jpg"), orig, t0) // exact copy
	var pngBuf bytes.Buffer
	png.Encode(&pngBuf, Shrink(base, 300))
	writeFile(t, filepath.Join(root, "캡처.png"), pngBuf.Bytes(), t0)
	writeFile(t, filepath.Join(root, "다른 사진.jpg"), encodeJPEG(t, photo(8, 800, 600), 90), t0)
	writeFile(t, filepath.Join(root, "망가진.jpg"), []byte(strings.Repeat("not a jpeg ", 400)), t0)

	opt := Options{Roots: []string{root}, Recursive: true, Exact: true, Similar: true, Level: LevelNormal,
		Workers: 2, CachePath: filepath.Join(t.TempDir(), "cache.gob")}
	res, err := Scan(context.Background(), opt, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Groups) != 1 {
		t.Fatalf("groups: %v", groupNames(res.Groups))
	}
	g := res.Groups[0]
	if g.Kind != GroupSimilar || len(g.Files) != 4 {
		t.Fatalf("group: kind %v files %v", g.Kind, groupNames(res.Groups))
	}
	if k := g.Keep(); k.W != 800 || k.H != 600 || filepath.Base(k.Dir) == "카톡" {
		t.Errorf("kept %s (%dx%d)", k.Path, k.W, k.H)
	}
	for _, f := range g.Files[1:] {
		same, sim := g.Relation(f)
		switch f.Name {
		case "IMG_0001.jpg":
			if !same {
				t.Errorf("exact copy not marked as same")
			}
		default:
			if same || sim < 85 || sim > 99 {
				t.Errorf("%s: same=%v similarity=%d", f.Name, same, sim)
			}
		}
	}
	if res.Skipped.Pictures != 1 {
		t.Errorf("broken pictures = %d", res.Skipped.Pictures)
	}
	first := reads.Load()
	if first != 5 { // 6 pictures, the exact copy is read once
		t.Errorf("pictures read = %d", first)
	}

	// second scan: everything comes from the cache
	res2, err := Scan(context.Background(), opt, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reads.Load() != first {
		t.Errorf("cache not used: %d reads", reads.Load()-first)
	}
	if strings.Join(groupNames(res2.Groups), "|") != strings.Join(groupNames(res.Groups), "|") {
		t.Errorf("cached scan differs: %v", groupNames(res2.Groups))
	}

	// similar photos only (no exact search): the copies still group
	opt.Exact = false
	res3, _ := Scan(context.Background(), opt, nil)
	if len(res3.Groups) != 1 || len(res3.Groups[0].Files) != 4 {
		t.Errorf("similar only: %v", groupNames(res3.Groups))
	}
}

func TestMoveToAndBack(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "중복 파일")
	writeFile(t, filepath.Join(src, "a", "사진.jpg"), []byte("A"), time.Time{})
	writeFile(t, filepath.Join(src, "b", "사진.jpg"), []byte("B"), time.Time{})
	writeFile(t, filepath.Join(dst, "사진.jpg"), []byte("already there"), time.Time{})
	paths := []string{filepath.Join(src, "a", "사진.jpg"), filepath.Join(src, "b", "사진.jpg"), filepath.Join(src, "missing.jpg")}
	moved := MoveTo(paths, dst, nil)
	if moved[0].Err != nil || filepath.Base(moved[0].To) != "사진 (2).jpg" ||
		moved[1].Err != nil || filepath.Base(moved[1].To) != "사진 (3).jpg" || moved[2].Err == nil {
		t.Fatalf("moved: %+v", moved)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "사진.jpg")); string(b) != "already there" {
		t.Error("existing file was touched")
	}
	back := MoveBack(moved, nil)
	if len(back) != 2 || back[0].Err != nil || back[1].Err != nil {
		t.Fatalf("back: %+v", back)
	}
	for i, want := range []string{"A", "B"} {
		if b, err := os.ReadFile(paths[i]); err != nil || string(b) != want {
			t.Errorf("file %d not restored: %q %v", i, b, err)
		}
	}
}

func TestSkipDir(t *testing.T) {
	root := filepath.VolumeName(os.TempDir()) + string(filepath.Separator) // C:\ or /
	if !skipDir(filepath.Join(root, "Windows"), "Windows") || !skipDir(filepath.Join(root, "$RECYCLE.BIN"), "$RECYCLE.BIN") {
		t.Error("system folders must be skipped")
	}
	if skipDir(filepath.Join(root, "Users", "me", "Windows"), "Windows") {
		t.Error("a folder named Windows inside the user's files is fine")
	}
}
