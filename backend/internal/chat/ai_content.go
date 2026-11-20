// 文件作用说明：管理租户纯文本知识、共享媒体及带必填使用描述的有序AI话术。
package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// aiBlock 表示可精确重放的文字或媒体内容，媒体只保存稳定编号。
type aiBlock struct {
	Type       string          `json:"type"`
	Text       string          `json:"text,omitempty"`
	MediaID    string          `json:"media_id,omitempty"`
	FormFields []formField     `json:"form_fields,omitempty"`
	Options    []welcomeOption `json:"options,omitempty"`
}

// aiContentInput 保存内容编辑的白名单字段，不接受租户或创建成员编号。
type aiContentInput struct {
	Title        string    `json:"title"`
	FolderID     *string   `json:"folder_id"`
	Body         string    `json:"body"`
	Description  string    `json:"description"`
	Language     string    `json:"language"`
	Blocks       []aiBlock `json:"blocks"`
	AttachmentID string    `json:"attachment_id"`
	Version      int64     `json:"version"`
	Enabled      bool      `json:"enabled"`
}

// aiResources 为固定资源提供租户隔离、所有权校验和乐观版本更新。
func (s *Service) aiResources(resource string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := memberActor(r)
		id := r.PathValue("id")
		tables := map[string]string{"knowledge": "ai_knowledge_bases", "media": "ai_media", "scripts": "ai_scripts"}
		table := tables[resource]
		var in aiContentInput
		if r.Method != "GET" && r.Method != "DELETE" {
			if e := platform.Decode(w, r, &in); e != nil {
				replyError(w, r, e)
				return
			}
			in.Title = strings.TrimSpace(in.Title)
			if in.Title == "" || len([]rune(in.Title)) > 100 {
				returnError(w, r, "ai_title_required", "请输入标题")
				return
			}
		}
		var result any
		err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
			ctx := r.Context()
			if e := liveMember(ctx, tx, a); e != nil {
				return e
			}
			if r.Method == "GET" {
				query := "SELECT to_jsonb(x)||jsonb_build_object('editable',x.owner_membership_id=$1) FROM " + table + " x WHERE NOT x.archived ORDER BY x.updated_at DESC LIMIT 1000"
				if resource == "media" {
					query = `SELECT to_jsonb(x)||jsonb_build_object('editable',x.owner_membership_id=$1,'attachment',to_jsonb(f)-'storage_key'-'sha256','references',(SELECT count(*) FROM ai_scripts s WHERE NOT s.archived AND s.blocks @> jsonb_build_array(jsonb_build_object('media_id',x.id::text)))) FROM ai_media x JOIN attachments f ON f.id=x.id WHERE x.enabled ORDER BY x.created_at DESC LIMIT 1000`
				}
				rows, e := tx.Query(ctx, query, a.MemberID)
				if e != nil {
					return e
				}
				items, e := jsonRows(rows)
				result = map[string]any{"items": items}
				return e
			}
			if r.Method == "POST" {
				if _, e := tx.Exec(ctx, "SELECT id FROM tenants WHERE id=$1 FOR UPDATE", a.TenantID); e != nil {
					return e
				}
				var count int
				if e := tx.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); e != nil {
					return e
				}
				if count >= 1000 {
					return platform.Fail(400, "ai_resource_limit", "资源数量已达到上限")
				}
				id = newID()
			} else {
				if !uuidPattern.MatchString(id) {
					return platform.Fail(404, "not_found", "记录不存在或无权访问")
				}
				var owner string
				var version int64
				q := "SELECT owner_membership_id::text,version FROM " + table + " WHERE id=$1 AND NOT archived FOR UPDATE"
				if resource == "media" {
					q = "SELECT owner_membership_id::text,1::bigint FROM ai_media WHERE id=$1 AND enabled FOR UPDATE"
				}
				if e := tx.QueryRow(ctx, q, id).Scan(&owner, &version); e != nil {
					return e
				}
				if owner != a.MemberID && !(r.Method == "DELETE" && isAdmin(a)) {
					return platform.Fail(403, "ai_owner_required", "只能修改或删除自己的内容")
				}
				if r.Method != "DELETE" && resource != "media" && in.Version != version {
					return platform.Fail(409, "ai_version_conflict", "内容已更新，请刷新后重试")
				}
			}
			if r.Method == "DELETE" {
				q := "UPDATE " + table + " SET enabled=false,archived=true,version=version+1,updated_at=now() WHERE id=$1"
				if resource == "media" {
					q = "UPDATE ai_media SET enabled=false WHERE id=$1"
				}
				_, e := tx.Exec(ctx, q, id)
				result = map[string]any{"id": id}
				return e
			}
			switch resource {
			case "knowledge":
				if in.FolderID != nil {
					if e := aiValidateFolder(ctx, tx, a.TenantID, *in.FolderID); e != nil {
						return e
					}
				}
				if strings.TrimSpace(in.Body) == "" || len([]byte(in.Body)) > 16000 {
					return platform.Fail(400, "ai_knowledge_limit", "知识库正文不能为空且不能超过16000字节")
				}
				if r.Method == "POST" {
					_, err := tx.Exec(ctx, `INSERT INTO ai_knowledge_bases(id,tenant_id,owner_membership_id,title,draft_body,enabled,folder_id) VALUES($1,$2,$3,$4,$5,$6,NULLIF($7,'')::uuid)`, id, a.TenantID, a.MemberID, in.Title, in.Body, in.Enabled, in.FolderID)
					if err != nil {
						return err
					}
				} else {
					// 目录属于组织元数据，正文未变时移动文档不重新生成描述或改变已发布正文。
					if in.FolderID != nil {
						if _, e := tx.Exec(ctx, "UPDATE ai_knowledge_bases SET folder_id=NULLIF($2,'')::uuid,updated_at=now() WHERE id=$1", id, *in.FolderID); e != nil {
							return e
						}
					}
					var unchanged bool
					if e := tx.QueryRow(ctx, "SELECT draft_body=$2 AND title=$3 AND enabled=$4 AND status='ready' FROM ai_knowledge_bases WHERE id=$1", id, in.Body, in.Title, in.Enabled).Scan(&unchanged); e != nil {
						return e
					}
					if unchanged {
						var e error
						result, e = txJSON(ctx, tx, "SELECT to_jsonb(x) FROM ai_knowledge_bases x WHERE id=$1", id)
						return e
					}
					if _, e := tx.Exec(ctx, `UPDATE ai_knowledge_bases SET title=$2,draft_body=$3,enabled=$4,status='pending',version=version+1,error_code='',updated_at=now() WHERE id=$1`, id, in.Title, in.Body, in.Enabled); e != nil {
						return e
					}
				}
				if e := s.enqueueAISummary(ctx, tx, a, id); e != nil {
					return e
				}
			case "scripts":
				in.Description = strings.TrimSpace(in.Description)
				if in.Description == "" || len([]rune(in.Description)) > 400 {
					return platform.Fail(400, "ai_description_required", "请填写话术使用描述")
				}
				if !aiLanguage(in.Language, false) {
					return platform.Fail(400, "ai_invalid_language", "请选择支持的语言")
				}
				if e := validateAIBlocks(ctx, tx, in.Blocks); e != nil {
					return e
				}
				if r.Method == "POST" {
					_, err := tx.Exec(ctx, `INSERT INTO ai_scripts(id,tenant_id,owner_membership_id,title,description,language,blocks,enabled) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, id, a.TenantID, a.MemberID, in.Title, in.Description, in.Language, jsonValue(in.Blocks), in.Enabled)
					if err != nil {
						return err
					}
				} else {
					_, err := tx.Exec(ctx, `UPDATE ai_scripts SET title=$2,description=$3,language=$4,blocks=$5,enabled=$6,version=version+1,updated_at=now() WHERE id=$1`, id, in.Title, in.Description, in.Language, jsonValue(in.Blocks), in.Enabled)
					if err != nil {
						return err
					}
				}
			case "media":
				if r.Method == "POST" {
					if !uuidPattern.MatchString(in.AttachmentID) {
						return platform.Fail(400, "ai_media_required", "请先上传媒体文件")
					}
					var total int64
					if e := tx.QueryRow(ctx, "SELECT coalesce(sum(f.size_bytes),0) FROM ai_media m JOIN attachments f ON f.id=m.id WHERE m.enabled").Scan(&total); e != nil {
						return e
					}
					tag, e := tx.Exec(ctx, `INSERT INTO ai_media(id,tenant_id,owner_membership_id,title) SELECT id,tenant_id,$2,$3 FROM attachments WHERE id=$1 AND uploader_id=$2 AND uploader_kind='agent' AND (content_type LIKE 'image/%' OR content_type LIKE 'audio/%' OR content_type IN('video/mp4','video/webm','video/quicktime')) AND size_bytes+$4<=536870912`, in.AttachmentID, a.MemberID, in.Title, total)
					if e != nil {
						return e
					}
					if tag.RowsAffected() != 1 {
						return platform.Fail(400, "ai_media_unavailable", "媒体不可用或容量已达到上限")
					}
					id = in.AttachmentID
				} else {
					if _, e := tx.Exec(ctx, "UPDATE ai_media SET title=$2 WHERE id=$1", id, in.Title); e != nil {
						return e
					}
				}
			}
			result = map[string]any{"id": id}
			return nil
		})
		if err != nil {
			replyError(w, r, err)
			return
		}
		platform.JSON(w, 200, result)
	}
}

// validateAIBlocks 在保存阶段验证全部素材归属和真实类型，拒绝任意URL。
func validateAIBlocks(ctx context.Context, tx pgx.Tx, blocks []aiBlock) error {
	if len(blocks) == 0 || len(blocks) > 12 {
		return platform.Fail(400, "ai_blocks_invalid", "话术需要1到12个内容块")
	}
	total := 0
	for _, b := range blocks {
		if len(b.FormFields) > 0 || len(b.Options) > 0 {
			return platform.Fail(400, "ai_blocks_invalid", "请通过快捷回复管理表单和选项")
		}
		total += len([]rune(b.Text))
		if total > 10000 {
			return platform.Fail(400, "ai_blocks_invalid", "话术内容过长")
		}
		if b.Type == "text" {
			if strings.TrimSpace(b.Text) == "" || b.MediaID != "" {
				return platform.Fail(400, "ai_blocks_invalid", "请检查话术内容块")
			}
			continue
		}
		if !inStrings(b.Type, "image", "video", "audio") || !uuidPattern.MatchString(b.MediaID) || b.Text != "" {
			return platform.Fail(400, "ai_blocks_invalid", "请检查话术内容块")
		}
		var mime string
		if e := tx.QueryRow(ctx, "SELECT f.content_type FROM ai_media m JOIN attachments f ON f.id=m.id WHERE m.id=$1 AND m.enabled", b.MediaID).Scan(&mime); e != nil {
			return platform.Fail(400, "ai_media_unavailable", "媒体不可用或容量已达到上限")
		}
		if !strings.HasPrefix(mime, b.Type+"/") {
			return platform.Fail(400, "attachment_type_mismatch", "附件内容与消息类型不一致")
		}
	}
	return nil
}

// enqueueAISummary 保存正文对应的可靠描述任务，不在保存事务中请求供应商。
func (s *Service) enqueueAISummary(ctx context.Context, tx pgx.Tx, a actor, id string) error {
	var version int64
	var body string
	if e := tx.QueryRow(ctx, "SELECT version,draft_body FROM ai_knowledge_bases WHERE id=$1", id).Scan(&version, &body); e != nil {
		return e
	}
	snapshot := map[string]any{"body": body, "knowledge_version": version, "locale": "zh-CN"}
	_, e := tx.Exec(ctx, `INSERT INTO ai_jobs(tenant_id,kind,knowledge_id,owner_membership_id,snapshot) VALUES($1,'summary',$2,$3,$4)`, a.TenantID, id, a.MemberID, jsonValue(snapshot))
	return e
}

// aiLanguage 限定九语言及机器人自动识别标识。
func aiLanguage(v string, auto bool) bool {
	return auto && v == "auto" || inStrings(v, "zh-CN", "en", "zh-TW", "ja", "ko", "es", "pt-BR", "fr", "de")
}

// decodeAIBlocks 从持久快照恢复内容块，损坏数据始终返回错误。
func decodeAIBlocks(raw []byte) ([]aiBlock, error) {
	var blocks []aiBlock
	e := json.Unmarshal(raw, &blocks)
	return blocks, e
}
