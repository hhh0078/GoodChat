// 文件作用说明：验证编辑器文件修订、图片保护及元素选择通道，不依赖真实业务库或外部模型。
package decoration

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
)

// editorText 为测试构造精确保留空字符串语义的文本指针。
func editorText(value string) *string { return &value }

// TestEditorPackageRevisions 验证新图片与空文本正确合并，原始字节不变且不能触碰核心资源。
func TestEditorPackageRevisions(t *testing.T) {
	base, err := industryTemplatePackage("finance", defaultTemplateScripts())
	if err != nil {
		t.Fatal(err)
	}
	before := base.SHA256
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, image.NewNRGBA(image.Rect(0, 0, 20, 30))); err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(imageData.Bytes())
	result, err := mergePackageFiles(base, "", []packageFilePatch{{Path: "static/images/new.png", DataBase64: &encoded}, {Path: "static/empty.txt", Content: editorText("")}})
	if err != nil {
		t.Fatal(err)
	}
	files := templateFileMap(result)
	if base.SHA256 != before || result.SHA256 == before || !bytes.Equal(files["static/images/new.png"].Content, imageData.Bytes()) || files["static/empty.txt"].Size != 0 {
		t.Fatal("修订应保留原包并精确保存小图与空文本")
	}
	for _, change := range []packageFilePatch{
		{Path: templateSDKPath, Content: editorText("fake")},
		{Path: templateUIPath, Content: editorText("fake")},
		{Path: templateContractPath, Content: editorText("{}")},
		{Path: "template.json", Content: editorText("{}")},
		{Path: "static/missing.txt"},
		{Path: "static/two.txt", Content: editorText(""), DataBase64: &encoded},
		{Path: "static/broken.png", DataBase64: editorText("not base64")},
		{Path: "static/wrong.jpg", DataBase64: &encoded},
		{Path: "image.png", DataBase64: &encoded},
		{Path: "static/script.js", DataBase64: editorText(base64.StdEncoding.EncodeToString([]byte("alert(1)")))},
	} {
		if _, err := mergePackageFiles(base, "", []packageFilePatch{change}); err == nil {
			t.Errorf("不应允许错误修订：%s", change.Path)
		}
	}
	if _, err := mergePackageFiles(base, "", []packageFilePatch{{Path: "static/a.txt", Content: editorText("a")}, {Path: "static/a.txt", Content: editorText("b")}}); err == nil {
		t.Fatal("重复路径不能被后写内容覆盖")
	}
	for _, file := range packageEditorFiles(result) {
		if file["path"] == templateSDKPath && file["protected"] != true || file["path"] == "static/empty.txt" && file["content"] != "" {
			t.Fatal("完整源码必须区分保护脚本与空文本")
		}
	}
}

// TestEditorImagesCompressOnlyWhenNeeded 验证大图按原格式缩小，而小图原始字节与透明通道保持不变。
func TestEditorImagesCompressOnlyWhenNeeded(t *testing.T) {
	var raw bytes.Buffer
	if err := png.Encode(&raw, image.NewNRGBA(image.Rect(0, 0, 2400, 3))); err != nil {
		t.Fatal(err)
	}
	file, err := editorImageFile("static/banner.png", raw.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(file.Content))
	if err != nil || format != "png" || config.Width != 1920 || config.Height != 2 {
		t.Fatal("大图应按比例缩小并保持PNG格式")
	}
	if _, err := editorImageFile("static/pretend.png", []byte("not image")); err == nil {
		t.Fatal("扩展名不能冒充真实图片")
	}
}

// TestEditorPreviewRequestStrictness 验证一次性UUID必填、请求字段严格及空文本字段语义正确。
func TestEditorPreviewRequestStrictness(t *testing.T) {
	var input packageEditorPreviewInput
	valid := `{"editor_nonce":"10000000-0000-4000-8000-000000000001","files":[{"path":"static/empty.txt","content":""}]}`
	if err := decodeEditorPreview(httptest.NewRecorder(), httptest.NewRequest("POST", "/", strings.NewReader(valid)), &input); err != nil || input.Files[0].Content == nil {
		t.Fatal("合法空文本编辑预览应被接受")
	}
	for _, invalid := range []string{`{"files":[]}`, `{"editor_nonce":"not-a-uuid","files":[]}`, valid + `{}`, `{"editor_nonce":"10000000-0000-4000-8000-000000000001","tenant_id":"foreign","files":[]}`} {
		input = packageEditorPreviewInput{}
		if err := decodeEditorPreview(httptest.NewRecorder(), httptest.NewRequest("POST", "/", strings.NewReader(invalid)), &input); err == nil {
			t.Fatal("非法编辑预览请求必须被拒绝")
		}
	}
}

// TestEditorInspectorMessageBoundary 执行真实选择脚本，覆盖来源验证、模式隔离及动态子节点回归到作者节点。
func TestEditorInspectorMessageBoundary(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("元素选择脚本回归需要本机Node运行时")
	}
	source, _ := json.Marshal(editorPreviewBoot("https://admin.example", "release-test", "nonce-test"))
	harness := `import assert from 'node:assert/strict';import {runInNewContext} from 'node:vm';
const events=new Map(),calls=[];
class Element {
 /** constructor 初始化源码节点或运行时子节点的测试替身。 */
 constructor(tag,id,parent=null){this.tagName=tag;this.editorID=id;this.id='';this.parent=parent;this.style={};this.hidden=false;this.kind='';this.classList={contains:()=>false}}
 /** closest 将运行时节点归回最近作者源码节点。 */
 closest(selector){if(selector==='[data-gc-edit-id]'&&this.editorID)return this;if(selector==='.gc-message'&&this.kind==='bubble'||selector==='.gc-message-row'&&this.kind==='row'||selector==='.gc-avatar'&&this.kind==='avatar')return this;return this.parent?.closest(selector)||null}
 /** getAttribute 读取唯一编辑编号。 */
 getAttribute(){return this.editorID}
 /** contains 检查动态节点是否仍属于当前作者消息区域。 */
 contains(node){for(let value=node;value;value=value.parent)if(value===this)return true;return false}
 /** querySelector 仅供重新定位测试中保存的动态节点。 */
 querySelector(){return this.dynamic||null}
 /** getBoundingClientRect 返回高亮尺寸。 */
 getBoundingClientRect(){return {left:4,top:8,width:200,height:60}}
 /** setAttribute 保留无障碍标记而不执行代码。 */
 setAttribute(){}
 /** attachShadow 创建不参与模板CSS的覆盖层容器。 */
 attachShadow(){return {append(){}}}
}
const authored=new Element('DIV','edit-root'),dynamic=new Element('SPAN','',authored);
const parent={postMessage:(value,origin)=>calls.push({value,origin})};
const window={addEventListener:(name,fn)=>events.set(name,fn)};
const document={createElement:tag=>new Element(tag,''),documentElement:{appendChild(){}},querySelectorAll:()=>[authored]};
runInNewContext(` + string(source) + `,{window,parent,document,Element,innerWidth:390,innerHeight:844});
events.get('DOMContentLoaded')();
assert.equal(calls[0].value.type,'goodchat:editor-ready');assert.equal(calls[0].origin,'https://admin.example');
/** click 构造可观察是否阻止业务动作的选择事件。 */
function click(){let prevented=0;events.get('click')({target:dynamic,preventDefault(){prevented++},stopImmediatePropagation(){prevented++}});return prevented}
assert.equal(click(),2);assert.equal(calls.at(-1).value.element_id,'edit-root');assert.equal(calls.at(-1).value.tag,'div');
/** mode 验证控制命令必须携带四项精确匹配的窗口上下文。 */
function mode(overrides={}){events.get('message')({source:parent,origin:'https://admin.example',data:{type:'goodchat:editor-mode',release_id:'release-test',nonce:'nonce-test',mode:'interact'},...overrides})}
mode({origin:'https://evil.example'});assert.equal(click(),2);
mode({source:{}});assert.equal(click(),2);
mode({data:{type:'goodchat:editor-mode',release_id:'foreign',nonce:'nonce-test',mode:'interact'}});assert.equal(click(),2);
mode({data:{type:'goodchat:editor-mode',release_id:'release-test',nonce:'wrong',mode:'interact'}});assert.equal(click(),2);
mode();const count=calls.length;assert.equal(click(),0);assert.equal(calls.length,count);
events.get('message')({source:parent,origin:'https://admin.example',data:{type:'goodchat:editor-highlight',release_id:'release-test',nonce:'nonce-test',element_id:'edit-root'}});
assert.equal(calls.length,count);
events.get('message')({source:parent,origin:'https://admin.example',data:{type:'goodchat:editor-mode',release_id:'release-test',nonce:'nonce-test',mode:'select'}});
authored.id='gc-messages';
const row=new Element('DIV','',authored);row.kind='row';row.classList={contains:value=>value==='gc-own'};
const bubble=new Element('DIV','',row);bubble.kind='bubble';authored.dynamic=bubble;
events.get('click')({target:bubble,preventDefault(){},stopImmediatePropagation(){}});
assert.equal(calls.at(-1).value.style_selector,'.gc-message-row.gc-own .gc-message');assert.equal(calls.at(-1).value.element_id,'edit-root');
row.classList={contains:()=>false};events.get('click')({target:bubble,preventDefault(){},stopImmediatePropagation(){}});
assert.equal(calls.at(-1).value.style_selector,'.gc-message-row:not(.gc-own):not(.gc-system) .gc-message');
const avatar=new Element('IMG','',row);avatar.kind='avatar';events.get('click')({target:avatar,preventDefault(){},stopImmediatePropagation(){}});
assert.equal(calls.at(-1).value.style_selector,'.gc-avatar');
const beforeHighlight=calls.length;events.get('message')({source:parent,origin:'https://admin.example',data:{type:'goodchat:editor-highlight',release_id:'release-test',nonce:'nonce-test',element_id:'edit-root',style_selector:'body *'}});assert.equal(calls.length,beforeHighlight);`
	command := exec.Command(node, "--input-type=module")
	command.Stdin = strings.NewReader(harness)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("编辑器选择通道边界失败：%v\n%s", err, output)
	}
}

// TestEditorDocumentMarker 在浏览器解析前标记body且不重复属性，保留全部模板源码与核心引用。
func TestEditorDocumentMarker(t *testing.T) {
	raw := []byte(`<!doctype html><html><head></head><body data-gc-editor="false"><h1>保留原文</h1><script src="static/custom.js"></script></body></html>`)
	marked, err := markEditorDocument(raw)
	if err != nil || strings.Count(string(marked), `data-gc-editor="true"`) != 1 || strings.Contains(string(marked), `data-gc-editor="false"`) || !strings.Contains(string(marked), "保留原文") {
		t.Fatal("编辑器标记应覆盖为true并保留原始模板")
	}
}
