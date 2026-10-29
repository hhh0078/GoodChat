/** 文件作用说明：以两列紧凑展示客户搜索和时间筛选入口，点击后查看与调整日期区间。 */
"use client";
import { useState } from "react";
import { Button, DatePicker, Input, Popover } from "antd";
import { CalendarOutlined } from "@ant-design/icons";
import dayjs from "dayjs";
import type { ConversationPeriod } from "@/lib/conversation-list";

/** ConversationFilters 用简短入口展开真实日期筛选，日期值保留在弹层中。 */
export function ConversationFilters({
  period,
  timezone,
  onPeriod,
  onSearch,
}: {
  period: ConversationPeriod;
  timezone: string;
  onPeriod: (period: ConversationPeriod, custom: boolean) => void;
  onSearch: (query: string) => void;
}) {
  const [open, setOpen] = useState(false);
  return (
    <div className="conversation-filters">
      <Input.Search
        allowClear
        aria-label="搜索客户"
        placeholder="姓名 / 手机号"
        onSearch={onSearch}
      />
      <Popover
        trigger="click"
        placement="bottomRight"
        open={open}
        onOpenChange={setOpen}
        content={
          <div className="conversation-period-popover">
            <DatePicker.RangePicker
              aria-label="会话活动日期"
              classNames={{ popup: { root: "conversation-period-calendar" } }}
              allowClear={false}
              value={[dayjs(period.from), dayjs(period.to)]}
              onChange={(value) => {
                if (!value?.[0] || !value[1]) return;
                onPeriod(
                  { from: value[0].format("YYYY-MM-DD"), to: value[1].format("YYYY-MM-DD") },
                  true,
                );
                setOpen(false);
              }}
            />
            <div className="conversation-period-help">
              <span>
                按最新活动时间 · {timezone}
                <br />
                每日 00:00 至 23:59:59
              </span>
              <Button
                type="link"
                size="small"
                onClick={() => {
                  onPeriod(period, false);
                  setOpen(false);
                }}
              >
                最近3天
              </Button>
            </div>
          </div>
        }
      >
        <button
          className="conversation-period-trigger"
          type="button"
          aria-label="时间筛选"
          aria-description={`当前范围：${period.from} 00:00 至 ${period.to} 23:59:59`}
          aria-expanded={open}
        >
          <CalendarOutlined aria-hidden="true" />
          <span>时间筛选</span>
        </button>
      </Popover>
    </div>
  );
}
