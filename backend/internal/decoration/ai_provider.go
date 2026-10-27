// 文件作用说明：调用服务端配置的OpenAI兼容模型，只发送租户设计需求与草稿，不回传供应商敏感错误。
package decoration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"supportdesk/backend/internal/platform"
)

// aiEndpoint 兼容完整chat/completions地址、v1地址和供应商根地址。
func aiEndpoint(raw string, production bool) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") || production && u.Scheme != "https" {
		return "", platform.Fail(503, "ai_configuration_invalid", "平台模型地址配置无效")
	}
	path := strings.TrimRight(u.Path, "/")
	if !strings.HasSuffix(path, "/chat/completions") {
		if path == "" {
			path = "/v1"
		}
		path += "/chat/completions"
	}
	u.Path = path
	return u.String(), nil
}

// callProvider 请求真实模型的单个JSON设计，并区分未调用和结果待核对状态。
func (s *Service) callProvider(ctx context.Context, mode, prompt, model string, base json.RawMessage) (providerResult, error) {
	result := providerResult{Billing: "not_called", Usage: json.RawMessage(`{}`)}
	endpoint, err := aiEndpoint(s.cfg.AIEndpoint, s.cfg.Production)
	if err != nil {
		return result, err
	}
	contract := `输出一个JSON对象，禁止Markdown代码围栏。结构为{"schema_version":1,"theme":{"primary_color":"#1f6f5b","background":"#f6f7f4","text_color":"#17231f","radius":16},"brand":{"title":"标题","subtitle":"副标题","logo_url":""},"blocks":[{"id":"welcome","type":"welcome","title":"欢迎","content":"欢迎语","items":[]},{"id":"chat","type":"chat","title":"在线咨询","content":"","items":[]}],"custom_css":""}。区块type只允许welcome/announcement/faq/contact/chat，必须包含chat。颜色为六位十六进制。CSS禁止url和@import。`
	if mode == "code" {
		contract = `输出一个JSON对象{"html":"静态HTML片段","css":"静态CSS","javascript":"JavaScript"}，禁止Markdown代码围栏。HTML不包含html/head/body标签、外部脚本、外链、form、iframe、meta、base、object、embed。CSS禁止url和@import。静态代码在跨来源沙箱运行且不能访问外网。必须使用window.GoodChat.getState()/sendText(text,client_message_id?,reply_to_id?)/withdrawMessage(message_id)/typing(active)/subscribe(handler)/upload(blob,name)/download(id)/saveAttachment(id)/read(seq)绘制并操作当前授权聊天，不能获取任何令牌或指定会话路径。状态包含conversation及messages数组、is_typing和peer_read_seq；消息含is_own、sender_kind、sender_name、created_at、seq、text、reply和attachment。seq保持字符串，用BigInt比较。使用textContent显示消息，区分本人右侧气泡、客服和系统，提供引用、取消引用及本人消息撤回；撤回在气泡内确认，不使用沙箱禁止的window.confirm。提供发送输入框和真实可见的原生file input作为唯一添加附件控件，不使用hidden或代点。图片音频用download返回的Blob预览，文件保存必须saveAttachment请求可信父层确认，不能在沙箱内创建下载链接。`
	}
	system := "你是GoodChat访客咨询页面设计器。只处理主动提交的装修需求，不能改变鉴权、分配或业务权限。使用自然色系，不使用蓝紫渐变。不得包含任何外部请求或外部资源。" + contract
	user := "设计需求：" + prompt + "\n当前草稿（修改时保留未要求变更部分）：" + string(base)
	body, err := json.Marshal(map[string]any{"model": model, "messages": []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": user}}, "response_format": map[string]string{"type": "json_object"}})
	if err != nil {
		return result, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return result, err
	}
	request.Header.Set("Authorization", "Bearer "+s.cfg.AIKey)
	request.Header.Set("Content-Type", "application/json")
	result.Billing = "uncertain"
	response, err := s.client.Do(request)
	if err != nil {
		return result, platform.Fail(502, "ai_provider_unreachable", "模型请求未正常完成，供应商用量待核对；原草稿已保留")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024+1))
	if err != nil || len(raw) > 1024*1024 {
		return result, platform.Fail(502, "ai_response_invalid", "模型响应过大或不完整，原草稿已保留")
	}
	var payload struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage json.RawMessage `json:"usage"`
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			return result, platform.Fail(502, "ai_provider_auth_failed", "AI服务授权失败，请联系平台管理员更新模型密钥或权限；原草稿已保留")
		}
		if response.StatusCode == http.StatusTooManyRequests {
			return result, platform.Fail(502, "ai_provider_rate_limited", "模型服务当前额度不足或请求过多，请联系平台管理员核对供应商额度；原草稿已保留")
		}
		return result, platform.Fail(502, "ai_provider_rejected", "模型供应商暂未完成请求，请检查平台模型配置或稍后重试")
	}
	if json.Unmarshal(raw, &payload) != nil || len(payload.Choices) == 0 {
		return result, platform.Fail(502, "ai_response_invalid", "模型未返回可用的设计内容，原草稿已保留")
	}
	if usage, reported := safeProviderUsage(payload.Usage); reported {
		result.Usage = usage
		result.Billing = "reported"
	}
	content := strings.TrimSpace(payload.Choices[0].Message.Content)
	if strings.HasPrefix(content, "```") {
		lines := strings.Split(content, "\n")
		if len(lines) > 2 && strings.TrimSpace(lines[len(lines)-1]) == "```" {
			content = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}
	design, err := validateDesign(mode, json.RawMessage(content))
	if err != nil {
		var api *platform.APIError
		if errors.As(err, &api) {
			return result, platform.Fail(502, "ai_design_invalid", "模型设计未通过安全和结构校验："+api.Message)
		}
		return result, err
	}
	result.Design = design
	return result, nil
}

// safeProviderUsage 只保留已知非负整数用量，供应商额外元数据不得透传租户。
func safeProviderUsage(raw json.RawMessage) (json.RawMessage, bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return json.RawMessage(`{}`), false
	}
	usage := map[string]int64{}
	for _, key := range []string{"prompt_tokens", "completion_tokens", "total_tokens", "input_tokens", "output_tokens"} {
		value, err := strconv.ParseInt(string(fields[key]), 10, 64)
		if err == nil && value >= 0 {
			usage[key] = value
		}
	}
	if len(usage) == 0 {
		return json.RawMessage(`{}`), false
	}
	encoded, _ := json.Marshal(usage)
	return encoded, true
}
