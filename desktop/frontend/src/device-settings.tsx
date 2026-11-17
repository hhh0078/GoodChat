/** 文件作用说明：展示本机屏幕常亮与通知权限，系统授权不与账号业务提醒开关混为一谈。 */
import { useEffect, useState } from "react";
import {
  Alert,
  App,
  Button,
  Divider,
  Drawer,
  Space,
  Spin,
  Switch,
  Tag,
  Typography,
} from "antd";
import { invoke, message } from "./native";

type DeviceState = {
  keep_awake: boolean;
  power_error: string;
  notifications: { allowed: boolean; status: string; description: string };
};

/** DeviceSettings 在用户打开或从系统设置返回时读取真实状态，拒绝时保留系统设置入口。 */
export function DeviceSettings({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const [state, setState] = useState<DeviceState | null>(null);
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const { message: toast } = App.useApp();

  useEffect(() => {
    if (!open) return;
    let disposed = false;
    /** refresh 只在当前抽屉有效时更新，关闭后不回写过期查询。 */
    async function refresh() {
      setLoading(true);
      try {
        const value = await invoke<DeviceState>("DeviceStatus");
        if (!disposed) {
          setState(value);
          setError("");
        }
      } catch (problem) {
        if (!disposed) setError(message(problem));
      } finally {
        if (!disposed) setLoading(false);
      }
    }
    void refresh();
    window.addEventListener("focus", refresh);
    return () => {
      disposed = true;
      window.removeEventListener("focus", refresh);
    };
  }, [open]);

  /** action 等待原生操作后重新读取状态，拒绝授权或保存失败不会显示虚假的成功。 */
  async function action(method: string, ...args: unknown[]) {
    setBusy(true);
    setError("");
    try {
      const result = await invoke(method, ...args);
      if (method === "EnableNotifications" && result !== true)
        setError(
          "系统尚未允许通知。Windows请打开系统设置；Mac被拒绝后也需要在系统设置中开启。",
        );
      if (method === "TestNotification")
        toast.success(
          "测试通知已提交，请查看系统通知中心；专注模式可能暂时静默。",
        );
    } catch (problem) {
      setError(message(problem));
    } finally {
      try {
        setState(await invoke<DeviceState>("DeviceStatus"));
      } catch (problem) {
        setError(message(problem));
      }
      setBusy(false);
    }
  }

  return (
    <Drawer title="客户端设置" open={open} onClose={onClose} size={460}>
      {error && (
        <Alert
          type="error"
          showIcon
          title={error}
          style={{ marginBottom: 20 }}
        />
      )}
      {!state ? (
        <Spin description="正在读取本机状态" />
      ) : (
        <Space orientation="vertical" size={20} style={{ width: "100%" }}>
          <section>
            <Space style={{ width: "100%", justifyContent: "space-between" }}>
              <Typography.Title level={5} style={{ margin: 0 }}>
                保持屏幕常亮
              </Typography.Title>
              <Switch
                aria-label="保持屏幕常亮"
                checked={state.keep_awake}
                loading={busy}
                disabled={loading}
                onChange={(enabled) => void action("SetKeepAwake", enabled)}
              />
            </Space>
            <Typography.Paragraph type="secondary" style={{ marginTop: 12 }}>
              程序运行期间阻止空闲息屏和自动睡眠。关闭窗口后仍保持，彻底退出时释放；下次启动恢复你的选择。
            </Typography.Paragraph>
            <Typography.Text type="secondary">
              手动锁屏、休眠、合盖及系统电源策略仍由系统控制，开启后耗电会增加。
            </Typography.Text>
            {state.power_error && (
              <Alert
                type="warning"
                title={state.power_error}
                style={{ marginTop: 12 }}
              />
            )}
          </section>
          <Divider style={{ margin: 0 }} />
          <section>
            <Space>
              <Typography.Title level={5} style={{ margin: 0 }}>
                系统通知权限
              </Typography.Title>
              <Tag color={state.notifications.allowed ? "green" : "gold"}>
                {state.notifications.allowed
                  ? "已允许"
                  : state.notifications.status === "unknown"
                    ? "待检查"
                    : "未开启"}
              </Tag>
            </Space>
            <Typography.Paragraph style={{ marginTop: 12 }}>
              {state.notifications.description}
            </Typography.Paragraph>
            <Space wrap>
              {!state.notifications.allowed && (
                <Button
                  type="primary"
                  loading={busy}
                  onClick={() => void action("EnableNotifications")}
                >
                  申请通知权限
                </Button>
              )}
              <Button
                disabled={busy}
                onClick={() => void action("OpenNotificationSettings")}
              >
                打开系统设置
              </Button>
              <Button
                disabled={busy}
                onClick={() => void action("DeviceStatus")}
              >
                刷新状态
              </Button>
              <Button
                disabled={busy || !state.notifications.allowed}
                onClick={() => void action("TestNotification")}
              >
                发送测试通知
              </Button>
            </Space>
            <Typography.Paragraph type="secondary" style={{ marginTop: 16 }}>
              授权后，在工作台「消息音量」中勾选「系统通知」，以启用当前账号的后台业务提醒。通知内容不展示聊天正文。
            </Typography.Paragraph>
          </section>
        </Space>
      )}
    </Drawer>
  );
}
