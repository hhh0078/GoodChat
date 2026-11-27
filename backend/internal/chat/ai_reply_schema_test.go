// 文件作用说明：验证实际模型请求携带文字/话术分支约束，中文混合回复有序发送，生产复现的空正文仍被严格拒绝。
package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/eino-contrib/jsonschema"
)

// TestAIReplySchemaAndMixedDelivery 通过真实HTTP适配器核对工具参数，并验证文字与授权话术仍能在一条计划中有序提交。
func TestAIReplySchemaAndMixedDelivery(t *testing.T) {
	f := newFixture(t)
	var scriptID string
	var request struct {
		Tools []struct {
			Function struct {
				Name       string             `json:"name"`
				Parameters *jsonschema.Schema `json:"parameters"`
			} `json:"function"`
		} `json:"tools"`
	}
	var raw json.RawMessage
	model := setupAITest(t, f, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil || json.Unmarshal(raw, &request) != nil {
			t.Error("无法读取实际工具参数请求")
		}
		aiFixtureResponse(w, "submit_reply", aiPlan{Items: []aiReplyItem{{Type: "text", Text: "您好，以下是服务说明。"}, {Type: "script", ScriptID: scriptID, Version: 1}}}, "", true)
	})
	status, data := f.request("POST", "/api/v1/tenant/ai/scripts", f.owner, "", map[string]any{"title": "协议隔离说明", "description": "仅用于混合回复协议回归", "language": "zh-CN", "enabled": true, "blocks": []aiBlock{{Type: "text", Text: "这是授权的固定服务说明。"}}})
	scriptID = mustData(t, status, data)["id"].(string)
	b := defaultAIBot()
	b.Mode, b.ModelID, b.ScriptIDs = "auto", model, []string{scriptID}
	status, data = f.request("PUT", "/api/v1/me/ai/bot", f.owner, "", b)
	bot := mustData(t, status, data)["id"].(string)
	status, data = f.request("PUT", "/api/v1/tenant/ai/settings", f.owner, "", map[string]any{"default_bot_id": bot, "reception_mode": "ai_first"})
	mustData(t, status, data)
	token, _, conversation := f.visitor("")
	status, data = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "请介绍服务"})
	mustData(t, status, data)
	runAITestJob(t, f)
	if len(request.Tools) != 4 || strings.Contains(string(raw), `"strict"`) || strings.Contains(string(raw), `"oneOf"`) {
		t.Fatal("工具集合或供应商扩展发生不兼容变化")
	}
	var reply *jsonschema.Schema
	for _, tool := range request.Tools {
		if tool.Function.Name == "submit_reply" {
			reply = tool.Function.Parameters
		}
	}
	if reply == nil || reply.Type != "object" || !slices.Contains(reply.Required, "items") || reply.Properties == nil {
		t.Fatal("实际请求缺少回复参数约束")
	}
	items, ok := reply.Properties.Get("items")
	if !ok || items.Items == nil || len(items.Items.AnyOf) != 2 {
		t.Fatal("实际适配器没有发送文字与话术两分支")
	}
	for index, expected := range [][]string{{"type", "text"}, {"type", "script_id", "version"}} {
		branch := items.Items.AnyOf[index]
		if branch == nil || branch.Properties == nil || branch.Type != "object" || len(branch.Required) != len(expected) || branch.Properties.Len() != len(expected) || string(jsonValue(branch.AdditionalProperties)) != "false" {
			t.Fatal("回复分支仍容许缺失必填字段或混入其他类型字段")
		}
		kind, ok := branch.Properties.Get("type")
		if !ok || len(kind.Enum) != 1 || kind.Enum[0] != []string{"text", "script"}[index] {
			t.Fatal("回复分支没有明确区分文字与话术")
		}
		for _, field := range expected {
			if !slices.Contains(branch.Required, field) {
				t.Fatal("回复分支缺少必填字段", field)
			}
		}
	}
	var contents []string
	if err := f.admin.QueryRow(context.Background(), "SELECT array_agg(content ORDER BY event_seq) FROM messages WHERE conversation_id=$1 AND source='ai'", conversation).Scan(&contents); err != nil || !slices.Equal(contents, []string{"您好，以下是服务说明。", "这是授权的固定服务说明。"}) {
		t.Fatal("混合文字与话术回复没有按原顺序实际发送", err)
	}
}

// TestAIReplyRejectsObservedMalformedItems 防止把真实供应商缺少正文的参数修成假成功，严格保留话术版本与混合类型校验。
func TestAIReplyRejectsObservedMalformedItems(t *testing.T) {
	for _, raw := range []string{
		`{"items":[{"type":"text","version":1}]}`,
		`{"items":[{"type":"text","version":2}]}`,
		`{"items":[{"type":"text","text":"您好","version":1}]}`,
		`{"items":[{"type":"script","script_id":"allowed","version":1,"text":"混入正文"}]}`,
		`{"items":[{"type":"script","script_id":"allowed","version":2}]}`,
	} {
		var plan aiPlan
		if aiDecode(raw, &plan) == nil && validateAIPlan(plan, []aiScript{{ID: "allowed", Version: 1}}) == nil {
			t.Fatal("非法供应商参数绕过了原严格验证", raw)
		}
	}
}
