// 文件作用说明：实现平台版本化模板、租户私有模板和显式复制，公共升级不会覆盖私有设计。
package decoration

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// templateInput 定义模板写入字段，拒绝客户端提供租户或所有者编号。
type templateInput struct {
	Name       string          `json:"name"`
	RenderMode string          `json:"render_mode"`
	Design     json.RawMessage `json:"design"`
	Version    int64           `json:"version"`
	Status     string          `json:"status,omitempty"`
}

// listTemplates 列出租户私有设计及当前可选择的平台模板。
func (s *Service) listTemplates(w http.ResponseWriter, r *http.Request) {
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		rows, err := tx.Query(r.Context(), "SELECT to_jsonb(t) FROM tenant_templates t ORDER BY created_at DESC,id LIMIT 500")
		if err != nil {
			return nil, err
		}
		items, err := jsonRows(rows)
		if err != nil {
			return nil, err
		}
		rows, err = tx.Query(r.Context(), "SELECT to_jsonb(t) FROM platform_templates t WHERE status='published' ORDER BY created_at,id LIMIT 100")
		if err != nil {
			return nil, err
		}
		public, err := jsonRows(rows)
		return map[string]any{"items": items, "platform_templates": public}, err
	})
}

// saveTemplate 创建或以乐观版本号更新私有模板，取消旧AI对草稿的覆盖资格。
func (s *Service) saveTemplate(w http.ResponseWriter, r *http.Request) {
	var input templateInput
	if err := platform.Decode(w, r, &input); err != nil {
		respond(w, r, nil, err)
		return
	}
	if input.Status != "" {
		respond(w, r, nil, platform.Fail(400, "unsupported_field", "模板状态由保存与AI任务确定"))
		return
	}
	if err := validName(input.Name); err != nil {
		respond(w, r, nil, err)
		return
	}
	design, err := validateDesign(input.RenderMode, input.Design)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	id := r.PathValue("id")
	if id != "" {
		if err := idValue(id); err != nil {
			respond(w, r, nil, err)
			return
		}
	}
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		p := platform.Current(r)
		if err := checkPackageDesign(r.Context(), tx, input.RenderMode, design); err != nil {
			return nil, err
		}
		if err := s.checkFeature(r.Context(), tx, p.TenantID, decorationFeature(input.RenderMode)); err != nil {
			return nil, err
		}
		if id == "" {
			return record(r.Context(), tx, `INSERT INTO tenant_templates(tenant_id,owner_membership_id,name,render_mode,design) VALUES($1,$2,$3,$4,$5) RETURNING to_jsonb(tenant_templates)`, p.TenantID, p.MembershipID, input.Name, input.RenderMode, design)
		}
		var mode string
		var version int64
		if err := tx.QueryRow(r.Context(), "SELECT render_mode,version FROM tenant_templates WHERE id=$1 FOR UPDATE", id).Scan(&mode, &version); err != nil {
			return nil, err
		}
		if version != input.Version {
			return nil, conflict()
		}
		if mode != input.RenderMode {
			return nil, platform.Fail(400, "mode_change_requires_copy", "切换渲染模式请新建模板，以保留原作品")
		}
		return record(r.Context(), tx, "UPDATE tenant_templates SET name=$2,design=$3,version=version+1,status='ready',updated_at=now() WHERE id=$1 RETURNING to_jsonb(tenant_templates)", id, input.Name, design)
	})
}

// decorationFeature 将代码装修映射到高级权益，其余使用基础装修权益。
func decorationFeature(mode string) string {
	if mode == "code" {
		return "advanced_decoration"
	}
	return "decoration"
}

// duplicateTemplate 显式复制私有或已上架的平台模板，来源版本固定不随升级更新。
func (s *Service) duplicateTemplate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := idValue(id); err != nil {
		respond(w, r, nil, err)
		return
	}
	s.tenant(w, r, func(tx pgx.Tx) (any, error) { return s.copyTemplate(r.Context(), tx, platform.Current(r), id) })
}

// copyTemplate 优先复制当前租户私有模板，公共模板只读取已上架固定版本。
func (s *Service) copyTemplate(ctx context.Context, tx pgx.Tx, p platform.Principal, id string) (json.RawMessage, error) {
	var mode string
	err := tx.QueryRow(ctx, "SELECT render_mode FROM tenant_templates WHERE id=$1 AND status='ready'", id).Scan(&mode)
	if err == nil {
		if err := s.checkFeature(ctx, tx, p.TenantID, decorationFeature(mode)); err != nil {
			return nil, err
		}
		return record(ctx, tx, `INSERT INTO tenant_templates(tenant_id,owner_membership_id,name,render_mode,design,source_platform_template_id,source_platform_version) SELECT tenant_id,$2,name||' 副本',render_mode,design,source_platform_template_id,source_platform_version FROM tenant_templates WHERE id=$1 AND status='ready' RETURNING to_jsonb(tenant_templates)`, id, p.MembershipID)
	}
	if err != pgx.ErrNoRows {
		return nil, err
	}
	var publicDesign json.RawMessage
	var name string
	var publicVersion int64
	if err := tx.QueryRow(ctx, "SELECT render_mode,design,name,version FROM platform_templates WHERE id=$1 AND status='published' FOR SHARE", id).Scan(&mode, &publicDesign, &name, &publicVersion); err != nil {
		return nil, err
	}
	if err := s.checkFeature(ctx, tx, p.TenantID, decorationFeature(mode)); err != nil {
		return nil, err
	}
	var code CodeDesign
	if mode == "code" && json.Unmarshal(publicDesign, &code) == nil && code.PlatformPackageID != "" {
		pkg, err := loadPlatformTemplatePackage(ctx, tx, code.PlatformPackageID)
		if err != nil {
			return nil, err
		}
		stored, err := s.storePackage(ctx, tx, p, name+" 副本", pkg)
		if err != nil {
			return nil, err
		}
		privateID, err := storedPackageID(stored)
		if err != nil {
			return nil, err
		}
		design, _ := json.Marshal(CodeDesign{PackageID: privateID})
		return record(ctx, tx, "INSERT INTO tenant_templates(tenant_id,owner_membership_id,name,render_mode,design,source_platform_template_id,source_platform_version) VALUES($1,$2,$3,'code',$4,$5,$6) RETURNING to_jsonb(tenant_templates)", p.TenantID, p.MembershipID, name+" 副本", design, id, publicVersion)
	}
	return record(ctx, tx, `INSERT INTO tenant_templates(tenant_id,owner_membership_id,name,render_mode,design,source_platform_template_id,source_platform_version) SELECT $2,$3,name||' 副本',render_mode,design,id,version FROM platform_templates WHERE id=$1 AND status='published' RETURNING to_jsonb(tenant_templates)`, id, p.TenantID, p.MembershipID)
}

// platformTemplates 维护平台模板当前指针，每次编辑创建不可变历史版本。
func (s *Service) platformTemplates(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		rows, err := s.pool.Query(r.Context(), "SELECT to_jsonb(t) FROM platform_templates t ORDER BY created_at,id LIMIT 500")
		if err != nil {
			respond(w, r, nil, err)
			return
		}
		items, err := jsonRows(rows)
		respond(w, r, map[string]any{"items": items}, err)
		return
	}
	var input templateInput
	if err := platform.Decode(w, r, &input); err != nil {
		respond(w, r, nil, err)
		return
	}
	if err := validName(input.Name); err != nil {
		respond(w, r, nil, err)
		return
	}
	design, err := validateDesign(input.RenderMode, input.Design)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	var privateCode CodeDesign
	if input.RenderMode == "code" && json.Unmarshal(design, &privateCode) == nil && privateCode.PackageID != "" {
		respond(w, r, nil, invalidDesign("公共平台模板不能引用租户私有代码包，请使用内联模板或保持为租户私有模板"))
		return
	}
	if input.Status == "" {
		input.Status = "published"
	}
	if input.Status != "published" && input.Status != "hidden" {
		respond(w, r, nil, platform.Fail(400, "invalid_status", "平台模板状态只能是published或hidden"))
		return
	}
	id := r.PathValue("id")
	if id != "" {
		if err := idValue(id); err != nil {
			respond(w, r, nil, err)
			return
		}
	}
	var result json.RawMessage
	err = pgx.BeginFunc(r.Context(), s.pool, func(tx pgx.Tx) error {
		if privateCode.PlatformPackageID != "" {
			if _, err := loadPlatformTemplatePackage(r.Context(), tx, privateCode.PlatformPackageID); err != nil {
				return err
			}
		}
		if id == "" {
			err := tx.QueryRow(r.Context(), `INSERT INTO platform_templates(name,render_mode,design,status) VALUES($1,$2,$3,$4) RETURNING id::text`, input.Name, input.RenderMode, design, input.Status).Scan(&id)
			if err != nil {
				return err
			}
		} else {
			tag, err := tx.Exec(r.Context(), "UPDATE platform_templates SET name=$2,render_mode=$3,design=$4,status=$5,version=version+1,updated_at=now() WHERE id=$1 AND version=$6", id, input.Name, input.RenderMode, design, input.Status, input.Version)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return conflict()
			}
		}
		if _, err := tx.Exec(r.Context(), `INSERT INTO platform_template_versions(template_id,version,name,render_mode,design) SELECT id,version,name,render_mode,design FROM platform_templates WHERE id=$1`, id); err != nil {
			return err
		}
		var err error
		result, err = record(r.Context(), tx, "SELECT to_jsonb(t) FROM platform_templates t WHERE id=$1", id)
		return err
	})
	respond(w, r, result, err)
}
