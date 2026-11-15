// playtest: hosts a livedevice bundle the way Live does and checks that sound
// comes back through its hack: a note into an instrument, a tone through an
// effect, notes out of a MIDI device. Prints the peak, the time each block
// took and the late blocks.
//
//   playtest <Bundle.vst3> "<class name>" [blocks]
#include "public.sdk/source/vst/hosting/eventlist.h"
#include "public.sdk/source/vst/hosting/hostclasses.h"
#include "public.sdk/source/vst/hosting/module.h"
#include "public.sdk/source/vst/hosting/parameterchanges.h"
#include "public.sdk/source/vst/hosting/plugprovider.h"
#include "public.sdk/source/vst/hosting/processdata.h"
#include "public.sdk/source/vst/utility/stringconvert.h"
#include "pluginterfaces/vst/ivstaudioprocessor.h"
#include "pluginterfaces/vst/ivstcomponent.h"

#include <chrono>
#include <cmath>
#include <cstdio>
#include <cstring>
#include <string>
#include <thread>

using namespace Steinberg;
using namespace Steinberg::Vst;

int main(int argc, char** argv) {
    if (argc < 3) {
        std::fprintf(stderr, "usage: %s <Bundle.vst3> <class name> [blocks]\n", argv[0]);
        return 2;
    }
    int blocks = argc > 3 ? std::atoi(argv[3]) : 1500;
    std::string err;
    auto module = VST3::Hosting::Module::create(argv[1], err);
    if (!module) { std::fprintf(stderr, "load: %s\n", err.c_str()); return 1; }
    auto factory = module->getFactory();
    PluginContextFactory::instance().setPluginContext(new HostApplication());
    VST3::Hosting::ClassInfo found;
    bool ok = false;
    for (auto& ci : factory.classInfos())
        if (ci.category() == kVstAudioEffectClass && ci.name() == argv[2]) { found = ci; ok = true; }
    if (!ok) { std::fprintf(stderr, "no class %s\n", argv[2]); return 1; }
    auto provider = owned(new PlugProvider(factory, found, true));
    if (!provider->initialize()) { std::fprintf(stderr, "initialize failed\n"); return 1; }
    auto component = provider->getComponentPtr();
    // the titles as a host reads them (build.sh checks one that Go escapes)
    if (auto ctl = provider->getControllerPtr()) {
        for (int32 k = 0; k < ctl->getParameterCount(); k++) {
            ParameterInfo pi{};
            if (ctl->getParameterInfo(k, pi) == kResultOk && pi.title[0] != 0x00A0)
                std::printf("param: %s\n", VST3::StringConvert::convert(pi.title).c_str());
        }
    }
    FUnknownPtr<IAudioProcessor> proc(component);
    bool effect = component->getBusCount(kAudio, kInput) > 0;
    bool midiOnly = component->getBusCount(kAudio, kOutput) == 0;
    bool midiOut = component->getBusCount(kEvent, kOutput) > 0;

    const int frames = 128;
    const double rate = 44100;
    ProcessSetup setup{kRealtime, kSample32, frames, rate};
    proc->setupProcessing(setup);
    if (!midiOnly) component->activateBus(kAudio, kOutput, 0, true);
    if (midiOut) component->activateBus(kEvent, kOutput, 0, true);
    if (effect) component->activateBus(kAudio, kInput, 0, true);
    component->activateBus(kEvent, kInput, 0, true);
    component->setActive(true);
    proc->setProcessing(true);

    HostProcessData data;
    data.prepare(*component, frames, kSample32);
    EventList events, outEvents;
    ParameterChanges changes;
    data.inputEvents = &events;
    data.outputEvents = &outEvents;
    data.inputParameterChanges = &changes;
    ProcessContext ctx{};
    ctx.state = ProcessContext::kTempoValid | ProcessContext::kPlaying | ProcessContext::kProjectTimeMusicValid;
    ctx.tempo = 120;
    ctx.sampleRate = rate;
    data.processContext = &ctx;
    data.numSamples = frames; // a host sets it for every block
    for (int c = 0; !midiOnly && c < data.outputs[0].numChannels; c++) // a missed write must read as silence
        std::memset(data.outputs[0].channelBuffers32[c], 0, sizeof(float) * frames);
    int notesOut = 0;

    // wait for the plugin to reach the service (its background thread)
    std::this_thread::sleep_for(std::chrono::milliseconds(1500));

    double peak = 0, worst = 0, sum = 0;
    int counted = 0, sounding = 0;
    for (int b = 0; b < blocks; b++) {
        events.clear();
        outEvents.clear();
        if (!effect && b % 100 == 0) {
            Event e{};
            e.type = Event::kNoteOnEvent;
            e.noteOn.pitch = 48 + (b / 100) % 12;
            e.noteOn.velocity = 0.9f;
            events.addEvent(e);
        }
        if (!effect && b % 100 == 60) {
            Event e{};
            e.type = Event::kNoteOffEvent;
            e.noteOff.pitch = 48 + (b / 100) % 12;
            events.addEvent(e);
        }
        if (effect) {
            for (int i = 0; i < frames; i++) {
                float v = 0.4f * std::sin(2 * M_PI * 220 * (b * frames + i) / rate);
                data.inputs[0].channelBuffers32[0][i] = v;
                data.inputs[0].channelBuffers32[1][i] = v;
            }
        }
        ctx.projectTimeMusic = b * frames / rate * 2;
        auto t0 = std::chrono::steady_clock::now();
        tresult pr = proc->process(data);
        (void)pr;
        double us = std::chrono::duration<double, std::micro>(std::chrono::steady_clock::now() - t0).count();
        if (b > 10) { worst = std::max(worst, us); sum += us; counted++; }
        for (int32 i = 0; i < outEvents.getEventCount(); i++) {
            Event e{};
            if (outEvents.getEvent(i, e) == kResultOk && e.type == Event::kNoteOnEvent) notesOut++;
        }
        if (midiOnly) {
            std::this_thread::sleep_for(std::chrono::microseconds((long)(1e6 * frames / rate) - (long)us));
            continue;
        }
        double bp = 0;
        for (int i = 0; i < frames; i++)
            bp = std::max(bp, (double)std::fabs(data.outputs[0].channelBuffers32[0][i]));
        peak = std::max(peak, bp);
        if (bp > 0.001) sounding++;
        std::this_thread::sleep_for(std::chrono::microseconds((long)(1e6 * frames / rate) - (long)us));
    }
    proc->setProcessing(false);
    if (midiOnly || midiOut) {
        std::printf("%s: %d notes in, %d notes out, hand-off avg %.0f us, worst %.0f us\n", argv[2], blocks / 100,
                    notesOut, sum / std::max(1, counted), worst);
        return notesOut > 0 ? 0 : 3;
    }
    component->setActive(false);
    std::printf("%s: peak %.3f, %d of %d blocks sound, hand-off avg %.0f us, worst %.0f us (block %.0f us)\n",
                argv[2], peak, sounding, blocks, sum / std::max(1, counted), worst, 1e6 * frames / rate);
    return peak > 0.001 ? 0 : 3;
}
