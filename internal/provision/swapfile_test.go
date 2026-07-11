package provision

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestResizeSwapfileRunsOnlyTargetedPlaybook(t *testing.T) {
	az := &fakeAzure{info: runningInfo()}
	ts := &fakeTailscale{findPeerResults: []peerResult{{peer: onlinePeer()}}}
	svc, runner, waiter := newTestService(t, az, ts)

	requireNoErr(t, svc.ResizeSwapfile(context.Background()))

	if len(runner.params) != 1 {
		t.Fatalf("Ansible runs = %d, want 1", len(runner.params))
	}
	p := runner.params[0]
	if p.Playbook != "swapfile.yml" {
		t.Fatalf("playbook = %q, want swapfile.yml", p.Playbook)
	}
	if p.Host != "rover-vm.tailnet.test" {
		t.Fatalf("host = %q", p.Host)
	}
	if len(waiter.hosts) != 1 || waiter.hosts[0] != p.Host {
		t.Fatalf("wait hosts = %v, want %q", waiter.hosts, p.Host)
	}
}

func TestResizeSwapfileRejectsStoppedVM(t *testing.T) {
	info := runningInfo()
	info.PowerState = "VM deallocated"
	svc, runner, _ := newTestService(t, &fakeAzure{info: info}, nil)

	err := svc.ResizeSwapfile(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("error = %v", err)
	}
	if len(runner.params) != 0 {
		t.Fatalf("Ansible runs = %d, want 0", len(runner.params))
	}
}

func TestSwapfileScriptPlan(t *testing.T) {
	tests := []struct {
		name       string
		memKiB     string
		fileBytes  int64
		active     bool
		wantFields string
	}{
		{name: "creates half-RAM swap", memKiB: "8388608", wantFields: "action=create target_bytes=4294967296 current_bytes=0 active=no"},
		{name: "keeps correctly sized active manual swap", memKiB: "4194304", fileBytes: 2147483648, active: true, wantFields: "action=keep target_bytes=2147483648 current_bytes=2147483648 active=yes"},
		{name: "resizes wrong existing swap", memKiB: "2097152", fileBytes: 536870912, wantFields: "action=resize target_bytes=1073741824 current_bytes=536870912 active=no"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			meminfo := filepath.Join(dir, "meminfo")
			swap := filepath.Join(dir, "swapfile")
			if err := os.WriteFile(meminfo, []byte("MemTotal:       "+tt.memKiB+" kB\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if tt.fileBytes > 0 {
				f, err := os.Create(swap)
				if err != nil {
					t.Fatal(err)
				}
				if err := f.Truncate(tt.fileBytes); err != nil {
					t.Fatal(err)
				}
				_ = f.Close()
			}
			active := ""
			if tt.active {
				active = swap
			}
			cmd := exec.Command("bash", filepath.Join("..", "..", "scripts", "swapfile"), "plan")
			cmd.Env = append(os.Environ(),
				"ROVER_MEMINFO="+meminfo,
				"ROVER_SWAPFILE="+swap,
				"ROVER_ACTIVE_SWAPFILES="+active,
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("plan: %v\n%s", err, out)
			}
			if got := strings.TrimSpace(string(out)); got != tt.wantFields {
				t.Fatalf("plan = %q, want %q", got, tt.wantFields)
			}
		})
	}
}

func TestSwapfileScriptRefusesInsufficientDiskBeforeSwapoff(t *testing.T) {
	dir := t.TempDir()
	meminfo := filepath.Join(dir, "meminfo")
	swap := filepath.Join(dir, "swapfile")
	fstab := filepath.Join(dir, "fstab")
	bin := filepath.Join(dir, "bin")
	log := filepath.Join(dir, "calls")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, meminfo, "MemTotal: 2097152 kB\n", 0o644)
	writeTestFile(t, fstab, "# test fstab\n", 0o644)
	f, err := os.Create(swap)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(536870912); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	writeTestFile(t, filepath.Join(bin, "id"), "#!/bin/sh\nprintf '0\\n'\n", 0o755)
	writeTestFile(t, filepath.Join(bin, "df"), "#!/bin/sh\nprintf 'Filesystem 1-blocks Used Available Capacity Mounted on\\nmock 1 0 1 0%% /\\n'\n", 0o755)
	writeTestFile(t, filepath.Join(bin, "du"), "#!/bin/sh\nprintf '0 %s\\n' \"$2\"\n", 0o755)
	writeTestFile(t, filepath.Join(bin, "swapoff"), "#!/bin/sh\nprintf 'swapoff\\n' >>\"$ROVER_TEST_LOG\"\n", 0o755)

	cmd := exec.Command("bash", filepath.Join("..", "..", "scripts", "swapfile"), "apply")
	cmd.Env = append(os.Environ(),
		"PATH="+bin+":"+os.Getenv("PATH"),
		"ROVER_MEMINFO="+meminfo,
		"ROVER_SWAPFILE="+swap,
		"ROVER_FSTAB="+fstab,
		"ROVER_ACTIVE_SWAPFILES="+swap,
		"ROVER_TEST_LOG="+log,
	)
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "insufficient disk space") {
		t.Fatalf("apply error = %v, output = %s", err, out)
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatalf("swapoff ran before disk-space rejection; stat error = %v", err)
	}
}

func TestSwapfileScriptDisablesActiveSwapBeforeResize(t *testing.T) {
	dir := t.TempDir()
	meminfo := filepath.Join(dir, "meminfo")
	swap := filepath.Join(dir, "swapfile")
	fstab := filepath.Join(dir, "fstab")
	bin := filepath.Join(dir, "bin")
	log := filepath.Join(dir, "calls")
	procSwaps := filepath.Join(dir, "proc-swaps")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, meminfo, "MemTotal: 2097152 kB\n", 0o644)
	writeTestFile(t, fstab, swap+" none swap defaults 0 0\n", 0o644)
	writeTestFile(t, procSwaps, "Filename Type Size Used Priority\n"+swap+" file 1 0 -2\n", 0o644)
	f, err := os.Create(swap)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(536870912); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	writeTestFile(t, filepath.Join(bin, "id"), "#!/bin/sh\nprintf '0\\n'\n", 0o755)
	writeTestFile(t, filepath.Join(bin, "df"), "#!/bin/sh\nprintf 'Filesystem 1-blocks Used Available Capacity Mounted on\\nmock 3000000000 0 2000000000 0%% /\\n'\n", 0o755)
	writeTestFile(t, filepath.Join(bin, "du"), "#!/bin/sh\nprintf '536870912 %s\\n' \"$2\"\n", 0o755)
	writeTestFile(t, filepath.Join(bin, "fallocate"), "#!/bin/sh\ntruncate -s \"$2\" \"$3\"\n", 0o755)
	writeTestFile(t, filepath.Join(bin, "swapoff"), "#!/bin/sh\nprintf 'swapoff\\n' >>\"$ROVER_TEST_LOG\"\nprintf 'Filename Type Size Used Priority\\n' >\"$ROVER_PROC_SWAPS\"\n", 0o755)
	for _, command := range []string{"mkswap", "swapon"} {
		writeTestFile(t, filepath.Join(bin, command), "#!/bin/sh\nprintf '"+command+"\\n' >>\"$ROVER_TEST_LOG\"\n", 0o755)
	}

	cmd := exec.Command("bash", filepath.Join("..", "..", "scripts", "swapfile"), "apply")
	cmd.Env = append(os.Environ(),
		"PATH="+bin+":"+os.Getenv("PATH"),
		"ROVER_MEMINFO="+meminfo,
		"ROVER_SWAPFILE="+swap,
		"ROVER_FSTAB="+fstab,
		"ROVER_PROC_SWAPS="+procSwaps,
		"ROVER_TEST_LOG="+log,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("apply: %v\n%s", err, out)
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(calls), "swapoff\nmkswap\nswapon\n"; got != want {
		t.Fatalf("calls = %q, want %q", got, want)
	}
	info, err := os.Stat(swap)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 1073741824 {
		t.Fatalf("swap size = %d, want 1073741824", info.Size())
	}
	fstabData, err := os.ReadFile(fstab)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(fstabData), swap+" none swap sw 0 0\n"; got != want {
		t.Fatalf("fstab = %q, want %q", got, want)
	}
}

func writeTestFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}
