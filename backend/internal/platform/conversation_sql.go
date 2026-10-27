// 文件作用说明：集中定义聊天记录与统计使用的来源快照SQL口径，固定咨询表别名为c，不接受客户端SQL片段。
package platform

// ConversationChannelSQL 优先使用咨询创建时tracking中的渠道，缺失或空值回退entry快照，均缺失时返回空字符串。
const ConversationChannelSQL = `COALESCE(NULLIF(c.source_snapshot->'tracking'->>'channel_code',''),NULLIF(c.source_snapshot->'entry'->>'channel_code',''),'')`
