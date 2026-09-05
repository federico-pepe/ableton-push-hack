module push-audio-loopback

go 1.25.0

replace github.com/federico-pepe/ableton-push-hack/core => ../../../core

require (
	github.com/federico-pepe/ableton-push-hack/core v0.0.0-00010101000000-000000000000
	golang.org/x/sys v0.47.0
)
