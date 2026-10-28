/** 文件作用说明：在独立来源运行已发布装修，并用最小权限 MessageChannel 桥接真实访客聊天。 */
"use client";
import { useEffect, useRef, useState } from "react";
import { Alert, App, Button, Modal } from "antd";
import {
  publicBridgeMessage,
  requestBridgeAttachment,
  requestBridgeAvatar,
  safeRenderURL,
  validBridgeHandshake,
  validateBridgeRequest,
  visibleBridgeAttachment,
} from "@/lib/frame-bridge";
import { errorMessage } from "@/lib/api";
import { exportFilename, startFileDownload } from "@/lib/csv-download";
import { uploadAndSendAttachment, type AttachmentAttempt } from "@/lib/attachment-upload";
import { useChat, type ChatIdentity } from "./use-chat";
import { AttachmentSender } from "./attachment-sender";
import { UnconfirmedMessages } from "./unconfirmed-messages";
import { useVisitorReminders } from "./use-visitor-reminders";
import type { ChatMessage } from "@/lib/types";
type Decoration = { render_url: string; release_id: string };
type PendingDownload = { id: string; name: string };
/** DecoratedVisitor 将跨源页面限制在当前访客当前咨询的白名单能力内。 */
export function DecoratedVisitor({
  id,
  identity,
  decoration,
  onStatus,
  notificationSound,
}: {
  id: string;
  identity: ChatIdentity;
  decoration: Decoration;
  onStatus: (value: string) => void;
  notificationSound?: string;
}) {
  const chat = useChat(id, identity);
  useVisitorReminders(chat.messages, {
    enabled: true,
    loading: chat.loading || !chat.conversation,
    conversationId: id,
    sound: notificationSound,
  });
  const { message } = App.useApp();
  const latest = useRef(chat);
  latest.current = chat;
  const frame = useRef<HTMLIFrameElement | null>(null);
  const channel = useRef<MessagePort | null>(null);
  const mutations = useRef(new Map<string, AttachmentAttempt>());
  const visible = useRef(false);
  const firstLoad = useRef(false);
  const [ready, setReady] = useState(false);
  const [bridgeError, setBridgeError] = useState("");
  const [revision, setRevision] = useState(0);
  const [pendingDownload, setPendingDownload] = useState<PendingDownload | null>(null);
  const downloadRequest = useRef<PendingDownload | null>(null);
  const downloadController = useRef<AbortController | null>(null);
  const [downloading, setDownloading] = useState(false);
  const [downloadError, setDownloadError] = useState<unknown>(null);
  const renderURL =
    typeof location === "undefined" ? null : safeRenderURL(decoration.render_url, location.origin);
  const source = renderURL?.toString() || "";
  const origin = renderURL?.origin || "";
  /** closeDownload 废止下载确认及尚未完成的请求，旧身份不能继续下载。 */
  function closeDownload() {
    downloadController.current?.abort();
    downloadController.current = null;
    downloadRequest.current = null;
    setPendingDownload(null);
    setDownloading(false);
    setDownloadError(null);
  }
  /** confirmDownload 仅由可信父页面确认按钮触发，重新授权后发起实际文件下载。 */
  async function confirmDownload() {
    const pending = downloadRequest.current;
    if (!pending || downloadController.current) return;
    const controller = new AbortController();
    downloadController.current = controller;
    setDownloading(true);
    setDownloadError(null);
    try {
      const file = await requestBridgeAttachment(
        pending.id,
        () => latest.current.messages,
        identity.token,
        controller.signal,
      );
      if (controller.signal.aborted || pending !== downloadRequest.current) return;
      startFileDownload(file.blob, exportFilename(null, file.name));
      closeDownload();
      message.success("下载已发起，请查看浏览器下载记录。");
    } catch (problem) {
      if (!controller.signal.aborted) setDownloadError(problem);
    } finally {
      if (downloadController.current === controller) {
        downloadController.current = null;
        setDownloading(false);
      }
    }
  }
  /** publicMessage 依据当前访客身份标记本人消息，原始发送者编号不会进入租户代码。 */
  function publicMessage(item: ChatMessage) {
    const result = publicBridgeMessage(item, identity.actorId);
    return {
      ...result,
      sender_avatar_url: result.sender_avatar_url
        ? new URL(result.sender_avatar_url, location.origin).href
        : undefined,
    };
  }
  /** uploadAttachment 为装修SDK与父层通用入口复用校验、上传缓存和幂等发送。 */
  async function uploadAttachment(file: unknown, name: unknown, requestID: string) {
    const current = latest.current;
    if (!current.conversation || current.conversation.status === "closed")
      throw new Error("当前会话无法发送附件");
    let command = mutations.current.get(requestID);
    if (!command) {
      command = { clientId: crypto.randomUUID() };
      mutations.current.set(requestID, command);
    }
    return publicMessage(
      await uploadAndSendAttachment(
        file,
        name,
        command,
        (form) => current.request("/attachments", { method: "POST", body: form }),
        (body) => current.send(body),
      ),
    );
  }
  useEffect(() => {
    if (chat.conversation) onStatus(chat.conversation.status);
  }, [chat.conversation, onStatus]);
  /** state 取得最小聊天态，附件只返回可见元信息，二进制必须再次授权下载。 */
  function state() {
    const value = latest.current;
    return {
      protocol_version: 1,
      conversation: value.conversation
        ? {
            id: value.conversation.id,
            status: value.conversation.status,
            agent_name: value.conversation.agent_name,
          }
        : null,
      messages: value.messages.filter((item) => item.audience !== "internal").map(publicMessage),
      connected: value.connected,
      loading: value.loading,
      is_typing: value.typingUntil > Date.now(),
      peer_read_seq: value.readSeq,
      has_older: value.hasOlder,
      error: value.error ? errorMessage(value.error) : null,
    };
  }
  useEffect(() => {
    if (ready) channel.current?.postMessage({ type: "state", data: state() });
  }, [
    chat.messages,
    chat.conversation,
    chat.connected,
    chat.typingUntil,
    chat.readSeq,
    chat.loading,
    chat.error,
    ready,
  ]);
  useEffect(() => {
    if (!source || !origin) return;
    closeDownload();
    setReady(false);
    setBridgeError("");
    firstLoad.current = false;
    const observer = new IntersectionObserver((entries) => {
      visible.current = entries.some((entry) => entry.isIntersecting);
    });
    if (frame.current) observer.observe(frame.current);
    let calls = 0;
    let windowStart = Date.now();
    let disposed = false;
    let activeRequests = 0;
    const commands = new Map<string, { signature: string; result: Promise<unknown> }>();
    const acceptedNonces = new Set<string>();
    const scopeController = new AbortController();
    /** dispatch 严格校验方法、结构、长度及本会话边界，未知方法不转发给 API。 */
    async function dispatch(method: string, payload: Record<string, unknown>, requestID: string) {
      const current = latest.current;
      validateBridgeRequest(method, payload, current.messages, identity.actorId);
      if (method === "getState") return state();
      if (method === "getAvatar")
        return {
          file: await requestBridgeAvatar(
            String(payload.message_id),
            () => latest.current.messages,
            scopeController.signal,
          ),
        };
      if (method === "sendText") {
        const text = payload.text ?? payload.content;
        if (typeof text !== "string" || !text.trim() || text.length > 10000)
          throw new Error("请输入不超过10000字的消息");
        let command = mutations.current.get(requestID);
        if (!command) {
          command = { clientId: crypto.randomUUID() };
          mutations.current.set(requestID, command);
          if (mutations.current.size > 128)
            mutations.current.delete(mutations.current.keys().next().value!);
        }
        const clientID = payload.client_message_id || command.clientId;
        if (
          clientID !== undefined &&
          (typeof clientID !== "string" ||
            !/^[\da-f]{8}-[\da-f]{4}-[\da-f]{4}-[\da-f]{4}-[\da-f]{12}$/i.test(clientID))
        )
          throw new Error("消息编号格式无效");
        return publicMessage(
          await current.send({
            client_message_id: typeof clientID === "string" ? clientID : crypto.randomUUID(),
            content: text,
            message_type: "text",
            audience: "public",
            reply_to_id: typeof payload.reply_to_id === "string" ? payload.reply_to_id : undefined,
          }),
        );
      }
      if (method === "withdrawMessage") {
        await current.withdraw(String(payload.message_id));
        return { id: payload.message_id, withdrawn: true };
      }
      if (method === "typing") {
        const active = payload.active ?? payload.is_typing;
        if (typeof active !== "boolean") throw new Error("输入状态格式无效");
        current.typing(active);
        return { accepted: true };
      }
      if (method === "upload") {
        return uploadAttachment(payload.file ?? payload.blob, payload.name, requestID);
      }
      if (method === "download") {
        const file = await requestBridgeAttachment(
          String(payload.attachment_id),
          () => latest.current.messages,
          identity.token,
          scopeController.signal,
        );
        return {
          file: file.blob,
          name: file.name,
          content_type: file.content_type,
        };
      }
      if (method === "saveAttachment") {
        const attachment = visibleBridgeAttachment(current.messages, payload.attachment_id);
        if (downloadRequest.current) throw new Error("已有附件等待下载确认，请先完成或取消");
        const pending = { id: attachment.id, name: attachment.file_name };
        downloadRequest.current = pending;
        setPendingDownload(pending);
        setDownloadError(null);
        return { confirmation_required: true };
      }
      if (method === "read") {
        const seq = payload.last_read_seq;
        if (
          typeof seq !== "string" ||
          !/^\d+$/.test(seq) ||
          !current.messages.some((item) => (item.seq || item.event_seq) === seq)
        )
          throw new Error("已读位置不属于当前可见消息");
        if (document.visibilityState !== "visible" || !visible.current) return { accepted: false };
        return current.request(`/conversations/${id}/read-cursor`, {
          method: "POST",
          bodyJSON: { last_read_seq: seq },
        });
      }
      if (method === "loadOlder") {
        await current.loadOlder();
        return state();
      }
      throw new Error("当前页面无权调用此方法");
    }
    /** handshake 只接受指定 iframe、精确来源、版本和发布编号的一次性握手。 */
    function handshake(event: MessageEvent) {
      const input = event.data;
      if (
        !validBridgeHandshake(
          input,
          event.origin,
          origin,
          event.source === frame.current?.contentWindow,
          decoration.release_id,
        ) ||
        acceptedNonces.has(input.nonce) ||
        channel.current
      )
        return;
      acceptedNonces.add(input.nonce);
      const connection = new MessageChannel();
      channel.current = connection.port1;
      connection.port1.onmessage = async (event) => {
        const request = event.data;
        if (
          !request ||
          typeof request.id !== "string" ||
          !request.id ||
          request.id.length > 80 ||
          typeof request.method !== "string" ||
          (request.payload !== undefined &&
            (!request.payload ||
              typeof request.payload !== "object" ||
              Array.isArray(request.payload)))
        )
          return;
        let counted = false;
        try {
          if (Date.now() - windowStart > 10000) {
            calls = 0;
            windowStart = Date.now();
          }
          if (++calls > 80 || activeRequests >= 4) throw new Error("请求过于频繁，请稍后再试");
          activeRequests++;
          counted = true;
          const signature = JSON.stringify({ method: request.method, payload: request.payload });
          let command = commands.get(request.id);
          if (command && command.signature !== signature)
            throw new Error("相同请求编号携带了不同内容");
          if (!command) {
            command = {
              signature,
              result: dispatch(request.method, request.payload || {}, request.id),
            };
            if (["sendText", "upload", "withdrawMessage"].includes(request.method)) {
              commands.set(request.id, command);
              if (commands.size > 128) commands.delete(commands.keys().next().value!);
            }
          }
          const result = await command.result;
          if (!disposed) connection.port1.postMessage({ id: request.id, result });
        } catch (problem) {
          commands.delete(request.id);
          if (!disposed)
            connection.port1.postMessage({
              id: request.id,
              error: { code: "BRIDGE_REQUEST_FAILED", message: errorMessage(problem) },
            });
        } finally {
          if (counted) activeRequests = Math.max(0, activeRequests - 1);
        }
      };
      connection.port1.start();
      (event.source as Window).postMessage(
        {
          type: "goodchat:port",
          version: 1,
          release_id: decoration.release_id,
          nonce: input.nonce,
        },
        origin,
        [connection.port2],
      );
      connection.port1.postMessage({ type: "state", data: state() });
      setReady(true);
      setBridgeError("");
    }
    window.addEventListener("message", handshake);
    const timeout = setTimeout(() => {
      if (!channel.current && !disposed)
        setBridgeError("装修页面尚未连接聊天能力，请重新加载或联系商户。");
    }, 10000);
    return () => {
      disposed = true;
      clearTimeout(timeout);
      observer.disconnect();
      window.removeEventListener("message", handshake);
      channel.current?.close();
      channel.current = null;
      scopeController.abort();
      closeDownload();
    };
    // 会话和发布来源改变时必须废止整个旧通道，业务状态通过 latest 读取。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [source, origin, decoration.release_id, id, identity.token, identity.actorId, revision]);
  if (!source)
    return (
      <Alert
        type="error"
        showIcon
        title="此入口的装修来源配置无效"
        description="需要平台配置独立的装修运行来源后才能打开该发布版本。"
      />
    );
  return (
    <div style={{ display: "flex", flexDirection: "column", flex: 1, minHeight: 0 }}>
      {bridgeError && (
        <Alert
          type="warning"
          title={bridgeError}
          action={
            <Button size="small" onClick={() => setRevision((value) => value + 1)}>
              重新加载
            </Button>
          }
        />
      )}
      <UnconfirmedMessages
        key={`${id}.${identity.actorId}.${identity.tenantId || "visitor"}`}
        items={chat.unconfirmed}
        loading={chat.loading}
        pendingError={chat.pendingError}
        onRetry={chat.send}
      />
      <iframe
        key={`${source}.${revision}`}
        ref={frame}
        src={source}
        title="商户客服页面"
        sandbox="allow-scripts allow-same-origin"
        allow="camera 'none'; microphone 'none'"
        referrerPolicy="no-referrer"
        onLoad={() => {
          if (firstLoad.current) {
            closeDownload();
            channel.current?.close();
            channel.current = null;
            setReady(false);
          }
          firstLoad.current = true;
        }}
        style={{ width: "100%", height: "100%", flex: 1, minHeight: 300, border: 0 }}
      />
      <AttachmentSender
        key={`${id}.${identity.actorId}.${source}.${revision}`}
        disabled={!chat.conversation || chat.conversation.status === "closed"}
        onSend={uploadAttachment}
      />
      <Modal
        title="下载聊天附件"
        open={!!pendingDownload}
        okText="确认下载"
        cancelText="取消"
        confirmLoading={downloading}
        onOk={() => void confirmDownload()}
        onCancel={closeDownload}
      >
        <p style={{ overflowWrap: "anywhere" }}>{pendingDownload?.name}</p>
        <p className="muted">确认后会重新检查附件权限并发起下载，请在浏览器下载记录中查看结果。</p>
        {!!downloadError && <Alert type="error" showIcon title={errorMessage(downloadError)} />}
      </Modal>
    </div>
  );
}
