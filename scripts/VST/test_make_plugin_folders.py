import importlib.util
import os
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("mpf", os.path.join(HERE, "make-plugin-folders.py"))
mpf = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mpf)

DOC = """intro text

# Categories

Reverb: VerbSevens, Hard Vacuum, Ghost, VerbSevens

Tape: ToTape9, Tape

############ After the block

Stray: NotACategory
"""


class MakePluginFolders(unittest.TestCase):
    def write(self, text):
        d = tempfile.mkdtemp()
        p = os.path.join(d, "Airwindopedia.txt")
        with open(p, "w") as f:
            f.write(text)
        return p

    def test_reads_only_the_category_block(self):
        cats = mpf.read_categories(self.write(DOC))
        self.assertEqual([c for c, _ in cats], ["Reverb", "Tape"])

    def test_names_with_spaces_match_and_order_is_kept(self):
        cats = mpf.read_categories(self.write(DOC))
        cfg, missing = mpf.build(cats, ["Tape", "HardVacuum", "ToTape9", "VerbSevens"])
        reverb = cfg["folders"][0]["folders"][0]
        self.assertEqual(reverb["plugins"], ["VerbSevens", "HardVacuum"])  # duplicate dropped
        tape = cfg["folders"][0]["folders"][1]
        self.assertEqual(tape["plugins"], ["ToTape9", "Tape"])

    def test_unmatched_names_are_reported(self):
        cats = mpf.read_categories(self.write(DOC))
        _, missing = mpf.build(cats, ["VerbSevens"])
        self.assertIn(("Reverb", "Ghost"), missing)

    def test_period_between_names_splits(self):
        cats = mpf.read_categories(self.write("# Categories\n\nXYZ: XBandpass. XHighpass, Z\n"))
        self.assertEqual(cats[0][1], ["XBandpass", "XHighpass", "Z"])

    def test_empty_category_is_left_out(self):
        cfg, _ = mpf.build([("Empty", ["Nope"])], ["Real"])
        self.assertEqual(cfg["folders"][0]["folders"], [])


if __name__ == "__main__":
    unittest.main()
