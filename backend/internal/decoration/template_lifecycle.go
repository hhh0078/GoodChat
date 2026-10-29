// 文件作用说明：提供行业模板物化、模板使用位置与经过引用检查的私有模板删除接口。
package decoration

import (
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"supportdesk/backend/internal/platform"
)

// registerTemplateLifecycle 注册仅管理员可以执行的模板复制、导入、导出与删除接口。
func (s *Service) registerTemplateLifecycle(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/tenant/template-industries", s.auth.RequireAdmin(s.templateIndustries))
	mux.HandleFunc("POST /api/v1/tenant/template-industries/{key}/copy", s.auth.RequireAdmin(s.copyIndustryTemplate))
	mux.HandleFunc("GET /api/v1/tenant/templates/{id}/usage", s.auth.RequireAdmin(s.templateUsage))
	mux.HandleFunc("GET /api/v1/tenant/templates/{id}/export", s.auth.RequireAdmin(s.exportTemplate))
	mux.HandleFunc("POST /api/v1/tenant/templates/import", s.auth.RequireAdmin(s.importTemplate))
	mux.HandleFunc("DELETE /api/v1/tenant/templates/{id}", s.auth.RequireAdmin(s.deleteTemplate))
}

// templateIndustries 返回平台预置行业设计，不向租户暴露其他租户的私有资源包。
func (s *Service) templateIndustries(w http.ResponseWriter, r *http.Request) {
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		return map[string]any{"items": industryTemplateCatalog()}, nil
	})
}

// copyIndustryTemplate 用当前平台脚本快照生成本租户自己的完整静态包与模板。
func (s *Service) copyIndustryTemplate(w http.ResponseWriter, r *http.Request) {
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		ctx, p := r.Context(), platform.Current(r)
		if err := s.checkFeature(ctx, tx, p.TenantID, "advanced_decoration"); err != nil {
			return nil, err
		}
		scripts, err := s.currentTemplateScripts(ctx, tx)
		if err != nil {
			return nil, err
		}
		pkg, err := industryTemplatePackage(r.PathValue("key"), scripts)
		if err != nil {
			return nil, err
		}
		name := "行业咨询模板"
		for _, item := range industryTemplateCatalog() {
			if item.Key == r.PathValue("key") {
				name = item.Name
			}
		}
		stored, err := s.storePackage(ctx, tx, p, name, pkg)
		if err != nil {
			return nil, err
		}
		packageID, err := storedPackageID(stored)
		if err != nil {
			return nil, err
		}
		return record(ctx, tx, `INSERT INTO tenant_templates(tenant_id,owner_membership_id,name,render_mode,design) VALUES($1,$2,$3,'code',jsonb_build_object('package_id',$4::text)) RETURNING to_jsonb(tenant_templates)`, p.TenantID, p.MembershipID, name, packageID)
	})
}

// templateUsage 列出所有仍绑定的入口、页面及活动任务，停用入口也计入引用。
func (s *Service) templateUsage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := idValue(id); err != nil {
		respond(w, r, nil, err)
		return
	}
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		var found string
		if err := tx.QueryRow(r.Context(), "SELECT id::text FROM tenant_templates WHERE id=$1", id).Scan(&found); err != nil {
			return nil, err
		}
		rows, err := tx.Query(r.Context(), `SELECT jsonb_build_object('id',l.id,'name',l.name,'status',l.status,'binding_mode',l.binding_mode,'page_id',l.page_id) FROM access_links l JOIN decoration_pages p ON p.id=l.page_id AND p.tenant_id=l.tenant_id WHERE p.template_id=$1 ORDER BY l.name,l.id`, id)
		if err != nil {
			return nil, err
		}
		links, err := jsonRows(rows)
		if err != nil {
			return nil, err
		}
		rows, err = tx.Query(r.Context(), `SELECT jsonb_build_object('id',id,'name',name,'current_release_id',current_release_id) FROM decoration_pages WHERE template_id=$1 ORDER BY created_at,id`, id)
		if err != nil {
			return nil, err
		}
		pages, err := jsonRows(rows)
		if err != nil {
			return nil, err
		}
		rows, err = tx.Query(r.Context(), `SELECT jsonb_build_object('id',id,'status',status,'source','legacy') FROM ai_design_jobs WHERE template_id=$1 AND status IN('queued','running','validating') UNION ALL SELECT jsonb_build_object('id',j.id,'status',j.status,'source','assistant') FROM assistant_jobs j JOIN assistant_threads t ON t.id=j.thread_id AND t.tenant_id=j.tenant_id WHERE j.status IN('queued','running','validating') AND (j.base_template_id=$1 OR t.template_id=$1 OR t.page_id IN(SELECT id FROM decoration_pages WHERE template_id=$1))`, id)
		if err != nil {
			return nil, err
		}
		jobs, err := jsonRows(rows)
		return map[string]any{"id": id, "links": links, "pages": pages, "active_jobs": jobs, "can_delete": len(links) == 0 && len(jobs) == 0}, err
	})
}

// deleteTemplate 调用受控数据库事务清理模板与无引用文件，任何冲突都完整回滚。
func (s *Service) deleteTemplate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := idValue(id); err != nil {
		respond(w, r, nil, err)
		return
	}
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		result, err := record(r.Context(), tx, "SELECT decoration_delete_template($1::uuid,$2::uuid)", platform.Current(r).TenantID, id)
		if databaseError, ok := err.(*pgconn.PgError); ok {
			switch databaseError.Code {
			case "P0002":
				return nil, pgx.ErrNoRows
			case "23503", "40P01", "40001":
				return nil, platform.Fail(409, "template_in_use", "模板仍有链接、活动AI任务或并发绑定，请先检查使用位置并解除绑定后重试")
			}
		}
		return result, err
	})
}
