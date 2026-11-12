package services

import (
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"testing"

	"github.com/xhd2015/dot-pkgs/go-pkgs/service"
)

// TestKillConfiguredPortOwnerFreesNonLeaderListener guards the port reclaim
// path: the listener is a child of the shell that started it, so its pid is not
// its group leader. Signalling only that pid leaves the rest of the group
// holding the port, which is what reclaiming a stale service must avoid.
func TestKillConfiguredPortOwnerFreesNonLeaderListener(t *testing.T) {
	port := freePort(t)
	cmd := exec.Command("bash", "-c",
		fmt.Sprintf("python3 -m http.server %d --bind 127.0.0.1 & sleep 300", port))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		_ = service.StopProcessGroup(cmd.Process.Pid)
		_, _ = cmd.Process.Wait()
	}()

	if !waitForServing(t, port, true, 15*time.Second) {
		t.Fatalf("listener never bound port %d", port)
	}
	pids, err := service.ListeningPIDs(port)
	if err != nil {
		t.Fatalf("ListeningPIDs(%d) error = %v", port, err)
	}
	if len(pids) != 1 {
		t.Fatalf("ListeningPIDs(%d) = %v, want exactly the listener", port, pids)
	}
	listenerPID := pids[0]
	if listenerPID == cmd.Process.Pid {
		t.Fatalf("listener pid %d is the group leader; want a non-leader listener", listenerPID)
	}

	if err := killConfiguredPortOwner(&ServicePortForward{Port: port}); err != nil {
		t.Fatalf("killConfiguredPortOwner(%d) error = %v", port, err)
	}
	if service.Alive(listenerPID) {
		t.Fatalf("listener pid %d survived killConfiguredPortOwner", listenerPID)
	}
	if !waitForServing(t, port, false, 5*time.Second) {
		t.Fatalf("port %d still served after killConfiguredPortOwner", port)
	}
}

// TestServiceAliveTreatsZombieAsDead pins the zombie rule the extraction
// inherits from go-pkgs/service: an unreaped child still answers kill(pid, 0),
// so stop logic must not treat it as running, or restarts fail with
// "process N did not exit".
func TestServiceAliveTreatsZombieAsDead(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("zombie /proc state is Linux-specific")
	}
	// Parent holds a zombie child: fork+exit in child, sleep in parent.
	cmd := exec.Command("python3", "-c", "import os,time\npid=os.fork()\nif pid==0:\n os._exit(0)\ntime.sleep(60)")
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	var zombiePID int
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		out, err := exec.Command("bash", "-c",
			"ps -o pid=,stat= --ppid "+strconv.Itoa(cmd.Process.Pid)+" | awk '$2 ~ /Z/ {print $1; exit}'").Output()
		if err == nil {
			s := strings.TrimSpace(string(out))
			if s != "" {
				zombiePID, _ = strconv.Atoi(s)
				if zombiePID > 0 {
					break
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if zombiePID <= 0 {
		t.Fatal("failed to create zombie child")
	}
	if !service.Zombie(zombiePID) {
		t.Fatalf("pid %d should be zombie", zombiePID)
	}
	if service.Alive(zombiePID) {
		t.Fatalf("service.Alive(%d) = true for zombie; want false", zombiePID)
	}
	if err := service.StopProcess(zombiePID); err != nil {
		t.Fatalf("service.StopProcess(zombie) error = %v", err)
	}
}

func TestCreateOrUpdateRestartOfMissingPIDStillSaves(t *testing.T) {
	dir := t.TempDir()
	m := NewManagerAt(dir)
	enabled := true
	def := ServiceDefinition{
		Name:       "restart-flake",
		Command:    "sleep 0.3",
		WorkingDir: dir,
		Enabled:    &enabled,
	}

	saved, err := m.createOrUpdate(def, false)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := m.Start(saved.ID); err != nil {
		t.Fatalf("start: %v", err)
	}
	// Let the child exit on its own so the supervisor's tracked pid is gone by
	// the time the restart runs; this is the same class of failure as a zombie
	// for stop, where the pid it remembers is no longer stoppable.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if proc, ok := m.sup.Process(saved.ID); !ok || proc.PID == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	procDef := m.defs[0]
	procDef.Command = "sleep 301"
	out, err := m.createOrUpdate(procDef, true)
	if err != nil {
		t.Fatalf("createOrUpdate with restart should not fail after save: %v", err)
	}
	if out.ID != saved.ID {
		t.Fatalf("id=%s want %s", out.ID, saved.ID)
	}
	if out.Command != "sleep 301" {
		t.Fatalf("command not saved: %q", out.Command)
	}
	_ = m.stop(saved.ID, true, true)
}
