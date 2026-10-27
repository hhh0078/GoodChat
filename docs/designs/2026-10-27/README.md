<!-- 文件作用说明：集中索引GoodChat本轮54个主画面、7个移动变体及分组manifest，记录设计资产映射和待补项。 -->
# GoodChat 全站设计图索引

更新日期：2026-10-27。本轮范围为53个业务主画面、1个404画面，加7个移动变体，共61个设计目标。标签页单独计作画面，不一定拥有独立URL。

用户已选最近一轮第1张的布局，并明确要求“颜色点缀就行，也别用深色造成视觉冲突太强”。当前采用[明亮工作台修订图](13-workspace.png)：白色与中性浅灰基底、炭灰正文、绿色仅点缀选中态、主操作和状态。上一轮深色方向图已经弃用，不作为本次开发目标。共同约束见[设计方向](设计方向.md)。

本索引记录图稿资产及页面映射；本轮集成检查见[设计对照验收](../../../design-qa.md)。图稿生成、代码实现、浏览器交互检查和同尺寸视觉验收需要分别确认。图中的样例姓名、计数、时间、内容和地址不会自动成为业务数据。实际页面以真实API、权限与已支持能力为准。

## 分组清单

| 分组 | 主画面 | 移动画面 | 生成记录与说明 |
| --- | --- | --- | --- |
| 官网、认证、访客、404 | 13 | 5 | [public-manifest.json](public-manifest.json) |
| 租户后台 | 19 | 0 | [console-manifest.json](console-manifest.json)、[实现与边界说明](租户后台实现说明.md) |
| 装修、运营、平台管理 | 21 | 0 | [装修运营平台设计清单](装修运营平台设计清单.md) |
| 共享工作台 | 1 | 2 | [工作台设计图](13-workspace.png)、[生成提示词](13-workspace.prompt.txt)；移动变体见下表 |

manifest记录生成来源和提示词，不应当作业务验收报告。公开页面中的隐私、条款与留言图已按真实文案、字段修正，实际业务文案仍以源代码为准。

## 54个主画面

| 编号 | 页面或标签画面 | 路由或状态 | 当前设计图 | 资产状态 |
| --- | --- | --- | --- | --- |
| 01 | 官网首页 | `/` | [01-home.png](01-home.png) | PNG已生成；实现与验收待独立核对 |
| 02 | 产品功能 | `/features` | [02-features.png](02-features.png) | PNG已生成；实现与验收待独立核对 |
| 03 | 使用场景 | `/solutions` | [03-solutions.png](03-solutions.png) | PNG已生成；实现与验收待独立核对 |
| 04 | 新手帮助 | `/help` | [04-help.png](04-help.png) | PNG已生成；实现与验收待独立核对 |
| 05 | 隐私政策 | `/privacy` | [05-privacy.png](05-privacy.png) | PNG已生成；实现与验收待独立核对 |
| 06 | 服务条款 | `/terms` | [06-terms.png](06-terms.png) | PNG已生成；实现与验收待独立核对 |
| 07 | 定价 | `/pricing` | [07-pricing.png](07-pricing.png) | PNG已生成；实现与验收待独立核对 |
| 08 | 登录 | `/login` | [08-login.png](08-login.png) | PNG已生成；实现与验收待独立核对 |
| 09 | 注册 | `/register` | [09-register.png](09-register.png) | PNG已生成；实现与验收待独立核对 |
| 10 | 重置密码 | `/reset-password` | [10-reset-password.png](10-reset-password.png) | PNG已生成；实现与验收待独立核对 |
| 11 | 接受邀请 | `/accept-invitation` | [11-invitation.png](11-invitation.png) | PNG已生成；实现与验收待独立核对 |
| 12 | 租户概览 | `/console` | [12-dashboard.png](12-dashboard.png) | PNG已生成；实现与验收待独立核对 |
| 13 | 客服工作台 | `/workspace` | [13-workspace.png](13-workspace.png) | PNG已生成；实现与验收待独立核对 |
| 14 | 团队成员 | `/console/members · members` | [14-members.png](14-members.png) | PNG已生成；实现与验收待独立核对 |
| 15 | 邀请记录 | `/console/members · invitations` | [15-invitations.png](15-invitations.png) | PNG已生成；实现与验收待独立核对 |
| 16 | 接入链接 | `/console/links` | [16-access-links.png](16-access-links.png) | PNG已生成；实现与验收待独立核对 |
| 17 | 业务组 | `/console/routing · groups` | [17-service-groups.png](17-service-groups.png) | PNG已生成；实现与验收待独立核对 |
| 18 | 分配池 | `/console/routing · pools` | [18-routing-pools.png](18-routing-pools.png) | PNG已生成；实现与验收待独立核对 |
| 19 | 技能字典 | `/console/routing · skills` | [19-routing-skills.png](19-routing-skills.png) | PNG已生成；实现与验收待独立核对 |
| 20 | 客服技能 | `/console/routing · agent-skills` | [20-agent-skills.png](20-agent-skills.png) | PNG已生成；实现与验收待独立核对 |
| 21 | 客户档案 | `/console/contacts · contacts` | [21-contacts.png](21-contacts.png) | PNG已生成；实现与验收待独立核对 |
| 22 | 客户分类 | `/console/contacts · categories` | [22-contact-categories.png](22-contact-categories.png) | PNG已生成；实现与验收待独立核对 |
| 23 | 客户标签 | `/console/contacts · tags` | [23-contact-tags.png](23-contact-tags.png) | PNG已生成；实现与验收待独立核对 |
| 24 | 标签分类 | `/console/contacts · tag-categories` | [24-tag-categories.png](24-tag-categories.png) | PNG已生成；实现与验收待独立核对 |
| 25 | 自定义字段 | `/console/contacts · fields` | [25-custom-fields.png](25-custom-fields.png) | PNG已生成；实现与验收待独立核对 |
| 26 | 我的模板 | `/console/templates · mine` | [26-my-templates.png](26-my-templates.png) | PNG已生成；实现与验收待独立核对 |
| 27 | 平台模板 | `/console/templates · platform` | [27-platform-templates.png](27-platform-templates.png) | PNG已生成；实现与验收待独立核对 |
| 28 | 私有资源包 | `/console/templates · packages` | [28-private-packages.png](28-private-packages.png) | PNG已生成；实现与验收待独立核对 |
| 29 | AI创作记录 | `/console/templates · ai` | [29-ai-jobs.png](29-ai-jobs.png) | PNG已生成；实现与验收待独立核对 |
| 30 | 装修页面与发布 | `/console/templates · pages` | [30-pages-publishing.png](30-pages-publishing.png) | PNG已生成；实现与验收待独立核对 |
| 31 | 模板编辑器 | `/console/templates/[id]` | [31-template-editor.png](31-template-editor.png) | PNG已生成；实现与验收待独立核对 |
| 32 | 页面编辑器 | `/console/pages/[id]` | [32-page-editor.png](32-page-editor.png) | PNG已生成；实现与验收待独立核对 |
| 33 | 服务与欢迎语 | `/console/automation · settings` | [33-welcome-settings.png](33-welcome-settings.png) | PNG已生成；实现与验收待独立核对 |
| 34 | 高级欢迎规则 | `/console/automation · welcome-rules` | [34-welcome-rules.png](34-welcome-rules.png) | PNG已生成；实现与验收待独立核对 |
| 35 | 营业与评价策略 | `/console/automation · policy` | [35-business-evaluation-policy.png](35-business-evaluation-policy.png) | PNG已生成；实现与验收待独立核对 |
| 36 | 快捷话术 | `/console/automation · replies` | [36-quick-replies.png](36-quick-replies.png) | PNG已生成；实现与验收待独立核对 |
| 37 | 关键词回复 | `/console/automation · keywords` | [37-keyword-replies.png](37-keyword-replies.png) | PNG已生成；实现与验收待独立核对 |
| 38 | 离线留言 | `/console/automation · leads` | [38-offline-leads.png](38-offline-leads.png) | PNG已生成；实现与验收待独立核对 |
| 39 | 留言邮件提醒 | `/console/automation · lead-notifications` | [39-lead-email-notifications.png](39-lead-email-notifications.png) | PNG已生成；实现与验收待独立核对 |
| 40 | 服务评价 | `/console/automation · evaluations` | [40-service-evaluations.png](40-service-evaluations.png) | PNG已生成；实现与验收待独立核对 |
| 41 | 消息提醒 | `/console/automation · notifications` | [41-message-notifications.png](41-message-notifications.png) | PNG已生成；实现与验收待独立核对 |
| 42 | 聊天记录 | `/console/records` | [42-chat-records.png](42-chat-records.png) | PNG已生成；实现与验收待独立核对 |
| 43 | 服务统计 | `/console/reports` | [43-service-reports.png](43-service-reports.png) | PNG已生成；实现与验收待独立核对 |
| 44 | 套餐与坐席 | `/console/billing` | [44-billing.png](44-billing.png) | PNG已生成；实现与验收待独立核对 |
| 45 | 访客进入回调 | `/console/settings · callbacks` | [45-callback-settings.png](45-callback-settings.png) | PNG已生成；实现与验收待独立核对 |
| 46 | 回调投递日志 | `/console/settings · deliveries` | [46-callback-deliveries.png](46-callback-deliveries.png) | PNG已生成；实现与验收待独立核对 |
| 47 | 账号与设备 | `/console/settings · account` | [47-account-devices.png](47-account-devices.png) | PNG已生成；实现与验收待独立核对 |
| 48 | 平台概览 | `/platform · overview` | [48-platform-overview.png](48-platform-overview.png) | PNG已生成；实现与验收待独立核对 |
| 49 | 租户运营 | `/platform · tenants` | [49-platform-tenants.png](49-platform-tenants.png) | PNG已生成；实现与验收待独立核对 |
| 50 | 公共装修模板 | `/platform · templates` | [50-platform-public-templates.png](50-platform-public-templates.png) | PNG已生成；实现与验收待独立核对 |
| 51 | 套餐版本与官网价格 | `/platform · plans` | [51-platform-plans.png](51-platform-plans.png) | PNG已生成；实现与验收待独立核对 |
| 52 | 升级申请与人工开通 | `/platform · upgrades` | [52-platform-upgrades.png](52-platform-upgrades.png) | PNG已生成；实现与验收待独立核对 |
| 53 | 访客聊天 | `/c/[entryKey]` | [53-visitor.png](53-visitor.png) | PNG已生成；实现与验收待独立核对 |
| 54 | 404 | `未知地址` | [54-not-found.png](54-not-found.png) | PNG已生成；实现与验收待独立核对 |

## 7个移动变体

移动图目标画布为390×844，仅包含网页内容；实际导出尺寸以PNG文件为准。移动图并非完整覆盖全部后台页面、软键盘、录音和弹窗状态的证明。

| 编号 | 画面 | 对应页面或状态 | 当前设计图 | 资产状态 |
| --- | --- | --- | --- | --- |
| m01 | 官网首页 | `/` | [m01-home.png](m01-home.png) | PNG已生成；实现与验收待独立核对 |
| m02 | 邮箱登录 | `/login` | [m02-login.png](m02-login.png) | PNG已生成；实现与验收待独立核对 |
| m03 | 访客聊天 | `/c/[entryKey]` | [m03-visitor.png](m03-visitor.png) | PNG已生成；实现与验收待独立核对 |
| m04 | 访客留言 | `访客聊天内留言抽屉` | [m04-visitor-lead.png](m04-visitor-lead.png) | PNG已生成；实现与验收待独立核对 |
| m05 | 访客评价 | `访客聊天内评价抽屉` | [m05-visitor-rating.png](m05-visitor-rating.png) | PNG已生成；实现与验收待独立核对 |
| m06 | 客服咨询列表 | `/workspace · 列表` | [m06-workspace-list.png](m06-workspace-list.png) | PNG已生成；实现与验收待独立核对 |
| m07 | 客服聊天 | `/workspace · 当前咨询` | [m07-workspace-chat.png](m07-workspace-chat.png) | PNG已生成；实现与验收待独立核对 |

## 审阅时需要核对

- 每页先核对导航、标题、信息区块和主操作，再核对筛选、表格、抽屉和状态反馈。
- 图中的虚构值、误生成的排序图标、浏览路径、渠道、逐次投递记录或额外表单字段，需与实际能力对应，不能据此捏造业务实现。
- 租户与成员授权、接待转移、内部备注、邮箱投递、AI任务、套餐价格和发布流程仍由真实后端约束。
- 图稿/页面的同尺寸比较及主要交互验证由本任务集成验收统一补充。具体通过范围与未覆盖状态以根设计对照报告为准，不能从图稿存在推导业务验收。

本次写入索引时检查：54/54个主画面PNG已存在，7/7个移动PNG已存在，合计61个；装修、运营与平台的生成清单也已补齐。
