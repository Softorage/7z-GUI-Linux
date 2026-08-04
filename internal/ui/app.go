package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	appstate "github.com/Softorage/7z-GUI-Linux/internal/app"
	"github.com/Softorage/7z-GUI-Linux/internal/domain"
	"github.com/Softorage/7z-GUI-Linux/internal/ui/tabs"
)

// BuildMainLayout constructs the primary application interface layout.
func BuildMainLayout(w fyne.Window, a fyne.App) fyne.CanvasObject {
	// Bottom Info Bar
	appstate.InfoBar = widget.NewLabel("Ready. Interact with an option to see its description.")
	appstate.InfoBar.Alignment = fyne.TextAlignCenter
	appstate.InfoBar.Wrapping = fyne.TextWrapWord // Properly wraps text instead of resizing window

	// Build Tab Contents
	explorerTab := tabs.BuildExplorerTab(w)
	compressTab := tabs.BuildCompressTab(w)
	extractTab := tabs.BuildExtractTab(w)
	checksumTab := tabs.BuildChecksumTab(w)
	statusTab := tabs.BuildStatusTab(w)

	// Create a Max container that will act as the dynamic main content area
	contentArea := container.NewStack()

	// Construct Sidebar Tabs Menu
	titles := make([]string, 5)
	titles[domain.ExplorerTabRank] = "Explorer"
	titles[domain.CompressTabRank] = "Compress"
	titles[domain.ExtractTabRank] = "Extract"
	titles[domain.ChecksumTabRank] = "Checksum"
	titles[domain.StatusTabRank] = "Status"

	appstate.Tabs = widget.NewList(
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
	appstate.Tabs.OnSelected = func(id widget.ListItemID) {
		appstate.StateMu.RLock()
		running := appstate.IsOperationRunning
		appstate.StateMu.RUnlock()

		if running && id != domain.StatusTabRank {
			appstate.SetInfo("Action locked: Operation currently in progress.")
			appstate.Tabs.Select(domain.StatusTabRank) // Force back to Status
			return
		}

		// Swap out the objects inside the main content area
		switch id {
		case domain.ExplorerTabRank:
			contentArea.Objects = []fyne.CanvasObject{explorerTab}
		case domain.CompressTabRank:
			contentArea.Objects = []fyne.CanvasObject{compressTab}
		case domain.ExtractTabRank:
			contentArea.Objects = []fyne.CanvasObject{extractTab}
		case domain.ChecksumTabRank:
			contentArea.Objects = []fyne.CanvasObject{checksumTab}
		case domain.StatusTabRank:
			contentArea.Objects = []fyne.CanvasObject{statusTab}
		}
		contentArea.Refresh()
	}

	sidebar := BuildSidebar(a)

	// Combine Sidebar (Left) and Main Content (Right)
	return container.NewBorder(
		nil,
		nil,
		sidebar,
		nil,
		container.NewBorder(
			nil,
			container.NewVBox(widget.NewSeparator(), appstate.InfoBar),
			nil,
			nil,
			contentArea,
		),
	)
}
