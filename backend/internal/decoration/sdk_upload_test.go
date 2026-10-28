// 文件作用说明：执行实际生成的装修SDK，验证磁盘File在子来源转为内存Blob、上传边界及脚本语法，不将VM替身当作真实浏览器验收。
package decoration

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// TestSDKUploadUsesMemoryBlob 执行完整SDK及合法握手，验证File和Blob字节、MIME、名称保留，并在非法输入时禁止读取文件及发送RPC。
func TestSDKUploadUsesMemoryBlob(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("SDK执行回归需要本机Node运行时")
	}
	script, err := json.Marshal(sdkScript("https://parent.example", "test-release", false))
	if err != nil {
		t.Fatal(err)
	}
	harness := `import assert from 'node:assert/strict';
import { runInNewContext, Script } from 'node:vm';
const source=` + string(script) + `;
new Script(source);
const events=new Map(),calls=[];
const parent={postMessage(){}};
const port={start(){},close(){},postMessage(call){calls.push(call);queueMicrotask(()=>this.onmessage({data:{id:call.id,result:{accepted:true}}}));}};
const window={addEventListener(name,handler){events.set(name,handler)},removeEventListener(name){events.delete(name)}};
runInNewContext(source,{window,parent,Blob,crypto:{randomUUID:()=> 'upload-test-nonce'},setInterval:()=>1,clearInterval(){},setTimeout,clearTimeout});
events.get('message')({source:parent,origin:'https://parent.example',ports:[port],data:{type:'goodchat:port',version:1,nonce:'upload-test-nonce',release_id:'test-release'}});
let reads=0;
/** DiskFile 模拟只能在子来源读取的磁盘文件，父层收到该实例就视为回归。 */
class DiskFile extends Blob {
 /** constructor 初始化带名称和原始MIME的磁盘文件替身。 */
 constructor(parts,options,name){super(parts,options);this.name=name;}
 /** arrayBuffer 记录子来源实际读取次数，拒绝非法输入时不应调用。 */
 async arrayBuffer(){reads++;return super.arrayBuffer();}
}
const original=new DiskFile(['real bytes'],{type:'audio/webm;codecs=opus'},'recording.webm');
await window.GoodChat.upload(original);
assert.equal(reads,1);
assert.equal(calls.length,1);
assert.equal(calls[0].method,'upload');
assert.equal(calls[0].payload.name,'recording.webm');
assert.equal(calls[0].payload.file instanceof Blob,true);
assert.equal(calls[0].payload.file instanceof DiskFile,false);
assert.notEqual(calls[0].payload.file,original);
assert.equal(calls[0].payload.file.type,'audio/webm;codecs=opus');
assert.equal(await calls[0].payload.file.text(),'real bytes');
await window.GoodChat.upload(new Blob(['video'],{type:'video/webm'}),'video.webm');
assert.equal(calls[1].payload.file.type,'video/webm');
assert.equal(await calls[1].payload.file.text(),'video');
await window.GoodChat.upload(new Blob(['unnamed']));
assert.equal(calls[2].payload.name,'附件');
const oversized=new Blob([new Uint8Array(20*1024*1024+1)]);
Object.defineProperty(oversized,'arrayBuffer',{value(){throw new Error('超限文件不应被读取')}});
for(const [file,name] of [[{},'bad'],[new Blob([]),'empty'],[oversized,'large'],[original,'x'.repeat(241)],[original,'  '],[original,{}]]) {
 await assert.rejects(window.GoodChat.upload(file,name));
}
assert.equal(reads,1);
assert.equal(calls.length,3);
const changed=new Blob(['before']);
Object.defineProperty(changed,'arrayBuffer',{value:async()=>new ArrayBuffer(1)});
await assert.rejects(window.GoodChat.upload(changed,'changed.txt'));
assert.equal(calls.length,3);
assert.equal(Object.isFrozen(window.GoodChat),true);
console.log('SDK内存Blob路径、MIME、非法大小名称和脚本语法均通过');
`
	command := exec.Command(node, "--input-type=module")
	command.Stdin = strings.NewReader(harness)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("SDK上传执行回归失败: %v\n%s", err, output)
	}
}
