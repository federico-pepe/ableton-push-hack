from . import arrangement, browser, listen, notes, transport

ALL = (transport, browser, notes, listen, arrangement)


def register_all(registry):
    for mod in ALL:
        mod.register(registry)
