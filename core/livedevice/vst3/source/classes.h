// classes.h: a hack's device classes, from its class file, and the bundle's
// configuration (livedevice.json).
//
// The plugin never loads a hack's code: Live sees one class per device the
// hack describes (read from the file), and the engine runs in the hack.
#pragma once

#include "params.h"

#include <string>
#include <vector>

namespace livedevice {

struct ClassInfo {
    std::string uid;      // 32 hex digits
    std::string name;     // as Live shows it
    std::string kind;     // instrument | effect | midi | midifx
    std::string category; // VST3 sub categories
    std::vector<ParamMeta> params;
    std::vector<std::vector<int>> banks;
};

// One answer from the service's control socket.
struct Reply {
    bool ok = false;
    int id = 0;
    std::string shm, state, error;
};
bool parseReply(const std::string& line, Reply& r);
std::string jsonString(const std::string& s); // quoted and escaped

// The bundle's livedevice.json (Contents/Resources): which hack this copy of
// the plugin belongs to. $LIVEDEVICE_CONFIG, $LIVEDEVICE_SOCKET and
// $LIVEDEVICE_CLASSES override it (tests).
struct Config {
    std::string vendor, url, email, socket, classes;
};
const Config& config();

std::string classesPath();
const std::vector<ClassInfo>& classes();

} // namespace livedevice
