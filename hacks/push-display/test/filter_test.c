/* filter_test.c -- the hook's MIDI and LED filter logic, run with `make test`. */
#include "../src/push_hook.c"
static void ev(unsigned char *b, unsigned char type, unsigned char flags, uint32_t cc_or_len) {
    memset(b, 0, 28); b[0] = type; b[1] = flags;
    if (type == 10) memcpy(b + 20, &cc_or_len, 4);
    if (flags & 4) memcpy(b + 16, &cc_or_len, 4);
}
/* Feed process_display_out a frame in 4 chunks (2 frames' worth, as Push3
 * sends it) and check the mirror holds the first half and ends on an even seq. */
static int mirror_test(void) {
    static push_mirror_t mir; static unsigned char wire[TOTAL_BYTES];
    static const unsigned char hdr[16] = {0xFF,0xCC,0xAA,0x88};
    int bad = 0;
    memset(&mir, 0, sizeof mir); mirror = &mir; shm = NULL;
    for (int i = 0; i < TOTAL_BYTES; i++) wire[i] = (unsigned char)(i * 31 + 5);
    for (int f = 0; f < 2; f++) {
        process_display_out((unsigned char *)hdr, 16);
        if (!(mir.seq & 1)) { puts("seq not odd mid-frame"); bad = 1; }
        for (int off = 0; off < TOTAL_BYTES; off += TOTAL_BYTES / 4)
            process_display_out(wire + off, TOTAL_BYTES / 4);
        if (mir.seq != 2u * (f + 1)) { printf("seq %u after frame %d\n", mir.seq, f); bad = 1; }
    }
    if (memcmp(mir.pixels, wire, FRAME_BYTES) != 0) { puts("mirror pixels differ"); bad = 1; }
    mirror = NULL;
    return bad;
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
    if (mirror_test()) bad = 1;

    /* midipass: pads and CC 88+ that stay Live's */
    static push_midi_pass_t mp; memset(&mp, 0, sizeof mp); mp.magic = MIDI_PASS_MAGIC;
    midi_pass = &mp; midi_pass_retry = time(NULL) + 3600;
    unsigned char p[28*4]; size_t q = 0;
    ev(p+q, 6, 0, 0); p[q+17] = 60; q += 28;   /* pad note: passed with pads on */
    ev(p+q, 6, 0, 0); p[q+17] = 3;  q += 28;   /* knob touch note: held */
    ev(p+q, 10, 0, 118); q += 28;              /* Delete (118): passed with its bit */
    ev(p+q, 10, 0, 110); q += 28;              /* Device View (110): held */
    unsigned char p2[sizeof p]; memcpy(p2, p, sizeof p);
    filter_seq_write(p, q);                    /* pads off, no bits: all held */
    for (int i = 0; i < 4; i++) if (p[i*28] != 0) { printf("midipass off: event %d passed\n", i); bad = 1; }
    mp.pads = 1; mp.pass_hi[(118-88)/8] |= 1 << ((118-88) % 8);
    filter_seq_write(p2, q);
    unsigned char want2[4] = {6, 0, 10, 0};
    for (int i = 0; i < 4; i++) if (p2[i*28] != want2[i]) { printf("midipass on: event %d type %u, want %u\n", i, p2[i*28], want2[i]); bad = 1; }
    if (!pass_cc_num(118) || pass_cc_num(110) || !pass_cc_num(85) || pass_cc_num(200)) { puts("pass_cc_num"); bad = 1; }
    /* the input from Push's hardware (input_passes), pads on: the pads, their
     * pressure, the touch strip as pitch bend and as a mod wheel (CC 1) pass;
     * other notes and controllers without their bit are held */
    struct { unsigned char type; uint32_t n; int want; } in[] = {
        {6, 60, 1}, {7, 60, 1}, {8, 60, 1}, {6, 12, 0}, {6, 3, 0},
        {10, 1, 1}, {10, 20, 0}, {10, 118, 1}, {12, 0, 1}, {13, 0, 1}, {11, 0, 0}, {130, 0, 1},
    };
    for (size_t i = 0; i < sizeof in / sizeof in[0]; i++) {
        unsigned char e[28]; ev(e, in[i].type, 0, in[i].n);
        if (in[i].type >= 6 && in[i].type <= 8) e[17] = (unsigned char)in[i].n;
        if (input_passes(e) != in[i].want) { printf("input type %u n %u: %d, want %d\n", in[i].type, in[i].n, input_passes(e), in[i].want); bad = 1; }
    }
    mp.pads = 0; /* pads off: the strip, its mod wheel and the pads stay the hack's */
    {
        unsigned char e[28];
        ev(e, 10, 0, 1); if (input_passes(e)) { puts("CC 1 passed with pads off"); bad = 1; }
        ev(e, 13, 0, 0); if (input_passes(e)) { puts("pitch bend passed with pads off"); bad = 1; }
    }
    mp.magic = 0; /* a file that is not ours yet */
    if (pass_pads() || pass_cc_num(118)) { puts("midipass without magic passed"); bad = 1; }
    puts(bad ? "FAIL" : "PASS: mirror frame copied, notes and CC 20 held, Play passed, SysEx and its payload untouched, midipass pads and CC 88+, the input (the touch strip's mod wheel with the pads)");
    return bad;
}
