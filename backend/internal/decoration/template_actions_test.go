// 文件作用说明：验证新模板的按钮必须绑定固定SDK或明确本地UI，历史包的读取和导出仍保持兼容。
package decoration

import (
	"context"
	"strings"
	"testing"
)

// templateWithActionButton 在有效模板中插入一个待测试业务或本地界面按钮。
func templateWithActionButton(t *testing.T, button string) (*staticPackage, []TemplateFile) {
	t.Helper()
	pkg, err := industryTemplatePackage("finance", defaultTemplateScripts())
	if err != nil {
		t.Fatal(err)
	}
	files := []TemplateFile{}
	for _, file := range pkg.Files {
		if isProtectedTemplatePath(file.Path) {
			continue
		}
		text := string(file.Content)
		if file.Path == "index.html" {
			text = strings.Replace(text, `<nav class="gc-service-entries"`, button+`<nav class="gc-service-entries"`, 1)
		}
		files = append(files, TemplateFile{Path: file.Path, Content: text})
	}
	return pkg, files
}

// TestNewTemplateButtonsRequireActions 拒绝空按钮、只声明分期期数和无效动作，不替模型猜测业务消息。
func TestNewTemplateButtonsRequireActions(t *testing.T) {
	for name, button := range map[string]string{
		"empty button":           `<button type="button"></button>`,
		"period without action":  `<button type="button" class="option-chip" data-period="6">6个月</button>`,
		"empty send":             `<button type="button" data-gc-send="  ">6个月</button>`,
		"missing type":           `<button data-gc-send="咨询6个月">6个月</button>`,
		"fake core id":           `<button id="gc-finance" type="button">6个月</button>`,
		"wrong attachment place": `<button type="button" data-kind="image">图片</button>`,
		"multiple actions":       `<button type="button" data-gc-send="6个月" data-gc-ui="open">6个月</button>`,
		"invalid dialog target":  `<button type="button" data-dialog-open="missing">说明</button>`,
		"empty UI action":        `<button type="button" data-gc-ui="">更多</button>`,
		"invalid UI action":      `<button type="button" data-gc-ui="run()">更多</button>`,
		"close outside dialog":   `<button type="button" data-dialog-close>关闭</button>`,
	} {
		t.Run(name, func(t *testing.T) {
			_, files := templateWithActionButton(t, button)
			if _, err := buildTemplatePackage("index.html", files, defaultTemplateScripts()); err == nil {
				t.Fatal("新模板的按钮缺少有效明确动作，不应被接受")
			}
		})
	}
}

// TestNewTemplateExplicitActions 接受固定咨询文字和由额外UI脚本实现的明确本地弹框或界面动作。
func TestNewTemplateExplicitActions(t *testing.T) {
	for name, button := range map[string]string{
		"fixed send":   `<button type="button" data-gc-send="咨询分期6个月">6个月</button>`,
		"local dialog": `<button type="button" data-dialog-open="details">说明</button><dialog id="details"><button type="button" data-dialog-close>关闭</button></dialog>`,
		"local UI":     `<button type="button" data-gc-ui="toggle_details">展开说明</button>`,
	} {
		t.Run(name, func(t *testing.T) {
			_, files := templateWithActionButton(t, button)
			pkg, err := buildTemplatePackage("index.html", files, defaultTemplateScripts())
			if err != nil {
				t.Fatal(err)
			}
			if err := verifyNewTemplatePackage(context.Background(), nil, pkg); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestNewButtonRulesDoNotBlockExistingPackage 旧包仍可通过原静态校验和导出，但新导入入口会拒绝未绑定按钮。
func TestNewButtonRulesDoNotBlockExistingPackage(t *testing.T) {
	original, _ := templateWithActionButton(t, "")
	files := append([]packageFile(nil), original.Files...)
	for i := range files {
		if files[i].Path == "index.html" {
			raw := strings.Replace(string(files[i].Content), `<nav class="gc-service-entries"`, `<button type="button" data-period="6">6个月</button><nav class="gc-service-entries"`, 1)
			file, err := checkStaticFile("index.html", []byte(raw))
			if err != nil {
				t.Fatal(err)
			}
			files[i] = file
		}
	}
	legacy, err := finishPackage("index.html", files)
	if err != nil {
		t.Fatalf("已有资源包读取不能因新增动作规则被阻断：%v", err)
	}
	if _, err := encodeTemplateArchive("历史模板", legacy); err != nil {
		t.Fatal(err)
	}
	if err := verifyNewTemplatePackage(context.Background(), nil, legacy); err == nil {
		t.Fatal("相同未绑定按钮不能通过新导入强校验")
	}
}
