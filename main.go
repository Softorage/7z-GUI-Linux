package main

import (
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"

	"github.com/Softorage/7z-GUI-Linux/assets"
	appstate "github.com/Softorage/7z-GUI-Linux/internal/app"
	"github.com/Softorage/7z-GUI-Linux/internal/domain"
	"github.com/Softorage/7z-GUI-Linux/internal/engine"
	"github.com/Softorage/7z-GUI-Linux/internal/sys"
	"github.com/Softorage/7z-GUI-Linux/internal/ui"
	"github.com/Softorage/7z-GUI-Linux/internal/ui/components"
	"github.com/Softorage/7z-GUI-Linux/internal/version"
)

func main() {
	a := app.NewWithID(domain.AppID)
	if err := appstate.InitConfig(); err != nil {
		fmt.Printf("Warning: Failed to initialize configuration: %v\n", err)
	}

	a.SetIcon(assets.ResourceLogoPng)
	w := a.NewWindow("7-Zip GUI for Linux")
	w.Resize(fyne.NewSize(1040, 650))

	// Layout Main Window
	mainLayout := ui.BuildMainLayout(w, a)
	w.SetContent(mainLayout)

	// Pre-select first tab
	appstate.Tabs.Select(domain.ExplorerTabRank)

	// Dependency check
	engine.CheckDependencies(w)

	// Set backend7z to store the backend 7-zip being used
	backend7z := ""
	// Using length instead of filepath.base, as it allows to differentiate between 7zzs and ./7zzs
	// Restrict length to 5 letter in case of absolue path (./7zzs). Check the length first to prevent a crash
	if len(engine.Root7zCmd) >= 5 {
		backend7z = engine.Root7zCmd[len(engine.Root7zCmd)-5:]
	} else {
		// Fallback if the string is shorter than 5 letters
		if engine.Root7zCmd == "7z" {
			backend7z = "p7zip"
		} else {
			backend7z = engine.Root7zCmd
		}
	}
	// Set initial value for the default record under Operations History
	appstate.HistoryData = []domain.OperationLog{
		{
			ID:        0,
			File:      fmt.Sprintf("7-Zip GUI (%s) with '%s' as backend", version.Version, backend7z),
			OpType:    "Initialized",
			Status:    "Ready",
			Timestamp: time.Now().Format("15:04:05"),
		},
	}

	// Conditionally check for updates asynchronously based on user preference
	appstate.UserConfigMu.RLock()
	checkOnStartup := appstate.UserConfig.Updates.CheckOnStartup
	appstate.UserConfigMu.RUnlock()

	if checkOnStartup {
		go sys.CheckForUpdates(w, a, components.ShowUpdateDialog)
	}
	w.ShowAndRun()
}
