package components

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	appstate "github.com/Softorage/7z-GUI-Linux/internal/app"
	"github.com/Softorage/7z-GUI-Linux/internal/engine"
	"github.com/Softorage/7z-GUI-Linux/internal/sys"
	"github.com/Softorage/7z-GUI-Linux/internal/version"
)

func ShowUpdateDialog(w fyne.Window, a fyne.App, rel sys.GithubRelease) {
	relURL, err := url.Parse(rel.HTMLURL)
	if err != nil {
		relURL, _ = url.Parse("https://github.com/Softorage/7z-GUI-Linux/releases/latest")
	}

	display := engine.DetectDisplayServer()
	arch := engine.DetectArch()
	matchingAsset := sys.FindMatchingAsset(&rel, display, arch)

	heading := widget.NewLabelWithStyle("New Update Available!", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	verInfo := widget.NewLabel(fmt.Sprintf("Installed: %s  ➜  Latest: %s", version.Version, rel.TagName))
	verInfo.Alignment = fyne.TextAlignCenter
	platformInfo := widget.NewLabel(fmt.Sprintf("Detected Platform: %s (%s)", display, arch))
	platformInfo.Alignment = fyne.TextAlignCenter

	contentObjects := []fyne.CanvasObject{heading, verInfo, platformInfo}

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

	// Read current startup preference
	appstate.UserConfigMu.RLock()
	startupCheckEnabled := appstate.UserConfig.Updates.CheckOnStartup
	appstate.UserConfigMu.RUnlock()

	// Preference toggle directly inside the dialog
	disableStartupCheck := widget.NewCheck("Do not check for updates on startup", func(checked bool) {
		appstate.UserConfigMu.Lock()
		appstate.UserConfig.Updates.CheckOnStartup = !checked
		appstate.UserConfigMu.Unlock()

		if err := appstate.SaveConfig(); err != nil {
			appstate.SetInfo("Warning: Failed to persist update preference.")
		} else if checked {
			appstate.SetInfo("Startup update check disabled. You can re-enable it in Settings.")
		} else {
			appstate.SetInfo("Startup update check enabled.")
		}
	})
	disableStartupCheck.SetChecked(!startupCheckEnabled)

	settingsHint := widget.NewRichText(&widget.TextSegment{
		Text: "You can also check for updates manually or re-enable this in Settings.",
		Style: widget.RichTextStyle{
			SizeName:  theme.SizeNameCaptionText,
			ColorName: theme.ColorNamePlaceHolder,
		},
	})

	contentObjects = append(contentObjects, widget.NewSeparator(), disableStartupCheck, settingsHint)

	var d dialog.Dialog
	rootContainer := container.NewStack()

	var updateBtn *widget.Button
	updateBtn = widget.NewButtonWithIcon("Update Now", theme.DownloadIcon(), func() {
		if matchingAsset == nil {
			dialog.ShowInformation("No Direct Binary", fmt.Sprintf("No prebuilt archive found for %s-%s. Opening release page in browser.", display, arch), w)
			a.OpenURL(relURL)
			if d != nil {
				d.Hide()
			}
			return
		}

		// Switch dialog to interactive progress mode
		ctx, cancel := context.WithCancel(context.Background())

		statusLabel := widget.NewLabel("Preparing workspace...")
		statusLabel.Wrapping = fyne.TextWrapWord
		assetInfoLabel := widget.NewLabel(fmt.Sprintf("Package: %s (%s)", matchingAsset.Name, sys.FormatSize(matchingAsset.Size)))
		assetInfoLabel.TextStyle = fyne.TextStyle{Bold: true}

		progressBar := widget.NewProgressBar()
		progressBar.Min = 0.0
		progressBar.Max = 1.0
		progressBar.SetValue(0.0)

		cancelDownloadBtn := widget.NewButtonWithIcon("Cancel", theme.CancelIcon(), func() {
			cancel()
			if d != nil {
				d.Hide()
			}
		})
		cancelDownloadBtn.Importance = widget.LowImportance

		progressBox := container.NewVBox(
			widget.NewLabelWithStyle("Updating 7GL", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
			widget.NewSeparator(),
			assetInfoLabel,
			progressBar,
			statusLabel,
			widget.NewSeparator(),
			container.NewHBox(layout.NewSpacer(), cancelDownloadBtn),
		)

		rootContainer.Objects = []fyne.CanvasObject{progressBox}
		rootContainer.Refresh()

		go func() {
			// Allocate RAM tmpfs staging workspace (bounded by user config)
			requiredBytes := uint64(matchingAsset.Size) * 4
			if requiredBytes < 40*1024*1024 {
				requiredBytes = 40 * 1024 * 1024
			}

			appstate.UserConfigMu.RLock()
			ramPercent := appstate.UserConfig.System.RAMUsagePercent
			ramLimitMB := appstate.UserConfig.System.RAMLimitMB
			appstate.UserConfigMu.RUnlock()

			tempDir, isRAM, allocatedBytes := sys.SelectTempStorage(requiredBytes, ramPercent, ramLimitMB)
			archivePath := filepath.Join(tempDir, matchingAsset.Name)

			cleanup := func() {
				sys.ReleaseTempStorage(tempDir, isRAM, allocatedBytes)
			}

			fyne.Do(func() {
				if isRAM {
					statusLabel.SetText("Downloading update into RAM (tmpfs)...")
				} else {
					statusLabel.SetText("Downloading update...")
				}
			})

			// Stream asset directly with throttled progress updates
			err := sys.DownloadAsset(ctx, matchingAsset, archivePath, func(ratio float64, cur, tot int64) {
				progressBar.SetValue(ratio)
				statusLabel.SetText(fmt.Sprintf("Downloading: %s / %s (%.0f%%)", sys.FormatSize(cur), sys.FormatSize(tot), ratio*100))
			})
			if err != nil {
				cleanup()
				if ctx.Err() != context.Canceled {
					fyne.Do(func() {
						dialog.ShowError(fmt.Errorf("failed to download update: %w", err), w)
						if d != nil {
							d.Hide()
						}
					})
				}
				return
			}

			fyne.Do(func() {
				cancelDownloadBtn.Disable()
				statusLabel.SetText("Extracting update package in memory...")
				progressBar.SetValue(1.0)
			})

			extractDir := filepath.Join(tempDir, "extracted")
			if err := os.MkdirAll(extractDir, 0755); err != nil {
				cleanup()
				fyne.Do(func() {
					dialog.ShowError(fmt.Errorf("failed to prepare extraction directory: %w", err), w)
					if d != nil {
						d.Hide()
					}
				})
				return
			}

			// Decompress .tar.xz using 7-Zip stream pipeline
			if err := engine.ExtractArchive(archivePath, extractDir, ""); err != nil {
				cleanup()
				fyne.Do(func() {
					dialog.ShowError(fmt.Errorf("failed to extract update archive: %w", err), w)
					if d != nil {
						d.Hide()
					}
				})
				return
			}

			scriptPath := sys.FindUpdateScript(extractDir)
			if scriptPath == "" {
				cleanup()
				fyne.Do(func() {
					dialog.ShowError(fmt.Errorf("update script (update.sh or install.sh) not found in archive"), w)
					if d != nil {
						d.Hide()
					}
				})
				return
			}

			binPath, err := sys.ResolveBinaryLocation()
			if err != nil {
				binPath, _ = os.Executable()
			}
			isWritable := sys.IsInstallWritable(binPath)

			fyne.Do(func() {
				if !isWritable {
					statusLabel.SetText("Requesting root permissions to update system installation...")
				} else {
					statusLabel.SetText("Applying update...")
				}
			})

			if _, err := sys.ExecuteUpdateScript(scriptPath, isWritable); err != nil {
				cleanup()
				fyne.Do(func() {
					dialog.ShowError(fmt.Errorf("failed to execute update script: %w", err), w)
					if d != nil {
						d.Hide()
					}
				})
				return
			}

			fyne.Do(func() {
				statusLabel.SetText("Update started! 7GL will now close to complete installation.")
			})

			// Hand-off delay before closing GUI to avoid race on binary replacement
			time.Sleep(1500 * time.Millisecond)
			fyne.Do(func() {
				a.Quit()
			})
		}()
	})
	updateBtn.Importance = widget.HighImportance

	manualBtn := widget.NewButton("Manual Download ↗", func() {
		a.OpenURL(relURL)
		if d != nil {
			d.Hide()
		}
	})
	manualBtn.Importance = widget.LowImportance

	laterBtn := widget.NewButton("Later", func() {
		if d != nil {
			d.Hide()
		}
	})
	laterBtn.Importance = widget.LowImportance

	actionRow := container.NewVBox(
		container.NewHBox(layout.NewSpacer(), manualBtn, laterBtn, updateBtn),
		widget.NewLabel("Update Now requires to close the app. Please be advised."),
	)
	promptLayout := container.NewBorder(nil, actionRow, nil, nil, container.NewVBox(contentObjects...))

	rootContainer.Objects = []fyne.CanvasObject{promptLayout}

	d = dialog.NewCustomWithoutButtons("Software Update", rootContainer, w)
	d.Resize(fyne.NewSize(500, 360))
	d.Show()
}
