/*
 * bridge.h — thin C bridge between Go (via cgo) and two things Go can't
 * do natively: dlopen a Move Anything plugin_api_v2 DSP module, and drive
 * ALSA PCM playback. Both halves are adaptations of already-proven code:
 * the plugin half mirrors ~/Developer/schwung-braids-main's own test
 * harness (validated 2026-08-28 against Braids' dsp.so); the PCM half
 * mirrors hacks/push-audio-loopback/src/loopback_feed.c.
 */
#ifndef BRIDGE_H
#define BRIDGE_H

#include <stdint.h>

/* ---- DSP plugin (plugin_api_v2) ---------------------------------------- */

typedef struct bridge_plugin bridge_plugin_t; /* opaque */

/* Loads so_path, calls its move_plugin_init_v2, and creates one instance
 * rooted at module_dir (for preset scanning; may be "."). Returns NULL on
 * any failure — call bridge_last_error() for why. */
bridge_plugin_t *bridge_plugin_load(const char *so_path, const char *module_dir,
                                     int sample_rate, int frames_per_block);
void bridge_plugin_unload(bridge_plugin_t *p);
void bridge_plugin_set_param(bridge_plugin_t *p, const char *key, const char *val);
/* Writes key's current value/metadata into buf (NUL-terminated) and returns
 * the length written, or -1 if key is unknown or buf is too small. "engine",
 * "engine_name", and "chain_params" (full JSON param list: key/name/type/
 * min/max/options per param) are the keys this host reads. */
int bridge_plugin_get_param(bridge_plugin_t *p, const char *key, char *buf, int buf_len);
/* msg is a raw 1-3 byte MIDI message (status, data1, data2); source=0
 * (MOVE_MIDI_SOURCE_INTERNAL) matches what braids_plugin.cpp expects for a
 * directly-played note. */
void bridge_plugin_on_midi(bridge_plugin_t *p, const uint8_t *msg, int len);
/* Renders frames of interleaved stereo (L,R,L,R,...) into out. out must
 * hold at least frames*2 int16_t. */
void bridge_plugin_render(bridge_plugin_t *p, int16_t *out_interleaved_lr, int frames);
const char *bridge_last_error(void);

/* ---- ALSA PCM playback -------------------------------------------------- */

typedef struct bridge_pcm bridge_pcm_t; /* opaque */

/* Opens device for playback at channels/rate, requesting period_hint
 * frames per period and buffer_hint frames total buffer (both are hints —
 * ALSA rounds to what the device/driver actually supports; read back the
 * granted period via bridge_pcm_period). Returns NULL on failure. */
bridge_pcm_t *bridge_pcm_open(const char *device, unsigned int channels,
                               unsigned int rate, unsigned int period_hint,
                               unsigned int buffer_hint);
unsigned int bridge_pcm_period(bridge_pcm_t *pcm);
unsigned int bridge_pcm_channels(bridge_pcm_t *pcm);
/* Blocking write of frames interleaved samples (channels per frame, per
 * bridge_pcm_channels). Returns frames written, or a negative ALSA error
 * code. Recovers from -EPIPE/-ESTRPIPE internally and retries once. */
int bridge_pcm_writei(bridge_pcm_t *pcm, const int16_t *buf, unsigned int frames);
void bridge_pcm_close(bridge_pcm_t *pcm);

/* Best-effort SCHED_FIFO for the calling thread (root only). Returns 0 on
 * success; a nonzero return is not fatal, just more exposed to scheduling
 * jitter — matches loopback_feed.c's own posture. */
int bridge_set_realtime(int priority);

#endif
