package meta

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// tiffWithDate builds a minimal TIFF block: IFD0 → ExifIFD → DateTimeOriginal.
func tiffWithDate(bo binary.ByteOrder, date string) []byte {
	var b bytes.Buffer
	if bo == binary.LittleEndian {
		b.WriteString("II")
	} else {
		b.WriteString("MM")
	}
	w16 := func(v uint16) { binary.Write(&b, bo, v) }
	w32 := func(v uint32) { binary.Write(&b, bo, v) }
	w16(42)
	w32(8) // IFD0 at 8
	// IFD0: 1 entry (ExifIFD pointer)
	w16(1)
	w16(0x8769)
	w16(4)
	w32(1)
	w32(8 + 2 + 12 + 4) // Exif IFD right after IFD0
	w32(0)              // next IFD
	// Exif IFD: 1 entry (DateTimeOriginal, ASCII, 20 bytes)
	exifIFD := uint32(b.Len())
	w16(1)
	w16(0x9003)
	w16(2)
	w32(20)
	w32(exifIFD + 2 + 12 + 4)
	w32(0)
	b.WriteString(date)
	b.WriteByte(0)
	return b.Bytes()
}

func jpegWithExif(tiff []byte) []byte {
	var b bytes.Buffer
	b.Write([]byte{0xFF, 0xD8})
	// an APP0 segment first, like real cameras
	b.Write([]byte{0xFF, 0xE0, 0x00, 0x10})
	b.WriteString("JFIF\x00")
	b.Write(make([]byte, 9))
	payload := append([]byte("Exif\x00\x00"), tiff...)
	b.Write([]byte{0xFF, 0xE1})
	binary.Write(&b, binary.BigEndian, uint16(len(payload)+2))
	b.Write(payload)
	b.Write([]byte{0xFF, 0xDA, 0x00, 0x02, 0xFF, 0xD9})
	return b.Bytes()
}

func boxBytes(typ string, payload []byte) []byte {
	var b bytes.Buffer
	binary.Write(&b, binary.BigEndian, uint32(8+len(payload)))
	b.WriteString(typ)
	b.Write(payload)
	return b.Bytes()
}

func write(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func want(t *testing.T, path string, y int, mo time.Month, d, h, mi, s int) {
	t.Helper()
	got, ok := DateTaken(path)
	if !ok {
		t.Fatalf("%s: no date found", filepath.Base(path))
	}
	exp := time.Date(y, mo, d, h, mi, s, 0, time.Local)
	if !got.Equal(exp) {
		t.Fatalf("%s: got %v want %v", filepath.Base(path), got, exp)
	}
}

func TestJPEG(t *testing.T) {
	want(t, write(t, "a.jpg", jpegWithExif(tiffWithDate(binary.LittleEndian, "2019:08:15 10:20:30"))), 2019, 8, 15, 10, 20, 30)
	want(t, write(t, "b.JPEG", jpegWithExif(tiffWithDate(binary.BigEndian, "2021:12:31 23:59:58"))), 2021, 12, 31, 23, 59, 58)
}

func TestTIFFRaw(t *testing.T) {
	want(t, write(t, "c.dng", tiffWithDate(binary.LittleEndian, "2020:01:02 03:04:05")), 2020, 1, 2, 3, 4, 5)
}

func TestPNG(t *testing.T) {
	var b bytes.Buffer
	b.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'})
	chunk := func(typ string, data []byte) {
		binary.Write(&b, binary.BigEndian, uint32(len(data)))
		b.WriteString(typ)
		b.Write(data)
		b.Write([]byte{0, 0, 0, 0}) // CRC (not checked)
	}
	chunk("IHDR", make([]byte, 13))
	chunk("eXIf", tiffWithDate(binary.BigEndian, "2018:03:04 05:06:07"))
	chunk("IEND", nil)
	want(t, write(t, "d.png", b.Bytes()), 2018, 3, 4, 5, 6, 7)
}

func TestWebP(t *testing.T) {
	exif := append([]byte("Exif\x00\x00"), tiffWithDate(binary.LittleEndian, "2017:07:07 07:07:07")...)
	var chunks bytes.Buffer
	chunks.WriteString("VP8 ")
	binary.Write(&chunks, binary.LittleEndian, uint32(4))
	chunks.Write([]byte{1, 2, 3, 4})
	chunks.WriteString("EXIF")
	binary.Write(&chunks, binary.LittleEndian, uint32(len(exif)))
	chunks.Write(exif)
	if len(exif)%2 == 1 {
		chunks.WriteByte(0)
	}
	var b bytes.Buffer
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(4+chunks.Len()))
	b.WriteString("WEBP")
	b.Write(chunks.Bytes())
	want(t, write(t, "e.webp", b.Bytes()), 2017, 7, 7, 7, 7, 7)
}

func TestMP4(t *testing.T) {
	// 2023-06-01 12:00:00 UTC as seconds since 1904
	when := time.Date(2023, 6, 1, 12, 0, 0, 0, time.UTC)
	secs := uint32(when.Sub(epoch1904) / time.Second)
	mvhd := make([]byte, 100)
	binary.BigEndian.PutUint32(mvhd[4:8], secs)
	var file bytes.Buffer
	file.Write(boxBytes("ftyp", []byte("isom\x00\x00\x02\x00isomiso2mp41")))
	file.Write(boxBytes("mdat", make([]byte, 64)))
	file.Write(boxBytes("moov", boxBytes("mvhd", mvhd)))
	got, ok := DateTaken(write(t, "f.mp4", file.Bytes()))
	if !ok || !got.Equal(when) {
		t.Fatalf("mp4: got %v %v want %v", got, ok, when)
	}
}

func TestHEIC(t *testing.T) {
	tiff := tiffWithDate(binary.BigEndian, "2022:10:09 08:07:06")
	exifItem := append([]byte{0, 0, 0, 6}, append([]byte("Exif\x00\x00"), tiff...)...)

	// infe v2: version(1) flags(3) item_ID(2) protection(2) type(4) name("\0")
	infe := boxBytes("infe", append([]byte{2, 0, 0, 0, 0, 7, 0, 0}, []byte("Exif\x00")...))
	infeImg := boxBytes("infe", append([]byte{2, 0, 0, 0, 0, 1, 0, 0}, []byte("hvc1\x00")...))
	iinf := boxBytes("iinf", append([]byte{0, 0, 0, 0, 0, 2}, append(infeImg, infe...)...))

	build := func(exifOffset uint32) []byte {
		// iloc v1, offset_size 4, length_size 4, base_offset_size 0, index_size 0
		var il bytes.Buffer
		il.Write([]byte{1, 0, 0, 0, 0x44, 0x00})
		binary.Write(&il, binary.BigEndian, uint16(1)) // item count
		binary.Write(&il, binary.BigEndian, uint16(7)) // item ID
		binary.Write(&il, binary.BigEndian, uint16(0)) // construction method 0
		binary.Write(&il, binary.BigEndian, uint16(0)) // data ref index
		binary.Write(&il, binary.BigEndian, uint16(1)) // extent count
		binary.Write(&il, binary.BigEndian, exifOffset)
		binary.Write(&il, binary.BigEndian, uint32(len(exifItem)))
		iloc := boxBytes("iloc", il.Bytes())
		meta := boxBytes("meta", append([]byte{0, 0, 0, 0}, append(boxBytes("hdlr", make([]byte, 25)), append(iinf, iloc...)...)...))
		var f bytes.Buffer
		f.Write(boxBytes("ftyp", []byte("heic\x00\x00\x00\x00mif1heic")))
		f.Write(meta)
		f.Write(boxBytes("mdat", exifItem))
		return f.Bytes()
	}
	first := build(0)
	exifAt := uint32(len(first) - len(exifItem))
	want(t, write(t, "g.heic", build(exifAt)), 2022, 10, 9, 8, 7, 6)
}

func TestGarbageIsSafe(t *testing.T) {
	cases := [][]byte{
		nil,
		{0xFF, 0xD8, 0xFF, 0xE1, 0xFF, 0xFF, 'E', 'x', 'i', 'f'},
		[]byte("II*\x00\xff\xff\xff\xff"),
		boxBytes("ftyp", []byte("heic")),
		append(boxBytes("ftyp", []byte("heic")), []byte{0, 0, 0, 1, 'm', 'e', 't', 'a', 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}...),
		jpegWithExif(tiffWithDate(binary.LittleEndian, "0000:00:00 00:00:00")),
		jpegWithExif(tiffWithDate(binary.LittleEndian, "abcd:ef:gh ij:kl:mn")),
	}
	for i, c := range cases {
		if _, ok := DateTaken(write(t, "x.jpg", c)); ok {
			t.Errorf("case %d: expected no date", i)
		}
	}
	if _, ok := DateTaken(write(t, "notes.txt", jpegWithExif(tiffWithDate(binary.LittleEndian, "2019:08:15 10:20:30")))); ok {
		t.Error("non-media extension should be ignored")
	}
}
