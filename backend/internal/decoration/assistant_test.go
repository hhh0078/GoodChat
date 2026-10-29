// 文件作用说明：验证助手JSON协议、配置归属加密、网络边界及真实图片压缩；假模型测试不代表真实供应商验收。
package decoration

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"supportdesk/backend/internal/platform"
)

// TestAssistantImageTimeoutBudget 验证图片请求具备至少十分钟等待预算，文本模型仍保持较短超时。
func TestAssistantImageTimeoutBudget(t *testing.T) {
	text := assistantHTTPClient()
	images := assistantImageHTTPClient()
	if text.Timeout != 110*time.Second || images.Timeout < 600*time.Second || images.Transport.(*http.Transport).ResponseHeaderTimeout < 600*time.Second {
		t.Fatal("文本与图片模型必须分别使用适当的超时预算")
	}
}

// TestAssistantSettingsSingletonLock 验证平台单例不会因管理员切换租户而使用不同锁，租户配置继续各自隔离。
func TestAssistantSettingsSingletonLock(t *testing.T) {
	if assistantSettingsLockKey("platform", "one") != assistantSettingsLockKey("platform", "two") {
		t.Fatal("平台单例必须共用固定锁")
	}
	if assistantSettingsLockKey("tenant", "one") == assistantSettingsLockKey("tenant", "two") {
		t.Fatal("租户配置应按租户分别锁定")
	}
}

// TestAssistantLegacyCreationRetired 验证旧版全局模型入口已关闭，不能绕过私有配置及固定脚本生成约束。
func TestAssistantLegacyCreationRetired(t *testing.T) {
	s := &Service{}
	response := httptest.NewRecorder()
	s.createAIJob(response, httptest.NewRequest("POST", "/api/v1/tenant/ai-design-jobs", strings.NewReader(`{"render_mode":"code"}`)))
	if response.Code != 409 || !strings.Contains(response.Body.String(), "assistant_required") {
		t.Fatal("旧生成入口必须明确引导新助手")
	}
}

// TestAssistantImportedMetadataExcluded 验证导出后重导入的清单不会成为AI源码，核心字节也由平台重新注入。
func TestAssistantImportedMetadataExcluded(t *testing.T) {
	pkg, err := industryTemplatePackage("finance", defaultTemplateScripts())
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := checkStaticFile("template.json", []byte(`{"name":"portable"}`))
	if err != nil {
		t.Fatal(err)
	}
	pkg.Files = append(pkg.Files, metadata)
	files, _, err := assistantSplitBasePackage(pkg)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.Path == "template.json" || file.Path == templateContractPath || strings.HasPrefix(file.Path, "static/goodchat/") {
			t.Fatal("不可编辑导出清单和核心文件不应交给AI")
		}
	}
	if _, err := buildTemplatePackage("index.html", files, defaultTemplateScripts()); err != nil {
		t.Fatal("导入模板的可编辑文件应可再次打包")
	}
}

// assistantRoundTripper 在测试中拦截HTTPS模型请求，不连接任何外部服务。
type assistantRoundTripper func(*http.Request) (*http.Response, error)

// RoundTrip 将请求交给受控测试函数。
func (f assistantRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestAssistantProtocol 拒绝多份JSON、路径穿越、文本夹带文件及重复图片目标。
func TestAssistantProtocol(t *testing.T) {
	for _, raw := range []string{`{"schema_version":1,"type":"text","message":"你好"}`, `{"schema_version":1,"type":"files","message":"完成","files":[{"path":"index.html","content":"<main>test</main>"}],"image_tasks":[{"id":"hero","path":"static/hero.jpg","prompt":"自然绿色的服务插画","width":1024,"height":1024}]}`} {
		if _, err := parseAssistantOutput(raw); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{`{"schema_version":1,"type":"text","message":"你好"} {}`, `{"schema_version":1,"type":"text","message":"你好","files":[{"path":"index.html","content":"x"}]}`, `{"schema_version":1,"type":"files","files":[{"path":"../index.html","content":"x"}]}`, `{"schema_version":1,"type":"files","files":[{"path":"index.html","content":"x"},{"path":"INDEX.html","content":"x"}]}`, `{"schema_version":1,"type":"files","files":[{"path":"static/x.png","content":"fake"}]}`, `{"schema_version":1,"type":"unknown","message":"x"}`} {
		if _, err := parseAssistantOutput(raw); err == nil {
			t.Fatal("非法联合JSON被接受")
		}
	}
}

// TestAssistantSecretBinding 验证密钥密文不包含原文且无法跨租户、平台或密文篡改读取。
func TestAssistantSecretBinding(t *testing.T) {
	s := &Service{cfg: platform.Config{AuthSecret: strings.Repeat("s", 40)}}
	secret := "test-private-ai-key"
	encrypted, err := s.assistantSecret(secret, "tenant:one", false)
	if err != nil || strings.Contains(encrypted, secret) {
		t.Fatal("加密失败")
	}
	plain, err := s.assistantSecret(encrypted, "tenant:one", true)
	if err != nil || plain != secret {
		t.Fatal("解密失败")
	}
	for _, owner := range []string{"tenant:two", "platform"} {
		if _, err := s.assistantSecret(encrypted, owner, true); err == nil {
			t.Fatal("密文不能跨归属解密")
		}
	}
	raw, _ := base64.RawStdEncoding.DecodeString(encrypted)
	raw[len(raw)-1] ^= 1
	if _, err := s.assistantSecret(base64.RawStdEncoding.EncodeToString(raw), "tenant:one", true); err == nil {
		t.Fatal("篡改密文被接受")
	}
}

// TestAssistantNetworkBoundary 验证地址规范化及直接IP的内网边界。
func TestAssistantNetworkBoundary(t *testing.T) {
	endpoint, err := assistantEndpoint("https://model.example.com/compatible-mode/v1", "chat/completions")
	if err != nil || endpoint != "https://model.example.com/compatible-mode/v1/chat/completions" {
		t.Fatal("兼容地址解析错误")
	}
	for _, raw := range []string{"http://example.com/v1", "https://localhost/v1", "https://127.0.0.1/v1", "https://169.254.169.254/v1", "https://user:password@example.com/v1", "https://example.com/v1?secret=x"} {
		if _, err := assistantEndpoint(raw, "chat/completions"); err == nil {
			t.Fatal("危险模型地址被接受")
		}
	}
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "172.16.1.1", "192.168.0.1", "100.64.1.1", "169.254.1.1", "::1", "fc00::1", "2001:db8::1"} {
		if assistantPublicIP(net.ParseIP(ip)) {
			t.Fatal("禁止网络地址被接受")
		}
	}
	if !assistantPublicIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("正常公网IP被拒")
	}
}

// TestAssistantImageNormalization 验证压缩尺寸、MIME、解码与非法伪图片拒绝。
func TestAssistantImageNormalization(t *testing.T) {
	picture := image.NewNRGBA(image.Rect(0, 0, 2400, 1200))
	for y := 0; y < 1200; y++ {
		for x := 0; x < 2400; x++ {
			picture.SetNRGBA(x, y, color.NRGBA{R: 80, G: 170, B: 130, A: 255})
		}
	}
	var raw bytes.Buffer
	if err := png.Encode(&raw, picture); err != nil {
		t.Fatal(err)
	}
	clean, mime, width, height, err := assistantNormalizeImage(raw.Bytes(), "")
	if err != nil || mime != "image/png" || width != 1600 || height != 800 || len(clean) > 1<<20 {
		t.Fatal("规范化失败")
	}
	if _, _, err := image.Decode(bytes.NewReader(clean)); err != nil {
		t.Fatal("规范化图片不能解码")
	}
	if _, _, _, _, err := assistantNormalizeImage([]byte("<svg></svg>"), ""); err == nil {
		t.Fatal("伪图片被接受")
	}
}

// TestAssistantImagePreservesUsefulInput 验证小图、透明PNG和长截图不进行无收益压缩，生成素材匹配目标格式且不会丢弃GIF动画帧。
func TestAssistantImagePreservesUsefulInput(t *testing.T) {
	for _, size := range []image.Point{{32, 32}, {600, 5000}} {
		picture := image.NewNRGBA(image.Rect(0, 0, size.X, size.Y))
		picture.SetNRGBA(0, 0, color.NRGBA{R: 120, A: 90})
		var buffer bytes.Buffer
		_ = png.Encode(&buffer, picture)
		clean, mime, width, height, err := assistantNormalizeImage(buffer.Bytes(), "")
		if err != nil || mime != "image/png" || width != size.X || height != size.Y || !bytes.Equal(clean, buffer.Bytes()) {
			t.Fatal("无需压缩的透明图和清晰长截图应保留原图")
		}
	}
	var jpegBytes bytes.Buffer
	_ = jpeg.Encode(&jpegBytes, image.NewRGBA(image.Rect(0, 0, 64, 64)), &jpeg.Options{Quality: 75})
	clean, mime, _, _, err := assistantNormalizeImage(jpegBytes.Bytes(), "")
	if err != nil || mime != "image/jpeg" || !bytes.Equal(clean, jpegBytes.Bytes()) {
		t.Fatal("已在预算内的JPEG不应再次有损压缩")
	}
	converted, mime, _, _, err := assistantNormalizeImage(jpegBytes.Bytes(), ".png")
	_, format, decodeErr := image.Decode(bytes.NewReader(converted))
	if err != nil || decodeErr != nil || mime != "image/png" || format != "png" {
		t.Fatal("PNG目标必须实际编码为PNG")
	}
	var pngBytes bytes.Buffer
	_ = png.Encode(&pngBytes, image.NewNRGBA(image.Rect(0, 0, 16, 16)))
	converted, mime, _, _, err = assistantNormalizeImage(pngBytes.Bytes(), ".jpg")
	_, format, decodeErr = image.Decode(bytes.NewReader(converted))
	if err != nil || decodeErr != nil || mime != "image/jpeg" || format != "jpeg" {
		t.Fatal("JPEG目标必须实际编码为JPEG")
	}
	var animated bytes.Buffer
	frame := image.NewPaletted(image.Rect(0, 0, 2, 2), color.Palette{color.Black, color.White})
	_ = gif.EncodeAll(&animated, &gif.GIF{Image: []*image.Paletted{frame, frame}, Delay: []int{10, 10}})
	animatedResult, animatedMIME, _, _, err := assistantNormalizeImage(animated.Bytes(), "")
	if err != nil || animatedMIME != "image/gif" || !bytes.Equal(animatedResult, animated.Bytes()) {
		t.Fatal("预算内GIF必须保留全部动画字节")
	}
	decoded, err := gif.DecodeAll(bytes.NewReader(animatedResult))
	if err != nil || len(decoded.Image) != 2 {
		t.Fatal("GIF动画不能静默丢帧")
	}
}

// TestAssistantProviderUsesPrivateHeader 验证JSON协议与密钥不进入模型消息，供应商敏感响应不透传。
func TestAssistantProviderUsesPrivateHeader(t *testing.T) {
	s := &Service{}
	calls := 0
	s.assistantClient = &http.Client{Transport: assistantRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		raw, _ := io.ReadAll(r.Body)
		if r.Header.Get("Authorization") != "Bearer private-test-key" || strings.Contains(string(raw), "private-test-key") {
			t.Error("密钥鉴权边界错误")
		}
		var body map[string]any
		if json.Unmarshal(raw, &body) != nil || body["response_format"] == nil {
			t.Error("必须请求JSON响应")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"{\"schema_version\":1,\"type\":\"text\",\"message\":\"连接正常\"}"}}],"usage":{"total_tokens":12,"api_key":"must-not-leak"}}`)), Header: make(http.Header)}, nil
	})}
	output, usage, billing, err := s.assistantTextCall(context.Background(), assistantConfiguration{BaseURL: "https://example.com/v1", Model: "test-model", APIKey: "private-test-key"}, []map[string]any{{"role": "user", "content": "测试"}})
	if err != nil || output.Type != "text" || billing != "reported" || strings.Contains(string(usage), "must-not-leak") || calls != 1 {
		t.Fatal("模型响应处理失败")
	}
}
