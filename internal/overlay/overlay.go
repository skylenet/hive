package overlay

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// DefaultBaseDirName is the default directory name for overlay data.
	DefaultBaseDirName = ".hive/overlays"

	// EnvOverlayDir is the environment variable to override the overlay directory.
	EnvOverlayDir = "HIVE_OVERLAY_DIR"
)

// Config specifies an overlay mount request from a simulator.
type Config struct {
	// SnapshotPath is the host path to the read-only snapshot directory (lower dir).
	SnapshotPath string
	// ContainerMountPath is where the overlay appears inside the container.
	ContainerMountPath string
}

// Mount represents an active overlay filesystem mount.
type Mount struct {
	ID            string    `json:"id"`
	ContainerID   string    `json:"containerId"`
	LowerDir      string    `json:"lowerDir"`
	UpperDir      string    `json:"upperDir"`
	WorkDir       string    `json:"workDir"`
	MergedDir     string    `json:"mergedDir"`
	ContainerPath string    `json:"containerPath"`
	CreatedAt     time.Time `json:"createdAt"`
}

// ManagerConfig configures the overlay manager.
type ManagerConfig struct {
	// BaseDir is the directory where overlay work directories are created.
	// Defaults to /var/lib/hive/overlays
	BaseDir string
	// Logger for overlay operations.
	Logger *slog.Logger
}

// DefaultManagerConfig returns sensible defaults for the overlay manager.
// The base directory defaults to ./.hive/overlays relative to the current working directory,
// but can be overridden via the HIVE_OVERLAY_DIR environment variable.
func DefaultManagerConfig() ManagerConfig {
	baseDir := os.Getenv(EnvOverlayDir)
	if baseDir == "" {
		// Default to current working directory + .hive/overlays
		cwd, err := os.Getwd()
		if err != nil {
			cwd = "."
		}
		baseDir = filepath.Join(cwd, DefaultBaseDirName)
	}
	return ManagerConfig{
		BaseDir: baseDir,
		Logger:  slog.Default(),
	}
}

// Manager manages OverlayFS mounts for containers.
type Manager interface {
	// CreateOverlay creates a new overlay mount for a container.
	// Returns the mount info which includes MergedDir for bind mounting.
	CreateOverlay(containerID string, config Config) (*Mount, error)

	// CleanupOverlay unmounts and removes an overlay for a container.
	CleanupOverlay(containerID string) error

	// CleanupAll unmounts and removes all managed overlays.
	CleanupAll() error

	// RecoverOrphanedMounts cleans up any orphaned mounts from previous runs.
	RecoverOrphanedMounts() error

	// GetOverlay returns the overlay for a container, if any.
	GetOverlay(containerID string) (*Mount, bool)
}

// manager implements Manager.
type manager struct {
	config   ManagerConfig
	logger   *slog.Logger
	mu       sync.RWMutex
	overlays map[string]*Mount // containerID -> mount
}

// NewManager creates a new overlay manager.
func NewManager(config ManagerConfig) (Manager, error) {
	if config.BaseDir == "" {
		config.BaseDir = DefaultManagerConfig().BaseDir
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}

	// Ensure base directory exists.
	if err := os.MkdirAll(config.BaseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create overlay base directory: %w", err)
	}

	m := &manager{
		config:   config,
		logger:   config.Logger.With("component", "overlay-manager"),
		overlays: make(map[string]*Mount),
	}

	return m, nil
}

// CreateOverlay creates a new overlay mount for a container.
func (m *manager) CreateOverlay(containerID string, config Config) (*Mount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if overlay already exists.
	if _, exists := m.overlays[containerID]; exists {
		return nil, ErrOverlayExists
	}

	// Validate snapshot path.
	info, err := os.Stat(config.SnapshotPath)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("%w: %s", ErrSnapshotNotFound, config.SnapshotPath)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to stat snapshot path: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%w: %s", ErrSnapshotNotDirectory, config.SnapshotPath)
	}

	// Generate unique overlay ID.
	overlayID := fmt.Sprintf("%s_%d", containerID[:12], time.Now().UnixNano())

	// Create overlay directory structure.
	overlayDir := filepath.Join(m.config.BaseDir, overlayID)
	upperDir := filepath.Join(overlayDir, "upper")
	workDir := filepath.Join(overlayDir, "work")
	mergedDir := filepath.Join(overlayDir, "merged")

	for _, dir := range []string{upperDir, workDir, mergedDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			// Clean up on failure.
			os.RemoveAll(overlayDir)
			return nil, fmt.Errorf("failed to create overlay directory %s: %w", dir, err)
		}
	}

	// Create mount struct.
	mount := &Mount{
		ID:            overlayID,
		ContainerID:   containerID,
		LowerDir:      config.SnapshotPath,
		UpperDir:      upperDir,
		WorkDir:       workDir,
		MergedDir:     mergedDir,
		ContainerPath: config.ContainerMountPath,
		CreatedAt:     time.Now(),
	}

	// Perform the actual mount.
	if err := m.mountOverlay(mount); err != nil {
		os.RemoveAll(overlayDir)
		return nil, err
	}

	// Register the overlay.
	m.overlays[containerID] = mount

	// Persist state for crash recovery.
	if err := m.persistState(); err != nil {
		m.logger.Warn("failed to persist overlay state", "err", err)
	}

	m.logger.Info("created overlay",
		"containerId", containerID,
		"overlayId", overlayID,
		"snapshot", config.SnapshotPath,
		"merged", mergedDir)

	return mount, nil
}

// CleanupOverlay unmounts and removes an overlay for a container.
func (m *manager) CleanupOverlay(containerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	mount, exists := m.overlays[containerID]
	if !exists {
		return nil // No overlay to clean up.
	}

	if err := m.cleanupMount(mount); err != nil {
		m.logger.Error("failed to cleanup overlay",
			"containerId", containerID,
			"err", err)
		return err
	}

	delete(m.overlays, containerID)

	// Update persisted state.
	if err := m.persistState(); err != nil {
		m.logger.Warn("failed to persist overlay state", "err", err)
	}

	m.logger.Info("cleaned up overlay",
		"containerId", containerID,
		"overlayId", mount.ID)

	return nil
}

// CleanupAll unmounts and removes all managed overlays.
func (m *manager) CleanupAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var lastErr error
	for containerID, mount := range m.overlays {
		if err := m.cleanupMount(mount); err != nil {
			m.logger.Error("failed to cleanup overlay",
				"containerId", containerID,
				"err", err)
			lastErr = err
		}
	}

	// Clear the map.
	m.overlays = make(map[string]*Mount)

	// Clear persisted state.
	statePath := filepath.Join(m.config.BaseDir, "state.json")
	os.Remove(statePath)

	return lastErr
}

// RecoverOrphanedMounts cleans up any orphaned mounts from previous runs.
func (m *manager) RecoverOrphanedMounts() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	statePath := filepath.Join(m.config.BaseDir, "state.json")
	data, err := os.ReadFile(statePath)
	if os.IsNotExist(err) {
		return nil // No state to recover.
	}
	if err != nil {
		return fmt.Errorf("failed to read state file: %w", err)
	}

	var state map[string]*Mount
	if err := json.Unmarshal(data, &state); err != nil {
		m.logger.Warn("failed to parse state file, removing", "err", err)
		os.Remove(statePath)
		return nil
	}

	// Clean up each orphaned overlay.
	for containerID, mount := range state {
		m.logger.Info("recovering orphaned overlay",
			"containerId", containerID,
			"overlayId", mount.ID)

		if err := m.cleanupMount(mount); err != nil {
			m.logger.Error("failed to cleanup orphaned overlay",
				"containerId", containerID,
				"err", err)
		}
	}

	// Remove state file.
	os.Remove(statePath)

	return nil
}

// GetOverlay returns the overlay for a container, if any.
func (m *manager) GetOverlay(containerID string) (*Mount, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	mount, exists := m.overlays[containerID]
	return mount, exists
}

// persistState writes overlay state to disk for crash recovery.
func (m *manager) persistState() error {
	if len(m.overlays) == 0 {
		// Remove state file if no overlays.
		statePath := filepath.Join(m.config.BaseDir, "state.json")
		os.Remove(statePath)
		return nil
	}

	data, err := json.MarshalIndent(m.overlays, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	statePath := filepath.Join(m.config.BaseDir, "state.json")
	if err := os.WriteFile(statePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}

	return nil
}

// Verify interface compliance.
var _ Manager = (*manager)(nil)
