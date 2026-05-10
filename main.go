package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

func main() {
	a := app.New()
	a.SetIcon(resourceIconPng)
	w := a.NewWindow("7-Zip GUI for Linux")
	w.Resize(fyne.NewSize(800, 650))

	// Bottom Info Bar
	infoBar = widget.NewLabel("Ready. Interact with an option to see its description.")
	infoBar.Alignment = fyne.TextAlignCenter
	infoBar.Wrapping = fyne.TextWrapWord // Properly wraps text instead of resizing window

	// Build Tabs
	compressTab := buildCompressTab(w)
	extractTab := buildExtractTab(w)
	statusTab := buildStatusTab(w)

	tabs = container.NewAppTabs(
		container.NewTabItem("Compress", compressTab),
		container.NewTabItem("Extract", extractTab),
		container.NewTabItem("Status", statusTab),
	)

	// Intercept tab switching to lock user on Status tab during operations
	tabs.OnSelected = func(t *container.TabItem) {
		stateMu.RLock()
		running := isOperationRunning
		stateMu.RUnlock()

		if running && t.Text != "Status" {
			setInfo("Action locked: Operation currently in progress.")
			tabs.SelectIndex(2) // Force back to Status (index 2)
		}
	}

	// Layout Main Window
	w.SetContent(container.NewBorder(
		nil,
		container.NewVBox(widget.NewSeparator(), infoBar),
		nil,
		nil,
		tabs,
	))

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
