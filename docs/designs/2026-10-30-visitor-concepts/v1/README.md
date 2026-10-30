<!-- 文件作用说明：索引七个行业的手机与电脑首轮设计稿、实际生成提示词和当前视觉审批状态。 -->
# 七行业访客模板设计稿 v1

2026-10-30，使用内置 ImageGen 生成。没有调用租户配置的 Qwen 接口。本轮是设计稿评审，尚未根据这些图片修改页面，也尚未合并本轮功能分支到 dev。

每张图是同一个行业概念的电脑与手机对应布局。以下序号按主对话实际展示顺序记录，后续选图以此为准。

| 展示序号 | 行业 | 设计图 | 实际提示词 |
| --- | --- | --- | --- |
| 1 | 金融 | [finance.png](finance.png) | [finance.prompt.md](finance.prompt.md) |
| 2 | 医疗 | [medical.png](medical.png) | [medical.prompt.md](medical.prompt.md) |
| 3 | 房产 | [property.png](property.png) | [property.prompt.md](property.prompt.md) |
| 4 | 企业服务 | [business.png](business.png) | [business.prompt.md](business.prompt.md) |
| 5 | 电竞 | [gaming.png](gaming.png) | [gaming.prompt.md](gaming.prompt.md) |
| 6 | 电商 | [commerce.png](commerce.png) | [commerce.prompt.md](commerce.prompt.md) |
| 7 | 教育 | [education.png](education.png) | [education.prompt.md](education.prompt.md) |

共同要求：优先聊天，真实人物或场景图片参与构图；去除套娃卡片与无意义大附件占位图；保留留言、附件、输入发送和 GoodChat 底部技术支持。手机与电脑采用对应重排，而非拉伸同一张网页。

两张用户附件均实际提供给各次 ImageGen：最初金融通道作为场景正向参考；最新被否定页面作为反例。教育图经过一次定向修订，移除手机标题栏拥挤副标题。其他六套一次生成后原样保存，无裁切、拉伸或拼接。

这些是概念设计板，不是浏览器尺寸验收截图。生成器未严格遵守提示词中的所有画板尺寸；后续选定后按真实手机、电脑与短视口重新实现并逐套验收。业务文案和图片仍可按租户修改，设计图不新增订单、楼盘或医疗业务接口。
