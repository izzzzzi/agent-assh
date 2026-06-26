//go:build windows

package safety

import "os"

// checkOwnership is a no-op on Windows (ownership concept differs).
func checkOwnership(path string, info os.FileInfo) error {
	return nil
}
