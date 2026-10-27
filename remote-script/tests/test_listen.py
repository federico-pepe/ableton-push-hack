import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
from fakes import FakeRouting, FakeTrack, FakeView  # noqa: E402
from PushHack.errors import CommandError  # noqa: E402
from PushHack.ns import listen  # noqa: E402


class ListenSong(object):
    def __init__(self):
        self.pad = FakeTrack("Pad")
        self.pad.has_midi_input = True
        self.tracks = [self.pad]
        self.view = FakeView()
        self.view.selected_track = self.pad

    def create_midi_track(self, index):
        t = FakeTrack("MIDI")
        t.available_input_routing_types = [FakeRouting("Pad", attached=self.pad)]
        self.tracks.append(t)


class ListenTest(unittest.TestCase):
    def setUp(self):
        self.song = ListenSong()
        self.logs = []
        self.l = listen.Listen(self.logs.append)

    def test_status_without_track(self):
        self.assertEqual(self.l.cmd_get(self.song, {}), {"ok": True, "on": False, "track": False})

    def test_on_creates_track_points_output_and_follows_selection(self):
        st = self.l.cmd_on(self.song, {"port": "Stacks Listen port"})
        lt = self.song.tracks[-1]
        self.assertEqual(lt.name, listen.LISTEN_NAME)
        self.assertTrue(st["ok"] and st["on"])
        self.assertEqual(lt.output_routing_type.display_name, "Stacks Listen port")
        self.assertEqual(lt.current_monitoring_state, listen.MONITOR_IN)
        self.assertEqual(lt.input_routing_type.attached_object, self.song.pad)
        self.assertEqual(lt.input_routing_channel.display_name, "Post FX")
        self.assertEqual(st["source"], "Pad")

    def test_on_twice_reuses_the_track(self):
        self.l.cmd_on(self.song, {"port": "Stacks Listen port"})
        self.l.cmd_on(self.song, {"port": "Stacks Listen port"})
        self.assertEqual(len(self.song.tracks), 2)

    def test_unknown_port_reports_outputs_and_stays_off(self):
        st = self.l.cmd_on(self.song, {"port": "Nope"})
        self.assertFalse(st["ok"])
        self.assertFalse(st["on"])
        self.assertIn("Track", st["outputs"])

    def test_no_port_is_an_error(self):
        with self.assertRaises(CommandError):
            self.l.cmd_on(self.song, {})

    def test_off_silences(self):
        self.l.cmd_on(self.song, {"port": "Stacks Listen port"})
        st = self.l.cmd_off(self.song, {})
        self.assertFalse(st["on"])
        self.assertEqual(self.song.tracks[-1].current_monitoring_state, listen.MONITOR_OFF)

    def test_tick_follows_a_new_selection_only_when_on(self):
        other = FakeTrack("Lead")
        other.has_midi_input = True
        self.song.tracks.append(other)
        self.song.view.selected_track = other
        self.l.tick(self.song)                       # off: nothing happens
        self.assertIsNone(self.l.src)
        self.l.cmd_on(self.song, {"port": "Stacks Listen port"})
        lt = self.song.tracks[-1]
        lt.available_input_routing_types.append(FakeRouting("Lead", attached=other))
        self.l.tick(self.song)
        self.assertIs(self.l.src, other)

    def test_deleted_listen_track_turns_it_off(self):
        self.l.cmd_on(self.song, {"port": "Stacks Listen port"})
        self.song.tracks.pop()
        self.l.tick(self.song)
        self.assertFalse(self.l.on)

    def test_audio_track_selection_is_ignored(self):
        self.l.cmd_on(self.song, {"port": "Stacks Listen port"})
        audio = FakeTrack("Drums", midi=False)
        self.song.view.selected_track = audio
        self.l.tick(self.song)
        self.assertIsNot(self.l.src, audio)


if __name__ == "__main__":
    unittest.main()
