package main

import (
	"fmt"
	"image/color"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// TODO: check if new version available and provide instructions to update in a dialog.

func main() {
	a := app.NewWithID("com.softorage.7gl")
	a.SetIcon(resourceLogoPng)
	w := a.NewWindow("7-Zip GUI for Linux")
	w.Resize(fyne.NewSize(1040, 650))

	// Bottom Info Bar
	infoBar = widget.NewLabel("Ready. Interact with an option to see its description.")
	infoBar.Alignment = fyne.TextAlignCenter
	infoBar.Wrapping = fyne.TextWrapWord // Properly wraps text instead of resizing window

	// Build Tab Contents
	explorerTab := buildExplorerTab(w)
	compressTab := buildCompressTab(w)
	extractTab := buildExtractTab(w)
	checksumTab := buildChecksumTab(w)
	statusTab := buildStatusTab(w)

	// Create a Max container that will act as the dynamic main content area
	contentArea := container.NewMax()

	// Construct Sidebar Tabs Menu
	titles := make([]string, 5)
	titles[ExplorerTabRank] = "Explorer"
	titles[CompressTabRank] = "Compress"
	titles[ExtractTabRank] = "Extract"
	titles[ChecksumTabRank] = "Checksum"
	titles[StatusTabRank] = "Status"

	tabs = widget.NewList(
		func() int { return len(titles) },
		func() fyne.CanvasObject {
			lbl := widget.NewLabel("")
			lbl.TextStyle = fyne.TextStyle{Bold: true}
			return container.NewPadded(lbl)
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			// Updating list elements safely
			o.(*fyne.Container).Objects[0].(*widget.Label).SetText(titles[i])
		},
	)

	// Handle switching tab views
	tabs.OnSelected = func(id widget.ListItemID) {
		stateMu.RLock()
		running := isOperationRunning
		stateMu.RUnlock()

		if running && id != StatusTabRank {
			setInfo("Action locked: Operation currently in progress.")
			tabs.Select(StatusTabRank) // Force back to Status
			return
		}

		// Swap out the objects inside the main content area
		switch id {
		case ExplorerTabRank:
			contentArea.Objects = []fyne.CanvasObject{explorerTab}
		case CompressTabRank:
			contentArea.Objects = []fyne.CanvasObject{compressTab}
		case ExtractTabRank:
			contentArea.Objects = []fyne.CanvasObject{extractTab}
		case ChecksumTabRank:
			contentArea.Objects = []fyne.CanvasObject{checksumTab}
		case StatusTabRank:
			contentArea.Objects = []fyne.CanvasObject{statusTab}
		}
		contentArea.Refresh()
	}

	// Construct Sidebar Elements
	// Top: App Name and version
	appLabel := widget.NewRichText(
		&widget.TextSegment{
			Text: "7GL",
			Style: widget.RichTextStyle{
				SizeName:  theme.SizeNameHeadingText,
				TextStyle: fyne.TextStyle{Bold: true},
				Alignment: fyne.TextAlignCenter,
			},
		},
		&widget.TextSegment{
			Text: fmt.Sprintf("\nversion %s", version),
			Style: widget.RichTextStyle{
				SizeName:  theme.SizeNameCaptionText,
				ColorName: theme.ColorNamePlaceHolder,
				Alignment: fyne.TextAlignCenter,
			},
		},
	)

	// Center the text using an HBox with spacers on both sides
	centeredAppLabel := container.NewHBox(layout.NewSpacer(), appLabel, layout.NewSpacer())

	sidebarTop := container.NewVBox(
		container.NewPadded(centeredAppLabel),
		widget.NewLabel(""),
	)

	// Bottom: Github URL, Sponsor URL & Version
	sourceCodeURL, _ := url.Parse("https://github.com/Softorage/7z-GUI-Linux")
	sponsorURL, _ := url.Parse("https://rzp.io/rzp/hY39lZGa")

	//sourceCodeBtn := widget.NewButtonWithIcon("View Source", resourceSourceCodeSvg, func() { a.OpenURL(sourceCodeURL) })
	sourceCodeBtn := widget.NewButton("View Source ↗", func() { a.OpenURL(sourceCodeURL) })
	//sourceCodeBtn.IconPlacement = widget.ButtonIconLeadingText
	sourceCodeBtn.Importance = widget.LowImportance
	sourceCodeBtn.Alignment = widget.ButtonAlignLeading

	sponsorBtn := widget.NewButton("Sponsor ↗", func() { a.OpenURL(sponsorURL) })
	sponsorBtn.Importance = widget.LowImportance
	sponsorBtn.Alignment = widget.ButtonAlignLeading

	tabsBottom := container.NewVBox(
		container.NewPadded(sourceCodeBtn),
		container.NewPadded(sponsorBtn),
	)

	aboutText := widget.NewRichText(&widget.TextSegment{
		Text: fmt.Sprintf("A Softorage Project"),
		Style: widget.RichTextStyle{
			SizeName:  theme.SizeNameCaptionText,
			ColorName: theme.ColorNamePlaceHolder,
		},
	})

	sidebarBottom := container.NewVBox(
		tabsBottom,
		container.NewCenter(aboutText),
	)

	sidebarContent := container.NewBorder(sidebarTop, sidebarBottom, nil, nil, tabs)

	// Create Sidebar Background
	// A translucent gray (alpha=25) creates a subtle contrast for both Light and Dark themes.
	sidebarBg := canvas.NewRectangle(color.NRGBA{R: 128, G: 128, B: 128, A: 25})
	// Force a minimum width to make the sidebar cozier/wider (180px width)
	sidebarBg.SetMinSize(fyne.NewSize(180, 0))

	// Combine the background color and the sidebar content
	sidebar := container.NewMax(sidebarBg, sidebarContent)

	// Combine Sidebar (Left) and Main Content (Right)
	mainLayout := container.NewBorder(
		nil,
		nil,
		sidebar,
		nil,
		container.NewBorder(
			nil,
			container.NewVBox(widget.NewSeparator(), infoBar),
			nil,
			nil,
			contentArea,
		),
	)

	// Layout Main Window
	w.SetContent(mainLayout)

	// Pre-select first tab
	tabs.Select(ExplorerTabRank)

	// Dependency check
	checkDependencies(w)

	// Set backend7z to store the backend 7-zip being used
	backend7z := ""
	// Using length instead of filepath.base, as it allows to differentiate between 7zzs and ./7zzs
	// Restrict length to 5 letter in case of absolue path (./7zzs). Check the length first to prevent a crash
	if len(root7zCmd) >= 5 {
		backend7z = root7zCmd[len(root7zCmd)-5:]
	} else {
		// Fallback if the string is shorter than 5 letters
		if root7zCmd == "7z" {
			backend7z = "p7zip"
		} else {
			backend7z = root7zCmd
		}
	}
	// Set initial value for the default record under Operations History
	historyData = []operationLog{
		{
			ID:        0,
			File:      fmt.Sprintf("7-Zip GUI (%s) with '%s' as backend", version, backend7z),
			OpType:    "Initialized",
			Status:    "Ready",
			Timestamp: time.Now().Format("15:04:05"),
		},
	}

	w.ShowAndRun()
}

func checkDependencies(w fyne.Window) {
	// Check for 7zz in PATH
	if _, err := exec.LookPath("7zz"); err == nil {
		root7zCmd = "7zz"
		return
	}
	// Check for 7zzs in PATH
	if _, err := exec.LookPath("7zzs"); err == nil {
		root7zCmd = "7zzs"
		return
	}
	// Check for ./7zzs (placed in the same directory as the app)
	local7zzsPath := getFullCmdPath("7zzs", w)
	if info, err := os.Stat(local7zzsPath); err == nil && !info.IsDir() {
		// Ensure the file has executable permissions (Unix/Linux)
		if info.Mode().Perm()&0111 != 0 {
			root7zCmd = local7zzsPath
			return
		}
	}
	// Check for 7z in PATH
	if _, err := exec.LookPath("7z"); err == nil {
		root7zCmd = "7z"
		return
	}

	dialog.ShowInformation("7-Zip Not Found", "No 7z found to be installed or at recognized place in the system. We have automated workflow that ensures you have 7-Zip when you install this tool. It appears something may not worked correctly during install. It is recommended to either uninstall the tool, download latest copy and reinstall, so that you have a working copy of 7-Zip on your system, or install 7-Zip manually.", w)
}

// use this for 7zzs that sits beside our binary
func getFullCmdPath(appname string, w fyne.Window) string {
	exePath, err := os.Executable()
	if err != nil {
		dialog.ShowError(fmt.Errorf("Failed to get executable path: %v", err), w)
	}
	realPath, err := filepath.EvalSymlinks(exePath)
	if err != nil {
		realPath = exePath
	}
	exeDir := filepath.Dir(realPath)
	appnamePath := filepath.Join(exeDir, appname)
	return appnamePath
}
