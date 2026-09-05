/*
 * loopback_feed.c — low-jitter test-tone writer into an ALSA Loopback
 * device on Push 3, sized to Live's own negotiated hw_params.
 *
 * Context: `snd-aloop` (built from Push3's GPL kernel source, see
 * push-tethered-app's plans/) gives Live a selectable "Loopback" audio
 * device. A quick `speaker-test` into the cross-wired side proved audio
 * reaches Live and comes out Push3's real speaker/headphone jack — but
 * `speaker-test` is a generic tool with no shared clock with Live's own
 * audio engine, so the result was audibly glitchy. This tool exists to
 * fix that: match Live's exact hw_params and use ALSA's own blocking
 * backpressure (snd_pcm_writei) instead of a hand-rolled timer, so the
 * write rate self-paces to the real playback clock.
 *
 * Usage:
 *   loopback_feed <device> <channels> <rate> [freq_hz] [seconds]
 *
 * Example — matches what Live negotiated on its Loopback input on Push 3
 * (device 0 capture, cross-wired from device 1 playback):
 *   ./loopback_feed hw:Loopback,1,0 32 44100 440 10
 *
 * Build (needs libasound2-dev; see Makefile — cross-built via Docker
 * debian:bullseye, dynamic linker path fixed for AbletonOS's layout):
 *   make
 *
 * Run as root over SSH (SCHED_FIFO requires it; falls back to SCHED_OTHER
 * with a warning if not root — still usable, just more exposed to
 * scheduling jitter).
 */

#include <alsa/asoundlib.h>
#include <errno.h>
#include <math.h>
#include <sched.h>
#include <signal.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

#define PERIOD_FRAMES_HINT  128   /* matches Live's own negotiated period on Push 3 */
#define BUFFER_FRAMES_HINT  1536  /* 12 periods — absorbs this thread's own jitter;
                                   * the two ends of a Loopback pair may set
                                   * independent buffer/period sizes, only
                                   * format/channels/rate must match. */
#define RT_PRIORITY         50

static volatile sig_atomic_t g_running = 1;

static void on_signal(int sig) {
    (void)sig;
    g_running = 0;
}

static void fill_period(int16_t *buf, snd_pcm_uframes_t frames, unsigned int channels,
                         double *phase, double phase_inc, int16_t amplitude) {
    memset(buf, 0, (size_t)frames * channels * sizeof(int16_t));
    for (snd_pcm_uframes_t f = 0; f < frames; f++) {
        int16_t s = (int16_t)(amplitude * sin(*phase));
        /* Front L/R only (channels 0/1) — matches how a real Live track
         * would receive a stereo source; remaining channels stay silent. */
        buf[f * channels + 0] = s;
        if (channels > 1) buf[f * channels + 1] = s;
        *phase += phase_inc;
        if (*phase >= 2.0 * M_PI) *phase -= 2.0 * M_PI;
    }
}

int main(int argc, char **argv) {
    if (argc < 4) {
        fprintf(stderr, "usage: %s <device> <channels> <rate> [freq_hz=440] [seconds=0(infinite)]\n",
                argv[0]);
        return 2;
    }

    const char *device      = argv[1];
    unsigned int channels   = (unsigned int)strtoul(argv[2], NULL, 10);
    unsigned int rate       = (unsigned int)strtoul(argv[3], NULL, 10);
    double freq_hz          = argc > 4 ? strtod(argv[4], NULL) : 440.0;
    double seconds          = argc > 5 ? strtod(argv[5], NULL) : 0.0;

    signal(SIGINT, on_signal);
    signal(SIGTERM, on_signal);

    snd_pcm_t *pcm = NULL;
    int err = snd_pcm_open(&pcm, device, SND_PCM_STREAM_PLAYBACK, 0);
    if (err < 0) {
        fprintf(stderr, "snd_pcm_open(%s) failed: %s\n", device, snd_strerror(err));
        return 1;
    }

    snd_pcm_hw_params_t *hw;
    snd_pcm_hw_params_alloca(&hw);
    snd_pcm_hw_params_any(pcm, hw);
    snd_pcm_hw_params_set_access(pcm, hw, SND_PCM_ACCESS_RW_INTERLEAVED);
    snd_pcm_hw_params_set_format(pcm, hw, SND_PCM_FORMAT_S16_LE);
    snd_pcm_hw_params_set_channels(pcm, hw, channels);

    unsigned int rate_set = rate;
    snd_pcm_hw_params_set_rate_near(pcm, hw, &rate_set, 0);

    snd_pcm_uframes_t period = PERIOD_FRAMES_HINT;
    snd_pcm_hw_params_set_period_size_near(pcm, hw, &period, 0);

    snd_pcm_uframes_t buffer = BUFFER_FRAMES_HINT;
    snd_pcm_hw_params_set_buffer_size_near(pcm, hw, &buffer);

    err = snd_pcm_hw_params(pcm, hw);
    if (err < 0) {
        fprintf(stderr, "snd_pcm_hw_params(%s) failed: %s "
                "(channels/rate must match whatever the paired Loopback "
                "device already negotiated)\n", device, snd_strerror(err));
        snd_pcm_close(pcm);
        return 1;
    }

    snd_pcm_hw_params_get_period_size(hw, &period, 0);
    snd_pcm_hw_params_get_buffer_size(hw, &buffer);
    snd_pcm_hw_params_get_rate(hw, &rate_set, 0);

    fprintf(stderr, "loopback_feed: device=%s channels=%u rate=%u (requested %u) "
            "period=%lu buffer=%lu freq=%.1fHz\n",
            device, channels, rate_set, rate, (unsigned long)period,
            (unsigned long)buffer, freq_hz);

    struct sched_param sp = { .sched_priority = RT_PRIORITY };
    if (sched_setscheduler(0, SCHED_FIFO, &sp) != 0) {
        fprintf(stderr, "loopback_feed: warning: SCHED_FIFO unavailable (%s) — "
                "continuing SCHED_OTHER, more exposed to scheduling jitter\n",
                strerror(errno));
    }

    err = snd_pcm_prepare(pcm);
    if (err < 0) {
        fprintf(stderr, "snd_pcm_prepare failed: %s\n", snd_strerror(err));
        snd_pcm_close(pcm);
        return 1;
    }

    int16_t *buf = calloc((size_t)period * channels, sizeof(int16_t));
    if (!buf) {
        fprintf(stderr, "out of memory\n");
        snd_pcm_close(pcm);
        return 1;
    }

    double phase = 0.0;
    double phase_inc = 2.0 * M_PI * freq_hz / (double)rate_set;
    const int16_t amplitude = 16000; /* headroom below full-scale 32767 */

    struct timespec t_start, t_now;
    clock_gettime(CLOCK_MONOTONIC, &t_start);

    unsigned long frames_written = 0;
    unsigned long xruns = 0;

    while (g_running) {
        if (seconds > 0.0) {
            clock_gettime(CLOCK_MONOTONIC, &t_now);
            double elapsed = (t_now.tv_sec - t_start.tv_sec) +
                              (t_now.tv_nsec - t_start.tv_nsec) / 1e9;
            if (elapsed >= seconds) break;
        }

        fill_period(buf, period, channels, &phase, phase_inc, amplitude);

        snd_pcm_sframes_t written = snd_pcm_writei(pcm, buf, period);
        if (written == -EPIPE) {
            xruns++;
            snd_pcm_prepare(pcm);
            continue;
        } else if (written == -ESTRPIPE) {
            while ((err = snd_pcm_resume(pcm)) == -EAGAIN) {
                struct timespec ts = { .tv_sec = 0, .tv_nsec = 100 * 1000 * 1000 };
                nanosleep(&ts, NULL);
            }
            if (err < 0) snd_pcm_prepare(pcm);
            continue;
        } else if (written < 0) {
            fprintf(stderr, "snd_pcm_writei error: %s\n", snd_strerror((int)written));
            break;
        } else if ((snd_pcm_uframes_t)written != period) {
            fprintf(stderr, "short write: %ld/%lu frames\n", (long)written,
                    (unsigned long)period);
        }
        frames_written += (unsigned long)written;
    }

    snd_pcm_drain(pcm);
    snd_pcm_close(pcm);
    free(buf);

    fprintf(stderr, "loopback_feed: done. frames_written=%lu (%.2fs) xruns=%lu\n",
            frames_written, (double)frames_written / (double)rate_set, xruns);

    return 0;
}
