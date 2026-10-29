// 文件作用说明：以平台独立不可变资源包管理公开模板，行业模板及明确上架的私有作品均复制实际字节，不跨租户引用私有包。
package decoration

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// registerPlatformTemplatePackages 注册仅平台管理员可执行的官方模板初始化及显式私有作品上架接口。
func (s *Service) registerPlatformTemplatePackages(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/platform/template-industries/{key}/copy", s.auth.RequirePlatform(s.createPlatformIndustryTemplate))
	mux.HandleFunc("POST /api/v1/platform/templates/from-private", s.auth.RequirePlatform(s.publishPrivatePlatformTemplate))
}

// platformPackageInput 定义公开模板创建参数，来源必须由明确动作指定，不接受任意租户编号。
type platformPackageInput struct {
	Name             string `json:"name"`
	Status           string `json:"status"`
	SourceTemplateID string `json:"source_template_id,omitempty"`
}

// platformPackageStatus 校验显式上架状态，不把未知状态静默转换为公开。
func platformPackageStatus(status string) (string, error) {
	if status == "" {
		status = "hidden"
	}
	if status != "published" && status != "hidden" {
		return "", platform.Fail(400, "invalid_status", "平台模板状态只能是published或hidden")
	}
	return status, nil
}

// storePlatformTemplatePackage 校验并原子保存平台独立包，文件与SDK字节按本版本冻结。
func storePlatformTemplatePackage(ctx context.Context, tx pgx.Tx, pkg *staticPackage) (string, error) {
	if err := verifyNewTemplatePackage(ctx, tx, pkg); err != nil {
		return "", err
	}
	manifest, _ := json.Marshal(pkg.Files)
	var id string
	if err := tx.QueryRow(ctx, "INSERT INTO platform_template_packages(entrypoint,manifest,file_count,total_bytes,sha256) VALUES($1,$2,$3,$4,$5) RETURNING id::text", pkg.Entrypoint, manifest, len(pkg.Files), pkg.Total, pkg.SHA256).Scan(&id); err != nil {
		return "", err
	}
	for _, file := range pkg.Files {
		if _, err := tx.Exec(ctx, "INSERT INTO platform_template_package_files(package_id,path,content_type,sha256,content) VALUES($1,$2,$3,$4,$5)", id, file.Path, file.ContentType, file.SHA256, file.Content); err != nil {
			return "", err
		}
	}
	return id, nil
}

// loadPlatformTemplatePackage 只读取独立平台包，并验证实际文件、冻结清单和已登记SDK版本；不接触租户私有表。
func loadPlatformTemplatePackage(ctx context.Context, tx pgx.Tx, id string) (*staticPackage, error) {
	if err := idValue(id); err != nil {
		return nil, err
	}
	var entry, hash string
	var count int
	var total int64
	if err := tx.QueryRow(ctx, "SELECT entrypoint,sha256,file_count,total_bytes FROM platform_template_packages WHERE id=$1", id).Scan(&entry, &hash, &count, &total); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, "SELECT path,content FROM platform_template_package_files WHERE package_id=$1 ORDER BY path", id)
	if err != nil {
		return nil, err
	}
	var files []packageFile
	for rows.Next() {
		var name string
		var content []byte
		if err := rows.Scan(&name, &content); err != nil {
			rows.Close()
			return nil, err
		}
		file, err := checkStaticFile(name, content)
		if err != nil {
			rows.Close()
			return nil, err
		}
		files = append(files, file)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	pkg, err := finishPackage(entry, files)
	if err != nil {
		return nil, err
	}
	if pkg.SHA256 != hash || len(pkg.Files) != count || int64(pkg.Total) != total {
		return nil, invalidDesign("平台模板资源与冻结清单不一致")
	}
	if err := verifyNewTemplatePackage(ctx, tx, pkg); err != nil {
		return nil, err
	}
	return pkg, nil
}

// savePlatformPackageTemplate 新建公开模板及其第一份不可变历史版本。
func savePlatformPackageTemplate(ctx context.Context, tx pgx.Tx, name, status, industry string, pkg *staticPackage) (json.RawMessage, error) {
	packageID, err := storePlatformTemplatePackage(ctx, tx, pkg)
	if err != nil {
		return nil, err
	}
	design, _ := json.Marshal(CodeDesign{PlatformPackageID: packageID})
	var id string
	if err := tx.QueryRow(ctx, "INSERT INTO platform_templates(name,render_mode,design,status,industry_key) VALUES($1,'code',$2,$3,NULLIF($4,'')) RETURNING id::text", name, design, status, industry).Scan(&id); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO platform_template_versions(template_id,version,name,render_mode,design) SELECT id,version,name,render_mode,design FROM platform_templates WHERE id=$1", id); err != nil {
		return nil, err
	}
	return record(ctx, tx, "SELECT to_jsonb(t) FROM platform_templates t WHERE id=$1", id)
}

// createPlatformIndustryTemplate 按行业幂等创建默认公开静态模板，重复调用返回既有作品而不覆盖已编辑版本。
func (s *Service) createPlatformIndustryTemplate(w http.ResponseWriter, r *http.Request) {
	var input platformPackageInput
	if err := platform.Decode(w, r, &input); err != nil {
		respond(w, r, nil, err)
		return
	}
	key := r.PathValue("key")
	name := ""
	for _, item := range industryTemplateCatalog() {
		if item.Key == key {
			name = item.Name
			break
		}
	}
	if name == "" {
		respond(w, r, nil, platform.Fail(404, "not_found", "未找到该行业模板"))
		return
	}
	if input.SourceTemplateID != "" {
		respond(w, r, nil, invalidDesign("行业初始化不接受私有模板来源"))
		return
	}
	if input.Name != "" {
		name = input.Name
	}
	if err := validName(name); err != nil {
		respond(w, r, nil, err)
		return
	}
	status, err := platformPackageStatus(input.Status)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	s.assistantScope(w, r, "platform", func(tx pgx.Tx) (any, error) {
		if _, err := tx.Exec(r.Context(), "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "platform-template-industry:"+key); err != nil {
			return nil, err
		}
		existing, err := record(r.Context(), tx, "SELECT to_jsonb(t) FROM platform_templates t WHERE industry_key=$1", key)
		if err == nil {
			return existing, nil
		}
		if err != pgx.ErrNoRows {
			return nil, err
		}
		scripts, err := s.currentTemplateScripts(r.Context(), tx)
		if err != nil {
			return nil, err
		}
		pkg, err := industryTemplatePackage(key, scripts)
		if err != nil {
			return nil, err
		}
		return savePlatformPackageTemplate(r.Context(), tx, name, status, key, pkg)
	})
}

// publishPrivatePlatformTemplate 仅允许平台管理员明确公开自己当前管理租户的作品，并复制为完全独立的平台资源。
func (s *Service) publishPrivatePlatformTemplate(w http.ResponseWriter, r *http.Request) {
	p := platform.Current(r)
	if p.TenantID == "" || p.Role != "owner" && p.Role != "admin" {
		respond(w, r, nil, platform.Fail(403, "admin_required", "请在自己管理的租户工作区明确选择要上架的私有模板"))
		return
	}
	var input platformPackageInput
	if err := platform.Decode(w, r, &input); err != nil {
		respond(w, r, nil, err)
		return
	}
	if err := idValue(input.SourceTemplateID); err != nil {
		respond(w, r, nil, err)
		return
	}
	if err := validName(input.Name); err != nil {
		respond(w, r, nil, err)
		return
	}
	status, err := platformPackageStatus(input.Status)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		var mode string
		var design json.RawMessage
		if err := tx.QueryRow(r.Context(), "SELECT render_mode,design FROM tenant_templates WHERE id=$1 AND status='ready' FOR KEY SHARE", input.SourceTemplateID).Scan(&mode, &design); err != nil {
			return nil, err
		}
		var code CodeDesign
		_ = json.Unmarshal(design, &code)
		var pkg *staticPackage
		var err error
		if mode == "code" && code.PackageID != "" {
			pkg, err = loadArchivePackage(r.Context(), tx, code)
		} else {
			scripts, e := s.currentTemplateScripts(r.Context(), tx)
			if e != nil {
				return nil, e
			}
			pkg, err = exportTemplatePackage(mode, design, scripts)
		}
		if err != nil {
			return nil, err
		}
		return savePlatformPackageTemplate(r.Context(), tx, input.Name, status, "", pkg)
	})
}
