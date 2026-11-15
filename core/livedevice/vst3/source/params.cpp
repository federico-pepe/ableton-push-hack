// params.cpp: a small JSON reader, enough for the class file, the bundle's
// livedevice.json and the hack's answers; and the loaders for both files.
#include "params.h"

#include <cctype>
#include <cstdlib>
#include <map>

#include "classes.h"

#include <dlfcn.h>
#include <fstream>
#include <mutex>
#include <sstream>

namespace livedevice {
namespace {

struct Val {
    enum Kind { Null, Num, Str, Arr, Obj, Bool } kind = Null;
    double num = 0;
    std::string str;
    std::vector<Val> arr;
    std::map<std::string, Val> obj;
};

struct Parser {
    const std::string& s;
    size_t i = 0;
    explicit Parser(const std::string& in) : s(in) {}
    void ws() { while (i < s.size() && std::isspace((unsigned char)s[i])) i++; }
    bool parse(Val& v) {
        ws();
        if (i >= s.size()) return false;
        char c = s[i];
        if (c == '{') return obj(v);
        if (c == '[') return arr(v);
        if (c == '"') { v.kind = Val::Str; return string(v.str); }
        if (c == 't' && s.compare(i, 4, "true") == 0) { v.kind = Val::Bool; v.num = 1; i += 4; return true; }
        if (c == 'f' && s.compare(i, 5, "false") == 0) { v.kind = Val::Bool; i += 5; return true; }
        if (c == 'n' && s.compare(i, 4, "null") == 0) { v.kind = Val::Null; i += 4; return true; }
        char* end = nullptr;
        v.num = std::strtod(s.c_str() + i, &end);
        if (end == s.c_str() + i) return false;
        v.kind = Val::Num;
        i = (size_t)(end - s.c_str());
        return true;
    }
    uint32_t hex4(size_t at) const {
        uint32_t v = 0;
        for (size_t k = at; k < at + 4; k++) {
            char c = k < s.size() ? s[k] : '0';
            v = v * 16 + (c >= '0' && c <= '9' ? c - '0' : c >= 'a' && c <= 'f' ? c - 'a' + 10 : c >= 'A' && c <= 'F' ? c - 'A' + 10 : 0);
        }
        return v;
    }
    static void utf8(std::string& out, uint32_t cp) {
        if (cp >= 0xD800 && cp < 0xE000) cp = 0xFFFD; // a lone surrogate
        if (cp < 0x80) out += (char)cp;
        else if (cp < 0x800) { out += (char)(0xC0 | cp >> 6); out += (char)(0x80 | (cp & 0x3F)); }
        else if (cp < 0x10000) { out += (char)(0xE0 | cp >> 12); out += (char)(0x80 | (cp >> 6 & 0x3F)); out += (char)(0x80 | (cp & 0x3F)); }
        else { out += (char)(0xF0 | cp >> 18); out += (char)(0x80 | (cp >> 12 & 0x3F)); out += (char)(0x80 | (cp >> 6 & 0x3F)); out += (char)(0x80 | (cp & 0x3F)); }
    }
    bool string(std::string& out) {
        i++; // "
        while (i < s.size() && s[i] != '"') {
            if (s[i] == '\\' && i + 1 < s.size()) {
                char e = s[++i];
                switch (e) {
                case 'n': out += '\n'; break;
                case 't': out += '\t'; break;
                case 'r': out += '\r'; break;
                case 'b': out += '\b'; break;
                case 'f': out += '\f'; break;
                case 'u': { // Go writes & < > this way: decode, to UTF-8
                    uint32_t cp = hex4(i + 1);
                    i += 4;
                    if (cp >= 0xD800 && cp < 0xDC00 && i + 6 < s.size() && s[i + 1] == '\\' && s[i + 2] == 'u') {
                        uint32_t lo = hex4(i + 3);
                        if (lo >= 0xDC00 && lo < 0xE000) {
                            cp = 0x10000 + ((cp - 0xD800) << 10) + (lo - 0xDC00);
                            i += 6;
                        }
                    }
                    utf8(out, cp);
                    break;
                }
                default: out += e;
                }
                i++;
                continue;
            }
            out += s[i++];
        }
        i++;
        return true;
    }
    bool arr(Val& v) {
        v.kind = Val::Arr;
        i++;
        ws();
        if (i < s.size() && s[i] == ']') { i++; return true; }
        for (;;) {
            Val e;
            if (!parse(e)) return false;
            v.arr.push_back(std::move(e));
            ws();
            if (i < s.size() && s[i] == ',') { i++; continue; }
            if (i < s.size() && s[i] == ']') { i++; return true; }
            return false;
        }
    }
    bool obj(Val& v) {
        v.kind = Val::Obj;
        i++;
        ws();
        if (i < s.size() && s[i] == '}') { i++; return true; }
        for (;;) {
            ws();
            std::string k;
            if (i >= s.size() || s[i] != '"' || !string(k)) return false;
            ws();
            if (i >= s.size() || s[i] != ':') return false;
            i++;
            Val e;
            if (!parse(e)) return false;
            v.obj[k] = std::move(e);
            ws();
            if (i < s.size() && s[i] == ',') { i++; continue; }
            if (i < s.size() && s[i] == '}') { i++; return true; }
            return false;
        }
    }
};

std::string strOf(const Val& o, const char* k) {
    auto it = o.obj.find(k);
    return it != o.obj.end() && it->second.kind == Val::Str ? it->second.str : "";
}
bool numOf(const Val& o, const char* k, double& out) {
    auto it = o.obj.find(k);
    if (it == o.obj.end()) return false;
    if (it->second.kind == Val::Num) { out = it->second.num; return true; }
    if (it->second.kind == Val::Str) {
        char* end = nullptr;
        out = std::strtod(it->second.str.c_str(), &end);
        return end != it->second.str.c_str();
    }
    return false;
}

} // namespace


} // namespace livedevice

namespace livedevice {
namespace {

ParamMeta metaFromVal(const Val& o) {
    ParamMeta m;
    m.key = strOf(o, "key");
    m.name = strOf(o, "name");
    m.type = strOf(o, "type");
    m.unit = strOf(o, "unit");
    m.format = strOf(o, "format");
    numOf(o, "min", m.min);
    numOf(o, "max", m.max);
    double d;
    if (numOf(o, "default", d)) { m.def = d; m.hasDefault = true; } // normalized
    auto opts = o.obj.find("options");
    if (opts != o.obj.end() && opts->second.kind == Val::Arr)
        for (const Val& x : opts->second.arr)
            if (x.kind == Val::Str) m.options.push_back(x.str);
    return m;
}

} // namespace

// The bundle's own folder: this binary is <Name>.vst3/Contents/x86_64-linux/<Name>.so.
static std::string bundleResources() {
    Dl_info di{};
    if (!dladdr((void*)&bundleResources, &di) || !di.dli_fname) return "";
    std::string p = di.dli_fname;
    for (int up = 0; up < 2; up++) {
        auto i = p.rfind('/');
        if (i == std::string::npos) return "";
        p.resize(i);
    }
    return p + "/Resources";
}

const Config& config() {
    static Config c;
    static std::once_flag once;
    std::call_once(once, [] {
        std::string path;
        if (const char* e = std::getenv("LIVEDEVICE_CONFIG"); e && *e) path = e;
        else path = bundleResources() + "/livedevice.json";
        std::ifstream f(path);
        if (f) {
            std::stringstream ss;
            ss << f.rdbuf();
            std::string doc = ss.str();
            Val root;
            Parser p(doc);
            if (p.parse(root) && root.kind == Val::Obj) {
                c.vendor = strOf(root, "vendor");
                c.url = strOf(root, "url");
                c.email = strOf(root, "email");
                c.socket = strOf(root, "socket");
                c.classes = strOf(root, "classes");
            }
        }
        // a test runs a bundle against its own hack
        if (const char* e = std::getenv("LIVEDEVICE_SOCKET"); e && *e) c.socket = e;
        if (const char* e = std::getenv("LIVEDEVICE_CLASSES"); e && *e) c.classes = e;
        if (c.vendor.empty()) c.vendor = "Push Hack";
    });
    return c;
}

std::string classesPath() { return config().classes; }

// A class with no category gets the one its kind needs: Live sorts devices
// into instruments and effects by it.
static std::string defaultCategory(const std::string& kind) {
    return kind == "effect" || kind == "midifx" ? "Fx" : "Instrument";
}

const std::vector<ClassInfo>& classes() {
    static std::vector<ClassInfo> out;
    static std::once_flag once;
    std::call_once(once, [] {
        std::ifstream f(classesPath());
        if (!f) return;
        std::stringstream ss;
        ss << f.rdbuf();
        std::string doc = ss.str();
        Val root;
        Parser p(doc);
        if (!p.parse(root) || root.kind != Val::Obj) return;
        auto cl = root.obj.find("classes");
        if (cl == root.obj.end() || cl->second.kind != Val::Arr) return;
        for (const Val& c : cl->second.arr) {
            if (c.kind != Val::Obj) continue;
            ClassInfo ci;
            ci.uid = strOf(c, "uid");
            ci.name = strOf(c, "name");
            ci.kind = strOf(c, "kind");
            ci.category = strOf(c, "category");
            if (ci.category.empty()) ci.category = defaultCategory(ci.kind);
            if (ci.uid.size() != 32 || ci.name.empty()) continue;
            auto ps = c.obj.find("params");
            if (ps != c.obj.end() && ps->second.kind == Val::Arr)
                for (const Val& x : ps->second.arr)
                    if (x.kind == Val::Obj) ci.params.push_back(metaFromVal(x));
            auto bs = c.obj.find("banks");
            if (bs != c.obj.end() && bs->second.kind == Val::Arr)
                for (const Val& b : bs->second.arr) {
                    std::vector<int> row;
                    if (b.kind == Val::Arr)
                        for (const Val& i : b.arr)
                            if (i.kind == Val::Num) row.push_back((int)i.num);
                    ci.banks.push_back(row);
                }
            out.push_back(std::move(ci));
        }
    });
    return out;
}

bool parseReply(const std::string& line, Reply& r) {
    Val v;
    Parser p(line);
    if (!p.parse(v) || v.kind != Val::Obj) return false;
    auto ok = v.obj.find("ok");
    r.ok = ok != v.obj.end() && ok->second.kind == Val::Bool && ok->second.num != 0;
    double id = 0;
    if (numOf(v, "id", id)) r.id = (int)id;
    r.shm = strOf(v, "shm");
    r.state = strOf(v, "state");
    r.error = strOf(v, "error");
    return true;
}

std::string jsonString(const std::string& s) {
    std::string o = "\"";
    for (unsigned char c : s) {
        switch (c) {
        case '"': o += "\\\""; break;
        case '\\': o += "\\\\"; break;
        case '\n': o += "\\n"; break;
        case '\t': o += "\\t"; break;
        case '\r': o += "\\r"; break;
        default:
            if (c < 0x20) { char b[8]; std::snprintf(b, sizeof b, "\\u%04x", c); o += b; }
            else o += (char)c;
        }
    }
    return o + "\"";
}

} // namespace livedevice
