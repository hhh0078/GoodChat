// 文件作用说明：验证新模板的按钮必须绑定固定SDK或明确本地UI，历史包的读取和导出仍保持兼容。
package decoration

import (
	"context"
	"strings"
	"testing"
)

// templateWithActionButton 保留真实模板的二进制素材，在稳定的主区域末尾插入待测按钮。
func templateWithActionButton(t *testing.T, button string) (*staticPackage, []TemplateFile, []packageFile) {
	t.Helper()
	pkg, err := industryTemplatePackage("finance", defaultTemplateScripts())
	if err != nil {
		t.Fatal(err)
	}
	files, assets, err := assistantSplitBasePackage(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := buildTemplatePackageAssets("index.html", files, defaultTemplateScripts(), assets); err != nil {
		t.Fatalf("插入待测按钮前的完整静态包必须有效：%v", err)
	}
	for i := range files {
		if files[i].Path == "index.html" {
			if strings.Count(files[i].Content, "</main>") != 1 {
				t.Fatal("测试模板必须有唯一主区域，不能静默跳过按钮插入")
			}
			files[i].Content = strings.Replace(files[i].Content, "</main>", button+"</main>", 1)
		}
	}
	return pkg, files, assets
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
			_, files, assets := templateWithActionButton(t, button)
			if _, err := buildTemplatePackageAssets("index.html", files, defaultTemplateScripts(), assets); err == nil {
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
			_, files, assets := templateWithActionButton(t, button)
			pkg, err := buildTemplatePackageAssets("index.html", files, defaultTemplateScripts(), assets)
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
	original, _, _ := templateWithActionButton(t, "")
	files := append([]packageFile(nil), original.Files...)
	for i := range files {
		if files[i].Path == "index.html" {
			markup := string(files[i].Content)
			if strings.Count(markup, "</main>") != 1 {
				t.Fatal("测试模板必须有唯一主区域，确保旧按钮确实被插入")
			}
			raw := strings.Replace(markup, "</main>", `<button type="button" data-period="6">6个月</button></main>`, 1)
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

// TestNewTemplateRejectsPermanentHiddenControls 固定SDK仅切换hidden属性，新模板不能叠加它无法解除的隐藏类或内联样式。
func TestNewTemplateRejectsPermanentHiddenControls(t *testing.T) {
	pkg, err := industryTemplatePackage("finance", defaultTemplateScripts())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"gc-reply-bar", "gc-attachment-menu", "gc-messages", "gc-text", "gc-send", "gc-compose"} {
		for name, attr := range map[string]string{
			"hidden class":      ` class="layout hidden"`,
			"is-hidden class":   ` class="is-hidden"`,
			"display none":      ` style="display: none !important"`,
			"visibility hidden": ` style="color:red; visibility: hidden"`,
			"escaped style":     ` style="display:\6e one"`,
			"commented style":   ` style="display:/*initial*/none"`,
		} {
			t.Run(id+"/"+name, func(t *testing.T) {
				files, assets, err := assistantSplitBasePackage(pkg)
				if err != nil {
					t.Fatal(err)
				}
				for i := range files {
					if files[i].Path == "index.html" {
						marker := `id="` + id + `"`
						if strings.Count(files[i].Content, marker) != 1 {
							t.Fatal("测试必须修改唯一的真实固定控件", id)
						}
						files[i].Content = strings.Replace(files[i].Content, marker, marker+attr, 1)
					}
				}
				if _, err := buildTemplatePackageAssets("index.html", files, defaultTemplateScripts(), assets); err == nil {
					t.Fatal("不能接受SDK无法解除的永久隐藏样式")
				}
			})
		}
	}
	lookup := templateFileMap(pkg)
	entry := lookup["index.html"]
	entry.Content = []byte(strings.Replace(string(entry.Content), `id="gc-attachment-menu"`, `id="gc-attachment-menu" class="hidden"`, 1))
	entry.SHA256 = digest(entry.Content)
	entry.Size = len(entry.Content)
	lookup[entry.Path] = entry
	all := []packageFile{}
	for _, file := range lookup {
		all = append(all, file)
	}
	stored, err := finishPackage("index.html", all)
	if err != nil {
		t.Fatalf("新增预检不能阻止历史已存包的读取：%v", err)
	}
	if _, err := encodeTemplateArchive("历史样式", stored); err != nil {
		t.Fatal(err)
	}
	if err := verifyNewTemplatePackage(context.Background(), nil, stored); err == nil {
		t.Fatal("同一历史样式不能通过新导入入口")
	}
}

// TestNewTemplateKeepsSDKHiddenAttribute 保留正常自定义类和必需hidden属性，不静默删除样式或降低既有导入要求。
func TestNewTemplateKeepsSDKHiddenAttribute(t *testing.T) {
	pkg, err := industryTemplatePackage("finance", defaultTemplateScripts())
	if err != nil {
		t.Fatal(err)
	}
	files, assets, err := assistantSplitBasePackage(pkg)
	if err != nil {
		t.Fatal(err)
	}
	for i := range files {
		if files[i].Path == "index.html" {
			files[i].Content = strings.Replace(files[i].Content, `id="gc-reply-bar"`, `id="gc-reply-bar" class="custom-reply hidden-label"`, 1)
		}
	}
	next, err := buildTemplatePackageAssets("index.html", files, defaultTemplateScripts(), assets)
	if err != nil {
		t.Fatal(err)
	}
	lookup := templateFileMap(next)
	if !strings.Contains(string(lookup["index.html"].Content), `class="custom-reply hidden-label"`) {
		t.Fatal("正常自定义类不能被静默移除")
	}
	if err := verifyNewTemplatePackage(context.Background(), nil, next); err != nil {
		t.Fatal(err)
	}
	zipFiles := map[string][]byte{}
	for name, file := range lookup {
		text := file.Content
		if name == "index.html" {
			text = []byte(strings.ReplaceAll(string(text), ` hidden=""`, ""))
		}
		zipFiles[name] = text
	}
	if _, err := readStaticZIP(zipFixture(t, zipFiles, 0), "index.html"); err == nil {
		t.Fatal("初始hidden属性仍必须通过原有严格ZIP校验")
	}
}
