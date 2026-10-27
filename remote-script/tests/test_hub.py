import json
import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
from fakes import FakeSong  # noqa: E402
from PushHack.hub import Hub  # noqa: E402
from PushHack import protocol  # noqa: E402


class HubCase(unittest.TestCase):
    def setUp(self):
        self.song = FakeSong()
        self.sent = []
        self.closed = []
        self.logs = []
        self.hub = Hub(lambda: self.song, lambda c, t: self.sent.append((c, t)),
                       self.closed.append, self.logs.append)

    def ask(self, **msg):
        self.sent = []
        self.hub.on_line("A", json.dumps(msg).encode())
        return [json.loads(t) for c, t in self.sent]


class BuiltinTest(HubCase):
    def test_hello(self):
        r = self.ask(id=1, cmd="hello")[0]
        self.assertTrue(r["ok"])
        self.assertEqual(r["id"], 1)
        self.assertEqual(r["data"]["protocol"], protocol.PROTOCOL)
        self.assertIn("transport", r["data"]["namespaces"])

    def test_bad_json_keeps_client(self):
        self.hub.on_line("A", b"{nope")
        self.assertEqual(json.loads(self.sent[0][1]), {"ok": False, "error": "bad_json"})
        self.assertEqual(self.closed, [])

    def test_non_object_json(self):
        self.hub.on_line("A", b"[1,2]")
        self.assertEqual(json.loads(self.sent[0][1])["error"], "bad_json")

    def test_missing_cmd(self):
        self.assertEqual(self.ask(id=4)[0]["error"], "no_cmd")

    def test_unknown_namespace_and_command(self):
        self.assertEqual(self.ask(id=2, ns="nope", cmd="x")[0]["error"], "unknown_ns")
        self.assertEqual(self.ask(id=3, ns="transport", cmd="x")[0]["error"], "unknown_cmd")

    def test_reply_without_id_has_no_id(self):
        r = self.ask(ns="transport", cmd="get")[0]
        self.assertNotIn("id", r)


class TransportTest(HubCase):
    def test_get(self):
        d = self.ask(id=1, ns="transport", cmd="get")[0]["data"]
        self.assertEqual(d["tempo"], 120.0)
        self.assertEqual(d["signature"], [4, 4])
        self.assertFalse(d["playing"])

    def test_play_stop_toggle(self):
        self.ask(ns="transport", cmd="play")
        self.assertTrue(self.song.is_playing)
        self.ask(ns="transport", cmd="play_toggle")
        self.assertFalse(self.song.is_playing)
        self.ask(ns="transport", cmd="play_toggle")
        self.ask(ns="transport", cmd="stop")
        self.assertFalse(self.song.is_playing)

    def test_set_tempo_is_clamped(self):
        self.ask(ns="transport", cmd="set_tempo", v=5)
        self.assertEqual(self.song.tempo, 20.0)
        self.ask(ns="transport", cmd="set_tempo", v=5000)
        self.assertEqual(self.song.tempo, 999.0)
        self.ask(ns="transport", cmd="set_tempo", v=133.5)
        self.assertEqual(self.song.tempo, 133.5)

    def test_set_time_not_negative(self):
        self.ask(ns="transport", cmd="set_time", v=-3)
        self.assertEqual(self.song.current_song_time, 0.0)

    def test_set_loop_and_flags(self):
        self.ask(ns="transport", cmd="set_loop", start=8, length=0)
        self.assertEqual((self.song.loop_start, self.song.loop_length), (8.0, 0.0001))
        self.ask(ns="transport", cmd="set_loop_on", on=True)
        self.ask(ns="transport", cmd="set_punch_in", on=True)
        self.assertTrue(self.song.loop and self.song.punch_in)

    def test_missing_arg_is_a_failed_reply_not_a_crash(self):
        r = self.ask(id=9, ns="transport", cmd="set_tempo")[0]
        self.assertEqual((r["ok"], r["error"], r["id"]), (False, "failed", 9))
        self.assertTrue(self.logs)


class SubscribeTest(HubCase):
    def events(self):
        out = [json.loads(t) for c, t in self.sent if c == "A"]
        return [e for e in out if e.get("t") == "event"]

    def test_unknown_topic(self):
        self.assertEqual(self.ask(id=1, cmd="subscribe", topics=["nope"])[0]["error"], "unknown_topic")
        self.assertEqual(self.ask(id=2, cmd="subscribe", topics="transport")[0]["error"], "unknown_topic")

    def test_first_tick_sends_state_then_only_changes(self):
        self.ask(id=1, cmd="subscribe", topics=["transport"])
        self.sent = []
        self.hub.tick()
        self.assertEqual(len(self.events()), 1)
        self.sent = []
        self.hub.tick()
        self.assertEqual(self.events(), [])
        self.song.tempo = 100.0
        self.hub.tick()
        ev = self.events()
        self.assertEqual(len(ev), 1)
        self.assertEqual(ev[0]["data"]["tempo"], 100.0)

    def test_unsubscribe_stops_events(self):
        self.ask(cmd="subscribe", topics=["transport"])
        self.ask(cmd="unsubscribe", topics=["transport"])
        self.sent = []
        self.hub.tick()
        self.assertEqual(self.sent, [])

    def test_dropped_client_gets_nothing(self):
        self.ask(cmd="subscribe", topics=["transport"])
        self.hub.drop_client("A")
        self.sent = []
        self.hub.tick()
        self.assertEqual(self.sent, [])

    def test_two_clients_each_get_state(self):
        self.hub.on_line("A", b'{"cmd":"subscribe","topics":["transport"]}')
        self.hub.on_line("B", b'{"cmd":"subscribe","topics":["transport"]}')
        self.sent = []
        self.hub.tick()
        self.assertEqual(sorted(c for c, t in self.sent), ["A", "B"])

    def test_topic_error_does_not_stop_tick(self):
        self.hub.registry.topic("bad", lambda song: 1 / 0)
        self.ask(cmd="subscribe", topics=["bad", "transport"])
        self.sent = []
        self.hub.tick()
        self.assertEqual(len(self.events()), 1)


class LegacyTest(HubCase):
    def legacy(self, text):
        self.sent = []
        self.hub.on_legacy("L", text)
        return self.sent[0][1]

    def test_queries(self):
        self.assertEqual(self.legacy("get_tempo"), "120.0000")
        self.assertEqual(self.legacy("get_beat"), "0.000000")
        self.assertEqual(self.legacy("get_playing"), "0")
        self.song.is_playing = True
        self.assertEqual(self.legacy("get_playing"), "1")

    def test_ping_and_empty(self):
        self.assertEqual(self.legacy("ping"), "OK")
        self.assertEqual(self.legacy(""), "OK")

    def test_play_stop(self):
        self.assertEqual(self.legacy("play"), "OK")
        self.assertTrue(self.song.is_playing)
        self.legacy("stop")
        self.assertFalse(self.song.is_playing)

    def test_unknown_command_errors_and_logs(self):
        self.assertEqual(self.legacy("frobnicate"), "ERROR")
        self.assertTrue(self.logs)

    def test_get_scale_json(self):
        self.song.root_note = 2
        self.song.scale_name = "Minor"
        self.song.scale_intervals = [0, 2, 3, 5, 7, 8, 10]
        self.assertEqual(json.loads(self.legacy("get_scale")),
                         {"supported": True, "root": 2, "name": "Minor",
                          "intervals": [0, 2, 3, 5, 7, 8, 10], "mode": True})

    def test_get_scale_unsupported(self):
        self.assertEqual(json.loads(self.legacy("get_scale")), {"supported": False})

    def test_load_failure_still_answers_ok_and_logs(self):
        # no Live here: the real browser import fails, as a missing item would
        self.assertEqual(self.legacy("load:Bass.adv"), "OK")
        self.assertTrue(self.logs)

    def test_closes_after_reply(self):
        self.legacy("ping")
        self.assertEqual(self.closed, ["L"])


if __name__ == "__main__":
    unittest.main()
