// Package updatecheck holds the deterministic parts of the desktop update
// policy: semantic version comparison and the update-manifest acceptance
// rules used before any download. It is transport-free so it can be tested
// without a network or a packaged build.
package updatecheck

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Compare orders two semantic versions: negative when a is older than b,
// zero when equal, positive when a is newer. Versions follow
// major.minor.patch with optional hyphen-separated prerelease tags
// ("1.2.0-beta.1"); a version with a prerelease is older than the same
// version without one. Missing segments count as zero, and anything that
// is not a number in a segment makes the comparison conservative: the
// candidate is treated as older, so it is never offered.
func Compare(a, b string) int {
	pa, aPre, aErr := parse(a)
	pb, bPre, bErr := parse(b)
	if aErr != nil {
		if bErr != nil {
			return 0
		}
		return -1
	}
	if bErr != nil {
		return 1
	}
	for i := range 3 {
		if pa[i] != pb[i] {
			return sign(pa[i] - pb[i])
		}
	}
	if aPre == "" && bPre == "" {
		return 0
	}
	if aPre == "" {
		return 1
	}
	if bPre == "" {
		return -1
	}
	return comparePrerelease(aPre, bPre)
}

func sign(n int64) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

func parse(v string) ([3]int64, string, error) {
	var nums [3]int64
	v = strings.TrimSpace(v)
	if v == "" {
		return nums, "", errors.New("empty version")
	}
	core := v
	pre := ""
	if i := strings.IndexByte(v, '-'); i >= 0 {
		core, pre = v[:i], v[i+1:]
		if pre == "" {
			return nums, "", fmt.Errorf("version %q has an empty prerelease", v)
		}
	}
	for i, part := range strings.SplitN(core, ".", 3+1) {
		if i == 3 {
			return nums, "", fmt.Errorf("version %q has more than three segments", v)
		}
		if part == "" {
			return nums, "", fmt.Errorf("version %q has an empty segment", v)
		}
		n, err := strconv.ParseInt(part, 10, 64)
		if err != nil || n < 0 {
			return nums, "", fmt.Errorf("version %q segment %q is not a number", v, part)
		}
		nums[i] = n
	}
	return nums, pre, nil
}

// comparePrerelease orders two prerelease tags per semver: dot-separated,
// numeric identifiers compare numerically and come before alphanumeric
// ones; a longer list of equal prefix is greater.
func comparePrerelease(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	for i := range min(len(as), len(bs)) {
		an, aErr := strconv.ParseInt(as[i], 10, 64)
		bn, bErr := strconv.ParseInt(bs[i], 10, 64)
		if aErr == nil && bErr == nil {
			if an != bn {
				return sign(an - bn)
			}
			continue
		}
		if aErr != nil && bErr != nil {
			if c := strings.Compare(as[i], bs[i]); c != 0 {
				return c
			}
			continue
		}
		// Numeric identifiers have lower precedence than alphanumeric.
		if aErr != nil {
			return 1
		}
		return -1
	}
	return sign(int64(len(as) - len(bs)))
}
