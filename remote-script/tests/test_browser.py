import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
from fakes import FakeBrowser  # noqa: E402
from PushHack.errors import CommandError  # noqa: E402
from PushHack.ns import browser as nsb  # noqa: E402


class BrowserTest(unittest.TestCase):
    def setUp(self):
        self.fb = FakeBrowser()
        self.logs = []
        self.b = nsb.Browser(self.logs.append, lambda: self.fb)

    def test_split_scope(self):
        self.assertEqual(nsb.split_scope("drums:Kick Kit.adg"), ("drums", "Kick Kit.adg"))
        self.assertEqual(nsb.split_scope("Wobble"), (None, "Wobble"))
        self.assertEqual(nsb.split_scope("nope:Wobble"), (None, "nope:Wobble"))

    def test_name_matches_with_and_without_extension(self):
        self.assertTrue(nsb.name_matches("Wobble.adv", "Wobble"))
        self.assertTrue(nsb.name_matches("kick", "kick.wav"))
        self.assertFalse(nsb.name_matches("Wobble.adv", "Wob"))
        self.assertFalse(nsb.name_matches("a.txt", "a"))

    def test_load_scoped_finds_sounds_root_first(self):
        r = self.b.cmd_load(None, {"name": "Wobble", "scope": "instruments"})
        self.assertEqual(r, {"loaded": "Wobble.adv"})
        self.assertEqual(self.fb.loaded, ["Wobble.adv"])

    def test_load_falls_back_to_all_roots(self):
        self.b.cmd_load(None, {"name": "Kick Kit", "scope": "instruments"})
        self.assertEqual(self.fb.loaded, ["Kick Kit.adg"])

    def test_load_walks_into_loadable_device_nodes(self):
        self.fb.sounds = []
        self.b.cmd_load(None, {"name": "Wobble"})
        self.assertEqual(self.fb.loaded, ["Wobble.adv"])

    def test_not_found_is_a_command_error(self):
        with self.assertRaises(CommandError) as cm:
            self.b.cmd_load(None, {"name": "Missing"})
        self.assertEqual(cm.exception.code, "not_found")
        self.assertEqual(self.fb.loaded, [])

    def test_folder_is_not_loadable(self):
        with self.assertRaises(CommandError) as cm:
            self.b.cmd_load_uri(None, {"uri": "q:Bass"})   # a folder, not loadable
        self.assertEqual(cm.exception.code, "not_loadable")

    def test_load_uri_and_sample(self):
        self.b.cmd_load_uri(None, {"uri": "q:op-wobble"})
        self.b.cmd_load_sample(None, {"name": "kick.wav"})
        self.assertEqual(self.fb.loaded, ["Wobble.adv", "kick.wav"])

    def test_list_plugins(self):
        out = self.b.cmd_list_plugins(None, {})["plugins"]
        self.assertEqual(out, [
            {"vendor": "Acme", "name": "Synth", "presets": ["Warm", "Cold"]},
            {"vendor": "", "name": "Solo", "presets": []},
        ])

    def test_load_plugin_default_and_preset(self):
        self.b.cmd_load_plugin(None, {"plugin": "Synth"})
        self.b.cmd_load_plugin(None, {"plugin": "Synth", "preset": "Cold"})
        self.assertEqual(self.fb.loaded, ["Synth", "Cold.vstpreset"])

    def test_load_plugin_missing_preset(self):
        with self.assertRaises(CommandError):
            self.b.cmd_load_plugin(None, {"plugin": "Synth", "preset": "Nope"})
        self.assertTrue(any("no preset" in m for m in self.logs))

    def test_depth_limit_stops_the_walk(self):
        from fakes import FakeItem
        deep = FakeItem("leaf.adv", loadable=True)
        for i in range(nsb.MAX_DEPTH + 3):
            deep = FakeItem("d%d" % i, [deep])
        self.fb.sounds = [deep]
        with self.assertRaises(CommandError):
            self.b.cmd_load(None, {"name": "leaf"})

    def test_dump_logs_and_is_bounded(self):
        r = self.b.cmd_dump(None, {"root": "plugins", "depth": 3})
        self.assertGreater(r["lines"], 0)
        self.assertTrue(any("dump|" in m for m in self.logs))
        with self.assertRaises(CommandError):
            self.b.cmd_dump(None, {"root": "no_such_root"})


if __name__ == "__main__":
    unittest.main()
