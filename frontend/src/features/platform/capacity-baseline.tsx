/** 文件作用说明：记录总管理员人工业务压测基线，显示保守在线余量并允许清除错误或演示记录。 */
"use client";
import { useState } from "react";
import { Alert, App, Button, Form, Input, InputNumber, Modal, Popconfirm, Space, Tag } from "antd";
import { api } from "@/lib/api";
import { ErrorNotice } from "@/components/page-state";
import styles from "@/features/console/operations-statistics.module.css";

export type CapacityState = {
  status: "uncalibrated" | "calibrated" | "stale" | "unavailable";
  reason: string;
  current_online_people: number;
  safe_online_people: number | null;
  estimated_additional_people: number | null;
  baseline: {
    tested_people: number;
    safety_factor: number;
    test_date: string;
    notes: string;
    updated_at: string;
    resource_spec: {
      scope: string;
      cpu_cores: number;
      memory_limit_bytes: number | null;
      visible_memory_bytes: number | null;
    };
  } | null;
};
type BaselineInput = {
  tested_people: number;
  safety_factor: number;
  test_date: string;
  notes: string;
};

/** todayText 生成浏览器当前日历日期，不把UTC前一日当作用户今天。 */
function todayText() {
  const now = new Date();
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-${String(now.getDate()).padStart(2, "0")}`;
}

/** CapacityBaseline 展示人工校准来源，服务端负责规格指纹和公式，不接受客户端资源上限。 */
export function CapacityBaseline({
  value,
  onChanged,
}: {
  value: CapacityState | undefined;
  onChanged: () => void;
}) {
  const { message } = App.useApp();
  const [form] = Form.useForm<BaselineInput>();
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const stored = value?.baseline;
  /** edit 只填入已有人工记录或空白测试信息，不虚构已经完成的压测人数。 */
  function edit() {
    setError(null);
    form.resetFields();
    form.setFieldsValue(
      stored
        ? {
            tested_people: stored.tested_people,
            safety_factor: stored.safety_factor,
            test_date: stored.test_date,
            notes: stored.notes,
          }
        : { safety_factor: 0.7, test_date: todayText() },
    );
    setOpen(true);
  }
  /** save 保存已完成测试的人工记录，当前部署规格由服务端同步采集。 */
  async function save(input: BaselineInput) {
    setBusy(true);
    setError(null);
    try {
      await api("/platform/monitoring/capacity", { method: "PATCH", bodyJSON: input });
      setOpen(false);
      message.success("业务压测基线已记录");
      onChanged();
    } catch (problem) {
      setError(problem);
    } finally {
      setBusy(false);
    }
  }
  /** clear 删除错误或演示基线，恢复未校准状态而不改动服务器或业务数据。 */
  async function clear() {
    setBusy(true);
    setError(null);
    try {
      await api("/platform/monitoring/capacity", { method: "DELETE" });
      message.success("压测基线已清除");
      onChanged();
    } catch (problem) {
      setError(problem);
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <div className={styles.resourceHeader}>
        <h2>预计还能同时在线</h2>
        <Tag
          color={
            value?.status === "calibrated"
              ? "green"
              : value?.status === "stale"
                ? "orange"
                : undefined
          }
        >
          {value?.status === "calibrated"
            ? "已记录基线"
            : value?.status === "stale"
              ? "规格已变化"
              : value?.status === "unavailable"
                ? "基线暂不可用"
                : "待压测校准"}
        </Tag>
      </div>
      <div className={styles.resourceReading}>
        <strong>{value?.estimated_additional_people ?? "—"}</strong>
        <span className="muted">人</span>
      </div>
      <p className="muted" style={{ fontSize: 12, lineHeight: 1.8 }}>
        {value?.reason || "正在读取压测记录"}
      </p>
      {stored && (
        <div style={{ fontSize: 12, lineHeight: 1.8, marginBottom: 12 }}>
          <div>
            实测 {stored.tested_people} 人 × {stored.safety_factor} 安全系数
            {value?.safe_online_people !== null && value?.safe_online_people !== undefined
              ? ` = ${value.safe_online_people} 人安全容量`
              : ""}
          </div>
          <div className="muted">
            测试日期 {stored.test_date} · 当前实时在线 {value?.current_online_people ?? "—"} 人
          </div>
          <p
            className="muted"
            style={{ whiteSpace: "pre-wrap", overflowWrap: "anywhere", margin: "6px 0" }}
          >
            {stored.notes}
          </p>
        </div>
      )}
      {!open && <ErrorNotice error={error} />}
      <Space wrap>
        <Button onClick={edit} disabled={busy}>
          记录压测基线
        </Button>
        {stored && (
          <Popconfirm
            title="清除当前压测基线？"
            description="并发估算会恢复为待校准，业务消息不受影响。"
            onConfirm={clear}
          >
            <Button danger loading={busy}>
              清除基线
            </Button>
          </Popconfirm>
        )}
      </Space>
      <Modal
        title="记录已完成的业务压测"
        open={open}
        onCancel={() => !busy && setOpen(false)}
        footer={null}
        destroyOnHidden
      >
        <Alert
          type="info"
          showIcon
          title="这是管理员录入的已完成测试记录，系统不会自动验证测试结果。请填写实际业务场景；运行规格变化后需要重新校准。"
        />
        <ErrorNotice error={error} />
        <Form form={form} layout="vertical" onFinish={save} style={{ marginTop: 18 }}>
          <Form.Item
            name="tested_people"
            label="实测同时在线人数"
            rules={[{ required: true, message: "请填写实际测试人数" }]}
          >
            <InputNumber min={1} max={10000000} precision={0} style={{ width: "100%" }} />
          </Form.Item>
          <Form.Item
            name="safety_factor"
            label="安全系数"
            extra="默认0.7；安全容量向下取整，不向未测试的更大并发外推。"
            rules={[{ required: true, message: "请填写安全系数" }]}
          >
            <InputNumber min={0.3} max={0.9} step={0.05} precision={2} style={{ width: "100%" }} />
          </Form.Item>
          <Form.Item
            name="test_date"
            label="测试日期"
            rules={[{ required: true, message: "请选择实际完成测试的日期" }]}
          >
            <Input type="date" />
          </Form.Item>
          <Form.Item
            name="notes"
            label="测试场景与结果说明"
            rules={[{ required: true, whitespace: true, message: "请填写实际压测场景及结果" }]}
          >
            <Input.TextArea
              rows={3}
              maxLength={2000}
              placeholder="记录客服/访客比例、消息频率、媒体情况和响应表现。"
            />
          </Form.Item>
          <Space>
            <Button type="primary" htmlType="submit" loading={busy}>
              保存压测基线
            </Button>
            <Button disabled={busy} onClick={() => setOpen(false)}>
              取消
            </Button>
          </Space>
        </Form>
      </Modal>
    </>
  );
}
