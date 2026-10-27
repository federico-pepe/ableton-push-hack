class CommandError(Exception):
    """A command failed with a protocol error code (not a crash)."""

    def __init__(self, code):
        Exception.__init__(self, code)
        self.code = code
