package diskmon

import (
	"fmt"
	"sort"
	"testing"
)

// skipSum is the invariant every report must satisfy: the rows the page shows
// plus the folded remainder are exactly the folders missing from the totals.
func skipSum(groups []SkipGroup, other *SkipOther) int {
	sum := 0
	for _, group := range groups {
		sum += group.Count
	}
	if other != nil {
		sum += other.Dirs
	}
	return sum
}

// assertSkipInvariants holds the three properties a grouped list must keep: the
// counts add up, no two rows are the same row, and no row is contained in
// another of its kind.
func assertSkipInvariants(t *testing.T, groups []SkipGroup, other *SkipOther, want int) {
	t.Helper()
	if got := skipSum(groups, other); got != want {
		t.Errorf("groups + other = %d, want %d (the uncapped skip count)", got, want)
	}
	seen := map[string]bool{}
	for _, group := range groups {
		key := group.Kind + " " + group.Prefix
		if seen[key] {
			t.Errorf("duplicate group %q (%q)", group.Prefix, group.Kind)
		}
		seen[key] = true
	}
	for _, a := range groups {
		for _, b := range groups {
			if a.Kind != b.Kind || a.Prefix == b.Prefix {
				continue
			}
			if isPathPrefix(a.Prefix, b.Prefix) {
				t.Errorf("group %q (%q) contains %q; the shorter prefix must absorb the longer",
					a.Prefix, a.Kind, b.Prefix)
			}
		}
	}
	if len(groups) > maxSkipGroups {
		t.Errorf("%d groups exceed the cap of %d", len(groups), maxSkipGroups)
	}
}

// TestSkipGroupsFoldsUnderHome is the live case: hundreds of sandbox containers
// that all time out must read as one row, not 283.
func TestSkipGroupsFoldsUnderHome(t *testing.T) {
	var skipped []SkippedPath
	add := func(n int, path, kind, reason string) {
		for i := 0; i < n; i++ {
			skipped = append(skipped, SkippedPath{
				Path:   fmt.Sprintf("%s/%02d", path, i),
				Kind:   kind,
				Reason: reason,
			})
		}
	}
	add(283, "~/Library/Containers", SkipTimeout, "operation timed out after 1s")
	add(96, "~/Library/Group Containers", SkipTimeout, "operation timed out after 1s")
	add(6, "~/Library/Application Support", SkipPermission, "operation not permitted")
	add(1, "~/Library/Saved Application State", SkipPermission, "operation not permitted")
	add(4, "/private", SkipTimeout, "operation timed out after 1s")
	add(2, "/home", SkipByDesign, "autofs mount")

	groups, other := groupSkips(skipped)
	assertSkipInvariants(t, groups, other, len(skipped))

	byPrefix := map[string]SkipGroup{}
	for _, group := range groups {
		byPrefix[group.Prefix] = group
	}
	containers, ok := byPrefix["~/Library/Containers"]
	if !ok {
		t.Fatalf("no row for ~/Library/Containers; got %+v", groups)
	}
	if containers.Count != 283 || containers.Kind != SkipTimeout {
		t.Errorf("~/Library/Containers = %+v, want 283 timeout folders", containers)
	}
	if containers.Reason != "operation timed out after 1s" {
		t.Errorf("reason = %q, want the timeout text", containers.Reason)
	}
	if containers.Example == "" {
		t.Error("a folded row must carry one concrete example path")
	}
}

// TestSkipGroupsKeepsSingleDirectoryWhole: a group of one names the directory,
// because that is the folder the user has to open.
func TestSkipGroupsKeepsSingleDirectoryWhole(t *testing.T) {
	groups, other := groupSkips([]SkippedPath{
		{Path: "~/Library/Mail/V10", Kind: SkipPermission, Reason: "operation not permitted"},
	})
	assertSkipInvariants(t, groups, other, 1)
	if len(groups) != 1 || groups[0].Prefix != "~/Library/Mail/V10" {
		t.Fatalf("groups = %+v, want the full path of the single folder", groups)
	}
	if groups[0].Example != "" {
		t.Errorf("a single row needs no example, got %q", groups[0].Example)
	}
}

// TestSkipGroupsSeparatesKinds: the same folder can be both denied and timing
// out, and the report must not merge two different reasons into one row.
func TestSkipGroupsSeparatesKinds(t *testing.T) {
	groups, other := groupSkips([]SkippedPath{
		{Path: "~/Library/Containers/a", Kind: SkipTimeout},
		{Path: "~/Library/Containers/b", Kind: SkipPermission},
		{Path: "~/Library/Containers/c", Kind: SkipTimeout},
	})
	assertSkipInvariants(t, groups, other, 3)
	kinds := map[string]int{}
	for _, group := range groups {
		kinds[group.Kind] += group.Count
	}
	if kinds[SkipTimeout] != 2 || kinds[SkipPermission] != 1 {
		t.Errorf("kinds = %v, want 2 timeout + 1 permission", kinds)
	}
}

// TestSkipGroupsSortsActionableFirst pins the display order: what the user can
// fix before what the product decided.
func TestSkipGroupsSortsActionableFirst(t *testing.T) {
	groups, other := groupSkips([]SkippedPath{
		{Path: "/home/x", Kind: SkipByDesign, Reason: "autofs mount"},
		{Path: "/Users/me/Library/Caches/a", Kind: SkipByDesign},
		{Path: "/Users/me/Library/Caches/b", Kind: SkipTimeout},
		{Path: "/Users/me/Library/Caches/c", Kind: SkipPermission},
		{Path: "/Users/me/Library/Caches/d", Kind: SkipIO},
	})
	assertSkipInvariants(t, groups, other, 5)
	// Two absolute prefixes fold to /home and /Users/me, so by_design may appear
	// twice; what matters is that it never appears before a fixable kind.
	last := -1
	for _, group := range groups {
		rank := skipKindRank(group.Kind)
		if rank < last {
			t.Errorf("kind order jumps backwards at %q (%q)", group.Prefix, group.Kind)
		}
		last = rank
	}
	if skipKindRank(groups[len(groups)-1].Kind) != skipKindOrder[SkipByDesign] {
		t.Errorf("last row = %+v, want a by_design row", groups[len(groups)-1])
	}
}

// TestSkipGroupsCapsAndFolds: past the cap the rows stop, but the count does
// not: skip_other keeps the total exact, so the page never under-reports.
func TestSkipGroupsCapsAndFolds(t *testing.T) {
	var skipped []SkippedPath
	for i := 0; i < maxSkipGroups+4; i++ {
		for j := 0; j <= i; j++ {
			skipped = append(skipped, SkippedPath{
				Path:   fmt.Sprintf("/opt/tool%d/dir%d", i, j),
				Kind:   SkipPermission,
				Reason: "operation not permitted",
			})
		}
	}
	groups, other := groupSkips(skipped)
	assertSkipInvariants(t, groups, other, len(skipped))
	if len(groups) != maxSkipGroups {
		t.Fatalf("%d groups, want the cap of %d", len(groups), maxSkipGroups)
	}
	if other == nil || other.Groups == 0 || other.Dirs == 0 {
		t.Fatalf("other = %+v, want the folded remainder", other)
	}
	if skipSum(groups, nil)+other.Dirs != len(skipped) {
		t.Errorf("the remainder lost directories: %d + %d != %d",
			skipSum(groups, nil), other.Dirs, len(skipped))
	}
	// The biggest groups survive the cap, because those are the ones the user
	// would act on.
	if groups[0].Count != len(skipped)-0 && groups[0].Count < maxSkipGroups+3 {
		t.Errorf("largest group = %+v, want the darkest group kept", groups[0])
	}
}

// TestSkipGroupPrefix covers the two rules and the paths that are shorter than
// them: a prefix must never be widened past the directory it describes.
func TestSkipGroupPrefix(t *testing.T) {
	cases := map[string]string{
		"~/Library/Containers/cloud.centaur.goj/Data": "~/Library/Containers",
		"~/Library/Mail":            "~/Library/Mail",
		"~/Downloads":               "~/Downloads",
		"~":                         "~",
		"/private/var/folders/ab/T": "/private/var",
		"/home":                     "/home",
		"/Volumes/External/Backups": "/Volumes/External",
	}
	for path, want := range cases {
		if got := skipGroupPrefix(path); got != want {
			t.Errorf("skipGroupPrefix(%q) = %q, want %q", path, got, want)
		}
	}
}

// TestSkipGroupsMergesReasons: a folded row keeps the reason that explains most
// of its directories, not whichever arrived last.
func TestSkipGroupsMergesReasons(t *testing.T) {
	groups, _ := groupSkips([]SkippedPath{
		{Path: "~/Library/Containers/a", Kind: SkipTimeout, Reason: "operation timed out after 1s"},
		{Path: "~/Library/Containers/b", Kind: SkipTimeout, Reason: "operation timed out after 1s"},
		{Path: "~/Library/Containers/c", Kind: SkipTimeout, Reason: "input/output error"},
	})
	if len(groups) != 1 {
		t.Fatalf("groups = %+v, want one folded row", groups)
	}
	if groups[0].Reason != "operation timed out after 1s" {
		t.Errorf("reason = %q, want the dominant one", groups[0].Reason)
	}
}

// TestSkipGroupsEmpty proves an empty walk produces no rows at all, so the page
// can hide the section instead of rendering "0 folders".
func TestSkipGroupsEmpty(t *testing.T) {
	groups, other := groupSkips(nil)
	if len(groups) != 0 || other != nil {
		t.Fatalf("groups = %+v, other = %+v, want nothing", groups, other)
	}
}

// TestSkipGroupsIsDeterministic guards against map iteration leaking into the
// report: two runs over the same skips must serialize identically, or every
// report would diff against the last one for no reason.
func TestSkipGroupsIsDeterministic(t *testing.T) {
	var skipped []SkippedPath
	for i := 0; i < 40; i++ {
		skipped = append(skipped, SkippedPath{
			Path:   fmt.Sprintf("/Users/me/Library/Caches/app%d", i%7),
			Kind:   []string{SkipPermission, SkipTimeout, SkipIO, SkipByDesign}[i%4],
			Reason: "r",
		})
	}
	first, firstOther := groupSkips(skipped)
	for run := 0; run < 5; run++ {
		next, nextOther := groupSkips(skipped)
		if fmt.Sprint(first) != fmt.Sprint(next) || fmt.Sprint(firstOther) != fmt.Sprint(nextOther) {
			t.Fatalf("run %d differs from the first", run)
		}
	}
	if !sort.SliceIsSorted(first, func(i, j int) bool {
		if first[i].Kind != first[j].Kind {
			return skipKindRank(first[i].Kind) < skipKindRank(first[j].Kind)
		}
		return first[i].Count >= first[j].Count
	}) {
		t.Errorf("groups are not sorted actionable-first then by size: %+v", first)
	}
}
