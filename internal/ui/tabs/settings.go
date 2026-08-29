package tabs

import (
	"fmt"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	appstate "github.com/Softorage/7z-GUI-Linux/internal/app"
	"github.com/Softorage/7z-GUI-Linux/internal/domain"
	"github.com/Softorage/7z-GUI-Linux/internal/sys"
	"github.com/Softorage/7z-GUI-Linux/internal/ui/components"
	"github.com/Softorage/7z-GUI-Linux/internal/version"
)

// BuildSettingsTab constructs the Settings configuration view.
func BuildSettingsTab(w fyne.Window, a fyne.App) fyne.CanvasObject {
	// Read initial configuration safely
	appstate.UserConfigMu.RLock()
	startupCheck := appstate.UserConfig.Updates.CheckOnStartup
	initialRAMPercent := appstate.UserConfig.System.RAMUsagePercent
	initialRAMLimitMB := appstate.UserConfig.System.RAMLimitMB
	appstate.UserConfigMu.RUnlock()

	// Section: Software Updates
	checkOnStartupCheck := widget.NewCheck("Check for updates automatically on startup", func(checked bool) {
		appstate.UserConfigMu.Lock()
		appstate.UserConfig.Updates.CheckOnStartup = checked
		appstate.UserConfigMu.Unlock()

		if err := appstate.SaveConfig(); err != nil {
			appstate.SetInfo("Warning: Failed to save preferences to disk.")
		} else {
			if checked {
				appstate.SetInfo("Automatic startup update checks enabled.")
			} else {
				appstate.SetInfo("Automatic startup update checks disabled.")
			}
		}
	})
	checkOnStartupCheck.SetChecked(startupCheck)

	updateStatusLabel := widget.NewLabelWithStyle(
		fmt.Sprintf("Installed Version: %s", version.Version),
		fyne.TextAlignLeading,
		fyne.TextStyle{Italic: true},
	)

	var checkNowBtn *widget.Button
	checkNowBtn = widget.NewButtonWithIcon("Check for Updates Now", theme.ViewRefreshIcon(), func() {
		checkNowBtn.Disable()
		updateStatusLabel.SetText("Checking for updates...")
		appstate.SetInfo("Connecting to update server...")

		go sys.CheckForUpdatesManual(
			w,
			a,
			func(win fyne.Window, app fyne.App, rel sys.GithubRelease) {
				checkNowBtn.Enable()
				updateStatusLabel.SetText(fmt.Sprintf("New version available: %s", rel.TagName))
				components.ShowUpdateDialog(win, app, rel)
			},
			func() {
				checkNowBtn.Enable()
				updateStatusLabel.SetText(fmt.Sprintf("You are using the latest version (%s).", version.Version))
				dialog.ShowInformation("Up to Date", fmt.Sprintf("7GL is up to date (version %s).", version.Version), w)
				appstate.SetInfo("Application is up to date.")
			},
			func(err error) {
				checkNowBtn.Enable()
				updateStatusLabel.SetText("Failed to check for updates.")
				dialog.ShowError(fmt.Errorf("Update check failed: %w", err), w)
				appstate.SetInfo("Update check failed.")
			},
		)
	})
	checkNowBtn.Importance = widget.MediumImportance

	updateSection := container.NewVBox(
		widget.NewLabelWithStyle("Updates & Maintenance", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewSeparator(),
		checkOnStartupCheck,
		container.NewHBox(checkNowBtn, layout.NewSpacer()),
		updateStatusLabel,
	)

	// Section: Nested Archives & RAM Staging
	if initialRAMPercent <= 0 {
		initialRAMPercent = domain.DefaultRAMPercent
	}
	if initialRAMLimitMB <= 0 {
		initialRAMLimitMB = domain.DefaultRAMLimitMB
	}

	ramPercentLabel := widget.NewLabel(fmt.Sprintf("%d%%", initialRAMPercent))
	ramPercentLabel.TextStyle = fyne.TextStyle{Bold: true}

	ramSlider := widget.NewSlider(float64(domain.MinRAMUsagePercent), float64(domain.MaxRAMUsagePercent))
	ramSlider.Step = 1
	ramSlider.SetValue(float64(initialRAMPercent))

	ramLimitEntry := widget.NewEntry()
	ramLimitEntry.SetText(strconv.FormatInt(initialRAMLimitMB, 10))
	ramLimitEntry.PlaceHolder = fmt.Sprintf("e.g. %d", domain.DefaultRAMLimitMB)

	ramLimitWarning := widget.NewLabel("")
	ramLimitWarning.Importance = widget.WarningImportance
	ramLimitWarning.Wrapping = fyne.TextWrapWord

	ramDiagnosticLabel := widget.NewLabel("")
	ramDiagnosticLabel.Wrapping = fyne.TextWrapWord

	updateRAMDiagnostics := func() {
		currentPercent := int(ramSlider.Value)
		limitMB, err := strconv.ParseInt(strings.TrimSpace(ramLimitEntry.Text), 10, 64)
		if err != nil || limitMB < domain.MinRAMLimitMB {
			limitMB = domain.DefaultRAMLimitMB
		}

		totalRAM := sys.GetTotalRAMBytes()
		availRAM := sys.GetAvailableRAMBytes()

		budget := (availRAM * uint64(currentPercent)) / 100
		maxCap := uint64(limitMB) * 1024 * 1024
		if budget > maxCap {
			budget = maxCap
		}

		ramPercentLabel.SetText(fmt.Sprintf("%d%%", currentPercent))

		if strings.TrimSpace(ramLimitEntry.Text) != "" {
			parsedVal, parseErr := strconv.ParseInt(strings.TrimSpace(ramLimitEntry.Text), 10, 64)
			if parseErr == nil && parsedVal < 512 {
				ramLimitWarning.SetText("Note: Staging cap below 512 MB will cause most nested archives to stage on disk.")
				ramLimitWarning.Show()
			} else {
				ramLimitWarning.Hide()
			}
		}

		ramDiagnosticLabel.SetText(fmt.Sprintf(
			"System RAM: %s (Available: %s)\nEffective Max RAM Staging: %s (larger archives will stage on disk cache)",
			sys.FormatSize(int64(totalRAM)),
			sys.FormatSize(int64(availRAM)),
			sys.FormatSize(int64(budget)),
		))
	}

	saveRAMConfig := func() {
		currentPercent := int(ramSlider.Value)
		limitMB, err := strconv.ParseInt(strings.TrimSpace(ramLimitEntry.Text), 10, 64)
		if err != nil || limitMB < domain.MinRAMLimitMB {
			limitMB = domain.DefaultRAMLimitMB
		}

		appstate.UserConfigMu.Lock()
		appstate.UserConfig.System.RAMUsagePercent = currentPercent
		appstate.UserConfig.System.RAMLimitMB = limitMB
		appstate.UserConfigMu.Unlock()

		_ = appstate.SaveConfig()
		updateRAMDiagnostics()
	}

	ramSlider.OnChanged = func(_ float64) {
		saveRAMConfig()
	}

	ramLimitEntry.OnChanged = func(_ string) {
		saveRAMConfig()
	}

	createPresetBtn := func(label string, mb int64) *widget.Button {
		b := widget.NewButton(label, func() {
			ramLimitEntry.SetText(strconv.FormatInt(mb, 10))
		})
		b.Importance = widget.LowImportance
		return b
	}

	presetRow := container.NewHBox(
		createPresetBtn("2 GB", 2048),
		createPresetBtn("4 GB", 4096),
		createPresetBtn("8 GB", 8192),
		createPresetBtn("16 GB", 16384),
		createPresetBtn("32 GB", 32768),
	)

	updateRAMDiagnostics()

	ramSection := container.NewVBox(
		widget.NewLabelWithStyle("RAM Staging for Nested Archives", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewSeparator(),
		widget.NewForm(
			widget.NewFormItem("RAM Allocation (%):", container.NewBorder(nil, nil, nil, ramPercentLabel, ramSlider)),
			widget.NewFormItem("RAM Hard Cap (MB):", ramLimitEntry),
			widget.NewFormItem("Quick Presets:", presetRow),
		),
		ramLimitWarning,
		ramDiagnosticLabel,
	)

	// Section: Clipboard & Storage
	clearClipboardCheck := widget.NewCheck("Clear 7GL's clipboard after successful paste", func(checked bool) {
		appstate.SetClipboardClearOnSuccess(checked)
		if checked {
			appstate.SetInfo("Clipboard auto-clear enabled.")
		} else {
			appstate.SetInfo("Clipboard auto-clear disabled.")
		}
	})
	clearClipboardCheck.SetChecked(appstate.GetClipboardClearOnSuccess())

	clipboardSection := container.NewVBox(
		widget.NewLabelWithStyle("Clipboard", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewSeparator(),
		clearClipboardCheck,
	)

	// Section Write Locations
	createPathRow := func(path string, label string) fyne.CanvasObject {
		copyBtn := widget.NewButtonWithIcon("", theme.ContentCopyIcon(), func() {
			a.Clipboard().SetContent(path)
			appstate.SetInfo(fmt.Sprintf("%s copied to clipboard.", label))
		})
		copyBtn.Importance = widget.LowImportance

		pathLabel := widget.NewLabel(path)
		pathLabel.TextStyle = fyne.TextStyle{Monospace: true}
		pathLabel.Truncation = fyne.TextTruncateEllipsis

		return container.NewBorder(nil, nil, nil, copyBtn, pathLabel)
	}

	storageForm := widget.NewForm(
		widget.NewFormItem("Configuration:", createPathRow(appstate.GetConfigFilePath(), "Config path")),
		widget.NewFormItem("Disk Cache:", createPathRow(sys.GetDiskCacheDir(), "Disk cache directory")),
		widget.NewFormItem("RAM tmpfs Staging:", createPathRow(domain.TmpfsDefaultDir, "RAM tmpfs path")),
	)

	writeLocationsSection := container.NewVBox(
		widget.NewLabelWithStyle("Storage & Write Locations", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewSeparator(),
		storageForm,
	)
	// Combine sections into a scrollable card container
	settingsContent := container.NewVBox(
		updateSection,
		widget.NewLabel(""),
		ramSection,
		widget.NewLabel(""),
		clipboardSection,
		widget.NewLabel(""),
		writeLocationsSection,
	)

	return container.NewPadded(container.NewBorder(
		container.NewVBox(
			widget.NewRichTextFromMarkdown("## Settings"),
			widget.NewSeparator(),
		),
		nil,
		nil,
		nil,
		container.NewVScroll(settingsContent),
	))
}
