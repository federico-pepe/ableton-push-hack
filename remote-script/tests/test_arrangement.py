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

class ArrangementCommandsTest(unittest.TestCase):
    def setUp(self):
        self.song = ArrSong()
        self.sent = []
        self.hub = Hub(lambda: self.song, lambda c, t: self.sent.append(json.loads(t)))

    def call(self, cmd, **kw):
        self.sent = []
        msg = {"id": 1, "ns": "arrangement", "cmd": cmd}
        msg.update(kw)
        self.hub.on_line("c", json.dumps(msg).encode())
        return self.sent[0]

    def test_snapshot_and_position_commands(self):
        d = self.call("snapshot")["data"]
        self.assertEqual(d["locators"][0]["name"], "Drop")
        self.assertIn("time", self.call("position")["data"])

    def test_toggles_and_bta(self):
        for cmd in ("loop_toggle", "punch_in_toggle", "punch_out_toggle", "bta"):
            self.assertTrue(self.call(cmd)["ok"])
        self.assertTrue(self.song.loop and self.song.punch_in and self.song.punch_out)
        self.assertEqual(self.song.back_to_arranger, 0)
        self.call("loop_toggle")
        self.assertFalse(self.song.loop)

    def test_topics_stream_snapshot_and_position_when_subscribed(self):
        self.hub.on_line("c", b'{"cmd":"subscribe","topics":["arrangement","position"]}')
        self.sent = []
        self.hub.tick()
        topics = sorted(m["topic"] for m in self.sent if m.get("t") == "event")
        self.assertEqual(topics, ["arrangement", "position"])
        self.sent = []
        self.hub.tick()
        self.assertEqual([m for m in self.sent if m.get("t") == "event"], [])
        self.song.current_song_time = 7.0
        self.hub.tick()
        ev = [m for m in self.sent if m.get("t") == "event"]
        self.assertEqual([m["topic"] for m in ev], ["position"])
        self.assertEqual(ev[0]["data"]["time"], 7.0)


if __name__ == "__main__":
    unittest.main()
