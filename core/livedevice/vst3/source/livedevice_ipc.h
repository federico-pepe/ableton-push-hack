/*
 * livedevice_ipc.h: the shared memory between a device in Live (the
 * livedevice VST3 plugin) and its engine in a hack's process.
 *
 * One file per device, /dev/shm/<hack>-dev-<id>, mode 0666 (Live runs as
 * ableton, a hack as root). Fixed offsets: the Go side
 * (core/livedevice/ipc.go) has the same numbers, and ipc_test.go reads this
 * file to check they agree. Bump LD_IPC_VERSION on any change.
 *
 * One block:
 *   plugin: writes frames, transport, events, params, input; req++; wakes req
 *   hack:   processes; writes output and MIDI out; done = req; wakes done
 *   plugin: waits on done for at most half a block, else outputs silence
 */
#ifndef LIVEDEVICE_IPC_H
#define LIVEDEVICE_IPC_H

#define LD_IPC_MAGIC 0x5645444Cu /* "LDEV" */
#define LD_IPC_VERSION 2u /* 2: parameter changes from the hack back to Live */
#define LD_IPC_MAXF 4096 /* frames per block, at most */
#define LD_IPC_MAXEV 256
#define LD_IPC_MAXPAR 256

#define LD_OFF_MAGIC 0
#define LD_OFF_VERSION 4
#define LD_OFF_REQ 8      /* u32, futex: the plugin's block counter */
#define LD_OFF_DONE 12    /* u32, futex: the last block the hack finished */
#define LD_OFF_FRAMES 16  /* u32 */
#define LD_OFF_FLAGS 20   /* u32: bit0 playing, bit1 transport valid */
#define LD_OFF_BPM 24     /* f64 */
#define LD_OFF_BEAT 32    /* f64: quarter notes since song start */
#define LD_OFF_NEV 40     /* u32 */
#define LD_OFF_NPAR 44    /* u32 */
#define LD_OFF_LATE 48    /* u32: blocks the plugin gave up waiting for */
#define LD_OFF_ALIVE 52   /* u32: the hack's heartbeat, a counter */
#define LD_OFF_EVENTS 64  /* LD_IPC_MAXEV x { u32 offset; u8 len; u8 b[3]; } */
#define LD_OFF_PARAMS (LD_OFF_EVENTS + LD_IPC_MAXEV * 8) /* x { u32 index; f32 norm; } */
#define LD_OFF_IN_L (LD_OFF_PARAMS + LD_IPC_MAXPAR * 8)  /* f32[LD_IPC_MAXF] */
#define LD_OFF_IN_R (LD_OFF_IN_L + LD_IPC_MAXF * 4)
#define LD_OFF_OUT_L (LD_OFF_IN_R + LD_IPC_MAXF * 4)
#define LD_OFF_OUT_R (LD_OFF_OUT_L + LD_IPC_MAXF * 4)
#define LD_OFF_ONEV (LD_OFF_OUT_R + LD_IPC_MAXF * 4) /* u32: MIDI events the hack sends back */
#define LD_OFF_OEVENTS (LD_OFF_ONEV + 64)             /* LD_IPC_MAXEV x { u32 offset; u8 len; u8 b[3]; } */
#define LD_OFF_ONPAR (LD_OFF_OEVENTS + LD_IPC_MAXEV * 8) /* u32: parameter changes the hack sends back */
#define LD_OFF_OPARAMS (LD_OFF_ONPAR + 64)              /* LD_IPC_MAXPAR x { u32 index; f32 norm; } */
#define LD_IPC_SIZE (LD_OFF_OPARAMS + LD_IPC_MAXPAR * 8)

#endif
