package dupes

import (
	"bufio"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // register decoders
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"

	"github.com/game-addicted-man/NameSmith/internal/meta"
)

// photoExts are the files "비슷한 사진" looks at. Camera RAW files are left
// out on purpose: photographers keep them next to their JPEGs.
var photoExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".jpe": true, ".jfif": true,
	".png": true, ".gif": true, ".bmp": true, ".dib": true, ".webp": true,
	".tif": true, ".tiff": true,
	".heic": true, ".heif": true, ".hif": true, ".avif": true,
}

// IsPhoto reports whether the file name looks like a photo or picture.
func IsPhoto(name string) bool { return photoExts[strings.ToLower(filepath.Ext(name))] }

// decoderRotates: these formats are shown upright by the decoder itself, so
// EXIF orientation must not be applied again.
func decoderRotates(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".heic", ".heif", ".hif", ".avif":
		return true
	}
	return false
}

// Loader reads photos. One Loader is used by one goroutine at a time.
type Loader interface {
	// Grid returns the brightness grid of the photo as stored (EXIF
	// orientation not applied) and its stored width and height.
	Grid(path string) (g Grid, w, h int, err error)
	// Thumb returns a copy that fits in max×max (orientation not applied)
	// and the stored width and height.
	Thumb(path string, max int) (img image.Image, w, h int, err error)
	Close()
}

// ErrNotPicture means the file could not be read as a picture.
var ErrNotPicture = errors.New("사진으로 읽을 수 없어요")

// maxGoPixels guards memory: the built-in decoders hold the whole picture.
const maxGoPixels = 120_000_000

// goLoader uses Go's own decoders (JPEG, PNG, GIF, BMP, WebP, TIFF).
type goLoader struct{}

func (goLoader) Close() {}

func decodeFile(path string) (img image.Image, err error) {
	defer func() {
		if r := recover(); r != nil {
			img, err = nil, fmt.Errorf("%w (%v)", ErrNotPicture, r)
		}
	}()
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(bufio.NewReader(f))
	if err != nil {
		return nil, ErrNotPicture
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > maxGoPixels {
		return nil, ErrNotPicture
	}
	if _, err := f.Seek(0, 0); err != nil {
		return nil, err
	}
	img, _, err = image.Decode(bufio.NewReaderSize(f, 256<<10))
	if err != nil {
		return nil, ErrNotPicture
	}
	return img, nil
}

func (goLoader) Grid(path string) (Grid, int, int, error) {
	img, err := decodeFile(path)
	if err != nil {
		return Grid{}, 0, 0, err
	}
	b := img.Bounds()
	return GridOf(img), b.Dx(), b.Dy(), nil
}

func (goLoader) Thumb(path string, max int) (image.Image, int, int, error) {
	img, err := decodeFile(path)
	if err != nil {
		return nil, 0, 0, err
	}
	b := img.Bounds()
	return Shrink(img, max), b.Dx(), b.Dy(), nil
}

// Preview returns a small upright copy of a photo for showing on screen.
func Preview(l Loader, path string, maxSide int) (image.Image, error) {
	img, _, _, err := l.Thumb(path, maxSide)
	if err != nil {
		return nil, err
	}
	o := meta.Read(path).Orientation
	if decoderRotates(path) {
		o = 1
	}
	return OrientImage(img, o), nil
}
