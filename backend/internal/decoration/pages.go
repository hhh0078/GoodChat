// 文件作用说明：维护装修页面的独立草稿、乐观保存与短期隔离预览。
package decoration

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// pageInput 定义装修页面创建和保存字段。
type pageInput struct {
	Name       string          `json:"name"`
	TemplateID string          `json:"template_id,omitempty"`
	RenderMode string          `json:"render_mode,omitempty"`
	Draft      json.RawMessage `json:"draft,omitempty"`
	Version    int64           `json:"version,omitempty"`
}

// listPages 列出当前租户页面及当前线上版本，空数据返回空数组。
func (s *Service) listPages(w http.ResponseWriter, r *http.Request) {
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		rows, err := tx.Query(r.Context(), "SELECT to_jsonb(p) FROM decoration_pages p ORDER BY created_at DESC,id LIMIT 500")
		if err != nil {
			return nil, err
		}
		items, err := jsonRows(rows)
		return map[string]any{"items": items}, err
	})
}

// createPage 从私有模板或平台固定版本创建互不覆盖的页面草稿。
func (s *Service) createPage(w http.ResponseWriter, r *http.Request) {
	var input pageInput
	if err := platform.Decode(w, r, &input); err != nil {
		respond(w, r, nil, err)
		return
	}
	if err := validName(input.Name); err != nil {
		respond(w, r, nil, err)
		return
	}
	if input.TemplateID != "" {
		if err := idValue(input.TemplateID); err != nil {
			respond(w, r, nil, err)
			return
		}
	}
	if input.RenderMode != "" || len(input.Draft) > 0 || input.Version != 0 {
		respond(w, r, nil, platform.Fail(400, "unsupported_field", "创建页面请选择模板，使用保存接口编辑草稿"))
		return
	}
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		ctx, p := r.Context(), platform.Current(r)
		mode := "schema"
		design := defaultDesign(mode, input.Name)
		var template any
		if input.TemplateID != "" {
			var templateID string
			err := tx.QueryRow(ctx, "SELECT id::text,render_mode,design FROM tenant_templates WHERE id=$1 AND status='ready'", input.TemplateID).Scan(&templateID, &mode, &design)
			if err == pgx.ErrNoRows {
				copied, copyErr := s.copyTemplate(ctx, tx, p, input.TemplateID)
				if copyErr != nil {
					return nil, copyErr
				}
				var item struct {
					ID         string          `json:"id"`
					RenderMode string          `json:"render_mode"`
					Design     json.RawMessage `json:"design"`
				}
				if err := json.Unmarshal(copied, &item); err != nil {
					return nil, err
				}
				templateID, mode, design = item.ID, item.RenderMode, item.Design
			} else if err != nil {
				return nil, err
			}
			template = templateID
		}
		if err := s.checkFeature(ctx, tx, p.TenantID, decorationFeature(mode)); err != nil {
			return nil, err
		}
		return record(ctx, tx, `INSERT INTO decoration_pages(tenant_id,name,template_id,render_mode,draft) VALUES($1,$2,$3,$4,$5) RETURNING to_jsonb(decoration_pages)`, p.TenantID, input.Name, template, mode, design)
	})
}

// getPage 读取当前租户的装修草稿，其他租户编号自然返回不存在。
func (s *Service) getPage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := idValue(id); err != nil {
		respond(w, r, nil, err)
		return
	}
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		return record(r.Context(), tx, "SELECT to_jsonb(p) FROM decoration_pages p WHERE id=$1", id)
	})
}

// savePage 按草稿版本保存并阻止隐式渲染模式切换覆盖原作品。
func (s *Service) savePage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := idValue(id); err != nil {
		respond(w, r, nil, err)
		return
	}
	var input pageInput
	if err := platform.Decode(w, r, &input); err != nil {
		respond(w, r, nil, err)
		return
	}
	if input.TemplateID != "" {
		respond(w, r, nil, platform.Fail(400, "unsupported_field", "页面来源模板不能在保存时替换"))
		return
	}
	if err := validName(input.Name); err != nil {
		respond(w, r, nil, err)
		return
	}
	design, err := validateDesign(input.RenderMode, input.Draft)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		var mode string
		var version int64
		if err := tx.QueryRow(r.Context(), "SELECT render_mode,version FROM decoration_pages WHERE id=$1 FOR UPDATE", id).Scan(&mode, &version); err != nil {
			return nil, err
		}
		if version != input.Version {
			return nil, conflict()
		}
		if mode != input.RenderMode {
			return nil, platform.Fail(400, "mode_change_requires_copy", "切换渲染模式请新建页面，以保留原作品")
		}
		if err := checkPackageDesign(r.Context(), tx, mode, design); err != nil {
			return nil, err
		}
		if err := s.checkFeature(r.Context(), tx, platform.Current(r).TenantID, decorationFeature(mode)); err != nil {
			return nil, err
		}
		return record(r.Context(), tx, "UPDATE decoration_pages SET name=$2,draft=$3,version=version+1,updated_at=now() WHERE id=$1 RETURNING to_jsonb(decoration_pages)", id, input.Name, design)
	})
}

// previewPage 固定当前草稿为十五分钟快照，预览不会切换线上页面。
func (s *Service) previewPage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := idValue(id); err != nil {
		respond(w, r, nil, err)
		return
	}
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		var mode string
		var design json.RawMessage
		if err := tx.QueryRow(r.Context(), "SELECT render_mode,draft FROM decoration_pages WHERE id=$1", id).Scan(&mode, &design); err != nil {
			return nil, err
		}
		design, err := validateDesign(mode, design)
		if err != nil {
			return nil, err
		}
		token, err := platform.Token()
		if err != nil {
			return nil, err
		}
		var previewID string
		if err := tx.QueryRow(r.Context(), `INSERT INTO decoration_previews(tenant_id,page_id,render_mode,design,token_hash) VALUES($1,$2,$3,$4,$5) RETURNING id::text`, platform.Current(r).TenantID, id, mode, design, digest([]byte(token))).Scan(&previewID); err != nil {
			return nil, err
		}
		return map[string]any{"preview_url": s.renderURL(previewID, token), "release_id": previewID, "render_mode": mode, "design": design}, nil
	})
}

// EntryDecoration 读取入口绑定的不可变设计，未发布页面回退基础聊天。
func (s *Service) EntryDecoration(ctx context.Context, tx pgx.Tx, tenantID, linkID string) (map[string]any, error) {
	var pageID, releaseID, mode string
	var design json.RawMessage
	err := tx.QueryRow(ctx, `SELECT p.id::text,r.id::text,r.render_mode,r.design FROM access_links l JOIN decoration_pages p ON p.id=l.page_id AND p.tenant_id=l.tenant_id JOIN decoration_releases r ON r.id=CASE WHEN l.binding_mode='PIN_RELEASE' THEN l.pinned_release_id ELSE p.current_release_id END AND r.tenant_id=p.tenant_id AND r.page_id=p.id WHERE l.tenant_id=$1 AND l.id=$2`, tenantID, linkID).Scan(&pageID, &releaseID, &mode, &design)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	result := map[string]any{"render_mode": mode, "render_url": s.renderURL(releaseID, ""), "release_id": releaseID, "page_id": pageID}
	if mode == "schema" {
		result["design"] = design
	}
	return result, nil
}
