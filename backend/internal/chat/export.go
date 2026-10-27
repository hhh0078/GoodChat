// 文件作用说明：仅为具备export权益的租户管理员导出授权聊天CSV；元数据与正文分开，默认排除内部备注，最多10000行和32MiB，超限整份拒绝。
package chat

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

const maxExportRows = 10000
const maxExportBytes = 32 << 20

const exportMetadataColumns = `c.id::text,u.id::text,u.display_name,u.phone,c.status,COALESCE(c.current_agent_id::text,''),COALESCE(a.display_name,''),` + platform.ConversationChannelSQL + `,c.created_at,c.closed_at`
const exportMessageColumns = `,msg.id::text,msg.event_seq::text,msg.sender_kind,
 CASE WHEN msg.sender_kind='visitor' THEN COALESCE((SELECT display_name FROM contacts WHERE id=msg.sender_id),'访客') WHEN msg.sender_kind='system' THEN '系统' ELSE COALESCE((SELECT COALESCE(NULLIF(tm.display_name,''),usr.display_name) FROM tenant_memberships tm JOIN users usr ON usr.id=tm.user_id WHERE tm.id=msg.sender_id),'客服') END,
 msg.message_type,msg.audience,CASE WHEN msg.withdrawn_at IS NULL THEN msg.content ELSE '' END,
 CASE WHEN msg.withdrawn_at IS NULL THEN COALESCE(f.file_name,'') ELSE '' END,
 msg.withdrawn_at IS NOT NULL,msg.created_at`

// exportFlag 只接受显式true或false，避免把拼写错误误认为允许导出内部内容。
func exportFlag(r *http.Request, key string) (bool, error) {
	value := r.URL.Query().Get(key)
	if value == "" || value == "false" {
		return false, nil
	}
	if value == "true" {
		return true, nil
	}
	return false, platform.Fail(400, "invalid_export_option", "导出选项只能为true或false")
}

// csvSafeCell 将可能被电子表格执行的单元格前置文本标记，保留CSV引号转义与多行正文。
func csvSafeCell(value string) string {
	trimmed := strings.TrimLeftFunc(value, func(r rune) bool { return unicode.IsSpace(r) || r < 0x20 })
	if strings.HasPrefix(value, "\t") || strings.HasPrefix(value, "\r") || strings.HasPrefix(value, "\n") || strings.HasPrefix(value, "\x00") {
		return "'" + value
	}
	if trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return "'" + value
	}
	return value
}

// csvValue 按租户时区和显式时差格式化时间，空时间保持空白，所有最终文本统一防公式注入。
func csvValue(value any, location *time.Location) string {
	if value == nil {
		return ""
	}
	if stamp, ok := value.(time.Time); ok {
		return stamp.In(location).Format(time.RFC3339Nano)
	}
	return csvSafeCell(fmt.Sprint(value))
}

// renderExportCSV 使用统一查询生成完整CSV，异步任务可按实际行数报告进度及检查取消。
func renderExportCSV(ctx context.Context, tx pgx.Tx, r *http.Request, a actor, rowLimit, byteLimit int, progress func(int) error) ([]byte, int, string, error) {
	messages, err := exportFlag(r, "include_messages")
	if err != nil {
		return nil, 0, "", err
	}
	internal, err := exportFlag(r, "include_internal")
	if err != nil {
		return nil, 0, "", err
	}
	if internal && !messages {
		return nil, 0, "", platform.Fail(400, "invalid_export_option", "内部备注选项仅适用于消息正文导出")
	}
	args, zone, err := conversationFilterArgs(ctx, tx, r, a)
	if err != nil {
		return nil, 0, "", err
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		return nil, 0, "", platform.Fail(400, "invalid_timezone", "租户时区配置不正确")
	}
	headers := []string{"会话编号", "客户编号", "客户名称", "客户电话", "会话状态", "当前客服编号", "当前客服名称", "来源渠道", "会话创建时间（" + zone + "）", "会话结束时间（" + zone + "）"}
	columns, joins, where, order := exportMetadataColumns, conversationJoins, conversationWhere, " ORDER BY c.updated_at DESC,c.id"
	if messages {
		columns += exportMessageColumns
		joins += " JOIN messages msg ON msg.conversation_id=c.id AND msg.tenant_id=c.tenant_id LEFT JOIN attachments f ON f.id=msg.attachment_id AND f.tenant_id=msg.tenant_id"
		if !internal {
			where += " AND msg.audience='public'"
		}
		order += ",msg.event_seq"
		headers = append(headers, "消息编号", "消息序号", "发送者类型", "发送者名称", "消息类型", "可见范围", "消息正文", "附件名称", "是否已撤回", "消息发送时间（"+zone+"）")
	}
	rows, err := tx.Query(ctx, "SELECT "+columns+joins+where+order+" LIMIT "+strconv.Itoa(rowLimit+1), args...)
	if err != nil {
		return nil, 0, "", err
	}
	defer rows.Close()
	var buffer bytes.Buffer
	buffer.WriteString("\xef\xbb\xbf")
	writer := csv.NewWriter(&buffer)
	if err = writer.Write(headers); err != nil {
		return nil, 0, "", err
	}
	count := 0
	for rows.Next() {
		count++
		if count > rowLimit {
			return nil, 0, "", platform.Fail(409, "export_limit_exceeded", fmt.Sprintf("导出最多%d条数据，请缩小筛选范围；未生成截断文件", rowLimit))
		}
		values, e := rows.Values()
		if e != nil {
			return nil, 0, "", e
		}
		record := make([]string, len(values))
		for index, value := range values {
			record[index] = csvValue(value, location)
		}
		if e = writer.Write(record); e != nil {
			return nil, 0, "", e
		}
		writer.Flush()
		if e = writer.Error(); e != nil {
			return nil, 0, "", e
		}
		if buffer.Len() > byteLimit {
			return nil, 0, "", platform.Fail(409, "export_size_exceeded", fmt.Sprintf("CSV最多%dMiB，请缩小筛选范围；未生成截断文件", byteLimit>>20))
		}
		if progress != nil && count%250 == 0 {
			if e = progress(count); e != nil {
				return nil, 0, "", e
			}
		}
	}
	if err = rows.Err(); err != nil {
		return nil, 0, "", err
	}
	rows.Close()
	writer.Flush()
	if err = writer.Error(); err != nil {
		return nil, 0, "", err
	}
	if progress != nil {
		if err = progress(count); err != nil {
			return nil, 0, "", err
		}
	}
	return buffer.Bytes(), count, zone, nil
}

// exportRecords 同步生成完整CSV并审计，所有失败与超限均不输出半份文件。
func (s *Service) exportRecords(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	if !isAdmin(a) {
		replyError(w, r, platform.Fail(403, "admin_required", "导出聊天记录需要租户管理员权限"))
		return
	}
	var data []byte
	var count int
	var zone string
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		if err := liveMember(r.Context(), tx, a); err != nil {
			return err
		}
		if err := s.checkFeature(r.Context(), tx, a.TenantID, "export"); err != nil {
			return err
		}
		var err error
		data, count, zone, err = renderExportCSV(r.Context(), tx, r, a, maxExportRows, maxExportBytes, nil)
		if err != nil {
			return err
		}
		return audit(r.Context(), tx, a, "chat_records.exported", "", map[string]any{"include_messages": r.URL.Query().Get("include_messages") == "true", "include_internal": r.URL.Query().Get("include_internal") == "true", "rows": count, "timezone": zone, "filter_hash": hash(r.URL.Query().Encode()), "row_limit": maxExportRows, "byte_limit": maxExportBytes})
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	kind := "conversations"
	if r.URL.Query().Get("include_messages") == "true" {
		kind = "messages"
	}
	writeCSVDownload(w, data, count, zone, kind, maxExportRows, maxExportBytes)
}

// writeCSVDownload 仅对已完整生成且通过授权的CSV发出标准下载头。
func writeCSVDownload(w http.ResponseWriter, data []byte, count int, zone, kind string, rowLimit, byteLimit int) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="GoodChat-`+kind+`-`+time.Now().UTC().Format("20060102-150405")+`.csv"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Export-Row-Count", strconv.Itoa(count))
	w.Header().Set("X-Export-Row-Limit", strconv.Itoa(rowLimit))
	w.Header().Set("X-Export-Byte-Limit", strconv.Itoa(byteLimit))
	w.Header().Set("X-Export-Timezone", zone)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
