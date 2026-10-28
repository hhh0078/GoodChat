// 文件作用说明：统一平台默认头像与租户上传头像引用的安全展示地址，供身份和聊天投影复用。
package platform

import "regexp"

var defaultAvatarPattern = regexp.MustCompile(`^default-(0[1-9]|[1-4][0-9]|50)$`)
var uploadedAvatarPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// AvatarURL 将已授权的头像引用转换为本站公开图片地址，非法引用返回空字符串。
// ref 只能为固定平台头像编号或上传图片UUID，不接受任意URL。
func AvatarURL(ref string) string {
	if defaultAvatarPattern.MatchString(ref) {
		return "/images/avatars/" + ref + ".png"
	}
	if uploadedAvatarPattern.MatchString(ref) {
		return "/api/v1/avatar-assets/" + ref
	}
	return ""
}

// AvatarURLSQL 为服务端固定表别名生成头像投影，别名不接受请求数据。
// alias 应为调用处固定的SQL表别名，例如m或tm；返回SQL片段而非用户输入。
func AvatarURLSQL(alias string) string {
	if alias != "m" && alias != "tm" {
		panic("头像SQL只允许服务端预定义成员表别名")
	}
	ref := alias + ".avatar_ref"
	return "CASE WHEN " + ref + " LIKE 'default-%' THEN '/images/avatars/'||" + ref + "||'.png' WHEN " + ref + "<>'' THEN '/api/v1/avatar-assets/'||" + ref + " ELSE '' END"
}
