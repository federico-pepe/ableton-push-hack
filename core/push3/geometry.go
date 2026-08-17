package push3

import "github.com/federico-pepe/ableton-push-hack/core/display"

// Display geometry — moved to core/display/geometry.go 2026-08-17, since it
// is not Push-3-specific (identical on Push 2, confirmed by measurement).
// Re-exported here so existing callers (push3.VisW etc.) keep working.
const (
	VisW       = display.VisW
	VisH       = display.VisH
	Stride     = display.Stride
	FrameBytes = display.FrameBytes
	TotalBytes = display.TotalBytes
)
