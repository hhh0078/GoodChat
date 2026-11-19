// 文件作用说明：在服务端模块之间传递当前平台文字模型，密钥禁止通过JSON响应序列化。
package platform

type TextModel struct {
	BaseURL   string
	Model     string
	APIKey    string `json:"-"`
	MaxInput  int
	MaxOutput int
	Extras    map[string]any `json:"-"`
}
