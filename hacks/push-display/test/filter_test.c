/* filter_test.c -- the hook's MIDI and LED filter logic, run with `make test`. */
#include "../src/push_hook.c"
static void ev(unsigned char *b, unsigned char type, unsigned char flags, uint32_t cc_or_len) {
    memset(b, 0, 28); b[0] = type; b[1] = flags;
    if (type == 10) memcpy(b + 20, &cc_or_len, 4);
    if (flags & 4) memcpy(b + 16, &cc_or_len, 4);
}
int main(void) {
    static push_midi_filt_t f; memset(&f, 0, sizeof f); f.magic = MIDI_FILT_MAGIC;
    f.pass_cc[85/8] |= 1 << (85 % 8); f.pass_cc[86/8] |= 1 << (86 % 8);
    midi_filt = &f;
    unsigned char b[28*5 + 6]; size_t o = 0;
    ev(b+o, 6, 0, 0); o += 28;                 /* note on  -> held */
    ev(b+o, 10, 0, 85); o += 28;               /* CC 85    -> passed */
    ev(b+o, 130, 4, 6); o += 28;               /* SysEx, 6 payload bytes */
    memcpy(b+o, "\xf0\x00\x21\x1d\x06\xf7", 6); o += 6;
    ev(b+o, 10, 0, 20); o += 28;               /* CC 20    -> held */
    ev(b+o, 7, 0, 0); o += 28;                 /* note off -> held */
    unsigned char want[5] = {0, 10, 130, 0, 0}; size_t at[5] = {0, 28, 56, 90, 118};
    f.enabled = 0; filter_seq_write(b, o);     /* not filtering: the hook never calls it, but types must survive a disabled call path */
    f.enabled = 1; filter_seq_write(b, o);
    int bad = 0;
    for (int i = 0; i < 5; i++) if (b[at[i]] != want[i]) { printf("event %d type %u, want %u\n", i, b[at[i]], want[i]); bad = 1; }
    if (memcmp(b + 84, "\xf0\x00\x21\x1d\x06\xf7", 6) != 0) { puts("SysEx payload changed"); bad = 1; }
    if (led_held != 3) { printf("held %lu, want 3\n", led_held); bad = 1; }
    puts(bad ? "FAIL" : "PASS: notes and CC 20 held, Play passed, SysEx and its payload untouched");
    return bad;
}
