package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// TODO: allow disabling update check (after config file is there)

// version is passed at build time
var version string = "dev"

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Body    string `json:"body"`
}

// checkForUpdates fetches the latest release from GitHub API in the background
func checkForUpdates(w fyne.Window, a fyne.App) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/Softorage/7z-GUI-Linux/releases/latest", nil)
	if err != nil {
		return
	}

	req.Header.Set("User-Agent", "7GL-App/"+version)
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

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return
	}

	if isNewerVersion(rel.TagName, version) {
		fyne.Do(func() {
			showUpdateDialog(w, a, rel)
		})
	}
}

// isNewerVersion compares semver strings (e.g. "v1.2.0" vs "1.1.9")
func isNewerVersion(latest, current string) bool {
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

func showUpdateDialog(w fyne.Window, a fyne.App, rel githubRelease) {
	relURL, err := url.Parse(rel.HTMLURL)
	if err != nil {
		relURL, _ = url.Parse("https://github.com/Softorage/7z-GUI-Linux/releases/latest")
	}

	heading := widget.NewLabelWithStyle("New Update Available!", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	verInfo := widget.NewLabel(fmt.Sprintf("Installed: %s  ➜  Latest: %s", version, rel.TagName))
	verInfo.Alignment = fyne.TextAlignCenter

	contentObjects := []fyne.CanvasObject{heading, verInfo}

	if notes := strings.TrimSpace(rel.Body); notes != "" {
		notesHeader := widget.NewLabelWithStyle("Release Notes:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})

		//notesText := widget.NewRichTextFromMarkdown(notes)
		notesText := widget.NewRichText(&widget.TextSegment{
			Text: notes,
			Style: widget.RichTextStyle{
				ColorName: theme.ColorNamePlaceHolder, // e.g., ColorNamePrimary, ColorNamePlaceHolder, ColorNameWarning, ColorNameError
			},
		})
		notesText.Wrapping = fyne.TextWrapWord

		notesBox := container.NewGridWrap(fyne.NewSize(440, 160), container.NewPadded(container.NewVScroll(notesText)))
		contentObjects = append(contentObjects, widget.NewSeparator(), notesHeader, notesBox)
	}

	dialog.ShowCustomConfirm(
		"Software Update",
		"Download Update ↗",
		"Later",
		container.NewVBox(contentObjects...),
		func(confirm bool) {
			if confirm {
				a.OpenURL(relURL)
			}
		},
		w,
	)
}
