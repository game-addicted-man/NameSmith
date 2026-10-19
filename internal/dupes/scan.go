// Package dupes finds duplicate files: files with exactly the same content,
// and photos that look alike (resized, re-saved or slightly edited copies,
// shots taken a moment apart). It has no GUI code.
package dupes

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// Kind narrows which files "같은 파일" looks at.
type Kind int

const (
	KindAll Kind = iota
	KindPhotos
	KindVideos
	KindMusic
	KindDocuments
	KindArchives
)

// KindLabels are shown in the "파일 종류" box, in Kind order.
var KindLabels = []string{"모든 파일", "사진", "동영상", "음악", "문서", "압축 파일"}

var kindExts = map[Kind][]string{
	KindPhotos: {".jpg", ".jpeg", ".jpe", ".jfif", ".png", ".gif", ".bmp", ".dib", ".webp", ".tif", ".tiff",
		".heic", ".heif", ".hif", ".avif", ".dng", ".cr2", ".cr3", ".nef", ".nrw", ".arw", ".srf", ".sr2", ".orf", ".rw2", ".pef", ".raf", ".svg", ".psd", ".ico"},
	KindVideos: {".mp4", ".m4v", ".mov", ".qt", ".avi", ".wmv", ".mkv", ".webm", ".flv", ".3gp", ".3g2", ".mts", ".m2ts", ".ts", ".mpg", ".mpeg", ".vob"},
	KindMusic:  {".mp3", ".m4a", ".aac", ".wav", ".flac", ".ogg", ".oga", ".opus", ".wma", ".aiff", ".aif", ".alac", ".mid", ".midi"},
	KindDocuments: {".pdf", ".hwp", ".hwpx", ".doc", ".docx", ".xls", ".xlsx", ".csv", ".ppt", ".pptx", ".txt", ".rtf", ".odt", ".ods", ".odp",
		".md", ".epub", ".xps", ".pages", ".key", ".numbers"},
	KindArchives: {".zip", ".7z", ".rar", ".alz", ".egg", ".tar", ".gz", ".tgz", ".bz2", ".xz", ".iso", ".cab"},
}

// Matches reports whether name belongs to kind k.
func (k Kind) Matches(name string) bool {
	if k == KindAll {
		return true
	}
	ext := strings.ToLower(filepath.Ext(name))
	for _, e := range kindExts[k] {
		if e == ext {
			return true
		}
	}
	return false
}

// Level is how alike photos must be to count as "비슷한 사진".
type Level int

const (
	LevelStrict Level = iota // 거의 똑같은 사진만
	LevelNormal              // 비슷한 사진 (추천)
	LevelLoose               // 조금 비슷한 사진까지
)

// LevelLabels are shown in the "비슷한 정도" box, in Level order.
var LevelLabels = []string{"거의 똑같은 사진만", "비슷한 사진 (추천)", "조금 비슷한 사진까지 (연속 촬영 등)"}

// Match reports whether two photos at these hash distances count as
// similar at this level. The numbers come from measuring real photos:
// re-saved, resized, brightened or captioned copies stay under about 24 fine
// / 9 coarse bits, a 3% crop under about 44 coarse bits, while different
// photos were never closer than 172 fine / 34 coarse bits.
func (l Level) Match(fine, coarse int) bool {
	switch l {
	case LevelStrict:
		return fine <= 32 && coarse <= 8
	case LevelLoose:
		return (coarse <= 22 && fine <= 240) || fine <= 96
	default:
		return (coarse <= 12 && fine <= 200) || fine <= 48
	}
}

// Options says where and what to look for.
type Options struct {
	Roots     []string
	Recursive bool
	Exact     bool  // same content
	Similar   bool  // similar photos
	Kind      Kind  // which files the exact search looks at
	Level     Level // how alike photos must be
	MinSize   int64 // the exact search ignores smaller files (at least 1 byte)
	Workers   int   // 0 = automatic
	CachePath string
}

// File is one file that was looked at.
type File struct {
	Path     string
	Dir      string
	Name     string
	Size     int64
	Modified time.Time
	Created  time.Time
	Root     int // index of the folder in Options.Roots it was found in

	sum    [32]byte // SHA-256 of the content (exact search)
	hasSum bool

	hash        Hash // photo hash, upright
	hasHash     bool
	W, H        int // photo size as shown (0 when not a photo or unread)
	Taken       time.Time
	Orientation int
}

// SameContent reports whether both files were found to have identical content.
func (f *File) SameContent(o *File) bool {
	return f.hasSum && o.hasSum && f.Size == o.Size && f.sum == o.sum
}

// Phase is the step a scan is in.
type Phase int

const (
	PhaseList    Phase = iota // 파일 목록 만드는 중
	PhaseCompare              // 같은 파일 비교 중
	PhasePhotos               // 사진 살펴보는 중
	PhaseGroup                // 비슷한 사진 묶는 중
)

// Progress is reported while scanning.
type Progress struct {
	Phase       Phase
	Done, Total int
	Found       int // files listed so far
}

// Skipped counts files that were left out.
type Skipped struct {
	Hidden     int // hidden or system files
	Cloud      int // online-only (not downloaded) cloud files
	Unreadable int // could not be opened
	Pictures   int // photos that could not be decoded
}

// Result of a scan.
type Result struct {
	Groups  []*Group
	Files   int // files looked at
	Skipped Skipped
	Elapsed time.Duration
}

// MaxFiles protects memory when a whole drive is chosen.
const MaxFiles = 2_000_000

// Scan looks through the folders and returns the groups of duplicates.
// It can be cancelled with ctx; progress may be nil.
func Scan(ctx context.Context, opt Options, progress func(Progress)) (*Result, error) {
	start := time.Now()
	if progress == nil {
		progress = func(Progress) {}
	}
	if opt.Workers <= 0 {
		opt.Workers = runtime.NumCPU()
		if opt.Workers > 8 {
			opt.Workers = 8
		}
	}
	if opt.MinSize < 1 {
		opt.MinSize = 1
	}
	res := &Result{}

	files, err := listFiles(ctx, opt, &res.Skipped, progress)
	if err != nil {
		return nil, err
	}
	res.Files = len(files)

	var exact [][]*File
	if opt.Exact {
		var cand []*File
		for _, f := range files {
			if f.Size >= opt.MinSize && opt.Kind.Matches(f.Name) {
				cand = append(cand, f)
			}
		}
		exact, err = findExact(ctx, cand, opt.Workers, &res.Skipped, func(done, total int) {
			progress(Progress{Phase: PhaseCompare, Done: done, Total: total, Found: len(files)})
		})
		if err != nil {
			return nil, err
		}
	}

	var groups []*Group
	if opt.Similar {
		groups, err = findSimilar(ctx, files, exact, opt, &res.Skipped, progress)
		if err != nil {
			return nil, err
		}
	} else {
		for _, set := range exact {
			groups = append(groups, newExactGroup(set))
		}
	}
	for _, g := range groups {
		g.ChooseKeep(KeepRecommended, len(opt.Roots))
	}
	sortGroups(groups)
	res.Groups = groups
	res.Elapsed = time.Since(start)
	return res, nil
}

// ---------- listing ----------

// skipDir reports folders that must never be touched: Windows itself and
// installed programs at the root of a drive, the recycle bin and so on.
func skipDir(path, name string) bool {
	switch strings.ToLower(name) {
	case "$recycle.bin", "system volume information", "$windows.~bt", "$windows.~ws", "windowsapps", "$sysreset", "recovery":
		return true
	}
	parent := filepath.Dir(path)
	if isDriveRoot(parent) {
		switch strings.ToLower(name) {
		case "windows", "program files", "program files (x86)", "programdata", "msocache", "perflogs":
			return true
		}
	}
	return false
}

func isDriveRoot(p string) bool {
	p = filepath.Clean(p)
	return len(p) <= 3 && len(p) >= 2 && p[1] == ':' || p == "/"
}

func listFiles(ctx context.Context, opt Options, sk *Skipped, progress func(Progress)) ([]*File, error) {
	seen := map[string]bool{}
	var files []*File
	lastReport := time.Now()

	add := func(root int, dir string, info os.FileInfo) {
		path := filepath.Join(dir, info.Name())
		key := strings.ToUpper(path)
		if seen[key] {
			return
		}
		seen[key] = true
		created := createdTime(info)
		files = append(files, &File{
			Path: path, Dir: dir, Name: info.Name(), Size: info.Size(),
			Modified: info.ModTime(), Created: created, Root: root,
		})
		if time.Since(lastReport) > 150*time.Millisecond {
			lastReport = time.Now()
			progress(Progress{Phase: PhaseList, Found: len(files)})
		}
	}

	type dirItem struct {
		root int
		path string
	}
	for ri, r := range opt.Roots {
		r = filepath.Clean(strings.TrimSpace(r))
		if r == "" || r == "." {
			continue
		}
		info, err := os.Stat(r)
		if err != nil {
			sk.Unreadable++
			continue
		}
		if !info.IsDir() {
			if info.Mode().IsRegular() {
				switch skipKind(info) {
				case skipHidden:
					sk.Hidden++
				case skipCloud:
					sk.Cloud++
				default:
					add(ri, filepath.Dir(r), info)
				}
			}
			continue
		}
		stack := []dirItem{{ri, r}}
		for len(stack) > 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if len(files) >= MaxFiles {
				return files, nil
			}
			d := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			entries, err := os.ReadDir(d.path)
			if err != nil {
				sk.Unreadable++
				continue
			}
			var subdirs []string
			for _, e := range entries {
				info, err := e.Info()
				if err != nil {
					continue
				}
				switch skipKind(info) {
				case skipHidden:
					if !e.IsDir() {
						sk.Hidden++
					}
					continue
				case skipCloud:
					if !e.IsDir() {
						sk.Cloud++
						continue
					}
				}
				mode := info.Mode()
				if e.IsDir() {
					if opt.Recursive && mode&(os.ModeSymlink|os.ModeIrregular) == 0 && !skipDir(filepath.Join(d.path, e.Name()), e.Name()) {
						subdirs = append(subdirs, filepath.Join(d.path, e.Name()))
					}
					continue
				}
				if !mode.IsRegular() || strings.HasPrefix(e.Name(), ".namesmith-") {
					continue
				}
				add(d.root, d.path, info)
			}
			// visit subfolders in name order
			sort.Slice(subdirs, func(i, j int) bool { return subdirs[i] > subdirs[j] })
			for _, s := range subdirs {
				stack = append(stack, dirItem{d.root, s})
			}
		}
	}
	progress(Progress{Phase: PhaseList, Found: len(files)})
	return files, nil
}

type skipReason int

const (
	skipNone skipReason = iota
	skipHidden
	skipCloud
)
