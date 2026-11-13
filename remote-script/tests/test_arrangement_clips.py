import json
import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
from fakes import FakeSong  # noqa: E402
from PushHack.hub import Hub  # noqa: E402
from PushHack.ns import arrangement as arr  # noqa: E402


class Note(object):
    def __init__(self, pitch, start, dur, vel=100, mute=False):
        self.pitch, self.start_time, self.duration = pitch, start, dur
        self.velocity, self.mute = vel, mute


class AClip(object):
    """An arrangement clip. `src` names the material and `off` is the
    material's time at the clip's left edge, so a test can check that the
    content stays lined up after edits."""

    def __init__(self, start, end, src="A", off=0.0, midi=True):
        self.start_time, self.end_time = float(start), float(end)
        self.src, self.off = src, float(off)
        self.is_midi_clip = midi
        self.name, self.color, self.color_index, self.muted = src, 5, 3, False
        self.looping = False
        self.start_marker = self.loop_start = float(off)
        self.end_marker = self.loop_end = float(off) + self.end_time - self.start_time
        self.notes = [Note(60, 0.0, 1.0), Note(64, 1.0, 0.5, 80)]
        self.gain, self.pitch_coarse, self.warping = 0.5, 0, True
        self.file_path, self.sample_rate, self.sample_length = "/x.wav", 44100.0, 88200
        self.applied = None
        self.quantized = None

    def content_at(self, t):
        return (self.src, self.off + t - self.start_time)

    def get_notes_extended(self, p0, ps, t0, ts):
        return [n for n in self.notes if t0 <= n.start_time < t0 + ts]

    def get_all_notes_extended(self):
        return list(self.notes)

    def apply_note_modifications(self, notes):
        self.applied = notes

    def quantize(self, grid, amount):
        self.quantized = (grid, amount)

    def crop(self):
        pass

    def __setattr__(self, k, v):
        object.__setattr__(self, k, v)
        if k == "loop_end" and not getattr(self, "looping", True) and hasattr(self, "loop_start"):
            object.__setattr__(self, "end_time", self.start_time + v - self.loop_start)


class ATrack(object):
    """A track that pastes like Live: a paste overwrites what is under it."""

    def __init__(self, name="T", midi=True):
        self.name, self.has_midi_input, self.is_foldable = name, midi, False
        self.color, self.group_track = 0, None
        self.arrangement_clips = []
        self.calls = 0

    def _sort(self):
        self.arrangement_clips.sort(key=lambda c: c.start_time)

    def _clear(self, a, b):
        out = []
        for c in self.arrangement_clips:
            if c.end_time <= a or c.start_time >= b:
                out.append(c)
                continue
            if c.start_time < a:            # left part stays, same object
                right_end = c.end_time
                c.end_time = a
                out.append(c)
                if right_end > b:
                    r = AClip(b, right_end, c.src, c.off + b - c.start_time, c.is_midi_clip)
                    out.append(r)
            elif c.end_time > b:            # right part stays, new object
                out.append(AClip(b, c.end_time, c.src, c.off + b - c.start_time, c.is_midi_clip))
        self.arrangement_clips = out

    def duplicate_clip_to_arrangement(self, clip, at):
        self.calls += 1
        length = clip.end_time - clip.start_time
        src, off, midi = clip.src, clip.off, clip.is_midi_clip
        self._clear(at, at + length)
        self.arrangement_clips.append(AClip(at, at + length, src, off, midi))
        self._sort()

    def delete_clip(self, clip):
        self.arrangement_clips.remove(clip)

    def create_midi_clip(self, start, length):
        self._clear(start, start + length)
        self.arrangement_clips.append(AClip(start, start + length, "new"))
        self._sort()

    def layout(self):
        return [(c.start_time, c.end_time, c.src, c.off) for c in self.arrangement_clips]


class EditSong(FakeSong):
    def __init__(self):
        FakeSong.__init__(self)
        self.a = ATrack("A")
        self.b = ATrack("B")
        self.tracks = [self.a, self.b]
        self.cue_points = []
        self.undo_steps = []

    def begin_undo_step(self):
        self.undo_steps.append("begin")

    def end_undo_step(self):
        self.undo_steps.append("end")


class EditTest(unittest.TestCase):
    def setUp(self):
        self.song = EditSong()
        self.song.a.arrangement_clips = [AClip(0, 8, "A"), AClip(16, 20, "N")]
        self.sent = []
        self.hub = Hub(lambda: self.song, lambda c, t: self.sent.append(json.loads(t)))

    def call(self, cmd, **kw):
        self.sent = []
        msg = {"id": 7, "ns": "arrangement", "cmd": cmd}
        msg.update(kw)
        self.hub.on_line("c", json.dumps(msg).encode())
        return self.sent[0]

    def layout(self):
        return self.song.a.layout()

    def test_snapshot_has_muted_flag(self):
        self.song.a.arrangement_clips[0].muted = True
        clips = arr.build_snapshot(self.song)["tracks"][0]["clips"]
        self.assertEqual(clips[0][4], 1)
        self.assertEqual(clips[1][4], 0)

    def test_info_for_midi_and_audio(self):
        self.song.b.arrangement_clips = [AClip(4, 6, "S", midi=False)]
        r = self.call("clips_info", clips=[{"track": 0, "start": 0}, {"track": 1, "start": 4},
                                           {"track": 0, "start": 3}])
        midi, audio, missing = r["data"]["clips"]
        self.assertEqual(midi["kind"], "midi")
        self.assertEqual(midi["notes"][1], [64, 1.0, 0.5, 80, 0])
        self.assertEqual(audio["kind"], "audio")
        self.assertEqual(audio["file"], "/x.wav")
        self.assertIsNone(missing)

    def test_info_limits_the_batch(self):
        r = self.call("clips_info", clips=[{"track": 0, "start": 0}] * 9)
        self.assertEqual(r["error"], "bad_arg")

    def test_delete(self):
        self.assertTrue(self.call("clip_delete", track=0, start=16)["ok"])
        self.assertEqual(self.layout(), [(0, 8, "A", 0)])
        self.assertEqual(self.song.undo_steps, ["begin", "end"])

    def test_unknown_clip(self):
        self.assertEqual(self.call("clip_delete", track=0, start=3)["error"], "no_clip")

    def test_move_right_overlapping_itself(self):
        r = self.call("clip_move", track=0, start=0, to=2)
        self.assertEqual(self.layout(), [(2, 10, "A", 0), (16, 20, "N", 0)])
        self.assertEqual(r["data"]["clip"], {"track": 0, "start": 2.0, "end": 10.0})

    def test_move_left_overlapping_itself(self):
        self.call("clip_move", track=0, start=16, to=14)
        self.assertEqual(self.layout(), [(0, 8, "A", 0), (14, 18, "N", 0)])

    def test_move_overwrites_what_is_there(self):
        self.call("clip_move", track=0, start=16, to=6)
        self.assertEqual(self.layout(), [(0, 6, "A", 0), (6, 10, "N", 0)])

    def test_move_to_another_track(self):
        self.call("clip_move", track=0, start=16, to=4, to_track=1)
        self.assertEqual(self.layout(), [(0, 8, "A", 0)])
        self.assertEqual(self.song.b.layout(), [(4, 8, "N", 0)])

    def test_duplicate_goes_right_after(self):
        r = self.call("clip_duplicate", track=0, start=16)
        self.assertEqual(self.layout(), [(0, 8, "A", 0), (16, 20, "N", 0), (20, 24, "N", 0)])
        self.assertEqual(r["data"]["clip"]["start"], 20.0)

    def test_split_keeps_content_in_place_and_touches_nothing_else(self):
        r = self.call("clip_split", track=0, start=0, at=3)
        self.assertTrue(r["ok"], r)
        self.assertEqual(self.layout(), [(0, 3, "A", 0), (3, 8, "A", 3), (16, 20, "N", 0)])
        self.assertEqual(r["data"]["clip"]["start"], 3.0)
        self.assertEqual(r["data"]["left"]["end"], 3.0)

    def test_split_outside_the_clip_is_refused(self):
        self.assertEqual(self.call("clip_split", track=0, start=0, at=8)["error"], "bad_arg")
        self.assertEqual(self.layout(), [(0, 8, "A", 0), (16, 20, "N", 0)])

    def test_trim_end_shorter(self):
        self.song.a.arrangement_clips[0].looping = True   # forces the paste way
        self.call("clip_trim", track=0, start=0, new_end=5)
        self.assertEqual(self.layout(), [(0, 5, "A", 0), (16, 20, "N", 0)])

    def test_trim_start_keeps_content_in_place(self):
        r = self.call("clip_trim", track=0, start=0, new_start=2)
        self.assertEqual(self.layout(), [(2, 8, "A", 2), (16, 20, "N", 0)])
        self.assertEqual(r["data"]["clip"]["start"], 2.0)

    def test_trim_both_edges(self):
        self.call("clip_trim", track=0, start=0, new_start=1, new_end=7)
        self.assertEqual(self.layout(), [(1, 7, "A", 1), (16, 20, "N", 0)])

    def test_extend_end_stops_at_the_next_clip(self):
        self.call("clip_trim", track=0, start=0, new_end=30)
        self.assertEqual(self.layout()[0][:2], (0, 16))

    def test_extend_a_looped_clip_is_refused(self):
        self.song.a.arrangement_clips[0].looping = True
        self.assertEqual(self.call("clip_trim", track=0, start=0, new_end=10)["error"], "looping")

    def test_trim_cannot_move_start_left(self):
        self.assertEqual(self.call("clip_trim", track=0, start=16, new_start=12)["error"], "bad_arg")

    def test_set_mute_and_color(self):
        r = self.call("clip_set", track=0, start=0, muted=True, color_index=99)
        self.assertEqual(sorted(r["data"]["changed"]), ["color_index", "muted"])
        c = self.song.a.arrangement_clips[0]
        self.assertTrue(c.muted)
        self.assertEqual(c.color_index, 69)

    def test_gain_on_midi_is_refused(self):
        self.assertEqual(self.call("clip_set", track=0, start=0, gain=0.3)["error"], "wrong_clip_kind")

    def test_transpose_midi_and_audio(self):
        self.call("clip_transpose", track=0, start=0, semitones=12)
        self.assertEqual([n.pitch for n in self.song.a.arrangement_clips[0].applied], [72, 76])
        self.song.b.arrangement_clips = [AClip(0, 4, "S", midi=False)]
        self.call("clip_transpose", track=1, start=0, semitones=-3)
        self.assertEqual(self.song.b.arrangement_clips[0].pitch_coarse, -3)

    def test_velocity(self):
        self.call("clip_velocity", track=0, start=0, delta=40)
        self.assertEqual([n.velocity for n in self.song.a.arrangement_clips[0].applied], [127.0, 120.0])

    def test_quantize(self):
        self.call("clip_quantize", track=0, start=0)
        self.assertEqual(self.song.a.arrangement_clips[0].quantized, (5, 1.0))

    def test_new_midi_clip(self):
        r = self.call("clip_new", track=0, start=10, length=4)
        self.assertEqual(r["data"]["clip"], {"track": 0, "start": 10.0, "end": 14.0})

    def test_new_clip_on_audio_track_is_refused(self):
        self.song.b.has_midi_input = False
        self.assertEqual(self.call("clip_new", track=1, start=0)["error"], "wrong_track_kind")


if __name__ == "__main__":
    unittest.main()
