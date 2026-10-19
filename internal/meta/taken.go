// Package meta reads the date a photo or video was taken (and the EXIF
// orientation of photos), straight from the file: EXIF in JPEG/TIFF/camera
// RAW/HEIC/AVIF/PNG/WebP, and the creation time of MP4/MOV videos. It reads only small parts of each file and never
// fails loudly: anything unexpected just means "no date".
package meta

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// mediaExts are extensions worth opening.
var mediaExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".jpe": true, ".jfif": true,
	".heic": true, ".heif": true, ".hif": true, ".avif": true,
	".tif": true, ".tiff": true, ".dng": true, ".cr2": true, ".nef": true, ".nrw": true,
	".arw": true, ".srf": true, ".sr2": true, ".orf": true, ".rw2": true, ".pef": true,
	".png": true, ".webp": true,
	".mp4": true, ".m4v": true, ".mov": true, ".qt": true, ".3gp": true, ".3g2": true,
}

// IsMedia reports whether the file name looks like a photo or video.
func IsMedia(name string) bool { return mediaExts[strings.ToLower(filepath.Ext(name))] }

// Info is what Read finds in a photo or video.
type Info struct {
	Taken       time.Time // when it was taken
	HasTaken    bool
	Orientation int // EXIF orientation 1-8 (0 = not stored)
}

// Read returns the capture date and EXIF orientation of the photo or video
// at path. Missing or broken metadata simply gives a zero Info.
func Read(path string) Info {
	if !IsMedia(path) {
		return Info{}
	}
	f, err := os.Open(path)
	if err != nil {
		return Info{}
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return Info{}
	}
	var p parser
	t, ok := p.detect(f, st.Size())
	info := Info{Orientation: p.orient}
	if ok && plausible(t) {
		info.Taken, info.HasTaken = t, true
	}
	return info
}

// DateTaken returns when the photo or video at path was taken.
func DateTaken(path string) (time.Time, bool) {
	i := Read(path)
	return i.Taken, i.HasTaken
}

// parser remembers what it saw while looking for the date.
type parser struct {
	orient int
}

func plausible(t time.Time) bool {
	return t.Year() >= 1980 && t.Before(time.Now().AddDate(1, 0, 0))
}

func (p *parser) detect(r io.ReaderAt, size int64) (t time.Time, ok bool) {
	defer func() {
		if recover() != nil {
			t, ok = time.Time{}, false
		}
	}()
	head := make([]byte, 16)
	n, _ := r.ReadAt(head, 0)
	head = head[:n]
	switch {
	case len(head) >= 3 && head[0] == 0xFF && head[1] == 0xD8 && head[2] == 0xFF:
		return p.fromJPEG(r, size)
	case len(head) >= 4 && (string(head[:2]) == "II" || string(head[:2]) == "MM"):
		return p.fromTIFF(r, 0, size)
	case len(head) >= 8 && bytes.Equal(head[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'}):
		return p.fromPNG(r, size)
	case len(head) >= 12 && string(head[:4]) == "RIFF" && string(head[8:12]) == "WEBP":
		return p.fromWebP(r, size)
	case len(head) >= 8 && isBoxType(head[4:8]):
		return p.fromISOBMFF(r, size)
	}
	return time.Time{}, false
}

// ---------- EXIF / TIFF ----------

// fromTIFF parses a TIFF structure starting at base and returns
// DateTimeOriginal, DateTimeDigitized or DateTime (in that order).
func (p *parser) fromTIFF(r io.ReaderAt, base, size int64) (time.Time, bool) {
	hdr := make([]byte, 8)
	if _, err := r.ReadAt(hdr, base); err != nil {
		return time.Time{}, false
	}
	var bo binary.ByteOrder
	switch string(hdr[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return time.Time{}, false
	}
	ifd0 := int64(bo.Uint32(hdr[4:8]))

	tags0 := readIFD(r, bo, base, ifd0, size)
	if v, ok := tags0[0x0112]; ok && v.typ == 3 && p.orient == 0 {
		if o := int(bo.Uint16(v.raw[:2])); o >= 1 && o <= 8 {
			p.orient = o
		}
	}
	var original, digitized, plain string
	if v, ok := tags0[0x0132]; ok {
		plain = readASCII(r, bo, base, v, size)
	}
	if v, ok := tags0[0x8769]; ok {
		exifOff := int64(bo.Uint32(v.raw[:]))
		tagsE := readIFD(r, bo, base, exifOff, size)
		if v, ok := tagsE[0x9003]; ok {
			original = readASCII(r, bo, base, v, size)
		}
		if v, ok := tagsE[0x9004]; ok {
			digitized = readASCII(r, bo, base, v, size)
		}
	}
	for _, s := range []string{original, digitized, plain} {
		if t, ok := parseExifTime(s); ok {
			return t, true
		}
	}
	return time.Time{}, false
}

type ifdEntry struct {
	typ   uint16
	count uint32
	raw   [4]byte
}

// readIFD reads the entries of one IFD that NameSmith cares about.
func readIFD(r io.ReaderAt, bo binary.ByteOrder, base, off, size int64) map[uint16]ifdEntry {
	out := map[uint16]ifdEntry{}
	if off <= 0 || base+off+2 > size {
		return out
	}
	cnt := make([]byte, 2)
	if _, err := r.ReadAt(cnt, base+off); err != nil {
		return out
	}
	n := int(bo.Uint16(cnt))
	if n <= 0 || n > 1000 {
		return out
	}
	buf := make([]byte, 12*n)
	if _, err := r.ReadAt(buf, base+off+2); err != nil {
		return out
	}
	for i := 0; i < n; i++ {
		e := buf[12*i : 12*i+12]
		tag := bo.Uint16(e[0:2])
		switch tag {
		case 0x0112, 0x0132, 0x8769, 0x9003, 0x9004:
			var en ifdEntry
			en.typ = bo.Uint16(e[2:4])
			en.count = bo.Uint32(e[4:8])
			copy(en.raw[:], e[8:12])
			out[tag] = en
		}
	}
	return out
}

func readASCII(r io.ReaderAt, bo binary.ByteOrder, base int64, e ifdEntry, size int64) string {
	if e.typ != 2 || e.count == 0 || e.count > 64 {
		return ""
	}
	var b []byte
	if e.count <= 4 {
		b = e.raw[:e.count]
	} else {
		off := int64(bo.Uint32(e.raw[:]))
		if base+off+int64(e.count) > size {
			return ""
		}
		b = make([]byte, e.count)
		if _, err := r.ReadAt(b, base+off); err != nil {
			return ""
		}
	}
	return strings.TrimRight(string(b), "\x00 ")
}

// parseExifTime reads "2024:05:01 13:45:30" (also with - or / as date
// separators and optional sub-seconds or time zone). The camera's local time
// is kept as is.
func parseExifTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if len(s) < 10 {
		return time.Time{}, false
	}
	num := func(x string) (int, bool) {
		n := 0
		if x == "" {
			return 0, false
		}
		for _, c := range x {
			if c < '0' || c > '9' {
				return 0, false
			}
			n = n*10 + int(c-'0')
		}
		return n, true
	}
	y, ok1 := num(s[0:4])
	mo, ok2 := num(s[5:7])
	d, ok3 := num(s[8:10])
	if !ok1 || !ok2 || !ok3 || y == 0 || mo < 1 || mo > 12 || d < 1 || d > 31 {
		return time.Time{}, false
	}
	h, mi, sec := 0, 0, 0
	if len(s) >= 19 {
		var ok4, ok5, ok6 bool
		h, ok4 = num(s[11:13])
		mi, ok5 = num(s[14:16])
		sec, ok6 = num(s[17:19])
		if !ok4 || !ok5 || !ok6 || h > 23 || mi > 59 || sec > 60 {
			h, mi, sec = 0, 0, 0
		}
	}
	return time.Date(y, time.Month(mo), d, h, mi, sec, 0, time.Local), true
}

// ---------- JPEG ----------

func (p *parser) fromJPEG(r io.ReaderAt, size int64) (time.Time, bool) {
	pos := int64(2)
	hdr := make([]byte, 4)
	for steps := 0; steps < 64 && pos+4 <= size && pos < 1<<20; steps++ {
		if _, err := r.ReadAt(hdr, pos); err != nil {
			break
		}
		if hdr[0] != 0xFF {
			break
		}
		marker := hdr[1]
		if marker == 0xFF { // fill byte
			pos++
			continue
		}
		if marker == 0xD8 || (marker >= 0xD0 && marker <= 0xD7) || marker == 0x01 {
			pos += 2
			continue
		}
		if marker == 0xDA || marker == 0xD9 { // start of scan / end of image
			break
		}
		segLen := int64(binary.BigEndian.Uint16(hdr[2:4]))
		if segLen < 2 {
			break
		}
		if marker == 0xE1 && segLen > 14 {
			sig := make([]byte, 6)
			if _, err := r.ReadAt(sig, pos+4); err == nil && string(sig[:4]) == "Exif" {
				tiffBase := pos + 10
				limit := pos + 2 + segLen
				if limit > size {
					limit = size
				}
				if t, ok := p.fromTIFF(r, tiffBase, limit); ok {
					return t, true
				}
			}
		}
		pos += 2 + segLen
	}
	return time.Time{}, false
}

// ---------- PNG ----------

func (p *parser) fromPNG(r io.ReaderAt, size int64) (time.Time, bool) {
	pos := int64(8)
	hdr := make([]byte, 8)
	for steps := 0; steps < 10000 && pos+8 <= size; steps++ {
		if _, err := r.ReadAt(hdr, pos); err != nil {
			break
		}
		n := int64(binary.BigEndian.Uint32(hdr[0:4]))
		typ := string(hdr[4:8])
		if typ == "eXIf" {
			return p.fromTIFF(r, pos+8, min64(size, pos+8+n))
		}
		if typ == "IEND" {
			break
		}
		pos += 12 + n
	}
	return time.Time{}, false
}

// ---------- WebP ----------

func (p *parser) fromWebP(r io.ReaderAt, size int64) (time.Time, bool) {
	pos := int64(12)
	hdr := make([]byte, 8)
	for steps := 0; steps < 1000 && pos+8 <= size; steps++ {
		if _, err := r.ReadAt(hdr, pos); err != nil {
			break
		}
		n := int64(binary.LittleEndian.Uint32(hdr[4:8]))
		if string(hdr[0:4]) == "EXIF" {
			start := pos + 8
			sig := make([]byte, 6)
			if _, err := r.ReadAt(sig, start); err == nil && string(sig[:4]) == "Exif" {
				start += 6
			}
			return p.fromTIFF(r, start, min64(size, pos+8+n))
		}
		pos += 8 + n + (n & 1)
	}
	return time.Time{}, false
}

// ---------- ISO base media (HEIC, AVIF, MP4, MOV, 3GP) ----------

func isBoxType(b []byte) bool {
	switch string(b) {
	case "ftyp", "moov", "mdat", "wide", "free", "skip", "pnot":
		return true
	}
	return false
}

type box struct {
	typ       string
	start     int64 // offset of the box header
	dataStart int64 // offset of the payload
	end       int64 // offset just past the box
}

// boxes lists the boxes between start and end.
func boxes(r io.ReaderAt, start, end int64) []box {
	var out []box
	hdr := make([]byte, 16)
	pos := start
	for steps := 0; steps < 4096 && pos+8 <= end; steps++ {
		if _, err := r.ReadAt(hdr[:8], pos); err != nil {
			break
		}
		sz := int64(binary.BigEndian.Uint32(hdr[0:4]))
		typ := string(hdr[4:8])
		hl := int64(8)
		switch sz {
		case 1:
			if _, err := r.ReadAt(hdr[8:16], pos+8); err != nil {
				return out
			}
			sz = int64(binary.BigEndian.Uint64(hdr[8:16]))
			hl = 16
		case 0:
			sz = end - pos
		}
		if sz < hl || pos+sz > end {
			if pos+sz > end && sz >= hl {
				// tolerate a truncated last box
				out = append(out, box{typ: typ, start: pos, dataStart: pos + hl, end: end})
			}
			break
		}
		out = append(out, box{typ: typ, start: pos, dataStart: pos + hl, end: pos + sz})
		pos += sz
	}
	return out
}

func find(bs []box, typ string) (box, bool) {
	for _, b := range bs {
		if b.typ == typ {
			return b, true
		}
	}
	return box{}, false
}

func (p *parser) fromISOBMFF(r io.ReaderAt, size int64) (time.Time, bool) {
	top := boxes(r, 0, size)
	if meta, ok := find(top, "meta"); ok {
		if t, ok := p.fromHEIFMeta(r, meta, size); ok {
			return t, true
		}
	}
	if moov, ok := find(top, "moov"); ok {
		if mvhd, ok := find(boxes(r, moov.dataStart, moov.end), "mvhd"); ok {
			return fromMVHD(r, mvhd)
		}
	}
	return time.Time{}, false
}

var epoch1904 = time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC)

func fromMVHD(r io.ReaderAt, b box) (time.Time, bool) {
	buf := make([]byte, 12)
	if _, err := r.ReadAt(buf, b.dataStart); err != nil {
		return time.Time{}, false
	}
	var secs uint64
	if buf[0] == 1 {
		secs = binary.BigEndian.Uint64(buf[4:12])
	} else {
		secs = uint64(binary.BigEndian.Uint32(buf[4:8]))
	}
	if secs == 0 {
		return time.Time{}, false
	}
	return epoch1904.Add(time.Duration(secs) * time.Second).Local(), true
}

// fromHEIFMeta finds the "Exif" item in a HEIF meta box and parses it.
func (p *parser) fromHEIFMeta(r io.ReaderAt, meta box, size int64) (time.Time, bool) {
	kids := boxes(r, meta.dataStart+4, meta.end) // meta is a full box
	iinf, ok1 := find(kids, "iinf")
	iloc, ok2 := find(kids, "iloc")
	if !ok1 || !ok2 {
		return time.Time{}, false
	}
	exifID, ok := exifItemID(r, iinf)
	if !ok {
		return time.Time{}, false
	}
	var idatStart int64 = -1
	if idat, ok := find(kids, "idat"); ok {
		idatStart = idat.dataStart
	}
	off, length, ok := itemLocation(r, iloc, exifID, idatStart)
	if !ok || length < 8 || off+length > size {
		return time.Time{}, false
	}
	pre := make([]byte, 4)
	if _, err := r.ReadAt(pre, off); err != nil {
		return time.Time{}, false
	}
	skip := int64(binary.BigEndian.Uint32(pre))
	tiff := off + 4 + skip
	if skip > 64 || tiff >= off+length {
		return time.Time{}, false
	}
	return p.fromTIFF(r, tiff, off+length)
}

func exifItemID(r io.ReaderAt, iinf box) (uint32, bool) {
	hdr := make([]byte, 8)
	if _, err := r.ReadAt(hdr, iinf.dataStart); err != nil {
		return 0, false
	}
	start := iinf.dataStart + 6 // version(1) flags(3) count(2)
	if hdr[0] != 0 {
		start = iinf.dataStart + 8 // count is 32-bit
	}
	for _, infe := range boxes(r, start, iinf.end) {
		if infe.typ != "infe" {
			continue
		}
		b := make([]byte, 16)
		n, _ := r.ReadAt(b, infe.dataStart)
		b = b[:n]
		if len(b) < 12 {
			continue
		}
		ver := b[0]
		var id uint32
		var typ string
		switch {
		case ver == 2:
			id = uint32(binary.BigEndian.Uint16(b[4:6]))
			typ = string(b[8:12])
		case ver >= 3 && len(b) >= 14:
			id = binary.BigEndian.Uint32(b[4:8])
			typ = string(b[10:14])
		default:
			continue
		}
		if typ == "Exif" {
			return id, true
		}
	}
	return 0, false
}

func itemLocation(r io.ReaderAt, iloc box, want uint32, idatStart int64) (off, length int64, ok bool) {
	n := iloc.end - iloc.dataStart
	if n <= 0 || n > 1<<20 {
		return 0, 0, false
	}
	b := make([]byte, n)
	if _, err := r.ReadAt(b, iloc.dataStart); err != nil {
		return 0, 0, false
	}
	p := 0
	need := func(k int) bool { return p+k <= len(b) }
	readN := func(size int) (uint64, bool) {
		if size == 0 {
			return 0, true
		}
		if !need(size) {
			return 0, false
		}
		var v uint64
		for i := 0; i < size; i++ {
			v = v<<8 | uint64(b[p+i])
		}
		p += size
		return v, true
	}
	if !need(6) {
		return 0, 0, false
	}
	ver := b[0]
	p = 4
	offSize, lenSize := int(b[p]>>4), int(b[p]&0x0F)
	baseSize, idxSize := int(b[p+1]>>4), int(b[p+1]&0x0F)
	p += 2
	if ver != 1 && ver != 2 {
		idxSize = 0
	}
	var count uint64
	if ver < 2 {
		count, ok = readN(2)
	} else {
		count, ok = readN(4)
	}
	if !ok || count > 10000 {
		return 0, 0, false
	}
	for i := uint64(0); i < count; i++ {
		var id uint64
		if ver < 2 {
			id, ok = readN(2)
		} else {
			id, ok = readN(4)
		}
		if !ok {
			return 0, 0, false
		}
		method := uint64(0)
		if ver == 1 || ver == 2 {
			m, ok := readN(2)
			if !ok {
				return 0, 0, false
			}
			method = m & 0x0F
		}
		if _, ok := readN(2); !ok { // data_reference_index
			return 0, 0, false
		}
		baseOff, ok := readN(baseSize)
		if !ok {
			return 0, 0, false
		}
		extents, ok := readN(2)
		if !ok {
			return 0, 0, false
		}
		var firstOff, firstLen uint64
		for e := uint64(0); e < extents; e++ {
			if idxSize > 0 {
				if _, ok := readN(idxSize); !ok {
					return 0, 0, false
				}
			}
			eo, ok1 := readN(offSize)
			el, ok2 := readN(lenSize)
			if !ok1 || !ok2 {
				return 0, 0, false
			}
			if e == 0 {
				firstOff, firstLen = eo, el
			}
		}
		if uint32(id) != want {
			continue
		}
		start := int64(baseOff + firstOff)
		switch method {
		case 0:
		case 1:
			if idatStart < 0 {
				return 0, 0, false
			}
			start += idatStart
		default:
			return 0, 0, false
		}
		return start, int64(firstLen), true
	}
	return 0, 0, false
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
