/*
 * bridge.c — see bridge.h. Two independent halves in one file for build
 * simplicity (one cgo preamble); they share nothing.
 */
#include "bridge.h"

#include <alsa/asoundlib.h>
#include <dlfcn.h>
#include <errno.h>
#include <sched.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static char g_last_error[512];

static void set_error(const char *fmt, ...) {
    va_list ap;
    va_start(ap, fmt);
    vsnprintf(g_last_error, sizeof(g_last_error), fmt, ap);
    va_end(ap);
}

const char *bridge_last_error(void) {
    return g_last_error;
}

/* ---- DSP plugin --------------------------------------------------------- */

typedef struct host_api_v1 {
    uint32_t api_version;
    int sample_rate;
    int frames_per_block;
    uint8_t *mapped_memory;
    int audio_out_offset;
    int audio_in_offset;
    void (*log)(const char *msg);
    int (*midi_send_internal)(const uint8_t *msg, int len);
    int (*midi_send_external)(const uint8_t *msg, int len);
} host_api_v1_t;

typedef struct plugin_api_v2 {
    uint32_t api_version;
    void* (*create_instance)(const char *module_dir, const char *json_defaults);
    void (*destroy_instance)(void *instance);
    void (*on_midi)(void *instance, const uint8_t *msg, int len, int source);
    void (*set_param)(void *instance, const char *key, const char *val);
    int (*get_param)(void *instance, const char *key, char *buf, int buf_len);
    int (*get_error)(void *instance, char *buf, int buf_len);
    void (*render_block)(void *instance, int16_t *out_interleaved_lr, int frames);
} plugin_api_v2_t;

typedef plugin_api_v2_t* (*move_plugin_init_v2_fn)(const host_api_v1_t *host);

struct bridge_plugin {
    void *dl_handle;
    plugin_api_v2_t *api;
    void *instance;
};

static void plugin_log(const char *msg) {
    fprintf(stderr, "[dsp] %s\n", msg);
}

bridge_plugin_t *bridge_plugin_load(const char *so_path, const char *module_dir,
                                     int sample_rate, int frames_per_block) {
    void *lib = dlopen(so_path, RTLD_NOW);
    if (!lib) {
        set_error("dlopen(%s): %s", so_path, dlerror());
        return NULL;
    }

    move_plugin_init_v2_fn init_fn =
        (move_plugin_init_v2_fn)dlsym(lib, "move_plugin_init_v2");
    if (!init_fn) {
        set_error("dlsym(move_plugin_init_v2): %s", dlerror());
        dlclose(lib);
        return NULL;
    }

    static host_api_v1_t host; /* static: outlives this call, api may keep the pointer */
    memset(&host, 0, sizeof(host));
    host.api_version = 1;
    host.sample_rate = sample_rate;
    host.frames_per_block = frames_per_block;
    host.log = plugin_log;

    plugin_api_v2_t *api = init_fn(&host);
    if (!api) {
        set_error("move_plugin_init_v2 returned NULL");
        dlclose(lib);
        return NULL;
    }

    void *instance = api->create_instance(module_dir, NULL);
    if (!instance) {
        set_error("create_instance failed");
        dlclose(lib);
        return NULL;
    }

    bridge_plugin_t *p = (bridge_plugin_t *)malloc(sizeof(bridge_plugin_t));
    p->dl_handle = lib;
    p->api = api;
    p->instance = instance;
    return p;
}

void bridge_plugin_unload(bridge_plugin_t *p) {
    if (!p) return;
    if (p->api && p->instance) p->api->destroy_instance(p->instance);
    if (p->dl_handle) dlclose(p->dl_handle);
    free(p);
}

void bridge_plugin_set_param(bridge_plugin_t *p, const char *key, const char *val) {
    if (!p || !p->api) return;
    p->api->set_param(p->instance, key, val);
}

int bridge_plugin_get_param(bridge_plugin_t *p, const char *key, char *buf, int buf_len) {
    if (!p || !p->api || !p->api->get_param) return -1;
    return p->api->get_param(p->instance, key, buf, buf_len);
}

void bridge_plugin_on_midi(bridge_plugin_t *p, const uint8_t *msg, int len) {
    if (!p || !p->api) return;
    p->api->on_midi(p->instance, msg, len, /* MOVE_MIDI_SOURCE_INTERNAL */ 0);
}

void bridge_plugin_render(bridge_plugin_t *p, int16_t *out_interleaved_lr, int frames) {
    if (!p || !p->api) return;
    p->api->render_block(p->instance, out_interleaved_lr, frames);
}

/* ---- ALSA PCM playback --------------------------------------------------- */

struct bridge_pcm {
    snd_pcm_t *pcm;
    unsigned int channels;
    snd_pcm_uframes_t period;
};

bridge_pcm_t *bridge_pcm_open(const char *device, unsigned int channels,
                               unsigned int rate, unsigned int period_hint,
                               unsigned int buffer_hint) {
    snd_pcm_t *pcm = NULL;
    int err = snd_pcm_open(&pcm, device, SND_PCM_STREAM_PLAYBACK, 0);
    if (err < 0) {
        set_error("snd_pcm_open(%s): %s", device, snd_strerror(err));
        return NULL;
    }

    snd_pcm_hw_params_t *hw;
    snd_pcm_hw_params_alloca(&hw);
    snd_pcm_hw_params_any(pcm, hw);
    snd_pcm_hw_params_set_access(pcm, hw, SND_PCM_ACCESS_RW_INTERLEAVED);
    snd_pcm_hw_params_set_format(pcm, hw, SND_PCM_FORMAT_S16_LE);
    snd_pcm_hw_params_set_channels(pcm, hw, channels);

    unsigned int rate_set = rate;
    snd_pcm_hw_params_set_rate_near(pcm, hw, &rate_set, 0);

    snd_pcm_uframes_t period = period_hint;
    snd_pcm_hw_params_set_period_size_near(pcm, hw, &period, 0);

    snd_pcm_uframes_t buffer = buffer_hint;
    snd_pcm_hw_params_set_buffer_size_near(pcm, hw, &buffer);

    err = snd_pcm_hw_params(pcm, hw);
    if (err < 0) {
        set_error("snd_pcm_hw_params(%s): %s (channels/rate must match "
                   "whatever the device already negotiated)", device, snd_strerror(err));
        snd_pcm_close(pcm);
        return NULL;
    }

    snd_pcm_hw_params_get_period_size(hw, &period, 0);

    err = snd_pcm_prepare(pcm);
    if (err < 0) {
        set_error("snd_pcm_prepare: %s", snd_strerror(err));
        snd_pcm_close(pcm);
        return NULL;
    }

    bridge_pcm_t *out = (bridge_pcm_t *)malloc(sizeof(bridge_pcm_t));
    out->pcm = pcm;
    out->channels = channels;
    out->period = period;
    return out;
}

unsigned int bridge_pcm_period(bridge_pcm_t *pcm) {
    return pcm ? (unsigned int)pcm->period : 0;
}

unsigned int bridge_pcm_channels(bridge_pcm_t *pcm) {
    return pcm ? pcm->channels : 0;
}

int bridge_pcm_writei(bridge_pcm_t *pcm, const int16_t *buf, unsigned int frames) {
    if (!pcm) return -1;
    snd_pcm_sframes_t written = snd_pcm_writei(pcm->pcm, buf, frames);
    if (written == -EPIPE || written == -ESTRPIPE) {
        snd_pcm_recover(pcm->pcm, (int)written, 1);
        written = snd_pcm_writei(pcm->pcm, buf, frames);
    }
    return (int)written;
}

void bridge_pcm_close(bridge_pcm_t *pcm) {
    if (!pcm) return;
    snd_pcm_drain(pcm->pcm);
    snd_pcm_close(pcm->pcm);
    free(pcm);
}

int bridge_set_realtime(int priority) {
    struct sched_param sp;
    memset(&sp, 0, sizeof(sp));
    sp.sched_priority = priority;
    return sched_setscheduler(0, SCHED_FIFO, &sp);
}
