package sys

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"

	"github.com/Softorage/7z-GUI-Linux/internal/version"
)

// TODO: allow disabling update check (after config file is there)

type GithubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Body    string `json:"body"`
}

// CheckForUpdates fetches the latest release from GitHub API in the background
func CheckForUpdates(w fyne.Window, a fyne.App, showDialog func(fyne.Window, fyne.App, GithubRelease)) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/Softorage/7z-GUI-Linux/releases/latest", nil)
	if err != nil {
		return
	}

	req.Header.Set("User-Agent", "7GL-App/"+version.Version)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return
	}

	var rel GithubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return
	}

	if IsNewerVersion(rel.TagName, version.Version) {
		fyne.Do(func() {
			if showDialog != nil {
				showDialog(w, a, rel)
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