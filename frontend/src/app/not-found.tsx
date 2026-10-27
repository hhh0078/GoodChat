/** 文件作用说明：为不存在的网页提供明确提示与返回入口。 */
import { Result, Button } from "antd";
/** NotFound 提示未知页面并引导返回官网。 */
export default function NotFound() {
  return (
    <Result
      status="404"
      title="这个页面还没有找到"
      subTitle="请检查链接，或者回到首页重新开始。"
      extra={
        <Button type="primary" href="/">
          返回首页
        </Button>
      }
    />
  );
}
