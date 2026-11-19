/** 文件作用说明：分别管理每日文章计划和搜索引擎凭据，私密配置仅显示是否已保存。 */
"use client";
import { Alert, App, Button, Checkbox, Drawer, Form, Input, Select, Switch } from "antd";
import { useEffect, useState, useRef } from "react";
import { api, errorMessage } from "@/lib/api";
import { languageNames, locales, availableTimezones } from "@/i18n/preferences";
import { useTranslator } from "@/i18n/use-translator";
import type { ArticleSettings } from "./types";
/** ArticleSettingsDrawer 采用设置版本保存，密钥空白时保留原配置，不回显服务端密文。 */
export function ArticleSettingsDrawer({
  section,
  onClose,
  onSaved,
}: {
  section: "ai" | "search" | undefined;
  onClose: () => void;
  onSaved: () => void;
}) {
  const lastSection = useRef(section);
  if (section) lastSection.current = section;
  const visibleSection = section || lastSection.current;
  const tr = useTranslator();
  const { message } = App.useApp();
  const [form] = Form.useForm();
  const [settings, setSettings] = useState<ArticleSettings | null>(null);
  const [busy, setBusy] = useState(false);
  const [pending, setPending] = useState(false);
  useEffect(() => {
    if (!section) return;
    let alive = true;
    setSettings(null);
    form.resetFields();
    api<ArticleSettings>("/platform/article-settings")
      .then((data) => {
        if (alive) {
          setSettings(data);
          form.setFieldsValue(data);
        }
      })
      .catch((e) => message.error(errorMessage(e)));
    return () => {
      alive = false;
    };
  }, [section, form, message]);
  /** save 使用当前配置版本独立提交分区，发生冲突时不覆盖其他管理员设置。 */
  async function save() {
    setBusy(true);
    try {
      const values = await form.validateFields();
      const data = await api<ArticleSettings>("/platform/article-settings", {
        method: "PUT",
        bodyJSON: { ...values, section, version: settings?.version },
      });
      setSettings(data);
      form.setFieldsValue(data);
      message.success(tr("设置已保存"));
      onSaved();
      onClose();
    } catch (e) {
      if (e instanceof Error) message.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  /** run 主动建立一项生成或提交任务，不把排队当成实际成功。 */
  async function run() {
    setPending(true);
    try {
      await api(section === "ai" ? "/platform/article-generate" : "/platform/article-sync", {
        method: "POST",
      });
      message.success(tr("任务已排队，请查看执行记录。"));
      onSaved();
    } catch (e) {
      message.error(errorMessage(e));
    } finally {
      setPending(false);
    }
  }
  const configured = (yes?: boolean) => (yes ? tr("已配置，留空保留") : tr("尚未配置"));
  return (
    <Drawer
      open={!!section}
      onClose={onClose}
      size={660}
      loading={!settings}
      title={tr(visibleSection === "ai" ? "AI定时文章" : "搜索引擎设置")}
      footer={
        <Button type="primary" onClick={() => void save()} loading={busy} disabled={!settings}>
          {tr("保存")}
        </Button>
      }
    >
      <Form form={form} layout="vertical">
        {visibleSection === "ai" ? (
          <>
            <Alert
              type={settings?.ai_configured ? "info" : "warning"}
              showIcon
              title={tr("使用平台已启用的文字模型，每次生成前读取全部已有文章标题并检查重复。")}
              description={settings?.model || tr("平台尚未配置可用文字模型")}
            />
            <Form.Item name="ai_enabled" label={tr("每日自动发布")} valuePropName="checked">
              <Switch />
            </Form.Item>
            <Form.Item
              name="ai_prompt"
              label={tr("文章提示词")}
              extra={tr("请描述行业、使用场景和关键词；标题应是客户可能搜索的具体问题。")}
            >
              <Input.TextArea rows={7} maxLength={16000} />
            </Form.Item>
            <Form.Item name="source_language" label={tr("原文语言")} rules={[{ required: true }]}>
              <Select options={locales.map((value) => ({ value, label: languageNames[value] }))} />
            </Form.Item>
            <Form.Item name="target_languages" label={tr("翻译语言")}>
              <Checkbox.Group
                options={locales.map((value) => ({ value, label: languageNames[value] }))}
              />
            </Form.Item>
            <div className="article-editor-pair">
              <Form.Item
                name="daily_time"
                label={tr("每日发布时间")}
                rules={[{ required: true, pattern: /^([01]\d|2[0-3]):[0-5]\d$/ }]}
              >
                <Input type="time" />
              </Form.Item>
              <Form.Item name="timezone" label={tr("时区")} rules={[{ required: true }]}>
                <Select
                  showSearch
                  options={availableTimezones("Asia/Shanghai").map((value) => ({
                    value,
                    label: value,
                  }))}
                />
              </Form.Item>
            </div>
            <p className="muted">
              {tr("默认每天18:00，北京时间。先保存设置，再手动生成。")}{" "}
              {settings?.next_run_at && new Date(settings.next_run_at).toLocaleString()}
            </p>
            <Button
              onClick={() => void run()}
              loading={pending}
              disabled={!settings?.ai_configured}
            >
              {tr("立即生成一篇")}
            </Button>
          </>
        ) : (
          <>
            <Alert
              type="info"
              showIcon
              title={tr("提交成功仅代表搜索平台接收请求，不代表已收录。")}
              description={tr(
                "百度提交文章链接；必应使用IndexNow；Google通过Search Console提交站点地图。",
              )}
            />
            <p>
              <a href={settings?.sitemap_url} target="_blank" rel="noreferrer">
                {settings?.sitemap_url}
              </a>
            </p>
            <Form.Item name="baidu_enabled" label={tr("百度自动提交")} valuePropName="checked">
              <Switch />
            </Form.Item>
            <Form.Item name="baidu_token" label={tr("百度推送令牌")}>
              <Input.Password
                autoComplete="new-password"
                placeholder={configured(settings?.baidu_token_configured)}
              />
            </Form.Item>
            <Form.Item
              name="indexnow_enabled"
              label={tr("必应 / IndexNow 自动提交")}
              valuePropName="checked"
            >
              <Switch />
            </Form.Item>
            <Form.Item
              name="indexnow_key"
              label={tr("IndexNow验证键")}
              extra={settings?.indexnow_key_location}
            >
              <Input.Password
                autoComplete="new-password"
                placeholder={configured(settings?.indexnow_key_configured)}
              />
            </Form.Item>
            <Form.Item
              name="google_enabled"
              label={tr("Google站点地图提交")}
              valuePropName="checked"
            >
              <Switch />
            </Form.Item>
            <Form.Item
              name="google_site"
              label={tr("Search Console站点资源")}
              extra={tr("服务账号须有该站点管理权限；填写网址或sc-domain资源。")}
            >
              <Input placeholder="https://example.com/" />
            </Form.Item>
            <Form.Item name="google_credentials" label={tr("Google服务账号JSON")}>
              <Input.TextArea
                rows={4}
                autoComplete="off"
                placeholder={configured(settings?.google_credentials_configured)}
              />
            </Form.Item>
            <Button onClick={() => void run()} loading={pending}>
              {tr("提交全部已发布文章")}
            </Button>
          </>
        )}
      </Form>
    </Drawer>
  );
}
