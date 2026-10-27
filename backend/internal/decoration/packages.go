// 文件作用说明：提供租户私有静态包的导入、列表、源码读取和不可变修订接口。
package decoration

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// packageRevisionInput 定义完整校验后克隆新包的文本变更，禁止修改原包字节。
type packageRevisionInput struct {
	Name       string `json:"name"`
	Entrypoint string `json:"entrypoint"`
	Files      []struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	} `json:"files"`
}

// registerPackages 注册管理员导入修订以及租户内私有素材读取接口。
func (s *Service) registerPackages(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/tenant/code-packages", s.auth.RequireAdmin(s.importPackage))
	mux.HandleFunc("GET /api/v1/tenant/code-packages", s.auth.RequireTenant(s.listPackages))
	mux.HandleFunc("GET /api/v1/tenant/code-packages/example", s.auth.RequireTenant(s.examplePackage))
	mux.HandleFunc("GET /api/v1/tenant/code-packages/{id}", s.auth.RequireTenant(s.getPackage))
	mux.HandleFunc("GET /api/v1/tenant/code-packages/{id}/files", s.auth.RequireTenant(s.getPackageFile))
	mux.HandleFunc("POST /api/v1/tenant/code-packages/{id}/revisions", s.auth.RequireAdmin(s.revisePackage))
	mux.HandleFunc("POST /api/v1/tenant/decoration-assets", s.auth.RequireAdmin(s.importDecorationAsset))
}

// packageSlot 约束解析和像素分配并发，不允许导入请求耗尽服务端内存。
func (s *Service) packageSlot(w http.ResponseWriter, r *http.Request) bool {
	select {
	case s.packageSlots <- struct{}{}:
		return true
	default:
		respond(w, r, nil, platform.Fail(429, "import_busy", "当前正在处理其他资源，请稍后重试"))
		return false
	}
}

// readPackageMultipart 直接流式读取受限multipart，不在磁盘创建临时文件。
func readPackageMultipart(w http.ResponseWriter, r *http.Request, limit int) ([]byte, map[string]string, error) {
	r.Body = http.MaxBytesReader(w, r.Body, int64(limit+65536))
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, nil, invalidDesign("请使用multipart上传file文件")
	}
	fields := map[string]string{}
	var raw []byte
	count := 0
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, invalidDesign("上传格式不完整或超过大小限制")
		}
		count++
		if count > 4 {
			return nil, nil, invalidDesign("上传字段过多")
		}
		name := part.FormName()
		if name != "file" && name != "name" && name != "entrypoint" {
			return nil, nil, invalidDesign("上传包含未知字段")
		}
		if _, ok := fields[name]; ok {
			return nil, nil, invalidDesign("上传字段不能重复")
		}
		fields[name] = ""
		n := 1024
		if name == "file" {
			n = limit
		}
		content, readErr := io.ReadAll(io.LimitReader(part, int64(n+1)))
		_ = part.Close()
		if readErr != nil || len(content) > n {
			return nil, nil, invalidDesign("上传内容超过大小限制")
		}
		if name == "file" {
			raw = content
		} else {
			fields[name] = string(content)
		}
	}
	if len(raw) == 0 {
		return nil, nil, invalidDesign("请选择非空文件")
	}
	return raw, fields, nil
}

// importPackage 校验整个ZIP后在单个租户事务写入不可变包，失败不留下半包。
func (s *Service) importPackage(w http.ResponseWriter, r *http.Request) {
	if !s.packageSlot(w, r) {
		return
	}
	defer func() { <-s.packageSlots }()
	raw, fields, err := readPackageMultipart(w, r, packageZIPLimit)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	if err := validName(fields["name"]); err != nil {
		respond(w, r, nil, err)
		return
	}
	pkg, err := readStaticZIP(raw, fields["entrypoint"])
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		if err := s.checkFeature(r.Context(), tx, platform.Current(r).TenantID, "advanced_decoration"); err != nil {
			return nil, err
		}
		return s.storePackage(r.Context(), tx, platform.Current(r), fields["name"], pkg)
	})
}

// storePackage 原子保存完整清单与字节，返回供草稿引用的私有编号。
func (s *Service) storePackage(ctx context.Context, tx pgx.Tx, p platform.Principal, name string, pkg *staticPackage) (any, error) {
	manifest, _ := json.Marshal(pkg.Files)
	var id string
	if err := tx.QueryRow(ctx, `INSERT INTO decoration_code_packages(tenant_id,owner_membership_id,name,entrypoint,manifest,file_count,total_bytes,sha256) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id::text`, p.TenantID, p.MembershipID, name, pkg.Entrypoint, manifest, len(pkg.Files), pkg.Total, pkg.SHA256).Scan(&id); err != nil {
		return nil, err
	}
	for _, file := range pkg.Files {
		if _, err := tx.Exec(ctx, `INSERT INTO decoration_package_files(tenant_id,package_id,path,content_type,sha256,content) VALUES($1,$2,$3,$4,$5,$6)`, p.TenantID, id, file.Path, file.ContentType, file.SHA256, file.Content); err != nil {
			return nil, err
		}
	}
	return packageResult(ctx, tx, id)
}

// packageResult 返回不含原始字节的资源包元数据与代码设计引用。
func packageResult(ctx context.Context, tx pgx.Tx, id string) (any, error) {
	item, err := record(ctx, tx, "SELECT to_jsonb(p)-'creation_xid' FROM decoration_code_packages p WHERE id=$1", id)
	return map[string]any{"package": item, "design": CodeDesign{PackageID: id}}, err
}

// listPackages 列出租户最近的资源包，绝不通过公开渲染站点枚举私有素材。
func (s *Service) listPackages(w http.ResponseWriter, r *http.Request) {
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		rows, err := tx.Query(r.Context(), "SELECT to_jsonb(p)-'creation_xid' FROM decoration_code_packages p ORDER BY created_at DESC,id LIMIT 200")
		if err != nil {
			return nil, err
		}
		items, err := jsonRows(rows)
		return map[string]any{"items": items}, err
	})
}

// getPackage 读取本租户资源清单，跨租户编号返回不存在。
func (s *Service) getPackage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := idValue(id); err != nil {
		respond(w, r, nil, err)
		return
	}
	s.tenant(w, r, func(tx pgx.Tx) (any, error) { return packageResult(r.Context(), tx, id) })
}

// getPackageFile 仅经认证返回私有源码或受限二进制data URL，不发送管理员凭证给装修站点。
func (s *Service) getPackageFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := idValue(id); err != nil {
		respond(w, r, nil, err)
		return
	}
	name := r.URL.Query().Get("path")
	if err := packagePath(name); err != nil {
		respond(w, r, nil, err)
		return
	}
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		var mime, hash string
		var content []byte
		if err := tx.QueryRow(r.Context(), "SELECT content_type,sha256,content FROM decoration_package_files WHERE package_id=$1 AND path=$2", id, name).Scan(&mime, &hash, &content); err != nil {
			return nil, err
		}
		result := map[string]any{"path": name, "content_type": mime, "sha256": hash, "size": len(content)}
		if strings.HasPrefix(mime, "text/") || mime == "application/json" {
			result["content"] = string(content)
		} else {
			result["data_url"] = "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(content)
		}
		return result, nil
	})
}

// revisePackage 将源码改动合并到新包后再次校验全部依赖，原包与旧发布保持不变。
func (s *Service) revisePackage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := idValue(id); err != nil {
		respond(w, r, nil, err)
		return
	}
	var input packageRevisionInput
	if !s.packageSlot(w, r) {
		return
	}
	defer func() { <-s.packageSlots }()
	if err := decodePackageRevision(w, r, &input); err != nil {
		respond(w, r, nil, err)
		return
	}
	if len(input.Files) > packageFileCount {
		respond(w, r, nil, invalidDesign("一次最多修改200个文本资源"))
		return
	}
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		if err := s.checkFeature(r.Context(), tx, platform.Current(r).TenantID, "advanced_decoration"); err != nil {
			return nil, err
		}
		var name, entry string
		if err := tx.QueryRow(r.Context(), "SELECT name,entrypoint FROM decoration_code_packages WHERE id=$1", id).Scan(&name, &entry); err != nil {
			return nil, err
		}
		if input.Name != "" {
			name = input.Name
		}
		if err := validName(name); err != nil {
			return nil, err
		}
		if input.Entrypoint != "" {
			entry = input.Entrypoint
		}
		rows, err := tx.Query(r.Context(), "SELECT path,content_type,sha256,content FROM decoration_package_files WHERE package_id=$1 ORDER BY path", id)
		if err != nil {
			return nil, err
		}
		files := map[string]packageFile{}
		for rows.Next() {
			var file packageFile
			if err := rows.Scan(&file.Path, &file.ContentType, &file.SHA256, &file.Content); err != nil {
				rows.Close()
				return nil, err
			}
			file.Size = len(file.Content)
			files[file.Path] = file
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		changed := map[string]bool{}
		for _, change := range input.Files {
			if changed[change.Path] {
				return nil, invalidDesign("修改文件路径不能重复")
			}
			changed[change.Path] = true
			file, err := checkStaticFile(change.Path, []byte(change.Content))
			if err != nil {
				return nil, err
			}
			if !strings.HasPrefix(file.ContentType, "text/") && file.ContentType != "application/json" {
				return nil, invalidDesign("源码修订仅支持文本；更换二进制素材请重新导入ZIP")
			}
			files[change.Path] = file
		}
		all := make([]packageFile, 0, len(files))
		for _, file := range files {
			all = append(all, file)
		}
		pkg, err := finishPackage(entry, all)
		if err != nil {
			return nil, err
		}
		return s.storePackage(r.Context(), tx, platform.Current(r), name, pkg)
	})
}

// decodePackageRevision 对源码修订单独限制为20MiB请求预算，避免普通JSON接口的1MiB上限截断合法资源。
func decodePackageRevision(w http.ResponseWriter, r *http.Request, input *packageRevisionInput) error {
	r.Body = http.MaxBytesReader(w, r.Body, packageTotalLimit+65536)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		return invalidDesign("源码修订必须是单个JSON对象且请求不能超过20MiB")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return invalidDesign("源码修订只能包含一个JSON对象")
	}
	return nil
}

// checkPackageDesign 检查包归属并验证可选入口覆盖源码，生成列复合外键作为第二层隔离。
func checkPackageDesign(ctx context.Context, tx pgx.Tx, mode string, design json.RawMessage) error {
	if mode != "code" {
		return nil
	}
	var code CodeDesign
	if json.Unmarshal(design, &code) != nil || code.PackageID == "" {
		return nil
	}
	var entry string
	var raw json.RawMessage
	if err := tx.QueryRow(ctx, "SELECT entrypoint,manifest FROM decoration_code_packages WHERE id=$1", code.PackageID).Scan(&entry, &raw); err != nil {
		return err
	}
	var manifest []packageFile
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return err
	}
	files := map[string]packageFile{}
	for _, file := range manifest {
		files[file.Path] = file
	}
	if code.HTML != "" {
		if _, err := packageHTML([]byte(code.HTML), entry, "", "", "", files); err != nil {
			return err
		}
	}
	if _, err := packageCSS(code.CSS, entry, "", files); err != nil {
		return err
	}
	return packageJS(code.Javascript, entry, files)
}
