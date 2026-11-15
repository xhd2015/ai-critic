package diskmon

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// dirTask is one directory handed to a worker.
type dirTask struct {
	node *scanNode
	// priority is inherited by every child, so an important subtree is finished
	// before the rest of the volume is even listed.
	priority bool
}

// scanQueue hands directories to workers, keeping priority work ahead of the
// rest. Workers block while the queue is empty but directories are still in
// flight, and exit as soon as nothing is left in either deque.
type scanQueue struct {
	mu       sync.Mutex
	cond     *sync.Cond
	high     []*dirTask
	low      []*dirTask
	inflight int
	started  bool
	stopped  bool
}

func newScanQueue() *scanQueue {
	q := &scanQueue{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// release lets blocked workers start popping. Seeding happens after the workers
// exist but before they run, so no goroutine can observe an empty queue and
// quit while the roots are still being queued.
func (q *scanQueue) release() {
	q.mu.Lock()
	q.started = true
	q.mu.Unlock()
	q.cond.Broadcast()
}

func (q *scanQueue) push(task *dirTask) {
	q.mu.Lock()
	if !q.stopped {
		if task.priority {
			q.high = append(q.high, task)
		} else {
			q.low = append(q.low, task)
		}
	}
	q.mu.Unlock()
	q.cond.Broadcast()
}

func (q *scanQueue) pop() (*dirTask, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for {
		if q.stopped {
			return nil, false
		}
		if n := len(q.high); n > 0 {
			task := q.high[n-1]
			q.high = q.high[:n-1]
			q.inflight++
			return task, true
		}
		if n := len(q.low); n > 0 {
			task := q.low[n-1]
			q.low = q.low[:n-1]
			q.inflight++
			return task, true
		}
		// Nothing queued: either the walk is over, or another worker is still
		// reading a directory that may push more.
		if q.started && q.inflight == 0 {
			return nil, false
		}
		q.cond.Wait()
	}
}

func (q *scanQueue) done() {
	q.mu.Lock()
	q.inflight--
	q.mu.Unlock()
	q.cond.Broadcast()
}

func (q *scanQueue) stop() {
	q.mu.Lock()
	q.stopped = true
	q.mu.Unlock()
	q.cond.Broadcast()
}

// runtimeWorkers is how many directories may be listed at once. The walk is
// dominated by syscalls rather than CPU, but the kernel serialises directory
// reads on a few locks, so more than eight workers stopped paying off.
func runtimeWorkers() int {
	return runtime.GOMAXPROCS(0)
}

// physicalPath maps a path in the classifier's space back onto the mount being
// walked, so "/Users/me" becomes "/System/Volumes/Data/Users/me".
func physicalPath(logical, volume string) string {
	if volume == "" {
		return logical
	}
	return filepath.Join(volume, logical)
}

// dirStallTimeout is how long a directory may make no progress before its scan
// is abandoned. A File Provider container can block a single lstat for minutes,
// and the syscall cannot be interrupted, so the worker drops the directory
// instead of holding the whole scan hostage. It is reported as not counted.
const (
	dirStallTimeout = 500 * time.Millisecond
	dirStallPoll    = 50 * time.Millisecond
)

// dirReadTimeout bounds one directory listing. Even a sixty-thousand-entry
// directory lists well inside this; the File Provider and sandbox container
// directories never return at all, and a goroutine stuck in the kernel cannot
// be cancelled, so the scan walks away from it and reports the directory as not
// counted.
const dirReadTimeout = 1 * time.Second

// scanDirWithin lists one directory and then scans its entries, giving up if
// either phase stops making progress. The abandoned goroutine keeps a descriptor
// until the kernel releases it, which is a handful per scan, and its late result
// is ignored.
func (a *scanAccumulator) scanDirWithin(task *dirTask) {
	// A wedged tree is dropped as a whole: its remaining tasks are still
	// queued, so they bail out here before touching the filesystem.
	if a.insideDropped(task.node) {
		a.settle(task.node, 0, 0, nil, nil, false, task.priority)
		return
	}
	if a.expired() {
		return
	}
	a.mu.Lock()
	task.node.started = true
	a.mu.Unlock()
	a.setPhase(task.node.path)

	entries, err, ok := a.readDir(task.node.path)
	if !ok {
		a.stall(task.node)
		return
	}
	if err != nil {
		// The directory is missing from the totals, so the report names it and
		// says why. Without this the only trace was a bare no_access counter and
		// the page could not explain a single one of its skipped folders.
		kind := SkipIO
		if os.IsPermission(err) {
			kind = SkipPermission
		}
		skip := a.newSkip(a.class.logical(task.node.path), kind, readReason(err))
		a.settle(task.node, 0, 0, nil, []SkippedPath{skip}, os.IsPermission(err), task.priority)
		return
	}

	var ticks atomic.Int64
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.scanEntries(task, entries, &ticks)
	}()
	ticker := time.NewTicker(dirStallPoll)
	defer ticker.Stop()
	last := ticks.Load()
	lastMoved := time.Now()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if moved := ticks.Load(); moved != last {
				last = moved
				lastMoved = time.Now()
				continue
			}
			if time.Since(lastMoved) < dirStallTimeout {
				continue
			}
			a.stall(task.node)
			return
		}
	}
}

// readDir lists a directory, giving up after dirReadTimeout. ok is false when
// the listing was abandoned; the goroutine reading it stays in the kernel until
// the filesystem releases it.
func (a *scanAccumulator) readDir(path string) (entries []os.DirEntry, err error, ok bool) {
	type result struct {
		entries []os.DirEntry
		err     error
	}
	ch := make(chan result, 1)
	go func() {
		entries, err := os.ReadDir(path)
		ch <- result{entries: entries, err: err}
	}()
	timer := time.NewTimer(dirReadTimeout)
	defer timer.Stop()
	select {
	case r := <-ch:
		return r.entries, r.err, true
	case <-timer.C:
		return nil, nil, false
	}
}

// insideDropped reports whether the task's parent was abandoned, which means
// the whole tree is missing from the report and every task still queued under
// it can be settled without touching the filesystem.
func (a *scanAccumulator) insideDropped(node *scanNode) bool {
	parent := node.parent
	if parent == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return parent.abandoned
}

// stall drops the directory that stopped making progress. Only the directory
// itself is dropped: escalating to its parent would let one wedged subtree take
// a healthy parent such as ~/Library with it, and a report that silently loses
// ~/Library/Caches is worse than a slow scan. The bounds on this are the read
// timeout and the budget, both of which end the scan.
func (a *scanAccumulator) stall(node *scanNode) {
	a.abandon(node)
}

// abandon marks a directory unreadable and settles it with no bytes, so its
// parent can still finish. It is idempotent: a directory is abandoned at most
// once, and a late settle from the stuck goroutine sees the flag and does
// nothing.
//
// A directory whose listing already completed is never abandoned. The watchdog
// polls on a ticker, so it can decide a directory stalled in the same instant
// that the directory finished; dropping it then would consume its completion a
// second time and leave the parent counting work that no longer exists.
func (a *scanAccumulator) abandon(node *scanNode) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if node.abandoned || node.settled {
		return
	}
	node.abandoned = true
	// It holds no bytes, so it must never show up as one of the biggest folders.
	node.record = false
	a.settleLocked(node, 0, 0, nil, []SkippedPath{
		// The path is recorded before the walk away: nothing else will ever
		// describe this directory.
		a.newSkip(a.class.logical(node.path), SkipTimeout, fmt.Sprintf("operation timed out after %s", dirReadTimeout)),
	}, false, false)
}

// scanDir lists one directory, accounts for the files directly inside it, and
// queues the subdirectories. Sizes are published in a single critical section,
// because the completion counter of the parent must never be observed without
// the children that go with it.
// scanEntries folds an already listed directory into the tree. It ticks once per
// entry so the watchdog can tell a slow directory from a wedged one.
func (a *scanAccumulator) scanEntries(task *dirTask, entries []os.DirEntry, ticks *atomic.Int64) {
	node := task.node
	var (
		dirSize  int64
		dirFiles int64
		children []*scanNode
		skipped  []SkippedPath
	)
	if a.expired() {
		return
	}
	for _, entry := range entries {
		ticks.Add(1)
		if entry.IsDir() {
			path := filepath.Join(node.path, entry.Name())
			logical := a.class.logical(path)
			if kind, reason, skip := a.skipDir(logical, entry.Name()); skip {
				if reason != "" {
					skipped = append(skipped, a.newSkip(logical, kind, reason))
				}
				continue
			}
			child := &scanNode{path: path, parent: node, cat: "other", pending: 1}
			child.cat, child.reclaimable = a.class.classify(path)
			child.record = a.shouldRecord(path)
			children = append(children, child)
			continue
		}
		if !entry.Type().IsRegular() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		dirFiles++
		size := info.Size()
		if size <= 0 {
			continue
		}
		if dev, ino, ok := hardlinkKey(info); ok {
			// Several links share one inode, and two workers can reach it
			// through different directories. The bytes are parked and
			// attributed once the walk is over, to the link with the smallest
			// path, so the totals never depend on walk order.
			a.parkLink([2]uint64{dev, ino}, filepath.Join(node.path, entry.Name()), node, size)
			continue
		}
		dirSize += size
	}
	a.settle(node, dirSize, dirFiles, children, skipped, false, task.priority)
}

// settle publishes one directory: its own file bytes, the children it
// discovered, and the end of its listing. A node folds into its parent as soon
// as its last child subtree finishes, which is what makes the totals
// independent of the order the workers happen to finish in.
func (a *scanAccumulator) settle(
	node *scanNode,
	dirSize, dirFiles int64,
	children []*scanNode,
	skipped []SkippedPath,
	noAccess, priority bool,
) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if node.abandoned {
		// A stalled read owns this directory now; its bytes are lost and were
		// already reported as a skip.
		return
	}
	a.settleLocked(node, dirSize, dirFiles, children, skipped, noAccess, priority)
}

// settleLocked publishes one directory with the accumulator lock already held.
// A directory is published once. The watchdog and the worker that lists a
// directory can both arrive here for the same node, and publishing twice would
// count its bytes twice and decrement its parent's counter twice.
func (a *scanAccumulator) settleLocked(
	node *scanNode,
	dirSize, dirFiles int64,
	children []*scanNode,
	skipped []SkippedPath,
	noAccess, priority bool,
) {
	if node.settled {
		return
	}
	node.settled = true
	a.files += dirFiles
	if noAccess {
		a.noAccess++
	}
	for _, skip := range skipped {
		key := skip.Kind + "\x00" + skip.Path
		if a.skippedAt[key] {
			continue
		}
		a.skippedAt[key] = true
		a.skipped = append(a.skipped, skip)
	}
	if dirSize > 0 {
		node.size += dirSize
		a.walked += dirSize
		a.catBytes[node.cat] += dirSize
		if node.reclaimable {
			node.reclaim += dirSize
			a.catReclaim[node.cat] += dirSize
		}
	}
	if node.record && !node.recorded {
		node.recorded = true
		a.recorded = append(a.recorded, node)
	}

	for _, child := range children {
		// A subtree is only ever handed out once, even when the walk reaches it
		// twice, e.g. a priority root that the volume root also lists.
		if a.visited[child.path] {
			continue
		}
		a.visited[child.path] = true
		node.pending++
		a.queue.push(&dirTask{node: child, priority: priority})
	}
	a.completeLocked(node)
}

// completeLocked decrements a node's outstanding work and folds finished
// subtrees upwards. It runs under the accumulator lock and walks up the tree
// iteratively: depths here can exceed a hundred, and each level only costs a
// decrement.
//
// An abandoned directory is folded like any other. Abandoning a directory only
// settles its own listing; the children it handed to the queue before it stalled
// are still out there and each of them completes exactly once. Stopping the walk
// at the abandoned node would leave its parent's counter above zero forever,
// which is how a scan that finished inside its budget could still report a
// priority root as only partly read.
func (a *scanAccumulator) completeLocked(node *scanNode) {
	for node != nil {
		node.pending--
		if node.pending > 0 {
			return
		}
		parent := node.parent
		if parent == nil {
			return
		}
		parent.size += node.size
		parent.reclaim += node.reclaim
		node = parent
	}
}

// skipDir reports whether a subdirectory must be left alone, and under which
// kind. Everything here is by design: the scan chose not to look, rather than
// the filesystem refusing. A reason is only returned for skips that cost
// coverage and are worth telling the user about.
func (a *scanAccumulator) skipDir(logical, name string) (kind, reason string, skip bool) {
	if skipDirNames[name] {
		return SkipByDesign, "", true
	}
	if reason, ok := skipPaths[logical]; ok {
		return SkipByDesign, reason, true
	}
	if isCloudDrive(logical) {
		return SkipByDesign, "cloud drive", true
	}
	return "", "", false
}

// newSkip builds the report row for one directory the walk did not count. The
// path is shortened the same way entries are, so the page reads in one space,
// and the depth is stored because grouping folds by prefix.
func (a *scanAccumulator) newSkip(logical, kind, reason string) SkippedPath {
	path := shortenHome(logical, a.cfg.Home)
	return SkippedPath{Path: path, Kind: kind, Reason: reason, Depth: segments(path)}
}

// readReason keeps the errno text and drops the path the error repeats: the
// report already carries the path in its own field, and "operation not
// permitted" is the part the user recognizes.
func readReason(err error) string {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) && pathErr.Err != nil {
		return pathErr.Err.Error()
	}
	return err.Error()
}

// withinRoots keeps priority seeding inside the mount actually being scanned,
// so a test fixture never reaches out to the host's /Library/Developer.
func (a *scanAccumulator) withinRoots(path string) bool {
	for _, root := range a.cfg.Roots {
		root = filepath.Clean(root)
		if path == root || strings.HasPrefix(path, root+"/") {
			return true
		}
	}
	return false
}

// parkedLink is one inode's bytes, waiting for the walk to end. path picks the
// winner: the smallest link path is a stable choice, unlike "whichever worker
// saw it first".
type parkedLink struct {
	path string
	node *scanNode
	size int64
}

// parkLink records one sighting of a multi-link inode. The accumulator lock is
// already held by no one here, so it takes it briefly.
func (a *scanAccumulator) parkLink(key [2]uint64, path string, node *scanNode, size int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	seen, ok := a.links[key]
	if !ok {
		a.links[key] = &parkedLink{path: path, node: node, size: size}
		return
	}
	if path < seen.path {
		seen.path, seen.node, seen.size = path, node, size
	}
}

// attributeLinks adds each parked inode's bytes once. It runs after every
// worker has stopped, so the ancestors have already folded and are simply
// bumped by the delta instead of being recomputed.
func (a *scanAccumulator) attributeLinks() {
	for _, link := range a.links {
		node := link.node
		node.size += link.size
		if node.reclaimable {
			node.reclaim += link.size
			a.catReclaim[node.cat] += link.size
		}
		for parent := node.parent; parent != nil; parent = parent.parent {
			parent.size += link.size
			parent.reclaim += link.size
		}
		a.walked += link.size
		a.catBytes[node.cat] += link.size
	}
}
