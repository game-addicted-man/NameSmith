//go:build !windows

package engine

import (
	"io/fs"
	"strings"
	"time"
)

// statExtra: on systems without a creation time the modified time is used,
// and dot-files count as hidden.
func statExtra(info fs.FileInfo) (created time.Time, hidden bool) {
	return info.ModTime(), strings.HasPrefix(info.Name(), ".")
}
