/** 文件作用说明：校验限定用途退订链接，用户确认后才提交取消，不暴露账号邮箱。 */
"use client";
import { ErrorNotice } from "@/components/page-state";
import { api } from "@/lib/api";
import { useTranslator } from "@/i18n/use-translator";
import { useSearchParams } from "next/navigation";
import { Alert, Button, Card } from "antd";
import { useEffect, useState } from "react";
/** UnsubscribeForm 处理链接校验、明确确认与成功提示，失效链接不执行任何操作。 */
export function UnsubscribeForm() {
  const tr = useTranslator();
  const params = useSearchParams();
  const token = params.get("token") || "";
  const [valid, setValid] = useState(false);
  const [done, setDone] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  useEffect(() => {
    const c = new AbortController();
    setValid(false);
    api("/public/mail-unsubscribe?token=" + encodeURIComponent(token), { signal: c.signal })
      .then(() => {
        if (!c.signal.aborted) setValid(true);
      })
      .catch((e) => {
        if (!c.signal.aborted) setError(e);
      });
    return () => c.abort();
  }, [token]);
  /** unsubscribe 仅在用户明确点击后取消该类别订阅，不改变账号安全邮件。 */
  async function unsubscribe() {
    setBusy(true);
    try {
      await api("/public/mail-unsubscribe?token=" + encodeURIComponent(token), { method: "POST" });
      setDone(true);
    } catch (e) {
      setError(e);
    } finally {
      setBusy(false);
    }
  }
  return (
    <Card style={{ maxWidth: 600, margin: "48px auto", width: "100%" }}>
      <h1>{tr("邮件订阅管理")}</h1>
      <p>{tr("取消此类邮件订阅后，必要的账号安全通知仍会发送。")}</p>
      {Boolean(error) && <ErrorNotice error={error} />}{" "}
      {done ? (
        <Alert type="success" title={tr("已取消此类邮件订阅")} />
      ) : (
        <Button type="primary" disabled={!valid} loading={busy} onClick={unsubscribe}>
          {tr("确认退订")}
        </Button>
      )}
    </Card>
  );
}
