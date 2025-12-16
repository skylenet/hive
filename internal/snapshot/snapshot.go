// Package snapshot provides functionality to fetch and cache Ethereum client snapshots.
package snapshot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DefaultBaseURL is the default base URL for ethpandaops snapshots.
	DefaultBaseURL = "https://snapshots.ethpandaops.io"

	// SnapshotFileName is the name of the snapshot archive file.
	SnapshotFileName = "snapshot.tar.zst"

	// DefaultCacheDirName is the default directory name for snapshot cache.
	DefaultCacheDirName = ".hive/snapshots"

	// EnvSnapshotDir is the environment variable to override the snapshot cache directory.
	EnvSnapshotDir = "HIVE_SNAPSHOT_DIR"
)

// Config specifies snapshot fetching configuration.
type Config struct {
	// Network is the Ethereum network (e.g., "mainnet", "sepolia", "hoodi").
	Network string

	// Client is the execution client name (e.g., "geth", "nethermind").
	Client string

	// BlockNumber is a specific block number. Defaults to "latest".
	BlockNumber string

	// BaseURL overrides the default ethpandaops URL.
	BaseURL string

	// CacheDir overrides the default cache directory.
	CacheDir string
}

// Fetcher downloads and caches snapshots on the host.
type Fetcher struct {
	cacheDir string
	baseURL  string
	client   *http.Client
	log      *slog.Logger
}

// NewFetcher creates a new snapshot fetcher.
func NewFetcher(log *slog.Logger) *Fetcher {
	cacheDir := os.Getenv(EnvSnapshotDir)
	if cacheDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			cwd = "."
		}
		cacheDir = filepath.Join(cwd, DefaultCacheDirName)
	}

	if log == nil {
		log = slog.Default()
	}

	return &Fetcher{
		cacheDir: cacheDir,
		baseURL:  DefaultBaseURL,
		client:   &http.Client{Timeout: 0}, // No timeout for large downloads
		log:      log,
	}
}

// EnsureSnapshot ensures a snapshot is available locally, downloading if needed.
// Returns the local path to the extracted snapshot directory.
func (f *Fetcher) EnsureSnapshot(ctx context.Context, cfg Config) (string, error) {
	// Normalize inputs
	network := strings.ToLower(cfg.Network)
	client := strings.ToLower(mapClientName(cfg.Client))
	blockNumber := cfg.BlockNumber
	if blockNumber == "" {
		blockNumber = "latest"
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = f.baseURL
	}

	cacheDir := cfg.CacheDir
	if cacheDir == "" {
		cacheDir = f.cacheDir
	}

	// Resolve "latest" to actual block number
	if blockNumber == "latest" {
		resolved, err := f.resolveLatestBlock(ctx, baseURL, network, client)
		if err != nil {
			return "", fmt.Errorf("failed to resolve latest block: %w", err)
		}
		f.log.Info("Resolved latest snapshot block",
			"network", network,
			"client", client,
			"block", resolved)
		blockNumber = resolved
	}

	// Build local cache path
	snapshotDir := filepath.Join(cacheDir, network, client, blockNumber)
	extractedDir := filepath.Join(snapshotDir, "data")
	markerPath := filepath.Join(snapshotDir, ".complete")

	// Check if snapshot already exists and is complete
	if _, err := os.Stat(markerPath); err == nil {
		f.log.Info("Using cached snapshot",
			"network", network,
			"client", client,
			"block", blockNumber,
			"path", extractedDir)
		return extractedDir, nil
	}

	// Download and extract the snapshot
	if err := f.downloadSnapshot(ctx, baseURL, network, client, blockNumber, snapshotDir); err != nil {
		return "", err
	}

	return extractedDir, nil
}

// resolveLatestBlock fetches the actual block number for "latest" from the snapshot server.
func (f *Fetcher) resolveLatestBlock(ctx context.Context, baseURL, network, client string) (string, error) {
	latestURL := fmt.Sprintf("%s/%s/%s/latest", baseURL, network, client)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestURL, nil)
	if err != nil {
		return "", err
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to resolve latest: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return "", err
	}

	blockNumber := strings.TrimSpace(string(body))
	if blockNumber == "" {
		return "", fmt.Errorf("empty response from latest endpoint")
	}

	return blockNumber, nil
}

// downloadSnapshot downloads and extracts a snapshot.
func (f *Fetcher) downloadSnapshot(ctx context.Context, baseURL, network, client, blockNumber, destDir string) error {
	// Build the snapshot URL
	snapshotURL := fmt.Sprintf("%s/%s/%s/%s/%s",
		baseURL, network, client, blockNumber, SnapshotFileName)

	// Create destination directory
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create snapshot directory: %w", err)
	}

	archivePath := filepath.Join(destDir, SnapshotFileName)

	// Check if we have a partial or complete download
	archiveInfo, archiveErr := os.Stat(archivePath)
	if archiveErr == nil && archiveInfo.Size() > 0 {
		f.log.Info("Found existing archive file",
			"path", archivePath,
			"size", fmt.Sprintf("%d MB", archiveInfo.Size()/(1024*1024)))
	} else {
		f.log.Info("Downloading snapshot",
			"url", snapshotURL,
			"destination", destDir)
	}

	// Download the snapshot archive (supports resume)
	if err := f.downloadFile(ctx, snapshotURL, archivePath); err != nil {
		// Don't delete partial downloads - they can be resumed
		return fmt.Errorf("failed to download snapshot (partial file preserved for resume): %w", err)
	}

	// Extract the snapshot
	extractedDir := filepath.Join(destDir, "data")
	if err := os.MkdirAll(extractedDir, 0755); err != nil {
		return fmt.Errorf("failed to create extraction directory: %w", err)
	}

	f.log.Info("Extracting snapshot...", "archive", archivePath)

	if err := f.extractTarZst(ctx, archivePath, extractedDir); err != nil {
		// Extraction failed - clean up extracted dir but keep archive for retry
		os.RemoveAll(extractedDir)
		return fmt.Errorf("failed to extract snapshot (archive preserved): %w", err)
	}

	// Remove the archive to save space
	os.Remove(archivePath)

	// Write completion marker
	markerPath := filepath.Join(destDir, ".complete")
	if err := os.WriteFile(markerPath, []byte(time.Now().Format(time.RFC3339)), 0644); err != nil {
		f.log.Warn("Failed to write completion marker", "error", err)
	}

	f.log.Info("Snapshot extracted successfully",
		"path", extractedDir)

	return nil
}

// downloadFile downloads a file from URL to the local path with resume support.
func (f *Fetcher) downloadFile(ctx context.Context, url, destPath string) error {
	// Check if partial file exists for resume
	var existingSize int64
	if stat, err := os.Stat(destPath); err == nil {
		existingSize = stat.Size()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	// Request resume from existing position if we have a partial file
	if existingSize > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", existingSize))
		f.log.Info("Resuming download", "from", fmt.Sprintf("%d MB", existingSize/(1024*1024)))
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var out *os.File
	var totalSize int64

	switch resp.StatusCode {
	case http.StatusOK:
		// Server doesn't support range or sent full file - start fresh
		if existingSize > 0 {
			f.log.Info("Server sent full file, starting fresh download")
		}
		out, err = os.Create(destPath)
		if err != nil {
			return err
		}
		totalSize = resp.ContentLength

	case http.StatusPartialContent:
		// Server supports resume - append to existing file
		out, err = os.OpenFile(destPath, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		// Total size is existing + remaining
		totalSize = existingSize + resp.ContentLength
		f.log.Info("Resume accepted",
			"existing", fmt.Sprintf("%d MB", existingSize/(1024*1024)),
			"remaining", fmt.Sprintf("%d MB", resp.ContentLength/(1024*1024)),
			"total", fmt.Sprintf("%d MB", totalSize/(1024*1024)))

	case http.StatusRequestedRangeNotSatisfiable:
		// File is already complete or server confused - try fresh download
		f.log.Info("Range not satisfiable, starting fresh download")
		os.Remove(destPath)
		return f.downloadFile(ctx, url, destPath)

	default:
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}
	defer out.Close()

	// Copy with progress reporting
	written, err := io.Copy(out, &progressReader{
		reader:     resp.Body,
		total:      totalSize,
		downloaded: existingSize, // Start progress from existing size
		log:        f.log,
	})
	if err != nil {
		return err
	}

	f.log.Info("Download complete", "bytes", existingSize+written)
	return nil
}

// extractTarZst extracts a .tar.zst archive to the destination directory.
func (f *Fetcher) extractTarZst(ctx context.Context, archivePath, destDir string) error {
	// Check if zstd is available
	if _, err := exec.LookPath("zstd"); err != nil {
		return fmt.Errorf("zstd not found: please install zstd (apt-get install zstd)")
	}

	f.log.Info("Extracting snapshot...")

	// Use zstd to decompress and pipe to tar
	cmd := exec.CommandContext(ctx, "sh", "-c",
		fmt.Sprintf("zstd -d -c %q | tar -xf - -C %q", archivePath, destDir))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// GetCachedSnapshotPath returns the path if a snapshot is already cached, empty string otherwise.
func (f *Fetcher) GetCachedSnapshotPath(network, client, blockNumber string) string {
	if blockNumber == "" {
		blockNumber = "latest"
	}
	network = strings.ToLower(network)
	client = strings.ToLower(mapClientName(client))

	extractedDir := filepath.Join(f.cacheDir, network, client, blockNumber, "data")
	markerPath := filepath.Join(f.cacheDir, network, client, blockNumber, ".complete")

	if _, err := os.Stat(markerPath); err == nil {
		return extractedDir
	}
	return ""
}

// mapClientName maps hive client names to ethpandaops snapshot client names.
// It handles client names with nametags (e.g., "nethermind_default" -> "nethermind").
func mapClientName(hiveName string) string {
	// Strip nametag suffix if present (e.g., "nethermind_default" -> "nethermind")
	baseName := hiveName
	if idx := strings.Index(hiveName, "_"); idx > 0 {
		baseName = hiveName[:idx]
	}

	mapping := map[string]string{
		"go-ethereum": "geth",
		"nethermind":  "nethermind",
		"besu":        "besu",
		"reth":        "reth",
		"erigon":      "erigon",
	}
	if mapped, ok := mapping[strings.ToLower(baseName)]; ok {
		return mapped
	}
	return baseName
}

// progressReader wraps an io.Reader to report download progress.
type progressReader struct {
	reader     io.Reader
	total      int64
	downloaded int64
	lastReport time.Time
	log        *slog.Logger
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	pr.downloaded += int64(n)

	// Report progress every 5 seconds
	if time.Since(pr.lastReport) > 5*time.Second {
		pr.lastReport = time.Now()
		if pr.total > 0 {
			pct := float64(pr.downloaded) / float64(pr.total) * 100
			pr.log.Info("Download progress",
				"percent", fmt.Sprintf("%.1f%%", pct),
				"downloaded", fmt.Sprintf("%d MB", pr.downloaded/(1024*1024)),
				"total", fmt.Sprintf("%d MB", pr.total/(1024*1024)))
		} else {
			pr.log.Info("Download progress",
				"downloaded", fmt.Sprintf("%d MB", pr.downloaded/(1024*1024)))
		}
	}
	return n, err
}

// Metadata contains information about a cached snapshot.
type Metadata struct {
	Network     string `json:"network"`
	Client      string `json:"client"`
	BlockNumber string `json:"blockNumber"`
	LocalPath   string `json:"localPath"`
	FetchedAt   string `json:"fetchedAt"`
}

// SaveMetadata saves snapshot metadata to the cache directory.
func (f *Fetcher) SaveMetadata(snapshotDir string, meta *Metadata) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(snapshotDir, "metadata.json"), data, 0644)
}
