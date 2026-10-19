//go:build windows

package engine

import (
	"io/fs"
	"syscall"
	"time"
)

// statExtra returns the creation time and whether the file is hidden or a
// system file (such as desktop.ini or Thumbs.db).
func statExtra(info fs.FileInfo) (created time.Time, hidden bool) {
	d, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok || d == nil {
		return time.Time{}, false
	}
	created = time.Unix(0, d.CreationTime.Nanoseconds())
	hidden = d.FileAttributes&(syscall.FILE_ATTRIBUTE_HIDDEN|syscall.FILE_ATTRIBUTE_SYSTEM) != 0
	return created, hidden
}
