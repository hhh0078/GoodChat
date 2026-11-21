// 文件作用说明：提供平台九语言模板预览、乐观版本发布、回滚与已订阅公告发布。
package notifications

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/localization"
	"supportdesk/backend/internal/platform"
)

// validLanguage 校验明确支持语言和IANA时区，不接受静默降级的外部输入。
func validLanguage(locale, zone string) bool {
	return localization.Normalize(locale) == locale && localization.ValidTimezone(zone)
}

// templateInput 只允许受限文本编辑，不接收按钮网址或任意HTML。
type templateInput struct {
	Timezone string `json:"timezone"`
	Kind     string `json:"kind"`
	Locale   string `json:"locale"`
	Subject  string `json:"subject"`
	Body     string `json:"body"`
	Button   string `json:"button"`
	Base     int    `json:"base_version"`
	Restore  int    `json:"restore_version"`
}

// templates 返回该语言当前版本和可回滚历史；未发布时返回完整内置译文。
func (s *Service) templates(w http.ResponseWriter, r *http.Request) error {
	locale := localization.Normalize(r.URL.Query().Get("locale"))
	if locale == "" {
		locale = "en"
	}
	items := []map[string]any{}
	for _, d := range Definitions {
		subject, body, button := localization.Format(locale, d.Subject), localization.Format(locale, d.Body), localization.Format(locale, d.Button)
		version := 0
		rows, e := s.pool.Query(r.Context(), "SELECT version,subject,body,button,created_at FROM notification_templates WHERE kind=$1 AND locale=$2 ORDER BY version DESC LIMIT 20", d.Kind, locale)
		if e != nil {
			return e
		}
		history := []map[string]any{}
		for rows.Next() {
			var v int
			var a, b, c string
			var at time.Time
			if e := rows.Scan(&v, &a, &b, &c, &at); e != nil {
				rows.Close()
				return e
			}
			if version == 0 {
				version, subject, body, button = v, a, b, c
			}
			history = append(history, map[string]any{"version": v, "created_at": at})
		}
		rows.Close()
		if e := rows.Err(); e != nil {
			return e
		}
		items = append(items, map[string]any{"kind": d.Kind, "locale": locale, "subject": subject, "body": body, "button": button, "version": version, "history": history})
	}
	platform.JSON(w, 200, map[string]any{"items": items})
	return nil
}

// publish 将回滚也保存为新版本，并序列化同场景同语言的并发发布。
func (s *Service) publish(w http.ResponseWriter, r *http.Request) error {
	var in templateInput
	if e := platform.Decode(w, r, &in); e != nil {
		return e
	}
	if _, ok := Find(in.Kind); !ok || localization.Normalize(in.Locale) != in.Locale || in.Locale == "" {
		return platform.Fail(400, "invalid_notice_template", "邮件模板场景或语言无效")
	}
	var version int
	e := s.systemTx(r.Context(), func(tx pgx.Tx) error {
		ctx := r.Context()
		if _, e := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "mail-template:"+in.Kind+":"+in.Locale); e != nil {
			return e
		}
		if e := tx.QueryRow(ctx, "SELECT coalesce(max(version),0) FROM notification_templates WHERE kind=$1 AND locale=$2", in.Kind, in.Locale).Scan(&version); e != nil {
			return e
		}
		if version != in.Base {
			return platform.Fail(409, "notice_template_conflict", "邮件模板已更新，请重新读取")
		}
		if in.Restore > 0 {
			if e := tx.QueryRow(ctx, "SELECT subject,body,button FROM notification_templates WHERE kind=$1 AND locale=$2 AND version=$3", in.Kind, in.Locale, in.Restore).Scan(&in.Subject, &in.Body, &in.Button); e != nil {
				return platform.Fail(404, "notice_template_missing", "邮件模板版本不存在")
			}
		}
		definition, _ := Find(in.Kind)
		if e := ValidateFor(definition, in.Subject, in.Body, in.Button); e != nil {
			return platform.Fail(400, "invalid_notice_template", e.Error())
		}
		if strings.HasPrefix(in.Kind, "code_") && !strings.Contains(in.Body, "{code}") {
			return platform.Fail(400, "notice_code_required", "验证码模板必须保留验证码变量")
		}
		version++
		_, e := tx.Exec(ctx, "INSERT INTO notification_templates(kind,locale,version,subject,body,button,created_by) VALUES($1,$2,$3,$4,$5,$6,$7)", in.Kind, in.Locale, version, in.Subject, in.Body, in.Button, platform.Current(r).UserID)
		return e
	})
	if e != nil {
		return e
	}
	platform.JSON(w, 201, map[string]int{"version": version})
	return nil
}

// preview 仅使用演示变量渲染，不访问客户资料也不触发SMTP。
func (s *Service) preview(w http.ResponseWriter, r *http.Request) error {
	var in templateInput
	if e := platform.Decode(w, r, &in); e != nil {
		return e
	}
	d, ok := Find(in.Kind)
	if !ok || localization.Normalize(in.Locale) != in.Locale || in.Locale == "" {
		return platform.Fail(400, "invalid_notice_template", "邮件模板场景或语言无效")
	}
	zone := in.Timezone
	if zone == "" {
		zone = "UTC"
	}
	if !localization.ValidTimezone(zone) {
		return platform.Fail(400, "invalid_notice_locale", "请选择有效的语言与IANA时区")
	}
	mail, e := Render(platform.PublicURL(r.Context(), s.cfg.PublicURL), in.Locale, zone, d, map[string]string{"code": "123456", "token": "preview-only", "tenant": "GoodChat Demo", "name": "Demo", "balance": "CNY 5.00", "amount": "CNY 10.00", "reference": "DEMO", "sessions": "12", "tokens": "3456", "expires_at": LocalTime(time.Now().Add(7*24*time.Hour), zone)}, in.Subject, in.Body, in.Button, 0)
	if e != nil {
		return platform.Fail(400, "invalid_notice_template", e.Error())
	}
	platform.JSON(w, 200, mail)
	return nil
}

// announcement 仅向主动订阅的账号创建公告，限制长度且不允许富文本或外部链接。
func (s *Service) announcement(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Reference    string            `json:"reference"`
		Translations map[string]string `json:"translations"`
		Key          string            `json:"key"`
	}
	if e := platform.Decode(w, r, &in); e != nil {
		return e
	}
	for _, locale := range localization.Locales {
		if strings.TrimSpace(in.Translations[locale]) == "" || len([]rune(in.Translations[locale])) > 200 {
			return platform.Fail(400, "notice_announcement_languages", "请填写全部九种语言的公告摘要，每种最多200字")
		}
	}
	in.Reference = in.Translations["en"]
	if len(in.Key) < 8 || len(in.Key) > 100 {
		return platform.Fail(400, "invalid_notice_announcement", "请填写公告摘要与唯一发布编号")
	}
	count := 0
	e := s.systemTx(r.Context(), func(tx pgx.Tx) error {
		key := "announcement:" + in.Key
		if _, e := tx.Exec(r.Context(), "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", key); e != nil {
			return e
		}
		var previous string
		var oldTranslations map[string]string
		e := tx.QueryRow(r.Context(), "SELECT value->>'reference',coalesce(value->'translations','{}'::jsonb) FROM notification_state WHERE key=$1", key).Scan(&previous, &oldTranslations)
		if e == nil {
			oldJSON, _ := json.Marshal(oldTranslations)
			newJSON, _ := json.Marshal(in.Translations)
			if previous != in.Reference || string(oldJSON) != string(newJSON) {
				return platform.Fail(409, "notice_announcement_conflict", "同一发布编号不能使用不同公告内容")
			}
			return nil
		}
		if e != pgx.ErrNoRows {
			return e
		}
		data, _ := json.Marshal(map[string]any{"reference": in.Reference, "translations": in.Translations, "actor": platform.Current(r).UserID})
		if _, e := tx.Exec(r.Context(), "INSERT INTO notification_state(key,value) VALUES($1,$2)", key, data); e != nil {
			return e
		}
		rows, e := tx.Query(r.Context(), "SELECT user_id::text FROM mail_notification_preferences WHERE updates_enabled")
		if e != nil {
			return e
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if e := rows.Scan(&id); e != nil {
				rows.Close()
				return e
			}
			ids = append(ids, id)
		}
		rows.Close()
		if e := rows.Err(); e != nil {
			return e
		}
		for _, id := range ids {
			if e := Enqueue(r.Context(), tx, Event{User: id, Kind: "product_update", Key: "announcement:" + in.Key, Params: map[string]string{"reference": in.Reference, "_announcement": key}}); e != nil {
				return e
			}
			count++
		}
		return nil
	})
	if e != nil {
		return e
	}
	platform.JSON(w, 202, map[string]any{"queued_recipients": count, "reference": fmt.Sprint(in.Key)})
	return nil
}
