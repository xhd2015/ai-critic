package diskmon

import (
	"sort"
	"strings"
)

// skipPrefixSegments is how much of a path a group keeps. Home paths keep three
// segments ("~/Library/Mail"), everything else keeps two ("/private/var"). Two
// levels of an absolute path are almost always the mount or the system folder
// the user would act on, and three levels under home are what one TCC prompt or
// one File Provider extension covers, so those are the rows that can be fixed.
const (
	homePrefixSegments = 3
	rootPrefixSegments = 2
)

// maxSkipGroups caps the rows a report stores. Real volumes produce five or six;
// the cap exists so a pathological walk cannot grow the report without bound,
// and everything past it is folded into SkipOther rather than dropped.
const maxSkipGroups = 8

// maxRawSkips caps the raw detail kept beside the grouped rows. The groups carry
// the meaning; the raw list is there for debugging and for CLI consumers that
// still read it, so a few hundred paths is enough.
const maxRawSkips = 200

// skipKindOrder is the display order: what the user can fix first, what the
// product decided last.
var skipKindOrder = map[string]int{
	SkipPermission: 0,
	SkipIO:         1,
	SkipTimeout:    2,
	SkipByDesign:   3,
}

// groupSkips folds raw skipped paths into the rows a report stores. The rules,
// in order: bucket by (kind, prefix); fold a group into a shorter group of the
// same kind; a group of one keeps its full path, because the exact directory is
// what the user needs; sort actionable-first then by size; keep at most
// maxSkipGroups rows and fold the rest into SkipOther.
//
// It returns the rows and the remainder, and never mutates its input.
func groupSkips(skipped []SkippedPath) ([]SkipGroup, *SkipOther) {
	if len(skipped) == 0 {
		return nil, nil
	}

	type bucket struct {
		kind     string
		prefix   string
		count    int
		reasons  map[string]int
		example  string
		absorbed bool
	}

	buckets := make(map[string]*bucket)
	order := make([]string, 0, len(skipped))
	for _, skip := range skipped {
		prefix := skipGroupPrefix(skip.Path)
		key := skip.Kind + "\x00" + prefix
		b, ok := buckets[key]
		if !ok {
			b = &bucket{kind: skip.Kind, prefix: prefix, reasons: map[string]int{}}
			buckets[key] = b
			order = append(order, key)
		}
		b.count++
		if skip.Reason != "" {
			b.reasons[skip.Reason]++
		}
		if b.example == "" || skip.Path < b.example {
			b.example = skip.Path
		}
	}

	// Walk shortest prefixes first so a parent absorbs its children once.
	sort.Slice(order, func(i, j int) bool {
		a, b := buckets[order[i]], buckets[order[j]]
		if a.kind != b.kind {
			return skipKindRank(a.kind) < skipKindRank(b.kind)
		}
		if da, db := segments(a.prefix), segments(b.prefix); da != db {
			return da < db
		}
		return a.prefix < b.prefix
	})
	for i, key := range order {
		a := buckets[key]
		if a.absorbed {
			continue
		}
		for _, other := range order[i+1:] {
			b := buckets[other]
			if b.absorbed || b.kind != a.kind {
				continue
			}
			if !isPathPrefix(a.prefix, b.prefix) {
				continue
			}
			a.count += b.count
			for reason, n := range b.reasons {
				a.reasons[reason] += n
			}
			if b.example < a.example {
				a.example = b.example
			}
			b.absorbed = true
		}
	}

	groups := make([]SkipGroup, 0, len(order))
	for _, key := range order {
		b := buckets[key]
		if b.absorbed {
			continue
		}
		group := SkipGroup{
			Prefix: b.prefix,
			Kind:   b.kind,
			Count:  b.count,
			Reason: commonReason(b.reasons),
		}
		// A single directory is worth naming in full: "~/Library/Mail/V10" tells
		// the user which mailbox to open, "~/Library/Mail" does not.
		if b.count == 1 {
			group.Prefix = b.example
		} else {
			group.Example = b.example
		}
		groups = append(groups, group)
	}

	sort.SliceStable(groups, func(i, j int) bool {
		if groups[i].Kind != groups[j].Kind {
			return skipKindRank(groups[i].Kind) < skipKindRank(groups[j].Kind)
		}
		if groups[i].Count != groups[j].Count {
			return groups[i].Count > groups[j].Count
		}
		return groups[i].Prefix < groups[j].Prefix
	})

	if len(groups) <= maxSkipGroups {
		return groups, nil
	}
	other := &SkipOther{}
	for _, group := range groups[maxSkipGroups:] {
		other.Groups++
		other.Dirs += group.Count
	}
	return groups[:maxSkipGroups], other
}

// skipGroupPrefix returns the part of a path a group keeps. A path with fewer
// segments than the rule keeps stays whole rather than being widened to a
// parent it does not belong to.
func skipGroupPrefix(path string) string {
	path = strings.TrimSuffix(path, "/")
	if path == "" {
		return path
	}
	home := path == "~" || strings.HasPrefix(path, "~/")
	rest := strings.Trim(strings.TrimPrefix(path, "~"), "/")
	if rest == "" {
		return "~"
	}
	parts := strings.Split(rest, "/")
	// "~" is one of the kept segments, so home paths keep one fewer of the rest.
	keep, base := rootPrefixSegments, "/"
	if home {
		keep, base = homePrefixSegments-1, "~/"
	}
	if len(parts) <= keep {
		return base + strings.Join(parts, "/")
	}
	return base + strings.Join(parts[:keep], "/")
}

// segments counts the segments of a shortened path, with the home marker
// counting as one: "~" is 1, "~/Library/Mail" is 3, "/private/var" is 2. It is
// only used to order prefixes shortest-first, so the absolute values do not
// matter, only that a parent always measures shorter than its children.
func segments(path string) int {
	path = strings.Trim(path, "/")
	if path == "" || path == "~" {
		return 0
	}
	return len(strings.Split(path, "/"))
}

// isPathPrefix reports whether child sits inside parent. It compares on segment
// boundaries so "/private/var" does not swallow "/private/variable".
func isPathPrefix(parent, child string) bool {
	parent = strings.TrimSuffix(parent, "/")
	if parent == child || !strings.HasPrefix(child, parent+"/") {
		return false
	}
	if parent == "~" || parent == "" {
		return false
	}
	return true
}

// skipKindRank orders kinds for display; an unknown kind sorts with by_design so
// a future kind can never jump ahead of something the user can fix.
func skipKindRank(kind string) int {
	if rank, ok := skipKindOrder[kind]; ok {
		return rank
	}
	return skipKindOrder[SkipByDesign]
}

// commonReason picks the reason that explains most of a group's directories. A
// group is built from one kind, but errno text can differ within it.
func commonReason(reasons map[string]int) string {
	best, bestN := "", 0
	for reason, n := range reasons {
		if n > bestN || (n == bestN && reason < best) {
			best, bestN = reason, n
		}
	}
	return best
}
