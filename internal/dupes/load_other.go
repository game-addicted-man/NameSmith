//go:build !windows

package dupes

// NewLoader returns the photo reader for this system.
func NewLoader() Loader { return goLoader{} }
