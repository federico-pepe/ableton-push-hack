// Package semver parses and orders Push Hack release numbers.
//
// Form: MAJOR.MINOR.PATCH, then optionally -PRERELEASE, with an optional
// leading "v" (the git tag form). Order follows semver 2.0: a pre-release is
// lower than its release, and "alpha" < "beta" < "rc" by plain text order.
// Build metadata after "+" is dropped.
package semver

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a parsed release number.
type Version struct {
	major, minor, patch uint64
	pre                 []string
}

// Parse reads a release number such as "0.1.10-alpha" or "v0.1.10-rc.1".
func Parse(s string) (Version, error) {
	orig := s
	s = strings.TrimPrefix(s, "v")
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	core, pre, hasPre := strings.Cut(s, "-")
	nums := strings.Split(core, ".")
	if len(nums) != 3 {
		return Version{}, fmt.Errorf("semver: %q: want MAJOR.MINOR.PATCH", orig)
	}
	var v Version
	for i, p := range []*uint64{&v.major, &v.minor, &v.patch} {
		n, err := parseNum(nums[i])
		if err != nil {
			return Version{}, fmt.Errorf("semver: %q: %v", orig, err)
		}
		*p = n
	}
	if hasPre {
		v.pre = strings.Split(pre, ".")
		for _, id := range v.pre {
			if err := checkIdent(id); err != nil {
				return Version{}, fmt.Errorf("semver: %q: %v", orig, err)
			}
		}
	}
	return v, nil
}

// parseNum reads a number with no sign and no leading zero.
func parseNum(s string) (uint64, error) {
	if s == "" || (len(s) > 1 && s[0] == '0') {
		return 0, fmt.Errorf("bad number %q", s)
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("bad number %q", s)
		}
	}
	return strconv.ParseUint(s, 10, 64)
}

// checkIdent accepts [0-9A-Za-z-]+, and a number may not start with 0.
func checkIdent(s string) error {
	if s == "" {
		return fmt.Errorf("empty pre-release part")
	}
	numeric := true
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '-':
			numeric = false
		default:
			return fmt.Errorf("bad character %q in pre-release", c)
		}
	}
	if numeric && len(s) > 1 && s[0] == '0' {
		return fmt.Errorf("leading zero in pre-release number %q", s)
	}
	return nil
}

// Prerelease reports whether v carries a pre-release tag such as -alpha.
func (v Version) Prerelease() bool { return len(v.pre) > 0 }

// String returns the number without a leading "v" or build metadata.
func (v Version) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
	if len(v.pre) > 0 {
		s += "-" + strings.Join(v.pre, ".")
	}
	return s
}

// Compare returns -1, 0 or +1 as v is lower than, equal to or higher than w.
func (v Version) Compare(w Version) int {
	for _, p := range [][2]uint64{{v.major, w.major}, {v.minor, w.minor}, {v.patch, w.patch}} {
		if c := cmpUint(p[0], p[1]); c != 0 {
			return c
		}
	}
	switch {
	case len(v.pre) == 0 && len(w.pre) == 0:
		return 0
	case len(v.pre) == 0:
		return 1 // release > pre-release
	case len(w.pre) == 0:
		return -1
	}
	for i := 0; i < len(v.pre) && i < len(w.pre); i++ {
		if c := cmpIdent(v.pre[i], w.pre[i]); c != 0 {
			return c
		}
	}
	return cmpUint(uint64(len(v.pre)), uint64(len(w.pre)))
}

func cmpUint(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// cmpIdent orders two pre-release parts: numbers by value, and a number is
// lower than a word. Words go by plain text order.
func cmpIdent(a, b string) int {
	an, aerr := strconv.ParseUint(a, 10, 64)
	bn, berr := strconv.ParseUint(b, 10, 64)
	switch {
	case aerr == nil && berr == nil:
		return cmpUint(an, bn)
	case aerr == nil:
		return -1
	case berr == nil:
		return 1
	}
	return strings.Compare(a, b)
}
