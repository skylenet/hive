// Package simapi contains definitions of JSON objects used in the simulation API.
package simapi

type TestRequest struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Location    string `json:"location"`
	Category    string `json:"category"`
	Description string `json:"description"`
}

// NodeConfig contains the launch parameters for a client container.
type NodeConfig struct {
	Client      string            `json:"client"`
	Networks    []string          `json:"networks"`
	Environment map[string]string `json:"environment"`
	Overlays    []OverlaySpec     `json:"overlays,omitempty"`
}

// OverlaySpec specifies an overlay filesystem mount for a container.
// The overlay uses a read-only snapshot directory with an ephemeral
// writable layer. Changes are discarded when the container stops.
//
// There are two ways to specify a snapshot:
// 1. Local: Set SnapshotPath to an existing host directory
// 2. Remote: Set Network and Client to fetch from ethpandaops (or custom URL)
type OverlaySpec struct {
	// SnapshotPath is the host path to the read-only snapshot directory.
	// If empty, Network/Client are used to fetch a remote snapshot.
	SnapshotPath string `json:"snapshotPath,omitempty"`

	// ContainerPath is where the overlay appears inside the container.
	ContainerPath string `json:"containerPath"`

	// Remote snapshot configuration (used if SnapshotPath is empty):

	// Network is the Ethereum network (e.g., "mainnet", "sepolia", "hoodi").
	Network string `json:"network,omitempty"`

	// Client is the execution client name for the snapshot (e.g., "geth", "nethermind").
	// If empty, defaults to the client being started.
	Client string `json:"client,omitempty"`

	// BlockNumber is a specific block number to fetch. Defaults to "latest".
	BlockNumber string `json:"block,omitempty"`

	// URL is a custom base URL for snapshots (optional, overrides ethpandaops default).
	URL string `json:"url,omitempty"`
}

// StartNodeResponse is returned by the client startup endpoint.
type StartNodeResponse struct {
	ID string `json:"id"` // Container ID.
	IP string `json:"ip"` // IP address in bridge network
}

// NodeResponse is the description of a running client as returned by the API.
type NodeResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ExecRequest struct {
	Command []string `json:"command"`
}

type Error struct {
	Error string `json:"error"`
}
