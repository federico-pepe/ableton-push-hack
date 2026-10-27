# Wire format: one JSON object per line, both directions. No Live imports.
#
#   request  {"id":1,"ns":"transport","cmd":"get", ...args}
#   reply    {"id":1,"ok":true,"data":{...}}   or   {"id":1,"ok":false,"error":"code"}
#   event    {"t":"event","topic":"transport","data":{...}}   (pushed, no id)
#
# Built-in commands, no "ns": hello, subscribe, unsubscribe.

import json

PROTOCOL = 1
SCRIPT_VERSION = "0.2.0"

MAX_LINE_BYTES = 1024 * 1024


def decode(line):
    """Bytes or str -> dict, or None when it is not a JSON object."""
    try:
        if isinstance(line, bytes):
            line = line.decode("utf-8")
        msg = json.loads(line)
    except Exception:
        return None
    return msg if isinstance(msg, dict) else None


def encode(obj):
    return json.dumps(obj, separators=(",", ":"))


def ok(req_id, data=None):
    r = {"ok": True}
    if req_id is not None:
        r["id"] = req_id
    if data is not None:
        r["data"] = data
    return encode(r)


def err(req_id, code):
    r = {"ok": False, "error": code}
    if req_id is not None:
        r["id"] = req_id
    return encode(r)


def event(topic, data):
    return encode({"t": "event", "topic": topic, "data": data})
