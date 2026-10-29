// 文件作用说明：将模板导出为无需构建的完整静态ZIP，并将修改后的ZIP导入为新的租户私有模板。
package decoration

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// templateArchiveMetadata 描述可移植模板，不携带租户、身份凭证或模型配置。
type templateArchiveMetadata struct {
	SchemaVersion int    `json:"schema_version"`
	Name          string `json:"name"`
	Entrypoint    string `json:"entrypoint"`
	SDKVersion    string `json:"sdk_version,omitempty"`
}

// storedPackageID 从现有资源包保存结果中读取新包编号，避免依赖具体JSON表现类型。
func storedPackageID(stored any) (string, error) {
	raw, err := json.Marshal(stored)
	if err != nil {
		return "", err
	}
	var result struct {
		Design CodeDesign `json:"design"`
	}
	if json.Unmarshal(raw, &result) != nil || idValue(result.Design.PackageID) != nil {
		return "", fmt.Errorf("资源包保存结果缺少有效编号")
	}
	return result.Design.PackageID, nil
}

// exportTemplate 在认证租户事务内读取全部源码与二进制文件，再以ZIP响应下载。
func (s *Service) exportTemplate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := idValue(id); err != nil {
		respond(w, r, nil, err)
		return
	}
	var name string
	var archive []byte
	p := platform.Current(r)
	err := platform.TenantTx(r.Context(), s.pool, p.TenantID, func(tx pgx.Tx) error {
		var mode string
		var design json.RawMessage
		if err := tx.QueryRow(r.Context(), "SELECT name,render_mode,design FROM tenant_templates WHERE id=$1", id).Scan(&name, &mode, &design); err != nil {
			return err
		}
		if err := s.checkFeature(r.Context(), tx, p.TenantID, decorationFeature(mode)); err != nil {
			return err
		}
		var code CodeDesign
		_ = json.Unmarshal(design, &code)
		var pkg *staticPackage
		var err error
		if mode == "code" && code.PackageID != "" {
			if err := checkPackageDesign(r.Context(), tx, mode, design); err != nil {
				return err
			}
			pkg, err = loadArchivePackage(r.Context(), tx, code)
			if err == nil && !packageHasContract(pkg) {
				scripts, scriptErr := s.currentTemplateScripts(r.Context(), tx)
				if scriptErr != nil {
					return scriptErr
				}
				pkg, err = upgradeLegacyTemplatePackage(pkg, scripts)
			}
		} else {
			scripts, scriptErr := s.currentTemplateScripts(r.Context(), tx)
			if scriptErr != nil {
				return scriptErr
			}
			pkg, err = exportTemplatePackage(mode, design, scripts)
		}
		if err != nil {
			return err
		}
		archive, err = encodeTemplateArchive(name, pkg)
		return err
	})
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	filename := strings.ReplaceAll(filepath.Base(name), "/", "_") + ".zip"
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(archive)
}

// loadArchivePackage 读取选定资源包全部文件，并把既有入口覆盖内容固化进导出快照。
func loadArchivePackage(ctx context.Context, tx pgx.Tx, code CodeDesign) (*staticPackage, error) {
	var entry string
	if err := tx.QueryRow(ctx, "SELECT entrypoint FROM decoration_code_packages WHERE id=$1", code.PackageID).Scan(&entry); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, "SELECT path,content FROM decoration_package_files WHERE package_id=$1 ORDER BY path", code.PackageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	files := []packageFile{}
	for rows.Next() {
		var name string
		var content []byte
		if err := rows.Scan(&name, &content); err != nil {
			return nil, err
		}
		if name == entry && (code.HTML != "" || code.CSS != "" || code.Javascript != "") {
			if code.HTML != "" {
				content = []byte(code.HTML)
			}
			// 旧内联覆盖被固化为独立文件，新版受保护模板在checkPackageDesign中已禁止覆盖。
			if code.CSS != "" {
				style, err := checkStaticFile("static/export-overrides.css", []byte(code.CSS))
				if err != nil {
					return nil, err
				}
				files = append(files, style)
				content = append(content, []byte(`<link rel="stylesheet" href="/static/export-overrides.css">`)...)
			}
			if code.Javascript != "" {
				script, err := checkStaticFile("static/export-overrides.js", []byte(code.Javascript))
				if err != nil {
					return nil, err
				}
				files = append(files, script)
				content = append(content, []byte(`<script src="/static/export-overrides.js"></script>`)...)
			}
		}
		file, err := checkStaticFile(name, content)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return finishPackage(entry, files)
}

// encodeTemplateArchive 将完整包与可移植清单写入ZIP，不导出服务端的资源编号或访问令牌。
func encodeTemplateArchive(name string, pkg *staticPackage) ([]byte, error) {
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, file := range pkg.Files {
		if file.Path == "template.json" {
			continue
		}
		stream, err := writer.Create(file.Path)
		if err != nil {
			return nil, err
		}
		if _, err := stream.Write(file.Content); err != nil {
			return nil, err
		}
	}
	metadata, _ := json.MarshalIndent(templateArchiveMetadata{SchemaVersion: 1, Name: name, Entrypoint: pkg.Entrypoint}, "", "  ")
	stream, err := writer.Create("template.json")
	if err != nil {
		return nil, err
	}
	if _, err := stream.Write(metadata); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

// packageHasContract 判断包是否已使用平台固定脚本契约，既有旧包仅在导出时升级。
func packageHasContract(pkg *staticPackage) bool {
	for _, file := range pkg.Files {
		if file.Path == "goodchat-contract.json" {
			return true
		}
	}
	return false
}

// templateZIPMetadata 限量读取可移植清单，完整文件安全检查仍由readStaticZIP执行。
func templateZIPMetadata(raw []byte) (templateArchiveMetadata, error) {
	var metadata templateArchiveMetadata
	if len(raw) == 0 || len(raw) > packageZIPLimit {
		return metadata, invalidDesign("ZIP不能为空且不能超过10MiB")
	}
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return metadata, invalidDesign("无法读取模板ZIP")
	}
	found := false
	for _, file := range reader.File {
		if file.Name != "template.json" {
			continue
		}
		if found || file.UncompressedSize64 > 16384 {
			return metadata, invalidDesign("模板清单重复或过大")
		}
		found = true
		stream, err := file.Open()
		if err != nil {
			return metadata, invalidDesign("无法读取模板清单")
		}
		body, readErr := io.ReadAll(io.LimitReader(stream, 16385))
		_ = stream.Close()
		if readErr != nil || len(body) > 16384 || json.Unmarshal(body, &metadata) != nil || metadata.SchemaVersion != 1 {
			return metadata, invalidDesign("模板清单格式或版本不支持")
		}
		if err := packagePath(metadata.Entrypoint); err != nil {
			return metadata, err
		}
	}
	return metadata, nil
}

// importTemplate 校验修改后的完整ZIP并创建新模板，任何失败都不覆盖原来的版本。
func (s *Service) importTemplate(w http.ResponseWriter, r *http.Request) {
	if !s.packageSlot(w, r) {
		return
	}
	defer func() { <-s.packageSlots }()
	raw, fields, err := readPackageMultipart(w, r, packageZIPLimit)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	metadata, err := templateZIPMetadata(raw)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	entry := fields["entrypoint"]
	if entry == "" {
		entry = metadata.Entrypoint
	}
	if entry == "" {
		entry = "index.html"
	}
	pkg, err := readStaticZIP(raw, entry)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	name := fields["name"]
	if metadata.Entrypoint != "" && metadata.Entrypoint != pkg.Entrypoint {
		respond(w, r, nil, invalidDesign("模板清单入口与导入入口不一致"))
		return
	}
	if name == "" {
		name = metadata.Name
	}
	if err := validName(name); err != nil {
		respond(w, r, nil, err)
		return
	}
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		ctx, p := r.Context(), platform.Current(r)
		if err := s.checkFeature(ctx, tx, p.TenantID, "advanced_decoration"); err != nil {
			return nil, err
		}
		if err := verifyNewTemplatePackage(ctx, tx, pkg); err != nil {
			return nil, err
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
