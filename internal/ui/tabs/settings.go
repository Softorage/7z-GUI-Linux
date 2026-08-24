package tabs

import (
	"fmt"

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

	// Section: System & Environment Info
	systemInfoSection := container.NewVBox(
		widget.NewLabelWithStyle("System & Architecture", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewSeparator(),
		widget.NewForm(
			widget.NewFormItem("Config Path:", widget.NewLabel(fmt.Sprintf("~/.config/%s/config.yaml", domain.AppDirName))), // TODO: fetch the config directory instead of hardcoding like this. the user may be running as root.
			widget.NewFormItem("Tmpfs Staging:", widget.NewLabel(domain.TmpfsDefaultDir)),
		),
	)

	// Combine sections into a scrollable card container
	settingsContent := container.NewVBox(
		updateSection,
		widget.NewLabel(""),
		systemInfoSection,
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
