class FakeSong(object):
    def __init__(self):
        self.is_playing = False
        self.tempo = 120.0
        self.current_song_time = 0.0
        self.signature_numerator = 4
        self.signature_denominator = 4
        self.loop = False
        self.loop_start = 0.0
        self.loop_length = 4.0
        self.punch_in = False
        self.punch_out = False

    def start_playing(self):
        self.is_playing = True

    def stop_playing(self):
        self.is_playing = False
