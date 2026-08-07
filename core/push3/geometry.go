package push3

// Display geometry — same as Push 2 (confirmed by user):
//   960×160 physical pixels, stride = 1024 pixels per row (64px padding)
//   Push 3 sends the frame TWICE per display update (double-buffered):
//   chunk 1 = bytes   0..327679  (1024 × 160 × 2)
//   chunk 2 = bytes 327680..655359 (identical copy)
const (
	VisW       = 960                // visible pixel columns
	VisH       = 160                // rows
	Stride     = 1024               // pixels per row in framebuffer (incl. padding)
	FrameBytes = Stride * VisH * 2  // 327680 bytes per single frame
	TotalBytes = FrameBytes * 2     // 655360 bytes total (sent twice)
)
