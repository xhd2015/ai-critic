package agentcli

import "testing"

func TestParseUploadFlags(t *testing.T) {
	uf, err := parseUploadFlags([]string{"--dry-run", "--no-compress", "--no-override", "--chunk-size", "512K", "./a", "/tmp/a"})
	if err != nil {
		t.Fatalf("parseUploadFlags: %v", err)
	}
	if !uf.dryRun || !uf.noCompress || !uf.noOverride {
		t.Fatalf("dry=%v noCompress=%v noOverride=%v", uf.dryRun, uf.noCompress, uf.noOverride)
	}
	if uf.chunkSize != 512*1024 {
		t.Fatalf("chunkSize = %d, want %d", uf.chunkSize, 512*1024)
	}
	if len(uf.rest) != 2 || uf.rest[0] != "./a" || uf.rest[1] != "/tmp/a" {
		t.Fatalf("rest=%v", uf.rest)
	}

	uf, err = parseUploadFlags([]string{"./b"})
	if err != nil {
		t.Fatalf("parseUploadFlags: %v", err)
	}
	if uf.dryRun || uf.noCompress || uf.noOverride || uf.chunkSize != 0 || len(uf.rest) != 1 || uf.rest[0] != "./b" {
		t.Fatalf("got %#v", uf)
	}
}

func TestParseUploadFlagsChunkSizeForms(t *testing.T) {
	tests := []struct {
		args []string
		want int64
	}{
		{[]string{"--chunk-size", "1M", "./f"}, 1024 * 1024},
		{[]string{"--chunk-size=2M", "./f"}, 2 * 1024 * 1024},
		{[]string{"--chunk-size", "1048576", "./f"}, 1048576},
		{[]string{"--chunk-size", "64K", "./f"}, 64 * 1024},
	}
	for _, tc := range tests {
		uf, err := parseUploadFlags(tc.args)
		if err != nil {
			t.Fatalf("parseUploadFlags(%v): %v", tc.args, err)
		}
		if uf.chunkSize != tc.want {
			t.Fatalf("parseUploadFlags(%v).chunkSize = %d, want %d", tc.args, uf.chunkSize, tc.want)
		}
		if len(uf.rest) != 1 || uf.rest[0] != "./f" {
			t.Fatalf("rest = %v, want the positional arg intact", uf.rest)
		}
	}
}

func TestParseUploadFlagsChunkSizeErrors(t *testing.T) {
	for _, args := range [][]string{
		{"--chunk-size"},
		{"--chunk-size", "abc", "./f"},
		{"--chunk-size", "0", "./f"},
		{"--chunk-size", "-5M", "./f"},
	} {
		if _, err := parseUploadFlags(args); err == nil {
			t.Fatalf("parseUploadFlags(%v) = nil error, want failure", args)
		}
	}
}
