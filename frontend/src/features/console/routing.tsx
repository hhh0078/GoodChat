/** 文件作用说明：分别维护客服业务组与分配池，展示实际服务端成员和算法设置。 */
"use client";
import { Tabs, Tag } from "antd";
import { PageTitle } from "@/components/page-state";
import { ResourcePanel } from "./resource-panel";
import { useResource } from "@/lib/use-resource";
import type { Member, Resource } from "@/lib/types";
/** RoutingPage 将组织分组与实际候选分配池分开配置。 */
export function RoutingPage() {
  const groups = useResource<Resource>("/tenant/groups");
  const members = useResource<Member>("/tenant/members");
  return (
    <>
      <PageTitle
        title="分组与分配"
        description="客服业务组定义服务范围，分配池定义实际候选人与分配算法。"
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
                      options: members.items
                        .filter((item) => item.can_serve && !item.disabled)
                        .map((item) => ({
                          label: item.display_name || item.email,
                          value: item.id,
                        })),
                    },
                  ]}
                  help="这里维护客服的业务范围，客户分类在客户资料中独立管理。"
                />
              ),
            },
            {
              key: "pools",
              label: "分配池",
              children: (
                <ResourcePanel
                  title="分配池"
                  path="/tenant/routing-pools"
                  columns={[
                    { title: "名称", dataIndex: "name" },
                    {
                      title: "业务组",
                      dataIndex: "service_group_id",
                      render: (value) =>
                        groups.items.find((item) => item.id === value)?.name || "默认",
                    },
                    {
                      title: "算法",
                      dataIndex: "algorithm",
                      render: (value) => (
                        <Tag>
                          {(
                            {
                              least_load: "最少会话",
                              round_robin: "轮询",
                              manual: "人工领取",
                              least_ratio: "负载比例",
                              random: "随机",
                            } as Record<string, string>
                          )[String(value)] || String(value)}
                        </Tag>
                      ),
                    },
                    {
                      title: "接待方式",
                      dataIndex: "assignment_mode",
                      render: (value) => (value === "confirm" ? "确认接单" : "自动接入"),
                    },
                  ]}
                  fields={[
                    { name: "name", label: "分配池名称", required: true },
                    {
                      name: "service_group_id",
                      label: "所属业务组",
                      type: "select",
                      options: groups.items.map((item) => ({
                        label: item.name || item.id,
                        value: item.id,
                      })),
                    },
                    {
                      name: "algorithm",
                      label: "分配算法",
                      type: "select",
                      initial: "least_load",
                      required: true,
                      options: [
                        { label: "最少会话", value: "least_load" },
                        { label: "轮询", value: "round_robin" },
                        { label: "人工领取", value: "manual" },
                        { label: "负载比例", value: "least_ratio" },
                        { label: "随机分配", value: "random" },
                      ],
                    },
                    {
                      name: "assignment_mode",
                      label: "接待方式",
                      type: "select",
                      initial: "auto",
                      required: true,
                      options: [
                        { label: "自动接入", value: "auto" },
                        { label: "客服确认接单", value: "confirm" },
                      ],
                    },
                    {
                      name: "member_ids",
                      label: "候选客服",
                      type: "multi",
                      options: members.items
                        .filter((item) => item.can_serve && !item.disabled)
                        .map((item) => ({
                          label: item.display_name || item.email,
                          value: item.id,
                        })),
                      hint: "候选人仍需满足在线状态、业务组与接待容量约束。",
                    },
                  ]}
                />
              ),
            },
          ]}
        />
      </div>
    </>
  );
}
