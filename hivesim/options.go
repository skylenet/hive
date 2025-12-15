package hivesim

import (
	"io"
	"os"

	"github.com/ethereum/hive/internal/simapi"
)

// clientSetup collects client options.
type clientSetup struct {
	config simapi.NodeConfig
	// destination path -> open data function
	files map[string]func() (io.ReadCloser, error)
}

// StartOption is a parameter for starting a client.
type StartOption interface {
	apply(setup *clientSetup)
}

type optionFunc func(setup *clientSetup)

func (fn optionFunc) apply(setup *clientSetup) { fn(setup) }

func fileAsSrc(path string) func() (io.ReadCloser, error) {
	return func() (io.ReadCloser, error) {
		return os.Open(path)
	}
}

// WithInitialNetworks configures networks that the client is initially connected to.
func WithInitialNetworks(networks []string) StartOption {
	return optionFunc(func(setup *clientSetup) {
		setup.config.Networks = make([]string, len(networks))
		copy(setup.config.Networks, networks)
	})
}

// WithStaticFiles adds files from the local filesystem to the client. Map: destination file path -> source file path.
func WithStaticFiles(initFiles map[string]string) StartOption {
	return optionFunc(func(setup *clientSetup) {
		for k, v := range initFiles {
			setup.files[k] = fileAsSrc(v)
		}
	})
}

// WithDynamicFile adds a file to a client, sourced dynamically from the given src function,
// called upon usage of the returned StartOption.
//
// A StartOption, and thus the src function, should be reusable and safe to use in parallel.
// Dynamic files can override static file sources (see WithStaticFiles) and vice-versa.
func WithDynamicFile(dstPath string, src func() (io.ReadCloser, error)) StartOption {
	return optionFunc(func(setup *clientSetup) {
		setup.files[dstPath] = src
	})
}

// Bundle combines start options, e.g. to bundle files together as option.
func Bundle(option ...StartOption) StartOption {
	return optionFunc(func(setup *clientSetup) {
		for _, opt := range option {
			opt.apply(setup)
		}
	})
}

// Params contains client launch parameters.
// This exists because tests usually want to define common parameters as
// a global variable and then customize them for specific clients.
type Params map[string]string

var _ StartOption = (Params)(nil)

// apply implements StartOption.
func (p Params) apply(setup *clientSetup) {
	for k, v := range p {
		setup.config.Environment[k] = v
	}
}

// Set returns a copy of the parameters with 'key' set to 'value'.
func (p Params) Set(key, value string) Params {
	cpy := p.Copy()
	cpy[key] = value
	return cpy
}

// Copy returns a copy of the parameters.
func (p Params) Copy() Params {
	cpy := make(Params, len(p))
	for k, v := range p {
		cpy[k] = v
	}
	return cpy
}

// WithOverlaySnapshot configures an overlay filesystem mount for the client.
// The snapshot at snapshotPath (on the host) is mounted as a read-only lower layer,
// with an ephemeral writable upper layer. The overlay appears at containerPath
// inside the container. Changes are discarded when the container stops.
//
// This is useful for fast client startup with pre-synced chain data.
// Note: This feature requires Linux and root/sudo privileges.
func WithOverlaySnapshot(snapshotPath, containerPath string) StartOption {
	return optionFunc(func(setup *clientSetup) {
		setup.config.Overlays = append(setup.config.Overlays, simapi.OverlaySpec{
			SnapshotPath:  snapshotPath,
			ContainerPath: containerPath,
		})
	})
}

// WithRemoteSnapshot configures a remote snapshot to be fetched by the hive host.
// The snapshot is downloaded and cached on the host, then mounted as an overlay.
//
// Parameters:
//   - network: Ethereum network (e.g., "mainnet", "sepolia", "holesky", "hoodi")
//   - client: Execution client name (e.g., "geth", "nethermind", "besu", "reth")
//   - containerPath: Where the snapshot appears inside the container (e.g., "/data")
//
// Note: This feature requires Linux and root/sudo privileges on the hive host.
func WithRemoteSnapshot(network, client, containerPath string) StartOption {
	return optionFunc(func(setup *clientSetup) {
		setup.config.Overlays = append(setup.config.Overlays, simapi.OverlaySpec{
			Network:       network,
			Client:        client,
			ContainerPath: containerPath,
		})
	})
}

// WithRemoteSnapshotAt is like WithRemoteSnapshot but fetches a specific block number.
func WithRemoteSnapshotAt(network, client, blockNumber, containerPath string) StartOption {
	return optionFunc(func(setup *clientSetup) {
		setup.config.Overlays = append(setup.config.Overlays, simapi.OverlaySpec{
			Network:       network,
			Client:        client,
			BlockNumber:   blockNumber,
			ContainerPath: containerPath,
		})
	})
}

// WithRemoteSnapshotURL configures a remote snapshot from a custom URL.
func WithRemoteSnapshotURL(network, client, baseURL, containerPath string) StartOption {
	return optionFunc(func(setup *clientSetup) {
		setup.config.Overlays = append(setup.config.Overlays, simapi.OverlaySpec{
			Network:       network,
			Client:        client,
			URL:           baseURL,
			ContainerPath: containerPath,
		})
	})
}

// WithClientSnapshot creates a StartOption from a ClientDefinition's snapshot config.
// This is the simplest way to use a snapshot configured in the client-config.yaml file.
// Returns nil if the client has no snapshot configured.
//
// Example usage:
//
//	snapshotOpt := hivesim.WithClientSnapshot(clientDef)
//	opts := []hivesim.StartOption{params}
//	if snapshotOpt != nil {
//	    opts = append(opts, snapshotOpt)
//	}
//	client := t.StartClient(clientDef.Name, opts...)
func WithClientSnapshot(client *ClientDefinition) StartOption {
	if !client.HasSnapshot() {
		return nil
	}

	snap := client.Snapshot
	containerPath := snap.SnapshotContainerPath()

	return optionFunc(func(setup *clientSetup) {
		setup.config.Overlays = append(setup.config.Overlays, simapi.OverlaySpec{
			Network:       snap.Network,
			Client:        client.Name, // Host will map the name
			BlockNumber:   snap.BlockNumber,
			URL:           snap.URL,
			ContainerPath: containerPath,
		})
	})
}
