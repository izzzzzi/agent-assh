//go:build !windows

package safety

import (
	"os"
	"syscall"
)

// checkOwnership returns an error if the file is not owned by the current user.
// Only meaningful on Unix platforms.
func checkOwnership(path string, info os.FileInfo) error {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		if int(st.Uid) != os.Getuid() {
			return &PolicyError{
				Code:    "safety_policy_invalid",
				Message: path + " is not owned by the current user",
				Hint:    "chown $(id -un) " + path,
			}
		}
	}
	return nil
}
