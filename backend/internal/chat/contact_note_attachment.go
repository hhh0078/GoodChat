// 文件作用说明：按当前客户接待权限读取旧内部备注附件，共用私有文件路径校验和流式响应，禁止向访客暴露存储信息。
package chat

import (
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// contactNoteAttachment 只授权当前客户的未撤回内部消息附件，历史接待资格不能单独授予下载权限。
func (s *Service) contactNoteAttachment(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	contact, note := r.PathValue("id"), r.PathValue("note_id")
	if !uuidPattern.MatchString(note) {
		returnError(w, r, "invalid_note", "备注编号不正确")
		return
	}
	var key, name, kind string
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		if err := canSeeContact(r.Context(), tx, a, contact); err != nil {
			return err
		}
		return tx.QueryRow(r.Context(), `SELECT f.storage_key,f.file_name,f.content_type
 FROM messages m JOIN conversations c ON c.id=m.conversation_id AND c.tenant_id=m.tenant_id
 JOIN attachments f ON f.id=m.attachment_id AND f.tenant_id=m.tenant_id
 WHERE c.contact_id=$1 AND m.id=$2 AND m.audience='internal' AND m.withdrawn_at IS NULL`, contact, note).Scan(&key, &name, &kind)
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	s.serveChatAttachment(w, r, key, name, kind)
}

// serveChatAttachment 从固定私有目录流式读取常规文件，共用下载响应头并拒绝路径穿越或符号链接。
// key 为服务端上传生成的UUID存储键，name和kind只用于安全展示头，不参与路径拼接。
func (s *Service) serveChatAttachment(w http.ResponseWriter, r *http.Request, key, name, kind string) {
	if !uuidPattern.MatchString(key) {
		replyError(w, r, platform.Fail(404, "attachment_not_found", "附件不存在或无法读取"))
		return
	}
	path := filepath.Join(s.cfg.DataDir, "chat-attachments", key)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		replyError(w, r, platform.Fail(404, "attachment_not_found", "附件不存在或无法读取"))
		return
	}
	file, err := os.Open(path)
	if err != nil {
		replyError(w, r, platform.Fail(404, "attachment_not_found", "附件不存在或无法读取"))
		return
	}
	defer file.Close()
	disposition := "attachment"
	if strings.HasPrefix(kind, "image/") || strings.HasPrefix(kind, "audio/") || kind == "video/webm" {
		disposition = "inline"
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": name}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, name, info.ModTime(), file)
}
