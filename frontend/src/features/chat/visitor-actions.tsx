/** 文件作用说明：提交访客离线留言与已结束会话评价，复用独立访客授权且保持请求幂等。 */
"use client";
import { useEffect, useRef, useState } from "react";
import {
  Alert,
  App,
  Button,
  Drawer,
  Form,
  Input,
  Rate,
  Select,
  Space,
  Spin,
  Tag,
  Upload,
} from "antd";
import { api } from "@/lib/api";
import { ErrorNotice } from "@/components/page-state";
type EvaluationState = {
  enabled: boolean;
  can_submit: boolean;
  can_edit: boolean;
  submission_deadline?: string;
  edit_deadline?: string;
  timezone: string;
  allowed_tags: string[];
  evaluation?: { score: number; comment: string; tags: string[]; version: number } | null;
};
type LeadAttachment = { id: string; file_name: string; size_bytes: number; content_type: string };
/** VisitorActions 让访客主动留言和评价，成功反馈只来自真实后端。 */
export function VisitorActions({
  entryKey,
  conversationId,
  token,
  status,
}: {
  entryKey: string;
  conversationId: string;
  token: string;
  status: string;
}) {
  const [mode, setMode] = useState<"lead" | "evaluation" | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const [attachments, setAttachments] = useState<LeadAttachment[]>([]);
  const [uploading, setUploading] = useState(false);
  const uploadController = useRef<AbortController | null>(null);
  const [evaluation, setEvaluation] = useState<EvaluationState | null>(null);
  const [evaluationLoading, setEvaluationLoading] = useState(false);
  const [evaluationRevision, setEvaluationRevision] = useState(0);
  const requestID = useRef("");
  const [form] = Form.useForm();
  const { message } = App.useApp();
  const canEvaluate = !!evaluation && (evaluation.can_submit || evaluation.can_edit);
  useEffect(() => {
    requestID.current = "";
    uploadController.current?.abort();
    setUploading(false);
    setAttachments([]);
    setMode(null);
    setError(null);
    form.resetFields();
  }, [conversationId, token, form]);
  useEffect(() => () => uploadController.current?.abort(), []);
  /** uploadLeadFile 仅上传私有文件，不自动向聊天发消息；失败保留已经成功上传的附件。 */
  async function uploadLeadFile(file: File) {
    if (uploading || busy) return;
    setError(null);
    if (!file.size || file.size > 20 * 1024 * 1024 || attachments.length >= 3) {
      setError(new Error("留言最多3个附件，每个不超过20MB，图片最大10MB"));
      return;
    }
    const controller = new AbortController();
    uploadController.current = controller;
    setUploading(true);
    try {
      const body = new FormData();
      body.append("file", file);
      const attachment = await api<LeadAttachment>("/attachments", {
        method: "POST",
        headers: { Authorization: `Bearer ${token}` },
        body,
        signal: controller.signal,
      });
      if (!controller.signal.aborted)
        setAttachments((previous) => [
          ...previous,
          {
            id: attachment.id,
            file_name: attachment.file_name,
            size_bytes: attachment.size_bytes,
            content_type: attachment.content_type,
          },
        ]);
    } catch (problem) {
      if (!controller.signal.aborted) setError(problem);
    } finally {
      if (!controller.signal.aborted) setUploading(false);
    }
  }
  useEffect(() => {
    setEvaluation(null);
    if (status !== "closed") return;
    const controller = new AbortController();
    setEvaluationLoading(true);
    api<EvaluationState>(`/conversations/${conversationId}/evaluation`, {
      headers: { Authorization: `Bearer ${token}` },
      signal: controller.signal,
    })
      .then((data) => {
        if (controller.signal.aborted) return;
        setEvaluation(data);
        if (mode === "evaluation") {
          form.resetFields();
          if (data.evaluation) form.setFieldsValue(data.evaluation);
        }
      })
      .catch((problem) => {
        if (!controller.signal.aborted) setError(problem);
      })
      .finally(() => {
        if (!controller.signal.aborted) setEvaluationLoading(false);
      });
    return () => controller.abort();
  }, [conversationId, token, status, mode, evaluationRevision, form]);
  /** submit 向服务端提交留言或评价，留言失败重试保留同一请求编号。 */
  async function submit(values: Record<string, unknown>) {
    if (uploading) return;
    setBusy(true);
    setError(null);
    try {
      if (mode === "lead") {
        if (!requestID.current) requestID.current = crypto.randomUUID();
        await api("/visitor/leads", {
          method: "POST",
          headers: { Authorization: `Bearer ${token}` },
          bodyJSON: {
            ...values,
            entry_key: entryKey,
            conversation_id: conversationId,
            client_request_id: requestID.current,
            attachment_ids: attachments.map((item) => item.id),
          },
        });
        requestID.current = "";
        setAttachments([]);
        message.success("留言已提交");
      } else {
        if (!canEvaluate || evaluationLoading)
          throw new Error("当前不可提交或修改评价，请重新读取状态");
        await api(`/conversations/${conversationId}/evaluation`, {
          method: "POST",
          headers: { Authorization: `Bearer ${token}` },
          bodyJSON: {
            ...values,
            tags: values.tags || [],
            version: evaluation?.evaluation?.version || 0,
          },
        });
        setEvaluationRevision((value) => value + 1);
        message.success("感谢你的真实反馈");
      }
      setMode(null);
      form.resetFields();
    } catch (problem) {
      setError(problem);
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <Space
        style={{
          justifyContent: "center",
          width: "100%",
          padding: "8px 16px",
          borderTop: "1px solid #e4e9df",
        }}
      >
        {status !== "closed" && (
          <Button
            type="link"
            size="small"
            onClick={() => {
              setError(null);
              if (!requestID.current) {
                form.resetFields();
                setAttachments([]);
              }
              setMode("lead");
            }}
          >
            不方便等待？留下留言
          </Button>
        )}
        {status === "closed" && (
          <Button
            type="link"
            onClick={() => {
              setError(null);
              form.resetFields();
              setMode("evaluation");
            }}
          >
            {evaluation?.evaluation ? "查看或修改评价" : "评价本次服务"}
          </Button>
        )}
      </Space>
      <Drawer
        title={mode === "lead" ? "留言给接待团队" : "评价本次服务"}
        open={!!mode}
        onClose={() => {
          uploadController.current?.abort();
          setUploading(false);
          setMode(null);
        }}
        width={420}
        destroyOnHidden
      >
        <ErrorNotice
          error={error}
          retry={
            mode === "evaluation" ? () => setEvaluationRevision((value) => value + 1) : undefined
          }
        />
        {mode === "evaluation" && (
          <>
            {evaluationLoading && <Spin />}
            {evaluation && (
              <Alert
                type={canEvaluate ? "info" : "warning"}
                showIcon
                style={{ marginBottom: 16 }}
                title={
                  evaluation.evaluation ? (
                    <>
                      已保存评价 <Tag>v{evaluation.evaluation.version}</Tag>
                      {evaluation.can_edit ? "当前可修改" : "当前不可修改"}
                    </>
                  ) : !evaluation.enabled ? (
                    "商户暂未开放评价"
                  ) : evaluation.can_submit ? (
                    "请主动选择评分，没有预设满意分"
                  ) : (
                    "本次咨询的评价期限已结束"
                  )
                }
                description={
                  <>
                    {evaluation.submission_deadline && (
                      <div>
                        初评截止：
                        {new Date(evaluation.submission_deadline).toLocaleString("zh-CN", {
                          timeZone: evaluation.timezone,
                        })}
                      </div>
                    )}
                    {evaluation.evaluation && evaluation.edit_deadline && (
                      <div>
                        改评截止：
                        {new Date(evaluation.edit_deadline).toLocaleString("zh-CN", {
                          timeZone: evaluation.timezone,
                        })}
                      </div>
                    )}
                    时区 {evaluation.timezone}。每次修改都会保留原始版本。
                  </>
                }
              />
            )}
          </>
        )}
        <Form
          form={form}
          layout="vertical"
          onFinish={submit}
          disabled={mode === "evaluation" && (!canEvaluate || evaluationLoading)}
        >
          {mode === "lead" ? (
            <>
              <Form.Item
                name="display_name"
                label="你的称呼"
                rules={[{ required: true, whitespace: true }]}
              >
                <Input maxLength={80} />
              </Form.Item>
              <Form.Item
                name="phone"
                label="联系电话"
                rules={[{ required: true, whitespace: true }]}
              >
                <Input maxLength={32} />
              </Form.Item>
              <Form.Item
                name="content"
                label="需要什么帮助"
                rules={[{ required: true, whitespace: true }]}
              >
                <Input.TextArea rows={5} maxLength={5000} />
              </Form.Item>
              <Form.Item label="附件（选填）" extra="最多3个，每个20MB以内，图片最大10MB。">
                <Space direction="vertical" style={{ width: "100%" }}>
                  {attachments.map((item) => (
                    <Space key={item.id}>
                      <span style={{ overflowWrap: "anywhere" }}>{item.file_name}</span>
                      <Button
                        type="link"
                        size="small"
                        disabled={busy || uploading}
                        onClick={() =>
                          setAttachments((previous) =>
                            previous.filter((file) => file.id !== item.id),
                          )
                        }
                      >
                        移除
                      </Button>
                    </Space>
                  ))}
                  <Upload
                    showUploadList={false}
                    beforeUpload={(file) => {
                      void uploadLeadFile(file);
                      return false;
                    }}
                  >
                    <Button
                      loading={uploading}
                      disabled={busy || uploading || attachments.length >= 3}
                    >
                      添加留言附件
                    </Button>
                  </Upload>
                </Space>
              </Form.Item>
              <p className="form-hint">
                主动提交的留言附件会独立共享给处理团队；之后撤回聊天消息不会撤销这次共享。
              </p>
            </>
          ) : (
            <>
              <Form.Item
                name="score"
                label="服务评分"
                rules={[
                  {
                    required: true,
                    type: "number",
                    min: 1,
                    max: 5,
                    message: "请主动选择一至五星评分",
                  },
                ]}
              >
                <Rate />
              </Form.Item>
              <Form.Item name="tags" label="评价标签（选填）">
                <Select
                  mode="multiple"
                  options={(evaluation?.allowed_tags || []).map((value) => ({
                    label: value,
                    value,
                  }))}
                />
              </Form.Item>
              <Form.Item name="comment" label="想对我们说的话（选填）">
                <Input.TextArea rows={4} maxLength={2000} />
              </Form.Item>
            </>
          )}
          <Button type="primary" htmlType="submit" loading={busy} disabled={uploading}>
            提交{mode === "lead" ? "留言" : "评价"}
          </Button>
        </Form>
      </Drawer>
    </>
  );
}
