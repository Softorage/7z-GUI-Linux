package sys

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"

	"github.com/Softorage/7z-GUI-Linux/internal/version"
)

type GithubAsset struct {
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type GithubRelease struct {
	TagName string        `json:"tag_name"`
	HTMLURL string        `json:"html_url"`
	Body    string        `json:"body"`
	Assets  []GithubAsset `json:"assets"`
}

var httpClient = &http.Client{
	Timeout: 10 * time.Second,
	Transport: &http.Transport{
		DisableKeepAlives: true,
		Proxy:             http.ProxyFromEnvironment,
	},
}

var downloadClient = &http.Client{
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DisableKeepAlives:     false,
		MaxIdleConns:          5,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	},
}

// FindMatchingAsset resolves the release package matching the display server and architecture.
func FindMatchingAsset(rel *GithubRelease, display, arch string) *GithubAsset {
	if rel == nil || len(rel.Assets) == 0 {
		return nil
	}

	dispLower := strings.ToLower(strings.TrimSpace(display))
	archLower := strings.ToLower(strings.TrimSpace(arch))

	// Primary target format: 7z-GUI-Linux-<display>-<arch>.tar.xz
	canonical := fmt.Sprintf("7z-gui-linux-%s-%s.tar.xz", dispLower, archLower)
	for i := range rel.Assets {
		if strings.ToLower(rel.Assets[i].Name) == canonical {
			return &rel.Assets[i]
		}
	}

	// Segmented fallback: matches any archive containing "-<display>-<arch>."
	token := fmt.Sprintf("-%s-%s.", dispLower, archLower)
	for i := range rel.Assets {
		nameLower := strings.ToLower(rel.Assets[i].Name)
		if strings.Contains(nameLower, token) {
			return &rel.Assets[i]
		}
	}

	return nil
}

// ProgressReader wraps an io.Reader and emits rate-throttled progress updates to the UI.
type ProgressReader struct {
	reader     io.Reader
	total      int64
	current    int64
	lastTime   time.Time
	lastRatio  float64
	onProgress func(ratio float64, currentBytes, totalBytes int64)
}

func NewProgressReader(r io.Reader, total int64, onProgress func(float64, int64, int64)) *ProgressReader {
	return &ProgressReader{
		reader:     r,
		total:      total,
		onProgress: onProgress,
	}
}

func (pr *ProgressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	if n > 0 {
		pr.current += int64(n)
		var ratio float64
		if pr.total > 0 {
			ratio = float64(pr.current) / float64(pr.total)
			if ratio > 1.0 {
				ratio = 1.0
			}
		}

		now := time.Now()
		// Throttle GUI dispatches: >= 0.5% advance, >= 50ms interval, or terminal EOF
		isTerminal := err == io.EOF || (pr.total > 0 && pr.current >= pr.total)
		if (ratio-pr.lastRatio >= 0.005) || now.Sub(pr.lastTime) >= 50*time.Millisecond || isTerminal {
			pr.lastRatio = ratio
			pr.lastTime = now
			if pr.onProgress != nil {
				cur, tot, r := pr.current, pr.total, ratio
				cb := pr.onProgress
				fyne.Do(func() {
					cb(r, cur, tot)
				})
			}
		}
	}
	return n, err
}

// DownloadAsset streams a release asset directly to destPath using downloadClient.
func DownloadAsset(ctx context.Context, asset *GithubAsset, destPath string, onProgress func(ratio float64, currentBytes, totalBytes int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.BrowserDownloadURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create download request: %w", err)
	}
	req.Header.Set("User-Agent", "7GL-App/"+version.Version)
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := downloadClient.Do(req)
	if err != nil {
		return fmt.Errorf("network error downloading asset: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned unexpected status %s", resp.Status)
	}

	totalSize := asset.Size
	if totalSize <= 0 && resp.ContentLength > 0 {
		totalSize = resp.ContentLength
	}

	outFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to open output file: %w", err)
	}
	defer outFile.Close()

	buf := make([]byte, 64*1024)
	progressR := NewProgressReader(resp.Body, totalSize, onProgress)
	if _, err := io.CopyBuffer(outFile, progressR, buf); err != nil {
		_ = os.Remove(destPath)
		return fmt.Errorf("stream error during download: %w", err)
	}

	return nil
}

// FetchLatestRelease queries the GitHub API for the latest published release.
func FetchLatestRelease(parent context.Context) (*GithubRelease, error) {
	defer httpClient.CloseIdleConnections()

	// Derive a bounded 10-second timeout context from the parent context
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/Softorage/7z-GUI-Linux/releases/latest", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create update request: %w", err)
	}

	req.Header.Set("User-Agent", "7GL-App/"+version.Version)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Close = true

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("network error during update check: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned unexpected status: %s", resp.Status)
	}

	// Bounded to 2 MB to safely accommodate releases with many architecture assets
	limitedReader := io.LimitReader(resp.Body, 2*1024*1024)
	var rel GithubRelease
	if err := json.NewDecoder(limitedReader).Decode(&rel); err != nil {
		return nil, fmt.Errorf("failed to decode release payload: %w", err)
	}
	return &rel, nil
}

// CheckForUpdates performs a silent background update check on application startup.
func CheckForUpdates(w fyne.Window, a fyne.App, showDialog func(fyne.Window, fyne.App, GithubRelease)) {
	// Defer startup update check by 3 seconds to prevent network/TLS allocation
	// spikes while the application window, OpenGL context, and initial directory render settle.
	time.Sleep(2 * time.Second)

	rel, err := FetchLatestRelease(context.Background())
	if err != nil || rel == nil {
		return
	}

	if IsNewerVersion(rel.TagName, version.Version) {
		fyne.Do(func() {
			if showDialog != nil {
				showDialog(w, a, *rel)
			}
		})
	}
}

// CheckForUpdatesManual performs an interactive update check with user callbacks for success, up-to-date, and errors.
func CheckForUpdatesManual(w fyne.Window, a fyne.App, showDialog func(fyne.Window, fyne.App, GithubRelease), onUpToDate func(), onError func(error)) {
	rel, err := FetchLatestRelease(context.Background())
	if err != nil {
		fyne.Do(func() {
			if onError != nil {
				onError(err)
			}
		})
		return
	}

	if IsNewerVersion(rel.TagName, version.Version) {
		fyne.Do(func() {
			if showDialog != nil {
				showDialog(w, a, *rel)
			}
		})
	} else {
		fyne.Do(func() {
			if onUpToDate != nil {
				onUpToDate()
			}
		})
	}
}

// IsNewerVersion compares semver strings (e.g. "v1.2.0" vs "1.1.9")
func IsNewerVersion(latest, current string) bool {
	latestClean := strings.TrimPrefix(strings.TrimSpace(latest), "v")
	latestClean = strings.TrimPrefix(latestClean, "V")
	currentClean := strings.TrimPrefix(strings.TrimSpace(current), "v")
	currentClean = strings.TrimPrefix(currentClean, "V")

	// Split prerelease/metadata suffixes if present
	latestMain := strings.Split(latestClean, "-")[0]
	currentMain := strings.Split(currentClean, "-")[0]

	lParts := strings.Split(latestMain, ".")
	cParts := strings.Split(currentMain, ".")

	maxLen := len(lParts)
	if len(cParts) > maxLen {
		maxLen = len(cParts)
	}

	for i := 0; i < maxLen; i++ {
		var lNum, cNum int
		if i < len(lParts) {
			lNum, _ = strconv.Atoi(lParts[i])
		}
		if i < len(cParts) {
			cNum, _ = strconv.Atoi(cParts[i])
		}

		if lNum > cNum {
			return true
		} else if lNum < cNum {
			return false
		}
	}
	return false
}
