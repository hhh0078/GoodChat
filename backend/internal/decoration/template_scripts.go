// 文件作用说明：读取历史固定聊天核心快照，供静态模板导入、复制和预览使用，不执行AI代码生成。
package decoration

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strings"
)

// currentTemplateScripts 使用已登记的最新聊天核心，未登记时使用内置核心，不写入生成提示词。
func (s *Service) currentTemplateScripts(ctx context.Context, tx pgx.Tx) (TemplateScripts, error) {
	var raw []byte
	err := tx.QueryRow(ctx, "SELECT scripts FROM assistant_contract_versions ORDER BY version DESC LIMIT 1").Scan(&raw)
	if err == pgx.ErrNoRows {
		return defaultTemplateScripts(), nil
	}
	if err != nil {
		return TemplateScripts{}, err
	}
	var scripts TemplateScripts
	if err = json.Unmarshal(raw, &scripts); err != nil {
		return scripts, err
	}
	return scripts, validateTemplateScripts(scripts)
}

// templateScope 在已鉴权的平台或租户事务中操作静态模板资源。
func (s *Service) templateScope(w http.ResponseWriter, r *http.Request, source string, action func(pgx.Tx) (any, error)) {
	if source != "platform" {
		s.tenant(w, r, action)
		return
	}
	var value any
	err := pgx.BeginTxFunc(r.Context(), s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error { var e error; value, e = action(tx); return e })
	respond(w, r, value, err)
}

// rebuildTemplateCorePackage 为静态修订保留普通源码和素材，替换受保护核心时创建独立资源。
func rebuildTemplateCorePackage(pkg *staticPackage, scripts TemplateScripts) (*staticPackage, error) {
	if pkg == nil {
		return nil, invalidDesign("二次创作基础包不存在")
	}
	lookup := map[string]packageFile{}
	for _, file := range pkg.Files {
		lookup[file.Path] = file
	}
	contract, err := validateTemplatePackage(pkg.Entrypoint, lookup, true)
	if err != nil {
		return nil, err
	}
	if templateContractMatches(contract, scripts) {
		return pkg, nil
	}
	files, assets, err := splitTemplateBasePackage(pkg)
	if err != nil {
		return nil, err
	}
	return buildTemplatePackageAssets(pkg.Entrypoint, files, scripts, assets)
}

// splitTemplateBasePackage 为静态修订保留普通源码和素材，替换受保护核心时创建独立资源。
func splitTemplateBasePackage(pkg *staticPackage) ([]TemplateFile, []packageFile, error) {
	var files []TemplateFile
	var assets []packageFile
	for _, file := range pkg.Files {
		if strings.HasPrefix(file.Path, "static/goodchat/") || file.Path == "goodchat-contract.json" || file.Path == "template.json" {
			continue
		}
		if strings.HasPrefix(file.ContentType, "text/") || file.ContentType == "application/json" {
			files = append(files, TemplateFile{Path: file.Path, Content: string(file.Content)})
		} else {
			assets = append(assets, file)
		}
	}
	return files, assets, nil
}
