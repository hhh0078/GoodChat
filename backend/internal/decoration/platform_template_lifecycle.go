// 文件作用说明：提供平台模板使用计数与版本受控移除，保留租户既有作品并回收真正无引用的公共资源。
package decoration

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgconn"
	"supportdesk/backend/internal/platform"
)

// registerPlatformTemplateLifecycle 注册仅平台管理员可以使用的目录移除及不可变源码修订接口。
func (s *Service) registerPlatformTemplateLifecycle(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/platform/templates/{id}/usage", s.auth.RequirePlatform(s.platformTemplateUsage))
	mux.HandleFunc("DELETE /api/v1/platform/templates/{id}", s.auth.RequirePlatform(s.deletePlatformTemplate))
	mux.HandleFunc("GET /api/v1/platform/templates/{id}/source", s.auth.RequirePlatform(s.platformTemplateSource))
	mux.HandleFunc("POST /api/v1/platform/templates/{id}/revisions", s.auth.RequirePlatform(s.revisePlatformTemplate))
}

// platformLifecycleError 将受控清理的不存在和并发冲突转换为稳定接口错误。
func platformLifecycleError(err error) error {
	if databaseError, ok := err.(*pgconn.PgError); ok {
		switch databaseError.Code {
		case "P0002":
			return platform.Fail(404, "not_found", "平台模板已移除或不存在")
		case "23503", "40P01", "40001":
			return platform.Fail(409, "template_conflict", "平台模板已被修改或正在被使用，请刷新后重试")
		}
	}
	return err
}

// platformTemplateUsage 只返回跨租户来源的数量，不暴露副本、入口或租户身份。
func (s *Service) platformTemplateUsage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := r.PathValue("id")
	if err := idValue(id); err != nil {
		respond(w, r, nil, err)
		return
	}
	var result any
	err := s.pool.QueryRow(r.Context(), "SELECT platform_template_usage($1::uuid)", id).Scan(&result)
	respond(w, r, result, platformLifecycleError(err))
}

// deletePlatformTemplate 按调用者看到的版本移除目录，不解绑或改写任何租户页面。
func (s *Service) deletePlatformTemplate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := idValue(id); err != nil {
		respond(w, r, nil, err)
		return
	}
	var input struct {
		Version int64 `json:"version"`
	}
	if err := platform.Decode(w, r, &input); err != nil {
		respond(w, r, nil, err)
		return
	}
	if input.Version < 1 {
		respond(w, r, nil, platform.Fail(400, "version_required", "移除平台模板必须提供当前版本"))
		return
	}
	var result any
	err := s.pool.QueryRow(r.Context(), "SELECT platform_delete_template($1::uuid,$2)", id, input.Version).Scan(&result)
	respond(w, r, result, platformLifecycleError(err))
}
