// 文件作用说明：为私有及平台模板创建短期编辑快照，校验完整文件后仅注入受控元素选择通道。
package decoration

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"io"
	"net/http"

	"github.com/jackc/pgx/v5"
	xhtml "golang.org/x/net/html"
	"supportdesk/backend/internal/platform"
)

//go:embed editor_preview.js
var editorPreviewScript string

// packageEditorPreviewInput 定义编辑器尚未保存的文件修改和一次性窗口握手标识。
type packageEditorPreviewInput struct {
	Entrypoint  string             `json:"entrypoint,omitempty"`
	Files       []packageFilePatch `json:"files"`
	EditorNonce string             `json:"editor_nonce"`
}

// registerEditorPreviews 只允许管理员准备编辑快照，不向客服开放源码写入或选择通道。
func (s *Service) registerEditorPreviews(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/tenant/code-packages/{id}/editor-preview", s.auth.RequireAdmin(s.previewEditorPackage))
	mux.HandleFunc("POST /api/v1/platform/templates/{id}/editor-preview", s.auth.RequirePlatform(s.previewEditorPlatform))
}

// decodeEditorPreview 限制包含base64的请求预算并拒绝未知字段、重复对象及错误握手标识。
func decodeEditorPreview(w http.ResponseWriter, r *http.Request, input *packageEditorPreviewInput) error {
	r.Body = http.MaxBytesReader(w, r.Body, packageRevisionRequestLimit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil || decoder.Decode(new(any)) != io.EOF {
		return invalidDesign("编辑预览须为单个合法JSON对象，不能超过28MiB")
	}
	if idValue(input.EditorNonce) != nil {
		return invalidDesign("编辑预览必须提供新的UUID握手标识")
	}
	return nil
}

// previewEditorPackage 在当前租户内合并草稿源码，只生成十五分钟快照而不创建永久资源包。
func (s *Service) previewEditorPackage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := idValue(id); err != nil {
		respond(w, r, nil, err)
		return
	}
	if !s.packageSlot(w, r) {
		return
	}
	defer func() { <-s.packageSlots }()
	var input packageEditorPreviewInput
	if err := decodeEditorPreview(w, r, &input); err != nil {
		respond(w, r, nil, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		if err := s.checkFeature(r.Context(), tx, platform.Current(r).TenantID, "advanced_decoration"); err != nil {
			return nil, err
		}
		base, err := loadArchivePackage(r.Context(), tx, CodeDesign{PackageID: id})
		if err != nil {
			return nil, err
		}
		pkg, err := mergePackageFiles(base, input.Entrypoint, input.Files)
		if err != nil {
			return nil, err
		}
		return s.saveEditorPreview(r.Context(), tx, platform.Current(r).TenantID, pkg, input.EditorNonce)
	})
}

// previewEditorPlatform 只向平台管理员提供未归档公共模板的独立编辑快照。
func (s *Service) previewEditorPlatform(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := idValue(id); err != nil {
		respond(w, r, nil, err)
		return
	}
	if !s.packageSlot(w, r) {
		return
	}
	defer func() { <-s.packageSlots }()
	var input packageEditorPreviewInput
	if err := decodeEditorPreview(w, r, &input); err != nil {
		respond(w, r, nil, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		base, _, _, _, err := s.loadPlatformTemplateSource(r.Context(), tx, id)
		if err != nil {
			return nil, err
		}
		pkg, err := mergePackageFiles(base, input.Entrypoint, input.Files)
		if err != nil {
			return nil, err
		}
		return s.saveEditorPreview(r.Context(), tx, platform.Current(r).TenantID, pkg, input.EditorNonce)
	})
}

// saveEditorPreview 保存通过固定SDK与动作检查的临时文件，选择通道只在本快照有效。
func (s *Service) saveEditorPreview(ctx context.Context, tx pgx.Tx, tenant string, pkg *staticPackage, nonce string) (any, error) {
	if idValue(nonce) != nil {
		return nil, invalidDesign("编辑预览握手标识不正确")
	}
	if err := verifyNewTemplatePackage(ctx, tx, pkg); err != nil {
		return nil, err
	}
	result, err := s.saveTemplatePreview(ctx, tx, tenant, "", "code", json.RawMessage(`{}`), pkg)
	if err != nil {
		return nil, err
	}
	preview := result.(map[string]any)
	if _, err := tx.Exec(ctx, "UPDATE decoration_previews SET editor_nonce=$2 WHERE id=$1", preview["release_id"], nonce); err != nil {
		return nil, err
	}
	return result, nil
}

// editorPreviewBoot 使用服务端来源与快照编号封闭配置，不从租户脚本读取握手参数。
func editorPreviewBoot(parentOrigin, releaseID, nonce string) string {
	config, _ := json.Marshal(map[string]string{"origin": parentOrigin, "release_id": releaseID, "nonce": nonce})
	return "(function(){'use strict';const cfg=" + string(config) + ";\n" + editorPreviewScript + "\n})();"
}

// markEditorDocument 在浏览器解析前标记编辑文档，行业开屏脚本可即时跳过而无需等待选择器就绪。
func markEditorDocument(raw []byte) ([]byte, error) {
	doc, err := xhtml.Parse(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	var mark func(*xhtml.Node)
	mark = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode && node.Data == "body" {
			attrs := node.Attr[:0]
			for _, attr := range node.Attr {
				if attr.Key != "data-gc-editor" {
					attrs = append(attrs, attr)
				}
			}
			node.Attr = append(attrs, xhtml.Attribute{Key: "data-gc-editor", Val: "true"})
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			mark(child)
		}
	}
	mark(doc)
	var result bytes.Buffer
	if err := xhtml.Render(&result, doc); err != nil {
		return nil, err
	}
	return result.Bytes(), nil
}
