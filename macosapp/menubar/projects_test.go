package menubar

import (
	"strings"
	"testing"
)

func boolPtr(v bool) *bool { return &v }

func TestFormatProjectTitleParts(t *testing.T) {
	tests := []struct {
		name     string
		branch   string
		clean    *bool
		errMsg   string
		leading  string
		trailing string
	}{
		{
			name:     "error wins over git state",
			branch:   "main",
			clean:    boolPtr(false),
			errMsg:   "path does not exist",
			leading:  "proj",
			trailing: "⚠ Error",
		},
		{
			name:     "unknown status shows branch only",
			branch:   "main",
			clean:    nil,
			leading:  "proj",
			trailing: "· main",
		},
		{
			name:     "unknown status and branch shows nothing",
			clean:    nil,
			leading:  "proj",
			trailing: "",
		},
		{
			name:     "clean shows filled dot",
			branch:   "master",
			clean:    boolPtr(true),
			leading:  "proj",
			trailing: "● master",
		},
		{
			name:     "dirty shows hollow dot",
			branch:   "feature",
			clean:    boolPtr(false),
			leading:  "proj",
			trailing: "○ feature",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatProjectTitleParts("proj", tc.branch, tc.clean, tc.errMsg)
			if got.Leading != tc.leading || got.Trailing != tc.trailing {
				t.Fatalf("parts = %+v, want {%q %q}", got, tc.leading, tc.trailing)
			}
		})
	}
}

func TestFormatProjectTitleNoTrailingSpacesWhenDecorationEmpty(t *testing.T) {
	got := FormatProjectTitle("proj", "", nil, "")
	if got != "proj" {
		t.Fatalf("title = %q, want %q", got, "proj")
	}
	if strings.HasSuffix(got, " ") {
		t.Fatalf("title = %q, want no trailing space", got)
	}
}

func TestFormatProjectTitleJoinsWithDoubleSpace(t *testing.T) {
	if got := FormatProjectTitle("proj", "main", boolPtr(true), ""); got != "proj  ● main" {
		t.Fatalf("title = %q, want %q", got, "proj  ● main")
	}
	if got := FormatProjectTitle("proj", "main", nil, "boom"); got != "proj  ⚠ Error" {
		t.Fatalf("title = %q, want %q", got, "proj  ⚠ Error")
	}
}

func TestFormatWorktreeTitleParts(t *testing.T) {
	if got := FormatWorktreeTitleParts("wt", nil); got.Trailing != "" {
		t.Fatalf("unknown worktree trailing = %q, want empty", got.Trailing)
	}
	if got := FormatWorktreeTitleParts("wt", boolPtr(true)); got.Trailing != "● Clean" {
		t.Fatalf("clean worktree trailing = %q, want %q", got.Trailing, "● Clean")
	}
	if got := FormatWorktreeTitleParts("wt", boolPtr(false)); got.Trailing != "○ Dirty" {
		t.Fatalf("dirty worktree trailing = %q, want %q", got.Trailing, "○ Dirty")
	}
	if got := FormatWorktreeTitle("wt", nil); got != "wt" {
		t.Fatalf("unknown worktree title = %q, want %q", got, "wt")
	}
	if got := FormatWorktreeTitle("wt", boolPtr(false)); got != "wt  ○ Dirty" {
		t.Fatalf("dirty worktree title = %q, want %q", got, "wt  ○ Dirty")
	}
}

func TestFormatCleanLabel(t *testing.T) {
	if got := FormatCleanLabel(nil); got != "" {
		t.Fatalf("nil clean label = %q, want empty", got)
	}
	if got := FormatCleanLabel(boolPtr(true)); got != "● Clean" {
		t.Fatalf("clean label = %q", got)
	}
	if got := FormatCleanLabel(boolPtr(false)); got != "○ Dirty" {
		t.Fatalf("dirty label = %q", got)
	}
}

func TestFormatCleanPendingLabelUsesUnicodeEllipsis(t *testing.T) {
	got := FormatCleanPendingLabel()
	if got != "Status: loading…" {
		t.Fatalf("label = %q, want %q", got, "Status: loading…")
	}
	if strings.Contains(got, "...") {
		t.Fatalf("label = %q uses three ASCII periods", got)
	}
}

func TestFormatProjectsListStatusLabel(t *testing.T) {
	if got := FormatProjectsListStatusLabel(true, 3, ""); got != "" {
		t.Fatalf("with rows = %q, want empty", got)
	}
	if got := FormatProjectsListStatusLabel(true, 0, ""); got != FormatProjectsLoadingLabel() {
		t.Fatalf("loading = %q", got)
	}
	if got := FormatProjectsListStatusLabel(false, 0, "boom"); got != FormatProjectsLoadFailedLabel() {
		t.Fatalf("error = %q", got)
	}
	if got := FormatProjectsListStatusLabel(false, 0, ""); got != FormatProjectsEmptyLabel() {
		t.Fatalf("empty = %q", got)
	}
}

func TestApplyProjectsRefresh(t *testing.T) {
	s := ApplyProjectsRefreshStart(ProjectsListState{Projects: []string{"a"}})
	if !s.Loading || len(s.Projects) != 1 {
		t.Fatalf("start = %+v, want loading with items kept", s)
	}
	s = ApplyProjectsRefreshSuccess(s, []string{"b", "c"})
	if s.Loading || len(s.Projects) != 2 || s.Error != "" {
		t.Fatalf("success = %+v", s)
	}
	s = ApplyProjectsRefreshFailure(s, "boom")
	if s.Loading || len(s.Projects) != 2 || s.Error != "boom" {
		t.Fatalf("failure = %+v, want items kept", s)
	}
}
