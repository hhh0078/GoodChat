"""文件作用说明：用确定性的柔和合成音生成五个1秒提示音，无需外部音源或依赖。"""
import math
import struct
import wave
from pathlib import Path

RATE = 44100
DURATION = 1.0
TARGET = Path(__file__).resolve().parents[1] / "frontend/public/sounds"


def bell_note(time, onset, frequency, decay, brightness=0.2):
    """合成带柔和起音和自然衰减的音符，时间与频率分别以秒和赫兹表示。"""
    age = time - onset
    if age < 0:
        return 0.0
    attack = min(1.0, age / 0.015)
    envelope = attack * math.exp(-age / decay)
    return envelope * (math.sin(2 * math.pi * frequency * age) + brightness * math.sin(2 * math.pi * frequency * 2.01 * age))


def sample(sound, time):
    """生成当前提示音的单个采样，末尾淡出防止截断产生爆音。"""
    notes = {
        "gentle": [(0, 523.25, 0.28, 0.12), (0.22, 659.25, 0.28, 0.12), (0.45, 783.99, 0.3, 0.12)],
        "glass": [(0, 1174.66, 0.3, 0.06), (0.35, 1567.98, 0.35, 0.05)],
        "wood": [(0, 392, 0.08, 0.4), (0.22, 523.25, 0.1, 0.3), (0.5, 659.25, 0.15, 0.25)],
        "bell": [(0, 659.25, 0.4, 0.22), (0.28, 987.77, 0.4, 0.15)],
        "bubble": [(0.03, 740, 0.13, 0.08), (0.25, 554.37, 0.16, 0.08), (0.5, 830.61, 0.24, 0.06)],
    }
    value = sum(bell_note(time, *note) for note in notes[sound])
    return value * min(1.0, max(0.0, (DURATION - time) / 0.1))


def main():
    """写入单声道PCM WAV，逐个归一化并保留首尾静音平滑边界。"""
    TARGET.mkdir(parents=True, exist_ok=True)
    for sound in ("gentle", "glass", "wood", "bell", "bubble"):
        samples = [sample(sound, index / RATE) for index in range(int(RATE * DURATION))]
        peak = max(abs(value) for value in samples) or 1.0
        data = b"".join(struct.pack("<h", int(value / peak * 16000)) for value in samples)
        with wave.open(str(TARGET / f"{sound}.wav"), "wb") as output:
            output.setnchannels(1)
            output.setsampwidth(2)
            output.setframerate(RATE)
            output.writeframes(data)
        print(f"{sound}.wav: {DURATION:.1f}秒")


if __name__ == "__main__":
    main()
