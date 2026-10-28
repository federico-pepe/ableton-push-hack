class CommandError(Exception):
    """A command failed with a protocol error code (not a crash)."""

    def __init__(self, code, data=None):
        Exception.__init__(self, code)
        self.code = code
        self.data = data   # optional detail sent with the error (e.g. valid names)
