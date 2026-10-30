// 文件作用说明：验证获确认的七套摄影模板和不可变核心兼容性，按显式环境开关导出本机浏览器验收样本。
package decoration

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestApprovedIndustryAssets 确保每套设计都携带自身真实摄影、受控头像和图标，而非同版占位内容。
func TestApprovedIndustryAssets(t *testing.T) {
	samples := map[string]any{}
	scripts := defaultTemplateScripts()
	for _, item := range industryTemplateCatalog() {
		pkg, err := industryTemplatePackage(item.Key, scripts)
		if err != nil {
			t.Fatal(item.Key, err)
		}
		files := templateFileMap(pkg)
		hero := "static/assets/" + item.Key + "-hero.webp"
		if file, ok := files[hero]; !ok || file.ContentType != "image/webp" || file.Size < 1000 {
			t.Fatalf("%s缺少实际摄影素材", item.Key)
		}
		markup := string(files["index.html"].Content)
		if !strings.Contains(markup, `data-gc-preview-industry="`+item.Key+`"`) || !strings.Contains(markup, hero) {
			t.Fatalf("%s未关联对应行业预览和图片", item.Key)
		}
		for _, name := range []string{"icon-picture.png", "icon-smile.png", "avatar-agent.jpg", "avatar-visitor.jpg"} {
			if _, ok := files["static/assets/"+name]; !ok {
				t.Fatalf("%s缺少本地资源%s", item.Key, name)
			}
		}
		samples[item.Key] = map[string]any{"name": item.Name, "entrypoint": pkg.Entrypoint, "files": packageEditorFiles(pkg)}
	}
	if target := os.Getenv("GOODCHAT_APPROVED_FIXTURE"); target != "" {
		raw, err := json.Marshal(map[string]any{"scripts": scripts, "templates": samples})
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(target, raw, 0600); err != nil {
			t.Fatal("无法保存显式请求的无敏感验收资源")
		}
	}
}

// TestPublishedCore102RemainsImmutable 确认新预览不会破坏已发布核心，同时拒绝同版本伪造摘要。
func TestPublishedCore102RemainsImmutable(t *testing.T) {
	old := templateContract{SchemaVersion: 1, SDKVersion: "1.0.2", Files: map[string]string{templateSDKPath: "87297b49aafcb08361006e11316ccd6d8e240293996c17cb4ca1fa5b6f6bfe1b", templateUIPath: "e084a4cff0b02a16d7417e3ca93b23801180e632d371e1d1beb9ab2c914774bd"}}
	if !builtinTemplateContract(old) {
		t.Fatal("旧版核心合约失效")
	}
	old.Files[templateSDKPath] = digest([]byte("forged"))
	if builtinTemplateContract(old) {
		t.Fatal("同版本伪造核心通过")
	}
	if scripts := defaultTemplateScripts(); scripts.Version != "1.0.4" {
		t.Fatal("预览升级需要独立不可变版本")
	}
}

// TestApprovedPreviewProfiles 运行固定核心的七种内存预览，检查消息、照片、选项与附件能力不会创建真实业务。
func TestApprovedPreviewProfiles(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("需要本机Node执行固定预览核心")
	}
	source, _ := json.Marshal(templatePreviewScript)
	harness := `import assert from 'node:assert/strict';import {runInNewContext} from 'node:vm';import {randomUUID} from 'node:crypto';
 for(const key of ['finance','commerce','education','medical','property','business','gaming']){
 const footers=[],window={__GoodChatBoot:{preview:true}};
 const document={body:{dataset:{gcPreviewIndustry:key}},querySelector(){return{append(node){footers.push(node)}}},createElement(){return{}}};
 runInNewContext(` + string(source) + `,{window,document,Set,Map,Blob,Uint8Array,atob,Date,JSON,crypto:{randomUUID}});
 const api=window.GoodChat;assert.equal(api.preview,true);assert.equal(Object.isFrozen(api),true);
 let state=await api.getState();assert.equal(state.messages.length,3);assert.equal(state.has_older,false);assert.equal(state.messages.some(item=>item.attachment),false);assert.ok(state.messages.some(item=>item.options?.length>=2));assert.equal(footers.length,1);
 const photo=await api.getAvatar(state.messages[0].id);assert.equal(photo.file.type,'image/jpeg');assert.ok(photo.file.size>1000);
 const ownPhoto=await api.getAvatar(state.messages[1].id);assert.notDeepEqual(await photo.file.arrayBuffer(),await ownPhoto.file.arrayBuffer());
 await api.sendText('本地预览验收','10000000-0000-4000-8000-000000000009');await api.sendText('本地预览验收','10000000-0000-4000-8000-000000000009');state=await api.getState();assert.equal(state.messages.length,4);
 await api.upload(new Blob(['test'],{type:'text/plain'}),'验收.txt');state=await api.getState();assert.equal(state.messages.at(-1).attachment.file_name,'验收.txt');const file=await api.download(state.messages.at(-1).attachment.id);assert.equal(await file.file.text(),'test');
 await assert.rejects(api.request('unknown'));await assert.rejects(api.sendText(' '));
 }`
	command := exec.Command(node, "--input-type=module")
	command.Stdin = strings.NewReader(harness)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("行业内存预览失败：%v\n%s", err, output)
	}
}
