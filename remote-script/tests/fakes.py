class FakeSong(object):
    def __init__(self):
        self.is_playing = False
        self.tempo = 120.0
        self.current_song_time = 0.0
        self.signature_numerator = 4
        self.signature_denominator = 4
        self.loop = False
        self.loop_start = 0.0
        self.loop_length = 4.0
        self.punch_in = False
        self.punch_out = False

    def start_playing(self):
        self.is_playing = True

    def stop_playing(self):
        self.is_playing = False


class FakeItem(object):
    def __init__(self, name, children=None, loadable=False, uri=None):
        self.name = name
        self.children = children or []
        self.is_loadable = loadable
        self.is_folder = bool(children) and not loadable
        self.uri = uri or "q:" + name


class FakeBrowser(object):
    """Roots as Live gives them: single items or vectors of items."""

    def __init__(self):
        self.loaded = []
        self.sounds = [FakeItem("Sounds", [
            FakeItem("Bass", [FakeItem("Wobble.adv", loadable=True)])])]
        self.instruments = FakeItem("Instruments", [
            FakeItem("Operator", [FakeItem("Wobble.adv", loadable=True, uri="q:op-wobble")],
                     loadable=True)])
        self.drums = FakeItem("Drums", [FakeItem("Kick Kit.adg", loadable=True)])
        self.samples = FakeItem("Samples", [FakeItem("kick.wav", loadable=True)])
        self.plugins = FakeItem("Plug-Ins", [
            FakeItem("Acme", [
                FakeItem("Synth", [FakeItem("Warm.vstpreset", loadable=True),
                                   FakeItem("Cold.vstpreset", loadable=True)], loadable=True)]),
            FakeItem("Solo", loadable=True)])

    def load_item(self, item):
        self.loaded.append(item.name)


class FakeRouting(object):
    def __init__(self, name, attached=None):
        self.display_name = name
        self.attached_object = attached


class FakeTrack(object):
    def __init__(self, name, midi=True):
        self.name = name
        self.has_midi_input = midi
        self.is_foldable = False
        self.group_track = None
        self.color = 0xFF0000
        self.arrangement_clips = []
        self.available_input_routing_types = []
        self.available_input_routing_channels = [FakeRouting("Post FX"), FakeRouting("Pre FX")]
        self.input_routing_type = FakeRouting("Ext. In")
        self.input_routing_channel = FakeRouting("All Channels")
        self.available_output_routing_types = [FakeRouting("Track"), FakeRouting("Stacks Listen port")]
        self.output_routing_type = FakeRouting("Master")
        self.current_monitoring_state = 1


class FakeView(object):
    selected_track = None
