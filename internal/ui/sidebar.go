package ui

import (
	"fmt"
	"image/color"
	"net/url"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	appstate "github.com/Softorage/7z-GUI-Linux/internal/app"
	"github.com/Softorage/7z-GUI-Linux/internal/version"
)

// BuildSidebar constructs the left sidebar containing the branding, tab navigation list, and external link actions.
func BuildSidebar(a fyne.App) fyne.CanvasObject {
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
	return container.NewMax(sidebarBg, sidebarContent)
}