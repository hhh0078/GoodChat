// 文件作用说明：为混合回复工具定义互斥的文字与授权话术参数分支，避免模型遗漏正文或混入另一类型的字段。
package chat

import (
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
)

// aiReplyParams 沿用已验证兼容的嵌套anyOf，文字必填正文，话术必填编号及版本，不启用供应商strict扩展。
func aiReplyParams() *schema.ParamsOneOf {
	text := jsonschema.NewProperties()
	text.Set("type", &jsonschema.Schema{Type: "string", Enum: []any{"text"}})
	text.Set("text", &jsonschema.Schema{Type: "string", Description: "Required nonempty visitor reply text; never omit this field."})
	script := jsonschema.NewProperties()
	script.Set("type", &jsonschema.Schema{Type: "string", Enum: []any{"script"}})
	script.Set("script_id", &jsonschema.Schema{Type: "string", Description: "Exact authorized script ID."})
	script.Set("version", &jsonschema.Schema{Type: "integer", Description: "Exact authorized version."})
	properties := jsonschema.NewProperties()
	properties.Set("items", &jsonschema.Schema{Type: "array", Description: `Ordered 1 to 6 reply items. Text: {"type":"text","text":"您好，请问有什么可以帮助您？"}. Script: {"type":"script","script_id":"exact authorized ID","version":1}.`, Items: &jsonschema.Schema{AnyOf: []*jsonschema.Schema{
		{Type: "object", Properties: text, Required: []string{"type", "text"}, AdditionalProperties: jsonschema.FalseSchema},
		{Type: "object", Properties: script, Required: []string{"type", "script_id", "version"}, AdditionalProperties: jsonschema.FalseSchema},
	}}})
	return schema.NewParamsOneOfByJSONSchema(&jsonschema.Schema{Type: "object", Properties: properties, Required: []string{"items"}, AdditionalProperties: jsonschema.FalseSchema})
}
