package hivesim

import "slices"

// SuiteID identifies a test suite context.
type SuiteID uint32

// TestID identifies a test case context.
type TestID uint32

// TestResult describes the outcome of a test.
type TestResult struct {
	Pass    bool   `json:"pass"`
	Details string `json:"details"`
}

// TestStartInfo contains metadata about a test which is supplied to the hive API.
type TestStartInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Location    string `json:"location"`
	Category    string `json:"category"`
	Description string `json:"description"`
}

// ExecInfo is the result of running a command in a client container.
type ExecInfo struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exitCode"`
}

// ClientMetadata is part of the ClientDefinition and lists metadata
type ClientMetadata struct {
	Roles []string `yaml:"roles" json:"roles"`
}

// ClientDefinition is served by the /clients API endpoint to list the available clients
type ClientDefinition struct {
	Name     string                `json:"name"`
	Version  string                `json:"version"`
	Meta     ClientMetadata        `json:"meta"`
	Snapshot *ClientSnapshotConfig `json:"snapshot,omitempty"`
}

// HasRole reports whether the client has the given role.
func (m *ClientDefinition) HasRole(role string) bool {
	return slices.Contains(m.Meta.Roles, role)
}

// ClientSnapshotConfig specifies snapshot configuration for a client.
// When present, simulators can use this to auto-mount pre-synced data.
type ClientSnapshotConfig struct {
	// Network is the Ethereum network (e.g., "mainnet", "sepolia", "holesky", "hoodi").
	Network string `json:"network"`

	// URL is a custom snapshot URL (optional, overrides ethpandaops default).
	URL string `json:"url,omitempty"`

	// BlockNumber is a specific block number. Defaults to "latest".
	BlockNumber string `json:"block,omitempty"`

	// ContainerPath is where the snapshot appears inside the container.
	// Defaults to "/data".
	ContainerPath string `json:"path,omitempty"`

	// CacheDir overrides the default snapshot cache directory.
	CacheDir string `json:"cache_dir,omitempty"`
}

// HasSnapshot returns true if this client has a snapshot configured.
func (c *ClientDefinition) HasSnapshot() bool {
	return c.Snapshot != nil && c.Snapshot.Network != ""
}

// SnapshotContainerPath returns the container path for the snapshot,
// defaulting to "/data" if not specified.
func (c *ClientSnapshotConfig) SnapshotContainerPath() string {
	if c.ContainerPath != "" {
		return c.ContainerPath
	}
	return "/data"
}
