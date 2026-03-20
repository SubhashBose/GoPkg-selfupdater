// Package selfupdate provides binary self-update from GitHub releases.
//
// Usage:
//
//	cfg := selfupdate.Config{
//	    RepoURL:      "https://github.com/SubhashBose/RouteMUX",
//	    BinaryPrefix: "routemux-",
//	    OSSep:        "-",
//	    CurrentVersion: version, // e.g. "1.0.0"
//	}
//	updated, err := selfupdate.Update(cfg)
package selfupdate

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Config holds all parameters needed to perform a self-update.
type Config struct {
	// RepoURL is the GitHub repository URL, e.g.
	// "https://github.com/SubhashBose/RouteMUX"
	RepoURL string

	// BinaryPrefix is the prefix used in release asset names, e.g. "routemux-".
	BinaryPrefix string

	// OSSep is the separator between OS and arch in asset names, e.g. "-".
	// With prefix "routemux-" and sep "-", assets are: routemux-linux-amd64
	OSSep string

	// CurrentVersion is the running binary's version string WITHOUT the leading
	// "v", e.g. "1.0.0". It is compared against the release tag (stripped of
	// "v" if present).
	CurrentVersion string

	// HTTPClient allows injecting a custom http.Client. Defaults to
	// http.DefaultClient when nil.
	HTTPClient *http.Client
}

// Result carries information about an update attempt.
type Result struct {
	// Updated is true when the binary was replaced on disk.
	Updated bool
	// LatestVersion is the newest version found on GitHub (without leading "v").
	LatestVersion string
	// AssetName is the release asset that was downloaded, if any.
	AssetName string
}

// githubRelease is the subset of the GitHub releases API we need.
type githubRelease struct {
	TagName string          `json:"tag_name"`
	Assets  []githubAsset   `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Update checks GitHub for a newer release and, if one exists, downloads and
// replaces the running binary. It returns a Result and any error encountered.
//
// The caller should restart the process after a successful update.
func Update(cfg Config) (Result, error) {
	if err := cfg.validate(); err != nil {
		return Result{}, fmt.Errorf("selfupdate: invalid config: %w", err)
	}

	client := cfg.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	// ── 1. Fetch latest release from GitHub API ───────────────────────────────
	release, err := fetchLatestRelease(client, cfg.RepoURL)
	if err != nil {
		return Result{}, fmt.Errorf("selfupdate: fetch release: %w", err)
	}

	latestVer := strings.TrimPrefix(release.TagName, "v")
	res := Result{LatestVersion: latestVer}

	// ── 2. Compare versions ───────────────────────────────────────────────────
	if latestVer == cfg.CurrentVersion {
		return res, nil // already up to date
	}
	if !isNewer(latestVer, cfg.CurrentVersion) {
		return res, nil // release is older or equal — nothing to do
	}

	// ── 3. Detect OS / arch and find the matching asset ───────────────────────
	assetName := cfg.assetName()
	downloadURL := ""
	for _, a := range release.Assets {
		if strings.EqualFold(a.Name, assetName) {
			downloadURL = a.BrowserDownloadURL
			break
		}
	}
	if downloadURL == "" {
		return res, fmt.Errorf(
			"selfupdate: no asset named %q found in release %s", assetName, release.TagName)
	}
	res.AssetName = assetName

	// ── 4. Download to a temp file beside the current binary ─────────────────
	execPath, err := os.Executable()
	if err != nil {
		return res, fmt.Errorf("selfupdate: locate executable: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return res, fmt.Errorf("selfupdate: eval symlinks: %w", err)
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(execPath), ".selfupdate-*")
	if err != nil {
		return res, fmt.Errorf("selfupdate: create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		tmpFile.Close()
		os.Remove(tmpPath) // no-op if rename succeeded
	}()

	if err := downloadTo(client, downloadURL, tmpFile); err != nil {
		return res, fmt.Errorf("selfupdate: download asset: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return res, fmt.Errorf("selfupdate: close temp file: %w", err)
	}

	// ── 5. Make executable and atomically replace the running binary ──────────
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return res, fmt.Errorf("selfupdate: chmod: %w", err)
	}
	if err := os.Rename(tmpPath, execPath); err != nil {
		return res, fmt.Errorf("selfupdate: replace binary: %w", err)
	}

	res.Updated = true
	return res, nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func (cfg Config) validate() error {
	if cfg.RepoURL == "" {
		return fmt.Errorf("RepoURL is required")
	}
	if cfg.BinaryPrefix == "" {
		return fmt.Errorf("BinaryPrefix is required")
	}
	if cfg.CurrentVersion == "" {
		return fmt.Errorf("CurrentVersion is required")
	}
	if cfg.OSSep == "" {
		return fmt.Errorf("OSSep is required")
	}
	return nil
}

// assetName builds the expected release asset filename for the current platform.
//
// Pattern: {BinaryPrefix}{GOOS}{OSSep}{GOARCH}[.exe on Windows]
//
// Examples with prefix="routemux-" sep="-":
//
//	linux/amd64  → routemux-linux-amd64
//	darwin/arm64 → routemux-darwin-arm64
//	windows/amd64 → routemux-windows-amd64.exe
func (cfg Config) assetName() string {
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	name := cfg.BinaryPrefix + goos + cfg.OSSep + goarch
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// fetchLatestRelease calls the GitHub releases/latest API.
func fetchLatestRelease(client *http.Client, repoURL string) (*githubRelease, error) {
	// Convert https://github.com/owner/repo → https://api.github.com/repos/owner/repo/releases/latest
	repoURL = strings.TrimSuffix(repoURL, "/")
	const ghBase = "https://github.com/"
	if !strings.HasPrefix(repoURL, ghBase) {
		return nil, fmt.Errorf("RepoURL must start with %s", ghBase)
	}
	ownerRepo := strings.TrimPrefix(repoURL, ghBase)
	apiURL := "https://api.github.com/repos/" + ownerRepo + "/releases/latest"

	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %s for %s", resp.Status, apiURL)
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &rel, nil
}

// downloadTo streams the content at url into dst.
func downloadTo(client *http.Client, url string, dst io.Writer) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %s downloading %s", resp.Status, url)
	}
	_, err = io.Copy(dst, resp.Body)
	return err
}

// isNewer returns true if latest > current using simple dotted-integer
// comparison (e.g. "1.2.0" > "1.1.9"). Falls back to string inequality so
// non-semver tags still trigger an update rather than silently skipping.
// isNewer returns true if latest > current.
// Handles 1, 1.2, and 1.2.3 style versions.
func isNewer(latest, current string) bool {
	lp := parseSemver(latest)
	cp := parseSemver(current)
	if lp == nil || cp == nil {
		// couldn't parse — don't update to avoid false positives
		return false
	}
	for i := range lp {
		if lp[i] > cp[i] {
			return true
		}
		if lp[i] < cp[i] {
			return false
		}
	}
	return false
}

// parseSemver parses "1", "1.2", or "1.2.3" into a 3-element slice.
func parseSemver(v string) []int {
	parts := strings.Split(v, ".")
	if len(parts) > 3 {
		return nil
	}
	// Pad to always have 3 parts: "1.1" → [1, 1, 0]
	for len(parts) < 3 {
		parts = append(parts, "0")
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n := 0
		for _, c := range p {
			if c < '0' || c > '9' {
				return nil
			}
			n = n*10 + int(c-'0')
		}
		nums[i] = n
	}
	return nums
}