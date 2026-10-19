package dupes

import (
	"image"
	"image/color"
	"math"
	"math/bits"
)

// GridSize is the side of the brightness grid a photo is reduced to.
const GridSize = 17

// Grid holds the average brightness (0-255, on a white background) of
// 17×17 equal areas of a photo.
type Grid [GridSize][GridSize]float32

// Hash describes how a photo looks, as two "difference hashes" made from
// its brightness grid: for each area, whether the brightness rises to the
// right and downward. The fine part (512 bits, 16×16 areas) tells photos
// apart very reliably; the coarse part (128 bits, 8×8 areas) still matches
// when a photo was cropped a little or taken again a moment later.
// Copies that were resized, re-saved or lightly edited differ in few bits.
type Hash [10]uint64

const (
	FineBits   = 512
	CoarseBits = 128
)

// Distances returns how many fine and coarse bits differ.
func (h Hash) Distances(o Hash) (fine, coarse int) {
	for i := 0; i < 8; i++ {
		fine += bits.OnesCount64(h[i] ^ o[i])
	}
	for i := 8; i < 10; i++ {
		coarse += bits.OnesCount64(h[i] ^ o[i])
	}
	return fine, coarse
}

// score orders candidates: lower is closer.
func score(fine, coarse int) int { return fine + 4*coarse }

// Similarity turns distances into a percentage for people: 100 means the
// same look, unrelated photos come out around 25.
func Similarity(fine, coarse int) int {
	if fine == 0 && coarse == 0 {
		return 100
	}
	mix := (float64(fine)/FineBits + float64(coarse)/CoarseBits) / 2
	p := 100 - int(math.Round(mix*150))
	if p > 99 {
		p = 99
	}
	if p < 0 {
		p = 0
	}
	return p
}

// HashOf computes the hash of a grid.
func HashOf(g *Grid) Hash {
	var h Hash
	bit := 0
	set := func(on bool) {
		if on {
			h[bit>>6] |= 1 << (bit & 63)
		}
		bit++
	}
	for y := 0; y < GridSize-1; y++ {
		for x := 0; x < GridSize-1; x++ {
			set(g[y][x] < g[y][x+1])
		}
	}
	for x := 0; x < GridSize-1; x++ {
		for y := 0; y < GridSize-1; y++ {
			set(g[y][x] < g[y+1][x])
		}
	}
	// coarse 9×9 grid: 2×2 areas of the fine grid (the last row and column
	// stand alone)
	var c [9][9]float32
	for y := 0; y < 9; y++ {
		for x := 0; x < 9; x++ {
			var sum float32
			n := 0
			for dy := 0; dy < 2; dy++ {
				for dx := 0; dx < 2; dx++ {
					if yy, xx := y*2+dy, x*2+dx; yy < GridSize && xx < GridSize {
						sum += g[yy][xx]
						n++
					}
				}
			}
			c[y][x] = sum / float32(n)
		}
	}
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			set(c[y][x] < c[y][x+1])
		}
	}
	for x := 0; x < 8; x++ {
		for y := 0; y < 8; y++ {
			set(c[y][x] < c[y+1][x])
		}
	}
	return h
}

// Orient turns a grid the way EXIF orientation o (1-8) says the photo
// should be shown.
func Orient(g Grid, o int) Grid {
	const n = GridSize - 1
	var d Grid
	for y := 0; y < GridSize; y++ {
		for x := 0; x < GridSize; x++ {
			switch o {
			case 2: // mirrored
				d[y][x] = g[y][n-x]
			case 3: // upside down
				d[y][x] = g[n-y][n-x]
			case 4: // mirrored upside down
				d[y][x] = g[n-y][x]
			case 5: // mirrored, turned left
				d[y][x] = g[x][y]
			case 6: // turned right (portrait from most phones)
				d[y][x] = g[n-x][y]
			case 7: // mirrored, turned right
				d[y][x] = g[n-x][n-y]
			case 8: // turned left
				d[y][x] = g[x][n-y]
			default:
				d[y][x] = g[y][x]
			}
		}
	}
	return d
}

// OrientedSize returns the size a photo is shown at.
func OrientedSize(w, h, o int) (int, int) {
	if o >= 5 && o <= 8 {
		return h, w
	}
	return w, h
}

// luma of an 8-bit colour with straight alpha, on a white background.
func lumaOnWhite(r, g, b, a uint32) float32 {
	y := 0.299*float32(r) + 0.587*float32(g) + 0.114*float32(b)
	if a >= 255 {
		return y
	}
	return y*float32(a)/255 + 255*(1-float32(a)/255)
}

// GridOf computes the grid of an image by averaging its pixels. Very large
// images are sampled (every k-th pixel), which changes the averages very
// little.
func GridOf(img image.Image) Grid {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	var sum Grid
	var cnt [GridSize][GridSize]float32
	if w <= 0 || h <= 0 {
		return sum
	}
	step := 1
	if px := float64(w) * float64(h); px > 2_000_000 {
		step = int(math.Ceil(math.Sqrt(px / 2_000_000)))
	}
	cellX := make([]int, w)
	for x := range cellX {
		cellX[x] = x * GridSize / w
	}
	add := func(x, y int, v float32) {
		cy := y * GridSize / h
		cx := cellX[x]
		sum[cy][cx] += v
		cnt[cy][cx]++
	}

	switch m := img.(type) {
	case *image.YCbCr:
		for y := 0; y < h; y += step {
			row := m.Y[(y)*m.YStride:]
			for x := 0; x < w; x += step {
				add(x, y, float32(row[x]))
			}
		}
	case *image.Gray:
		for y := 0; y < h; y += step {
			row := m.Pix[y*m.Stride:]
			for x := 0; x < w; x += step {
				add(x, y, float32(row[x]))
			}
		}
	case *image.NRGBA:
		for y := 0; y < h; y += step {
			row := m.Pix[y*m.Stride:]
			for x := 0; x < w; x += step {
				p := row[x*4 : x*4+4]
				add(x, y, lumaOnWhite(uint32(p[0]), uint32(p[1]), uint32(p[2]), uint32(p[3])))
			}
		}
	case *image.RGBA:
		for y := 0; y < h; y += step {
			row := m.Pix[y*m.Stride:]
			for x := 0; x < w; x += step {
				p := row[x*4 : x*4+4]
				a := uint32(p[3])
				// premultiplied: colour + white × (1 - alpha)
				v := 0.299*float32(p[0]) + 0.587*float32(p[1]) + 0.114*float32(p[2]) + float32(255-a)
				add(x, y, v)
			}
		}
	default:
		for y := 0; y < h; y += step {
			for x := 0; x < w; x += step {
				c := color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
				add(x, y, lumaOnWhite(uint32(c.R), uint32(c.G), uint32(c.B), uint32(c.A)))
			}
		}
	}
	for y := range sum {
		for x := range sum[y] {
			if cnt[y][x] > 0 {
				sum[y][x] /= cnt[y][x]
			} else {
				sum[y][x] = 255
			}
		}
	}
	return sum
}

// Shrink returns a copy of img that fits in max×max (keeping its shape),
// made by averaging pixels. Small images are returned as they are.
func Shrink(img image.Image, max int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= max && h <= max {
		return img
	}
	tw, th := max, max
	if w >= h {
		th = int(math.Max(1, math.Round(float64(h)*float64(max)/float64(w))))
	} else {
		tw = int(math.Max(1, math.Round(float64(w)*float64(max)/float64(h))))
	}
	out := image.NewNRGBA(image.Rect(0, 0, tw, th))
	step := 1
	if px := float64(w) * float64(h); px > 4_000_000 {
		step = int(math.Ceil(math.Sqrt(px / 4_000_000)))
	}
	acc := make([][4]float64, tw*th)
	n := make([]float64, tw*th)
	for y := 0; y < h; y += step {
		ty := y * th / h
		for x := 0; x < w; x += step {
			tx := x * tw / w
			c := color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
			i := ty*tw + tx
			a := float64(c.A)
			acc[i][0] += float64(c.R) * a
			acc[i][1] += float64(c.G) * a
			acc[i][2] += float64(c.B) * a
			acc[i][3] += a
			n[i]++
		}
	}
	for i := range acc {
		if n[i] == 0 || acc[i][3] == 0 {
			continue
		}
		o := out.Pix[i*4 : i*4+4]
		o[0] = uint8(acc[i][0] / acc[i][3])
		o[1] = uint8(acc[i][1] / acc[i][3])
		o[2] = uint8(acc[i][2] / acc[i][3])
		o[3] = uint8(acc[i][3] / n[i])
	}
	return out
}

// OrientImage returns img turned the way EXIF orientation o says.
func OrientImage(img image.Image, o int) image.Image {
	if o <= 1 || o > 8 {
		return img
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	dw, dh := OrientedSize(w, h, o)
	out := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch o {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			out.Set(dx, dy, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return out
}
