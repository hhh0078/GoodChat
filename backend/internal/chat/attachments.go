// 文件作用说明：保存经实际内容检查的私有附件，并在每次下载时校验租户会话授权。
package chat

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// upload 把附件写入私有存储，数据库提交失败时删除未被引用的文件。
func (s *Service) upload(w http.ResponseWriter, r *http.Request) {
	a := currentActor(r)
	r.Body = http.MaxBytesReader(w, r.Body, 22<<20)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		returnError(w, r, "invalid_upload", "请选择不超过20MB的文件")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		returnError(w, r, "file_required", "请选择文件")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 20<<20+1))
	if err != nil {
		replyError(w, r, err)
		return
	}
	if len(data) == 0 || len(data) > 20<<20 {
		returnError(w, r, "file_too_large", "文件为空或超过20MB")
		return
	}
	kind := http.DetectContentType(data)
	if !allowedMedia(kind) {
		returnError(w, r, "unsupported_file", "暂不支持该文件类型，请使用图片、语音、PDF、文档或压缩文件")
		return
	}
	if strings.HasPrefix(kind, "image/") && len(data) > 10<<20 {
		returnError(w, r, "image_too_large", "图片不能超过10MB")
		return
	}
	id := newID()
	dir := filepath.Join(s.cfg.DataDir, "chat-attachments")
	if err := os.MkdirAll(dir, 0700); err != nil {
		replyError(w, r, err)
		return
	}
	path := filepath.Join(dir, id)
	if err := os.WriteFile(path, data, 0600); err != nil {
		replyError(w, r, err)
		return
	}
	sum := sha256.Sum256(data)
	name := filepath.Base(strings.ReplaceAll(header.Filename, "\\", "/"))
	if len(name) > 240 {
		name = name[:240]
	}
	var result map[string]any
	err = platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		if err := tenantActive(r.Context(), tx, a.TenantID); err != nil {
			return err
		}
		if a.ContactID != "" {
			if err := blocked(r.Context(), tx, a.ContactID); err != nil {
				return err
			}
		} else {
			if err := liveMember(r.Context(), tx, a); err != nil {
				return err
			}
		}
		_, err := tx.Exec(r.Context(), "INSERT INTO attachments(id,tenant_id,uploader_id,uploader_kind,file_name,storage_key,content_type,size_bytes,sha256) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)", id, a.TenantID, actorID(a), actorKind(a), name, id, kind, len(data), hex.EncodeToString(sum[:]))
		if err != nil {
			return err
		}
		result, err = txJSON(r.Context(), tx, "SELECT to_jsonb(a)-'storage_key'-'sha256' FROM attachments a WHERE id=$1", id)
		return err
	})
	if err != nil {
		_ = os.Remove(path)
		replyError(w, r, err)
		return
	}
	result["url"] = "/api/v1/attachments/" + id
	platform.JSON(w, 201, result)
}

// allowedMedia 禁止可执行HTML与SVG，允许受支持的图片、音频和常用文件。
func allowedMedia(kind string) bool {
	return strings.HasPrefix(kind, "image/") && kind != "image/svg+xml" || strings.HasPrefix(kind, "audio/") || inStrings(kind, "video/webm", "video/mp4", "video/quicktime", "application/pdf", "application/zip", "application/x-gzip", "application/x-rar-compressed", "application/vnd.ms-office", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "text/plain; charset=utf-8")
}

// download 从私有目录读取附件，猜测对象编号不能跳过会话可见与撤回检查。
func (s *Service) download(w http.ResponseWriter, r *http.Request) {
	a := currentActor(r)
	id := r.PathValue("id")
	if !uuidPattern.MatchString(id) {
		returnError(w, r, "invalid_attachment", "附件编号不正确")
		return
	}
	var key, name, kind string
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		ctx := r.Context()
		if err := tenantActive(ctx, tx, a.TenantID); err != nil {
			return err
		}
		if a.ContactID != "" {
			if err := blocked(ctx, tx, a.ContactID); err != nil {
				return err
			}
		} else {
			if err := liveMember(ctx, tx, a); err != nil {
				return err
			}
		}
		var uploader string
		if err := tx.QueryRow(ctx, "SELECT storage_key,file_name,content_type,uploader_id::text FROM attachments WHERE id=$1", id).Scan(&key, &name, &kind, &uploader); err != nil {
			return err
		}
		if a.ContactID == "" {
			var visibleReply bool
			if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM quick_replies WHERE attachment_id=$1 AND enabled AND (owner_membership_id IS NULL OR owner_membership_id=$2))", id, a.MemberID).Scan(&visibleReply); err != nil {
				return err
			}
			if visibleReply {
				return nil
			}
			var welcomeVisible bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM welcome_rules w WHERE NOT w.archived AND (w.scope_type='member' AND w.scope_id=$2 OR w.scope_type<>'member' AND w.enabled AND coalesce((SELECT (cs.settings->>'welcome_enabled')::boolean FROM chat_settings cs),true)) AND EXISTS(SELECT 1 FROM jsonb_array_elements(w.contents) content WHERE content->>'type'='image' AND content->>'attachment_id'=$1::text))`, id, a.MemberID).Scan(&welcomeVisible); err != nil {
				return err
			}
			if welcomeVisible {
				return nil
			}
		}
		shared, err := canReadLeadAttachment(ctx, tx, a, id)
		if err != nil {
			return err
		}
		if shared {
			return nil
		}
		rows, err := tx.Query(ctx, "SELECT DISTINCT conversation_id::text FROM messages WHERE attachment_id=$1 AND withdrawn_at IS NULL AND chat_message_visible(NULLIF($2,'')::uuid,NULLIF($3,'')::uuid,$4,id)", id, a.MemberID, a.ContactID, isAdmin(a))
		if err != nil {
			return err
		}
		conversations := []string{}
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				rows.Close()
				return err
			}
			conversations = append(conversations, c)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, c := range conversations {
			if canReadConversation(ctx, tx, a, c) == nil {
				return nil
			}
		}
		if len(conversations) == 0 && uploader == actorID(a) {
			var linked bool
			if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM messages WHERE attachment_id=$1)", id).Scan(&linked); err != nil {
				return err
			}
			if !linked {
				return nil
			}
		}
		return platform.Fail(404, "attachment_not_found", "附件不存在、已撤回或无权下载")
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	s.serveChatAttachment(w, r, key, name, kind)
}
