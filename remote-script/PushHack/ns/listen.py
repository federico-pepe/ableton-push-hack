# listen: a silent MIDI track whose input follows the selected track and whose
# output is a hack's MIDI port (Stacks hears what the user plays this way).
#
# On Push the pads feed the selected track through Push's own path, not a MIDI
# input another track could take. What a track plays can be taken from the
# track itself, though: a MIDI track whose input is another track gets that
# track's notes, live-played included, after its MIDI effects. So the listen
# track's input follows the selection. This is checked each tick, not by a
# listener: Live re-instantiates surfaces at startup, and a change made inside
# a notification is refused. The output is the hack's port, so the track
# sounds nothing; its monitoring is Off while nobody listens.

from ..errors import CommandError

LISTEN_NAME = "Stacks Listen"
MONITOR_IN = 0
MONITOR_OFF = 2


def names(seq):
    return [getattr(x, "display_name", str(x)) for x in seq]


class Listen(object):
    def __init__(self, log):
        self._log = log
        self.on = False
        self.port = ""
        self.src = None   # the track its input points at now

    def track(self, song, create):
        for t in song.tracks:
            if t.name == LISTEN_NAME and t.has_midi_input:
                return t
        if not create:
            return None
        song.create_midi_track(len(song.tracks))
        t = list(song.tracks)[-1]
        t.name = LISTEN_NAME
        self._log("PushHack: created track %r" % LISTEN_NAME)
        return t

    def point_input(self, lt, src):
        """lt's input <- what src plays, after its MIDI effects."""
        types = list(lt.available_input_routing_types)
        rt = None
        for t in types:
            if getattr(t, "attached_object", None) == src:
                rt = t
                break
        if rt is None:
            rt = next((t for t in types if src.name and src.name in t.display_name), None)
        if rt is None:
            return False
        lt.input_routing_type = rt
        chans = list(lt.available_input_routing_channels)
        post = [c for c in chans if "post" in c.display_name.lower() and "fx" in c.display_name.lower()]
        if post or chans:
            lt.input_routing_channel = (post or chans)[0]
        return True

    def follow(self, song):
        lt = self.track(song, create=False)
        if lt is None:  # deleted: nothing to follow with
            self.on, self.src = False, None
            return
        sel = song.view.selected_track
        if sel == lt or sel == self.src or not getattr(sel, "has_midi_input", False):
            return
        if self.point_input(lt, sel):
            self.src = sel
            self._log("PushHack: listening to %r" % sel.name)

    def tick(self, song):
        if not self.on:
            return
        try:
            self.follow(song)
        except Exception as e:
            self._log("PushHack: listen follow error: %r" % (e,))

    def status(self, song):
        lt = self.track(song, create=False)
        st = {"ok": True, "on": self.on, "track": lt is not None}
        if lt is not None:
            st.update(input=lt.input_routing_type.display_name,
                      channel=lt.input_routing_channel.display_name,
                      output=lt.output_routing_type.display_name,
                      monitoring=int(lt.current_monitoring_state),
                      source=self.src.name if self.src is not None else "",
                      inputs=names(lt.available_input_routing_types))
        return st

    def set(self, song, on, port):
        lt = self.track(song, create=on)
        if lt is None:
            self.on, self.src = False, None
            return self.status(song)
        if on:
            outs = list(lt.available_output_routing_types)
            out = next((o for o in outs if port and port in o.display_name), None)
            if out is None:
                self.on = False
                st = self.status(song)
                st.update(ok=False, outputs=names(outs),
                          error="Live offers no MIDI output named %r: switch Track on for it "
                                "in Push's MIDI settings" % port)
                return st
            lt.output_routing_type = out
            lt.current_monitoring_state = MONITOR_IN  # passes what it gets, unarmed
            self.on, self.port, self.src = True, port, None
            self.follow(song)
        else:
            self.on, self.src = False, None
            lt.current_monitoring_state = MONITOR_OFF  # nothing flows while nobody listens
        return self.status(song)

    # ---- commands ----
    def cmd_get(self, song, msg):
        return self.status(song)

    def cmd_on(self, song, msg):
        port = str(msg.get("port", ""))
        if not port:
            raise CommandError("no_port")
        return self.set(song, True, port)

    def cmd_off(self, song, msg):
        return self.set(song, False, "")


def register(reg):
    lis = Listen(lambda m: reg.log(m))
    reg.command("listen", "get", lis.cmd_get)
    reg.command("listen", "on", lis.cmd_on)
    reg.command("listen", "off", lis.cmd_off)
    reg.on_tick(lis.tick)
    reg.listen = lis
