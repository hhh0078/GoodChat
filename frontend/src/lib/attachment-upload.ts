/** 文件作用说明：复用聊天附件大小校验、上传结果缓存和固定消息编号，支持失败后幂等重试。 */
export type UploadedAttachment = { id: string; content_type: string };
export type AttachmentAttempt = { clientId: string; attachment?: UploadedAttachment };
type AttachmentMessage = {
  client_message_id: string;
  content: string;
  message_type: "image" | "audio" | "file";
  attachment_id: string;
  audience: "public";
};

/** uploadAndSendAttachment 上传并发送一次公开附件消息，重试复用已上传附件与原消息编号。 */
export async function uploadAndSendAttachment<T>(
  file: unknown,
  name: unknown,
  attempt: AttachmentAttempt,
  upload: (form: FormData) => Promise<UploadedAttachment>,
  send: (body: AttachmentMessage) => Promise<T>,
): Promise<T> {
  if (
    !(file instanceof Blob) ||
    typeof name !== "string" ||
    !name.trim() ||
    name.length > 240 ||
    file.size === 0 ||
    file.size > 20 * 1024 * 1024
  )
    throw new Error("请选择不超过20MB的有效文件");
  const form = new FormData();
  form.append("file", file, name);
  const attachment = attempt.attachment || (await upload(form));
  attempt.attachment = attachment;
  return send({
    client_message_id: attempt.clientId,
    content: "",
    message_type: attachment.content_type.startsWith("image/")
      ? "image"
      : attachment.content_type.startsWith("audio/") || attachment.content_type === "video/webm"
        ? "audio"
        : "file",
    attachment_id: attachment.id,
    audience: "public",
  });
}
