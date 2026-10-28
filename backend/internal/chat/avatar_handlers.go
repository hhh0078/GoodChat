// 文件作用说明：提供租户头像素材、访客头像池、成员固定头像与仅图片字节的公开读取接口。
package chat

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// registerAvatars 注册头像接口，素材上传允许有效成员，访客随机池只允许租户管理员修改。
func (s *Service) registerAvatars(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/tenant/avatar-assets", s.auth.RequireTenant(s.avatarAssets))
	mux.HandleFunc("POST /api/v1/tenant/avatar-assets", s.auth.RequireTenant(s.avatarAssets))
	mux.HandleFunc("GET /api/v1/tenant/avatar-pool", s.auth.RequireTenant(s.avatarPool))
	mux.HandleFunc("PATCH /api/v1/tenant/avatar-pool", s.auth.RequireAdmin(s.avatarPool))
	mux.HandleFunc("GET /api/v1/agent/avatar", s.auth.RequireTenant(s.agentAvatar))
	mux.HandleFunc("PATCH /api/v1/agent/avatar", s.auth.RequireTenant(s.agentAvatar))
	mux.HandleFunc("GET /api/v1/avatar-assets/{id}", s.publicAvatar)
}

// avatarAssets 列出平台及本租户头像，上传后只返回受控PNG素材信息，所有权由认证上下文确定。
func (s *Service) avatarAssets(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	if r.Method == http.MethodPost {
		s.uploadAvatar(w, r, a)
		return
	}
	items := builtinAvatars()
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		if err := liveMember(r.Context(), tx, a); err != nil {
			return err
		}
		rows, err := tx.Query(r.Context(), "SELECT id::text,name,width,height,size_bytes FROM avatar_uploads ORDER BY created_at DESC,id LIMIT 500")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item := avatarAsset{Category: "uploaded", ContentType: "image/png"}
			if err := rows.Scan(&item.ID, &item.Name, &item.Width, &item.Height, &item.SizeBytes); err != nil {
				return err
			}
			item.URL = platform.AvatarURL(item.ID)
			items = append(items, item)
		}
		return rows.Err()
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, map[string]any{"items": items})
}

// uploadAvatar 在身份复核后限制并发解码，保存实际图片字节而非伪造URL或客户端声明的媒体类型。
func (s *Service) uploadAvatar(w http.ResponseWriter, r *http.Request, a actor) {
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error { return liveMember(r.Context(), tx, a) })
	if err != nil {
		replyError(w, r, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, avatarUploadLimit+65536)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		returnError(w, r, "invalid_avatar_upload", "请上传不超过5MiB的头像图片")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		returnError(w, r, "avatar_file_required", "请选择头像图片")
		return
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, avatarUploadLimit+1))
	if err != nil {
		replyError(w, r, err)
		return
	}
	clean, width, height, err := s.normalizeUploadedAvatar(raw)
	if err != nil {
		replyError(w, r, err)
		return
	}
	digest := sha256.Sum256(clean)
	name := strings.TrimSpace(filepath.Base(strings.ReplaceAll(header.Filename, "\\", "/")))
	if name == "" || name == "." {
		name = "头像"
	}
	if len([]rune(name)) > 120 {
		name = string([]rune(name)[:120])
	}
	item := avatarAsset{ID: newID(), Name: name, Category: "uploaded", ContentType: "image/png", Width: width, Height: height, SizeBytes: len(clean)}
	item.URL = platform.AvatarURL(item.ID)
	err = platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		if err := liveMember(r.Context(), tx, a); err != nil {
			return err
		}
		_, err := tx.Exec(r.Context(), `INSERT INTO avatar_uploads(id,tenant_id,uploaded_by,name,width,height,size_bytes,image_data,sha256) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, item.ID, a.TenantID, a.MemberID, name, width, height, len(clean), clean, hex.EncodeToString(digest[:]))
		return err
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 201, item)
}

// normalizeUploadedAvatar 仅在CPU解码期间占用配额，网络慢传和数据库等待不会阻塞其他租户上传。
// raw 为已完成受限读取的图片字节，返回生产规范化结果。
func (s *Service) normalizeUploadedAvatar(raw []byte) ([]byte, int, int, error) {
	select {
	case s.avatarSlots <- struct{}{}:
		defer func() { <-s.avatarSlots }()
	default:
		return nil, 0, 0, platform.Fail(429, "avatar_upload_busy", "正在处理其他头像，请稍后重试")
	}
	return NormalizeAvatar(raw)
}

// avatarPool 读取或替换访客随机头像池，空数组明确恢复50张平台默认头像。
func (s *Service) avatarPool(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	refs := []string{}
	if r.Method == http.MethodPatch {
		var input struct {
			Refs *[]string `json:"avatar_refs"`
		}
		if err := platform.Decode(w, r, &input); err != nil {
			replyError(w, r, err)
			return
		}
		if input.Refs == nil {
			returnError(w, r, "avatar_refs_required", "请提交avatar_refs数组，空数组可恢复平台默认")
			return
		}
		seen := map[string]bool{}
		for _, raw := range *input.Refs {
			ref := strings.ToLower(strings.TrimSpace(raw))
			if !seen[ref] {
				seen[ref] = true
				refs = append(refs, ref)
			}
		}
	}
	var result map[string]any
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		if err := liveMember(r.Context(), tx, a); err != nil {
			return err
		}
		if r.Method == http.MethodPatch {
			if !isAdmin(a) {
				return platform.Fail(403, "admin_required", "仅租户管理员可维护访客头像池")
			}
			if err := validateAvatarRefs(r.Context(), tx, refs); err != nil {
				return err
			}
			if _, err := ensureSettings(r.Context(), tx, a.TenantID); err != nil {
				return err
			}
			if _, err := tx.Exec(r.Context(), `UPDATE chat_settings SET settings=settings||jsonb_build_object('visitor_avatar_pool',$2::jsonb),updated_at=now() WHERE tenant_id=$1`, a.TenantID, jsonValue(refs)); err != nil {
				return err
			}
			if err := audit(r.Context(), tx, a, "avatar_pool.updated", "", map[string]any{"avatar_refs": refs}); err != nil {
				return err
			}
		}
		pool, defaults, err := avatarPoolRefs(r.Context(), tx, a.TenantID)
		result = map[string]any{"avatar_refs": pool, "using_defaults": defaults}
		return err
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, result)
}

// agentAvatar 只修改当前租户会员的固定头像，不能通过输入覆盖其他成员或其他租户。
func (s *Service) agentAvatar(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	var input struct {
		Ref *string `json:"avatar_ref"`
	}
	if r.Method == http.MethodPatch {
		if err := platform.Decode(w, r, &input); err != nil {
			replyError(w, r, err)
			return
		}
		if input.Ref == nil {
			returnError(w, r, "avatar_ref_required", "请提交avatar_ref，空字符串可清除个人头像")
			return
		}
		*input.Ref = strings.ToLower(strings.TrimSpace(*input.Ref))
	}
	var ref string
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		if err := liveMember(r.Context(), tx, a); err != nil {
			return err
		}
		if r.Method == http.MethodPatch {
			if *input.Ref != "" {
				if err := validateAvatarRefs(r.Context(), tx, []string{*input.Ref}); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(r.Context(), "UPDATE tenant_memberships SET avatar_ref=$1 WHERE id=$2 AND tenant_id=$3", *input.Ref, a.MemberID, a.TenantID); err != nil {
				return err
			}
		}
		return tx.QueryRow(r.Context(), "SELECT avatar_ref FROM tenant_memberships WHERE id=$1 AND tenant_id=$2", a.MemberID, a.TenantID).Scan(&ref)
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, map[string]any{"avatar_ref": ref, "avatar_url": platform.AvatarURL(ref)})
}

// publicAvatar 只返回已经重编码的公开PNG字节，随机编号不能枚举租户元数据或读取私有聊天附件。
func (s *Service) publicAvatar(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !uuidPattern.MatchString(id) {
		returnError(w, r, "invalid_avatar", "头像编号无效")
		return
	}
	var content []byte
	var digest string
	var created time.Time
	if err := s.pool.QueryRow(r.Context(), "SELECT image_data,sha256,created_at FROM public_avatar_image($1)", id).Scan(&content, &digest, &created); err != nil {
		replyError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", `"`+digest+`"`)
	http.ServeContent(w, r, "avatar.png", created, bytes.NewReader(content))
}
