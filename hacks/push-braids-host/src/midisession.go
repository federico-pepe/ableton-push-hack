package main

// midisession.go — owns the MIDI subscription's lifecycle, so the
// on-screen I/O page (iopage.go) can retarget it at a different ALSA seq
// source without a process restart. Mirrors audiosession.go's
// watchHWParams/startAudioSession split: a supervisor loop that opens,
// tears down, and reopens as the target (here: sharedConfig's MIDI
// client/port) changes.

import (
	"log"

	"github.com/federico-pepe/ableton-push-hack/core/alsaseq"
)

// watchMIDI opens an ALSA seq subscription to whatever sharedConfig's
// current MIDI source is, and reopens it whenever that changes (the user
// picked a different port on the I/O page). Runs until shutdown fires.
func watchMIDI(rt *sharedConfig, handler alsaseq.Handler, shutdown <-chan struct{}) {
	var seq *alsaseq.Client
	var curClient, curPort byte
	haveSeq := false

	stop := func() {
		if haveSeq {
			seq.Close() // makes the ReadLoop goroutine's blocking read fail, ending it
			haveSeq = false
		}
	}
	defer stop()

	for {
		select {
		case <-shutdown:
			return
		default:
		}

		client, port := rt.getMIDI()
		if !haveSeq || client != curClient || port != curPort {
			stop()
			newSeq, err := openMIDISource(client, port, handler)
			if err != nil {
				log.Printf("opening MIDI source %d:%d: %v — will retry", client, port, err)
				if !sleepOrStop(waitPollInterval, shutdown) {
					return
				}
				continue
			}
			seq = newSeq
			curClient, curPort = client, port
			haveSeq = true
			log.Printf("subscribed to MIDI source %d:%d for pad/button input", client, port)
		}

		if !sleepOrStop(steadyPollInterval, shutdown) {
			return
		}
	}
}

func openMIDISource(client, port byte, handler alsaseq.Handler) (*alsaseq.Client, error) {
	seq, err := alsaseq.Open()
	if err != nil {
		return nil, err
	}
	if _, err := seq.CreatePort("Push Braids Host In",
		alsaseq.CapWrite|alsaseq.CapSubsWrite, alsaseq.PortTypeMidi|alsaseq.PortTypeApp); err != nil {
		seq.Close()
		return nil, err
	}
	if err := seq.Subscribe(alsaseq.Addr{Client: client, Port: port}); err != nil {
		seq.Close()
		return nil, err
	}
	go func() {
		if err := seq.ReadLoop(handler); err != nil {
			log.Printf("MIDI read loop ended: %v", err)
		}
	}()
	return seq, nil
}
