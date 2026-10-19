package engine

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MaxItems protects against dropping a whole drive by accident.
const MaxItems = 50000

// CollectResult is what Collect found.
type CollectResult struct {
	Items     []*Item
	Duplicate int  // already in the list
	Hidden    int  // hidden or system files skipped inside folders
	Failed    int  // could not be read
	Truncated bool // stopped at MaxItems
}

// Collect turns dropped or chosen paths into items. Folders are expanded
// (with their subfolders when recursive). Paths whose key is in known are
// counted as duplicates and skipped; new keys are added to known.
func Collect(paths []string, recursive bool, known map[string]bool, room int) CollectResult {
	var res CollectResult
	if room <= 0 {
		room = MaxItems
	}
	add := func(dir string, info fs.FileInfo) bool {
		if len(res.Items) >= room {
			res.Truncated = true
			return false
		}
		path := filepath.Join(dir, info.Name())
		k := PathKey(path)
		if known[k] {
			res.Duplicate++
			return true
		}
		known[k] = true
		created, _ := statExtra(info)
		if created.IsZero() {
			created = info.ModTime()
		}
		res.Items = append(res.Items, &Item{
			Dir:      dir,
			Name:     info.Name(),
			Size:     info.Size(),
			Modified: info.ModTime(),
			Created:  created,
			Checked:  true,
		})
		return true
	}

	var walkDir func(dir string) bool
	walkDir = func(dir string) bool {
		entries, err := os.ReadDir(dir)
		if err != nil {
			res.Failed++
			return true
		}
		sort.SliceStable(entries, func(i, j int) bool { return NaturalLess(entries[i].Name(), entries[j].Name()) })
		var subdirs []string
		for _, e := range entries {
			info, err := e.Info()
			if err != nil {
				res.Failed++
				continue
			}
			if _, hidden := statExtra(info); hidden || isOwnTemp(e.Name()) {
				if !e.IsDir() {
					res.Hidden++
				}
				continue
			}
			mode := info.Mode()
			if e.IsDir() {
				if recursive && mode&fs.ModeSymlink == 0 && mode&fs.ModeIrregular == 0 {
					subdirs = append(subdirs, filepath.Join(dir, e.Name()))
				}
				continue
			}
			if !mode.IsRegular() {
				continue
			}
			if !add(dir, info) {
				return false
			}
		}
		for _, sd := range subdirs {
			if !walkDir(sd) {
				return false
			}
		}
		return true
	}

	var files []string
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		info, err := os.Stat(p)
		if err != nil {
			res.Failed++
			continue
		}
		if info.IsDir() {
			if !walkDir(filepath.Clean(p)) {
				break
			}
			continue
		}
		files = append(files, filepath.Clean(p))
	}

	// Loose files are added in natural name order, folder by folder.
	sort.SliceStable(files, func(i, j int) bool {
		di, dj := filepath.Dir(files[i]), filepath.Dir(files[j])
		if !strings.EqualFold(di, dj) {
			return NaturalLess(di, dj)
		}
		return NaturalLess(filepath.Base(files[i]), filepath.Base(files[j]))
	})
	for _, f := range files {
		if res.Truncated {
			break
		}
		info, err := os.Stat(f)
		if err != nil || !info.Mode().IsRegular() {
			res.Failed++
			continue
		}
		add(filepath.Dir(f), info)
	}
	return res
}

func isOwnTemp(name string) bool {
	return strings.HasPrefix(name, ".namesmith-") && strings.HasSuffix(name, ".tmp")
}

// Refresh re-reads size and dates of an item from disk. It returns false
// when the file no longer exists.
func Refresh(it *Item) bool {
	info, err := os.Stat(it.Path())
	if err != nil || info.IsDir() {
		return false
	}
	it.Size = info.Size()
	it.Modified = info.ModTime()
	if created, _ := statExtra(info); !created.IsZero() {
		it.Created = created
	} else {
		it.Created = info.ModTime()
	}
	return true
}

// ListDir returns the names of everything in dir.
func ListDir(dir string) ([]string, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.Readdirnames(-1)
}
