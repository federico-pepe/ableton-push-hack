import json
import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
from fakes import FakeSong, FakeTrack  # noqa: E402
from PushHack.hub import Hub  # noqa: E402
from PushHack.ns import arrangement as arr  # noqa: E402


class Clip(object):
    def __init__(self, s, e, name="c", color=5):
        self.start_time, self.end_time, self.name, self.color = s, e, name, color


class Cue(object):
    def __init__(self, t, name):
        self.time, self.name = t, name


class ArrSong(FakeSong):
    def __init__(self):
        FakeSong.__init__(self)
        grp = FakeTrack("Group")
        grp.is_foldable = True
        t = FakeTrack("Bass")
        t.group_track = grp
        t.arrangement_clips = [Clip(0, 4.5, "A", 3)]
        self.tracks = [grp, t, FakeTrack("Aud", midi=False)]
        self.cue_points = [Cue(8, "Drop")]
        self.back_to_arranger = 1


class SnapshotTest(unittest.TestCase):
    def test_snapshot_shape(self):
        s = arr.build_snapshot(ArrSong())
        self.assertEqual([t["kind"] for t in s["tracks"]], ["group", "midi", "audio"])
        self.assertEqual(s["tracks"][1]["group"], 0)
        self.assertEqual(s["tracks"][1]["clips"], [[0.0, 4.5, "A", 3]])
        self.assertEqual(s["tracks"][0]["clips"], [])
        self.assertEqual(s["locators"], [{"time": 8.0, "name": "Drop"}])
        self.assertEqual(s["sig"], [4, 4])
        self.assertNotIn("t", s)

    def test_position_has_loop_and_punch(self):
        p = arr.build_position(ArrSong())
        self.assertTrue(p["bta"])
        self.assertEqual(p["loop_length"], 4.0)
        self.assertEqual(set(p), {"time", "playing", "bta", "loop_on", "loop_start",
                                  "loop_length", "punch_in", "punch_out"})

    def test_a_broken_track_does_not_break_the_snapshot(self):
        song = ArrSong()
        song.tracks[1].arrangement_clips = None
        self.assertEqual(arr.build_snapshot(song)["tracks"][1]["clips"], [])


class FeedTest(unittest.TestCase):
    def test_rebuilds_about_once_a_second(self):
        calls = []
        feed = arr.Feed(clock=lambda: 0.0)
        song = ArrSong()
        orig = arr.build_snapshot
        arr.build_snapshot = lambda s: calls.append(1) or orig(s)
        try:
            for _ in range(arr.SNAPSHOT_EVERY_TICKS * 2 - 1):
                feed.snapshot(song)
        finally:
            arr.build_snapshot = orig
        self.assertEqual(len(calls), 2)

    def test_slow_build_waits_longer(self):
        times = iter([0.0, 0.5, 100.0, 100.0])
        feed = arr.Feed(clock=lambda: next(times))
        feed.snapshot(ArrSong())       # took 0.5 s -> wait 100 ticks
        self.assertEqual(feed._next - feed._tick, 100)

    def test_invalidate_forces_a_rebuild(self):
        feed = arr.Feed(clock=lambda: 0.0)
        song = ArrSong()
        a = feed.snapshot(song)
        song.tempo = 99.0
        self.assertEqual(feed.snapshot(song), a)      # cached
        feed.invalidate()
        self.assertEqual(feed.snapshot(song)["tempo"], 99.0)


class CompatCommandsTest(unittest.TestCase):
    def test_coalesced_only_last_applies(self):
        song = ArrSong()
        n = arr.apply_compat(song, [
            {"t": "set_time", "v": 1}, {"t": "set_time", "v": 9},
            {"t": "set_loop", "start": 2, "length": 3}, {"t": "set_loop", "start": 8, "length": 16},
            {"t": "play_toggle"}])
        self.assertEqual(n, 3)
        self.assertEqual(song.current_song_time, 9.0)
        self.assertEqual((song.loop_start, song.loop_length), (8.0, 16.0))
        self.assertTrue(song.is_playing)

    def test_toggles_and_bta_and_unknown(self):
        song = ArrSong()
        arr.apply_compat(song, [{"t": "loop_toggle"}, {"t": "punch_in_toggle"},
                                {"t": "punch_out_toggle"}, {"t": "bta"}, {"t": "zzz"}, {}])
        self.assertTrue(song.loop and song.punch_in and song.punch_out)
        self.assertEqual(song.back_to_arranger, 0)


class HubCompatTest(unittest.TestCase):
    class Client(object):
        tag = "arrangement"

    def setUp(self):
        self.song = ArrSong()
        self.sent = []
        self.hub = Hub(lambda: self.song, lambda c, t: self.sent.append(json.loads(t)))
        self.client = self.Client()
        self.hub.on_connect(self.client)

    def test_connect_streams_snapshot_then_pos_without_asking(self):
        self.hub.tick()
        self.assertEqual([m["t"] for m in self.sent], ["snapshot", "pos"])

    def test_only_changes_after_that(self):
        self.hub.tick()
        self.sent = []
        self.hub.tick()
        self.assertEqual(self.sent, [])
        self.song.current_song_time = 7.0
        self.hub.tick()
        self.assertEqual([m["t"] for m in self.sent], ["pos"])
        self.assertEqual(self.sent[0]["time"], 7.0)

    def test_commands_apply_on_the_next_tick_coalesced(self):
        for v in (1, 2, 3):
            self.hub.on_line(self.client, json.dumps({"t": "set_time", "v": v}).encode())
        self.assertEqual(self.song.current_song_time, 0.0)
        self.hub.tick()
        self.assertEqual(self.song.current_song_time, 3.0)

    def test_new_protocol_client_is_not_streamed_to(self):
        self.sent = []
        other = object()
        self.hub.on_connect(other)
        self.hub.on_line(other, b'{"id":1,"cmd":"hello"}')
        self.assertTrue(self.sent[0]["ok"])

    def test_new_client_gets_a_fresh_snapshot(self):
        self.hub.tick()
        self.song.tempo = 77.0
        second = self.Client()
        self.hub.on_connect(second)
        self.sent = []
        self.hub.tick()
        snaps = [m for m in self.sent if m["t"] == "snapshot"]
        self.assertEqual(snaps[0]["tempo"], 77.0)

    def test_dropped_client_gets_nothing(self):
        self.hub.drop_client(self.client)
        self.hub.tick()
        self.assertEqual(self.sent, [])

    def test_new_protocol_commands(self):
        self.sent = []
        other = object()
        self.hub.on_line(other, b'{"id":5,"ns":"arrangement","cmd":"snapshot"}')
        self.assertEqual(self.sent[0]["data"]["locators"][0]["name"], "Drop")
        self.hub.on_line(other, b'{"id":6,"ns":"arrangement","cmd":"loop_toggle"}')
        self.assertTrue(self.song.loop)


if __name__ == "__main__":
    unittest.main()
