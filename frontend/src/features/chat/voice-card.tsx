/** 文件作用说明：以紧凑语音卡片播放已授权的音频，展示真实时长和进度并保持单一播放。 */
"use client";
import { useRef, useState } from "react";
import { Button, Slider } from "antd";
import { AudioOutlined, PauseOutlined, PlayCircleFilled } from "@ant-design/icons";

/** voiceTime 将真实秒数转换为分秒，非法媒体时长不伪造显示值。 */
function voiceTime(seconds: number): string {
  const value = Number.isFinite(seconds) ? Math.max(0, Math.floor(seconds)) : 0;
  return `${Math.floor(value / 60)}:${String(value % 60).padStart(2, "0")}`;
}
/** VoiceCard 只使用附件授权产生的临时地址，播放器失败时给出明确提示。 */
export function VoiceCard({ url, name }: { url: string; name: string }) {
  const audio = useRef<HTMLAudioElement | null>(null);
  const [playing, setPlaying] = useState(false);
  const [duration, setDuration] = useState(0);
  const [position, setPosition] = useState(0);
  const [error, setError] = useState("");
  /** toggle 处理浏览器播放拒绝，并在播放本条前暂停其他语音。 */
  async function toggle() {
    const node = audio.current;
    if (!node) return;
    if (!node.paused) {
      node.pause();
      return;
    }
    try {
      document.querySelectorAll("audio").forEach((item) => {
        if (item !== node) item.pause();
      });
      await node.play();
      setError("");
    } catch {
      setError("语音暂时无法播放，请重试或检查浏览器音频支持。");
    }
  }
  return (
    <div className={`voice-card ${playing ? "is-playing" : ""}`}>
      <audio
        ref={audio}
        src={url}
        preload="metadata"
        onLoadedMetadata={(event) =>
          setDuration(
            Number.isFinite(event.currentTarget.duration) ? event.currentTarget.duration : 0,
          )
        }
        onDurationChange={(event) =>
          setDuration(
            Number.isFinite(event.currentTarget.duration) ? event.currentTarget.duration : 0,
          )
        }
        onTimeUpdate={(event) => setPosition(event.currentTarget.currentTime)}
        onPlay={() => setPlaying(true)}
        onPause={() => setPlaying(false)}
        onEnded={() => setPlaying(false)}
        onError={() => {
          setPlaying(false);
          setError("当前浏览器无法读取此语音。");
        }}
      />
      <Button
        type="text"
        className="voice-play"
        icon={playing ? <PauseOutlined /> : <PlayCircleFilled />}
        aria-label={playing ? "暂停语音" : "播放语音"}
        onClick={() => void toggle()}
      />
      <div className="voice-card-main">
        <div className="voice-card-label">
          <span>
            <AudioOutlined /> 语音消息
          </span>
          <time>
            {voiceTime(position)} / {duration ? voiceTime(duration) : "—:—"}
          </time>
        </div>
        <Slider
          ariaLabelForHandle={`语音播放进度 ${name}`}
          min={0}
          max={duration || 1}
          step={0.1}
          value={Math.min(position, duration || position)}
          disabled={!duration}
          tooltip={{ formatter: (value) => voiceTime(value || 0) }}
          onChange={(value) => {
            if (audio.current) {
              audio.current.currentTime = value;
              setPosition(value);
            }
          }}
        />
      </div>
      {error && (
        <p className="voice-card-error" role="alert">
          {error}
        </p>
      )}
    </div>
  );
}
