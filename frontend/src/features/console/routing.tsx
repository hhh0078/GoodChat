/** 文件作用说明：分别维护业务组、真实分配池、权重与技能，展示技能和历史优先组合的实际配置。 */
"use client";
import { useState } from "react";
import {
  Alert,
  App,
  Button,
  Drawer,
  Form,
  Input,
  InputNumber,
  Popconfirm,
  Select,
  Space,
  Switch,
  Table,
  Tabs,
  Tag,
} from "antd";
import { PageTitle, ErrorNotice } from "@/components/page-state";
import { ResourcePanel } from "./resource-panel";
import { useResource } from "@/lib/use-resource";
import { api, errorMessage } from "@/lib/api";
import type { Member, Resource } from "@/lib/types";

type Pool = Resource & {
  service_group_id: string;
  algorithm: string;
  assignment_mode: string;
  members_mode: string;
  member_ids: string[];
  member_weights: Record<string, number>;
  required_skill_ids: string[];
  optional_skill_ids: string[];
  history_priority: boolean;
};
type SkilledMember = Resource & {
  display_name: string;
  can_serve: boolean;
  disabled: boolean;
  skill_ids: string[];
};
const algorithms = [
  { label: "最少会话", value: "least_load" },
  { label: "轮询", value: "round_robin" },
  { label: "加权轮询", value: "weighted_round_robin" },
  { label: "人工领取", value: "manual" },
  { label: "负载比例", value: "least_ratio" },
  { label: "随机分配", value: "random" },
];

/** RoutingPage 将业务组、分配池和成员技能分开保存，所有分配仍由后端统一检查资格。 */
export function RoutingPage() {
  const groups = useResource<Resource>("/tenant/groups");
  const members = useResource<Member>("/tenant/members");
  const skills = useResource<Resource>("/tenant/routing-skills");
  const memberOptions = members.items
    .filter((item) => item.can_serve && !item.disabled)
    .map((item) => ({ label: item.display_name || item.email, value: item.id }));
  return (
    <section className="console-screen console-routing">
      <PageTitle
        title="分组与分配"
        description="业务组定义服务范围；必备技能过滤资格，可选技能与历史接待形成优先级，最后执行一个主算法。"
      />
      <div className="table-card">
        <Tabs
          items={[
            {
              key: "groups",
              label: "客服业务组",
              children: (
                <ResourcePanel
                  title="业务组"
                  path="/tenant/groups"
                  onChanged={groups.reload}
                  fields={[
                    { name: "name", label: "业务组名称", required: true },
                    {
                      name: "member_ids",
                      label: "组内客服",
                      type: "multi",
                      options: memberOptions,
                    },
                  ]}
                  help="客户分类和客服技能独立管理。客服加入多个池仍共享同一接待容量。"
                />
              ),
            },
            {
              key: "pools",
              label: "分配池",
              children: (
                <Pools groups={groups.items} members={members.items} skills={skills.items} />
              ),
            },
            {
              key: "skills",
              label: "技能字典",
              children: (
                <ResourcePanel
                  title="技能"
                  path="/tenant/routing-skills"
                  onChanged={skills.reload}
                  fields={[{ name: "name", label: "技能名称", required: true }]}
                  help="归档的技能不能再满足必备要求，已有规则会继续阻止不合格分配；请同步调整引用规则。"
                />
              ),
            },
            {
              key: "member-skills",
              label: "客服技能",
              children: <MemberSkills skills={skills.items} />,
            },
          ]}
        />
      </div>
    </section>
  );
}

/** Pools 使用可读的逐客服权重表单维护真实算法，不把UUID映射JSON暴露为产品配置方式。 */
function Pools({
  groups,
  members,
  skills,
}: {
  groups: Resource[];
  members: Member[];
  skills: Resource[];
}) {
  const pools = useResource<Pool>("/tenant/routing-pools");
  const { message } = App.useApp();
  const [editing, setEditing] = useState<Pool | null | undefined>();
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [form] = Form.useForm();
  const groupID = Form.useWatch("service_group_id", form);
  const mode = Form.useWatch("members_mode", form) || "group";
  const selected = Form.useWatch("member_ids", form) || [];
  const algorithm = Form.useWatch("algorithm", form);
  const group = groups.find((item) => item.id === groupID);
  const groupIDs = Array.isArray(group?.member_ids) ? group.member_ids : [];
  const eligible = members.filter(
    (member) => member.can_serve && !member.disabled && groupIDs.includes(member.id),
  );
  const weighted = eligible.filter((member) => mode === "group" || selected.includes(member.id));
  const skillOptions = skills.map((skill) => ({ label: skill.name || skill.id, value: skill.id }));
  /** edit 填入可读的已有配置或明确的动态组默认值。 */
  function edit(pool: Pool | null) {
    form.resetFields();
    form.setFieldsValue(
      pool || {
        service_group_id: groups[0]?.id,
        algorithm: "least_load",
        assignment_mode: "auto",
        members_mode: "group",
        member_ids: [],
        member_weights: {},
        required_skill_ids: [],
        optional_skill_ids: [],
        history_priority: true,
      },
    );
    setError(null);
    setEditing(pool);
  }
  /** save 只发送当前候选范围内的权重，配置错误时保留表单并展示服务端原因。 */
  async function save(values: Record<string, unknown>) {
    setSaving(true);
    setError(null);
    try {
      const weights = Object.fromEntries(
        weighted.map((member) => [
          member.id,
          Number((values.member_weights as Record<string, number> | undefined)?.[member.id] || 1),
        ]),
      );
      await api(`/tenant/routing-pools${editing ? `/${editing.id}` : ""}`, {
        method: editing ? "PATCH" : "POST",
        tenantId: pools.tenantId,
        bodyJSON: {
          ...values,
          member_ids: mode === "fixed" ? selected : [],
          member_weights: weights,
        },
        idempotent: true,
      });
      message.success("分配规则已保存");
      pools.reload();
      setEditing(undefined);
    } catch (problem) {
      setError(problem);
    } finally {
      setSaving(false);
    }
  }
  /** archive 归档分配池，失败时展示真实服务端错误。 */
  async function archive(pool: Pool) {
    try {
      await api(`/tenant/routing-pools/${pool.id}`, { method: "DELETE", tenantId: pools.tenantId });
      pools.reload();
      message.success("分配池已归档");
    } catch (problem) {
      message.error(errorMessage(problem));
    }
  }
  return (
    <>
      <Space style={{ marginBottom: 16 }}>
        <Button type="primary" onClick={() => edit(null)}>
          新增分配池
        </Button>
      </Space>
      <ErrorNotice error={pools.error} retry={pools.reload} />
      <Table
        rowKey="id"
        dataSource={pools.items}
        pagination={{ pageSize: 10 }}
        columns={[
          { title: "名称", dataIndex: "name" },
          {
            title: "业务组",
            dataIndex: "service_group_id",
            render: (value) => groups.find((group) => group.id === value)?.name || "未找到业务组",
          },
          {
            title: "主算法",
            dataIndex: "algorithm",
            render: (value) => (
              <Tag>{algorithms.find((item) => item.value === value)?.label || value}</Tag>
            ),
          },
          {
            title: "组合规则",
            render: (_, row) => (
              <Space wrap>
                <Tag>历史策略随租户配置</Tag>
                <Tag>必备 {row.required_skill_ids?.length || 0}</Tag>
                <Tag>可选 {row.optional_skill_ids?.length || 0}</Tag>
              </Space>
            ),
          },
          {
            title: "接待方式",
            dataIndex: "assignment_mode",
            render: (value) => (value === "confirm" ? "确认接单" : "自动接入"),
          },
          {
            title: "操作",
            render: (_, row) => (
              <Space>
                <Button type="link" onClick={() => edit(row)}>
                  配置
                </Button>
                <Popconfirm
                  title="归档此分配池？"
                  description="现有咨询保留，新的咨询将无法继续分配到此池。"
                  onConfirm={() => archive(row)}
                >
                  <Button type="link" danger>
                    归档
                  </Button>
                </Popconfirm>
              </Space>
            ),
          },
        ]}
      />
      <Drawer
        rootClassName="console-drawer"
        title={editing ? "配置分配池" : "新增分配池"}
        open={editing !== undefined}
        onClose={() => setEditing(undefined)}
        size="large"
        destroyOnHidden
      >
        <ErrorNotice error={error} />
        <Alert
          type="info"
          showIcon
          title="规则执行顺序"
          description="统一资格与必备技能 → 严格/优先指定客服 → 仍合格的历史客服 → 可选技能匹配数量 → 主算法。原有接待不会因规则编辑被自动转走。"
          style={{ marginBottom: 20 }}
        />
        <Form form={form} layout="vertical" onFinish={save}>
          <h3 className="console-form-section">基本信息</h3>
          <Form.Item
            name="name"
            label="分配池名称"
            rules={[{ required: true, whitespace: true, max: 80 }]}
          >
            <Input maxLength={80} />
          </Form.Item>
          <Form.Item name="service_group_id" label="所属业务组" rules={[{ required: true }]}>
            <Select
              options={groups.map((item) => ({ label: item.name || item.id, value: item.id }))}
            />
          </Form.Item>
          <h3 className="console-form-section">分配方式</h3>
          <Form.Item name="members_mode" label="候选来源">
            <Select
              options={[
                { label: "动态使用业务组成员", value: "group" },
                { label: "仅固定候选客服", value: "fixed" },
              ]}
            />
          </Form.Item>
          {mode === "fixed" && (
            <Form.Item
              name="member_ids"
              label="固定候选客服"
              rules={[
                { required: true, type: "array", min: 1, message: "至少选择一位业务组内客服" },
              ]}
            >
              <Select
                mode="multiple"
                options={eligible.map((item) => ({
                  label: item.display_name || item.email,
                  value: item.id,
                }))}
              />
            </Form.Item>
          )}
          <Form.Item name="algorithm" label="主分配算法" rules={[{ required: true }]}>
            <Select options={algorithms} />
          </Form.Item>
          {algorithm === "weighted_round_robin" && (
            <>
              <p className="form-hint">
                权重控制长期分配比例；离线、示忙、满载或技能不符的成员不参与本轮。权重1至1000，配置保存会重置该池的轮询积分。
              </p>
              {weighted.map((member) => (
                <Form.Item
                  key={member.id}
                  name={["member_weights", member.id]}
                  label={member.display_name || member.email}
                  initialValue={1}
                  preserve={false}
                >
                  <InputNumber min={1} max={1000} precision={0} />
                </Form.Item>
              ))}
            </>
          )}
          <h3 className="console-form-section">资格与优先级</h3>
          <Form.Item name="required_skill_ids" label="全部必须具备的技能">
            <Select mode="multiple" allowClear options={skillOptions} />
          </Form.Item>
          <Form.Item
            name="optional_skill_ids"
            label="优先匹配的可选技能"
            extra="先按可选技能匹配数量排序，再执行主算法；不能绕过必备技能。"
          >
            <Select mode="multiple" allowClear options={skillOptions} />
          </Form.Item>
          <p className="form-hint">
            历史客服优先窗口在租户设置的“复访与历史”统一配置；历史客服仍须满足当前组池、技能、在线、忙闲和容量。
          </p>
          <Form.Item name="assignment_mode" label="接待方式">
            <Select
              options={[
                { label: "自动接入", value: "auto" },
                { label: "客服确认接单", value: "confirm" },
              ]}
            />
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={saving}>
            保存分配规则
          </Button>
        </Form>
      </Drawer>
    </>
  );
}

/** MemberSkills 为租户成员保存独立技能属性，不改变其管理角色或坐席资格。 */
function MemberSkills({ skills }: { skills: Resource[] }) {
  const resource = useResource<SkilledMember>("/tenant/routing-members");
  const { message } = App.useApp();
  const [editing, setEditing] = useState<SkilledMember | null>(null);
  const [ids, setIDs] = useState<string[]>([]);
  const [saving, setSaving] = useState(false);
  /** save 更新指定成员技能后重新读取实际持久化结果。 */
  async function save() {
    if (!editing) return;
    setSaving(true);
    try {
      await api(`/tenant/routing-members/${editing.id}`, {
        method: "PATCH",
        tenantId: resource.tenantId,
        bodyJSON: { skill_ids: ids },
      });
      resource.reload();
      setEditing(null);
      message.success("客服技能已保存");
    } catch (problem) {
      message.error(errorMessage(problem));
    } finally {
      setSaving(false);
    }
  }
  return (
    <>
      <ErrorNotice error={resource.error} retry={resource.reload} />
      <Table
        rowKey="id"
        dataSource={resource.items}
        columns={[
          { title: "成员", dataIndex: "display_name" },
          {
            title: "技能",
            render: (_, row) => (
              <Space wrap>
                {row.skill_ids.map((id) => (
                  <Tag key={id}>
                    {skills.find((skill) => skill.id === id)?.name || "已归档技能"}
                  </Tag>
                ))}
              </Space>
            ),
          },
          {
            title: "操作",
            render: (_, row) => (
              <Button
                type="link"
                onClick={() => {
                  setEditing(row);
                  setIDs(row.skill_ids);
                }}
              >
                配置技能
              </Button>
            ),
          },
        ]}
        pagination={{ pageSize: 10 }}
      />
      <Drawer
        rootClassName="console-drawer"
        title={`配置${editing?.display_name || "成员"}的技能`}
        open={!!editing}
        onClose={() => setEditing(null)}
      >
        <Select
          aria-label="成员技能"
          mode="multiple"
          value={ids}
          onChange={setIDs}
          style={{ width: "100%" }}
          options={skills.map((skill) => ({ label: skill.name || skill.id, value: skill.id }))}
        />
        <Button
          type="primary"
          loading={saving}
          onClick={() => void save()}
          style={{ marginTop: 20 }}
        >
          保存技能
        </Button>
      </Drawer>
    </>
  );
}
