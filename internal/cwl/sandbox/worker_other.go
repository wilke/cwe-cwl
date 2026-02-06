//go:build !linux

package sandbox

// applyResourceLimits is a no-op on non-Linux platforms.
// Resource limits are enforced via timeout in the parent process.
func applyResourceLimits() {
	// On macOS/Windows, we rely on:
	// 1. Parent process timeout and kill
	// 2. goja interrupt mechanism
	// 3. Container-level limits (if using container sandbox)
}
