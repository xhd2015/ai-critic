//go:build !darwin && !linux && !windows

package appmon

import "runtime"

// scanPlatform reports the feature as unsupported: Windows/macOS/Linux are the
// only platforms with an installed-app definition, and other unixes are rare
// enough that a silent empty list would be confusing.
func scanPlatform(env fsEnv) ([]App, []string, string, error) {
	_ = runtime.GOOS
	return nil, nil, noteUnsupported, nil
}

func iconFor(appPath string, env fsEnv) ([]byte, error) {
	return nil, ErrNoIcon
}
