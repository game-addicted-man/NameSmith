package dupes

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// GroupKind says why files are in a group.
type GroupKind int

const (
	GroupExact   GroupKind = iota // same content
	GroupSimilar                  // photos that look alike
)

// Group is a set of duplicates. Files[0] is the one to keep.
type Group struct {
	Kind  GroupKind
	Files []*File
}

// Keep returns the file that is kept.
func (g *Group) Keep() *File { return g.Files[0] }

// Reclaimable is the space freed when everything but the kept file goes.
func (g *Group) Reclaimable() int64 {
	var n int64
	for _, f := range g.Files[1:] {
		n += f.Size
	}
	return n
}

// Relation tells how f relates to the kept file: identical content, or
// how alike the photos look (0-100).
func (g *Group) Relation(f *File) (same bool, similarity int) {
	k := g.Keep()
	if f == k || f.SameContent(k) {
		return true, 100
	}
	if f.hasHash && k.hasHash {
		return false, Similarity(f.hash.Distances(k.hash))
	}
	return g.Kind == GroupExact, 100
}

// KeepRule decides which file of a group is kept.
type KeepRule int

const (
	KeepRecommended KeepRule = iota // 원본으로 보이는 파일 (추천)
	KeepBest                        // 화질·용량이 가장 큰 파일
	KeepOldest                      // 가장 오래된 파일
	KeepNewest                      // 가장 최근 파일
	KeepFirstFolder                 // 먼저 추가한 폴더의 파일
)

// KeepLabels are shown in the "남길 파일" box, in KeepRule order.
var KeepLabels = []string{"원본으로 보이는 파일 (추천)", "화질·용량이 가장 큰 파일", "가장 오래된 파일", "가장 최근 파일", "먼저 추가한 폴더의 파일"}

var (
	copyWords  = regexp.MustCompile(`(?i)복사본|사본|복제본|\bcopy\b|copy of`)
	copyNumber = regexp.MustCompile(`\s?\(\d{1,3}\)$`)
)

// copyPenalty is higher for names that look like copies:
// "보고서 - 복사본", "photo (2)", "Copy of plan".
func copyPenalty(name string) int {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	p := 0
	if copyWords.MatchString(base) {
		p += 2
	}
	if copyNumber.MatchString(base) {
		p++
	}
	return p
}

// Folder names where extra copies usually pile up. Korean words count
// anywhere in a folder name ("사진 백업"); English ones only as whole
// words ("photos_backup", not "template").
var (
	copyFoldersKo = []string{"백업", "복사본", "임시", "다운로드", "카카오톡", "받은 파일", "받은파일"}
	copyFoldersEn = []string{"backup", "backups", "bak", "copy", "copies", "temp", "tmp", "downloads", "download", "kakaotalk", "old"}
)

func hasWord(s, w string) bool {
	if s == w {
		return true
	}
	for _, sep := range []string{" ", "_", "-", "."} {
		if strings.HasPrefix(s, w+sep) || strings.HasSuffix(s, sep+w) || strings.Contains(s, sep+w+sep) {
			return true
		}
	}
	return false
}

// folderPenalty is 1 when the file sits in such a folder (at any depth).
func folderPenalty(dir string) int {
	for _, part := range strings.FieldsFunc(strings.ToLower(dir), func(r rune) bool { return r == '\\' || r == '/' }) {
		for _, w := range copyFoldersKo {
			if strings.Contains(part, w) {
				return 1
			}
		}
		for _, w := range copyFoldersEn {
			if hasWord(part, w) {
				return 1
			}
		}
	}
	return 0
}

// recommendedLess orders files from "most likely the original".
func recommendedLess(kind GroupKind, a, b *File) (less, decided bool) {
	if kind == GroupSimilar {
		if pa, pb := a.W*a.H, b.W*b.H; pa != pb {
			return pa > pb, true
		}
		if a.Size != b.Size {
			return a.Size > b.Size, true
		}
	}
	if ca, cb := copyPenalty(a.Name), copyPenalty(b.Name); ca != cb {
		return ca < cb, true
	}
	if fa, fb := folderPenalty(a.Dir), folderPenalty(b.Dir); fa != fb {
		return fa < fb, true
	}
	if !a.Created.Equal(b.Created) {
		return a.Created.Before(b.Created), true
	}
	if !a.Modified.Equal(b.Modified) {
		return a.Modified.Before(b.Modified), true
	}
	if la, lb := len([]rune(a.Name)), len([]rune(b.Name)); la != lb {
		return la < lb, true
	}
	return a.Path < b.Path, a.Path != b.Path
}

// ChooseKeep puts the file to keep first (by rule), then the others:
// identical copies first, then by how alike they look.
func (g *Group) ChooseKeep(rule KeepRule, nroots int) {
	less := func(a, b *File) bool {
		switch rule {
		case KeepBest:
			if pa, pb := a.W*a.H, b.W*b.H; pa != pb {
				return pa > pb
			}
			if a.Size != b.Size {
				return a.Size > b.Size
			}
		case KeepOldest:
			if !a.Modified.Equal(b.Modified) {
				return a.Modified.Before(b.Modified)
			}
		case KeepNewest:
			if !a.Modified.Equal(b.Modified) {
				return a.Modified.After(b.Modified)
			}
		case KeepFirstFolder:
			if a.Root != b.Root {
				return a.Root < b.Root
			}
		}
		l, _ := recommendedLess(g.Kind, a, b)
		return l
	}
	best := 0
	for i := 1; i < len(g.Files); i++ {
		if less(g.Files[i], g.Files[best]) {
			best = i
		}
	}
	g.Files[0], g.Files[best] = g.Files[best], g.Files[0]
	rest := g.Files[1:]
	sort.SliceStable(rest, func(i, j int) bool {
		si, pi := g.Relation(rest[i])
		sj, pj := g.Relation(rest[j])
		if si != sj {
			return si
		}
		if pi != pj {
			return pi > pj
		}
		return rest[i].Path < rest[j].Path
	})
}

func sortGroups(groups []*Group) {
	sort.SliceStable(groups, func(i, j int) bool {
		ri, rj := groups[i].Reclaimable(), groups[j].Reclaimable()
		if ri != rj {
			return ri > rj
		}
		return groups[i].Keep().Path < groups[j].Keep().Path
	})
}
