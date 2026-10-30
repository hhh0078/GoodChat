// 文件作用说明：把私有或已上架公共模板原子复制成新的静态代码作品，固定SDK并保留公共来源版本，不修改原件。
package decoration

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// assistantRemixTemplate 读取允许使用的固定设计快照，兼容旧组件、内联和多文件模板，返回全新私有模板编号。
func (s *Service) assistantRemixTemplate(ctx context.Context, tx pgx.Tx, p platform.Principal, id, name string) (string, error) {
	if err := s.checkFeature(ctx, tx, p.TenantID, "advanced_decoration"); err != nil {
		return "", err
	}
	var mode, sourceID string
	var sourceVersion int64
	var design json.RawMessage
	err := tx.QueryRow(ctx, "SELECT render_mode,design,coalesce(source_platform_template_id::text,''),coalesce(source_platform_version,0) FROM tenant_templates WHERE id=$1 FOR KEY SHARE", id).Scan(&mode, &design, &sourceID, &sourceVersion)
	if err == pgx.ErrNoRows {
		err = tx.QueryRow(ctx, "SELECT render_mode,design,id::text,version FROM platform_templates WHERE id=$1 AND status='published' AND archived_at IS NULL FOR SHARE", id).Scan(&mode, &design, &sourceID, &sourceVersion)
	}
	if err != nil {
		return "", err
	}
	scripts, err := s.currentTemplateScripts(ctx, tx)
	if err != nil {
		return "", err
	}
	var code CodeDesign
	_ = json.Unmarshal(design, &code)
	var pkg *staticPackage
	if mode == "code" && code.PlatformPackageID != "" {
		pkg, err = loadPlatformTemplatePackage(ctx, tx, code.PlatformPackageID)
	} else if mode == "code" && code.PackageID != "" {
		pkg, err = loadArchivePackage(ctx, tx, code)
		if err == nil {
			pkg, err = upgradeLegacyTemplatePackage(pkg, scripts)
		}
	} else {
		pkg, err = exportTemplatePackage(mode, design, scripts)
	}
	if err != nil {
		return "", err
	}
	if err := verifyNewTemplatePackage(ctx, tx, pkg); err != nil {
		return "", err
	}
	pkg, err = assistantRebuildCorePackage(pkg, scripts)
	if err != nil {
		return "", err
	}
	stored, err := s.storePackage(ctx, tx, p, name, pkg)
	if err != nil {
		return "", err
	}
	raw, _ := json.Marshal(stored)
	var converted struct {
		Design CodeDesign `json:"design"`
	}
	if err := json.Unmarshal(raw, &converted); err != nil {
		return "", err
	}
	newDesign, _ := json.Marshal(converted.Design)
	var copied string
	err = tx.QueryRow(ctx, "INSERT INTO tenant_templates(tenant_id,owner_membership_id,name,render_mode,design,source_platform_template_id,source_platform_version) VALUES($1,$2,$3,'code',$4,NULLIF($5,'')::uuid,NULLIF($6,0)) RETURNING id::text", p.TenantID, p.MembershipID, name, newDesign, sourceID, sourceVersion).Scan(&copied)
	return copied, err
}

// assistantRebuildCorePackage 只为创作副本重新冻结当前核心与SRI，保留普通源码和二进制素材，绝不修改原包。
func assistantRebuildCorePackage(pkg *staticPackage, scripts TemplateScripts) (*staticPackage, error) {
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
	files, assets, err := assistantSplitBasePackage(pkg)
	if err != nil {
		return nil, err
	}
	return buildTemplatePackageAssets(pkg.Entrypoint, files, scripts, assets)
}
