# A small fake of Live's object model, enough for the namespace tests.
from fakes import FakeSong


class Param(object):
    def __init__(self, name, value=0.0, lo=0.0, hi=1.0, quantized=False, items=None):
        self.name, self.value, self.min, self.max = name, value, lo, hi
        self.default_value = value
        self.is_quantized = quantized
        self.value_items = items or []
        self.automation_state = 0
        self.is_enabled = True

    def str_for_value(self, v):
        return "%.2f" % v


class Mixer(object):
    def __init__(self, sends=2):
        self.volume = Param("Volume", 0.85)
        self.panning = Param("Pan", 0.0, -1.0, 1.0)
        self.sends = [Param("Send %d" % i, 0.0) for i in range(sends)]
        self.track_activator = Param("Track Activator", 1.0)


class Clip(object):
    def __init__(self, name="Clip", midi=True, length=4.0):
        self.name, self.color, self.length = name, 0x112233, length
        self.is_midi_clip = midi
        self.is_audio_clip = not midi
        self.looping, self.muted = True, False
        self.loop_start, self.loop_end = 0.0, length
        self.start_marker, self.end_marker = 0.0, length
        self.is_playing = self.is_triggered = self.is_recording = False
        self.playing_position = 0.0
        self.signature_numerator = self.signature_denominator = 4
        self.launch_mode = self.launch_quantization = 0
        self.legato = False
        self.has_envelopes = False
        self.velocity_amount = 0.0
        self.gain, self.pitch_coarse, self.pitch_fine = 0.5, 0, 0
        self.warping, self.warp_mode = True, 0
        self.gain_display_string = "-6 dB"
        self.file_path = "/x/a.wav"
        self.notes = []
        self.calls = []

    def fire(self): self.calls.append("fire"); self.is_triggered = True
    def stop(self): self.calls.append("stop")
    def crop(self): self.calls.append("crop")
    def duplicate_loop(self): self.calls.append("duplicate_loop")
    def quantize(self, grid, amount): self.calls.append(("quantize", grid, amount))

    def get_notes_extended(self, pitch, span, t, tspan):
        return [n for n in self.notes if pitch <= n.pitch < pitch + span and t <= n.start_time < t + tspan]

    def remove_notes_extended(self, pitch, span, t, tspan):
        keep = [n for n in self.notes if n not in self.get_notes_extended(pitch, span, t, tspan)]
        self.notes = keep


class Note(object):
    def __init__(self, i, pitch, start, dur=0.25, vel=100.0):
        self.note_id, self.pitch, self.start_time, self.duration = i, pitch, start, dur
        self.velocity, self.mute, self.probability = vel, False, 1.0


class Slot(object):
    def __init__(self, clip=None):
        self.clip = clip
        self.is_playing = self.is_triggered = False
        self.calls = []

    @property
    def has_clip(self):
        return self.clip is not None

    def fire(self): self.calls.append("fire")
    def stop(self): self.calls.append("stop")

    def create_clip(self, length):
        self.clip = Clip("new", True, length)

    def delete_clip(self):
        self.clip = None


class Scene(object):
    def __init__(self, name):
        self.name, self.color, self.is_triggered, self.tempo = name, 0, False, 0.0
        self.calls = []

    def fire(self): self.calls.append("fire")
    def fire_as_selected(self): self.calls.append("fire_as_selected")


class Device(object):
    def __init__(self, name, params=None, chains=None, drum_pads=None):
        self.name, self.class_name, self.type, self.is_active = name, name, 1, True
        self.parameters = params or [Param("Device On", 1.0, 0.0, 1.0, True, ["Off", "On"]),
                                     Param("Gain", 0.5), Param("Mode", 1.0, 0.0, 3.0, True, ["a", "b", "c", "d"])]
        self.chains = chains or []
        self.can_have_drum_pads = drum_pads is not None
        self.drum_pads = drum_pads or []
        self.view = type("V", (), {"is_collapsed": False, "selected_drum_pad": None})()


class Chain(object):
    def __init__(self, name, devices=None):
        self.name, self.color, self.mute, self.solo = name, 5, False, False
        self.devices = devices or []

    def delete_device(self, i):
        del self.devices[i]


class Pad(object):
    def __init__(self, note, name="Pad"):
        self.note, self.name, self.mute, self.solo, self.chains = note, name, False, False, [object()]


class Track(object):
    def __init__(self, name, midi=True, slots=4):
        self.name, self.color = name, 0xFF0000
        self.has_midi_input, self.has_audio_input = midi, not midi
        self.is_foldable = False
        self.fold_state = 0
        self.group_track = None
        self.mute = self.solo = self.arm = False
        self.can_be_armed = True
        self.is_frozen = False
        self.is_visible = True
        self.current_monitoring_state = 1
        self.mixer_device = Mixer()
        self.clip_slots = [Slot() for _ in range(slots)]
        self.devices = []
        self.playing_slot_index = self.fired_slot_index = -1
        self.output_meter_left = self.output_meter_right = 0.0
        self.input_routing_type = type("R", (), {"display_name": "All Ins"})()
        self.input_routing_channel = type("R", (), {"display_name": "All"})()
        self.output_routing_type = type("R", (), {"display_name": "Master"})()
        self.output_routing_channel = type("R", (), {"display_name": "Track In"})()
        self.available_input_routing_types = [self.input_routing_type]
        self.available_output_routing_types = [self.output_routing_type]
        self.view = type("V", (), {"selected_device": None})()
        self.calls = []

    def stop_all_clips(self): self.calls.append("stop_all_clips")
    def delete_device(self, i): del self.devices[i]


class MasterTrack(object):
    def __init__(self):
        self.name, self.color = "Master", 0
        self.mixer_device = Mixer(0)
        self.devices = []
        self.output_meter_left = self.output_meter_right = 0.0
        self.output_routing_type = type("R", (), {"display_name": "Ext"})()
        self.output_routing_channel = type("R", (), {"display_name": "1/2"})()
        self.available_output_routing_types = []
        self.mute = self.solo = False
        self.view = type("V", (), {"selected_device": None})()


class View(object):
    def __init__(self, song):
        self.selected_track = song.tracks[0]
        self.selected_scene = song.scenes[0]
        self.detail_clip = None
        self.highlighted_clip_slot = None
        self.follow_song = True
        self.draw_mode = False
        self.selected = []

    def select_device(self, dev):
        self.selected.append(dev)


class LomSong(FakeSong):
    def __init__(self):
        FakeSong.__init__(self)
        self.tracks = [Track("Bass"), Track("Keys"), Track("Audio", midi=False)]
        self.return_tracks = [Track("A Reverb")]
        self.master_track = MasterTrack()
        self.scenes = [Scene("Intro"), Scene("Verse"), Scene("Chorus")]
        self.view = View(self)
        self.metronome = self.record_mode = self.session_record = False
        self.arrangement_overdub = False
        self.is_counting_in = False
        self.can_undo = self.can_redo = False
        self.can_jump_to_next_cue = self.can_jump_to_prev_cue = False
        self.can_capture_midi = False
        self.groove_amount = self.swing_amount = 0.0
        self.root_note, self.scale_name, self.scale_mode = 0, "Major", False
        self.scale_intervals = [0, 2, 4, 5, 7, 9, 11]
        self.calls = []

    def stop_all_clips(self): self.calls.append("stop_all_clips")
    def capture_and_insert_scene(self): self.calls.append("capture"); self.scenes.append(Scene("Captured"))
    def create_midi_track(self, i): self.tracks.append(Track("MIDI"))
    def create_audio_track(self, i): self.tracks.append(Track("Audio", midi=False))
    def create_return_track(self): self.return_tracks.append(Track("B"))
    def create_scene(self, i): self.scenes.append(Scene("New"))
    def duplicate_scene(self, i): self.calls.append(("dup_scene", i))
    def delete_scene(self, i): del self.scenes[i]
    def move_device(self, dev, dest, pos): self.calls.append(("move", dev.name, pos))
    def undo(self): self.calls.append("undo")
    def redo(self): self.calls.append("redo")
    def tap_tempo(self): self.calls.append("tap")
    def continue_playing(self): self.calls.append("continue")
