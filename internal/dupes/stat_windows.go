//go:build windows

package dupes

import (
	"os"
	"syscall"
	"time"
)

const (
	attrOffline            = 0x00001000
	attrRecallOnOpen       = 0x00040000
	attrRecallOnDataAccess = 0x00400000
)

func attrs(info os.FileInfo) *syscall.Win32FileAttributeData {
	d, _ := info.Sys().(*syscall.Win32FileAttributeData)
	return d
}

// skipKind: hidden/system files are left alone, and online-only cloud files
// (OneDrive and the like) are skipped because reading them would download them.
func skipKind(info os.FileInfo) skipReason {
	d := attrs(info)
	if d == nil {
		return skipNone
	}
	if d.FileAttributes&(syscall.FILE_ATTRIBUTE_HIDDEN|syscall.FILE_ATTRIBUTE_SYSTEM) != 0 {
		return skipHidden
	}
	if d.FileAttributes&(attrOffline|attrRecallOnOpen|attrRecallOnDataAccess) != 0 {
		return skipCloud
	}
	return skipNone
}

func createdTime(info os.FileInfo) time.Time {
	if d := attrs(info); d != nil {
		return time.Unix(0, d.CreationTime.Nanoseconds())
	}
	return info.ModTime()
}
