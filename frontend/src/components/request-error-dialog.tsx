/** 文件作用说明：全站请求失败统一使用确认弹窗，禁止遮罩、Esc及自动计时关闭。 */
"use client";
import { useEffect, useState } from "react";
import { Modal, Button, Space } from "antd";
import { useTranslator } from "@/i18n/use-translator";
import {
  acknowledgeRequestError,
  currentRequestError,
  subscribeRequestErrors,
  type RequestFailure,
} from "@/lib/request-error";
/** RequestErrorDialog 确认后才关闭，重试必须由用户主动点击并保留原表单内容。 */
export function RequestErrorDialog() {
  const tr = useTranslator();
  const [failure, setFailure] = useState<RequestFailure>();
  useEffect(() => subscribeRequestErrors(() => setFailure(currentRequestError())), []);
  return (
    <Modal
      open={!!failure}
      title={tr("请求失败")}
      closable={false}
      mask={{ closable: false }}
      keyboard={false}
      zIndex={3000}
      footer={
        <Space wrap>
          {failure?.retry && (
            <Button
              onClick={() => {
                const retry = failure.retry;
                acknowledgeRequestError();
                retry?.();
              }}
            >
              {tr("重试 ")}
            </Button>
          )}
          <Button type="primary" autoFocus onClick={acknowledgeRequestError}>
            {tr("确认")}
          </Button>
        </Space>
      }
    >
      <p style={{ whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>{failure?.content}</p>
    </Modal>
  );
}
