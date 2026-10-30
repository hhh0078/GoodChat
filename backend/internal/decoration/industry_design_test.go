// 文件作用说明：验证七套独立行业设计的可点选标识、本地图片和加载层退出行为。
package decoration

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// TestIndustryEditableTargetsUnique 确认每个模板的静态编辑标识稳定且唯一，照片只使用经过验证的本地数据。
func TestIndustryEditableTargetsUnique(t *testing.T) {
	for _, item := range industryTemplateCatalog() {
		t.Run(item.Key, func(t *testing.T) {
			pkg, err := industryTemplatePackage(item.Key, defaultTemplateScripts())
			if err != nil {
				t.Fatal(err)
			}
			raw := templateFileMap(pkg)["index.html"].Content
			if bytes.Contains(raw, []byte("{{")) {
				t.Fatal("行业页面仍有未物化的占位内容")
			}
			doc, err := xhtml.Parse(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			seen := map[string]bool{}
			// walk 校验静态编辑目标不重复，不依赖CSS选择器的偶然顺序。
			var walk func(*xhtml.Node)
			walk = func(node *xhtml.Node) {
				for _, attr := range node.Attr {
					if attr.Key != "data-gc-edit-id" {
						continue
					}
					if attr.Val == "" || seen[attr.Val] {
						t.Fatalf("静态编辑标识为空或重复：%q", attr.Val)
					}
					seen[attr.Val] = true
				}
				for child := node.FirstChild; child != nil; child = child.NextSibling {
					walk(child)
				}
			}
			walk(doc)
			for _, target := range []string{item.Key + "-layout", "chat-panel", "chat-messages", "message-input", "send-button", "leave-message"} {
				if !seen[target] {
					t.Fatalf("缺少必需编辑目标：%s", target)
				}
			}
			if len(seen) < 25 {
				t.Fatal("行业静态元素没有提供完整的点选编辑标识")
			}
		})
	}
}

// TestIndustryIntroAlwaysExits 执行真实扩展脚本，验证资源就绪、超时、编辑和减少动画都会解除蒙层。
func TestIndustryIntroAlwaysExits(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("扩展脚本执行验收需要本机Node运行时")
	}
	source, _ := json.Marshal(industryDialogScript)
	harness := `import assert from 'node:assert/strict';import {runInNewContext} from 'node:vm';
/** 执行一次本地开场，不创建真实网络与客服会话。 */
function run({editor=false,query='',reduced=false,complete=false}={}){
 const timers=[],events=new Map(),meter={value:20},label={textContent:''};
 const intro={hidden:false,querySelector(selector){return selector==='progress'?meter:label}};
 const document={body:{dataset:{gcEditor:editor?'true':'false'}},readyState:complete?'complete':'loading',querySelectorAll(){return[]},querySelector(selector){return selector==='[data-gc-intro]'?intro:null}};
 const window={location:{search:query},matchMedia(){return{matches:reduced}},addEventListener(name,handler){events.set(name,handler)}};
 runInNewContext(` + string(source) + `,{window,document,URLSearchParams,setTimeout(handler,delay){timers.push({handler,delay})}});
 return {intro,meter,label,timers,events};
}
for(const options of [{editor:true},{query:'?editor=1'},{reduced:true}]){const result=run(options);assert.equal(result.intro.hidden,true);assert.equal(result.timers.length,0)}
const waiting=run();assert.equal(waiting.intro.hidden,false);assert.equal(waiting.meter.value,65);assert.equal(waiting.timers.at(-1).delay,1800);waiting.timers.at(-1).handler();assert.equal(waiting.intro.hidden,true);
const loaded=run();loaded.events.get('load')();assert.equal(loaded.meter.value,100);assert.equal(loaded.label.textContent,'页面已就绪');loaded.timers.find(timer=>timer.delay===160).handler();assert.equal(loaded.intro.hidden,true);
const cached=run({complete:true});assert.equal(cached.meter.value,100);assert.equal(cached.timers.find(timer=>timer.delay===160).delay,160);`
	command := exec.Command(node, "--input-type=module")
	command.Stdin = strings.NewReader(harness)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("本地加载层不能保证退出：%v\n%s", err, output)
	}
}
