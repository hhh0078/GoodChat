/** 文件作用说明：验证父层与装修SDK共用附件校验、上传缓存和消息幂等重试。 */
import assert from "node:assert/strict";
import test from "node:test";
import { uploadAndSendAttachment, type AttachmentAttempt } from "../src/lib/attachment-upload.ts";
test("非法空附件与超20MB附件在网络请求之前拒绝", async () => {
  let uploads = 0;
  const upload = async () => {
    uploads++;
    return { id: "file-a", content_type: "text/plain" };
  };
  for (const [file, name] of [
    [null, "附件"],
    [new Blob([]), "附件"],
    [new Blob(["x".repeat(20 * 1024 * 1024 + 1)]), "附件"],
    [new Blob(["x"]), " "],
  ])
    await assert.rejects(
      uploadAndSendAttachment(file, name, { clientId: "message-id" }, upload, async () => ({})),
      /20MB/,
    );
  assert.equal(uploads, 0);
});
test("发送失败重试复用附件与原消息编号，避免重复上传和重复消息", async () => {
  const attempt: AttachmentAttempt = { clientId: "stable-message-id" };
  let uploads = 0;
  const sent: {
    client_message_id: string;
    attachment_id: string;
    message_type: string;
    audience: string;
  }[] = [];
  const upload = async (form: FormData) => {
    uploads++;
    assert.equal((form.get("file") as File).name, "附件.txt");
    return { id: "file-a", content_type: "text/plain" };
  };
  const send = async (body: (typeof sent)[number]) => {
    sent.push(body);
    if (sent.length === 1) throw new Error("响应丢失");
    return { id: "message-a" };
  };
  const file = new Blob(["actual file"]);
  await assert.rejects(
    uploadAndSendAttachment(file, "附件.txt", attempt, upload, send),
    /响应丢失/,
  );
  assert.deepEqual(await uploadAndSendAttachment(file, "附件.txt", attempt, upload, send), {
    id: "message-a",
  });
  assert.equal(uploads, 1);
  assert.equal(sent.length, 2);
  assert.deepEqual(sent[0], sent[1]);
  assert.equal(sent[1].client_message_id, "stable-message-id");
  assert.equal(sent[1].attachment_id, "file-a");
  assert.equal(sent[1].audience, "public");
});
