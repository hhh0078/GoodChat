/** 文件作用说明：为有权限的管理员维护租户或平台模型配置，以及平台提示词和官方脚本版本。 */
"use client";
import { ErrorNotice } from "@/components/page-state";
import { useTranslator } from "@/i18n/use-translator";
import { api } from "@/lib/api";
import {
  Alert,
  App,
  Button,
  Form,
  Input,
  Modal,
  Select,
  Space,
  Spin,
  Switch,
  Tabs,
  Tag,
} from "antd";
import { useEffect, useState } from "react";
import { FoundationTest } from "./foundation-test";
import type { AssistantConfiguration, AssistantSource } from "./state";
type Foundation = {
  version: number;
  prompt: string;
  scripts: {
    version: string;
    sdk: string;
    chat_ui: string;
  };
};
type ConfigurationInput = {
  enabled: boolean;
  base_url: string;
  model: string;
  api_key?: string;
  image_base_url: string;
  image_model: string;
  image_api_key?: string;
};
/** ModelSettings 只显示密钥是否存在，留空保存时不清除服务端已有密钥。 */
function ModelSettings({ source, tenantId }: { source: AssistantSource; tenantId: string }) {
  const tr = useTranslator();
  const [config, setConfig] = useState<AssistantConfiguration | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [testing, setTesting] = useState<string>("");
  const [revision, setRevision] = useState(0);
  const [form] = Form.useForm<ConfigurationInput>();
  const { message } = App.useApp();
  const path = `/${source}/assistant/configuration`;
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError(null);
    setConfig(null);
    form.resetFields();
    api<AssistantConfiguration>(path, { tenantId, signal: controller.signal })
      .then((value) => {
        if (controller.signal.aborted) return;
        setConfig(value);
        form.setFieldsValue({ ...value, api_key: "", image_api_key: "" });
      })
      .catch((problem) => {
        if (!controller.signal.aborted) setError(problem);
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [path, tenantId, revision, form]);
  /** save 将模型与密钥发送到当前管理范围，成功后清空输入框并读取脱敏配置。 */
  async function save(values: ConfigurationInput) {
    setBusy(true);
    setError(null);
    try {
      await api(path, {
        tenantId,
        method: "PUT",
        bodyJSON: { ...values, version: config?.version || 0 },
      });
      form.setFieldsValue({ api_key: "", image_api_key: "" });
      setRevision((value) => value + 1);
      window.dispatchEvent(new Event("goodchat:assistant-configuration-changed"));
      message.success(tr("AI 配置已保存"));
    } catch (problem) {
      setError(problem);
    } finally {
      setBusy(false);
    }
  }
  /** test 对已保存的模型发起实际请求，只展示服务端脱敏后的结果。 */
  async function test(kind: "text" | "image") {
    setTesting(kind);
    setError(null);
    try {
      const result = await api<{
        success: boolean;
        message: string;
      }>(`${path}/test`, {
        tenantId,
        method: "POST",
        bodyJSON: { kind },
      });
      if (!result.success) throw new Error(result.message || tr("模型连接测试未通过"));
      message.success(result.message || tr("模型连接测试通过"));
    } catch (problem) {
      setError(problem);
    } finally {
      setTesting("");
    }
  }
  return (
    <div className="assistant-settings-section">
      <Alert
        type="info"
        showIcon
        title={
          source === "tenant"
            ? tr("当前租户使用自己的 AI 配置生成模板")
            : tr("平台 AI 仅在选择平台配置时调用")
        }
        description={tr("密钥只保存在服务端私密配置，页面与模板不会获得密钥。测试会实际调用模型。")}
      />
      <ErrorNotice error={error} retry={() => setRevision((value) => value + 1)} />
      {loading ? (
        <Spin description={tr("正在读取配置")} />
      ) : (
        <Form form={form} layout="vertical" onFinish={save} disabled={busy || !!testing}>
          <Form.Item name="enabled" label={tr("允许使用此配置")} valuePropName="checked">
            <Switch />
          </Form.Item>
          <div className="assistant-settings-grid">
            <Form.Item
              name="base_url"
              label={tr("文本模型接口地址")}
              rules={[
                { required: true },
                { type: "url", message: tr("请输入有效的 HTTP 或 HTTPS 地址") },
              ]}
            >
              <Input placeholder="https://…/compatible-mode/v1" autoComplete="off" />
            </Form.Item>
            <Form.Item
              name="model"
              label={tr("文本模型 ID")}
              rules={[{ required: true, whitespace: true }]}
            >
              <Input placeholder={tr("模型 ID")} />
            </Form.Item>
          </div>
          <Form.Item
            name="api_key"
            label={
              <Space>
                {tr("文本模型密钥")}
                <Tag>{config?.has_api_key ? tr("已配置") : tr("未配置")}</Tag>
              </Space>
            }
          >
            <Input.Password
              autoComplete="new-password"
              placeholder={config?.has_api_key ? tr("留空保留现有密钥") : tr("输入 API Key")}
            />
          </Form.Item>
          <div className="assistant-settings-grid">
            <Form.Item
              name="image_base_url"
              label={tr("图片模型接口地址")}
              rules={[{ type: "url", message: tr("请输入有效地址") }]}
            >
              <Input placeholder={tr("图片生成接口地址")} />
            </Form.Item>
            <Form.Item name="image_model" label={tr("图片模型 ID")}>
              <Input placeholder={tr("图片生成模型 ID")} />
            </Form.Item>
          </div>
          <Form.Item
            name="image_api_key"
            label={
              <Space>
                {tr("图片模型密钥 ")}
                <Tag>
                  {config?.has_image_api_key
                    ? tr("已配置")
                    : config?.has_api_key
                      ? tr("沿用文本密钥")
                      : tr("未配置")}
                </Tag>
              </Space>
            }
          >
            <Input.Password
              autoComplete="new-password"
              placeholder={
                config?.has_image_api_key
                  ? tr("留空保留现有密钥")
                  : config?.has_api_key
                    ? tr("留空沿用文本模型密钥")
                    : tr("输入图片模型 API Key")
              }
            />
          </Form.Item>
          <Space wrap>
            <Button type="primary" htmlType="submit" loading={busy}>
              {tr("保存配置 ")}
            </Button>
            <Button
              onClick={() => void test("text")}
              loading={testing === "text"}
              disabled={!config?.has_api_key}
            >
              {tr("测试已保存文本配置 ")}
            </Button>
            <Button
              onClick={() => void test("image")}
              loading={testing === "image"}
              disabled={
                !config?.image_model || (!config?.has_image_api_key && !config?.has_api_key)
              }
            >
              {tr("测试已保存图片配置 ")}
            </Button>
          </Space>
        </Form>
      )}
    </div>
  );
}
/** FoundationSettings 保存新提示词和官方脚本版本，乐观版本冲突时保留当前编辑。 */
function FoundationSettings() {
  const tr = useTranslator();
  const [current, setCurrent] = useState<Foundation | null>(null);
  const [versions, setVersions] = useState<Foundation[]>([]);
  const [loadedVersion, setLoadedVersion] = useState<number | undefined>();
  const [verified, setVerified] = useState<{
    sdk: string;
    chat_ui: string;
  } | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(true);
  const [revision, setRevision] = useState(0);
  const [form] = Form.useForm();
  const sdk = Form.useWatch("sdk", form) || "";
  const chatUI = Form.useWatch("chat_ui", form) || "";
  const canSave = verified?.sdk === sdk && verified?.chat_ui === chatUI;
  const { message } = App.useApp();
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError(null);
    api<Foundation>("/platform/assistant/foundation", { signal: controller.signal })
      .then((value) => {
        if (controller.signal.aborted) return;
        setCurrent(value);
        setLoadedVersion(value.version);
        setVerified(null);
        form.resetFields();
        form.setFieldsValue({
          prompt: value.prompt,
          script_version: value.scripts.version,
          sdk: value.scripts.sdk,
          chat_ui: value.scripts.chat_ui,
        });
      })
      .catch((problem) => {
        if (!controller.signal.aborted) setError(problem);
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [revision, form]);
  useEffect(() => {
    const controller = new AbortController();
    api<{
      items: Foundation[];
    }>("/platform/assistant/foundation/versions", {
      signal: controller.signal,
    })
      .then((value) => {
        if (!controller.signal.aborted) setVersions(value.items || []);
      })
      .catch((problem) => {
        if (!controller.signal.aborted) setError(problem);
      });
    return () => controller.abort();
  }, [revision]);
  /** loadVersion 将旧内容载入编辑区，保存时仍以最新配置版本创建新的历史记录。 */
  function loadVersion(version: number) {
    const value = versions.find((item) => item.version === version);
    if (!value) return;
    form.setFieldsValue({
      prompt: value.prompt,
      script_version: value.scripts.version,
      sdk: value.scripts.sdk,
      chat_ui: value.scripts.chat_ui,
    });
    setLoadedVersion(version);
    setVerified(null);
  }
  /** save 提交完整官方脚本与提示词，不覆盖已经发布模板固定使用的版本。 */
  async function save(values: Record<string, string>) {
    if (!current || !canSave) {
      message.info(tr("请先运行当前候选脚本的模拟检查。"));
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const result = await api<Foundation>("/platform/assistant/foundation", {
        method: "PUT",
        bodyJSON: {
          version: current.version,
          prompt: values.prompt,
          scripts: { version: values.script_version, sdk: values.sdk, chat_ui: values.chat_ui },
        },
      });
      setCurrent(result);
      setVersions((items) => [result, ...items]);
      setLoadedVersion(result.version);
      message.success(tr("提示词与官方脚本新版本已保存"));
    } catch (problem) {
      setError(problem);
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="assistant-settings-section">
      <Alert
        type="warning"
        showIcon
        title={tr("官方脚本由平台维护，AI 生成文件不能修改")}
        description={tr(
          "新版本用于之后创建的模板。已有模板保留其固定版本，更新前请验证发送、接收和附件。",
        )}
      />
      <ErrorNotice error={error} />
      {loading ? (
        <Spin />
      ) : !current ? (
        <Button onClick={() => setRevision((value) => value + 1)}>{tr("重新读取")}</Button>
      ) : (
        <Form form={form} layout="vertical" onFinish={save} disabled={busy}>
          <Space>
            <Tag>
              {tr("配置版本 ")}
              {current.version}
            </Tag>
            <span className="muted">{tr("保存时校验当前版本，防止覆盖其他管理员的修改。")}</span>
          </Space>
          <Form.Item
            label={tr("载入历史版本")}
            extra={tr("载入仅替换编辑区内容。检查通过并保存后，旧内容会成为一个新的当前版本。")}
          >
            <Select
              aria-label={tr("选择官方脚本与提示词历史版本")}
              value={loadedVersion}
              onChange={loadVersion}
              options={versions.map((version) => ({
                value: version.version,
                label:
                  version.version === 0
                    ? tr("内置默认版本")
                    : tr("配置版本 {p0} \u00B7 JS {p1}", {
                        p0: String(version.version),
                        p1: String(version.scripts.version),
                      }),
              }))}
            />
          </Form.Item>
          <Form.Item
            name="script_version"
            label={tr("官方 JS 版本号")}
            rules={[{ required: true, whitespace: true }]}
          >
            <Input maxLength={80} />
          </Form.Item>
          <Tabs
            items={[
              {
                key: "prompt",
                label: tr("默认提示词"),
                forceRender: true,
                children: (
                  <Form.Item name="prompt" rules={[{ required: true, whitespace: true }]}>
                    <Input.TextArea aria-label={tr("默认提示词")} rows={14} />
                  </Form.Item>
                ),
              },
              {
                key: "sdk",
                label: tr("消息 SDK"),
                forceRender: true,
                children: (
                  <Form.Item name="sdk" rules={[{ required: true, whitespace: true }]}>
                    <Input.TextArea
                      className="code-editor"
                      aria-label={tr("官方消息 SDK 脚本")}
                      rows={18}
                      spellCheck={false}
                    />
                  </Form.Item>
                ),
              },
              {
                key: "chat",
                label: tr("聊天动作与渲染"),
                forceRender: true,
                children: (
                  <Form.Item name="chat_ui" rules={[{ required: true, whitespace: true }]}>
                    <Input.TextArea
                      className="code-editor"
                      aria-label={tr("官方聊天动作脚本")}
                      rows={18}
                      spellCheck={false}
                    />
                  </Form.Item>
                ),
              },
            ]}
          />
          <FoundationTest sdk={sdk} chatUI={chatUI} onVerified={setVerified} />
          <Button
            type="primary"
            htmlType="submit"
            loading={busy}
            disabled={!canSave}
            style={{ marginTop: 20 }}
          >
            {tr("保存新版本 ")}
          </Button>
        </Form>
      )}
    </div>
  );
}
/** AssistantSettings 依据身份提供配置标签，普通客服不会挂载此界面或发送配置请求。 */
export function AssistantSettings({
  open,
  onClose,
  tenantId,
  platformAdmin,
  tenantAdmin,
}: {
  open: boolean;
  onClose: () => void;
  tenantId: string;
  platformAdmin: boolean;
  tenantAdmin: boolean;
}) {
  const tr = useTranslator();
  return (
    <Modal
      title={tr("AI 与模板基础配置")}
      open={open}
      onCancel={onClose}
      footer={null}
      width={900}
      destroyOnHidden
      className="assistant-settings-modal"
    >
      <Tabs
        destroyOnHidden
        items={[
          ...(tenantAdmin
            ? [
                {
                  key: "tenant",
                  label: tr("本租户 AI"),
                  children: <ModelSettings source="tenant" tenantId={tenantId} />,
                },
              ]
            : []),
          ...(platformAdmin
            ? [
                {
                  key: "platform",
                  label: tr("平台 AI"),
                  children: <ModelSettings source="platform" tenantId={tenantId} />,
                },
                {
                  key: "foundation",
                  label: tr("官方 JS 与提示词"),
                  children: <FoundationSettings />,
                },
              ]
            : []),
        ]}
      />
    </Modal>
  );
}
