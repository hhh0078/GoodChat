// 文件作用说明：持久化幂等文字与附件消息，提供受众隔离、引用、撤回和已读。
package chat

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

var messageProjection = "to_jsonb(m)||jsonb_build_object('seq',m.event_seq::text,'event_seq',m.event_seq::text,'quick_reply_version',m.quick_reply_version::text,'options',CASE WHEN m.withdrawn_at IS NULL THEN m.options ELSE '[]'::jsonb END,'content',CASE WHEN m.withdrawn_at IS NULL THEN m.content ELSE '' END,'attachment',CASE WHEN m.withdrawn_at IS NULL THEN (SELECT to_jsonb(f)-'storage_key'-'sha256' FROM attachments f WHERE f.id=m.attachment_id AND f.tenant_id=m.tenant_id) ELSE NULL END,'sender_name',CASE WHEN m.sender_kind='system' THEN '系统' WHEN m.sender_kind='visitor' THEN (SELECT display_name FROM contacts WHERE id=m.sender_id) ELSE (SELECT u.display_name FROM tenant_memberships tm JOIN users u ON u.id=tm.user_id WHERE tm.id=m.sender_id AND tm.tenant_id=m.tenant_id) END,'sender_avatar_url',CASE WHEN m.sender_kind='visitor' THEN coalesce((SELECT avatar_url FROM contacts WHERE id=m.sender_id),'') WHEN m.sender_kind='agent' THEN coalesce((SELECT " + platform.AvatarURLSQL("tm") + " FROM tenant_memberships tm WHERE tm.id=m.sender_id AND tm.tenant_id=m.tenant_id),'') ELSE '' END,'reply',CASE WHEN m.reply_to_id IS NULL THEN NULL ELSE (SELECT jsonb_build_object('id',q.id,'content',CASE WHEN q.withdrawn_at IS NULL THEN left(q.content,200) ELSE '' END,'withdrawn',q.withdrawn_at IS NOT NULL) FROM messages q WHERE q.id=m.reply_to_id AND q.tenant_id=m.tenant_id) END)"

// messages 读取历史或提交新消息，接收成功只在事务提交之后返回。
func (s *Service) messages(w http.ResponseWriter, r *http.Request) {
	a := currentActor(r)
	id := r.PathValue("id")
	if r.Method == http.MethodGet {
		s.messageHistory(w, r, a, id)
		return
	}
	var in messageInput
	if err := platform.Decode(w, r, &in); err != nil {
		replyError(w, r, err)
		return
	}
	if in.Type == "" {
		in.Type = "text"
	}
	if in.Audience == "" {
		in.Audience = "public"
	}
	if !uuidPattern.MatchString(in.ClientID) || !inStrings(in.Type, "text", "image", "file", "audio") || !inStrings(in.Audience, "public", "internal") || len([]rune(in.Content)) > 10000 {
		returnError(w, r, "invalid_message", "消息类型、编号或长度不正确")
		return
	}
	if a.ContactID != "" && in.Audience != "public" {
		returnError(w, r, "invalid_audience", "访客不能发送内部备注")
		return
	}
	if in.QuickReplyID != "" {
		version, err := strconv.ParseInt(in.QuickReplyVersion, 10, 64)
		if !uuidPattern.MatchString(in.QuickReplyID) || err != nil || version < 1 {
			returnError(w, r, "invalid_quick_reply", "请选择有效话术与版本")
			return
		}
		in.QuickReplyVersion = strconv.FormatInt(version, 10)
		if a.ContactID != "" || in.Audience != "public" {
			replyError(w, r, platform.Fail(403, "quick_reply_forbidden", "只有接待客服可以发送公开快捷回复"))
			return
		}
	} else if in.QuickReplyVersion != "" {
		returnError(w, r, "invalid_quick_reply", "话术版本必须同时提供话术编号")
		return
	}
	if in.QuickReplyID == "" && in.Type == "text" && strings.TrimSpace(in.Content) == "" {
		returnError(w, r, "empty_message", "请输入消息内容")
		return
	}
	var result map[string]any
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		ctx := r.Context()
		if err := canReadConversation(ctx, tx, a, id); err != nil {
			return err
		}
		if err := tenantActive(ctx, tx, a.TenantID); err != nil {
			return err
		}
		var status, current string
		if err := tx.QueryRow(ctx, "SELECT status,COALESCE(current_agent_id::text,'') FROM conversations WHERE id=$1 FOR UPDATE", id).Scan(&status, &current); err != nil {
			return err
		}
		existing, err := txJSON(ctx, tx, "SELECT "+messageProjection+" FROM messages m WHERE conversation_id=$1 AND sender_kind=$2 AND sender_id=$3 AND client_message_id=$4", id, actorKind(a), actorID(a), in.ClientID)
		if err == nil {
			var matches bool
			if err := tx.QueryRow(ctx, "SELECT audience=$7 AND COALESCE(reply_to_id::text,'')=$9 AND CASE WHEN $10<>'' THEN coalesce(quick_reply_id::text,'')=$10 AND coalesce(quick_reply_version::text,'')=$11 ELSE quick_reply_id IS NULL AND content=$5 AND message_type=$6 AND COALESCE(attachment_id::text,'')=$8 END FROM messages WHERE conversation_id=$1 AND sender_kind=$2 AND sender_id=$3 AND client_message_id=$4", id, actorKind(a), actorID(a), in.ClientID, in.Content, in.Type, in.Audience, in.AttachmentID, in.ReplyID, in.QuickReplyID, in.QuickReplyVersion).Scan(&matches); err != nil {
				return err
			}
			if !matches {
				return platform.Fail(409, "idempotency_conflict", "相同消息编号携带了不同内容")
			}
			result = existing
			return redactMessageReplies(ctx, tx, a, []map[string]any{result})
		}
		if err != pgx.ErrNoRows {
			return err
		}
		if status == "closed" {
			return platform.Fail(409, "conversation_closed", "咨询已结束，请发起新咨询")
		}
		if a.ContactID == "" && (current != a.MemberID || status != "active" || !a.CanServe) {
			return platform.Fail(403, "not_current_agent", "只有正式接待客服可以发送消息")
		}
		if err := prepareQuickReplyMessage(ctx, tx, a, &in); err != nil {
			return err
		}
		if in.Type == "text" && in.AttachmentID != "" {
			return platform.Fail(400, "unexpected_attachment", "媒体附件请使用对应的图片、语音或文件类型")
		}
		if in.Type != "text" {
			if !uuidPattern.MatchString(in.AttachmentID) {
				return platform.Fail(400, "attachment_required", "请先上传有效附件")
			}
			var mime string
			if err := tx.QueryRow(ctx, "SELECT content_type FROM attachments WHERE id=$1 AND ((uploader_id=$2 AND uploader_kind=$3) OR $4<>'')", in.AttachmentID, actorID(a), actorKind(a), in.QuickReplyID).Scan(&mime); err != nil {
				return err
			}
			if in.Type == "image" && !strings.HasPrefix(mime, "image/") || in.Type == "audio" && !strings.HasPrefix(mime, "audio/") && !strings.HasPrefix(mime, "video/webm") {
				return platform.Fail(400, "attachment_type_mismatch", "附件内容与消息类型不一致")
			}
		}
		if in.ReplyID != "" {
			if !uuidPattern.MatchString(in.ReplyID) {
				return platform.Fail(400, "invalid_reply", "引用消息编号不正确")
			}
			var visible bool
			if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM messages WHERE id=$1 AND conversation_id=$2 AND (audience='public' OR $3))", in.ReplyID, id, in.Audience == "internal" && a.ContactID == "").Scan(&visible); err != nil {
				return err
			}
			if !visible {
				return platform.Fail(403, "reply_not_visible", "不能引用不可见的消息")
			}
			visible, err = messageVisible(ctx, tx, a, in.ReplyID)
			if err != nil {
				return err
			}
			if !visible {
				return platform.Fail(403, "reply_not_visible", "不能引用当前接待范围外的历史消息")
			}
		}
		messageID := newID()
		payload := map[string]any{"id": messageID, "client_message_id": in.ClientID, "conversation_id": id, "sender_id": actorID(a), "sender_kind": actorKind(a), "message_type": in.Type, "content": in.Content, "audience": in.Audience, "attachment_id": in.AttachmentID, "reply_to_id": in.ReplyID, "created_at": nowUTC()}
		seq, err := s.emit(ctx, tx, a.TenantID, id, "message.created", in.Audience, payload)
		if err != nil {
			return err
		}
		if in.Options == nil {
			in.Options = []welcomeOption{}
		}
		_, err = tx.Exec(ctx, "INSERT INTO messages(id,tenant_id,conversation_id,sender_id,sender_kind,client_message_id,message_type,content,audience,attachment_id,reply_to_id,event_seq,options,quick_reply_id,quick_reply_version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,NULLIF($10,'')::uuid,NULLIF($11,'')::uuid,$12,$13,NULLIF($14,'')::uuid,NULLIF($15,'')::bigint)", messageID, a.TenantID, id, actorID(a), actorKind(a), in.ClientID, in.Type, in.Content, in.Audience, in.AttachmentID, in.ReplyID, seq, jsonValue(in.Options), in.QuickReplyID, in.QuickReplyVersion)
		if err != nil {
			return err
		}
		if err := recordQuickReplyUse(ctx, tx, a, in.QuickReplyID); err != nil {
			return err
		}
		result, err = txJSON(ctx, tx, "SELECT "+messageProjection+" FROM messages m WHERE id=$1", messageID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "UPDATE conversation_events SET payload=$2 WHERE conversation_id=$1 AND seq=$3", id, jsonValue(result), seq)
		if err == nil && a.ContactID != "" && in.Type == "text" {
			err = s.enqueueKeyword(ctx, tx, a.TenantID, id, messageID, in.Content)
		}
		if err != nil {
			return err
		}
		return redactMessageReplies(ctx, tx, a, []map[string]any{result})
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, result)
}

// returnError 输出字段校验错误。
func returnError(w http.ResponseWriter, r *http.Request, code, message string) {
	replyError(w, r, platform.Fail(400, code, message))
}

// messageHistory 使用稳定序号分页并过滤内部消息。
func (s *Service) messageHistory(w http.ResponseWriter, r *http.Request, a actor, id string) {
	limit := 50
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 && n <= 100 {
		limit = n
	}
	before := int64(0)
	if text := r.URL.Query().Get("before_seq"); text != "" {
		n, err := strconv.ParseInt(text, 10, 64)
		if err != nil || n < 0 {
			returnError(w, r, "invalid_cursor", "历史游标格式不正确")
			return
		}
		before = n
	}
	var items []map[string]any
	var watermark int64
	scope := ""
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		if err := canReadConversation(r.Context(), tx, a, id); err != nil {
			return err
		}
		if before == 0 {
			// 与发送和撤回的会话行锁串行，确保最近消息和恢复游标属于同一个快照。
			if err := tx.QueryRow(r.Context(), "SELECT next_event_seq FROM conversations WHERE id=$1 FOR SHARE", id).Scan(&watermark); err != nil {
				return err
			}
			if err := canReadConversation(r.Context(), tx, a, id); err != nil {
				return err
			}
		}
		var err error
		scope, err = historyScope(r.Context(), tx, a, id)
		if err != nil {
			return err
		}
		rows, err := tx.Query(r.Context(), "SELECT "+messageProjection+" FROM messages m WHERE conversation_id=$1 AND ($2=0 OR event_seq<$2) AND chat_message_visible(NULLIF($3,'')::uuid,NULLIF($4,'')::uuid,$5,m.id) ORDER BY event_seq DESC LIMIT $6", id, before, a.MemberID, a.ContactID, isAdmin(a), limit)
		if err != nil {
			return err
		}
		items, err = jsonRows(rows)
		if err != nil {
			return err
		}
		return redactMessageReplies(r.Context(), tx, a, items)
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	cursor := ""
	if len(items) > 0 {
		cursor = items[len(items)-1]["seq"].(string)
	}
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	result := map[string]any{"items": items, "next_cursor": cursor, "has_more": len(items) == limit, "history_scope": scope}
	if before == 0 {
		result["sync_cursor"] = s.encodeScopedCursor(a, id, watermark, scope)
	}
	platform.JSON(w, 200, result)
}

// withdraw 保留消息身份，在时间窗内撤回正文和附件展示。
func (s *Service) withdraw(w http.ResponseWriter, r *http.Request) {
	a := currentActor(r)
	id, message := r.PathValue("id"), r.PathValue("message_id")
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		ctx := r.Context()
		if err := canReadConversation(ctx, tx, a, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "SELECT id FROM conversations WHERE id=$1 FOR UPDATE", id); err != nil {
			return err
		}
		var owner, audience string
		var valid, withdrawn bool
		if err := tx.QueryRow(ctx, "SELECT sender_id::text,audience,created_at>now()-interval '2 minutes',withdrawn_at IS NOT NULL FROM messages WHERE id=$1 AND conversation_id=$2 FOR UPDATE", message, id).Scan(&owner, &audience, &valid, &withdrawn); err != nil {
			return err
		}
		if owner != actorID(a) {
			return platform.Fail(403, "not_message_author", "只能撤回本人发送的消息")
		}
		visible, err := messageVisible(ctx, tx, a, message)
		if err != nil {
			return err
		}
		if !visible {
			return platform.Fail(404, "message_not_found", "消息不属于当前接待可见范围")
		}
		if withdrawn {
			return nil
		}
		if !valid {
			return platform.Fail(409, "withdraw_window_expired", "消息超过两分钟，无法撤回")
		}
		if _, err := tx.Exec(ctx, "UPDATE messages SET withdrawn_at=now() WHERE id=$1", message); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE conversation_events SET payload=payload||jsonb_build_object('content','','attachment',NULL,'options','[]'::jsonb,'withdrawn_at',now()) WHERE conversation_id=$1 AND event_type='message.created' AND payload->>'id'=$2", id, message); err != nil {
			return err
		}
		_, err = s.emit(ctx, tx, a.TenantID, id, "message.withdrawn", audience, map[string]any{"id": message})
		return err
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, map[string]any{"id": message, "withdrawn": true})
}

// readCursor 只允许上报本人可见且确实存在的消息位置。
func (s *Service) readCursor(w http.ResponseWriter, r *http.Request) {
	a := currentActor(r)
	id := r.PathValue("id")
	var in struct {
		Seq string `json:"last_read_seq"`
	}
	if err := platform.Decode(w, r, &in); err != nil {
		replyError(w, r, err)
		return
	}
	seq, err := strconv.ParseInt(in.Seq, 10, 64)
	if err != nil || seq < 0 {
		returnError(w, r, "invalid_read_cursor", "已读序号格式不正确")
		return
	}
	err = platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		ctx := r.Context()
		if err := canReadConversation(ctx, tx, a, id); err != nil {
			return err
		}
		// 与提醒领取使用相同咨询行锁，避免已确认查看后仍被另一个标签领取旧提醒。
		if a.ContactID == "" {
			if _, err := tx.Exec(ctx, "SELECT id FROM conversations WHERE id=$1 FOR UPDATE", id); err != nil {
				return err
			}
		}
		if seq > 0 {
			var visible bool
			if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM messages WHERE conversation_id=$1 AND event_seq=$2 AND (audience='public' OR $3) AND chat_message_visible(NULLIF($4,'')::uuid,NULLIF($5,'')::uuid,$6,id))", id, seq, a.ContactID == "", a.MemberID, a.ContactID, isAdmin(a)).Scan(&visible); err != nil {
				return err
			}
			if !visible {
				return platform.Fail(400, "invalid_read_cursor", "已读位置不属于可见消息")
			}
		}
		var current int64
		if err := tx.QueryRow(ctx, "INSERT INTO read_cursors(tenant_id,conversation_id,participant_id,participant_kind,last_read_seq) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,conversation_id,participant_kind,participant_id) DO UPDATE SET last_read_seq=GREATEST(read_cursors.last_read_seq,EXCLUDED.last_read_seq),updated_at=now() RETURNING last_read_seq", a.TenantID, id, actorID(a), actorKind(a), seq).Scan(&current); err != nil {
			return err
		}
		seq = current
		if a.ContactID == "" {
			var receiving bool
			if err := tx.QueryRow(ctx, "SELECT coalesce(current_agent_id::text,'')=$2 FROM conversations WHERE id=$1", id, a.MemberID).Scan(&receiving); err != nil {
				return err
			}
			if !receiving {
				return nil
			}
			if err := attentionViewed(ctx, tx, a, id); err != nil {
				return err
			}
		}
		_, err := s.emit(ctx, tx, a.TenantID, id, "read.updated", "public", map[string]any{"participant_id": actorID(a), "participant_kind": actorKind(a), "last_read_seq": strconv.FormatInt(seq, 10)})
		return err
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, map[string]any{"last_read_seq": strconv.FormatInt(seq, 10)})
}
