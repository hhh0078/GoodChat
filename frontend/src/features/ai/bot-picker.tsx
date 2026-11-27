/** 文件作用说明：在机器人卡片标题栏展示本人配置切换，并通过独立名称弹窗新增关闭状态的机器人。 */
"use client";
import { ErrorNotice } from "@/components/page-state";
import { useSession } from "@/features/auth/session";
import { useTranslator } from "@/i18n/use-translator";
import { api } from "@/lib/api";
import { PlusOutlined } from "@ant-design/icons";
import { App, Button, Form, Input, Modal } from "antd";
import { useEffect, useRef, useState } from "react";
import type { AIBot } from "./types";

/** AIBotPicker 让本人机器人名称直接可见，并以按下状态标明正在编辑的配置。 */
export function AIBotPicker({
  bots,
  selectedId,
  disabled,
  dirty,
  onSelect,
  onCreate,
}: {
  bots: AIBot[];
  selectedId?: string;
  disabled: boolean;
  dirty: boolean;
  onSelect: (id: string) => void;
  onCreate: () => void;
}) {
  const tr = useTranslator();
  const { modal } = App.useApp();
  /** continueEditing 在切换或新增前确认未保存编辑，取消时保留旧输入与当前机器人。 */
  async function continueEditing(action: () => void) {
    if (
      dirty &&
      !(await modal.confirm({
        title: tr("放弃未保存的修改？"),
        content: tr("当前机器人的修改尚未保存，继续后将放弃这些修改。"),
        okText: tr("放弃并继续"),
        cancelText: tr("继续编辑"),
      }))
    )
      return;
    action();
  }
  return (
    <div className="ai-bot-picker">
      <div className="ai-bot-picker-main">
        <span className="ai-bot-picker-heading">
          {tr("我的机器人")}
          <span
            className="ai-bot-picker-count"
            aria-label={tr("共 {p0} 个机器人", { p0: bots.length })}
          >
            {bots.length}
          </span>
        </span>
        <div className="ai-bot-picker-options" role="group" aria-label={tr("选择机器人")}>
          {bots.map((bot) => (
            <Button
              key={bot.id}
              type={bot.id === selectedId ? "primary" : "default"}
              aria-pressed={bot.id === selectedId}
              disabled={disabled}
              onClick={() => {
                if (bot.id && bot.id !== selectedId) void continueEditing(() => onSelect(bot.id!));
              }}
            >
              {bot.name}
            </Button>
          ))}
        </div>
      </div>
      <Button
        icon={<PlusOutlined />}
        disabled={disabled}
        onClick={() => void continueEditing(onCreate)}
      >
        {tr("新增机器人")}
      </Button>
    </div>
  );
}

/** AIBotCreateModal 仅提交新名称，失败保留输入并允许重试，离开租户时取消未完成请求。 */
export function AIBotCreateModal({
  open,
  onClose,
  onCreated,
}: {
  open: boolean;
  onClose: () => void;
  onCreated: (bot: AIBot) => void;
}) {
  const tr = useTranslator();
  const { tenantId } = useSession();
  const [form] = Form.useForm();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const request = useRef<AbortController | null>(null);
  useEffect(() => () => request.current?.abort(), [tenantId]);
  /** create 以当前认证成员新增机器人，成功后交由设置页选中完整返回配置。 */
  async function create(values: { name: string }) {
    if (busy) return;
    const controller = new AbortController();
    request.current = controller;
    setBusy(true);
    setError(null);
    try {
      const bot = await api<AIBot>("/me/ai/bots", {
        method: "POST",
        tenantId,
        signal: controller.signal,
        bodyJSON: { name: values.name.trim() },
      });
      if (!controller.signal.aborted) {
        onCreated(bot);
        onClose();
      }
    } catch (problem) {
      if (!controller.signal.aborted) setError(problem);
    } finally {
      if (!controller.signal.aborted) setBusy(false);
    }
  }
  return (
    <Modal
      title={tr("新增机器人")}
      open={open}
      onCancel={onClose}
      onOk={() => form.submit()}
      okText={tr("新增机器人")}
      confirmLoading={busy}
      cancelButtonProps={{ disabled: busy }}
      closable={!busy}
      mask={{ closable: !busy }}
      keyboard={!busy}
      forceRender
      afterClose={() => {
        form.resetFields();
        setError(null);
      }}
    >
      <ErrorNotice error={error} />
      <Form
        name="create-ai-bot"
        form={form}
        layout="vertical"
        disabled={busy}
        onFinish={(values) => void create(values)}
      >
        <Form.Item
          name="name"
          label={tr("机器人名称")}
          rules={[{ required: true, whitespace: true }]}
        >
          <Input maxLength={60} autoFocus />
        </Form.Item>
      </Form>
    </Modal>
  );
}
