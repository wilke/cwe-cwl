//go:build linux

package sandbox

import (
	"os"
	"strconv"
	"syscall"
)

// applyResourceLimits sets OS-level resource constraints on Linux.
func applyResourceLimits() {
	// Memory limit from environment
	if memStr := os.Getenv("SANDBOX_MEMORY_MB"); memStr != "" {
		if memMB, err := strconv.ParseInt(memStr, 10, 64); err == nil {
			memBytes := uint64(memMB * 1024 * 1024)

			// Set address space limit
			var rLimit syscall.Rlimit
			rLimit.Cur = memBytes
			rLimit.Max = memBytes
			syscall.Setrlimit(syscall.RLIMIT_AS, &rLimit)
		}
	}

	// CPU time limit
	if timeStr := os.Getenv("SANDBOX_TIMEOUT_SEC"); timeStr != "" {
		if timeSec, err := strconv.ParseInt(timeStr, 10, 64); err == nil {
			var rLimit syscall.Rlimit
			rLimit.Cur = uint64(timeSec)
			rLimit.Max = uint64(timeSec)
			syscall.Setrlimit(syscall.RLIMIT_CPU, &rLimit)
		}
	}

	// Prevent fork bombs
	var nProcLimit syscall.Rlimit
	nProcLimit.Cur = 0
	nProcLimit.Max = 0
	syscall.Setrlimit(syscall.RLIMIT_NPROC, &nProcLimit)

	// No file creation
	var fSizeLimit syscall.Rlimit
	fSizeLimit.Cur = 0
	fSizeLimit.Max = 0
	syscall.Setrlimit(syscall.RLIMIT_FSIZE, &fSizeLimit)
}
