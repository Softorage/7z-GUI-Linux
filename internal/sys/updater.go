package sys

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"

	"github.com/Softorage/7z-GUI-Linux/internal/version"
)

type GithubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Body    string `json:"body"`
}

var httpClient = &http.Client{
	Timeout: 10 * time.Second,
}

// FetchLatestRelease queries the GitHub API for the latest published release.
func FetchLatestRelease(parent context.Context) (*GithubRelease, error) {
	// Derive a bounded 10-second timeout context from the parent context
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/Softorage/7z-GUI-Linux/releases/latest", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create update request: %w", err)
	}

	req.Header.Set("User-Agent", "7GL-App/"+version.Version)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("network error during update check: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned unexpected status: %s", resp.Status)
	}

	var rel GithubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("failed to decode release payload: %w", err)
	}
	return &rel, nil
}

// CheckForUpdates performs a silent background update check on application startup.
func CheckForUpdates(w fyne.Window, a fyne.App, showDialog func(fyne.Window, fyne.App, GithubRelease)) {
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