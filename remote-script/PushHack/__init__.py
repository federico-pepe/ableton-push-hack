# PushHack: the shared push-hack Ableton Live Remote Script.


def create_instance(c_instance):
    # Imported here so the pure-Python parts load without Live (tests).
    from .PushHack import PushHack
    return PushHack(c_instance)
