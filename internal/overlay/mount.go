//go:build linux

package overlay

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// mountOverlay performs the OverlayFS mount syscall.
func (m *manager) mountOverlay(mount *Mount) error {
	// Build mount options.
	// lowerdir: read-only snapshot
	// upperdir: writable changes layer
	// workdir: overlayfs internal metadata
	// redirect_dir: enables efficient directory rename operations
	opts := fmt.Sprintf(
		"lowerdir=%s,upperdir=%s,workdir=%s,redirect_dir=on",
		mount.LowerDir,
		mount.UpperDir,
		mount.WorkDir,
	)

	m.logger.Debug("mounting overlay",
		"target", mount.MergedDir,
		"options", opts)

	// Perform the mount syscall.
	if err := syscall.Mount("overlay", mount.MergedDir, "overlay", 0, opts); err != nil {
		// Check for permission error.
		if os.IsPermission(err) {
			return fmt.Errorf("%w: %v", ErrPermissionDenied, err)
		}
		return fmt.Errorf("%w: %v", ErrMountFailed, err)
	}

	return nil
}

// cleanupMount unmounts an overlay and removes its directories.
func (m *manager) cleanupMount(mount *Mount) error {
	// Check if actually mounted.
	if !m.isMounted(mount.MergedDir) {
		m.logger.Debug("overlay not mounted, cleaning up directories only",
			"merged", mount.MergedDir)
		return m.cleanupDirs(mount)
	}

	// Try normal unmount.
	m.logger.Debug("attempting normal unmount", "path", mount.MergedDir)
	if err := syscall.Unmount(mount.MergedDir, 0); err == nil {
		return m.cleanupDirs(mount)
	}

	// Try lazy unmount (MNT_DETACH).
	m.logger.Debug("attempting lazy unmount", "path", mount.MergedDir)
	if err := syscall.Unmount(mount.MergedDir, syscall.MNT_DETACH); err == nil {
		// Give filesystem time to actually unmount.
		time.Sleep(100 * time.Millisecond)
		return m.cleanupDirs(mount)
	}

	// Try to kill processes using the mount.
	m.logger.Warn("attempting to kill processes using mount", "path", mount.MergedDir)
	if err := m.killProcesses(mount.MergedDir); err != nil {
		m.logger.Warn("failed to kill processes", "path", mount.MergedDir, "err", err)
	}

	// Wait for processes to die.
	time.Sleep(500 * time.Millisecond)

	// Final attempt with force and lazy.
	m.logger.Debug("attempting force unmount", "path", mount.MergedDir)
	if err := syscall.Unmount(mount.MergedDir, syscall.MNT_FORCE|syscall.MNT_DETACH); err != nil {
		m.logger.Error("all unmount attempts failed",
			"path", mount.MergedDir,
			"err", err)
		return fmt.Errorf("%w: %v", ErrUnmountFailed, err)
	}

	return m.cleanupDirs(mount)
}

// cleanupDirs removes the overlay directory structure.
func (m *manager) cleanupDirs(mount *Mount) error {
	// Get the parent directory (the overlay ID directory).
	overlayDir := fmt.Sprintf("%s/%s", m.config.BaseDir, mount.ID)

	m.logger.Debug("removing overlay directories", "path", overlayDir)

	if err := os.RemoveAll(overlayDir); err != nil {
		m.logger.Warn("failed to remove overlay directory",
			"path", overlayDir,
			"err", err)
		return err
	}

	return nil
}

// isMounted checks if a path is a mount point by reading /proc/mounts.
func (m *manager) isMounted(path string) bool {
	file, err := os.Open("/proc/mounts")
	if err != nil {
		return false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[1] == path {
			return true
		}
	}

	return false
}

// killProcesses attempts to kill any processes using the given mount point.
// Uses fuser command if available.
func (m *manager) killProcesses(mountPath string) error {
	// Try using fuser to kill processes.
	// fuser -km: kill processes using the filesystem, send SIGKILL
	cmd := exec.Command("fuser", "-km", mountPath)
	if err := cmd.Run(); err != nil {
		// fuser returns non-zero if no processes found, which is fine.
		m.logger.Debug("fuser completed", "path", mountPath, "err", err)
	}
	return nil
}
