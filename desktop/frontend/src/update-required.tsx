/** 文件作用说明：强制更新时只允许下载、重新检查和切换服务器，不能进入客服业务。 */
import { useTranslator } from "@/i18n/use-translator";
import { DownloadOutlined, ReloadOutlined } from "@ant-design/icons";
import { Alert, Button } from "antd";
import { invoke } from "./native";
export type Update = {
  current_version: string;
  required: boolean;
  download_url: string;
  release: {
    version: string;
    description: string;
  } | null;
};
/** UpdateRequired 展示实际进程版本，下载新版后要求关闭旧程序再打开。 */
export function UpdateRequired({
  update,
  busy,
  error,
  onCheck,
  onServer,
}: {
  update: Update;
  busy: boolean;
  error: string;
  onCheck: () => void;
  onServer: () => void;
}) {
  const tr = useTranslator();
  return (
    <main className="desktop-entry">
      <section className="desktop-connect-card">
        <div className="desktop-entry-icon">
          <DownloadOutlined />
        </div>
        <span className="desktop-eyebrow">{tr("客户端更新")}</span>
        <h1>{tr("请更新后继续使用")}</h1>
        <p>
          {tr("当前版本 ")}
          {update.current_version}
          {tr(" \u00B7 最新版本 ")}
          {update.release?.version}
        </p>
        <p style={{ whiteSpace: "pre-wrap" }}>{update.release?.description}</p>
        <Alert
          type="warning"
          title={tr("下载并安装新版，关闭当前旧程序后重新打开。")}
        />
        {error && <Alert type="error" title={error} />}
        <Button
          type="primary"
          block
          size="large"
          icon={<DownloadOutlined />}
          onClick={() => void invoke("OpenExternal", update.download_url)}
        >
          {tr("下载最新版本 ")}
        </Button>
        <Button
          block
          className="desktop-return"
          icon={<ReloadOutlined />}
          loading={busy}
          onClick={onCheck}
        >
          {tr("重新检查版本 ")}
        </Button>
        <Button type="text" block onClick={onServer}>
          {tr("切换服务器 ")}
        </Button>
      </section>
    </main>
  );
}
