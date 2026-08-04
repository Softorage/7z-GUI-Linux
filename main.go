package main

import (
	"fmt"
	"image/color"
	"net/url"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/Softorage/7z-GUI-Linux/assets"
	appstate "github.com/Softorage/7z-GUI-Linux/internal/app"
	"github.com/Softorage/7z-GUI-Linux/internal/domain"
	"github.com/Softorage/7z-GUI-Linux/internal/engine"
	"github.com/Softorage/7z-GUI-Linux/internal/sys"
	"github.com/Softorage/7z-GUI-Linux/internal/ui/components"
	"github.com/Softorage/7z-GUI-Linux/internal/ui/tabs"
	"github.com/Softorage/7z-GUI-Linux/internal/version"
)

func main() {
	a := app.NewWithID("com.softorage.7gl")
	if err := appstate.InitConfig(); err != nil {
		fmt.Printf("Warning: Failed to initialize configuration: %v\n", err)
	}

	a.SetIcon(assets.ResourceLogoPng)
	w := a.NewWindow("7-Zip GUI for Linux")
	w.Resize(fyne.NewSize(1040, 650))

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
	contentArea := container.NewMax()

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
			Text: fmt.Sprintf("\nversion %s%s", version.Version, version.SponsorEditionTag),
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

	sidebarContent := container.NewBorder(sidebarTop, sidebarBottom, nil, nil, appstate.Tabs)

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
			container.NewVBox(widget.NewSeparator(), appstate.InfoBar),
			nil,
			nil,
			contentArea,
		),
	)

	// Layout Main Window
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

	// Check for updates asynchronously without blocking the UI
	go sys.CheckForUpdates(w, a, components.ShowUpdateDialog)
	w.ShowAndRun()
}
