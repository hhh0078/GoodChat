/** 文件作用说明：提供可索引的客户端下载页，实时展示平台管理员发布的各系统版本。 */
import { ClientDownloads } from "@/features/downloads/client-downloads";
import { publicMetadata } from "@/i18n/metadata";
import type { Metadata } from "next";
/** generateMetadata 输出当前语言的官网标题和替代地址。 */
export async function generateMetadata(): Promise<Metadata> {
  return publicMetadata(
    "/downloads",
    "客户端下载",
    "下载GoodChat Mac和Windows客服端，在桌面程序中接待客户并接收消息提醒。",
  );
}
/** DownloadsPage 提供独立官网入口，未提供的APK不会显示下载按钮。 */
export default function DownloadsPage() {
  return <ClientDownloads />;
}
