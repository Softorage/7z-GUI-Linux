package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
)

var Root7zCmd string = "7z"

func CheckDependencies(w fyne.Window) {
	// Check for 7zz in PATH
	if _, err := exec.LookPath("7zz"); err == nil {
		Root7zCmd = "7zz"
		return
	}
	// Check for 7zzs in PATH
	if _, err := exec.LookPath("7zzs"); err == nil {
		Root7zCmd = "7zzs"
		return
	}
	// Check for ./7zzs (placed in the same directory as the app)
	local7zzsPath := GetFullCmdPath("7zzs", w)
	if info, err := os.Stat(local7zzsPath); err == nil && !info.IsDir() {
		// Ensure the file has executable permissions (Unix/Linux)
		if info.Mode().Perm()&0111 != 0 {
			Root7zCmd = local7zzsPath
			return
		}
	}
	// Check for 7z in PATH
	if _, err := exec.LookPath("7z"); err == nil {
		Root7zCmd = "7z"
		return
	}

	dialog.ShowInformation("7-Zip Not Found", "No 7z found to be installed or at recognized place in the system. We have automated workflow that ensures you have 7-Zip when you install this tool. It appears something may not worked correctly during install. It is recommended to either uninstall the tool, download latest copy and reinstall, so that you have a working copy of 7-Zip on your system, or install 7-Zip manually.", w)
}

// use this for 7zzs that sits beside our binary
func GetFullCmdPath(appname string, w fyne.Window) string {
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