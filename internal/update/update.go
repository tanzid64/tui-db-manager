package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const DefaultRepo = "tanzid64/tui-db-manager"

type Release struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
	Assets      []Asset   `json:"assets"`
}

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type Cache struct {
	CheckedAt       time.Time `json:"checked_at"`
	LatestVersion   string    `json:"latest_version"`
	UpdateAvailable bool      `json:"update_available"`
	ReleaseURL      string    `json:"release_url"`
}

// CleanVersion strips "v" prefix and trims whitespace.
func CleanVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	return v
}

// CompareVersions compares two semver strings (e.g. "1.0.3" vs "1.0.4").
// Returns 1 if v1 > v2, -1 if v1 < v2, and 0 if v1 == v2.
func CompareVersions(v1, v2 string) int {
	v1 = CleanVersion(v1)
	v2 = CleanVersion(v2)

	if v1 == v2 {
		return 0
	}
	// "dev" is treated as pre-release / older than any real release
	if v2 == "dev" {
		return 1
	}
	if v1 == "dev" {
		return -1
	}

	parts1 := strings.Split(v1, ".")
	parts2 := strings.Split(v2, ".")

	maxLen := len(parts1)
	if len(parts2) > maxLen {
		maxLen = len(parts2)
	}

	for i := 0; i < maxLen; i++ {
		var n1, n2 int
		if i < len(parts1) {
			cleanPart := strings.Split(parts1[i], "-")[0]
			n1, _ = strconv.Atoi(cleanPart)
		}
		if i < len(parts2) {
			cleanPart := strings.Split(parts2[i], "-")[0]
			n2, _ = strconv.Atoi(cleanPart)
		}
		if n1 > n2 {
			return 1
		}
		if n1 < n2 {
			return -1
		}
	}
	return 0
}

func IsNewer(latest, current string) bool {
	return CompareVersions(latest, current) > 0
}

// FetchLatestRelease retrieves the latest release metadata from GitHub.
func FetchLatestRelease(ctx context.Context, repo string) (*Release, error) {
	if repo == "" {
		repo = DefaultRepo
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "dbk-cli-updater")
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api returned status %d", resp.StatusCode)
	}

	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// CheckForUpdate queries GitHub directly to see if a newer release exists.
func CheckForUpdate(ctx context.Context, repo, currentVersion string) (*Release, bool, error) {
	rel, err := FetchLatestRelease(ctx, repo)
	if err != nil {
		return nil, false, err
	}
	isNewer := IsNewer(rel.TagName, currentVersion)
	return rel, isNewer, nil
}

// CheckCached checks a local cache file before making a network call, preventing rate-limiting.
func CheckCached(ctx context.Context, configDir, repo, currentVersion string) (*Release, bool, error) {
	cachePath := filepath.Join(configDir, ".update_cache.json")
	if data, err := os.ReadFile(cachePath); err == nil {
		var c Cache
		if err := json.Unmarshal(data, &c); err == nil {
			// If cached within the last 4 hours, return cached answer
			if time.Since(c.CheckedAt) < 4*time.Hour {
				rel := &Release{
					TagName: c.LatestVersion,
					HTMLURL: c.ReleaseURL,
				}
				return rel, c.UpdateAvailable, nil
			}
		}
	}

	// Otherwise, check live
	rel, isNewer, err := CheckForUpdate(ctx, repo, currentVersion)
	if err != nil {
		return nil, false, err
	}

	// Save to cache
	c := Cache{
		CheckedAt:       time.Now(),
		LatestVersion:   rel.TagName,
		UpdateAvailable: isNewer,
		ReleaseURL:      rel.HTMLURL,
	}
	if data, err := json.Marshal(c); err == nil {
		_ = os.WriteFile(cachePath, data, 0o600)
	}

	return rel, isNewer, nil
}

// ApplyUpdate downloads the release archive for the current OS/architecture,
// verifies the checksum, and atomically replaces the target executable.
func ApplyUpdate(ctx context.Context, rel *Release, targetPath string) error {
	if targetPath == "" {
		execPath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("detect executable path: %w", err)
		}
		execPath, err = filepath.EvalSymlinks(execPath)
		if err != nil {
			return fmt.Errorf("resolve symlink for executable: %w", err)
		}
		targetPath = execPath
	}

	goos := runtime.GOOS
	goarch := runtime.GOARCH

	cleanVer := CleanVersion(rel.TagName)
	archiveName := fmt.Sprintf("dbk_%s_%s_%s.tar.gz", cleanVer, goos, goarch)

	var archiveURL, checksumURL string
	for _, asset := range rel.Assets {
		if asset.Name == archiveName {
			archiveURL = asset.BrowserDownloadURL
		} else if asset.Name == "checksums.txt" {
			checksumURL = asset.BrowserDownloadURL
		}
	}

	if archiveURL == "" {
		return fmt.Errorf("release does not contain archive for %s/%s (%s)", goos, goarch, archiveName)
	}

	client := &http.Client{Timeout: 60 * time.Second}

	// 1. Download archive bytes
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, archiveURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "dbk-cli-updater")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download archive: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download archive returned status %d", resp.StatusCode)
	}

	archiveBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read archive: %w", err)
	}

	// 2. Verify checksum if checksums.txt is available
	if checksumURL != "" {
		cReq, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumURL, nil)
		if err == nil {
			cReq.Header.Set("User-Agent", "dbk-cli-updater")
			cResp, err := client.Do(cReq)
			if err == nil && cResp.StatusCode == http.StatusOK {
				cBytes, _ := io.ReadAll(cResp.Body)
				cResp.Body.Close()
				expectedHash := ParseChecksum(string(cBytes), archiveName)
				if expectedHash != "" {
					h := sha256.Sum256(archiveBytes)
					actualHash := hex.EncodeToString(h[:])
					if actualHash != expectedHash {
						return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHash, actualHash)
					}
				}
			}
		}
	}

	// 3. Extract binary from tar.gz
	gzr, err := gzip.NewReader(bytes.NewReader(archiveBytes))
	if err != nil {
		return fmt.Errorf("decompress gzip: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	var binaryBytes []byte
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}
		if hdr.Name == "dbk" || filepath.Base(hdr.Name) == "dbk" {
			binaryBytes, err = io.ReadAll(tr)
			if err != nil {
				return fmt.Errorf("extract binary from archive: %w", err)
			}
			break
		}
	}

	if len(binaryBytes) == 0 {
		return errors.New("dbk binary not found inside release archive")
	}

	// 4. Atomically replace targetPath
	targetDir := filepath.Dir(targetPath)
	tmpFile, err := os.CreateTemp(targetDir, ".dbk-update-*")
	if err != nil {
		return fmt.Errorf("cannot write to %s (permission denied): %w\nTip: Run with sudo or reinstall via: curl -fsSL https://raw.githubusercontent.com/%s/main/install.sh | sh", targetDir, err, DefaultRepo)
	}
	tmpName := tmpFile.Name()
	defer os.Remove(tmpName)

	if _, err := tmpFile.Write(binaryBytes); err != nil {
		tmpFile.Close()
		return fmt.Errorf("write binary: %w", err)
	}
	if err := tmpFile.Chmod(0o755); err != nil {
		tmpFile.Close()
		return fmt.Errorf("chmod binary: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmpName, targetPath); err != nil {
		return fmt.Errorf("replace executable %s: %w", targetPath, err)
	}

	return nil
}

func ParseChecksum(checksumsText, filename string) string {
	lines := strings.Split(checksumsText, "\n")
	for _, l := range lines {
		fields := strings.Fields(l)
		if len(fields) >= 2 && fields[1] == filename {
			return strings.TrimSpace(fields[0])
		}
	}
	return ""
}
