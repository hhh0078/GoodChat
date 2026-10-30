// 文件作用说明：为平台模板提供完整受保护源码读取和版本化人工修订，所有改动生成独立静态包并保留旧版本。
package decoration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/jackc/pgx/v5"
)

// platformTemplateRevisionInput 定义平台模板人工文件补丁，版本必填且不接受跨租户或原包覆盖字段。
type platformTemplateRevisionInput struct {
	Name       string             `json:"name"`
	Status     string             `json:"status"`
	Version    int64              `json:"version"`
	Entrypoint string             `json:"entrypoint"`
	Files      []packageFilePatch `json:"files"`
}

// loadPlatformTemplateSource 锁定仍在目录内的固定版本，将旧组件设计临时转换为受保护静态源码。
func (s *Service) loadPlatformTemplateSource(ctx context.Context, tx pgx.Tx, id string) (*staticPackage, string, string, int64, error) {
	var name, status, mode string
	var version int64
	var design json.RawMessage
	err := tx.QueryRow(ctx, "SELECT name,status,version,render_mode,design FROM platform_templates WHERE id=$1 AND archived_at IS NULL FOR SHARE", id).Scan(&name, &status, &version, &mode, &design)
	if err != nil {
		return nil, "", "", 0, err
	}
	var code CodeDesign
	if mode == "code" && json.Unmarshal(design, &code) == nil && code.PlatformPackageID != "" {
		pkg, err := loadPlatformTemplatePackage(ctx, tx, code.PlatformPackageID)
		return pkg, name, status, version, err
	}
	scripts, err := s.currentTemplateScripts(ctx, tx)
	if err != nil {
		return nil, "", "", 0, err
	}
	pkg, err := exportTemplatePackage(mode, design, scripts)
	return pkg, name, status, version, err
}

// platformTemplateSource 仅向平台管理员导出普通源码与图片原始字节，保护标记与租户编辑器保持一致。
func (s *Service) platformTemplateSource(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	id := r.PathValue("id")
	if err := idValue(id); err != nil {
		respond(w, r, nil, err)
		return
	}
	s.assistantScope(w, r, "platform", func(tx pgx.Tx) (any, error) {
		pkg, name, status, version, err := s.loadPlatformTemplateSource(r.Context(), tx, id)
		if err != nil {
			return nil, err
		}
		var packageID string
		if err := tx.QueryRow(r.Context(), "SELECT coalesce(platform_package_id::text,'') FROM platform_templates WHERE id=$1", id).Scan(&packageID); err != nil {
			return nil, err
		}
		metadata := map[string]any{"id": packageID, "name": name, "entrypoint": pkg.Entrypoint, "file_count": len(pkg.Files), "total_bytes": pkg.Total, "sha256": pkg.SHA256}
		return map[string]any{"id": id, "name": name, "status": status, "version": version, "package": metadata, "entrypoint": pkg.Entrypoint, "files": packageEditorFiles(pkg)}, nil
	})
}

// decodePlatformTemplateRevision 严格限制单个源码修订请求，图片base64开销包含在受控请求预算内。
func decodePlatformTemplateRevision(w http.ResponseWriter, r *http.Request, input *platformTemplateRevisionInput) error {
	r.Body = http.MaxBytesReader(w, r.Body, packageRevisionRequestLimit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		return invalidDesign("平台修订必须是单个合法JSON对象且不能超过28MiB")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return invalidDesign("平台修订只能包含一个JSON对象")
	}
	if input.Version < 1 {
		return invalidDesign("平台修订必须提供当前版本")
	}
	return nil
}

// revisePlatformTemplate 在同一事务内检查版本、校验完整静态包并追加平台历史，旧资源从不原地改写。
func (s *Service) revisePlatformTemplate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := idValue(id); err != nil {
		respond(w, r, nil, err)
		return
	}
	if !s.packageSlot(w, r) {
		return
	}
	defer func() { <-s.packageSlots }()
	var input platformTemplateRevisionInput
	if err := decodePlatformTemplateRevision(w, r, &input); err != nil {
		respond(w, r, nil, err)
		return
	}
	s.assistantScope(w, r, "platform", func(tx pgx.Tx) (any, error) {
		var currentVersion int64
		if err := tx.QueryRow(r.Context(), "SELECT version FROM platform_templates WHERE id=$1 AND archived_at IS NULL FOR UPDATE", id).Scan(&currentVersion); err != nil {
			return nil, err
		}
		if currentVersion != input.Version {
			return nil, conflict()
		}
		base, name, status, _, err := s.loadPlatformTemplateSource(r.Context(), tx, id)
		if err != nil {
			return nil, err
		}
		if input.Name != "" {
			name = input.Name
		}
		if err := validName(name); err != nil {
			return nil, err
		}
		if input.Status != "" {
			status, err = platformPackageStatus(input.Status)
			if err != nil {
				return nil, err
			}
		}
		pkg, err := mergePackageFiles(base, input.Entrypoint, input.Files)
		if err != nil {
			return nil, err
		}
		// 平台手工修订使用当前官方核心生成新快照；历史资源和普通编辑器的保护规则保持不变。
		scripts, err := s.currentTemplateScripts(r.Context(), tx)
		if err != nil {
			return nil, err
		}
		pkg, err = assistantRebuildCorePackage(pkg, scripts)
		if err != nil {
			return nil, err
		}
		packageID, err := storePlatformTemplatePackage(r.Context(), tx, pkg)
		if err != nil {
			return nil, err
		}
		design, _ := json.Marshal(CodeDesign{PlatformPackageID: packageID})
		if _, err := tx.Exec(r.Context(), "UPDATE platform_templates SET name=$2,status=$3,render_mode='code',design=$4,version=version+1,updated_at=now() WHERE id=$1", id, name, status, design); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(r.Context(), "INSERT INTO platform_template_versions(template_id,version,name,render_mode,design) SELECT id,version,name,render_mode,design FROM platform_templates WHERE id=$1", id); err != nil {
			return nil, err
		}
		return record(r.Context(), tx, "SELECT to_jsonb(t) FROM platform_templates t WHERE id=$1", id)
	})
}
