import math, struct, wave

RATE = 24000
DUR = 1.30

def strike(t, t0, f, tau):
    """One bell strike: fundamental plus two quiet partials, exponential decay."""
    if t < t0:
        return 0.0
    d = t - t0
    env = math.exp(-d / tau)
    # 6ms attack ramp so the onset has no click.
    env *= min(1.0, d / 0.006)
    return env * (
        math.sin(2 * math.pi * f * d)
        + 0.30 * math.sin(2 * math.pi * f * 2.0 * d) * math.exp(-d / (tau * 0.5))
        + 0.12 * math.sin(2 * math.pi * f * 3.01 * d) * math.exp(-d / (tau * 0.3))
    )

n = int(RATE * DUR)
samples = []
for i in range(n):
    t = i / RATE
    v = 0.85 * strike(t, 0.00, 880.00, 0.40) + 0.75 * strike(t, 0.26, 1318.51, 0.52)
    # Fade the tail to true zero so the file ends silent.
    v *= min(1.0, (DUR - t) / 0.08)
    samples.append(v)

peak = max(abs(v) for v in samples) or 1.0
frames = b''.join(struct.pack('<h', int(max(-1.0, min(1.0, v / peak * 0.72)) * 32767)) for v in samples)

with wave.open('cmd/junkie/static/sounds/chime.wav', 'wb') as w:
    w.setnchannels(1)
    w.setsampwidth(2)
    w.setframerate(RATE)
    w.writeframes(frames)
