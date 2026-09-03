module push-braids-host

go 1.25.0

require github.com/federico-pepe/ableton-push-hack/core v0.0.0

require (
	golang.org/x/image v0.41.0 // indirect
	golang.org/x/text v0.37.0 // indirect
)

replace github.com/federico-pepe/ableton-push-hack/core => ../../../core
