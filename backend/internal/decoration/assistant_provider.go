// 文件作用说明：以禁止内网及重定向的连接调用租户模型，严格读取文本与文件JSON合约，并逐张请求真实图片模型。
package decoration

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"supportdesk/backend/internal/platform"
)

// assistantImageTask 表示模型提出的图片任务，图片实际字节必须由程序调用图片模型获得。
type assistantImageTask struct {
	ID     string `json:"id"`
	Path   string `json:"path"`
	Prompt string `json:"prompt"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// assistantOutput 是模型唯一允许返回的文本或静态文件联合协议。
type assistantOutput struct {
	SchemaVersion int                  `json:"schema_version"`
	Type          string               `json:"type"`
	Message       string               `json:"message"`
	Entrypoint    string               `json:"entrypoint,omitempty"`
	Files         []TemplateFile       `json:"files,omitempty"`
	ImageTasks    []assistantImageTask `json:"image_tasks,omitempty"`
}

// assistantEndpoint 规范OpenAI兼容地址，配置不能包含内网主机、凭据、查询或非HTTPS协议。
func assistantEndpoint(raw, operation string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(raw) > 2048 {
		return "", platform.Fail(400, "assistant_endpoint_invalid", "AI地址须为不含参数和凭据的HTTPS地址")
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || !strings.Contains(host, ".") {
		return "", platform.Fail(400, "assistant_endpoint_invalid", "AI地址不能指向本机或内网")
	}
	if ip := net.ParseIP(host); ip != nil && !assistantPublicIP(ip) {
		return "", platform.Fail(400, "assistant_endpoint_invalid", "AI地址不能指向本机或内网")
	}
	endpointPath := strings.TrimRight(u.Path, "/")
	for _, suffix := range []string{"/chat/completions", "/images/generations"} {
		endpointPath = strings.TrimSuffix(endpointPath, suffix)
	}
	if endpointPath == "" {
		endpointPath = "/v1"
	}
	u.Path = endpointPath + "/" + operation
	return u.String(), nil
}

// assistantPublicIP 拒绝私有、回环、链路本地、共享地址和非单播目标。
func assistantPublicIP(ip net.IP) bool {
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return false
	}
	for _, raw := range []string{"100.64.0.0/10", "192.0.0.0/24", "198.18.0.0/15", "192.0.2.0/24", "198.51.100.0/24", "203.0.113.0/24", "2001:db8::/32"} {
		_, block, _ := net.ParseCIDR(raw)
		if block.Contains(ip) {
			return false
		}
	}
	return true
}

// assistantHTTPClient 在每次建立实际连接时检查DNS全部地址并直接拨号已验证IP，避免DNS重绑定与代理绕过。
func assistantHTTPClient() *http.Client {
	transport := &http.Transport{TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 100 * time.Second, MaxIdleConns: 10, IdleConnTimeout: 30 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil || len(ips) == 0 {
			return nil, platform.Fail(502, "ai_provider_unreachable", "无法连接模型服务")
		}
		for _, candidate := range ips {
			if !assistantPublicIP(candidate.IP) {
				return nil, platform.Fail(400, "assistant_endpoint_invalid", "模型服务地址解析到了禁止访问的网络")
			}
		}
		var last error
		for _, candidate := range ips {
			conn, e := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, net.JoinHostPort(candidate.IP.String(), port))
			if e == nil {
				return conn, nil
			}
			last = e
		}
		return nil, last
	}
	return &http.Client{Transport: transport, Timeout: 110 * time.Second, CheckRedirect: rejectRedirect}
}

// assistantImageHTTPClient 为真实生图及结果下载保留650秒等待预算，网络目标校验仍与文本模型一致。
func assistantImageHTTPClient() *http.Client {
	client := assistantHTTPClient()
	client.Timeout = 650 * time.Second
	client.Transport.(*http.Transport).ResponseHeaderTimeout = 650 * time.Second
	return client
}

// parseAssistantOutput 严格验证联合JSON、静态文本文件和图片任务，拒绝多对象及重复目标。
func parseAssistantOutput(raw string) (assistantOutput, error) {
	var output assistantOutput
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&output) != nil || decoder.Decode(new(any)) != io.EOF {
		return output, platform.Fail(502, "assistant_output_invalid", "模型未返回约定的单个JSON对象")
	}
	if output.SchemaVersion != 1 || len(output.Message) > 16000 {
		return output, invalidDesign("模型响应版本或说明长度无效")
	}
	if output.Type == "text" {
		if len(output.Files) > 0 || len(output.ImageTasks) > 0 || strings.TrimSpace(output.Message) == "" {
			return output, invalidDesign("文本响应不能包含生成文件")
		}
		return output, nil
	}
	if output.Type != "files" || len(output.Files) == 0 || len(output.Files) > 100 || len(output.ImageTasks) > 4 {
		return output, invalidDesign("模型应返回text或files，文件不超过100个、图片不超过4张")
	}
	if output.Entrypoint == "" {
		output.Entrypoint = "index.html"
	}
	if err := packagePath(output.Entrypoint); err != nil {
		return output, err
	}
	seen := map[string]bool{}
	total := 0
	for _, file := range output.Files {
		if err := packagePath(file.Path); err != nil {
			return output, err
		}
		key := strings.ToLower(file.Path)
		if seen[key] {
			return output, invalidDesign("模型文件路径重复")
		}
		seen[key] = true
		total += len(file.Content)
		if len(file.Content) > 2<<20 || total > 4<<20 {
			return output, invalidDesign("模型生成的文本文件过大")
		}
		switch strings.ToLower(path.Ext(file.Path)) {
		case ".html", ".htm", ".css", ".js", ".mjs", ".json", ".txt":
		default:
			return output, invalidDesign("模型files只能返回静态文本文件，图片必须使用image_tasks")
		}
	}
	taskIDs := map[string]bool{}
	for _, task := range output.ImageTasks {
		if len(task.ID) < 1 || len(task.ID) > 100 || taskIDs[task.ID] || len(task.Prompt) < 5 || len(task.Prompt) > 4000 || task.Width < 128 || task.Width > 2048 || task.Height < 128 || task.Height > 2048 {
			return output, invalidDesign("图片任务编号、描述或尺寸无效")
		}
		taskIDs[task.ID] = true
		if err := packagePath(task.Path); err != nil {
			return output, err
		}
		if !strings.HasPrefix(task.Path, "static/") || strings.HasPrefix(task.Path, "static/goodchat/") || seen[strings.ToLower(task.Path)] {
			return output, invalidDesign("图片目标必须为static内独立资源路径")
		}
		seen[strings.ToLower(task.Path)] = true
		if ext := strings.ToLower(path.Ext(task.Path)); ext != ".jpg" && ext != ".jpeg" && ext != ".png" {
			return output, invalidDesign("生成图片目标扩展名应为jpg、jpeg或png")
		}
	}
	return output, nil
}

// assistantProviderJSON 执行一次模型请求，错误只返回稳定分类，调用不完整时用量标记待核对。
func (s *Service) assistantProviderJSON(ctx context.Context, endpoint, key string, body any, imageRequest bool) ([]byte, json.RawMessage, string, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, nil, "not_called", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, nil, "not_called", err
	}
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Content-Type", "application/json")
	client := s.assistantClient
	if client == nil {
		client = assistantHTTPClient()
		if imageRequest {
			client = assistantImageHTTPClient()
		}
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, nil, "uncertain", platform.Fail(502, "ai_provider_unreachable", "模型调用未完整结束，用量待核对；已完成阶段已保留")
	}
	defer response.Body.Close()
	content, err := io.ReadAll(io.LimitReader(response.Body, 16<<20+1))
	if err != nil || len(content) > 16<<20 {
		return nil, nil, "uncertain", platform.Fail(502, "ai_response_invalid", "模型响应过大或不完整，已完成阶段已保留")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		code, message := "ai_provider_rejected", "模型服务拒绝了本次请求，请核对模型能力和配置"
		if response.StatusCode == 401 || response.StatusCode == 403 {
			code, message = "ai_provider_auth_failed", "AI授权失败，请更新当前配置的密钥或权限"
		}
		if response.StatusCode == 429 {
			code, message = "ai_provider_rate_limited", "AI服务限流或额度不足，请核对当前配置的供应商额度"
		}
		return nil, nil, "uncertain", platform.Fail(502, code, message)
	}
	var envelope struct {
		Usage json.RawMessage `json:"usage"`
	}
	_ = json.Unmarshal(content, &envelope)
	usage, reported := safeProviderUsage(envelope.Usage)
	billing := "uncertain"
	if reported {
		billing = "reported"
	}
	return content, usage, billing, nil
}

// assistantTextCall 发送受控系统约束和私有创作上下文，密钥只放HTTP鉴权头。
func (s *Service) assistantTextCall(ctx context.Context, cfg assistantConfiguration, messages []map[string]any) (assistantOutput, json.RawMessage, string, error) {
	endpoint, err := assistantEndpoint(cfg.BaseURL, "chat/completions")
	if err != nil {
		return assistantOutput{}, nil, "not_called", err
	}
	body := map[string]any{"model": cfg.Model, "messages": messages, "response_format": map[string]string{"type": "json_object"}, "max_tokens": 14000}
	if strings.HasPrefix(strings.ToLower(cfg.Model), "qwen") {
		body["enable_thinking"] = false
	}
	raw, usage, billing, err := s.assistantProviderJSON(ctx, endpoint, cfg.APIKey, body, false)
	if err != nil {
		return assistantOutput{}, usage, billing, err
	}
	var payload struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(raw, &payload) != nil || len(payload.Choices) == 0 {
		return assistantOutput{}, usage, billing, platform.Fail(502, "assistant_output_invalid", "模型未返回可用JSON内容")
	}
	if payload.Choices[0].FinishReason == "length" {
		return assistantOutput{}, usage, billing, platform.Fail(502, "assistant_output_truncated", "模型输出达到长度上限，未写入不完整模板；请缩小本次改动范围")
	}
	output, err := parseAssistantOutput(payload.Choices[0].Message.Content)
	return output, usage, billing, err
}

// assistantImageCall 调用独立图片模型并取回真实图片，不把远程图片地址写入访客模板。
func (s *Service) assistantImageCall(ctx context.Context, cfg assistantConfiguration, task assistantImageTask) ([]byte, string, json.RawMessage, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 650*time.Second)
	defer cancel()
	base, key := cfg.ImageBaseURL, cfg.ImageAPIKey
	if base == "" {
		base = cfg.BaseURL
	}
	if key == "" {
		key = cfg.APIKey
	}
	if cfg.ImageModel == "" {
		return nil, "", nil, "not_called", platform.Fail(503, "image_model_unconfigured", "当前配置尚未填写图片模型，请配置后重试图片阶段")
	}
	endpoint, err := assistantEndpoint(base, "images/generations")
	if err != nil {
		return nil, "", nil, "not_called", err
	}
	raw, usage, billing, err := s.assistantProviderJSON(ctx, endpoint, key, map[string]any{"model": cfg.ImageModel, "prompt": task.Prompt, "n": 1, "size": strconv.Itoa(task.Width) + "x" + strconv.Itoa(task.Height), "response_format": "b64_json"}, true)
	if err != nil {
		return nil, "", usage, billing, err
	}
	var payload struct {
		Data []struct {
			B64 string `json:"b64_json"`
			URL string `json:"url"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &payload) != nil || len(payload.Data) == 0 {
		return nil, "", usage, billing, platform.Fail(502, "image_response_invalid", "图片模型未返回实际图片")
	}
	var imageBytes []byte
	if payload.Data[0].B64 != "" {
		imageBytes, err = base64.StdEncoding.Strict().DecodeString(payload.Data[0].B64)
	} else {
		u, e := url.Parse(payload.Data[0].URL)
		if e != nil || u.Scheme != "https" || u.User != nil || u.Host == "" {
			return nil, "", usage, billing, platform.Fail(502, "image_response_invalid", "图片模型返回的图片地址无效")
		}
		request, e := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if e != nil {
			return nil, "", usage, billing, e
		}
		client := s.assistantClient
		if client == nil {
			client = assistantImageHTTPClient()
		}
		response, e := client.Do(request)
		if e != nil {
			return nil, "", usage, billing, platform.Fail(502, "image_download_failed", "图片生成后未能下载，已完成文件阶段已保留")
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			return nil, "", usage, billing, platform.Fail(502, "image_download_failed", "图片生成后未能下载")
		}
		imageBytes, err = io.ReadAll(io.LimitReader(response.Body, 10<<20+1))
	}
	if err != nil {
		return nil, "", usage, billing, platform.Fail(502, "image_response_invalid", "图片模型返回的图片无法读取")
	}
	clean, mime, _, _, err := assistantNormalizeImage(imageBytes, strings.ToLower(path.Ext(task.Path)))
	return clean, mime, usage, billing, err
}
