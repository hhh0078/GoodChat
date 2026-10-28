/** 文件作用说明：隔离浏览器录音生命周期，限制时长并在离开会话时释放麦克风。 */
"use client";
import { useEffect, useRef, useState } from "react";
import { App } from "antd";
import { errorMessage } from "@/lib/api";

/** useVoiceRecorder 仅在用户点击后请求录音，异步授权与卸载交错时不发送旧会话语音。 */
export function useVoiceRecorder(onRecorded: (file: File) => Promise<void>) {
  const { message } = App.useApp();
  const [recording, setRecording] = useState(false);
  const recorder = useRef<MediaRecorder | null>(null);
  const stream = useRef<MediaStream | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const mounted = useRef(true);
  const starting = useRef(false);
  const callback = useRef(onRecorded);
  callback.current = onRecorded;
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      if (timer.current) clearTimeout(timer.current);
      if (recorder.current) {
        recorder.current.onstop = null;
        if (recorder.current.state !== "inactive") recorder.current.stop();
      }
      stream.current?.getTracks().forEach((track) => track.stop());
    };
  }, []);
  /** toggleRecording 开始或结束本次录音，停止后以真实媒体文件交给附件发送流程。 */
  async function toggleRecording() {
    if (recorder.current?.state === "recording") {
      recorder.current.stop();
      return;
    }
    if (starting.current) return;
    if (!navigator.mediaDevices?.getUserMedia || typeof MediaRecorder === "undefined") {
      message.warning("当前浏览器不支持录音，请上传音频文件。");
      return;
    }
    starting.current = true;
    try {
      const media = await navigator.mediaDevices.getUserMedia({ audio: true });
      if (!mounted.current) {
        media.getTracks().forEach((track) => track.stop());
        return;
      }
      stream.current = media;
      const mime = ["audio/webm", "audio/mp4"].find((value) =>
        MediaRecorder.isTypeSupported(value),
      );
      const instance = new MediaRecorder(media, mime ? { mimeType: mime } : undefined);
      const chunks: BlobPart[] = [];
      recorder.current = instance;
      instance.ondataavailable = (event) => {
        if (event.data.size) chunks.push(event.data);
      };
      instance.onstop = () => {
        if (timer.current) clearTimeout(timer.current);
        media.getTracks().forEach((track) => track.stop());
        if (!mounted.current) return;
        setRecording(false);
        const blob = new Blob(chunks, { type: instance.mimeType });
        if (blob.size)
          void callback.current(
            new File(
              [blob],
              `语音-${Date.now()}.${instance.mimeType.includes("mp4") ? "m4a" : "webm"}`,
              { type: instance.mimeType },
            ),
          );
      };
      instance.start();
      setRecording(true);
      timer.current = setTimeout(() => {
        if (instance.state === "recording") instance.stop();
      }, 60000);
    } catch (problem) {
      message.error(errorMessage(problem));
      stream.current?.getTracks().forEach((track) => track.stop());
    } finally {
      starting.current = false;
    }
  }
  return { recording, toggleRecording };
}
