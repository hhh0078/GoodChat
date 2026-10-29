/** 文件作用说明：隔离提醒偏好保存和领取响应的修订号，防止迟到请求重新开启已经取消的提示。 */
import type { RealtimeEvent } from "./realtime/client";

/** systemNotificationPreferences 在主动开启系统通知时将旧免打扰转换为零音量，保留静音而不阻挡通知。 */
export function systemNotificationPreferences(enabled: boolean, muted = false) {
  return enabled && muted
    ? { system_enabled: true, muted: false, volume: 0 }
    : { system_enabled: enabled };
}

/** createReminderRequests 为一个成员连接维护偏好保存与待播领取的独立修订号。 */
export function createReminderRequests() {
  let preferenceRevision = 0,
    preferenceChanges = 0,
    attentionRevision = 0;
  return {
    /** preferenceChanging 表示保存尚未确认，期间不使用旧偏好领取提醒。 */
    get preferenceChanging() {
      return preferenceChanges > 0;
    },
    /** preferenceRead 生成读取凭证，保存开始前发出的旧GET不能覆盖新设置。 */
    preferenceRead() {
      const revision = preferenceRevision;
      return () => revision === preferenceRevision && preferenceChanges === 0;
    },
    /** preferenceChange 开始保存并作废旧领取，返回可重复调用的保存结束检查。 */
    preferenceChange() {
      const revision = ++preferenceRevision;
      preferenceChanges++;
      attentionRevision++;
      let finished = false;
      return () => {
        if (!finished) {
          finished = true;
          preferenceChanges--;
        }
        return revision === preferenceRevision;
      };
    },
    /** attentionRead 生成领取凭证，已读或归属改变后旧响应不得播放。 */
    attentionRead() {
      const revision = attentionRevision;
      return () => revision === attentionRevision;
    },
    /** invalidateAttention 取消当前领取凭证，不依赖网络请求能否及时中止。 */
    invalidateAttention() {
      attentionRevision++;
    },
  };
}

/** reminderEventInvalidates 只把本人已读、归属转出、关闭和撤回作为取消旧待播的依据。 */
export function reminderEventInvalidates(event: RealtimeEvent, memberId: string) {
  if (event.event_type === "read.updated")
    return event.payload?.participant_kind === "agent" && event.payload.participant_id === memberId;
  if (event.event_type === "conversation.closed" || event.event_type === "message.withdrawn")
    return true;
  if (event.event_type !== "assignment.changed") return false;
  const payload = event.payload;
  return (
    payload?.accepted === false ||
    payload?.status === "queued" ||
    payload?.status === "closed" ||
    (!!payload && Object.hasOwn(payload, "agent_id") && payload.agent_id !== memberId)
  );
}
