/** 文件作用说明：以音量控制代替工作台提醒启停，保存真实偏好并独立申请系统通知。 */
"use client";
import { useState } from "react";
import { App, Button, Checkbox, Popover, Slider } from "antd";
import { AudioMutedOutlined, SoundOutlined } from "@ant-design/icons";
import { errorMessage } from "@/lib/api";
import type { useReminders } from "./reminders";
/** ReminderVolume 仅在用户操作时保存音量或申请通知权限，失败保持原偏好。 */
export function ReminderVolume({ reminders }: { reminders: ReturnType<typeof useReminders> }) {
  const { message } = App.useApp();
  const [preferenceBusy, setPreferenceBusy] = useState(false);
  const [volumeDraft, setVolumeDraft] = useState<number | null>(null);
  /** savePreferences 等待真实偏好保存，失败时保持现有设置并展示原因。 */
  async function savePreferences(values: Parameters<typeof reminders.updatePreferences>[0]) {
    setPreferenceBusy(true);
    try {
      await reminders.updatePreferences(values);
    } catch (problem) {
      message.error(errorMessage(problem));
    } finally {
      setPreferenceBusy(false);
      setVolumeDraft(null);
    }
  }
  /** changeSystemNotifications 只在明确选择后申请权限，失败不更改选择。 */
  async function changeSystemNotifications(enabled: boolean) {
    setPreferenceBusy(true);
    try {
      await reminders.setSystemNotifications(enabled);
    } catch (problem) {
      message.error(errorMessage(problem));
    } finally {
      setPreferenceBusy(false);
    }
  }
  const volume =
    volumeDraft ??
    (reminders.preferences?.muted || reminders.preferences?.sound_enabled === false
      ? 0
      : (reminders.preferences?.volume ?? 0.7));
  return (
    <Popover
      trigger="click"
      placement="bottomRight"
      title="消息音量"
      content={
        <div className="reminder-volume-panel">
          <div className="reminder-volume-label">
            <span>{volume === 0 ? "静音" : "提示音音量"}</span>
            <strong>{Math.round(volume * 100)}%</strong>
          </div>
          <p className="muted">仅设置你自己的账号。音量调至 0 即静音，图标会变红。</p>
          <Slider
            ariaLabelForHandle="消息提示音音量"
            min={0}
            max={1}
            step={0.05}
            value={volume}
            disabled={!reminders.preferences || preferenceBusy}
            onChange={setVolumeDraft}
            onChangeComplete={(value) =>
              void savePreferences({ volume: value, sound_enabled: true, muted: false })
            }
          />
          <Checkbox
            checked={reminders.preferences?.system_enabled || false}
            disabled={!reminders.preferences || preferenceBusy}
            onChange={(event) => void changeSystemNotifications(event.target.checked)}
          >
            浏览器系统通知
          </Checkbox>
          <p className="muted">
            浏览器通知会在页面处于后台时弹出，首次开启需允许通知权限；音量为 0 时仍可显示通知。
          </p>
          <p className="muted">
            {reminders.audioReady ? "提示音已就绪" : "点击或输入后，浏览器才允许播放提示音"}。
          </p>
          {!!reminders.error && <p className="preference-error">{errorMessage(reminders.error)}</p>}
        </div>
      }
    >
      <Button
        type="text"
        className={`reminder-volume-button ${volume === 0 ? "is-muted" : ""}`}
        icon={volume === 0 ? <AudioMutedOutlined /> : <SoundOutlined />}
        aria-label={`消息音量${Math.round(volume * 100)}%`}
      >
        <span className="agent-reminder-label">
          {volume === 0 ? "静音" : `${Math.round(volume * 100)}%`}
        </span>
      </Button>
    </Popover>
  );
}
