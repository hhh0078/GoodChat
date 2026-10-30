// 文件作用说明：验证七行业固定SDK模板、破坏性输入、旧模板导出闭环和纯内存预览动作。
package decoration

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// templateFileMap 将测试静态包转换为独立映射，修改测试字节不污染原包。
func templateFileMap(pkg *staticPackage) map[string]packageFile {
	files := map[string]packageFile{}
	for _, file := range pkg.Files {
		file.Content = append([]byte(nil), file.Content...)
		files[file.Path] = file
	}
	return files
}

// TestIndustryTemplatePackages 七个行业全部经过真实转换和SDK版本校验，并可ZIP导出后重导入。
func TestIndustryTemplatePackages(t *testing.T) {
	scripts := defaultTemplateScripts()
	if err := validateTemplateScripts(scripts); err != nil {
		t.Fatal(err)
	}
	if len(industryTemplateCatalog()) != 7 {
		t.Fatal("官方目录必须包含七个行业")
	}
	for _, industry := range industryTemplateCatalog() {
		t.Run(industry.Key, func(t *testing.T) {
			pkg, err := industryTemplatePackage(industry.Key, scripts)
			if err != nil {
				t.Fatal(err)
			}
			markup := string(templateFileMap(pkg)["index.html"].Content)
			if !strings.Contains(markup, `data-gc-action="leave-message"`) || !strings.Contains(markup, `data-gc-action="evaluation"`) || strings.Contains(markup, "GoodChat 提供技术支持") {
				t.Fatal("行业模板应提供固定留言/评价入口，技术支持由宿主统一保留")
			}
			files := templateFileMap(pkg)
			contract, err := validateTemplatePackage(pkg.Entrypoint, files, true)
			if err != nil || !templateContractMatches(contract, scripts) {
				t.Fatalf("固定版本不匹配：%v", err)
			}
			if err := verifyNewTemplatePackage(context.Background(), nil, pkg); err != nil {
				t.Fatal(err)
			}
			zipFiles := map[string][]byte{}
			for name, file := range files {
				zipFiles[name] = file.Content
			}
			reloaded, err := readStaticZIP(zipFixture(t, zipFiles, 0), "index.html")
			if err != nil || reloaded.SHA256 != pkg.SHA256 {
				t.Fatalf("静态ZIP重导入不能改变原版核心字节：%v", err)
			}
		})
	}
}

// TestTemplateContractRejectsBrokenFiles 缺引用、篡改SDK、删除合约和绕过固定动作均不能作为新模板发布。
func TestTemplateContractRejectsBrokenFiles(t *testing.T) {
	pkg, err := industryTemplatePackage("finance", defaultTemplateScripts())
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(map[string]packageFile){
		"missing contract": func(files map[string]packageFile) { delete(files, templateContractPath) },
		"mutated sdk": func(files map[string]packageFile) {
			file := files[templateSDKPath]
			file.Content = append(file.Content, []byte("\n//changed")...)
			files[templateSDKPath] = file
		},
		"missing sdk reference": func(files map[string]packageFile) {
			file := files["index.html"]
			file.Content = []byte(strings.Replace(string(file.Content), `src="static/goodchat/sdk.js"`, `src="static/custom.js"`, 1))
			files["index.html"] = file
		},
		"missing control": func(files map[string]packageFile) {
			file := files["index.html"]
			file.Content = []byte(strings.Replace(string(file.Content), `id="gc-send"`, `id="some-send"`, 1))
			files["index.html"] = file
		},
		"forged html version": func(files map[string]packageFile) {
			file := files["index.html"]
			file.Content = []byte(strings.Replace(string(file.Content), `data-goodchat-version="`+defaultTemplateScripts().Version+`"`, `data-goodchat-version="forged"`, 1))
			files["index.html"] = file
		},
		"network custom script": func(files map[string]packageFile) {
			file := files["static/custom.js"]
			file.Content = []byte(`new WebSocket("wss://example.invalid")`)
			files["static/custom.js"] = file
		},
		"inline custom script": func(files map[string]packageFile) {
			file := files["index.html"]
			file.Content = []byte(strings.Replace(string(file.Content), "</body>", "<script>window.GoodChat.getState()</script></body>", 1))
			files["index.html"] = file
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			files := templateFileMap(pkg)
			change(files)
			if _, err := validateTemplatePackage("index.html", files, true); err == nil {
				t.Fatal("破坏固定合约的模板被接受")
			}
		})
	}
	files := templateFileMap(pkg)
	var contract templateContract
	_ = json.Unmarshal(files[templateContractPath].Content, &contract)
	contract.Files[templateSDKPath] = digest([]byte("forged"))
	if templateContractMatches(contract, defaultTemplateScripts()) {
		t.Fatal("重算摘要不能冒充平台脚本")
	}
}

// TestTemplateJSONCannotOverrideSDK 确认AI只能提供外观文件，不能提交固定脚本或路径穿越。
func TestTemplateJSONCannotOverrideSDK(t *testing.T) {
	for _, name := range []string{templateSDKPath, templateUIPath, templateContractPath, "static/goodchat/other.js", "../escape.css"} {
		if _, err := buildTemplatePackage("index.html", []TemplateFile{{Path: name, Content: "window.GoodChat"}}, defaultTemplateScripts()); err == nil {
			t.Fatalf("不应接受AI覆盖或越界路径：%s", name)
		}
	}
}

// TestTemplateBuildNormalizesInitialHidden 仅构建器补齐确定控件的隐藏属性，未经规范化的ZIP仍不能导入。
func TestTemplateBuildNormalizesInitialHidden(t *testing.T) {
	scripts := defaultTemplateScripts()
	pkg, err := industryTemplatePackage("finance", scripts)
	if err != nil {
		t.Fatal(err)
	}
	generated := []TemplateFile{}
	assets := []packageFile{}
	rawZIPFiles := map[string][]byte{}
	for _, file := range pkg.Files {
		content := file.Content
		if file.Path == "index.html" {
			content = []byte(strings.ReplaceAll(string(content), ` hidden=""`, ""))
		}
		rawZIPFiles[file.Path] = content
		if isProtectedTemplatePath(file.Path) {
			continue
		}
		if strings.HasPrefix(file.ContentType, "text/") || file.ContentType == "application/json" {
			generated = append(generated, TemplateFile{Path: file.Path, Content: string(content)})
		} else {
			assets = append(assets, file)
		}
	}
	if _, err := readStaticZIP(zipFixture(t, rawZIPFiles, 0), "index.html"); err == nil {
		t.Fatal("ZIP导入不得自动补齐隐藏属性后绕过严格校验")
	}
	rebuilt, err := buildTemplatePackageAssets("index.html", generated, scripts, assets)
	if err != nil {
		t.Fatalf("构建器应固化三个既有平台控件的初始隐藏状态：%v", err)
	}
	if _, err := validateTemplatePackage("index.html", templateFileMap(rebuilt), true); err != nil {
		t.Fatal(err)
	}
	for _, alteration := range []struct{ old, next string }{
		{`id="gc-file"`, `id="missing-file"`},
		{`type="file"`, `type="text"`},
		{`id="gc-add"`, `id="gc-file"`},
	} {
		broken := append([]TemplateFile(nil), generated...)
		for i := range broken {
			if broken[i].Path == "index.html" {
				broken[i].Content = strings.Replace(broken[i].Content, alteration.old, alteration.next, 1)
			}
		}
		if _, err := buildTemplatePackageAssets("index.html", broken, scripts, assets); err == nil {
			t.Fatal("初始隐藏规范化不能放过缺节点、错误类型或重复ID")
		}
	}
}

// TestLegacyTemplateExportUpgradesToFixedSDK 验证结构化、内联及旧多文件包导出后都具有可重导入的固定脚本合约。
func TestLegacyTemplateExportUpgradesToFixedSDK(t *testing.T) {
	scripts := defaultTemplateScripts()
	inline := json.RawMessage(`{"html":"<h1>原有品牌内容</h1>","css":"h1{color:#123456}","javascript":"GoodChat.getState()"}`)
	for name, item := range map[string]struct {
		mode   string
		design json.RawMessage
	}{"schema": {"schema", defaultDesign("schema", "原有品牌内容")}, "inline": {"code", inline}} {
		t.Run(name, func(t *testing.T) {
			pkg, err := exportTemplatePackage(item.mode, item.design, scripts)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(templateFileMap(pkg)["index.html"].Content), "原有品牌内容") {
				t.Fatal("旧模板内容没有保留")
			}
			if err := verifyNewTemplatePackage(context.Background(), nil, pkg); err != nil {
				t.Fatal(err)
			}
		})
	}
	legacy, err := readStaticZIP(zipFixture(t, packageFixtureFiles(t), 0), "site/index.html")
	if err != nil {
		t.Fatal(err)
	}
	upgraded, err := upgradeLegacyTemplatePackage(legacy, scripts)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyNewTemplatePackage(context.Background(), nil, upgraded); err != nil {
		t.Fatal(err)
	}
}

// TestFixedSDKPreviewNeverConnects 执行同一固定SDK的模拟分支，确认发送去重、历史和附件都不连接父层或网络。
func TestFixedSDKPreviewNeverConnects(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("执行预览SDK需要本机Node运行时")
	}
	scripts := defaultTemplateScripts()
	source, _ := json.Marshal(templateBootScript("https://parent.example", "preview", true) + scripts.SDK)
	chatUI, _ := json.Marshal(scripts.ChatUI)
	harness := `import assert from 'node:assert/strict'; import {runInNewContext,Script} from 'node:vm';
const source=` + string(source) + `; new Script(source); new Script(` + string(chatUI) + `);
const window={}; const forbidden=()=>{throw new Error('preview must not connect')};
runInNewContext(source,{window,Blob,Uint8Array,atob,crypto:{randomUUID:()=>crypto.randomUUID()},parent:{postMessage:forbidden},fetch:forbidden,WebSocket:forbidden,setInterval:forbidden});
const chat=window.GoodChat; await chat.ready(); assert.equal(chat.preview,true); assert.equal(Object.isFrozen(chat),true);
const initial=await chat.getState(); assert.equal(initial.messages.length,3); assert.equal(initial.has_older,true);
await chat.sendText('模拟消息','10000000-0000-4000-8000-000000000003'); await chat.sendText('模拟消息','10000000-0000-4000-8000-000000000003');
assert.equal((await chat.getState()).messages.length,4);
await chat.loadOlder(); assert.equal((await chat.getState()).has_older,false); assert.equal((await chat.getState()).messages.length,5);
await chat.upload(new Blob(['file bytes'],{type:'text/plain'}),'test.txt'); const state=await chat.getState(); const own=state.messages.at(-1);
assert.equal(await (await chat.download(own.attachment.id)).file.text(),'file bytes');
await chat.withdrawMessage(own.id); assert.ok((await chat.getState()).messages.at(-1).withdrawn_at);
await assert.rejects(chat.request('arbitraryBusinessAction')); await assert.rejects(chat.getAvatar('other-conversation'));
assert.equal((await chat.read('12')).preview,true); console.log('preview state,send,dedup,history,attachment,no-network passed');`
	command := exec.Command(node, "--input-type=module")
	command.Stdin = strings.NewReader(harness)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("固定SDK模拟执行失败：%v\n%s", err, output)
	}
}
