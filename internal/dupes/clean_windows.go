//go:build windows

package dupes

import (
	"errors"
	"io/fs"
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modShell32           = windows.NewLazySystemDLL("shell32.dll")
	procSHFileOperationW = modShell32.NewProc("SHFileOperationW")
)

type shFileOpStruct struct {
	hwnd                  uintptr
	wFunc                 uint32
	pFrom                 *uint16
	pTo                   *uint16
	fFlags                uint16
	fAnyOperationsAborted int32
	hNameMappings         uintptr
	lpszProgressTitle     *uint16
}

const (
	foDelete           = 3
	fofSilent          = 0x0004
	fofNoConfirmation  = 0x0010
	fofAllowUndo       = 0x0040
	fofNoErrorUI       = 0x0400
	fofWantNukeWarning = 0x4000
)

// Recycle sends files to the Recycle Bin. If a file cannot go there (for
// example on a USB stick), Windows asks before deleting it for good. gone
// tells, per path, whether the file is no longer there; canceled is true
// when the person said no to such a question.
func Recycle(owner uintptr, paths []string, progress func(done, total int)) (gone []bool, canceled bool) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE); err == nil {
		defer windows.CoUninitialize()
	}
	gone = make([]bool, len(paths))
	const batch = 64
	for start := 0; start < len(paths); start += batch {
		end := start + batch
		if end > len(paths) {
			end = len(paths)
		}
		var list []uint16
		for _, p := range paths[start:end] {
			u, err := windows.UTF16FromString(p)
			if err != nil {
				continue
			}
			list = append(list, u...) // includes the terminating 0
		}
		list = append(list, 0) // list ends with a second 0
		op := shFileOpStruct{
			hwnd:   owner,
			wFunc:  foDelete,
			pFrom:  &list[0],
			fFlags: fofAllowUndo | fofNoConfirmation | fofWantNukeWarning | fofSilent | fofNoErrorUI,
		}
		procSHFileOperationW.Call(uintptr(unsafe.Pointer(&op)))
		for i := start; i < end; i++ {
			if _, err := os.Lstat(paths[i]); errors.Is(err, fs.ErrNotExist) {
				gone[i] = true
			}
		}
		if progress != nil {
			progress(end, len(paths))
		}
		if op.fAnyOperationsAborted != 0 {
			return gone, true
		}
	}
	return gone, false
}

// moveFile moves a file, also to another drive, never replacing anything.
func moveFile(from, to string) error {
	f, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	t, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	if err := windows.MoveFileEx(f, t, windows.MOVEFILE_COPY_ALLOWED|windows.MOVEFILE_WRITE_THROUGH); err != nil {
		return &os.LinkError{Op: "move", Old: from, New: to, Err: err}
	}
	return nil
}
