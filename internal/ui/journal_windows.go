//go:build windows

package ui

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"

	"github.com/game-addicted-man/NameSmith/internal/engine"
)

// recoverJournals finishes renames that were cut off (crash, power loss) in
// NameSmith windows that are no longer running.
func recoverJournals() (fixed int, problems []string) {
	files, _ := filepath.Glob(filepath.Join(dataDir(), "journal*.json"))
	for _, f := range files {
		var pid int
		if _, err := fmt.Sscanf(filepath.Base(f), "journal-%d.json", &pid); err == nil {
			if pid == os.Getpid() || processAlive(pid) {
				continue // still being written by a running NameSmith
			}
		}
		n, p := engine.Recover(f, nil)
		fixed += n
		problems = append(problems, p...)
	}
	return fixed, problems
}

func processAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == 259 // STILL_ACTIVE
}
