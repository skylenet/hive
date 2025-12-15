package hivesim

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DefaultSnapshotBaseURL is the default base URL for ethpandaops snapshots.
	DefaultSnapshotBaseURL = "https://snapshots.ethpandaops.io"

	// SnapshotFileName is the name of the snapshot archive file.
	SnapshotFileName = "snapshot.tar.zst"

	// SnapshotMetadataFile contains block information for the snapshot.
	SnapshotMetadataFile = "_snapshot_eth_getBlockByNumber.json"

	// DefaultSnapshotCacheDirName is the default directory name for snapshot cache.
	DefaultSnapshotCacheDirName = ".hive/snapshots"

	// EnvSnapshotCacheDir is the environment variable to override the snapshot cache directory.
	EnvSnapshotCacheDir = "HIVE_SNAPSHOT_DIR"
)

// SnapshotConfig configures snapshot fetching behavior.
type SnapshotConfig struct {
	// BaseURL is the base URL for snapshots. Defaults to ethpandaops.
	BaseURL string

	// CacheDir is where snapshots are cached locally.
	// Defaults to /var/lib/hive/snapshots.
	CacheDir string

	// Network is the Ethereum network (e.g., "mainnet", "sepolia", "holesky").
	Network string

	// Client is the execution client name (e.g., "geth", "nethermind", "besu", "reth", "erigon").
	Client string

	// BlockNumber is a specific block number to fetch. If empty, fetches "latest".
	BlockNumber string

	// HTTPClient is the HTTP client to use. If nil, uses http.DefaultClient.
	HTTPClient *http.Client
}

// DefaultSnapshotConfig returns a default snapshot configuration.
// The cache directory defaults to ./.hive/snapshots relative to the current working directory,
// but can be overridden via the HIVE_SNAPSHOT_DIR environment variable.
func DefaultSnapshotConfig() SnapshotConfig {
	cacheDir := os.Getenv(EnvSnapshotCacheDir)
	if cacheDir == "" {
		// Default to current working directory + .hive/snapshots
		cwd, err := os.Getwd()
		if err != nil {
			cwd = "."
		}
		cacheDir = filepath.Join(cwd, DefaultSnapshotCacheDirName)
	}
	return SnapshotConfig{
		BaseURL:  DefaultSnapshotBaseURL,
		CacheDir: cacheDir,
	}
}

// SnapshotMetadata contains information about a snapshot.
type SnapshotMetadata struct {
	Network     string `json:"network"`
	Client      string `json:"client"`
	BlockNumber string `json:"blockNumber"`
	BlockHash   string `json:"blockHash"`
	Timestamp   int64  `json:"timestamp"`
	LocalPath   string `json:"localPath"`
}

// SnapshotManager handles downloading and caching of snapshots.
type SnapshotManager struct {
	config SnapshotConfig
	client *http.Client
}

// NewSnapshotManager creates a new snapshot manager.
func NewSnapshotManager(config SnapshotConfig) *SnapshotManager {
	if config.BaseURL == "" {
		config.BaseURL = DefaultSnapshotBaseURL
	}
	if config.CacheDir == "" {
		config.CacheDir = "/var/lib/hive/snapshots"
	}

	client := config.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout: 0, // No timeout for large downloads.
		}
	}

	return &SnapshotManager{
		config: config,
		client: client,
	}
}

// EnsureSnapshot ensures a snapshot is available locally, downloading if needed.
// Returns the local path to the extracted snapshot directory.
func (m *SnapshotManager) EnsureSnapshot(ctx context.Context, network, client string) (string, error) {
	return m.EnsureSnapshotAt(ctx, network, client, "latest")
}

// EnsureSnapshotAt ensures a snapshot at a specific block is available locally.
// Returns the local path to the extracted snapshot directory.
func (m *SnapshotManager) EnsureSnapshotAt(ctx context.Context, network, client, blockNumber string) (string, error) {
	// Normalize inputs.
	network = strings.ToLower(network)
	client = strings.ToLower(client)

	// Build local cache path.
	snapshotDir := filepath.Join(m.config.CacheDir, network, client, blockNumber)
	extractedDir := filepath.Join(snapshotDir, "data")
	metadataPath := filepath.Join(snapshotDir, "metadata.json")

	// Check if snapshot already exists locally.
	if _, err := os.Stat(extractedDir); err == nil {
		// Verify metadata exists.
		if _, err := os.Stat(metadataPath); err == nil {
			return extractedDir, nil
		}
	}

	// Download and extract the snapshot.
	if err := m.downloadSnapshot(ctx, network, client, blockNumber, snapshotDir); err != nil {
		return "", err
	}

	return extractedDir, nil
}

// GetSnapshotPath returns the local path for a snapshot without downloading.
// Returns empty string if the snapshot is not cached locally.
func (m *SnapshotManager) GetSnapshotPath(network, client, blockNumber string) string {
	network = strings.ToLower(network)
	client = strings.ToLower(client)
	if blockNumber == "" {
		blockNumber = "latest"
	}

	extractedDir := filepath.Join(m.config.CacheDir, network, client, blockNumber, "data")
	if _, err := os.Stat(extractedDir); err == nil {
		return extractedDir
	}
	return ""
}

// downloadSnapshot downloads and extracts a snapshot.
func (m *SnapshotManager) downloadSnapshot(ctx context.Context, network, client, blockNumber, destDir string) error {
	// Build the snapshot URL.
	snapshotURL := fmt.Sprintf("%s/%s/%s/%s/%s",
		m.config.BaseURL, network, client, blockNumber, SnapshotFileName)

	fmt.Printf("Downloading snapshot from %s\n", snapshotURL)

	// Create destination directory.
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create snapshot directory: %w", err)
	}

	// Download the snapshot archive.
	archivePath := filepath.Join(destDir, SnapshotFileName)
	if err := m.downloadFile(ctx, snapshotURL, archivePath); err != nil {
		os.RemoveAll(destDir)
		return fmt.Errorf("failed to download snapshot: %w", err)
	}

	// Extract the snapshot.
	extractedDir := filepath.Join(destDir, "data")
	if err := os.MkdirAll(extractedDir, 0755); err != nil {
		os.RemoveAll(destDir)
		return fmt.Errorf("failed to create extraction directory: %w", err)
	}

	if err := m.extractTarZst(ctx, archivePath, extractedDir); err != nil {
		os.RemoveAll(destDir)
		return fmt.Errorf("failed to extract snapshot: %w", err)
	}

	// Download and save metadata.
	metadataURL := fmt.Sprintf("%s/%s/%s/%s/%s",
		m.config.BaseURL, network, client, blockNumber, SnapshotMetadataFile)
	metadata, err := m.fetchMetadata(ctx, metadataURL, network, client)
	if err != nil {
		// Metadata is optional, just log the error.
		fmt.Printf("Warning: could not fetch snapshot metadata: %v\n", err)
		metadata = &SnapshotMetadata{
			Network:     network,
			Client:      client,
			BlockNumber: blockNumber,
			LocalPath:   extractedDir,
		}
	} else {
		metadata.LocalPath = extractedDir
	}

	// Save metadata locally.
	metadataPath := filepath.Join(destDir, "metadata.json")
	if err := m.saveMetadata(metadata, metadataPath); err != nil {
		fmt.Printf("Warning: could not save metadata: %v\n", err)
	}

	// Optionally remove the archive to save space.
	os.Remove(archivePath)

	fmt.Printf("Snapshot extracted to %s\n", extractedDir)
	return nil
}

// downloadFile downloads a file from URL to the local path.
func (m *SnapshotManager) downloadFile(ctx context.Context, url, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// Create destination file.
	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Copy with progress reporting.
	written, err := io.Copy(out, &progressReader{
		reader: resp.Body,
		total:  resp.ContentLength,
	})
	if err != nil {
		return err
	}

	fmt.Printf("\nDownloaded %d bytes\n", written)
	return nil
}

// extractTarZst extracts a .tar.zst archive to the destination directory.
func (m *SnapshotManager) extractTarZst(ctx context.Context, archivePath, destDir string) error {
	// Try using zstd and tar commands (faster and more memory efficient).
	if m.extractWithZstdCommand(ctx, archivePath, destDir) == nil {
		return nil
	}

	// Fall back to pure Go implementation if commands not available.
	return m.extractWithGoZstd(archivePath, destDir)
}

// extractWithZstdCommand uses zstd and tar CLI tools.
func (m *SnapshotManager) extractWithZstdCommand(ctx context.Context, archivePath, destDir string) error {
	// Check if zstd is available.
	if _, err := exec.LookPath("zstd"); err != nil {
		return fmt.Errorf("zstd not found: %w", err)
	}

	fmt.Printf("Extracting snapshot with zstd...\n")

	// Use zstd to decompress and pipe to tar.
	cmd := exec.CommandContext(ctx, "sh", "-c",
		fmt.Sprintf("zstd -d -c %q | tar -xf - -C %q", archivePath, destDir))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// extractWithGoZstd extracts using pure Go (slower, for fallback).
func (m *SnapshotManager) extractWithGoZstd(archivePath, destDir string) error {
	// For tar.zst, we need the zstd library. Since it's a large dependency,
	// we'll require the zstd command to be installed.
	// If neither is available, suggest installing zstd.
	return fmt.Errorf("extraction requires 'zstd' command to be installed; run: apt-get install zstd")
}

// fetchMetadata fetches snapshot metadata from the remote server.
func (m *SnapshotManager) fetchMetadata(ctx context.Context, url, network, client string) (*SnapshotMetadata, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// Parse the eth_getBlockByNumber response.
	var blockResp struct {
		Result struct {
			Number    string `json:"number"`
			Hash      string `json:"hash"`
			Timestamp string `json:"timestamp"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&blockResp); err != nil {
		return nil, err
	}

	return &SnapshotMetadata{
		Network:     network,
		Client:      client,
		BlockNumber: blockResp.Result.Number,
		BlockHash:   blockResp.Result.Hash,
	}, nil
}

// saveMetadata saves snapshot metadata to a local file.
func (m *SnapshotManager) saveMetadata(metadata *SnapshotMetadata, path string) error {
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// progressReader wraps an io.Reader to report download progress.
type progressReader struct {
	reader     io.Reader
	total      int64
	downloaded int64
	lastReport time.Time
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	pr.downloaded += int64(n)

	// Report progress every second.
	if time.Since(pr.lastReport) > time.Second {
		pr.lastReport = time.Now()
		if pr.total > 0 {
			pct := float64(pr.downloaded) / float64(pr.total) * 100
			fmt.Printf("\rDownloading: %.1f%% (%d / %d MB)",
				pct, pr.downloaded/(1024*1024), pr.total/(1024*1024))
		} else {
			fmt.Printf("\rDownloading: %d MB", pr.downloaded/(1024*1024))
		}
	}
	return n, err
}

// FetchSnapshot is a convenience function to ensure a snapshot is available.
// It creates a default SnapshotManager and fetches the snapshot.
// Returns the local path to the extracted snapshot directory.
func FetchSnapshot(ctx context.Context, network, client string) (string, error) {
	mgr := NewSnapshotManager(DefaultSnapshotConfig())
	return mgr.EnsureSnapshot(ctx, network, client)
}

// FetchSnapshotWithConfig is like FetchSnapshot but allows custom configuration.
func FetchSnapshotWithConfig(ctx context.Context, config SnapshotConfig) (string, error) {
	if config.Network == "" {
		return "", fmt.Errorf("network is required")
	}
	if config.Client == "" {
		return "", fmt.Errorf("client is required")
	}

	mgr := NewSnapshotManager(config)
	blockNumber := config.BlockNumber
	if blockNumber == "" {
		blockNumber = "latest"
	}
	return mgr.EnsureSnapshotAt(ctx, config.Network, config.Client, blockNumber)
}

// SnapshotInfo contains information about available snapshots.
type SnapshotInfo struct {
	Network     string
	Client      string
	BlockNumber string
	URL         string
}

// ListAvailableNetworks returns commonly available networks on ethpandaops.
func ListAvailableNetworks() []string {
	return []string{
		"mainnet",
		"sepolia",
		"holesky",
		"hoodi",
	}
}

// ListAvailableClients returns commonly available clients on ethpandaops.
func ListAvailableClients() []string {
	return []string{
		"geth",
		"nethermind",
		"besu",
		"reth",
		"erigon",
	}
}

// GetSnapshotURL builds the URL for a snapshot.
func GetSnapshotURL(network, client, blockNumber string) string {
	if blockNumber == "" {
		blockNumber = "latest"
	}
	return fmt.Sprintf("%s/%s/%s/%s/%s",
		DefaultSnapshotBaseURL,
		strings.ToLower(network),
		strings.ToLower(client),
		blockNumber,
		SnapshotFileName,
	)
}

// Helper function to decompress gzip (for .tar.gz fallback).
func decompressGzip(src, dest string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	gzReader, err := gzip.NewReader(srcFile)
	if err != nil {
		return err
	}
	defer gzReader.Close()

	destFile, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, gzReader)
	return err
}
