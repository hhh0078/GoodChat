// 文件作用说明：定义不可由AI覆盖的访客脚本合约、版本摘要及平台历史脚本的可信校验。
package decoration

import (
	"context"
	_ "embed"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
)

const templateContractPath = "goodchat-contract.json"
const templateSDKPath = "static/goodchat/sdk.js"
const templateUIPath = "static/goodchat/chat-ui.js"

// TemplateScripts 保存平台管理员发布的一组固定访客脚本，资源包保存其完整副本。
type TemplateScripts struct {
	Version string `json:"version"`
	SDK     string `json:"sdk"`
	ChatUI  string `json:"chat_ui"`
}

// TemplateFile 定义AI可以提交的普通静态文本文件，不包含固定SDK字节。
type TemplateFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// templateContract 记录当前包所冻结的脚本版本与原始字节摘要。
type templateContract struct {
	SchemaVersion int               `json:"schema_version"`
	SDKVersion    string            `json:"sdk_version"`
	Files         map[string]string `json:"files"`
}

// templatePreviewScript 提供与真实SDK方法一致的纯内存预览实现。
//
//go:embed template_preview.js
var templatePreviewScript string

// templateActionScript 将行业入口限定为固定SDK文字消息，AI不实现业务事件。
//
//go:embed template_actions.js
var templateActionScript string

// templateViewportScript 统一适配宿主可用高度和手机软键盘，模板只负责视觉布局。
//
//go:embed template_viewport.js
var templateViewportScript string

var templateVersionPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

// defaultTemplateScripts 复用既有可靠桥接与消息界面，普通脚本字节不依赖具体租户或会话。
func defaultTemplateScripts() TemplateScripts {
	config, _ := json.Marshal(map[string]any{"origin": "", "release": "", "preview": false})
	sdk := strings.Replace(sdkScript("", "", false), "const cfg="+string(config)+";", "const cfg=window.__GoodChatBoot;", 1)
	return TemplateScripts{Version: "1.0.4", SDK: templatePreviewScript + "\n(function(){if(window.__GoodChatBoot?.preview)return;" + sdk + "})();", ChatUI: templateViewportScript + "\n" + schemaChatScript + "\n" + templateActionScript}
}

// validateTemplateScripts 校验管理员脚本的静态合约与体积，不在服务器执行浏览器代码。
func validateTemplateScripts(scripts TemplateScripts) error {
	if !templateVersionPattern.MatchString(scripts.Version) || len(scripts.SDK) < 100 || len(scripts.SDK) > 300000 || len(scripts.ChatUI) < 100 || len(scripts.ChatUI) > 300000 {
		return invalidDesign("固定脚本版本或大小不正确，每份脚本需为100到300000字节")
	}
	for _, text := range []string{scripts.SDK, scripts.ChatUI} {
		if strings.Contains(strings.ToLower(text), "</script") {
			return invalidDesign("固定脚本不能包含结束script标签")
		}
		if _, err := jsTokens(text); err != nil {
			return err
		}
	}
	for _, marker := range []string{"GoodChat", "__GoodChatBoot", "goodchat:init", "sendText", "getState", "loadOlder", "upload", "getAvatar", "subscribe", "openLeaveMessage", "openEvaluation"} {
		if !strings.Contains(scripts.SDK, marker) {
			return invalidDesignf("固定SDK缺少既有桥接方法：%s", marker)
		}
	}
	for _, marker := range []string{"GoodChat", "gc-messages", "gc-text", "gc-send", "sendText", "subscribe", "upload", "openLeaveMessage", "openEvaluation", "data-gc-action"} {
		if !strings.Contains(scripts.ChatUI, marker) {
			return invalidDesignf("固定聊天脚本缺少必需界面合约：%s", marker)
		}
	}
	return nil
}

// templateBootScript 仅注入平台生成的运行参数，身份和聊天凭证始终由可信父层持有。
func templateBootScript(parentOrigin, releaseID string, preview bool) string {
	config, _ := json.Marshal(map[string]any{"origin": parentOrigin, "release": releaseID, "preview": preview})
	return `Object.defineProperty(window,'__GoodChatBoot',{value:Object.freeze(` + string(config) + `),writable:false,configurable:false});`
}

// templateHasContract 判断资源清单是否声明新版固定脚本合约，旧数据库资源仍保持原行为。
func templateHasContract(files map[string]packageFile) bool {
	_, ok := files[templateContractPath]
	return ok
}

// parseTemplateContract 拒绝未知字段和不完整版本摘要，摘要本身不代表平台注册来源。
func parseTemplateContract(raw []byte) (templateContract, error) {
	var contract templateContract
	if err := strictJSON(raw, &contract); err != nil {
		return contract, err
	}
	if contract.SchemaVersion != 1 || !templateVersionPattern.MatchString(contract.SDKVersion) || len(contract.Files) != 2 {
		return contract, invalidDesign("固定脚本合约版本或文件清单不正确")
	}
	for _, name := range []string{templateSDKPath, templateUIPath} {
		if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(contract.Files[name]) {
			return contract, invalidDesign("固定脚本必须包含完整SHA256摘要")
		}
	}
	return contract, nil
}

// templateContractMatches 将包内摘要与一份平台可信版本进行精确比较。
func templateContractMatches(contract templateContract, scripts TemplateScripts) bool {
	return contract.SDKVersion == scripts.Version && contract.Files[templateSDKPath] == digest([]byte(scripts.SDK)) && contract.Files[templateUIPath] == digest([]byte(scripts.ChatUI))
}

// builtinTemplateContract 认可当前及已发布原始摘要，升级预览不会使历史包失效或重写历史脚本。
func builtinTemplateContract(contract templateContract) bool {
	if contract.SchemaVersion != 1 {
		return false
	}
	if templateContractMatches(contract, defaultTemplateScripts()) {
		return true
	}
	if contract.SDKVersion == "1.0.3" && contract.Files[templateSDKPath] == "65239177291785c0b5afbe840fd1102d93b0f242fc22fbf23ed2fe5cb0145c5e" && contract.Files[templateUIPath] == "e084a4cff0b02a16d7417e3ca93b23801180e632d371e1d1beb9ab2c914774bd" {
		return true
	}
	if contract.SDKVersion == "1.0.2" && contract.Files[templateSDKPath] == "87297b49aafcb08361006e11316ccd6d8e240293996c17cb4ca1fa5b6f6bfe1b" && contract.Files[templateUIPath] == "e084a4cff0b02a16d7417e3ca93b23801180e632d371e1d1beb9ab2c914774bd" {
		return true
	}
	return contract.SDKVersion == "1.0.1" &&
		contract.Files[templateSDKPath] == "8532ecb1fe008727b471c9f6448c142ae0559de9fc57bc25ad99e1825f41b7cc" &&
		contract.Files[templateUIPath] == "546ccdf023366b9ecdfe9aaccbcfd0db73ce5577cf50e0fa0a223d9e5ab7235c"
}

// verifyRegisteredTemplateContract 只认可内置版本或平台不可变历史，重算自报摘要不能冒充固定SDK。
func verifyRegisteredTemplateContract(ctx context.Context, tx pgx.Tx, contract templateContract) error {
	if builtinTemplateContract(contract) {
		return nil
	}
	rows, err := tx.Query(ctx, "SELECT scripts FROM assistant_contract_versions ORDER BY version DESC")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var raw json.RawMessage
		var scripts TemplateScripts
		if err := rows.Scan(&raw); err != nil {
			return err
		}
		if json.Unmarshal(raw, &scripts) == nil && templateContractMatches(contract, scripts) {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return invalidDesign("固定SDK字节或版本未在平台注册，不能修改SDK后重新计算摘要")
}

// verifyNewTemplatePackage 为所有新导入和修订要求完整固定合约，旧包兼容不适用于新上传。
func verifyNewTemplatePackage(ctx context.Context, tx pgx.Tx, pkg *staticPackage) error {
	files := map[string]packageFile{}
	for _, file := range pkg.Files {
		files[file.Path] = file
	}
	contract, err := validateTemplatePackage(pkg.Entrypoint, files, true)
	if err != nil {
		return err
	}
	if err := validateNewTemplateActions(files[pkg.Entrypoint].Content, files); err != nil {
		return err
	}
	return verifyRegisteredTemplateContract(ctx, tx, contract)
}

// verifyTemplatePackageScripts 检查持久资源包固定脚本的真实字节与历史来源，既有无合约包保持兼容。
func verifyTemplatePackageScripts(ctx context.Context, tx pgx.Tx, id string) error {
	var entry string
	if err := tx.QueryRow(ctx, "SELECT entrypoint FROM decoration_code_packages WHERE id=$1", id).Scan(&entry); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, "SELECT path,content_type,sha256,content FROM decoration_package_files WHERE package_id=$1 ORDER BY path", id)
	if err != nil {
		return err
	}
	files := map[string]packageFile{}
	for rows.Next() {
		var file packageFile
		if err := rows.Scan(&file.Path, &file.ContentType, &file.SHA256, &file.Content); err != nil {
			rows.Close()
			return err
		}
		file.Size = len(file.Content)
		files[file.Path] = file
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if !templateHasContract(files) {
		return nil
	}
	contract, err := validateTemplatePackage(entry, files, true)
	if err != nil {
		return err
	}
	return verifyRegisteredTemplateContract(ctx, tx, contract)
}
