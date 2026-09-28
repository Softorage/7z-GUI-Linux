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

	// Create a Max container that will act as the dynamic main content area
	contentArea := container.NewStack()

	// Tab view cache for lazy, on-demand instantiation to minimizes startup heap allocation
	var tabViews [6]fyne.CanvasObject

	getTab := func(id widget.ListItemID) fyne.CanvasObject {
		if id < 0 || int(id) >= len(tabViews) {
			return nil
		}
		if tabViews[id] != nil {
			return tabViews[id]
		}

		switch id {
		case domain.ExplorerTabRank:
			tabViews[id] = tabs.BuildExplorerTab(w)
		case domain.CompressTabRank:
			tabViews[id] = tabs.BuildCompressTab(w)
		case domain.ExtractTabRank:
			tabViews[id] = tabs.BuildExtractTab(w)
		case domain.ChecksumTabRank:
			tabViews[id] = tabs.BuildChecksumTab(w)
		case domain.StatusTabRank:
			tabViews[id] = tabs.BuildStatusTab(w)
		case domain.SettingsTabRank:
			tabViews[id] = tabs.BuildSettingsTab(w, a)
		}
		return tabViews[id]
	}

	// Construct Sidebar Tabs Menu
	titles := [6]string{
		domain.ExplorerTabRank: "Explorer",
		domain.CompressTabRank: "Compress",
		domain.ExtractTabRank:  "Extract",
		domain.ChecksumTabRank: "Checksum",
		domain.StatusTabRank:   "Status",
		domain.SettingsTabRank: "Settings",
	}

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

		selectedTab := getTab(id)
		if selectedTab == nil {
			return
		}

		if len(contentArea.Objects) == 1 && contentArea.Objects[0] == selectedTab {
			return
		}

		// Swap out the objects inside the main content area
		contentArea.Objects = []fyne.CanvasObject{selectedTab}
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
