// proxy.cpp: the livedevice plugin, a thin proxy (core/livedevice).
//
// One VST3 class per device a hack describes (its class file). The engine
// runs in the hack's process, outside Live: each audio block is handed over
// through shared memory (livedevice_ipc.h) and the answer is waited for at
// most half a block. A late or missing hack means silence, never a stall, and
// never a crash inside Live. This is the only push-hack code inside Live's
// process, so it stays small. Every hack installs the same binary under its
// own name, with a livedevice.json that names its socket and class file.

#include "classes.h"
#include "livedevice_ipc.h"

#include "base/source/fstreamer.h"
#include "pluginterfaces/base/ibstream.h"
#include "pluginterfaces/base/ustring.h"
#include "pluginterfaces/vst/ivstevents.h"
#include "pluginterfaces/vst/ivstmidicontrollers.h"
#include "pluginterfaces/vst/ivstparameterchanges.h"
#include "pluginterfaces/vst/ivstprocesscontext.h"
#include "public.sdk/source/main/pluginfactory.h"
#include "public.sdk/source/vst/vstaudioeffect.h"
#include "public.sdk/source/vst/vsteditcontroller.h"
#include "public.sdk/source/vst/vstparameters.h"

#include <atomic>
#include <chrono>
#include <cmath>
#include <cstring>
#include <fcntl.h>
#include <linux/futex.h>
#include <mutex>
#include <poll.h>
#include <sys/mman.h>
#include <sys/socket.h>
#include <sys/syscall.h>
#include <sys/un.h>
#include <thread>
#include <unistd.h>

using namespace Steinberg;
using namespace Steinberg::Vst;

#define LD_PLUGIN_VERSION "0.1.0"

namespace livedevice {

// hidden parameters that carry MIDI controllers (VST3 has no CC events)
enum : ParamID { kPitchBend = 1000, kModWheel = 1001, kPressure = 1002, kSustain = 1003 };

static FUID uidOf(const ClassInfo& c, bool controller) {
    uint32 l[4] = {0, 0, 0, 0};
    for (int i = 0; i < 4; i++) l[i] = (uint32)std::stoul(c.uid.substr(i * 8, 8), nullptr, 16);
    if (controller) l[0] ^= 0x5A5A5A5Au;
    return FUID(l[0], l[1], l[2], l[3]);
}

// toUtf16: a class file's UTF-8 (names, option texts) as a VST3 string of
// at most max characters. Byte by byte (fromAscii) made every non-ASCII
// character two wrong ones.
static void toUtf16(TChar* dst, const std::string& s, int max = 127) {
    int n = 0;
    for (size_t i = 0; i < s.size() && n < max;) {
        uint32_t c = (uint8_t)s[i], len = c < 0x80 ? 1 : c < 0xE0 ? 2 : c < 0xF0 ? 3 : 4;
        if (c >= 0x80 && c < 0xC0) { i++; continue; } // a stray continuation byte
        if (i + len > s.size()) break;
        uint32_t cp = len == 1 ? c : len == 2 ? c & 0x1F : len == 3 ? c & 0x0F : c & 0x07;
        for (uint32_t k = 1; k < len; k++) cp = cp << 6 | ((uint8_t)s[i + k] & 0x3F);
        i += len;
        if (cp >= 0x10000) {
            if (n + 2 > max) break;
            cp -= 0x10000;
            dst[n++] = (TChar)(0xD800 + (cp >> 10));
            dst[n++] = (TChar)(0xDC00 + (cp & 0x3FF));
        } else {
            dst[n++] = (TChar)cp;
        }
    }
    dst[n] = 0;
}

static inline uint32_t* u32(uint8_t* m, int off) { return reinterpret_cast<uint32_t*>(m + off); }
static inline float* f32(uint8_t* m, int off) { return reinterpret_cast<float*>(m + off); }
static inline double* f64(uint8_t* m, int off) { return reinterpret_cast<double*>(m + off); }

static void futexWake(uint32_t* a) { syscall(SYS_futex, a, FUTEX_WAKE, 1 << 30, nullptr, nullptr, 0); }
static void futexWait(uint32_t* a, uint32_t v, long ns) {
    timespec ts{ns / 1000000000L, ns % 1000000000L};
    syscall(SYS_futex, a, FUTEX_WAIT, v, &ts, nullptr, 0);
}

// ---- the control socket -------------------------------------------------------

class Link {
public:
    ~Link() { close(); }
    void close() {
        if (fd_ >= 0) ::close(fd_);
        fd_ = -1;
        buf_.clear();
    }
    // One request, one answer line, within timeoutMs. false: no service.
    bool call(const std::string& line, Reply& r, int timeoutMs) {
        std::lock_guard<std::mutex> g(mu_);
        if (fd_ < 0 && !connect()) return false;
        std::string out = line + "\n";
        if (::send(fd_, out.data(), out.size(), MSG_NOSIGNAL) != (ssize_t)out.size()) { close(); return false; }
        auto end = std::chrono::steady_clock::now() + std::chrono::milliseconds(timeoutMs);
        for (;;) {
            size_t nl = buf_.find('\n');
            if (nl != std::string::npos) {
                std::string one = buf_.substr(0, nl);
                buf_.erase(0, nl + 1);
                return parseReply(one, r);
            }
            int left = (int)std::chrono::duration_cast<std::chrono::milliseconds>(end - std::chrono::steady_clock::now()).count();
            if (left <= 0) { close(); return false; }
            pollfd p{fd_, POLLIN, 0};
            if (poll(&p, 1, left) <= 0) { close(); return false; }
            char tmp[65536];
            ssize_t n = ::recv(fd_, tmp, sizeof tmp, 0);
            if (n <= 0) { close(); return false; }
            buf_.append(tmp, (size_t)n);
        }
    }
    bool connected() const { return fd_ >= 0; }

private:
    bool connect() {
        fd_ = ::socket(AF_UNIX, SOCK_STREAM | SOCK_CLOEXEC, 0);
        if (fd_ < 0) return false;
        sockaddr_un a{};
        a.sun_family = AF_UNIX;
        std::strncpy(a.sun_path, config().socket.c_str(), sizeof a.sun_path - 1);
        if (::connect(fd_, (sockaddr*)&a, sizeof a) != 0) { ::close(fd_); fd_ = -1; return false; }
        return true;
    }
    std::mutex mu_;
    int fd_ = -1;
    std::string buf_;
};

// ---- processor ------------------------------------------------------------------

class Processor : public AudioEffect {
public:
    explicit Processor(const ClassInfo* c) : ci_(c), norm_(c->params.size(), -1.0) {
        setControllerClass(uidOf(*c, true));
    }
    static FUnknown* createInstance(void* ctx) { return (IAudioProcessor*)new Processor((const ClassInfo*)ctx); }

    tresult PLUGIN_API initialize(FUnknown* context) override {
        tresult r = AudioEffect::initialize(context);
        if (r != kResultOk) return r;
        addEventInput(STR16("Events"), 16);
        if (ci_->kind == "midifx") {
            addEventOutput(STR16("Events Out"), 16); // a MIDI effect: MIDI in, MIDI out, no audio
        } else {
            if (ci_->kind == "effect") addAudioInput(STR16("In"), SpeakerArr::kStereo);
            addAudioOutput(STR16("Out"), SpeakerArr::kStereo);
            // a MIDI effect on its own track: other tracks take its notes
            // with Live's "MIDI From: <track> / <this device>"
            if (ci_->kind == "midi") addEventOutput(STR16("Events Out"), 16);
        }
        stop_ = false;
        bg_ = std::thread([this] { keepConnected(); });
        return kResultOk;
    }
    tresult PLUGIN_API terminate() override {
        stop_ = true;
        if (bg_.joinable()) bg_.join();
        unmap();
        if (id_) {
            Reply r;
            link_.call("{\"op\":\"destroy\",\"id\":" + std::to_string(id_) + "}", r, 500);
        }
        return AudioEffect::terminate();
    }
    tresult PLUGIN_API setBusArrangements(SpeakerArrangement* in, int32 nIn, SpeakerArrangement* out, int32 nOut) override {
        bool effect = ci_->kind == "effect";
        if (ci_->kind == "midifx") return (nIn == 0 && nOut == 0) ? kResultTrue : kResultFalse;
        if (nOut == 1 && out[0] == SpeakerArr::kStereo && nIn == (effect ? 1 : 0) && (!effect || in[0] == SpeakerArr::kStereo))
            return AudioEffect::setBusArrangements(in, nIn, out, nOut);
        return kResultFalse;
    }
    tresult PLUGIN_API canProcessSampleSize(int32 s) override { return s == kSample32 ? kResultTrue : kResultFalse; }
    tresult PLUGIN_API setupProcessing(ProcessSetup& s) override {
        rate_ = s.sampleRate > 0 ? s.sampleRate : 44100;
        return AudioEffect::setupProcessing(s);
    }

    tresult PLUGIN_API process(ProcessData& d) override {
        uint8_t* m = shm_.load(std::memory_order_acquire);
        int frames = d.numSamples;
        float* outL = (d.numOutputs > 0 && d.outputs[0].numChannels > 1) ? d.outputs[0].channelBuffers32[0] : nullptr;
        float* outR = outL ? d.outputs[0].channelBuffers32[1] : nullptr;
        int nev = 0, npar = 0;
        if (m) {
            transport(m, d.processContext);
            params(m, d.inputParameterChanges, nev, npar);
            events(m, d.inputEvents, nev);
            *u32(m, LD_OFF_NEV) = (uint32_t)nev;
            *u32(m, LD_OFF_NPAR) = (uint32_t)npar;
        } else {
            remember(d.inputParameterChanges); // applied once the service answers
        }
        bool midiOnly = ci_->kind == "midifx";
        if (!outL && !midiOnly) return kResultOk;
        if (!m || frames <= 0 || frames > LD_IPC_MAXF) {
            if (outL) silence(d, frames);
            return kResultOk;
        }
        if (ci_->kind == "effect" && d.numInputs > 0 && d.inputs[0].numChannels > 1) {
            std::memcpy(f32(m, LD_OFF_IN_L), d.inputs[0].channelBuffers32[0], sizeof(float) * frames);
            std::memcpy(f32(m, LD_OFF_IN_R), d.inputs[0].channelBuffers32[1], sizeof(float) * frames);
        }
        *u32(m, LD_OFF_FRAMES) = (uint32_t)frames;
        uint32_t req = __atomic_add_fetch(u32(m, LD_OFF_REQ), 1, __ATOMIC_SEQ_CST);
        futexWake(u32(m, LD_OFF_REQ));
        // at most half a block: Live's deadline is the block, ours is half of it
        auto deadline = std::chrono::steady_clock::now() +
                        std::chrono::nanoseconds((long)(0.5e9 * frames / rate_));
        for (;;) {
            uint32_t done = __atomic_load_n(u32(m, LD_OFF_DONE), __ATOMIC_ACQUIRE);
            if (done == req) break;
            long left = (long)std::chrono::duration_cast<std::chrono::nanoseconds>(deadline - std::chrono::steady_clock::now()).count();
            if (left <= 0) {
                __atomic_add_fetch(u32(m, LD_OFF_LATE), 1, __ATOMIC_RELAXED);
                if (outL) silence(d, frames);
                return kResultOk;
            }
            futexWait(u32(m, LD_OFF_DONE), done, left);
        }
        paramsOut(m, d.outputParameterChanges);
        if (midiOnly) {
            eventsOut(m, d.outputEvents);
            return kResultOk;
        }
        if (ci_->kind == "midi") eventsOut(m, d.outputEvents);
        std::memcpy(outL, f32(m, LD_OFF_OUT_L), sizeof(float) * frames);
        std::memcpy(outR, f32(m, LD_OFF_OUT_R), sizeof(float) * frames);
        d.outputs[0].silenceFlags = 0;
        return kResultOk;
    }

    // parameter changes made on the hack's side (its web page, its own view)
    // this block, back to Live: its controller, Push's device view and
    // automation follow them. Remembered too, so a reconnect sends them.
    void paramsOut(uint8_t* m, IParameterChanges* out) {
        uint32_t n = *u32(m, LD_OFF_ONPAR);
        if (n > LD_IPC_MAXPAR) n = LD_IPC_MAXPAR;
        for (uint32_t i = 0; i < n; i++) {
            uint32_t id = *u32(m, LD_OFF_OPARAMS + i * 8);
            double v = *f32(m, LD_OFF_OPARAMS + i * 8 + 4);
            if (id >= norm_.size()) continue;
            norm_[id] = v;
            if (!out) continue;
            int32 qi = 0;
            if (IParamValueQueue* q = out->addParameterData((ParamID)id, qi)) {
                int32 pi = 0;
                q->addPoint(0, v, pi);
            }
        }
    }

    // what a MIDI effect played this block, back to Live
    void eventsOut(uint8_t* m, IEventList* out) {
        if (!out) return;
        uint32_t n = *u32(m, LD_OFF_ONEV);
        if (n > LD_IPC_MAXEV) n = LD_IPC_MAXEV;
        for (uint32_t i = 0; i < n; i++) {
            uint8_t* e = m + LD_OFF_OEVENTS + i * 8;
            uint8_t len = e[4], st = e[5] & 0xF0, ch = e[5] & 0x0F;
            Event ev{};
            ev.busIndex = 0;
            ev.sampleOffset = (int32)*u32(e, 0);
            if (len == 3 && (st == 0x90 && e[7] > 0)) {
                ev.type = Event::kNoteOnEvent;
                ev.noteOn = {ch, e[6], 0.f, e[7] / 127.f, 0, -1};
            } else if (len == 3 && (st == 0x80 || st == 0x90)) {
                ev.type = Event::kNoteOffEvent;
                ev.noteOff = {ch, e[6], e[7] / 127.f, -1, 0.f};
            } else if (len == 3 && st == 0xA0) {
                ev.type = Event::kPolyPressureEvent;
                ev.polyPressure = {ch, e[6], e[7] / 127.f, -1};
            } else {
                continue; // controllers have no event form in VST3
            }
            out->addEvent(ev);
        }
    }

    // The state is the hack's own text (Device.State); Live saves it in the Set.
    tresult PLUGIN_API getState(IBStream* s) override {
        if (id_) {
            Reply r;
            if (link_.call("{\"op\":\"get_state\",\"id\":" + std::to_string(id_) + "}", r, 2000) && r.ok) {
                std::lock_guard<std::mutex> g(stateMu_);
                state_ = r.state;
            }
        }
        std::string st;
        {
            std::lock_guard<std::mutex> g(stateMu_);
            st = state_;
        }
        IBStreamer w(s, kLittleEndian);
        w.writeInt32u((uint32)st.size());
        if (!st.empty()) s->write((void*)st.data(), (int32)st.size(), nullptr);
        return kResultOk;
    }
    tresult PLUGIN_API setState(IBStream* s) override {
        IBStreamer rd(s, kLittleEndian);
        uint32 n = 0;
        if (!rd.readInt32u(n) || n > (16u << 20)) return kResultFalse;
        std::string st(n, '\0');
        int32 got = 0;
        if (n && s->read(st.data(), (int32)n, &got) != kResultOk) return kResultFalse;
        {
            std::lock_guard<std::mutex> g(stateMu_);
            state_ = st;
        }
        if (id_ && n) {
            Reply r;
            link_.call("{\"op\":\"set_state\",\"id\":" + std::to_string(id_) + ",\"state\":" + jsonString(st) + "}", r, 3000);
        }
        return kResultOk;
    }

private:
    void silence(ProcessData& d, int frames) {
        for (int c = 0; c < d.outputs[0].numChannels; c++)
            std::memset(d.outputs[0].channelBuffers32[c], 0, sizeof(float) * frames);
        d.outputs[0].silenceFlags = 3;
    }
    void transport(uint8_t* m, ProcessContext* c) {
        uint32_t fl = 0;
        if (c && (c->state & ProcessContext::kTempoValid)) {
            *f64(m, LD_OFF_BPM) = c->tempo;
            fl |= 2;
            if (c->state & ProcessContext::kPlaying) fl |= 1;
            *f64(m, LD_OFF_BEAT) = (c->state & ProcessContext::kProjectTimeMusicValid) ? c->projectTimeMusic : -1.0;
        }
        *u32(m, LD_OFF_FLAGS) = fl;
    }
    void remember(IParameterChanges* ch) {
        if (!ch) return;
        for (int32 i = 0; i < ch->getParameterCount(); i++) {
            IParamValueQueue* q = ch->getParameterData(i);
            int32 off;
            ParamValue v;
            if (q && q->getPointCount() > 0 && q->getParameterId() < norm_.size() &&
                q->getPoint(q->getPointCount() - 1, off, v) == kResultOk)
                norm_[q->getParameterId()] = v;
        }
    }
    void addEvent(uint8_t* m, int& nev, uint32_t off, std::initializer_list<uint8_t> b) {
        if (nev >= LD_IPC_MAXEV) return;
        uint8_t* e = m + LD_OFF_EVENTS + nev * 8;
        *u32(e - 0, 0) = off;
        e[4] = (uint8_t)b.size();
        int i = 0;
        for (uint8_t x : b) e[5 + i++] = x;
        nev++;
    }
    void params(uint8_t* m, IParameterChanges* ch, int& nev, int& npar) {
        // values remembered while there was no service go first, once
        if (resend_) {
            for (size_t i = 0; i < norm_.size() && npar < LD_IPC_MAXPAR; i++) {
                if (norm_[i] < 0) continue;
                *u32(m, LD_OFF_PARAMS + npar * 8) = (uint32_t)i;
                *f32(m, LD_OFF_PARAMS + npar * 8 + 4) = (float)norm_[i];
                npar++;
            }
            resend_ = false;
        }
        if (!ch) return;
        for (int32 i = 0; i < ch->getParameterCount(); i++) {
            IParamValueQueue* q = ch->getParameterData(i);
            if (!q || q->getPointCount() <= 0) continue;
            int32 off;
            ParamValue v;
            if (q->getPoint(q->getPointCount() - 1, off, v) != kResultOk) continue;
            ParamID id = q->getParameterId();
            switch (id) {
            case kPitchBend: {
                int b = (int)std::lround(v * 16383);
                addEvent(m, nev, off, {0xE0, (uint8_t)(b & 127), (uint8_t)(b >> 7)});
                continue;
            }
            case kModWheel: addEvent(m, nev, off, {0xB0, 1, (uint8_t)std::lround(v * 127)}); continue;
            case kPressure: addEvent(m, nev, off, {0xD0, (uint8_t)std::lround(v * 127)}); continue;
            case kSustain: addEvent(m, nev, off, {0xB0, 64, (uint8_t)(v >= 0.5 ? 127 : 0)}); continue;
            }
            if (id >= norm_.size() || npar >= LD_IPC_MAXPAR) continue;
            norm_[id] = v;
            *u32(m, LD_OFF_PARAMS + npar * 8) = id;
            *f32(m, LD_OFF_PARAMS + npar * 8 + 4) = (float)v;
            npar++;
        }
    }
    void events(uint8_t* m, IEventList* ev, int& nev) {
        if (!ev) return;
        for (int32 i = 0; i < ev->getEventCount(); i++) {
            Event e{};
            if (ev->getEvent(i, e) != kResultOk) continue;
            uint32_t off = (uint32_t)e.sampleOffset;
            switch (e.type) {
            case Event::kNoteOnEvent:
                addEvent(m, nev, off, {(uint8_t)(0x90 | (e.noteOn.channel & 15)), (uint8_t)(e.noteOn.pitch & 127),
                                       (uint8_t)std::max(1, (int)std::lround(e.noteOn.velocity * 127))});
                break;
            case Event::kNoteOffEvent:
                addEvent(m, nev, off, {(uint8_t)(0x80 | (e.noteOff.channel & 15)), (uint8_t)(e.noteOff.pitch & 127),
                                       (uint8_t)std::lround(e.noteOff.velocity * 127)});
                break;
            case Event::kPolyPressureEvent:
                addEvent(m, nev, off, {(uint8_t)(0xA0 | (e.polyPressure.channel & 15)), (uint8_t)(e.polyPressure.pitch & 127),
                                       (uint8_t)std::lround(e.polyPressure.pressure * 127)});
                break;
            default:
                break;
            }
        }
    }

    // keepConnected: create the device in the service, map its memory, and
    // do it again whenever the service restarts (its heartbeat stops).
    void keepConnected() {
        uint32_t lastAlive = 0;
        int quiet = 0;
        while (!stop_) {
            uint8_t* m = shm_.load();
            if (!m) {
                Reply r;
                // creating a device loads its engine, which can take seconds (a
                // large module, a ROM boot): 2 s gave up on a device that was
                // still loading, and the retry loaded it a second time
                if (link_.call("{\"op\":\"create\",\"uid\":\"" + ci_->uid + "\"}", r, 20000) && r.ok && map(r.shm)) {
                    id_ = r.id;
                    std::string st;
                    {
                        std::lock_guard<std::mutex> g(stateMu_);
                        st = state_;
                    }
                    if (!st.empty()) {
                        Reply sr;
                        link_.call("{\"op\":\"set_state\",\"id\":" + std::to_string(id_) + ",\"state\":" + jsonString(st) + "}", sr, 3000);
                    }
                    resend_ = true;
                    shm_.store(mapped_, std::memory_order_release);
                    quiet = 0;
                } else {
                    sleepMs(1000);
                }
                continue;
            }
            sleepMs(250);
            uint32_t a = __atomic_load_n(u32(m, LD_OFF_ALIVE), __ATOMIC_RELAXED) + __atomic_load_n(u32(m, LD_OFF_DONE), __ATOMIC_RELAXED);
            quiet = (a == lastAlive) ? quiet + 1 : 0;
            lastAlive = a;
            if (quiet >= 8 || !link_.connected()) { // 2 s without a heartbeat: the service is gone
                shm_.store(nullptr);
                sleepMs(50); // let a running process() finish with the old mapping
                unmap();
                id_ = 0;
                link_.close();
            }
        }
    }
    bool map(const std::string& path) {
        int fd = ::open(path.c_str(), O_RDWR | O_CLOEXEC);
        if (fd < 0) return false;
        void* p = mmap(nullptr, LD_IPC_SIZE, PROT_READ | PROT_WRITE, MAP_SHARED, fd, 0);
        ::close(fd);
        if (p == MAP_FAILED) return false;
        if (*u32((uint8_t*)p, LD_OFF_MAGIC) != LD_IPC_MAGIC || *u32((uint8_t*)p, LD_OFF_VERSION) != LD_IPC_VERSION) {
            munmap(p, LD_IPC_SIZE);
            return false;
        }
        mapped_ = (uint8_t*)p;
        return true;
    }
    void unmap() {
        shm_.store(nullptr);
        if (mapped_) munmap(mapped_, LD_IPC_SIZE);
        mapped_ = nullptr;
    }
    void sleepMs(int ms) {
        for (int i = 0; i < ms / 50 && !stop_; i++) std::this_thread::sleep_for(std::chrono::milliseconds(50));
    }

    const ClassInfo* ci_;
    std::vector<double> norm_;
    Link link_;
    std::thread bg_;
    std::atomic<bool> stop_{false};
    std::atomic<uint8_t*> shm_{nullptr};
    uint8_t* mapped_ = nullptr;
    std::atomic<bool> resend_{false};
    int id_ = 0;
    double rate_ = 44100;
    std::mutex stateMu_;
    std::string state_;
};

// ---- controller -----------------------------------------------------------------

class DeviceParam : public Parameter {
public:
    DeviceParam(const ParamMeta& m, ParamID id) : meta_(m) {
        info.id = id;
        toUtf16(info.title, m.name);
        toUtf16(info.shortTitle, m.name, 8);
        // no unit: the value text carries it ("2.41 kHz"), Live would add a second one
        info.units[0] = 0;
        info.stepCount = m.stepCount();
        info.defaultNormalizedValue = m.hasDefault ? m.def : 0; // the class file stores it normalized
        info.flags = ParameterInfo::kCanAutomate | (m.isEnum() ? ParameterInfo::kIsList : 0);
        if (m.type == "pad") { // an empty slot on Push's page
            // A host needs a title; a no-break space reads as nothing.
            // Read only, Live shows it as "Parameter #N 0.12" whatever its title
            // (measured 2026-11-14); a plain parameter that will not move instead.
            info.flags = ParameterInfo::kCanAutomate;
            info.title[0] = info.shortTitle[0] = 0x00A0;
            info.title[1] = info.shortTitle[1] = 0;
        }
        setNormalized(info.defaultNormalizedValue);
    }
    void toString(ParamValue norm, String128 out) const override {
        if (meta_.type == "pad") { out[0] = 0x00A0; out[1] = 0; return; } // empty prints a number
        toUtf16(out, meta_.display(norm));
    }
    bool fromString(const TChar* s, ParamValue& norm) const override {
        std::string a;
        for (int i = 0; s && s[i] && i < 128; i++) a += s[i] < 128 ? (char)s[i] : '?';
        if (meta_.isEnum()) {
            for (size_t i = 0; i < meta_.options.size(); i++)
                if (meta_.options[i] == a) { norm = meta_.toNorm((double)i); return true; }
            return false;
        }
        double v = std::strtod(a.c_str(), nullptr);
        if (meta_.unit == "%" && meta_.max <= 1.0001) v /= 100;
        norm = meta_.toNorm(v);
        return true;
    }
    bool setNormalized(ParamValue v) override {
        if (meta_.type == "pad") v = 0; // nothing behind it: the knob stays put
        return Parameter::setNormalized(v);
    }
    ParamValue toPlain(ParamValue n) const override { return meta_.toPlain(n); }
    ParamValue toNormalized(ParamValue p) const override { return meta_.toNorm(p); }

private:
    ParamMeta meta_;
};

class Controller : public EditController, public IMidiMapping {
public:
    explicit Controller(const ClassInfo* c) : ci_(c) {}
    static FUnknown* createInstance(void* ctx) { return (IEditController*)new Controller((const ClassInfo*)ctx); }

    tresult PLUGIN_API initialize(FUnknown* context) override {
        tresult r = EditController::initialize(context);
        if (r != kResultOk) return r;
        for (size_t i = 0; i < ci_->params.size(); i++) parameters.addParameter(new DeviceParam(ci_->params[i], (ParamID)i));
        if (ci_->kind == "instrument" || ci_->kind == "midi") {
            parameters.addParameter(STR16("Pitch Bend"), nullptr, 0, 0.5, ParameterInfo::kIsHidden, kPitchBend);
            parameters.addParameter(STR16("Mod Wheel"), nullptr, 0, 0, ParameterInfo::kIsHidden, kModWheel);
            parameters.addParameter(STR16("Pressure"), nullptr, 0, 0, ParameterInfo::kIsHidden, kPressure);
            parameters.addParameter(STR16("Sustain"), nullptr, 1, 0, ParameterInfo::kIsHidden, kSustain);
        }
        return kResultOk;
    }
    tresult PLUGIN_API getMidiControllerAssignment(int32 bus, int16 ch, CtrlNumber cc, ParamID& id) override {
        if (bus != 0 || (ci_->kind != "instrument" && ci_->kind != "midi")) return kResultFalse;
        switch (cc) {
        case kPitchBend: id = livedevice::kPitchBend; return kResultTrue;
        case kCtrlModWheel: id = livedevice::kModWheel; return kResultTrue;
        case kAfterTouch: id = livedevice::kPressure; return kResultTrue;
        case kCtrlSustainOnOff: id = livedevice::kSustain; return kResultTrue;
        }
        return kResultFalse;
    }

    OBJ_METHODS(Controller, EditController)
    DEFINE_INTERFACES
        DEF_INTERFACE(IMidiMapping)
    END_DEFINE_INTERFACES(EditController)
    REFCOUNT_METHODS(EditController)

private:
    const ClassInfo* ci_;
};

} // namespace livedevice

// ---- factory: one class per device the hack describes ---------------------------

SMTG_EXPORT_SYMBOL IPluginFactory* PLUGIN_API GetPluginFactory() {
    static CPluginFactory* factory = nullptr;
    if (factory) {
        factory->addRef();
        return factory;
    }
    const auto& cfg = livedevice::config();
    PFactoryInfo info(cfg.vendor.c_str(), cfg.url.c_str(), cfg.email.c_str(), PFactoryInfo::kUnicode);
    factory = new CPluginFactory(info);
    for (const auto& c : livedevice::classes()) {
        TUID pid, cid;
        livedevice::uidOf(c, false).toTUID(pid);
        livedevice::uidOf(c, true).toTUID(cid);
        std::string ctl = c.name + " Controller";
        PClassInfo2 proc(pid, PClassInfo::kManyInstances, kVstAudioEffectClass, c.name.c_str(), Vst::kDistributable,
                         c.category.c_str(), cfg.vendor.c_str(), LD_PLUGIN_VERSION, kVstVersionString);
        PClassInfo2 cont(cid, PClassInfo::kManyInstances, kVstComponentControllerClass, ctl.c_str(), 0, "",
                         cfg.vendor.c_str(), LD_PLUGIN_VERSION, kVstVersionString);
        factory->registerClass(&proc, livedevice::Processor::createInstance, (void*)&c);
        factory->registerClass(&cont, livedevice::Controller::createInstance, (void*)&c);
    }
    return factory;
}
