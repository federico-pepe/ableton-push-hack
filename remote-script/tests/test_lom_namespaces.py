import json
import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
from lom import (Chain, Clip, Device, LomSong, Note, Pad, Param,  # noqa: E402
                 Slot)
from PushHack.hub import Hub  # noqa: E402


class Base(unittest.TestCase):
    def setUp(self):
        self.song = LomSong()
        self.sent = []
        self.hub = Hub(lambda: self.song, lambda c, t: self.sent.append(json.loads(t)))

    def call(self, ns, cmd, **kw):
        self.sent = []
        msg = {"id": 1, "ns": ns, "cmd": cmd}
        msg.update(kw)
        self.hub.on_line("c", json.dumps(msg).encode())
        return self.sent[0]

    def ok(self, ns, cmd, **kw):
        r = self.call(ns, cmd, **kw)
        self.assertTrue(r["ok"], r)
        return r.get("data")

    def fails(self, ns, cmd, code, **kw):
        r = self.call(ns, cmd, **kw)
        self.assertFalse(r["ok"], r)
        self.assertEqual(r["error"], code)


class TrackTest(Base):
    def test_list_has_tracks_returns_master_and_refs(self):
        d = self.ok("track", "list")
        self.assertEqual([t["name"] for t in d["tracks"]], ["Bass", "Keys", "Audio"])
        self.assertEqual([t["kind"] for t in d["tracks"]], ["midi", "midi", "audio"])
        self.assertEqual(d["returns"][0]["ref"], "return:0")
        self.assertEqual(d["master"]["ref"], "master")
        self.assertEqual(d["selected"], 0)
        self.assertEqual(d["tracks"][0]["volume"]["v"], 0.85)
        self.assertNotIn("arm", d["master"])

    def test_references(self):
        self.assertEqual(self.ok("track", "get", track="return:0")["name"], "A Reverb")
        self.assertEqual(self.ok("track", "get", track="master")["name"], "Master")
        self.assertEqual(self.ok("track", "get", track="selected")["name"], "Bass")
        self.fails("track", "get", "bad_track", track=9)
        self.fails("track", "get", "bad_track", track=-1)
        self.fails("track", "get", "bad_track", track="return:5")
        self.fails("track", "get", "bad_track", track="nope")
        self.fails("track", "get", "bad_track", track=True)
        self.fails("track", "get", "bad_track")

    def test_set_switches_and_clamps(self):
        self.ok("track", "set", track=1, mute=True, solo=True, arm=True, name="Lead", color=0xABCDEF)
        t = self.song.tracks[1]
        self.assertTrue(t.mute and t.solo and t.arm)
        self.assertEqual((t.name, t.color), ("Lead", 0xABCDEF))
        self.ok("track", "set", track=1, volume=5, pan=-9)
        self.assertEqual((t.mixer_device.volume.value, t.mixer_device.panning.value), (1.0, -1.0))
        self.ok("track", "set", track=1, send={"index": 1, "value": 0.4})
        self.assertEqual(t.mixer_device.sends[1].value, 0.4)

    def test_set_refusals(self):
        self.fails("track", "set", "no_changes", track=0)
        self.fails("track", "set", "bad_arg", track=0, send={"index": 7, "value": 0.1})
        self.fails("track", "set", "bad_arg", track=0, monitoring=5)
        self.fails("track", "set", "bad_arg", track=0, volume="loud")
        self.fails("track", "set", "cannot_arm", track="master", arm=True)
        self.fails("track", "set", "cannot_arm", track="return:0", arm=True)
        self.fails("track", "set", "bad_arg", track="master", mute=True)
        self.song.tracks[0].can_be_armed = False
        self.fails("track", "set", "cannot_arm", track=0, arm=True)

    def test_select_stop_create(self):
        self.ok("track", "select", track=2)
        self.assertIs(self.song.view.selected_track, self.song.tracks[2])
        self.ok("track", "stop", track=0)
        self.assertEqual(self.song.tracks[0].calls, ["stop_all_clips"])
        self.fails("track", "stop", "bad_track", track="master")
        d = self.ok("track", "create", kind="midi")
        self.assertEqual(d["tracks"], 4)
        self.fails("track", "create", "bad_arg", kind="video")

    def test_meters_topic_and_get(self):
        self.song.tracks[0].output_meter_left = 0.456
        d = self.ok("meters", "get")
        self.assertEqual(d["tracks"][0], [0.46, 0.0])
        self.assertEqual(len(d["returns"]), 1)

    def test_tracks_topic_is_throttled(self):
        self.ok_sub = self.call("", "subscribe") if False else None
        self.hub.on_line("c", b'{"cmd":"subscribe","topics":["tracks"]}')
        self.sent = []
        for _ in range(5):
            self.hub.tick()
        self.assertEqual(len([m for m in self.sent if m.get("t") == "event"]), 1)
        self.song.tracks[0].name = "Renamed"
        self.sent = []
        for _ in range(5):
            self.hub.tick()
        ev = [m for m in self.sent if m.get("t") == "event"]
        self.assertEqual(len(ev), 1)
        self.assertEqual(ev[0]["data"]["tracks"][0]["name"], "Renamed")


class SessionTest(Base):
    def setUp(self):
        Base.setUp(self)
        c = Clip("Beat", midi=True, length=8.0)
        self.song.tracks[0].clip_slots[1] = Slot(c)
        self.song.tracks[1].clip_slots[2] = Slot(Clip("Pad"))

    def test_grid_shape_and_states(self):
        self.song.tracks[0].clip_slots[1].is_playing = True
        d = self.ok("session", "get")
        self.assertEqual(len(d["grid"]), 3)       # scenes
        self.assertEqual(len(d["grid"][0]), 3)    # tracks
        self.assertEqual(d["grid"][0][0], 0)
        self.assertEqual(d["grid"][1][0], [0x112233, "Beat", 1, 8.0])
        self.assertEqual(d["scenes"][2]["name"], "Chorus")
        self.assertEqual((d["total_tracks"], d["total_scenes"]), (3, 3))

    def test_window_and_limits(self):
        d = self.ok("session", "get", track_offset=1, track_count=1, scene_offset=2, scene_count=5)
        self.assertEqual(len(d["grid"]), 1)
        self.assertEqual(d["grid"][0], [[0x112233, "Pad", 0, 4.0]])
        self.fails("session", "get", "too_big", track_count=100, scene_count=100)

    def test_recording_and_triggered(self):
        c = self.song.tracks[0].clip_slots[1]
        c.is_triggered = True
        self.assertEqual(self.ok("session", "get")["grid"][1][0][2], 2)
        c.clip.is_recording = True
        self.assertEqual(self.ok("session", "get")["grid"][1][0][2], 3)

    def test_fire_stop_scene(self):
        self.ok("session", "fire_clip", track=0, scene=1)
        self.ok("session", "stop_clip", track=0, scene=1)
        self.assertEqual(self.song.tracks[0].clip_slots[1].calls, ["fire", "stop"])
        self.ok("session", "fire_scene", scene=2)
        self.ok("session", "fire_scene", scene=0, as_selected=True)
        self.assertEqual(self.song.scenes[2].calls, ["fire"])
        self.assertEqual(self.song.scenes[0].calls, ["fire_as_selected"])
        self.ok("session", "stop_all")
        self.assertEqual(self.song.calls, ["stop_all_clips"])
        self.fails("session", "fire_clip", "bad_scene", track=0, scene=9)
        self.fails("session", "fire_clip", "bad_track", track=7, scene=0)
        self.fails("session", "fire_clip", "bad_track", track="master", scene=0)
        self.fails("session", "fire_scene", "bad_scene", scene=-1)

    def test_create_and_delete_clip(self):
        self.fails("session", "create_clip", "slot_taken", track=0, scene=1)
        self.ok("session", "create_clip", track=0, scene=0, length=2)
        self.assertEqual(self.song.tracks[0].clip_slots[0].clip.length, 2)
        self.fails("session", "create_clip", "bad_arg", track=2, scene=0, length=0)
        self.ok("session", "delete_clip", track=0, scene=0)
        self.fails("session", "delete_clip", "no_clip", track=0, scene=0)

    def test_scenes(self):
        self.ok("session", "capture_scene")
        self.assertEqual(self.song.scenes[-1].name, "Captured")
        self.ok("session", "select_scene", scene=1)
        self.assertIs(self.song.view.selected_scene, self.song.scenes[1])
        self.ok("session", "set_scene", scene=0, name="Opening", color=0x00FF00, tempo=130)
        s = self.song.scenes[0]
        self.assertEqual((s.name, s.color, s.tempo), ("Opening", 0x00FF00, 130.0))
        self.fails("session", "set_scene", "bad_arg", scene=0, tempo=5000)
        self.fails("session", "set_scene", "no_changes", scene=0)
        self.ok("session", "duplicate_scene", scene=0)
        self.fails("session", "delete_scene", "bad_scene", scene=9)
        self.ok("session", "delete_scene", scene=0)
        self.assertEqual(len(self.song.scenes), 3)   # 3, +1 captured, -1 deleted

    def test_the_last_scene_cannot_be_deleted(self):
        self.song.scenes = self.song.scenes[:1]
        self.fails("session", "delete_scene", "bad_scene", scene=0)

    def test_select_clip_slot(self):
        self.ok("session", "select_clip_slot", track=1, scene=2)
        self.assertIs(self.song.view.highlighted_clip_slot, self.song.tracks[1].clip_slots[2])
        self.assertIs(self.song.view.selected_track, self.song.tracks[1])


class DeviceTest(Base):
    def setUp(self):
        Base.setUp(self)
        self.inner = Device("Inner")
        rack = Device("Rack", chains=[Chain("c0", [self.inner]), Chain("c1")])
        self.synth = Device("Synth")
        self.song.tracks[0].devices = [self.synth, rack]
        self.song.tracks[0].view.selected_device = self.synth

    def test_list_and_rack_contents(self):
        d = self.ok("device", "list", track=0)["devices"]
        self.assertEqual([x["name"] for x in d], ["Synth", "Rack"])
        self.assertEqual(d[1]["chains"], 2)
        inner = self.ok("device", "list", track=0, path=[1], chain=0)["devices"]
        self.assertEqual([x["name"] for x in inner], ["Inner"])
        self.fails("device", "list", "bad_device", track=0, path=[1], chain=9)

    def test_params_page(self):
        d = self.ok("device", "params", track=0, device=0)
        self.assertEqual(d["total"], 3)
        self.assertEqual(d["params"][0]["name"], "Device On")
        self.assertEqual(d["params"][2]["items"], ["a", "b", "c", "d"])
        d = self.ok("device", "params", track=0, device=0, offset=2, count=5)
        self.assertEqual([p["i"] for p in d["params"]], [2])

    def test_set_param_raw_norm_quantized_clamped(self):
        self.ok("device", "set_param", track=0, device=0, index=1, value=0.9)
        self.assertEqual(self.synth.parameters[1].value, 0.9)
        self.ok("device", "set_param", track=0, device=0, index=1, value=7)
        self.assertEqual(self.synth.parameters[1].value, 1.0)
        self.ok("device", "set_param", track=0, device=0, index=1, norm=0.25)
        self.assertEqual(self.synth.parameters[1].value, 0.25)
        self.ok("device", "set_param", track=0, device=0, index=2, norm=0.5)
        self.assertEqual(self.synth.parameters[2].value, 2.0)   # 1.5 rounds, quantized
        self.fails("device", "set_param", "bad_param", track=0, device=0, index=9, value=0)
        self.fails("device", "set_param", "bad_arg", track=0, device=0, index=1)
        self.fails("device", "set_param", "bad_arg", track=0, device=0, index=1, value="x")
        self.synth.parameters[1].is_enabled = False
        self.fails("device", "set_param", "param_disabled", track=0, device=0, index=1, value=0.3)

    def test_inside_a_rack_by_path(self):
        d = self.ok("device", "params", track=0, path=[1, 0, 0])
        self.assertEqual(d["name"], "Inner")
        self.fails("device", "params", "bad_device", track=0, path=[1, 0])        # ends on a chain
        self.fails("device", "params", "bad_device", track=0, path=[1, 5, 0])
        self.fails("device", "params", "bad_device", track=0, path=[])
        self.fails("device", "params", "bad_device", track=0, device=7)

    def test_active_select_selected(self):
        self.ok("device", "set_active", track=0, device=0, on=False)
        self.assertEqual(self.synth.parameters[0].value, 0.0)
        self.ok("device", "select", track=0, device=1)
        self.assertEqual(self.song.view.selected[-1].name, "Rack")
        self.assertEqual(self.ok("device", "selected"), {"track": 0, "device": 0, "name": "Synth"})
        self.song.tracks[0].view.selected_device = None
        self.assertEqual(self.ok("device", "selected")["device"], -1)

    def test_selected_topic_carries_params(self):
        self.hub.on_line("c", b'{"cmd":"subscribe","topics":["device"]}')
        self.sent = []
        self.hub.tick()
        ev = [m for m in self.sent if m.get("t") == "event"][0]["data"]
        self.assertEqual(ev["name"], "Synth")
        self.assertEqual(len(ev["params"]), 3)

    def test_delete_move_chains(self):
        self.ok("device", "delete", track=0, device=0)
        self.assertEqual([d.name for d in self.song.tracks[0].devices], ["Rack"])
        self.fails("device", "delete", "bad_device", track=0, device=5)
        self.ok("device", "delete", track=0, path=[0, 0, 0])
        self.assertEqual(self.inner.name, "Inner")
        self.assertEqual(self.song.tracks[0].devices[0].chains[0].devices, [])
        self.ok("device", "move", track=0, device=0, to_index=1)
        self.assertEqual(self.song.calls[-1], ("move", "Rack", 1))
        self.fails("device", "move", "bad_device", track=0, device=0, to_index=9)
        cs = self.ok("device", "chains", track=0, device=0)["chains"]
        self.assertEqual([c["name"] for c in cs], ["c0", "c1"])
        self.ok("device", "set_chain", track=0, device=0, chain=1, mute=True, name="x")
        self.assertTrue(self.song.tracks[0].devices[0].chains[1].mute)
        self.fails("device", "set_chain", "no_changes", track=0, device=0, chain=1)
        self.fails("device", "set_chain", "bad_device", track=0, device=0, chain=9, mute=True)


class ClipTest(Base):
    def setUp(self):
        Base.setUp(self)
        self.midi = Clip("M", midi=True, length=4.0)
        self.audio = Clip("A", midi=False, length=8.0)
        self.song.tracks[0].clip_slots[0] = Slot(self.midi)
        self.song.tracks[2].clip_slots[0] = Slot(self.audio)
        self.midi.notes = [Note(1, 60, 0.0), Note(2, 64, 1.0), Note(3, 67, 2.5)]

    def test_get_midi_and_audio(self):
        m = self.ok("clip", "get", track=0, scene=0)
        self.assertEqual((m["kind"], m["length"], m["name"]), ("midi", 4.0, "M"))
        self.assertIn("velocity_amount", m)
        self.assertNotIn("gain", m)
        a = self.ok("clip", "get", track=2, scene=0)
        self.assertEqual((a["kind"], a["gain_display"], a["warping"]), ("audio", "-6 dB", True))
        self.fails("clip", "get", "no_clip", track=1, scene=0)
        self.fails("clip", "get", "no_clip", detail=True)

    def test_detail_clip(self):
        self.song.view.detail_clip = self.audio
        self.assertEqual(self.ok("clip", "get", detail=True)["name"], "A")

    def test_set_whitelist_and_kinds(self):
        self.ok("clip", "set", track=0, scene=0, name="Hooks", looping=False, loop_end=2.0, muted=True)
        self.assertEqual((self.midi.name, self.midi.looping, self.midi.loop_end, self.midi.muted),
                         ("Hooks", False, 2.0, True))
        self.fails("clip", "set", "wrong_clip_kind", track=0, scene=0, gain=0.3)
        self.fails("clip", "set", "wrong_clip_kind", track=2, scene=0, velocity_amount=0.1)
        self.ok("clip", "set", track=2, scene=0, gain=3, pitch_coarse=99, warping=False)
        self.assertEqual((self.audio.gain, self.audio.pitch_coarse, self.audio.warping), (1.0, 48, False))
        self.fails("clip", "set", "no_changes", track=0, scene=0)
        self.fails("clip", "set", "no_changes", track=0, scene=0, evil="x")

    def test_fire_stop_crop_dup_quantize(self):
        self.ok("clip", "fire", track=0, scene=0)
        self.ok("clip", "stop", track=0, scene=0)
        self.ok("clip", "crop", track=0, scene=0)
        self.ok("clip", "duplicate_loop", track=0, scene=0)
        self.ok("clip", "quantize", track=0, scene=0, grid=5, amount=2)
        self.assertEqual(self.midi.calls, ["fire", "stop", "crop", "duplicate_loop", ("quantize", 5, 1.0)])
        self.fails("clip", "quantize", "wrong_clip_kind", track=2, scene=0, grid=5)
        self.midi.looping = False
        self.fails("clip", "duplicate_loop", "not_looping", track=0, scene=0)

    def test_notes_range_and_remove(self):
        d = self.ok("clip", "notes", track=0, scene=0)
        self.assertEqual([n["pitch"] for n in d["notes"]], [60, 64, 67])
        d = self.ok("clip", "notes", track=0, scene=0, from_pitch=62, pitch_span=4, time_span=2)
        self.assertEqual([n["pitch"] for n in d["notes"]], [64])
        self.ok("clip", "remove_notes", track=0, scene=0, from_pitch=60, pitch_span=1)
        self.assertEqual([n.pitch for n in self.midi.notes], [64, 67])
        self.fails("clip", "notes", "wrong_clip_kind", track=2, scene=0)

    def test_add_notes_validates_before_touching_live(self):
        self.fails("clip", "add_notes", "bad_arg", track=0, scene=0)
        self.fails("clip", "add_notes", "bad_arg", track=0, scene=0, notes=[])
        self.fails("clip", "add_notes", "bad_arg", track=0, scene=0, notes="x")
        self.fails("clip", "add_notes", "wrong_clip_kind", track=2, scene=0, notes=[{"pitch": 60, "start": 0}])
        # No Live here: a valid request reaches the spec class and fails cleanly.
        self.fails("clip", "add_notes", "failed", track=0, scene=0, notes=[{"pitch": 60, "start": 0}])

    def test_convert_refuses_midi_and_unknown_kind(self):
        self.fails("clip", "convert", "wrong_clip_kind", track=0, scene=0, kind="drums")
        # Without Live.Conversions an audio clip still offers the two safe ones.
        self.assertEqual(self.ok("clip", "convert_options", track=2, scene=0)["options"], ["simpler", "drum_rack"])
        self.assertEqual(self.ok("clip", "convert_options", track=0, scene=0), {"options": []})


class NotesAndModesTest(Base):
    def setUp(self):
        Base.setUp(self)
        self.pads = [Pad(36, "Kick"), Pad(38, "Snare")]
        self.drums = Device("Drum Rack", drum_pads=self.pads)
        self.song.tracks[0].devices = [self.drums, Device("Synth")]

    def test_drum_pads(self):
        d = self.ok("notes", "drum_pads", track=0, device=0)["pads"]
        self.assertEqual([(p["note"], p["name"]) for p in d], [(36, "Kick"), (38, "Snare")])
        self.pads[1].chains = []
        self.assertEqual([p["note"] for p in self.ok("notes", "drum_pads", track=0, device=0, filled=True)["pads"]], [36])
        self.pads[1].chains = [object()]
        self.fails("notes", "drum_pads", "not_a_drum_rack", track=0, device=1)
        self.ok("notes", "set_drum_pad", track=0, device=0, note=38, mute=True, solo=True)
        self.assertTrue(self.pads[1].mute and self.pads[1].solo)
        self.fails("notes", "set_drum_pad", "bad_pad", track=0, device=0, note=99, mute=True)
        self.fails("notes", "set_drum_pad", "no_changes", track=0, device=0, note=36)
        self.ok("notes", "select_drum_pad", track=0, device=0, note=36)
        self.assertIs(self.drums.view.selected_drum_pad, self.pads[0])
        self.assertTrue(self.ok("notes", "drum_pads", track=0, device=0)["pads"][0]["selected"])

    def test_slices_need_a_simpler(self):
        self.fails("notes", "slices", "not_a_simpler", track=0, device=1)
        self.fails("notes", "set_simpler", "not_a_simpler", track=0, device=1, playback_mode=2)

    def test_simpler_modes_and_the_slicing_check(self):
        simpler = Device("Simpler")
        simpler.playback_mode = 0
        simpler.sample = type("S", (), {"slices": [0, 100, 200], "length": 300})()
        self.song.tracks[1].devices = [simpler]
        d = self.ok("notes", "slices", track=1, device=0)
        self.assertEqual((d["playback_mode"], d["slices"], d["length"]), (0, [0, 100, 200], 300))
        self.fails("notes", "slices_to_drum_rack", "not_sliced", track=1, device=0)
        self.fails("notes", "set_simpler", "bad_arg", track=1, device=0, playback_mode=7)
        self.assertEqual(self.ok("notes", "set_simpler", track=1, device=0, playback_mode=2)["playback_mode"], 2)
        self.assertEqual(simpler.playback_mode, 2)
        # In slicing mode the request reaches Live.Conversions; without Live it fails cleanly.
        self.fails("notes", "slices_to_drum_rack", "failed", track=1, device=0)

    def test_set_scale(self):
        self.ok("notes", "set_scale", root=2, name="Minor", mode=True)
        self.assertEqual((self.song.root_note, self.song.scale_name, self.song.scale_mode), (2, "Minor", True))
        self.fails("notes", "set_scale", "bad_arg", root=12)
        self.fails("notes", "set_scale", "no_changes")

    def test_selection_without_the_application(self):
        # No Live here: the view-less part of the snapshot still works.
        from PushHack.ns import modes
        d = modes.selection(self.song, view=None)
        self.assertEqual((d["track"], d["scene"], d["device"]), (0, 0, -1))
        self.assertIsNone(d["detail_clip"])
        self.assertTrue(d["follow_song"])
        self.song.view.detail_clip = Clip("Open", midi=False)
        self.assertEqual(modes.selection(self.song, view=None)["detail_clip"], {"name": "Open", "kind": "audio"})

    def test_modes_views_are_whitelisted(self):
        self.fails("modes", "focus", "bad_view", view="Settings")
        self.fails("modes", "focus", "bad_view")
        self.ok("modes", "set", follow_song=False, draw_mode=True)
        self.assertEqual((self.song.view.follow_song, self.song.view.draw_mode), (False, True))
        self.fails("modes", "set", "no_changes")


class TransportExtrasTest(Base):
    def test_set_undo_tap_jump(self):
        self.ok("transport", "set", metronome=True, record_mode=True, overdub=True, groove_amount=3, swing=-1)
        s = self.song
        self.assertEqual((s.metronome, s.record_mode, s.arrangement_overdub), (True, True, True))
        self.assertEqual((s.groove_amount, s.swing_amount), (1.0, 0.0))
        self.fails("transport", "set", "no_changes")
        self.fails("transport", "undo", "nothing_to_undo")
        s.can_undo = s.can_redo = True
        self.ok("transport", "undo")
        self.ok("transport", "redo")
        self.ok("transport", "tap_tempo")
        self.ok("transport", "continue")
        self.assertEqual(s.calls, ["undo", "redo", "tap", "continue"])
        self.fails("transport", "jump_cue", "no_cue", to="next")
        self.fails("transport", "capture_midi", "nothing_to_capture")

    def test_get_reports_the_new_state(self):
        self.song.metronome = True
        d = self.ok("transport", "get")
        self.assertTrue(d["metronome"])
        self.assertIn("can_undo", d)


class TopicThrottleTest(unittest.TestCase):
    def test_slow_topic_waits_longer(self):
        song = LomSong()
        times = iter([0.0, 0.5] + [0.0] * 50)   # the first build takes 0.5 s
        sent = []
        hub = Hub(lambda: song, lambda c, t: sent.append(json.loads(t)), clock=lambda: next(times))
        hub.on_line("c", b'{"cmd":"subscribe","topics":["meters"]}')
        hub.tick()
        self.assertEqual(hub._tstate["meters"][1] - hub._tstate["meters"][0], 100)

    def test_a_failing_topic_is_logged_and_retried_later(self):
        song = LomSong()
        logs = []
        hub = Hub(lambda: song, lambda c, t: None, log=logs.append)
        hub.registry.topic("boom", lambda s: 1 / 0, every=2)
        hub.on_line("c", b'{"cmd":"subscribe","topics":["boom"]}')
        for _ in range(4):
            hub.tick()
        self.assertTrue(any("boom" in l for l in logs))


if __name__ == "__main__":
    unittest.main()
