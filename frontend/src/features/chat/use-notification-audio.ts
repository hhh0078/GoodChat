/** 文件作用说明：由用户交互解锁浏览器声音，缓存1秒受控提示音并在卸载时释放播放资源。 */
"use client";
import { useCallback, useEffect, useRef, useState } from "react";
import { notificationSound } from "@/lib/notification-sounds";

/** useNotificationAudio 只在音频上下文已解锁或主动试听时播放，异步加载后仍核对当前挂载状态。 */
export function useNotificationAudio(enabled = true) {
  const context = useRef<AudioContext | null>(null);
  const buffers = useRef(new Map<string, Promise<AudioBuffer>>());
  const active = useRef<AudioBufferSourceNode | null>(null);
  const mounted = useRef(true);
  const generation = useRef(0);
  const [ready, setReady] = useState(false);
  /** stop 停止当前声音并作废仍在下载或解码的旧播放请求。 */
  const stop = useCallback(() => {
    generation.current++;
    active.current?.stop();
    active.current = null;
  }, []);
  /** isReady 读取上下文真实状态，避免系统挂起后陈旧ready抢占其他标签提醒。 */
  const isReady = useCallback(() => context.current?.state === "running", []);
  /** unlock 由明确的点击或键盘交互恢复声音，不绕过浏览器播放政策。 */
  const unlock = useCallback(async () => {
    if (!enabled || typeof AudioContext === "undefined" || !mounted.current) return null;
    try {
      if (!context.current) {
        const created = new AudioContext();
        context.current = created;
        created.onstatechange = () => {
          if (context.current !== created || !mounted.current) return;
          setReady(created.state === "running");
          if (created.state !== "running") stop();
        };
      }
      if (context.current.state === "suspended") await context.current.resume();
      if (mounted.current) setReady(context.current.state === "running");
      return context.current;
    } catch {
      if (mounted.current) setReady(false);
      return null;
    }
  }, [enabled, stop]);
  useEffect(() => {
    mounted.current = true;
    if (!enabled) {
      setReady(false);
      return;
    }
    /** onInteraction 只在用户输入事件中发起解锁。 */
    function onInteraction() {
      void unlock();
    }
    document.addEventListener("pointerdown", onInteraction);
    document.addEventListener("keydown", onInteraction);
    return () => {
      mounted.current = false;
      document.removeEventListener("pointerdown", onInteraction);
      document.removeEventListener("keydown", onInteraction);
      stop();
      void context.current?.close();
      context.current = null;
      buffers.current.clear();
    };
  }, [unlock, enabled, stop]);
  /** play 播放固定音频资源，多个咨询在同一周期合并为一个声音，主动试听允许当次解锁。 */
  const play = useCallback(
    async (id: string | undefined, volume: number, preview = false) => {
      const audio = preview ? await unlock() : context.current;
      if (!audio || audio.state !== "running" || volume <= 0 || !mounted.current) return false;
      stop();
      const expected = generation.current;
      const sound = notificationSound(id);
      try {
        if (!buffers.current.has(sound.id)) {
          const loading = fetch(sound.url, { credentials: "omit", redirect: "error" }).then(
            async (response) => {
              if (!response.ok || Number(response.headers.get("content-length")) > 128 * 1024)
                throw new Error("提示音读取失败");
              const data = await response.arrayBuffer();
              if (data.byteLength > 128 * 1024) throw new Error("提示音资源超出限制");
              const buffer = await audio.decodeAudioData(data);
              if (buffer.duration < 0.8 || buffer.duration > 1.2)
                throw new Error("提示音时长不正确");
              return buffer;
            },
          );
          buffers.current.set(sound.id, loading);
        }
        const buffer = await buffers.current.get(sound.id)!;
        if (
          !mounted.current ||
          expected !== generation.current ||
          audio !== context.current ||
          audio.state !== "running"
        )
          return false;
        active.current?.stop();
        const source = audio.createBufferSource(),
          gain = audio.createGain();
        source.buffer = buffer;
        gain.gain.value = Math.max(0, Math.min(1, volume));
        source.connect(gain);
        gain.connect(audio.destination);
        active.current = source;
        source.onended = () => {
          source.disconnect();
          gain.disconnect();
          if (active.current === source) active.current = null;
        };
        source.start();
        return true;
      } catch {
        buffers.current.delete(sound.id);
        return false;
      }
    },
    [unlock, stop],
  );
  return { ready, play, stop, isReady };
}
