/** 文件作用说明：管理接入链接、生成二维码和参数化链接，并调用真实入口接口保存。 */
"use client";
import { EmptyData, ErrorNotice, PageTitle } from "@/components/page-state";
import { localizeStatic } from "@/i18n/runtime";
import { useTranslator } from "@/i18n/use-translator";
import { api, errorMessage } from "@/lib/api";
import { linkTemplateBinding } from "@/lib/link-template-binding";
import { serialPoll } from "@/lib/reporting-state";
import type { AccessLink, Member, Resource } from "@/lib/types";
import { entryURL } from "@/lib/urls";
import { useResource } from "@/lib/use-resource";
import { CopyOutlined, PlusOutlined, QrcodeOutlined } from "@ant-design/icons";
import {
  App,
  Button,
  Checkbox,
  Drawer,
  Form,
  Input,
  Popconfirm,
  QRCode,
  Select,
  Space,
  Table,
  Tabs,
  Tag,
} from "antd";
import type { ColumnsType } from "antd/es/table";
import { useEffect, useState } from "react";
import { LinkTemplateFields, type PublishedTemplatePage } from "./link-template-fields";
const parameterExamples: Record<string, string> = {
  姓名: "张三",
  手机号: "13800000000",
  头像: "https://example.com/avatar.png",
  性别: "男",
  extra: '{"有无房产":"有","是否逾期":"无"}',
};
type LinkStatistics = {
  items: {
    id: string;
    visits: number;
    visitors: number;
    new_conversations: number;
    active: number;
    queued: number;
    closed: number;
  }[];
  timezone: string;
};
/** LinksPage 为独立业务入口维护路由范围并生成分享与嵌入内容。 */
export function LinksPage() {
  const tr = useTranslator();
  const links = useResource<AccessLink>("/tenant/access-links");
  const templatePages = useResource<PublishedTemplatePage>("/tenant/pages");
  const [statistics, setStatistics] = useState<{
    tenantId: string;
    data: LinkStatistics;
  } | null>(null);
  const [statisticsError, setStatisticsError] = useState<unknown>(null);
  const [statisticsRevision, setStatisticsRevision] = useState(0);
  useEffect(() => {
    if (!links.tenantId) return;
    const controller = new AbortController();
    /** loadStatistics 读取租户当地今日的入口访问计数，列表无需逐个链接请求。 */
    const loadStatistics = serialPoll(controller.signal, async () => {
      try {
        const data = await api<LinkStatistics>("/tenant/link-statistics", {
          tenantId: links.tenantId,
          signal: controller.signal,
        });
        if (!controller.signal.aborted) {
          setStatistics({ tenantId: links.tenantId, data });
          setStatisticsError(null);
        }
      } catch (problem) {
        if (!controller.signal.aborted) setStatisticsError(problem);
      }
    });
    void loadStatistics();
    const timer = setInterval(() => void loadStatistics(), 30000);
    return () => {
      controller.abort();
      clearInterval(timer);
    };
  }, [links.tenantId, statisticsRevision]);
  const currentStatistics = statistics?.tenantId === links.tenantId ? statistics.data : null;
  const metrics = new Map(currentStatistics?.items.map((item) => [item.id, item]) || []);
  const groups = useResource<Resource>("/tenant/groups");
  const pools = useResource<Resource>("/tenant/routing-pools");
  const skills = useResource<Resource>("/tenant/routing-skills");
  const members = useResource<Member>("/tenant/members");
  const [editing, setEditing] = useState<AccessLink | null | undefined>();
  const [sharing, setSharing] = useState<AccessLink | null>(null);
  const [parameterKeys, setParameterKeys] = useState<string[]>([]);
  const [parameterValues, setParameterValues] = useState(localizeStatic(parameterExamples, tr));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [form] = Form.useForm();
  const { message } = App.useApp();
  /** link 通过当前站点生成标准公开聊天地址。 */
  function link(item: AccessLink) {
    const profile = Object.fromEntries(parameterKeys.map((key) => [key, parameterValues[key]]));
    return typeof window === "undefined"
      ? `/c/${item.entry_key}`
      : entryURL(window.location.origin, item.entry_key, profile);
  }
  let parameterError = "";
  if (parameterKeys.includes("extra")) {
    try {
      const extra = JSON.parse(parameterValues.extra);
      if (!extra || typeof extra !== "object" || Array.isArray(extra))
        parameterError = tr("extra 必须是 JSON 对象");
    } catch {
      parameterError = tr("extra 必须是合法 JSON 对象");
    }
  }
  if (!parameterError && sharing && new TextEncoder().encode(link(sharing)).length > 2048)
    parameterError = tr("链接超过2 KiB，请减少资料，或改用POST初始化接口传递较大的JSON。");
  /** edit 将已有入口或初始配置填入真实保存表单。 */
  function edit(item: AccessLink | null) {
    setError(null);
    form.resetFields();
    form.setFieldsValue(
      item
        ? {
            ...item,
            reception_mode: item.reception_mode || "inherit",
            channel_tags: item.channel_tags || (item.channel_code ? [item.channel_code] : []),
            binding_mode: item.binding_mode || "FOLLOW_PAGE",
          }
        : {
            status: "active",
            agent_mode: "strict",
            reception_mode: "auto",
            channel_tags: [],
            required_skill_ids: [],
            optional_skill_ids: [],
            page_id: null,
            binding_mode: "FOLLOW_PAGE",
            pinned_release_id: null,
          },
    );
    setEditing(item);
  }
  /** save 创建或修改接入链接，失败时保留未保存内容。 */
  async function save(values: Record<string, unknown>) {
    setSaving(true);
    setError(null);
    try {
      await api(`/tenant/access-links${editing ? `/${editing.id}` : ""}`, {
        method: editing ? "PATCH" : "POST",
        tenantId: links.tenantId,
        bodyJSON: { ...values, ...linkTemplateBinding(values), agent_mode: "strict" },
        idempotent: true,
      });
      message.success(tr("接入链接已保存"));
      setEditing(undefined);
      links.reload();
    } catch (problem) {
      setError(problem);
    } finally {
      setSaving(false);
    }
  }
  /** copy 只有系统剪贴板写入成功后才反馈已复制。 */
  async function copy(value: string) {
    try {
      if (parameterError) {
        message.warning(parameterError);
        return;
      }
      await navigator.clipboard.writeText(value);
      message.success(tr("已复制"));
    } catch {
      message.warning(tr("浏览器未允许复制，请在分享窗口手动选择链接。"));
    }
  }
  /** archive 归档入口，服务端决定已有咨询的后续行为。 */
  async function archive(item: AccessLink) {
    try {
      await api(`/tenant/access-links/${item.id}`, { method: "DELETE", tenantId: links.tenantId });
      message.success(tr("入口已归档"));
      links.reload();
    } catch (problem) {
      message.error(errorMessage(problem));
    }
  }
  const columns: ColumnsType<AccessLink> = [
    {
      title: tr("入口名称"),
      dataIndex: "name",
      render: (value, row) => (
        <>
          <strong>{value}</strong>
          <div className="console-entry-key" title={row.entry_key}>
            {row.entry_key}
          </div>
        </>
      ),
    },
    {
      title: tr("接待范围"),
      key: "routing",
      render: (_, row) => (
        <>
          <div>
            {groups.items.find((item) => item.id === row.service_group_id)?.name || tr("默认范围")}
          </div>
          <div className="muted" style={{ fontSize: 12 }}>
            {pools.items.find((item) => item.id === row.routing_pool_id)?.name || tr("默认分配")}
          </div>
        </>
      ),
    },
    {
      title: tr("今日访客"),
      key: "visitors",
      width: 110,
      render: (_, row) => (
        <>
          <strong>{metrics.get(row.id)?.visitors ?? "—"}</strong>
          <div className="muted" style={{ fontSize: 12 }}>
            {tr("访问 ")}
            {metrics.get(row.id)?.visits ?? "—"}
            {tr(" 次 ")}
          </div>
        </>
      ),
    },
    {
      title: tr("今日咨询"),
      key: "conversations",
      width: 110,
      render: (_, row) => (
        <>
          <strong>{metrics.get(row.id)?.new_conversations ?? "—"}</strong>
          <div className="muted" style={{ fontSize: 12 }}>
            {tr("结束 ")}
            {metrics.get(row.id)?.closed ?? "—"}
          </div>
        </>
      ),
    },
    {
      title: tr("当前状态"),
      key: "current",
      width: 120,
      render: (_, row) => (
        <div style={{ fontSize: 12 }}>
          {tr("接待 ")}
          {metrics.get(row.id)?.active ?? "—"}
          <div className="muted">
            {tr("排队 ")}
            {metrics.get(row.id)?.queued ?? "—"}
          </div>
        </div>
      ),
    },
    {
      title: tr("渠道标识"),
      dataIndex: "channel_code",
      render: (value, row) => (
        <Space size={[0, 4]} wrap>
          {Array.from(new Set([value, ...(row.channel_tags || [])].filter(Boolean))).map((tag) => (
            <Tag key={tag}>{tag}</Tag>
          ))}
        </Space>
      ),
    },
    {
      title: tr("用户端模板"),
      key: "template",
      render: (_, row) => (
        <span>
          {row.page_id
            ? templatePages.items.find((page) => page.id === row.page_id)?.name || tr("已绑定模板")
            : tr("默认页面")}
          {row.page_id && (
            <div className="muted" style={{ fontSize: 12 }}>
              {row.binding_mode === "PIN_RELEASE" ? tr("固定版本") : tr("跟随发布")}
            </div>
          )}
        </span>
      ),
    },
    {
      title: tr("状态"),
      dataIndex: "status",
      render: (value) => (
        <Tag color={value === "active" ? "green" : "default"}>
          {value === "active" ? tr("启用") : tr("已停用")}
        </Tag>
      ),
    },
    {
      title: tr("操作"),
      width: 230,
      render: (_, row) => (
        <Space>
          <Button type="link" size="small" onClick={() => edit(row)}>
            {tr("配置 ")}
          </Button>
          <Button type="link" size="small" onClick={() => setSharing(row)}>
            {tr("分享 ")}
          </Button>
          <Button type="link" size="small" onClick={() => setSharing(row)}>
            {tr("访问与参数 ")}
          </Button>
          <Popconfirm title={tr("归档该入口？")} onConfirm={() => archive(row)}>
            <Button type="link" danger size="small">
              {tr("归档 ")}
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];
  return (
    <section className="console-screen console-links">
      <PageTitle
        title={tr("接入管理")}
        description={tr("不同入口分别配置接待范围、来源和品牌页面，电脑与手机都能打开。")}
        action={
          <Button type="primary" icon={<PlusOutlined />} onClick={() => edit(null)}>
            {tr("创建接入链接 ")}
          </Button>
        }
      />
      <p className="muted" style={{ fontSize: 12 }}>
        {tr("今日数据按")}
        {currentStatistics?.timezone || tr("租户当地时区")}
        {tr("统计，每30秒更新。访客按实际访问入口去重，咨询按创建入口统计。")}{" "}
        <Button
          type="link"
          size="small"
          onClick={() => setStatisticsRevision((value) => value + 1)}
        >
          {tr("刷新数据 ")}
        </Button>
      </p>
      <ErrorNotice
        error={statisticsError}
        retry={() => setStatisticsRevision((value) => value + 1)}
      />
      <div className="table-card">
        <ErrorNotice error={links.error} retry={links.reload} />
        <Table
          rowKey="id"
          columns={columns}
          dataSource={links.items}
          loading={links.loading}
          scroll={{ x: 1320 }}
          locale={{ emptyText: <EmptyData text={tr("创建第一个链接，开始接待客户。")} /> }}
        />
      </div>
      <Drawer
        rootClassName="console-drawer"
        title={editing ? tr("配置接入链接") : tr("创建接入链接")}
        open={editing !== undefined}
        onClose={() => setEditing(undefined)}
        width={500}
        destroyOnHidden
      >
        <ErrorNotice error={error} />
        <Form form={form} layout="vertical" onFinish={save}>
          <Form.Item
            name="name"
            label={tr("入口名称")}
            rules={[{ required: true, whitespace: true }]}
          >
            <Input placeholder={tr("例如：官网售前、订单售后")} maxLength={100} />
          </Form.Item>
          <ErrorNotice error={templatePages.error} retry={templatePages.reload} />
          <LinkTemplateFields form={form} tenantId={links.tenantId} pages={templatePages.items} />
          <Form.Item name="service_group_id" label={tr("客服业务组")}>
            <Select
              allowClear
              placeholder={tr("使用默认业务组")}
              options={groups.items.map((item) => ({
                label: item.name || item.id,
                value: item.id,
              }))}
            />
          </Form.Item>
          <Form.Item name="routing_pool_id" label={tr("分配池")}>
            <Select
              allowClear
              placeholder={tr("使用默认分配规则")}
              options={pools.items.map((item) => ({ label: item.name || item.id, value: item.id }))}
            />
          </Form.Item>
          <Form.Item
            name="required_skill_ids"
            label={tr("入口附加必备技能")}
            extra={tr("与分配池必备技能叠加；更换入口或公开参数不能放宽分配池要求。")}
          >
            <Select
              mode="multiple"
              allowClear
              options={skills.items.map((item) => ({
                label: item.name || item.id,
                value: item.id,
              }))}
            />
          </Form.Item>
          <Form.Item
            name="optional_skill_ids"
            label={tr("入口附加可选技能")}
            extra={tr("与池的可选技能共同排序，必备要求优先。")}
          >
            <Select
              mode="multiple"
              allowClear
              options={skills.items.map((item) => ({
                label: item.name || item.id,
                value: item.id,
              }))}
            />
          </Form.Item>
          <Form.Item
            name="reception_mode"
            label={tr("接待方式")}
            extra={tr("仅影响此链接；自动分配仍遵守客服组、技能与接待容量限制。")}
          >
            <Select
              options={[
                { label: tr("自动分配"), value: "auto" },
                { label: tr("客服自行领取"), value: "manual" },
                ...(editing?.reception_mode === "inherit"
                  ? [{ label: tr("沿用分配池的接待方式"), value: "inherit" }]
                  : []),
              ]}
            />
          </Form.Item>
          <Form.Item name="agent_id" label={tr("优先指定客服")}>
            <Select
              allowClear
              options={members.items
                .filter((item) => item.can_serve && !item.disabled)
                .map((item) => ({ label: item.display_name || item.email, value: item.id }))}
            />
          </Form.Item>
          <p className="form-hint">
            {tr(
              "指定客服优先于历史客服。指定客服离线时访客无法进入；历史客服离线才回到正常分配。 ",
            )}
          </p>
          <Form.Item
            name="channel_code"
            label={tr("主渠道标识")}
            extra={tr("用于来源筛选与统计，已有来源标识保持兼容。")}
          >
            <Input placeholder={tr("例如：官网、广告A")} maxLength={100} />
          </Form.Item>
          <Form.Item
            name="channel_tags"
            label={tr("渠道标签")}
            extra={tr("可输入多个标签并按回车确认，展示在客户姓名旁。")}
          >
            <Select
              mode="tags"
              maxCount={20}
              tokenSeparators={[",", "，"]}
              placeholder={tr("例如：官网、售前、广告A")}
            />
          </Form.Item>
          <Form.Item name="status" label={tr("入口状态")}>
            <Select
              options={[
                { label: tr("启用"), value: "active" },
                { label: tr("停用"), value: "disabled" },
              ]}
            />
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={saving}>
            {tr("保存入口 ")}
          </Button>
        </Form>
      </Drawer>
      <Drawer
        rootClassName="console-drawer console-share-drawer"
        title={
          <Space>
            <QrcodeOutlined />
            {tr("分享接入链接 ")}
          </Space>
        }
        open={!!sharing}
        onClose={() => setSharing(null)}
        width={620}
        mask={false}
        destroyOnHidden
      >
        <Tabs
          defaultActiveKey="link"
          items={[
            {
              key: "link",
              label: tr("链接"),
              children: (
                <Form layout="vertical">
                  <Form.Item
                    label={tr("勾选要拼接的资料参数")}
                    extra={tr(
                      "资料参数始终接收，中文字段更易读；phone、name、avatar_url、sex 等英文写法也兼容。",
                    )}
                  >
                    <Checkbox.Group
                      value={parameterKeys}
                      onChange={(keys) => setParameterKeys(keys as string[])}
                      options={Object.keys(localizeStatic(parameterExamples, tr)).map((key) => ({
                        label: key === "extra" ? tr("额外资料（extra JSON）") : key,
                        value: key,
                      }))}
                    />
                  </Form.Item>
                  {parameterKeys.map((key) => (
                    <Form.Item
                      key={key}
                      label={key === "extra" ? tr("额外资料 extra") : key}
                      validateStatus={key === "extra" && parameterError ? "error" : undefined}
                      help={key === "extra" ? parameterError || undefined : undefined}
                    >
                      {key === "性别" ? (
                        <Select
                          value={parameterValues[key]}
                          onChange={(value) =>
                            setParameterValues((previous) => ({ ...previous, [key]: value }))
                          }
                          options={[tr("男"), tr("女"), tr("未知")].map((value) => ({
                            value,
                            label: value,
                          }))}
                        />
                      ) : key === "extra" ? (
                        <Input.TextArea
                          rows={3}
                          value={parameterValues[key]}
                          onChange={(event) =>
                            setParameterValues((previous) => ({
                              ...previous,
                              [key]: event.target.value,
                            }))
                          }
                        />
                      ) : (
                        <Input
                          value={parameterValues[key]}
                          onChange={(event) =>
                            setParameterValues((previous) => ({
                              ...previous,
                              [key]: event.target.value,
                            }))
                          }
                        />
                      )}
                    </Form.Item>
                  ))}
                  <p className="form-hint">
                    {tr(
                      "性别支持\u201C男、女、未知\u201D，也支持 sex=man、sex=woman、sex=unknown。字段不能满足需求时，可在链接后添加 extra= ",
                    )}
                    {localizeStatic(parameterExamples, tr).extra}
                    {tr(
                      "。也可直接传入自定义字段；客服右侧客户资料会逐行显示。复制时会自动编码中文、JSON与特殊字符。 ",
                    )}
                  </p>
                  {parameterError && (
                    <p role="alert" className="form-hint">
                      {parameterError}
                    </p>
                  )}
                  <Form.Item label={tr("聊天链接")}>
                    <Input
                      readOnly
                      value={sharing ? link(sharing) : ""}
                      addonAfter={
                        <Button
                          type="text"
                          size="small"
                          icon={<CopyOutlined />}
                          aria-label={tr("复制链接")}
                          onClick={() => sharing && void copy(link(sharing))}
                        />
                      }
                    />
                  </Form.Item>
                  <Button
                    type="primary"
                    href={sharing && !parameterError ? link(sharing) : undefined}
                    target="_blank"
                    disabled={!!parameterError}
                  >
                    {tr("打开此链接 ")}
                  </Button>
                  <p className="form-hint">
                    {tr("访客通过此链接即可发起咨询；公开参数仅为展示资料，不用于恢复身份。 ")}
                  </p>
                  <section className="console-share-qr">
                    <h3>{tr("预览二维码")}</h3>
                    {sharing && !parameterError && <QRCode value={link(sharing)} />}
                  </section>
                </Form>
              ),
            },
            {
              key: "qr",
              label: tr("二维码"),
              children: (
                <div className="console-share-qr">
                  {sharing && !parameterError && <QRCode value={link(sharing)} />}
                  <p className="form-hint">{tr("可用手机扫描，打开对应咨询入口。")}</p>
                </div>
              ),
            },
            {
              key: "embed",
              label: tr("嵌入"),
              children: (
                <Form layout="vertical">
                  <Form.Item label={tr("网页嵌入示例")}>
                    <Input.TextArea
                      readOnly
                      rows={6}
                      value={
                        sharing
                          ? tr(
                              '<iframe src="{p0}" title="在线客服" style="width:100%;height:680px;border:0"></iframe>',
                              { p0: String(link(sharing)) },
                            )
                          : ""
                      }
                    />
                  </Form.Item>
                </Form>
              ),
            },
          ]}
        />
        <p className="form-hint">
          {tr(
            "姓名、手机号和业务 JSON 可按接入文档传入。公开参数只用于展示与上下文，不授予历史会话权限。 ",
          )}
        </p>
      </Drawer>
    </section>
  );
}
