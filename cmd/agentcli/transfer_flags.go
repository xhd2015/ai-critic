package agentcli

import (
	"fmt"
	"strconv"
	"strings"
)

// parseTransferFlags extracts --dry-run from argv and returns the remaining positional args.
func parseTransferFlags(args []string) (dryRun bool, rest []string) {
	for _, arg := range args {
		if arg == "--dry-run" {
			dryRun = true
			continue
		}
		rest = append(rest, arg)
	}
	return dryRun, rest
}

// uploadFlags is the parsed form of the upload-only switches.
type uploadFlags struct {
	dryRun     bool
	noCompress bool
	noOverride bool
	// chunkSize is the caller's --chunk-size in bytes; 0 means "use the
	// adaptive default".
	chunkSize int64
	rest      []string
}

// parseUploadFlags extracts upload-specific flags. --chunk-size accepts a plain
// byte count or a K/M suffix ("512K", "2M"); an unparsable value is reported
// rather than silently ignored, because a typo here changes transfer behaviour.
func parseUploadFlags(args []string) (uploadFlags, error) {
	var out uploadFlags
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--dry-run":
			out.dryRun = true
		case arg == "--no-compress":
			out.noCompress = true
		case arg == "--no-override":
			out.noOverride = true
		case arg == "--chunk-size":
			if i+1 >= len(args) {
				return out, fmt.Errorf("--chunk-size requires a value (e.g. 512K, 1M, 1048576)")
			}
			i++
			size, err := parseChunkSize(args[i])
			if err != nil {
				return out, err
			}
			out.chunkSize = size
		case strings.HasPrefix(arg, "--chunk-size="):
			size, err := parseChunkSize(strings.TrimPrefix(arg, "--chunk-size="))
			if err != nil {
				return out, err
			}
			out.chunkSize = size
		default:
			out.rest = append(out.rest, arg)
		}
	}
	return out, nil
}

// parseChunkSize turns "512K" / "2M" / "1048576" into bytes.
func parseChunkSize(raw string) (int64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, fmt.Errorf("--chunk-size value is empty")
	}
	mult := int64(1)
	switch last := s[len(s)-1]; last {
	case 'k', 'K':
		mult, s = 1024, s[:len(s)-1]
	case 'm', 'M':
		mult, s = 1024*1024, s[:len(s)-1]
	case 'g', 'G':
		mult, s = 1024*1024*1024, s[:len(s)-1]
	}
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("--chunk-size %q is not a size (want e.g. 512K, 1M, 1048576)", raw)
	}
	if n <= 0 {
		return 0, fmt.Errorf("--chunk-size must be positive, got %q", raw)
	}
	return n * mult, nil
}
