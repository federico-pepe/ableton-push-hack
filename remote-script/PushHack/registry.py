# Namespaces register their commands and topics here. No Live imports.
#
#   command  fn(song, msg) -> dict | None     runs on Live's engine thread
#   topic    fn(song) -> JSON-able value      polled each tick while subscribed


class Registry(object):
    def __init__(self):
        self._commands = {}
        self._topics = {}
        self._ticks = []
        self.log = lambda msg: None   # the hub points this at Live's log
        self.arrangement = None       # set by ns.arrangement: the compat feed

    def command(self, ns, name, fn):
        self._commands[(ns, name)] = fn

    def topic(self, name, fn):
        self._topics[name] = fn

    def on_tick(self, fn):
        """fn(song) runs once per Live tick, even with nobody connected."""
        self._ticks.append(fn)

    def tick_fns(self):
        return list(self._ticks)

    def find_command(self, ns, name):
        return self._commands.get((ns, name))

    def has_namespace(self, ns):
        return any(k[0] == ns for k in self._commands)

    def topic_fn(self, name):
        return self._topics.get(name)

    def namespaces(self):
        return sorted(set(k[0] for k in self._commands))

    def topics(self):
        return sorted(self._topics)
