package main

import "sync"

// sharedConfig is persistedConfig's live, in-memory counterpart: the
// values watchMIDI/watchHWParams actually act on, and the on-screen I/O
// page (iopage.go) writes to when the user picks a new port/device.
// Changing it takes effect on those supervisors' next poll tick — no
// process restart needed — and iopage.go persists it to braids-config.json
// right after, so the choice survives a restart too.
type sharedConfig struct {
	mu             sync.Mutex
	midiClient     byte
	midiPort       byte
	pcmDevice      string
	channelOffset  int
	pushManagerURL string
}

func newSharedConfig(cfg persistedConfig) *sharedConfig {
	return &sharedConfig{
		midiClient:     cfg.MidiClient,
		midiPort:       cfg.MidiPort,
		pcmDevice:      cfg.PCMDevice,
		channelOffset:  cfg.ChannelOffset,
		pushManagerURL: cfg.PushManagerURL,
	}
}

func (s *sharedConfig) getMIDI() (client, port byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.midiClient, s.midiPort
}

func (s *sharedConfig) setMIDI(client, port byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.midiClient, s.midiPort = client, port
}

func (s *sharedConfig) getPCM() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pcmDevice
}

func (s *sharedConfig) setPCM(device string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pcmDevice = device
}

func (s *sharedConfig) getChannelOffset() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.channelOffset
}

func (s *sharedConfig) setChannelOffset(offset int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.channelOffset = offset
}

// snapshot returns the current values as a persistedConfig, ready to save.
func (s *sharedConfig) snapshot() persistedConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return persistedConfig{
		MidiClient:     s.midiClient,
		MidiPort:       s.midiPort,
		PCMDevice:      s.pcmDevice,
		ChannelOffset:  s.channelOffset,
		PushManagerURL: s.pushManagerURL,
	}
}
