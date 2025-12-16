//go:build !linux

package overlay

import "fmt"

// mountOverlay is not supported on non-Linux systems.
func (m *manager) mountOverlay(mount *Mount) error {
	return fmt.Errorf("%w: overlayfs requires Linux", ErrOverlayNotSupported)
}

// cleanupMount is not supported on non-Linux systems.
func (m *manager) cleanupMount(mount *Mount) error {
	return fmt.Errorf("%w: overlayfs requires Linux", ErrOverlayNotSupported)
}

// isMounted always returns false on non-Linux systems.
func (m *manager) isMounted(path string) bool {
	return false
}

// killProcesses is a no-op on non-Linux systems.
func (m *manager) killProcesses(mountPath string) error {
	return nil
}
