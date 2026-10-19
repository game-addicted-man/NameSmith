package engine

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Hangul composition constants (Unicode 3.12, conjoining jamo).
const (
	hSBase  = 0xAC00
	hLBase  = 0x1100
	hVBase  = 0x1161
	hTBase  = 0x11A7
	hLCount = 19
	hVCount = 21
	hTCount = 28
	hNCount = hVCount * hTCount
	hSCount = hLCount * hNCount
)

// ComposeHangul joins Korean letters that were stored as separate jamo
// (NFD, typical for files copied from a Mac: "ㅅㅏㅈㅣㄴ") back into
// syllables ("사진"). Other text is left alone.
func ComposeHangul(s string) string {
	if !hasConjoiningJamo(s) {
		return s
	}
	rs := []rune(s)
	out := make([]rune, 0, len(rs))
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		// L + V (+ T)
		if r >= hLBase && r < hLBase+hLCount && i+1 < len(rs) {
			v := rs[i+1]
			if v >= hVBase && v < hVBase+hVCount {
				syl := hSBase + (r-hLBase)*hNCount + (v-hVBase)*hTCount
				i++
				if i+1 < len(rs) {
					t := rs[i+1]
					if t > hTBase && t < hTBase+hTCount {
						syl += t - hTBase
						i++
					}
				}
				out = append(out, syl)
				continue
			}
		}
		// precomposed LV syllable + T
		if r >= hSBase && r < hSBase+hSCount && (r-hSBase)%hTCount == 0 && i+1 < len(rs) {
			t := rs[i+1]
			if t > hTBase && t < hTBase+hTCount {
				out = append(out, r+(t-hTBase))
				i++
				continue
			}
		}
		out = append(out, r)
	}
	return string(out)
}

func hasConjoiningJamo(s string) bool {
	for _, r := range s {
		if (r >= 0x1100 && r <= 0x11FF) || (r >= 0xA960 && r <= 0xA97F) || (r >= 0xD7B0 && r <= 0xD7FF) {
			return true
		}
	}
	return false
}

// NaturalLess compares names the way Windows Explorer sorts them:
// case-insensitive, with runs of digits compared by their value
// ("사진2" comes before "사진10").
func NaturalLess(a, b string) bool { return NaturalCompare(a, b) < 0 }

// NaturalCompare returns -1, 0 or 1.
func NaturalCompare(a, b string) int {
	ia, ib := 0, 0
	for ia < len(a) && ib < len(b) {
		ca, cb := a[ia], b[ib]
		if isDigit(ca) && isDigit(cb) {
			// compare digit runs numerically
			ja, jb := ia, ib
			for ja < len(a) && isDigit(a[ja]) {
				ja++
			}
			for jb < len(b) && isDigit(b[jb]) {
				jb++
			}
			da := strings.TrimLeft(a[ia:ja], "0")
			db := strings.TrimLeft(b[ib:jb], "0")
			if len(da) != len(db) {
				if len(da) < len(db) {
					return -1
				}
				return 1
			}
			if da != db {
				if da < db {
					return -1
				}
				return 1
			}
			// same value: fewer leading zeros first
			if la, lb := ja-ia, jb-ib; la != lb {
				if la < lb {
					return -1
				}
				return 1
			}
			ia, ib = ja, jb
			continue
		}
		ra, sa := utf8.DecodeRuneInString(a[ia:])
		rb, sb := utf8.DecodeRuneInString(b[ib:])
		la, lb := unicode.ToLower(ra), unicode.ToLower(rb)
		if la != lb {
			// digits sort before letters, punctuation before both (like Explorer)
			ka, kb := runeClass(la), runeClass(lb)
			if ka != kb {
				if ka < kb {
					return -1
				}
				return 1
			}
			if la < lb {
				return -1
			}
			return 1
		}
		ia += sa
		ib += sb
	}
	switch {
	case len(a)-ia < len(b)-ib:
		return -1
	case len(a)-ia > len(b)-ib:
		return 1
	}
	// equal ignoring case: fall back to exact comparison for a stable order
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func runeClass(r rune) int {
	switch {
	case unicode.IsDigit(r):
		return 1
	case unicode.IsLetter(r):
		return 2
	default:
		return 0
	}
}
