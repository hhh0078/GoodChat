/** 文件作用说明：以两列紧凑展示客户搜索和时间筛选入口，点击后查看与调整日期区间。 */
"use client";
import { useTranslator } from "@/i18n/use-translator";
import type { ConversationPeriod } from "@/lib/conversation-list";
import { CalendarOutlined } from "@ant-design/icons";
import { Button, DatePicker, Input, Popover } from "antd";
import dayjs from "dayjs";
import { useState } from "react";
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
  const tr = useTranslator();
  const [open, setOpen] = useState(false);
  return (
    <div className="conversation-filters">
      <Input.Search
        allowClear
        aria-label={tr("搜索客户")}
        placeholder={tr("姓名 / 手机号")}
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
              aria-label={tr("会话活动日期")}
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
                {tr("按最新活动时间 \u00B7 ")}
                {timezone}
                <br />
                {tr("每日 00:00 至 23:59:59 ")}
              </span>
              <Button
                type="link"
                size="small"
                onClick={() => {
                  onPeriod(period, false);
                  setOpen(false);
                }}
              >
                {tr("最近3天 ")}
              </Button>
            </div>
          </div>
        }
      >
        <button
          className="conversation-period-trigger"
          type="button"
          aria-label={tr("时间筛选")}
          aria-description={tr("当前范围：{p0} 00:00 至 {p1} 23:59:59", {
            p0: String(period.from),
            p1: String(period.to),
          })}
          aria-expanded={open}
        >
          <CalendarOutlined aria-hidden="true" />
          <span>{tr("时间筛选")}</span>
        </button>
      </Popover>
    </div>
  );
}
