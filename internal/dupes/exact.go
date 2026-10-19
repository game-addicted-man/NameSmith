package dupes

import (
	"context"
	"crypto/sha256"
	"io"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// edge is how much of the start and of the end of a file the quick check
// reads before reading whole files.
const edge = 16 << 10

// findExact returns sets of files with identical content. Files are first
// grouped by size, then by a hash of their first and last 16 KB, and only
// files that still match are read completely.
func findExact(ctx context.Context, files []*File, workers int, sk *Skipped, progress func(done, total int)) ([][]*File, error) {
	bySize := map[int64][]*File{}
	for _, f := range files {
		bySize[f.Size] = append(bySize[f.Size], f)
	}
	var quick []*File
	for _, fs := range bySize {
		if len(fs) > 1 {
			quick = append(quick, fs...)
		}
	}
	var done atomic.Int64
	total := int64(len(quick))
	report := func() { progress(int(done.Load()), int(total)) }
	report()

	// step 1: start and end of each file
	quickSums := make(map[*File][32]byte, len(quick))
	var mu sync.Mutex
	var unreadable atomic.Int64
	err := parallel(ctx, workers, quick, func(f *File) {
		s, err := hashEdges(f.Path, f.Size)
		done.Add(1)
		if err != nil {
			unreadable.Add(1)
			return
		}
		mu.Lock()
		quickSums[f] = s
		mu.Unlock()
	}, report)
	if err != nil {
		return nil, err
	}

	type key struct {
		size int64
		sum  [32]byte
	}
	byQuick := map[key][]*File{}
	for _, f := range quick {
		if s, ok := quickSums[f]; ok {
			byQuick[key{f.Size, s}] = append(byQuick[key{f.Size, s}], f)
		}
	}

	// step 2: whole files, only where the quick check could not decide
	var full []*File
	for k, fs := range byQuick {
		if len(fs) < 2 {
			continue
		}
		if k.size <= 2*edge { // the quick check already read everything
			for _, f := range fs {
				f.sum, f.hasSum = k.sum, true
			}
			continue
		}
		full = append(full, fs...)
	}
	total += int64(len(full))
	report()
	err = parallel(ctx, workers, full, func(f *File) {
		s, err := hashFile(ctx, f.Path)
		done.Add(1)
		if err != nil {
			unreadable.Add(1)
			return
		}
		f.sum, f.hasSum = s, true
	}, report)
	if err != nil {
		return nil, err
	}
	sk.Unreadable += int(unreadable.Load())

	byFull := map[key][]*File{}
	for _, fs := range byQuick {
		if len(fs) < 2 {
			continue
		}
		for _, f := range fs {
			if f.hasSum {
				byFull[key{f.Size, f.sum}] = append(byFull[key{f.Size, f.sum}], f)
			}
		}
	}
	var sets [][]*File
	for _, fs := range byFull {
		fs = dropSameFile(fs)
		if len(fs) > 1 {
			sort.Slice(fs, func(i, j int) bool { return fs[i].Path < fs[j].Path })
			sets = append(sets, fs)
		}
	}
	return sets, nil
}

func hashEdges(path string, size int64) ([32]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return [32]byte{}, err
	}
	defer f.Close()
	h := sha256.New()
	if size <= 2*edge {
		if _, err := io.Copy(h, f); err != nil {
			return [32]byte{}, err
		}
	} else {
		buf := make([]byte, edge)
		if _, err := io.ReadFull(f, buf); err != nil {
			return [32]byte{}, err
		}
		h.Write(buf)
		if _, err := f.ReadAt(buf, size-edge); err != nil && err != io.EOF {
			return [32]byte{}, err
		}
		h.Write(buf)
	}
	var s [32]byte
	copy(s[:], h.Sum(nil))
	return s, nil
}

func hashFile(ctx context.Context, path string) ([32]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return [32]byte{}, err
	}
	defer f.Close()
	h := sha256.New()
	buf := make([]byte, 1<<20)
	for {
		if err := ctx.Err(); err != nil {
			return [32]byte{}, err
		}
		n, err := f.Read(buf)
		h.Write(buf[:n])
		if err == io.EOF {
			break
		}
		if err != nil {
			return [32]byte{}, err
		}
	}
	var s [32]byte
	copy(s[:], h.Sum(nil))
	return s, nil
}

// dropSameFile removes paths that are really the same file (a hard link, or
// the same folder reached two ways), so a file is never "a copy of itself".
func dropSameFile(fs []*File) []*File {
	if len(fs) < 2 {
		return fs
	}
	infos := make([]os.FileInfo, len(fs))
	for i, f := range fs {
		infos[i], _ = os.Stat(f.Path)
	}
	var out []*File
	var kept []os.FileInfo
next:
	for i, f := range fs {
		if infos[i] != nil {
			for _, k := range kept {
				if k != nil && os.SameFile(k, infos[i]) {
					continue next
				}
			}
		}
		out = append(out, f)
		kept = append(kept, infos[i])
	}
	return out
}

// parallel runs fn on every item with n workers. tick is called every so
// often (never at the same time as itself) so the caller can report progress.
func parallel(ctx context.Context, n int, items []*File, fn func(*File), tick func()) error {
	if len(items) == 0 {
		return ctx.Err()
	}
	if n < 1 {
		n = 1
	}
	jobs := make(chan *File)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range jobs {
				fn(f)
			}
		}()
	}
	stop := make(chan struct{})
	tickDone := make(chan struct{})
	go func() {
		defer close(tickDone)
		t := time.NewTicker(150 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				tick()
			case <-stop:
				return
			}
		}
	}()
	var err error
feed:
	for _, it := range items {
		select {
		case <-ctx.Done():
			err = ctx.Err()
			break feed
		case jobs <- it:
		}
	}
	close(jobs)
	wg.Wait()
	close(stop)
	<-tickDone
	tick()
	return err
}
