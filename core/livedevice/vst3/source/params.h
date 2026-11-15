// params.h: a device's parameters (the class file's "params") as VST3
// parameters.
//
// VST3 parameters are normalized 0..1. A parameter is a float with a range,
// an int, an enum with options, or a placeholder ("pad": an empty slot on
// Push's page). The controller uses these to show values in Live and on Push;
// the hack gets the normalized value.
#pragma once

#include <cmath>
#include <cstdio>
#include <cstdlib>
#include <string>
#include <vector>

namespace livedevice {

struct ParamMeta {
    std::string key, name, type, unit, format;
    double min = 0, max = 1, step = 0, def = 0;
    bool hasDefault = false;
    std::vector<std::string> options;

    bool isEnum() const { return type == "enum" && !options.empty(); }
    bool isInt() const { return type == "int"; }
    bool bipolar() const { return !isEnum() && min < 0 && max > 0 && std::fabs(min + max) < 1e-9; }

    // VST3 stepCount: 0 = continuous
    int stepCount() const {
        if (isEnum()) return (int)options.size() - 1;
        if (isInt()) return (int)std::lround(max - min);
        return 0;
    }
    double toPlain(double norm) const {
        if (isEnum()) return std::lround(norm * (options.size() - 1));
        double v = min + norm * (max - min);
        return isInt() ? std::round(v) : v;
    }
    double toNorm(double plain) const {
        if (isEnum()) return options.size() > 1 ? plain / (options.size() - 1) : 0;
        if (max <= min) return 0;
        double n = (plain - min) / (max - min);
        return n < 0 ? 0 : n > 1 ? 1 : n;
    }
    // what the module is written: the option word for an enum
    std::string moduleValue(double norm) const {
        double p = toPlain(norm);
        if (isEnum()) return options[(size_t)p];
        char b[64];
        if (isInt()) std::snprintf(b, sizeof b, "%d", (int)p);
        else std::snprintf(b, sizeof b, "%.6f", p);
        return b;
    }
    // what the module answers, read back as a plain value
    bool parseModuleValue(const std::string& s, double& plain) const {
        if (isEnum()) {
            for (size_t i = 0; i < options.size(); i++)
                if (options[i] == s) { plain = (double)i; return true; }
            char* end = nullptr;
            long i = std::strtol(s.c_str(), &end, 10);
            if (end && *end == 0 && i >= 0 && (size_t)i < options.size()) { plain = (double)i; return true; }
            return false;
        }
        char* end = nullptr;
        double v = std::strtod(s.c_str(), &end);
        if (end == s.c_str()) return false;
        plain = v;
        return true;
    }
    // what Live and Push show: "35 %", "2.41 kHz", "SINE^"
    std::string display(double norm) const {
        double p = toPlain(norm);
        if (isEnum()) return options[(size_t)p];
        std::string u = unit;
        double v = p;
        if (u == "%" && max <= 1.0001 && min >= -1.0001) v *= 100;
        char b[64];
        if (!format.empty() && format.find('%') != std::string::npos) std::snprintf(b, sizeof b, format.c_str(), v);
        else if (isInt()) std::snprintf(b, sizeof b, "%d", (int)std::lround(v));
        else if (u == "%" || std::fabs(v) >= 100) std::snprintf(b, sizeof b, "%.0f", v);
        else if (std::fabs(v) >= 10) std::snprintf(b, sizeof b, "%.1f", v);
        else std::snprintf(b, sizeof b, "%.2f", v);
        std::string s = b;
        if (bipolar() && v > 0) s = "+" + s;
        if (u == "Hz" && v >= 1000) {
            std::snprintf(b, sizeof b, "%.2f", v / 1000);
            return std::string(b) + " kHz";
        }
        return u.empty() ? s : s + " " + u;
    }
};


} // namespace livedevice
