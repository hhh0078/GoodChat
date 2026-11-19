/** 文件作用说明：以真实累计消费、额度状态和触底明细展示AI用量，保持整数微元及成员权限。 */
"use client";
import { LoadMore } from "@/components/load-more";
import { EmptyData, ErrorNotice } from "@/components/page-state";
import { useFormats } from "@/i18n/use-formats";
import { useTranslator } from "@/i18n/use-translator";
import { usePagedResource } from "@/lib/use-paged-resource";
import { Alert, Button, Tabs, Tag } from "antd";
import { useState } from "react";
import type { AIJob, AIUsageData, AIWalletEntry } from "./types";
import "./usage.css";

/** AIUsagePage 先展示全量已结算消费，再按需读取任务或仅管理员可见的余额流水。 */
export function AIUsagePage() {
  const tr = useTranslator();
  const { numberFormatter, dateTimeFormatter } = useFormats();
  const [tab, setTab] = useState("jobs");
  const usage = usePagedResource<AIJob, AIUsageData>("/tenant/ai/usage");
  const data = usage.data;
  const wallet = data?.wallet;
  const ledger = usePagedResource<AIWalletEntry>("/tenant/ai/ledger", !!wallet && tab === "ledger");
  /** money 以人民币展示微元，保留小额账单而不将已扣费记录四舍五入为零。 */
  function money(value: number) {
    return numberFormatter({
      style: "currency",
      currency: "CNY",
      minimumFractionDigits: 2,
      maximumFractionDigits: 6,
    }).format(value / 1000000);
  }
  /** time 显示个人时区时间，缺失或无效值保留占位。 */
  function time(value: string) {
    return Number.isFinite(new Date(value).getTime())
      ? dateTimeFormatter({ dateStyle: "short", timeStyle: "short" }).format(new Date(value))
      : "—";
  }
  const statuses: Record<string, string> = {
    queued: tr("等待处理"),
    running: tr("生成中"),
    draft: tr("待人工确认"),
    succeeded: tr("已完成"),
    failed: tr("失败"),
    cancelled: tr("已取消"),
    uncertain: tr("用量待核对"),
  };
  const kinds: Record<string, string> = {
    summary: tr("知识库描述"),
    restore: tr("知识库回滚"),
    reply: tr("客服回复"),
    test: tr("调试"),
  };
  const entryKinds: Record<string, string> = {
    topup: tr("充值"),
    charge: tr("消费"),
    adjustment: tr("核对调整"),
  };
  /** refresh 同时刷新概览及当前打开的账本，避免显示两组不同时间的数据。 */
  const refresh = () => {
    usage.reload();
    if (tab === "ledger") ledger.reload();
  };
  return (
    <div className="ai-resource ai-usage">
      <div className="ai-usage-heading">
        <div>
          <h2>{tr("用量与账单")}</h2>
          <p>{wallet ? tr("租户全部用量") : tr("我的AI用量")}</p>
        </div>
        <Button onClick={refresh} loading={usage.loading && !!data}>
          {tr("刷新")}
        </Button>
      </div>
      <ErrorNotice error={usage.error} retry={usage.items.length ? usage.loadMore : usage.reload} />
      <section
        className={`ai-usage-overview${wallet ? "" : " ai-usage-personal"}`}
        aria-label={tr("用量概览")}
      >
        <div className="ai-usage-metric ai-usage-total">
          <span>{tr("累计消费")}</span>
          <strong>{data?.summary ? money(data.summary.charged_micro) : "—"}</strong>
          <small>{tr("已扣费用")}</small>
        </div>
        {wallet && (
          <>
            <div className="ai-usage-metric">
              <span>{tr("当前可用余额")}</span>
              <strong>
                {wallet
                  ? wallet.unlimited
                    ? tr("不限制")
                    : money(wallet.balance_micro - wallet.reserved_micro)
                  : "—"}
              </strong>
              <small>
                {wallet
                  ? `${tr("AI账面余额")} ${money(wallet.balance_micro)}`
                  : tr("仅统计本人用量")}
              </small>
            </div>
            <div className="ai-usage-metric">
              <span>{tr("预算预占")}</span>
              <strong>{wallet ? money(wallet.reserved_micro) : "—"}</strong>
              <small>{tr("任务预留费用")}</small>
            </div>
          </>
        )}
        <div className="ai-usage-metric">
          <span>{tr("AI调用次数")}</span>
          <strong>{data?.summary ? numberFormatter().format(data.summary.calls) : "—"}</strong>
          <small>{tr("包含调试与辅助回复")}</small>
        </div>
      </section>
      {!!data?.summary?.uncertain && (
        <Alert type="warning" showIcon title={tr("存在用量待核对任务，请联系平台处理。")} />
      )}
      <details className="ai-billing-help">
        <summary>
          {tr("计费说明")}
          {wallet?.unlimited && <Tag>{tr("不限制")}</Tag>}
        </summary>
        {wallet?.unlimited && <p>{tr("当前不限额，费用仍按实际用量记录。")}</p>}
        <p>
          {tr(
            "输入Token包含文字、知识库、上下文及图片；图片按供应商返回的实际输入用量计费，不按上传张数额外收费。",
          )}
        </p>
        <p>
          {tr("知识库描述、调试和辅助回复均产生AI用量。待核对任务保留预算，核对前不会猜测扣费。")}
        </p>
      </details>
      <Tabs
        activeKey={tab}
        onChange={setTab}
        items={[
          {
            key: "jobs",
            label: tr("用量明细"),
            children: (
              <>
                <div className="ai-usage-records" aria-busy={usage.loading}>
                  {usage.items.map((job) => (
                    <article
                      key={job.id}
                      className="ai-usage-record"
                      aria-label={kinds[job.kind] || job.kind}
                    >
                      <div className="ai-usage-record-top">
                        <div>
                          <strong>{kinds[job.kind] || job.kind}</strong>
                          <Tag
                            color={
                              job.status === "uncertain" || job.status === "failed"
                                ? "warning"
                                : "default"
                            }
                          >
                            {statuses[job.status] || job.status}
                          </Tag>
                        </div>
                        <strong className="ai-record-amount">
                          {money(job.charged_micro || 0)}
                        </strong>
                      </div>
                      <div className="ai-record-meta">
                        <time dateTime={job.created_at}>{time(job.created_at)}</time>
                        {job.owner_name && <span>{job.owner_name}</span>}
                      </div>
                      <details>
                        <summary>{tr("用量详情")}</summary>
                        <dl className="ai-token-details">
                          <div>
                            <dt>{tr("输入Token")}</dt>
                            <dd>{numberFormatter().format(job.input_tokens)}</dd>
                          </div>
                          <div>
                            <dt>{tr("输出Token")}</dt>
                            <dd>{numberFormatter().format(job.output_tokens)}</dd>
                          </div>
                          <div>
                            <dt>{tr("图片输入次数")}</dt>
                            <dd>{numberFormatter().format(job.image_inputs)}</dd>
                          </div>
                        </dl>
                      </details>
                    </article>
                  ))}
                </div>
                {!usage.loading && !usage.error && !usage.items.length && (
                  <EmptyData text={tr("暂无数据")} />
                )}
                <LoadMore
                  loading={usage.loading}
                  hasMore={usage.hasMore}
                  error={usage.error}
                  onLoad={usage.loadMore}
                />
              </>
            ),
          },
          ...(wallet
            ? [
                {
                  key: "ledger",
                  label: tr("余额流水"),
                  children: (
                    <>
                      <ErrorNotice
                        error={ledger.error}
                        retry={ledger.items.length ? ledger.loadMore : ledger.reload}
                      />
                      <div className="ai-usage-records" aria-busy={ledger.loading}>
                        {ledger.items.map((entry) => (
                          <article key={entry.id} className="ai-usage-record">
                            <div className="ai-usage-record-top">
                              <strong>{entryKinds[entry.kind] || entry.kind}</strong>
                              <strong className="ai-record-amount">
                                {entry.delta_micro > 0 ? "+" : ""}
                                {money(entry.delta_micro)}
                              </strong>
                            </div>
                            <div className="ai-record-meta">
                              <span>{kinds[entry.reason] || entry.reason}</span>
                              <time dateTime={entry.created_at}>{time(entry.created_at)}</time>
                            </div>
                          </article>
                        ))}
                      </div>
                      {!ledger.loading && !ledger.error && !ledger.items.length && (
                        <EmptyData text={tr("暂无数据")} />
                      )}
                      <LoadMore
                        loading={ledger.loading}
                        hasMore={ledger.hasMore}
                        error={ledger.error}
                        onLoad={ledger.loadMore}
                      />
                    </>
                  ),
                },
              ]
            : []),
        ]}
      />
    </div>
  );
}
