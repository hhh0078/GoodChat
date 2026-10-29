// 文件作用说明：直接预览私有、公共和行业模板，使用十五分钟快照与临时文件，避免产生页面及永久资源包。
package decoration

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// registerTemplatePreviews 为可见模板注册统一短期预览接口，客服角色不能创建预览。
func (s *Service) registerTemplatePreviews(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/tenant/templates/{id}/preview", s.auth.RequireAdmin(s.previewTenantTemplate))
	mux.HandleFunc("POST /api/v1/tenant/template-industries/{key}/preview", s.auth.RequireAdmin(s.previewIndustryTemplate))
	mux.HandleFunc("POST /api/v1/platform/templates/{id}/preview", s.auth.RequirePlatform(s.previewPlatformTemplate))
}

// previewTenantTemplate 只读取本租户模板或已上架公共版本，不隐式复制模板或创建草稿页面。
func (s *Service) previewTenantTemplate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := r.PathValue("id")
	if err := idValue(id); err != nil {
		respond(w, r, nil, err)
		return
	}
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		var mode string
		var design json.RawMessage
		sourceID := id
		err := tx.QueryRow(r.Context(), "SELECT render_mode,design FROM tenant_templates WHERE id=$1", id).Scan(&mode, &design)
		if err == pgx.ErrNoRows {
			sourceID = ""
			err = tx.QueryRow(r.Context(), "SELECT render_mode,design FROM platform_templates WHERE id=$1 AND status='published'", id).Scan(&mode, &design)
		}
		if err != nil {
			return nil, err
		}
		return s.templatePreviewSnapshot(r.Context(), tx, platform.Current(r).TenantID, sourceID, mode, design)
	})
}

// previewPlatformTemplate 允许平台管理员预览未上架版本，短期凭证归属其当前工作空间。
func (s *Service) previewPlatformTemplate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := r.PathValue("id")
	if err := idValue(id); err != nil {
		respond(w, r, nil, err)
		return
	}
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		var mode string
		var design json.RawMessage
		if err := tx.QueryRow(r.Context(), "SELECT render_mode,design FROM platform_templates WHERE id=$1", id).Scan(&mode, &design); err != nil {
			return nil, err
		}
		return s.templatePreviewSnapshot(r.Context(), tx, platform.Current(r).TenantID, "", mode, design)
	})
}

// previewIndustryTemplate 以当前固定SDK生成官方行业预览，完整文件仅保留至短期预览过期。
func (s *Service) previewIndustryTemplate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		scripts, err := s.currentTemplateScripts(r.Context(), tx)
		if err != nil {
			return nil, err
		}
		pkg, err := industryTemplatePackage(r.PathValue("key"), scripts)
		if err != nil {
			return nil, err
		}
		return s.saveTemplatePreview(r.Context(), tx, platform.Current(r).TenantID, "", "code", json.RawMessage(`{}`), pkg)
	})
}

// templatePreviewSnapshot 将旧设计临时升级为可交互模拟预览，受保护私有包继续引用原始字节。
func (s *Service) templatePreviewSnapshot(ctx context.Context, tx pgx.Tx, tenant, sourceID, mode string, design json.RawMessage) (any, error) {
	var code CodeDesign
	if mode == "code" && json.Unmarshal(design, &code) == nil && code.PlatformPackageID != "" {
		pkg, err := loadPlatformTemplatePackage(ctx, tx, code.PlatformPackageID)
		if err != nil {
			return nil, err
		}
		return s.saveTemplatePreview(ctx, tx, tenant, sourceID, "code", json.RawMessage(`{}`), pkg)
	}
	normalized, err := validateDesign(mode, design)
	if err != nil {
		return nil, err
	}
	if err := checkPackageDesign(ctx, tx, mode, normalized); err != nil {
		return nil, err
	}
	var legacy *staticPackage
	if mode == "code" && code.PackageID != "" {
		var protected bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM decoration_package_files WHERE package_id=$1 AND path=$2)", code.PackageID, templateContractPath).Scan(&protected); err != nil {
			return nil, err
		}
		if protected {
			return s.saveTemplatePreview(ctx, tx, tenant, sourceID, mode, normalized, nil)
		}
		legacy, err = loadArchivePackage(ctx, tx, code)
		if err != nil {
			return nil, err
		}
	}
	scripts, err := s.currentTemplateScripts(ctx, tx)
	if err != nil {
		return nil, err
	}
	var pkg *staticPackage
	if legacy != nil {
		pkg, err = upgradeLegacyTemplatePackage(legacy, scripts)
	} else {
		pkg, err = exportTemplatePackage(mode, normalized, scripts)
	}
	if err != nil {
		return nil, err
	}
	return s.saveTemplatePreview(ctx, tx, tenant, sourceID, "code", json.RawMessage(`{}`), pkg)
}

// saveTemplatePreview 写入预览快照与可选临时静态文件，过期清理通过复合外键级联回收文件。
func (s *Service) saveTemplatePreview(ctx context.Context, tx pgx.Tx, tenant, sourceID, mode string, design json.RawMessage, pkg *staticPackage) (any, error) {
	if _, err := tx.Exec(ctx, "DELETE FROM decoration_previews WHERE expires_at<=now()"); err != nil {
		return nil, err
	}
	token, err := platform.Token()
	if err != nil {
		return nil, err
	}
	var entry any
	var manifest []byte
	if pkg != nil {
		entry = pkg.Entrypoint
		manifest, err = json.Marshal(pkg.Files)
		if err != nil {
			return nil, err
		}
	}
	var previewID string
	var expires time.Time
	var source any
	if sourceID != "" {
		source = sourceID
	}
	if err := tx.QueryRow(ctx, `INSERT INTO decoration_previews(tenant_id,render_mode,design,token_hash,ephemeral_entrypoint,ephemeral_manifest,source_template_id) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id::text,expires_at`, tenant, mode, design, digest([]byte(token)), entry, manifest, source).Scan(&previewID, &expires); err != nil {
		return nil, err
	}
	if pkg != nil {
		for _, file := range pkg.Files {
			if _, err := tx.Exec(ctx, "INSERT INTO decoration_preview_files(tenant_id,preview_id,path,content_type,content,sha256) VALUES($1,$2,$3,$4,$5,$6)", tenant, previewID, file.Path, file.ContentType, file.Content, file.SHA256); err != nil {
				return nil, err
			}
		}
	}
	return map[string]any{"preview_url": s.renderURL(previewID, token), "release_id": previewID, "expires_at": expires}, nil
}
