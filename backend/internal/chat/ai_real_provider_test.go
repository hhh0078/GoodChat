// 文件作用说明：显式读取私有配置执行真实Eino知识路由、媒体话术和人工交接验收，费率只用于隔离账本验算。
package chat

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"testing"
)

// TestAIRealProviderKnowledgeAndMedia 只有明确提供私有配置时才调用真实供应商，禁止输出密钥及原始供应商错误正文。
func TestAIRealProviderKnowledgeAndMedia(t *testing.T) {
	path := os.Getenv("GOODCHAT_REAL_AI_CONFIG")
	if path == "" {
		t.Skip("未提供真实供应商私有测试配置")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal("无法读取私有模型配置")
	}
	var cfg struct {
		Endpoint string `json:"endpoint"`
		Key      string `json:"key"`
		Model    string `json:"model"`
	}
	if json.Unmarshal(raw, &cfg) != nil || cfg.Key == "" {
		t.Fatal("私有模型配置无效")
	}
	f := newFixture(t)
	ctx := context.Background()
	f.s.cfg.AuthSecret = strings.Repeat("real-eino-isolation-", 3)
	// 本机VPN将公网域名映射到保留地址，仅此显式开发测试允许该网络；生产校验保持开启。
	f.s.cfg.CallbackAllowLocal = true
	id := newID()
	m := aiModel{ID: id, Name: "真实供应商隔离验收", BaseURL: strings.TrimSuffix(strings.TrimSpace(cfg.Endpoint), "/chat/completions"), Model: cfg.Model, Enabled: true, Version: 1, CostInput: 100000, CostOutput: 300000, SaleInput: 300000, SaleOutput: 600000, SupportsImages: true, MaxInput: 64000, MaxOutput: 1024}
	if e = f.s.validateAIModel(&m); e != nil {
		t.Fatal("私有模型地址无效")
	}
	secret, e := f.s.aiSecret(cfg.Key, id, false)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.admin.Exec(ctx, "INSERT INTO ai_chat_models(id,name,enabled) VALUES($1,$2,true)", id, m.Name); e != nil {
		t.Fatal(e)
	}
	if _, e = f.admin.Exec(ctx, "INSERT INTO ai_chat_model_versions(model_id,version,config,secret_ciphertext) VALUES($1,1,$2,$3)", id, jsonValue(m), secret); e != nil {
		t.Fatal(e)
	}
	if _, e = f.admin.Exec(ctx, "INSERT INTO ai_wallets(tenant_id,balance_micro) VALUES($1,100000000)", f.tenant); e != nil {
		t.Fatal(e)
	}
	ids := []string{}
	for _, k := range []struct{ title, body string }{{"售前产品与价格", "隔离验收产品Pro标准版固定价格为88元，不包含定制服务。"}, {"售后退款政策", "隔离验收产品申请退款需要在购买后7天内提交，并保留购买凭证。"}} {
		status, data := f.request("POST", "/api/v1/tenant/ai/knowledge", f.agent, "", map[string]any{"title": k.title, "body": k.body, "enabled": true})
		knowledge := mustData(t, status, data)["id"].(string)
		ids = append(ids, knowledge)
		runAITestJob(t, f)
		if f.countRows("SELECT count(*) FROM ai_knowledge_bases WHERE id=$1 AND status='ready' AND description<>''", knowledge) != 1 {
			var code string
			_ = f.admin.QueryRow(ctx, "SELECT error_code FROM ai_jobs WHERE knowledge_id=$1 ORDER BY created_at DESC LIMIT 1", knowledge).Scan(&code)
			t.Fatalf("真实描述生成未完成，安全错误分类:%s", code)
		}
	}
	// 最小容器头只验证真实模型选择ID及后端媒体消息落库；实际可播放文件由浏览器验收单独检查。
	pngBytes, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScLbtAAAAABJRU5ErkJggg==")
	wav := make([]byte, 48)
	copy(wav, "RIFF")
	binary.LittleEndian.PutUint32(wav[4:], 40)
	copy(wav[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(wav[16:], 16)
	binary.LittleEndian.PutUint16(wav[20:], 1)
	binary.LittleEndian.PutUint16(wav[22:], 1)
	binary.LittleEndian.PutUint32(wav[24:], 8000)
	binary.LittleEndian.PutUint32(wav[28:], 16000)
	binary.LittleEndian.PutUint16(wav[32:], 2)
	binary.LittleEndian.PutUint16(wav[34:], 16)
	copy(wav[36:], "data")
	binary.LittleEndian.PutUint32(wav[40:], 4)
	mp4 := append([]byte{0, 0, 0, 24}, []byte("ftypisom\x00\x00\x00\x00isommp42")...)
	blocks := []aiBlock{{Type: "text", Text: "以下是安装演示资料。"}}
	for _, media := range []struct {
		kind, name string
		data       []byte
	}{{"image", "demo.png", pngBytes}, {"video", "demo.mp4", mp4}, {"audio", "demo.wav", wav}} {
		status, data := uploadQuickReplyMedia(t, f, f.agent, media.name, media.data)
		if status != 201 {
			t.Fatalf("媒体上传失败:%d", status)
		}
		attachment := data["data"].(map[string]any)["id"].(string)
		status, data = f.request("POST", "/api/v1/tenant/ai/media", f.agent, "", map[string]any{"title": media.name, "attachment_id": attachment})
		mustData(t, status, data)
		blocks = append(blocks, aiBlock{Type: media.kind, MediaID: attachment})
	}
	status, data := f.request("POST", "/api/v1/tenant/ai/scripts", f.agent, "", map[string]any{"title": "安装媒体演示", "description": "当访客索要安装演示的图片、视频或语音时，使用此话术发送全部安装资料。", "language": "zh-CN", "enabled": true, "blocks": blocks})
	script := mustData(t, status, data)["id"].(string)
	bot := defaultAIBot()
	bot.Mode = "auto"
	bot.Settings.MaxTurns = 20
	bot.ModelID = id
	bot.KnowledgeIDs = ids
	bot.ScriptIDs = []string{script}
	status, data = f.request("PUT", "/api/v1/me/ai/bot", f.agent, "", bot)
	mustData(t, status, data)
	token, _, conversation := f.visitor("")
	f.online(f.agent)
	personalClaim(t, f, conversation, f.agent)
	// 第一条真实访客问题必须通过read_knowledge_bases读取售前正文。
	status, data = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "Pro标准版多少钱？"})
	mustData(t, status, data)
	runAITestJob(t, f)
	if f.countRows("SELECT count(*) FROM messages WHERE conversation_id=$1 AND source='ai' AND content LIKE '%88%'", conversation) == 0 {
		var code string
		_ = f.admin.QueryRow(ctx, "SELECT error_code FROM ai_jobs WHERE conversation_id=$1 ORDER BY created_at DESC LIMIT 1", conversation).Scan(&code)
		var meta []byte
		_ = f.admin.QueryRow(ctx, "SELECT coalesce(jsonb_agg(response_meta ORDER BY call_index),'[]'::jsonb) FROM ai_call_attempts WHERE job_id=(SELECT id FROM ai_jobs WHERE conversation_id=$1 ORDER BY created_at DESC LIMIT 1)", conversation).Scan(&meta)
		t.Fatalf("真实知识问答未回答88元，安全错误分类:%s，协议形状:%s", code, string(meta))
	}
	var planRaw []byte
	if e = f.admin.QueryRow(ctx, "SELECT result FROM ai_jobs WHERE conversation_id=$1 ORDER BY created_at DESC LIMIT 1", conversation).Scan(&planRaw); e != nil {
		t.Fatal(e)
	}
	var plan aiPlan
	if json.Unmarshal(planRaw, &plan) != nil || len(plan.ReadIDs) == 0 || plan.ReadIDs[0] != ids[0] {
		t.Fatal("真实模型没有读取匹配的售前知识库")
	}
	status, data = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "请把安装演示的图片、视频和语音都发给我。"})
	mustData(t, status, data)
	runAITestJob(t, f)
	for _, kind := range []string{"image", "file", "audio"} {
		if f.countRows("SELECT count(*) FROM messages WHERE conversation_id=$1 AND source='ai' AND message_type=$2 AND attachment_id IS NOT NULL", conversation, kind) != 1 {
			t.Fatalf("真实AI话术没有发送媒体类型:%s", kind)
		}
	}
	canvas := image.NewRGBA(image.Rect(0, 0, 128, 128))
	for y := 0; y < 128; y++ {
		for x := 0; x < 128; x++ {
			canvas.Set(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	var picture bytes.Buffer
	if e = png.Encode(&picture, canvas); e != nil {
		t.Fatal(e)
	}
	imageID := uploadAIVisitorImage(t, f, token, picture.Bytes())
	status, data = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "message_type": "image", "attachment_id": imageID, "content": "请识别这张图片的主要颜色，只根据图片回答。"})
	mustData(t, status, data)
	runAITestJob(t, f)
	if f.countRows("SELECT count(*) FROM ai_jobs WHERE conversation_id=$1 AND image_inputs>0 AND input_tokens>0 AND status='succeeded' AND sale_micro>0", conversation) != 1 {
		var code string
		_ = f.admin.QueryRow(ctx, "SELECT error_code FROM ai_jobs WHERE conversation_id=$1 ORDER BY created_at DESC LIMIT 1", conversation).Scan(&code)
		var meta []byte
		_ = f.admin.QueryRow(ctx, "SELECT response_meta FROM ai_call_attempts WHERE job_id=(SELECT id FROM ai_jobs WHERE conversation_id=$1 ORDER BY created_at DESC LIMIT 1) ORDER BY call_index DESC LIMIT 1", conversation).Scan(&meta)
		t.Fatalf("真实图片识别与计费失败:%s，协议形状:%s", code, string(meta))
	}
	if f.countRows("SELECT count(*) FROM messages WHERE conversation_id=$1 AND source='ai' AND (content LIKE '%红%' OR content ILIKE '%red%')", conversation) == 0 {
		t.Fatal("真实模型没有识别纯红访客图片")
	}
	status, data = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "我要人工客服，请停止机器人回复。"})
	mustData(t, status, data)
	runAITestJob(t, f)
	if f.countRows("SELECT count(*) FROM conversations WHERE id=$1 AND ai_paused", conversation) != 1 {
		t.Fatal("真实模型未转人工")
	}
	var input, output, charged int64
	var calls int
	if e = f.admin.QueryRow(ctx, "SELECT count(*),coalesce(sum(input_tokens),0),coalesce(sum(output_tokens),0),coalesce(sum(sale_micro),0) FROM ai_call_attempts WHERE tenant_id=$1 AND status='reported'", f.tenant).Scan(&calls, &input, &output, &charged); e != nil {
		t.Fatal(e)
	}
	if input <= 0 || output <= 0 || calls < 6 || f.countRows("SELECT count(*) FROM ai_wallet_entries WHERE kind='charge'") != 6 {
		t.Fatal("真实用量或账本不完整")
	}
	t.Logf("真实Eino验收通过：模型%s，调用%d次，输入%d Token，输出%d Token；已配置正式费率计算消费%d微元。", cfg.Model, calls, input, output, charged)
}

// uploadAIVisitorImage 通过访客凭证上传真实图片，不复用客服附件或绕过附件所有权。
func uploadAIVisitorImage(t *testing.T, f *fixture, token string, data []byte) string {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, e := form.CreateFormFile("file", "visitor-red.png")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = part.Write(data); e != nil {
		t.Fatal(e)
	}
	if e = form.Close(); e != nil {
		t.Fatal(e)
	}
	req, e := http.NewRequest("POST", f.server.URL+"/api/v1/attachments", &body)
	if e != nil {
		t.Fatal(e)
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	response, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	var result struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if response.StatusCode != 201 || json.NewDecoder(response.Body).Decode(&result) != nil || result.Data.ID == "" {
		t.Fatal("访客真实图片上传失败")
	}
	return result.Data.ID
}
