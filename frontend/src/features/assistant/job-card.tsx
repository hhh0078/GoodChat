/** 文件作用说明：展示真实 AI 阶段、图片状态、候选保存、取消及失败重试，避免把文本回复显示成生成文件。 */
"use client";
import { useState } from "react";
import { Alert, App, Button, Popconfirm, Space, Steps, Tag } from "antd";
import { api, errorMessage } from "@/lib/api";
import { activeAssistantJob, assistantJobLabel, type AssistantJob } from "./state";
import { downloadTemplate } from "./template-actions";

/** AssistantJobCard 依据服务端任务状态呈现操作，候选结果必须另存后才能下载为当前模板。 */
export function AssistantJobCard({
  job,
  tenantId,
  templateId,
  title,
  changing,
  candidateId,
  onCandidateSaved,
  onRetry,
  onCancel,
}: {
  job: AssistantJob;
  tenantId: string;
  templateId?: string;
  title?: string;
  changing: boolean;
  candidateId: string;
  onCandidateSaved: (id: string) => void;
  onRetry: () => Promise<void>;
  onCancel: () => Promise<void>;
}) {
  const [saving, setSaving] = useState(false);
  const [downloading, setDownloading] = useState(false);
  const { modal, message } = App.useApp();
  const active = activeAssistantJob(job);
  const candidate = job.applied === false && !!job.design;

  /** retry 对用量未知的失败任务先解释重复计费用量，再重试允许的阶段。 */
  function retry() {
    if (job.billing_status === "uncertain")
      modal.confirm({
        title: "重试此生成阶段？",
        content: "上次供应商用量尚未确认，重试可能产生重复模型用量。已完成阶段会保留。",
        okText: "继续重试",
        cancelText: "暂不重试",
        onOk: onRetry,
      });
    else void onRetry();
  }

  /** download 只下载后端完整 ZIP，不将对话文本或错误响应伪装成静态文件。 */
  async function download() {
    if (!templateId) return;
    setDownloading(true);
    try {
      await downloadTemplate(templateId, tenantId);
      message.success("模板 ZIP 下载已发起");
    } catch (problem) {
      modal.error({ title: "模板下载未完成", content: errorMessage(problem) });
    } finally {
      setDownloading(false);
    }
  }

  /** saveCandidate 将并发修改后保留的候选写为新模板，保持人工编辑与旧入口原状。 */
  async function saveCandidate() {
    if (!job.design) return;
    setSaving(true);
    try {
      const result = await api<{ id: string }>("/tenant/templates", {
        tenantId,
        method: "POST",
        idempotent: true,
        bodyJSON: {
          name: `${(title || "AI 模板").slice(0, 60)} 候选`,
          render_mode: "code",
          design: job.design,
        },
      });
      onCandidateSaved(result.id);
      window.dispatchEvent(new Event("goodchat:templates-changed"));
      message.success("候选结果已另存为私有模板");
    } catch (problem) {
      modal.error({ title: "候选模板未保存", content: errorMessage(problem) });
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="assistant-job" role="status" aria-live="polite">
      <div className="assistant-job-title">
        <strong>{assistantJobLabel(job)}</strong>
        <Tag
          color={
            job.status === "failed"
              ? "error"
              : job.status === "succeeded"
                ? "success"
                : "processing"
          }
        >
          {job.status === "succeeded"
            ? "已完成"
            : active
              ? "进行中"
              : job.status === "cancelled"
                ? "已取消"
                : "失败"}
        </Tag>
      </div>
      {active && (
        <Steps
          size="small"
          current={
            job.phase === "package" || job.status === "validating"
              ? 2
              : job.phase === "images"
                ? 1
                : 0
          }
          items={[{ title: "页面" }, { title: "素材" }, { title: "校验打包" }]}
        />
      )}
      {!!job.image_tasks?.length && (
        <div className="assistant-image-progress">
          {job.image_tasks.map((task, index) => (
            <Tag
              key={task.id}
              color={
                task.status === "failed"
                  ? "error"
                  : task.status === "succeeded"
                    ? "success"
                    : "default"
              }
            >
              图片 {index + 1} ·{" "}
              {task.status === "succeeded"
                ? "已生成"
                : task.status === "failed"
                  ? "失败"
                  : task.status === "running"
                    ? "生成中"
                    : task.status === "cancelled"
                      ? "已取消"
                      : "待处理"}
            </Tag>
          ))}
        </div>
      )}
      {job.error_message && <p className="assistant-job-error">{job.error_message}</p>}
      {job.billing_status === "uncertain" && (
        <p className="assistant-job-error">供应商用量待核对。重试可能产生重复用量。</p>
      )}
      {candidate && !candidateId && (
        <Alert
          type="warning"
          showIcon
          title="草稿已被修改，AI 候选没有覆盖人工编辑"
          description="右侧保留候选预览，另存为新模板后可继续编辑与下载。"
          style={{ margin: "12px 0" }}
        />
      )}
      <Space wrap>
        {active && (
          <Popconfirm
            title="取消当前生成？"
            description="已保存的对话和完成阶段会保留。"
            okText="取消生成"
            cancelText="继续等待"
            onConfirm={onCancel}
          >
            <Button loading={changing} size="small">
              取消生成
            </Button>
          </Popconfirm>
        )}
        {job.status === "failed" && (
          <Button size="small" onClick={retry} loading={changing}>
            重试失败阶段
          </Button>
        )}
        {candidate && !candidateId && (
          <Button size="small" type="primary" loading={saving} onClick={() => void saveCandidate()}>
            另存候选模板
          </Button>
        )}
        {job.status === "succeeded" && templateId && (!candidate || !!candidateId) && (
          <Button size="small" loading={downloading} onClick={() => void download()}>
            下载静态模板
          </Button>
        )}
      </Space>
    </div>
  );
}
