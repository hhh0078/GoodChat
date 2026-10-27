/** 文件作用说明：管理当前管理员的真实异步CSV任务，展示实际处理行数、取消、有效期和重新授权下载。 */
"use client";
import { useEffect, useRef, useState } from "react";
import { App, Button, Checkbox, Drawer, Select, Space, Table, Tag } from "antd";
import { api } from "@/lib/api";
import { requestCSV, startFileDownload } from "@/lib/csv-download";
import { useResource } from "@/lib/use-resource";
import { useSession } from "@/features/auth/session";
import { ErrorNotice, EmptyData } from "@/components/page-state";

type ExportJob = {
  id: string;
  status: "queued" | "running" | "completed" | "failed" | "cancelled";
  filters: string;
  row_count: number;
  size_bytes: number;
  error_message: string;
  created_at: string;
  expires_at: string;
};
const labels = {
  queued: "排队中",
  running: "正在生成",
  completed: "已生成",
  failed: "失败",
  cancelled: "已取消",
};

/** ExportJobs 沿用记录页筛选创建持久任务，下载结果只属于当前申请者。 */
export function ExportJobs({ filters }: { filters: string }) {
  const { tenantId } = useSession();
  const jobs = useResource<ExportJob>("/tenant/export-jobs");
  const [open, setOpen] = useState(false);
  const [kind, setKind] = useState("metadata");
  const [internal, setInternal] = useState(false);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState<unknown>(null);
  const attempt = useRef<{ filters: string; id: string } | null>(null);
  const action = useRef<AbortController | null>(null);
  const { message } = App.useApp();
  const pending = jobs.items.some((job) => ["queued", "running"].includes(job.status));
  useEffect(() => {
    if (!open || !pending) return;
    const timer = setInterval(jobs.reload, 3000);
    return () => clearInterval(timer);
  }, [open, pending, jobs.reload]);
  useEffect(() => {
    attempt.current = null;
    setError(null);
    setBusy("");
    return () => action.current?.abort();
  }, [tenantId]);
  /** submit 保留失败重试的幂等编号，成功反馈表示任务保存而非文件已经生成。 */
  async function submit() {
    const params = new URLSearchParams(filters);
    params.set("include_messages", String(kind === "messages"));
    params.set("include_internal", String(kind === "messages" && internal));
    const query = params.toString();
    if (!attempt.current || attempt.current.filters !== query)
      attempt.current = { filters: query, id: crypto.randomUUID() };
    const controller = new AbortController();
    action.current = controller;
    setBusy("create");
    setError(null);
    try {
      await api("/tenant/export-jobs", {
        method: "POST",
        tenantId,
        signal: controller.signal,
        bodyJSON: { filters: query, client_request_id: attempt.current.id },
      });
      if (controller.signal.aborted) return;
      attempt.current = null;
      jobs.reload();
      message.success("导出任务已保存，请查看实际生成状态");
    } catch (problem) {
      if (!controller.signal.aborted) setError(problem);
    } finally {
      if (action.current === controller) {
        setBusy("");
        action.current = null;
      }
    }
  }
  /** perform 取消等待或运行任务，或请求带当前身份校验的真实CSV下载。 */
  async function perform(job: ExportJob, download: boolean) {
    const controller = new AbortController();
    action.current = controller;
    setBusy(job.id);
    setError(null);
    try {
      if (download) {
        const file = await requestCSV(
          `/tenant/export-jobs/${job.id}/download`,
          tenantId,
          "GoodChat-批量导出.csv",
          controller.signal,
        );
        if (controller.signal.aborted) return;
        startFileDownload(file.blob, file.filename);
        message.success("下载已发起，请查看浏览器下载记录");
      } else {
        await api(`/tenant/export-jobs/${job.id}/cancel`, {
          method: "POST",
          tenantId,
          signal: controller.signal,
          bodyJSON: {},
        });
        if (!controller.signal.aborted) message.success("任务已取消");
      }
      jobs.reload();
    } catch (problem) {
      if (!controller.signal.aborted) setError(problem);
    } finally {
      if (action.current === controller) {
        setBusy("");
        action.current = null;
      }
    }
  }
  return (
    <>
      <Button
        onClick={() => {
          setOpen(true);
          jobs.reload();
        }}
      >
        批量导出任务
      </Button>
      <Drawer
        rootClassName="console-drawer console-export-drawer"
        title="批量导出任务"
        open={open}
        onClose={() => setOpen(false)}
        width={850}
      >
        <p>
          沿用聊天记录页当前筛选。每任务最多100000行、64MiB；每租户每日最多20次提交，失败或取消不退次数，临时文件与待执行预留合计最多256MiB。结果24小时有效，仅申请者在管理权限和导出功能仍有效时可下载。
        </p>
        <Space wrap>
          <Select
            aria-label="批量导出内容"
            value={kind}
            onChange={setKind}
            options={[
              { value: "metadata", label: "会话列表" },
              { value: "messages", label: "消息正文" },
            ]}
          />
          <Checkbox
            checked={internal}
            disabled={kind !== "messages"}
            onChange={(event) => setInternal(event.target.checked)}
          >
            明确包含内部备注
          </Checkbox>
          <Button
            type="primary"
            loading={busy === "create"}
            disabled={!!busy}
            onClick={() => void submit()}
          >
            创建导出任务
          </Button>
          <Button onClick={jobs.reload}>刷新任务</Button>
        </Space>
        <ErrorNotice error={error} />
        <ErrorNotice error={jobs.error} retry={jobs.reload} />
        <Table
          rowKey="id"
          dataSource={jobs.items}
          loading={jobs.loading}
          scroll={{ x: 720 }}
          locale={{ emptyText: <EmptyData text="还没有提交导出任务" /> }}
          columns={[
            {
              title: "内容",
              render: (_, job) =>
                new URLSearchParams(job.filters).get("include_messages") === "true"
                  ? "消息正文"
                  : "会话列表",
            },
            {
              title: "状态",
              render: (_, job) => (
                <>
                  <Tag
                    color={
                      job.status === "completed"
                        ? "green"
                        : job.status === "failed"
                          ? "red"
                          : "default"
                    }
                  >
                    {labels[job.status]}
                  </Tag>
                  {job.error_message && <p>{job.error_message}</p>}
                </>
              ),
            },
            {
              title: "实际处理",
              render: (_, job) =>
                `${job.row_count}行${job.size_bytes ? ` · ${(job.size_bytes / 1024).toFixed(1)}KiB` : ""}`,
            },
            {
              title: "有效期至",
              render: (_, job) => new Date(job.expires_at).toLocaleString("zh-CN"),
            },
            {
              title: "操作",
              render: (_, job) =>
                ["queued", "running"].includes(job.status) ? (
                  <Button disabled={!!busy} onClick={() => void perform(job, false)}>
                    取消
                  </Button>
                ) : job.status === "completed" ? (
                  <Button
                    loading={busy === job.id}
                    disabled={!!busy || Date.parse(job.expires_at) <= Date.now()}
                    onClick={() => void perform(job, true)}
                  >
                    {Date.parse(job.expires_at) <= Date.now() ? "已过期" : "下载CSV"}
                  </Button>
                ) : null,
            },
          ]}
        />
        <p className="muted">
          处理行数来自后台实际读取，不代表预估总量。失败、过期或取消不会提供截断文件；如超限，请缩小日期范围重新创建。
        </p>
      </Drawer>
    </>
  );
}
