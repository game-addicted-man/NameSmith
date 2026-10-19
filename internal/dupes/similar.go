package dupes

import (
	"context"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/game-addicted-man/NameSmith/internal/meta"
)

// newLoader is NewLoader; tests replace it.
var newLoader = NewLoader

// minPhotoBytes and minPhotoSide leave out icons and tiny pictures, which
// look alike far too easily.
const (
	minPhotoBytes = 2 << 10
	minPhotoSide  = 48
)

// findSimilar reads the photos, groups the ones that look alike and folds
// in the exact-duplicate sets found before.
func findSimilar(ctx context.Context, files []*File, exact [][]*File, opt Options, sk *Skipped, progress func(Progress)) ([]*Group, error) {
	setOf := map[*File]int{}
	for i, set := range exact {
		for _, f := range set {
			setOf[f] = i
		}
	}
	var photos []*File
	for _, f := range files {
		if !IsPhoto(f.Name) || f.Size < minPhotoBytes {
			continue
		}
		if i, ok := setOf[f]; ok && exact[i][0] != f {
			continue // only one of identical files needs to be looked at
		}
		photos = append(photos, f)
	}

	cache := loadCache(opt.CachePath)
	if err := analyzePhotos(ctx, photos, opt.Workers, cache, sk, func(done, total int) {
		progress(Progress{Phase: PhasePhotos, Done: done, Total: total, Found: len(files)})
	}); err != nil {
		return nil, err
	}
	cache.save()

	var ok []*File
	for _, f := range photos {
		if f.hasHash && f.W >= minPhotoSide && f.H >= minPhotoSide {
			ok = append(ok, f)
		}
	}
	progress(Progress{Phase: PhaseGroup, Done: 0, Total: len(ok), Found: len(files)})
	clusters, err := cluster(ctx, ok, opt.Level, func(done int) {
		progress(Progress{Phase: PhaseGroup, Done: done, Total: len(ok), Found: len(files)})
	})
	if err != nil {
		return nil, err
	}

	used := make([]bool, len(exact))
	var groups []*Group
	for _, c := range clusters {
		if len(c) < 2 {
			continue // alone (identical copies of it stay an exact group)
		}
		g := &Group{Kind: GroupSimilar}
		for _, f := range c {
			if i, inSet := setOf[f]; inSet {
				used[i] = true
				for _, same := range exact[i] {
					// copies share the look of the one that was read
					same.hash, same.hasHash, same.W, same.H = f.hash, f.hasHash, f.W, f.H
					same.Taken, same.Orientation = f.Taken, f.Orientation
					g.Files = append(g.Files, same)
				}
			} else {
				g.Files = append(g.Files, f)
			}
		}
		groups = append(groups, g)
	}
	for i, set := range exact {
		if !used[i] {
			groups = append(groups, newExactGroup(set))
		}
	}
	return groups, nil
}

func newExactGroup(set []*File) *Group {
	return &Group{Kind: GroupExact, Files: append([]*File(nil), set...)}
}

// analyzePhotos fills in the hash and size of each photo, using the cache
// for photos that were read before.
func analyzePhotos(ctx context.Context, photos []*File, workers int, cache *hashCache, sk *Skipped, progress func(done, total int)) error {
	var todo []*File
	for _, f := range photos {
		if e, ok := cache.get(f); ok {
			if e.OK {
				f.hash, f.hasHash, f.W, f.H = e.Hash, true, int(e.W), int(e.H)
				f.Orientation = int(e.Orient)
				if e.Taken != 0 {
					f.Taken = time.Unix(e.Taken, 0)
				}
			} else {
				sk.Pictures++
			}
			continue
		}
		todo = append(todo, f)
	}
	total := len(todo)
	var done, bad atomic.Int64
	progress(0, total)
	if total == 0 {
		return ctx.Err()
	}

	jobs := make(chan *File)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runtime.LockOSThread() // the Windows photo reader is tied to a thread
			defer runtime.UnlockOSThread()
			l := newLoader()
			defer l.Close()
			for f := range jobs {
				readPhoto(l, f)
				e := cacheEntry{Size: f.Size, Mod: f.Modified.UnixNano(), OK: f.hasHash}
				if f.hasHash {
					e.Hash, e.W, e.H, e.Orient = f.hash, int32(f.W), int32(f.H), int8(f.Orientation)
					if !f.Taken.IsZero() {
						e.Taken = f.Taken.Unix()
					}
				} else {
					bad.Add(1)
				}
				cache.put(f, e)
				done.Add(1)
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
				progress(int(done.Load()), total)
			case <-stop:
				return
			}
		}
	}()
	var err error
feed:
	for _, f := range todo {
		select {
		case <-ctx.Done():
			err = ctx.Err()
			break feed
		case jobs <- f:
		}
	}
	close(jobs)
	wg.Wait()
	close(stop)
	<-tickDone
	progress(int(done.Load()), total)
	sk.Pictures += int(bad.Load())
	return err
}

func readPhoto(l Loader, f *File) {
	g, w, h, err := l.Grid(f.Path)
	if err != nil {
		return
	}
	info := meta.Read(f.Path)
	o := info.Orientation
	if decoderRotates(f.Name) {
		o = 1
	}
	g = Orient(g, o)
	f.hash = HashOf(&g)
	f.hasHash = true
	f.W, f.H = OrientedSize(w, h, o)
	f.Orientation = o
	if info.HasTaken {
		f.Taken = info.Taken
	}
}

// cluster groups photos around the best-looking one of each group: photos
// are visited from the largest down, and each joins the closest group
// "leader" that matches at the level, or starts a new group. Every photo in a group
// is therefore close to the photo that will be kept, and a long chain of
// slightly different photos never ends up in one group.
func cluster(ctx context.Context, photos []*File, level Level, progress func(done int)) ([][]*File, error) {
	sorted := append([]*File(nil), photos...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if pa, pb := a.W*a.H, b.W*b.H; pa != pb {
			return pa > pb
		}
		if a.Size != b.Size {
			return a.Size > b.Size
		}
		return a.Path < b.Path
	})
	var leaders []*File
	var members [][]*File
	last := time.Now()
	for i, f := range sorted {
		if i%512 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if time.Since(last) > 150*time.Millisecond {
				last = time.Now()
				progress(i)
			}
		}
		best, bestScore := -1, 1<<30
		for li, l := range leaders {
			fine, coarse := f.hash.Distances(l.hash)
			if !level.Match(fine, coarse) {
				continue
			}
			if sc := score(fine, coarse); sc < bestScore {
				best, bestScore = li, sc
				if sc == 0 {
					break
				}
			}
		}
		if best >= 0 {
			members[best] = append(members[best], f)
			continue
		}
		leaders = append(leaders, f)
		members = append(members, []*File{f})
	}
	progress(len(sorted))
	var out [][]*File
	for _, m := range members {
		out = append(out, m)
	}
	return out, nil
}
