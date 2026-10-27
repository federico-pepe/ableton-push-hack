from . import (arrangement, browser, clip, debug, device, listen, modes, notes,
               session, track, transport)

ALL = (transport, browser, notes, listen, arrangement, debug, track, session,
       device, clip, modes)


def register_all(registry):
    for mod in ALL:
        mod.register(registry)
