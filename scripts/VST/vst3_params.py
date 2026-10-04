#!/usr/bin/env python3
"""List the parameters of a Linux VST3 plugin as JSON. Stdlib only.

Runs on Push (Linux x86_64), started over ssh by make-vst3-preset.py. It also
runs on a Mac against the Mac build of a plugin (same parameter IDs):

    ssh root@push.local python3 - /data/.vst3/Foo.vst3 < scripts/VST/vst3_params.py

It loads the plugin, creates its edit controller, and reads each
ParameterInfo. It never opens audio, never shows a window, and does not
process any sound. It talks to the plugin through the VST3 COM tables by hand.
"""
import ctypes
import glob
import json
import os
import sys

c_void_pp = ctypes.POINTER(ctypes.c_void_p)


def tuid(a, b, c, d):
    """VST3 interface IDs on Linux: four big-endian 32-bit words."""
    return bytes(
        (w >> s) & 0xFF for w in (a, b, c, d) for s in (24, 16, 8, 0)
    )


IID_FUNKNOWN = tuid(0x00000000, 0x00000000, 0xC0000000, 0x00000046)
IID_ICOMPONENT = tuid(0xE831FF31, 0xF2D54301, 0x928EBBEE, 0x25697802)
IID_IEDITCONTROLLER = tuid(0xDCD7BBE3, 0x7742448D, 0xA874AACC, 0x979C759E)
IID_ICONNECTIONPOINT = tuid(0x70A4156F, 0x6E6E4026, 0x989148BF, 0xAA60D8D1)
IID_IHOSTAPPLICATION = tuid(0x58E595CC, 0xDB2D4969, 0x8B6AAF8C, 0x36A664E5)

kCanAutomate, kIsReadOnly, kIsList, kIsHidden = 1, 2, 8, 16


class ParameterInfo(ctypes.Structure):
    _fields_ = [
        ("id", ctypes.c_uint32),
        ("title", ctypes.c_uint16 * 128),
        ("shortTitle", ctypes.c_uint16 * 128),
        ("units", ctypes.c_uint16 * 128),
        ("stepCount", ctypes.c_int32),
        ("defaultNormalizedValue", ctypes.c_double),
        ("unitId", ctypes.c_int32),
        ("flags", ctypes.c_int32),
    ]


class PClassInfo(ctypes.Structure):
    _fields_ = [
        ("cid", ctypes.c_char * 16),
        ("cardinality", ctypes.c_int32),
        ("category", ctypes.c_char * 32),
        ("name", ctypes.c_char * 64),
    ]


def u16(arr):
    chars = []
    for ch in arr:
        if ch == 0:
            break
        chars.append(chr(ch))
    return "".join(chars)


def method(obj, index, restype, *argtypes):
    """Call slot `index` of a COM object's vtable."""
    vtbl = ctypes.cast(
        ctypes.cast(obj, c_void_pp).contents, ctypes.POINTER(ctypes.c_void_p)
    )
    proto = ctypes.CFUNCTYPE(restype, ctypes.c_void_p, *argtypes)
    return lambda *a: proto(vtbl[index])(obj, *a)


# A minimal IHostApplication so plugins that insist on a host context start up.
_keep = []


def make_host():
    Q = ctypes.CFUNCTYPE(ctypes.c_int32, ctypes.c_void_p, ctypes.c_char_p, c_void_pp)
    R = ctypes.CFUNCTYPE(ctypes.c_uint32, ctypes.c_void_p)
    N = ctypes.CFUNCTYPE(ctypes.c_int32, ctypes.c_void_p, ctypes.POINTER(ctypes.c_uint16))
    C = ctypes.CFUNCTYPE(
        ctypes.c_int32, ctypes.c_void_p, ctypes.c_char_p, ctypes.c_char_p, c_void_pp
    )
    holder = ctypes.c_void_p()

    def query(_s, iid, out):
        raw = ctypes.string_at(iid, 16)
        if raw in (IID_FUNKNOWN, IID_IHOSTAPPLICATION):
            out[0] = ctypes.addressof(holder)
            return 0
        out[0] = None
        return -1  # kNoInterface

    def get_name(_s, buf):
        for i, ch in enumerate("push-hack\0"):
            buf[i] = ord(ch)
        return 0

    fns = [Q(query), R(lambda s: 1), R(lambda s: 1), N(get_name),
           C(lambda s, a, b, o: -1)]
    vtbl = (ctypes.c_void_p * len(fns))(*[ctypes.cast(f, ctypes.c_void_p) for f in fns])
    holder.value = ctypes.addressof(vtbl)
    _keep.extend([fns, vtbl, holder])
    return ctypes.addressof(holder)


def find_module(path):
    if path.endswith(".so"):
        return path
    if sys.platform == "darwin":
        hits = [h for h in glob.glob(os.path.join(path, "Contents", "MacOS", "*"))
                if os.path.isfile(h)]
    else:
        hits = glob.glob(os.path.join(path, "Contents", "x86_64-linux", "*.so"))
    if not hits:
        sys.exit("no plugin binary inside " + path)
    return hits[0]


def list_params(path):
    lib = ctypes.CDLL(find_module(path))
    lib.GetPluginFactory.restype = ctypes.c_void_p
    entry = "bundleEntry" if sys.platform == "darwin" else "ModuleEntry"
    if hasattr(lib, entry):
        getattr(lib, entry).argtypes = [ctypes.c_void_p]
        getattr(lib, entry)(None)
    factory = lib.GetPluginFactory()
    count = method(factory, 4, ctypes.c_int32)()
    host = make_host()

    classes = []
    for i in range(count):
        info = PClassInfo()
        method(factory, 5, ctypes.c_int32, ctypes.c_int32, ctypes.POINTER(PClassInfo))(
            i, ctypes.byref(info)
        )
        if info.category == b"Audio Module Class":
            classes.append(info)
    if not classes:
        sys.exit("plugin has no Audio Module Class")

    results = []
    for info in classes:
        comp = ctypes.c_void_p()
        res = method(factory, 6, ctypes.c_int32, ctypes.c_char_p, ctypes.c_char_p, c_void_pp)(
            bytes(info.cid), IID_ICOMPONENT, ctypes.byref(comp)
        )
        if res != 0 or not comp.value:
            sys.exit("createInstance(IComponent) failed: %d" % res)
        method(comp, 3, ctypes.c_int32, ctypes.c_void_p)(host)  # initialize

        # Prefer a separate controller (JUCE plugins fill their parameter list
        # only once the component and controller are connected). A single-
        # component plugin answers queryInterface on the component instead.
        ctrl = ctypes.c_void_p()
        split = False
        cid = ctypes.create_string_buffer(16)
        if method(comp, 5, ctypes.c_int32, ctypes.c_char_p)(cid) == 0 and any(cid.raw):
            res = method(factory, 6, ctypes.c_int32, ctypes.c_char_p, ctypes.c_char_p, c_void_pp)(
                cid.raw, IID_IEDITCONTROLLER, ctypes.byref(ctrl)
            )
            split = res == 0 and bool(ctrl.value)
        if split:
            method(ctrl, 3, ctypes.c_int32, ctypes.c_void_p)(host)
            cp_c, cp_e = ctypes.c_void_p(), ctypes.c_void_p()
            if (method(comp, 0, ctypes.c_int32, ctypes.c_char_p, c_void_pp)(
                    IID_ICONNECTIONPOINT, ctypes.byref(cp_c)) == 0
                    and method(ctrl, 0, ctypes.c_int32, ctypes.c_char_p, c_void_pp)(
                    IID_ICONNECTIONPOINT, ctypes.byref(cp_e)) == 0):
                method(cp_c, 3, ctypes.c_int32, ctypes.c_void_p)(cp_e)
                method(cp_e, 3, ctypes.c_int32, ctypes.c_void_p)(cp_c)
        else:
            ctrl = ctypes.c_void_p()
            res = method(comp, 0, ctypes.c_int32, ctypes.c_char_p, c_void_pp)(
                IID_IEDITCONTROLLER, ctypes.byref(ctrl)
            )
            if res != 0 or not ctrl.value:
                sys.exit("plugin has no edit controller")

        n = method(ctrl, 8, ctypes.c_int32)()
        params = []
        for i in range(n):
            p = ParameterInfo()
            if method(ctrl, 9, ctypes.c_int32, ctypes.c_int32, ctypes.POINTER(ParameterInfo))(
                i, ctypes.byref(p)
            ) != 0:
                continue
            params.append({
                "id": p.id,
                "title": u16(p.title),
                "units": u16(p.units),
                "steps": p.stepCount,
                "list": bool(p.flags & kIsList),
                "hidden": bool(p.flags & (kIsHidden | kIsReadOnly)),
                "automatable": bool(p.flags & kCanAutomate),
            })
        results.append({"class": info.name.decode("utf-8", "replace"), "params": params})
        # Skip terminate(): some plugins block on UI/audio threads we never started.
    return results


if __name__ == "__main__":
    if len(sys.argv) != 2:
        sys.exit("usage: vst3_params.py <plugin.vst3>")
    json.dump(list_params(sys.argv[1]), sys.stdout)
    sys.stdout.flush()
    os._exit(0)  # do not run plugin static destructors
