from . import transport

ALL = (transport,)


def register_all(registry):
    for mod in ALL:
        mod.register(registry)
