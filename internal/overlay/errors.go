// Package overlay provides OverlayFS-based snapshot management for Hive containers.
package overlay

import "errors"

var (
	// ErrOverlayNotSupported indicates OverlayFS is not available on this system.
	ErrOverlayNotSupported = errors.New("overlay filesystem not supported")

	// ErrSnapshotNotFound indicates the snapshot path doesn't exist.
	ErrSnapshotNotFound = errors.New("snapshot path not found")

	// ErrSnapshotNotDirectory indicates the snapshot path is not a directory.
	ErrSnapshotNotDirectory = errors.New("snapshot path is not a directory")

	// ErrMountFailed indicates the mount syscall failed.
	ErrMountFailed = errors.New("overlay mount failed")

	// ErrUnmountFailed indicates cleanup failed after multiple attempts.
	ErrUnmountFailed = errors.New("overlay unmount failed")

	// ErrPermissionDenied indicates insufficient privileges for overlay operations.
	ErrPermissionDenied = errors.New("insufficient privileges for overlay mount")

	// ErrOverlayExists indicates an overlay already exists for the given container.
	ErrOverlayExists = errors.New("overlay already exists for container")

	// ErrOverlayNotFound indicates no overlay exists for the given container.
	ErrOverlayNotFound = errors.New("overlay not found for container")
)
