//go:build windows

package ui

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The modern Explorer-style picker (IFileOpenDialog). It can select many
// files, or many folders, and has no buffer limit.

var (
	modOle32                        = windows.NewLazySystemDLL("ole32.dll")
	modShell32                      = windows.NewLazySystemDLL("shell32.dll")
	procCoCreateInstance            = modOle32.NewProc("CoCreateInstance")
	procSHCreateItemFromParsingName = modShell32.NewProc("SHCreateItemFromParsingName")

	clsidFileOpenDialog = windows.GUID{Data1: 0xDC1C5A9C, Data2: 0xE88A, Data3: 0x4DDE, Data4: [8]byte{0xA5, 0xA1, 0x60, 0xF8, 0x2A, 0x20, 0xAE, 0xF7}}
	iidIFileOpenDialog  = windows.GUID{Data1: 0xD57C7288, Data2: 0xD4AD, Data3: 0x4768, Data4: [8]byte{0xBE, 0x02, 0x9D, 0x96, 0x95, 0x32, 0xD9, 0x60}}
	iidIShellItem       = windows.GUID{Data1: 0x43826D1E, Data2: 0xE718, Data3: 0x42EE, Data4: [8]byte{0xBC, 0x55, 0xA1, 0xE2, 0x61, 0xC3, 0x7B, 0xFE}}
)

const (
	fosNoChangeDir     = 0x00000008
	fosPickFolders     = 0x00000020
	fosForceFileSystem = 0x00000040
	fosAllowMulti      = 0x00000200
	fosPathMustExist   = 0x00000800
	fosFileMustExist   = 0x00001000

	sigdnFileSysPath = 0x80058000
	hrCancelled      = 0x800704C7
)

// vtable slots
const (
	vtRelease = 2
	// IModalWindow
	vtShow = 3
	// IFileDialog
	vtSetOptions = 9
	vtGetOptions = 10
	vtSetFolder  = 12
	vtSetTitle   = 17
	vtSetOkLabel = 18
	// IFileOpenDialog
	vtGetResults = 27
	// IShellItem
	vtGetDisplayName = 5
	// IShellItemArray
	vtGetCount  = 7
	vtGetItemAt = 8
)

type comObject struct {
	vtbl *[32]uintptr
}

// call invokes a COM method. uintptrescapes keeps pointer arguments on the
// heap and alive during the call.
//
//go:uintptrescapes
func (o *comObject) call(slot int, args ...uintptr) uint32 {
	all := make([]uintptr, 0, len(args)+1)
	all = append(all, uintptr(unsafe.Pointer(o)))
	all = append(all, args...)
	r, _, _ := syscall.SyscallN(o.vtbl[slot], all...)
	return uint32(r)
}

func (o *comObject) release() {
	if o != nil {
		o.call(vtRelease)
	}
}

// pickPaths shows the picker. folders=true picks folders instead of files.
// It returns nil (and no error) when the user cancels.
func pickPaths(owner windows.HWND, folders bool, title, okLabel, startDir string) ([]string, error) {
	var dlg *comObject
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidFileOpenDialog)), 0, 1, // CLSCTX_INPROC_SERVER
		uintptr(unsafe.Pointer(&iidIFileOpenDialog)), uintptr(unsafe.Pointer(&dlg)))
	if uint32(hr) != 0 || dlg == nil {
		return nil, fmt.Errorf("파일 선택 창을 열 수 없어요 (0x%08X)", uint32(hr))
	}
	defer dlg.release()

	var opts uint32
	dlg.call(vtGetOptions, uintptr(unsafe.Pointer(&opts)))
	opts |= fosForceFileSystem | fosAllowMulti | fosPathMustExist | fosNoChangeDir
	if folders {
		opts |= fosPickFolders
	} else {
		opts |= fosFileMustExist
	}
	dlg.call(vtSetOptions, uintptr(opts))

	if t, err := windows.UTF16PtrFromString(title); err == nil {
		dlg.call(vtSetTitle, uintptr(unsafe.Pointer(t)))
	}
	if okLabel != "" {
		if t, err := windows.UTF16PtrFromString(okLabel); err == nil {
			dlg.call(vtSetOkLabel, uintptr(unsafe.Pointer(t)))
		}
	}
	if startDir != "" {
		if p, err := windows.UTF16PtrFromString(startDir); err == nil {
			var folder *comObject
			r, _, _ := procSHCreateItemFromParsingName.Call(uintptr(unsafe.Pointer(p)), 0,
				uintptr(unsafe.Pointer(&iidIShellItem)), uintptr(unsafe.Pointer(&folder)))
			if uint32(r) == 0 && folder != nil {
				dlg.call(vtSetFolder, uintptr(unsafe.Pointer(folder)))
				folder.release()
			}
		}
	}

	switch r := dlg.call(vtShow, uintptr(owner)); r {
	case 0:
	case hrCancelled:
		return nil, nil
	default:
		return nil, fmt.Errorf("파일 선택 창 오류 (0x%08X)", r)
	}

	var arr *comObject
	if r := dlg.call(vtGetResults, uintptr(unsafe.Pointer(&arr))); r != 0 || arr == nil {
		return nil, fmt.Errorf("고른 항목을 읽을 수 없어요 (0x%08X)", r)
	}
	defer arr.release()

	var n uint32
	arr.call(vtGetCount, uintptr(unsafe.Pointer(&n)))
	paths := make([]string, 0, n)
	for i := uint32(0); i < n; i++ {
		var item *comObject
		if arr.call(vtGetItemAt, uintptr(i), uintptr(unsafe.Pointer(&item))) != 0 || item == nil {
			continue
		}
		var psz *uint16
		if item.call(vtGetDisplayName, uintptr(sigdnFileSysPath), uintptr(unsafe.Pointer(&psz))) == 0 && psz != nil {
			paths = append(paths, windows.UTF16PtrToString(psz))
			windows.CoTaskMemFree(unsafe.Pointer(psz))
		}
		item.release()
	}
	return paths, nil
}
