// Package engine holds everything NameSmith knows about renaming files:
// the rules that build a new name, the preview with conflict checks, and the
// safe two-phase rename with undo. It has no GUI code, so it is tested on any OS.
package engine

import (
	"path/filepath"
	"strings"
	"time"
)

// Item is one file in the list.
type Item struct {
	Dir      string    // folder that holds the file
	Name     string    // current file name, extension included
	Size     int64     // bytes
	Modified time.Time // last write time
	Created  time.Time // creation time (falls back to Modified where unknown)

	Taken       time.Time // photo/video capture time; zero when the file has none
	TakenLoaded bool      // whether Taken has been looked up yet

	Checked bool // included in the rename
}

// Path returns the full path of the file.
func (it *Item) Path() string { return filepath.Join(it.Dir, it.Name) }

// Status says what will happen to an item.
type Status int

const (
	StatusUnchanged Status = iota // the rules give the same name
	StatusChanged                 // will be renamed
	StatusConflict                // new name collides with another file
	StatusInvalid                 // new name is not allowed on Windows
	StatusSkipped                 // unchecked by the user
)

// Label is the short Korean text shown in the status column.
func (s Status) Label() string {
	switch s {
	case StatusChanged:
		return "바뀜"
	case StatusConflict:
		return "겹침"
	case StatusInvalid:
		return "오류"
	case StatusSkipped:
		return "제외"
	default:
		return "그대로"
	}
}

// SplitName splits a file name into base and extension (with its dot).
// A name such as ".gitignore" has no extension: the whole name is the base.
func SplitName(name string) (base, ext string) {
	ext = filepath.Ext(name)
	if ext == name || ext == "." {
		return name, ""
	}
	return name[:len(name)-len(ext)], ext
}

// foldKey makes a case-insensitive key the way Windows (NTFS) compares names.
func foldKey(s string) string { return strings.ToUpper(s) }

// pathKey is the case-insensitive key of a file inside a folder.
func pathKey(dir, name string) string {
	return foldKey(filepath.Clean(dir)) + "\x00" + foldKey(name)
}

// DirKey is the case-insensitive key of a folder.
func DirKey(dir string) string { return foldKey(filepath.Clean(dir)) }

// PathKey is the case-insensitive key of a full path, used to drop duplicates.
func PathKey(path string) string {
	return foldKey(filepath.Clean(path))
}
