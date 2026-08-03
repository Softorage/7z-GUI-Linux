package assets

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

//go:embed logo.png
var resourceLogoPngData []byte
var ResourceLogoPng = &fyne.StaticResource{
	StaticName:    "logo.png",
	StaticContent: resourceLogoPngData,
}

// Note: Additional resources (e.g. resourceSourceCodeSvg) can be embedded here as needed.

/* // SVGs do not work well for some reason.
//go:embed icons/compress.svg
var resourceCompressSvgData []byte
var ResourceCompressSvg = &fyne.StaticResource{
	StaticName:    "icons/compress.svg",
	StaticContent: resourceCompressSvgData,
}

//go:embed icons/external-link.svg
var resourceExternalLinkSvgData []byte
var ResourceExternalLinkSvg = &fyne.StaticResource{
	StaticName:    "icons/external-link.svg",
	StaticContent: resourceExternalLinkSvgData,
}

//go:embed icons/extract.svg
var resourceExtractSvgData []byte
var ResourceExtractSvg = &fyne.StaticResource{
	StaticName:    "icons/extract.svg",
	StaticContent: resourceExtractSvgData,
}

//go:embed icons/info.svg
var resourceInfoSvgData []byte
var ResourceInfoSvg = &fyne.StaticResource{
	StaticName:    "icons/info.svg",
	StaticContent: resourceInfoSvgData,
}

//go:embed icons/source-code.svg
var resourceSourceCodeSvgData []byte
var ResourceSourceCodeSvg = &fyne.StaticResource{
	StaticName:    "icons/source-code.svg",
	StaticContent: resourceSourceCodeSvgData,
}
*/