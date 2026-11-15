package diskmon

import "time"

// AnalysisSchema versions the category rules. Trends only compare reports that
// share a schema, so adding or renaming a category never fabricates a jump.
const AnalysisSchema = 1

// Category is one bucket of the space breakdown. ID is the stable join key
// used by the trend charts; Label is free to change.
type Category struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Kind        string   `json:"kind"` // cache | media | doc | other
	Bytes       int64    `json:"bytes"`
	Reclaimable int64    `json:"reclaimable"`
	Paths       []string `json:"paths,omitempty"`
}

// Entry is one large directory found during a scan.
type Entry struct {
	Category    string `json:"category"`
	Path        string `json:"path"`
	Bytes       int64  `json:"bytes"`
	Reclaimable bool   `json:"reclaimable"`
}

// Skip kinds. The walk leaves a directory alone for one of four reasons, and
// they must never be mixed: the same folder can both time out and be denied,
// and merging those rows would make the reason lie.
const (
	// SkipPermission means the OS refused the listing (TCC, sandbox, SIP).
	SkipPermission = "permission"
	// SkipTimeout means the listing never returned; the kernel owns that
	// goroutine until the filesystem lets go of it.
	SkipTimeout = "timeout"
	// SkipIO covers every other read error: a vanishing directory, a broken
	// mount, a filesystem returning EIO.
	SkipIO = "io"
	// SkipByDesign is the scan's own policy: mounts that would block or count
	// the same bytes twice, and cloud File Provider roots.
	SkipByDesign = "by_design"
)

// SkippedPath is a folder the scan refused to enter or could not read, with the
// reason it is missing from the totals. Path is shortened the same way entries
// are ("~/Library/Mail"), and Depth is its segment count, which is what the
// grouping step folds by.
type SkippedPath struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"` // permission | timeout | io | by_design
	Reason string `json:"reason,omitempty"`
	Depth  int    `json:"depth,omitempty"`
}

// SkipGroup is one row of the "not counted" list: several unreadable folders
// that share a prefix and a reason, folded into one line. Reports store the
// grouped rows because a full-disk walk can produce hundreds of them, and a row
// per directory would say nothing a human can act on.
type SkipGroup struct {
	Prefix  string `json:"prefix"`
	Kind    string `json:"kind"`
	Count   int    `json:"count"`
	Reason  string `json:"reason,omitempty"`
	Example string `json:"example,omitempty"`
}

// SkipOther is the remainder that did not fit in the report's group cap. It is
// always consistent with NotCountedTotal: the sum of every group plus these
// directories is the number of folders missing from the totals.
type SkipOther struct {
	Groups int `json:"groups"`
	Dirs   int `json:"dirs"`
}

// A core folder's fate in one report, as the "stopped early" list shows it.
const (
	RootDone       = "done"
	RootPartial    = "partial"
	RootNotStarted = "not_started"
)

// PriorityRootState says whether one core folder was read to the end. The count
// in PriorityScanned answers "how many", this answers "which": without it the
// sheet can only say that something is missing, not what.
type PriorityRootState struct {
	Root  string `json:"root"`
	State string `json:"state"`
}

// ScanCoverage explains how far a report actually got. Truncated plus a complete
// priority set is the normal, useful outcome: the folders the page leads with
// are exact, the remainder is whatever the budget allowed.
//
// Priority completion and unreadable folders are reported separately on purpose:
// "the walk ran out of time in the folders the page leads with" is a different
// statement from "some folders could not be read", and only the first one makes
// the headline numbers a lower bound.
type ScanCoverage struct {
	PriorityRoots    []string `json:"priority_roots,omitempty"`
	PriorityScanned  int      `json:"priority_scanned"`
	PriorityComplete bool     `json:"priority_complete"`
	// PriorityStates is PriorityRoots in the same order, with each root's fate.
	PriorityStates []PriorityRootState `json:"priority_states,omitempty"`
	BudgetSeconds  int64               `json:"budget_s,omitempty"`
	ElapsedSeconds float64             `json:"elapsed_s,omitempty"`
	Truncated      bool                `json:"truncated,omitempty"`
	// NotCountedTotal counts every folder that is missing from the totals, of
	// every kind, before grouping. It is never truncated.
	NotCountedTotal int           `json:"not_counted_total"`
	SkipGroups      []SkipGroup   `json:"skip_groups,omitempty"`
	SkipOther       *SkipOther    `json:"skip_other,omitempty"`
	Skipped         []SkippedPath `json:"skipped,omitempty"`
}

// Report is one completed scan. Reports are append-only; older ones lose their
// per-directory entries but keep the category rollup that trends need.
type Report struct {
	ID           string    `json:"id"`
	Schema       int       `json:"schema"`
	ScannedAt    time.Time `json:"scanned_at"`
	ScanMillis   int64     `json:"scan_ms"`
	Complete     bool      `json:"complete"`
	Truncated    bool      `json:"truncated"`
	UsedBytes    int64     `json:"used_bytes"`
	TotalBytes   int64     `json:"total_bytes"`
	ScannedBytes int64     `json:"scanned_bytes"`
	Files        int64     `json:"files"`
	NoAccessDirs int       `json:"no_access_dirs"`
	// NotCountedTotal mirrors Coverage.NotCountedTotal so callers can show the
	// number without digging into the coverage object. NoAccessDirs stays for
	// older reports and the CLI: it only ever counted permission errors.
	NotCountedTotal int           `json:"not_counted_total,omitempty"`
	Roots           []string      `json:"roots,omitempty"`
	Categories      []Category    `json:"categories"`
	Entries         []Entry       `json:"entries,omitempty"`
	EntriesDropped  bool          `json:"entries_dropped,omitempty"`
	Coverage        *ScanCoverage `json:"coverage,omitempty"`
}

// ReportSummary is the list/trend shape: everything except the directory rows.
type ReportSummary struct {
	ID              string        `json:"id"`
	Schema          int           `json:"schema"`
	ScannedAt       time.Time     `json:"scanned_at"`
	ScanMillis      int64         `json:"scan_ms"`
	Complete        bool          `json:"complete"`
	Truncated       bool          `json:"truncated"`
	UsedBytes       int64         `json:"used_bytes"`
	TotalBytes      int64         `json:"total_bytes"`
	ScannedBytes    int64         `json:"scanned_bytes"`
	Files           int64         `json:"files"`
	NoAccessDirs    int           `json:"no_access_dirs"`
	NotCountedTotal int           `json:"not_counted_total,omitempty"`
	Reclaimable     int64         `json:"reclaimable"`
	HasEntries      bool          `json:"has_entries"`
	Categories      []Category    `json:"categories"`
	EntriesDropped  bool          `json:"entries_dropped,omitempty"`
	Coverage        *ScanCoverage `json:"coverage,omitempty"`
}

// JobProgress is what the running scan publishes. Percent is a heuristic: the
// scan cannot know the denominator before it finishes.
type JobProgress struct {
	ID           string    `json:"id"`
	StartedAt    time.Time `json:"started_at"`
	Phase        string    `json:"phase"`
	WalkedBytes  int64     `json:"walked_bytes"`
	Files        int64     `json:"files"`
	NoAccessDirs int       `json:"no_access_dirs"`
	Percent      int       `json:"percent"`
	State        string    `json:"state"` // running | cancelled | failed
}

// Attempt records how the newest try ended, independent of whether it produced
// a report. An interrupted try must never look like a completed scan.
type Attempt struct {
	At      time.Time `json:"at"`
	Outcome string    `json:"outcome"` // completed | cancelled | failed | interrupted
	JobID   string    `json:"job_id,omitempty"`
	Error   string    `json:"error,omitempty"`
}

// AnalysisStatus is the single source of truth the page polls.
type AnalysisStatus struct {
	Running         bool           `json:"running"`
	Job             *JobProgress   `json:"job,omitempty"`
	LastCompletedAt *time.Time     `json:"last_completed_at,omitempty"`
	LastAttempt     *Attempt       `json:"last_attempt,omitempty"`
	ReportCount     int            `json:"report_count"`
	Latest          *ReportSummary `json:"latest,omitempty"`
}

func (r Report) summary() ReportSummary {
	return ReportSummary{
		ID:              r.ID,
		Schema:          r.Schema,
		ScannedAt:       r.ScannedAt,
		ScanMillis:      r.ScanMillis,
		Complete:        r.Complete,
		Truncated:       r.Truncated,
		UsedBytes:       r.UsedBytes,
		TotalBytes:      r.TotalBytes,
		ScannedBytes:    r.ScannedBytes,
		Files:           r.Files,
		NoAccessDirs:    r.NoAccessDirs,
		NotCountedTotal: r.NotCountedTotal,
		Reclaimable:     r.reclaimableTotal(),
		HasEntries:      len(r.Entries) > 0,
		Categories:      r.Categories,
		EntriesDropped:  r.EntriesDropped,
		Coverage:        r.Coverage,
	}
}

func (r Report) reclaimableTotal() int64 {
	var sum int64
	for _, c := range r.Categories {
		sum += c.Reclaimable
	}
	return sum
}

// progressPercent keeps the bar honest: it never claims 100% before the report
// exists, and it is explicitly an estimate while walking.
func progressPercent(walked, used int64) int {
	if used <= 0 || walked <= 0 {
		return 0
	}
	budget := used * 9 / 10
	if budget <= 0 {
		return 0
	}
	pct := int(walked * 100 / budget)
	if pct > 99 {
		return 99
	}
	if pct < 0 {
		return 0
	}
	return pct
}
