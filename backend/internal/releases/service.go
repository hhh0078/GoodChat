// Package releases 管理跨平台客户端发布、数字版本比较和公开下载策略。
package releases

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"supportdesk/backend/internal/platform"
)

var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,4})\.(0|[1-9][0-9]{0,4})\.(0|[1-9][0-9]{0,4})$`)

// VersionParts 校验可写入两端系统文件属性的三段版本，不使用字符串排序。
func VersionParts(value string) ([3]int, error) {
	var result [3]int
	if !versionPattern.MatchString(value) {
		return result, errors.New("版本号须为三段数字，例如1.0.0")
	}
	for i, part := range strings.Split(value, ".") {
		n, err := strconv.Atoi(part)
		if err != nil || n > 65535 {
			return result, errors.New("版本号每段不能超过65535")
		}
		result[i] = n
	}
	return result, nil
}

// Required 仅当最新版本同时高于当前版本和0.0.1时强制更新，相同版本不循环拦截。
func Required(latest, current string) (bool, error) {
	next, err := VersionParts(latest)
	if err != nil {
		return false, err
	}
	old, err := VersionParts(current)
	if err != nil {
		return false, err
	}
	above := false
	for i := 0; i < 3; i++ {
		if next[i] != old[i] {
			above = next[i] > old[i]
			break
		}
	}
	return above && (next[0] > 0 || next[1] > 0 || next[2] > 1), nil
}

// Release 表示公开可见的更新内容，管理员身份另由服务端审计字段保存。
type Release struct {
	ID          string    `json:"id"`
	Version     string    `json:"version"`
	Description string    `json:"description"`
	WinURL      string    `json:"win_url"`
	MacURL      string    `json:"mac_url"`
	APKURL      string    `json:"apk_url"`
	Published   bool      `json:"published"`
	UpdatedAt   time.Time `json:"updated_at"`
}
type Service struct {
	pool *pgxpool.Pool
	auth platform.Authorizer
}

const columns = "id::text,version,description,win_url,mac_url,apk_url,published,updated_at"

// New 使用已有身份服务限制版本维护仅允许平台管理员。
func New(pool *pgxpool.Pool, auth platform.Authorizer) *Service {
	return &Service{pool: pool, auth: auth}
}

// Register 注册公开下载及管理员发布入口，客户端无需登录即可检查版本。
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/public/client-releases", s.listPublic)
	mux.HandleFunc("GET /api/v1/public/client-releases/latest", s.latest)
	mux.HandleFunc("GET /api/v1/platform/client-releases", s.auth.RequirePlatform(s.listAdmin))
	mux.HandleFunc("POST /api/v1/platform/client-releases", s.auth.RequirePlatform(s.save))
	mux.HandleFunc("PATCH /api/v1/platform/client-releases/{id}", s.auth.RequirePlatform(s.save))
}

// scan 以固定列顺序读取版本，避免泄漏内部审计身份。
func scan(row pgx.Row) (Release, error) {
	var v Release
	err := row.Scan(&v.ID, &v.Version, &v.Description, &v.WinURL, &v.MacURL, &v.APKURL, &v.Published, &v.UpdatedAt)
	return v, err
}

// listPublic 只公开已发布版本，下载页不缓存过期版本。
func (s *Service) listPublic(w http.ResponseWriter, r *http.Request) { s.list(w, r, true) }

// listAdmin 提供平台管理员完整草稿与已发布版本列表。
func (s *Service) listAdmin(w http.ResponseWriter, r *http.Request) { s.list(w, r, false) }

// list 按数字版本倒序读取有限发布记录。
func (s *Service) list(w http.ResponseWriter, r *http.Request, public bool) {
	q := "SELECT " + columns + " FROM client_releases"
	if public {
		q += " WHERE published"
	}
	q += " ORDER BY major DESC,minor DESC,patch DESC LIMIT 100"
	rows, err := s.pool.Query(r.Context(), q)
	if err != nil {
		platform.ErrorJSON(w, r, err)
		return
	}
	defer rows.Close()
	items := []Release{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			platform.ErrorJSON(w, r, err)
			return
		}
		items = append(items, v)
	}
	if err = rows.Err(); err != nil {
		platform.ErrorJSON(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	platform.JSON(w, 200, map[string]any{"items": items})
}

// latest 按目标平台选择有下载地址的最新发布，兼容尚未开发的APK。
func (s *Service) latest(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("platform")
	field := map[string]string{"win": "win_url", "mac": "mac_url", "apk": "apk_url"}[target]
	if field == "" {
		platform.ErrorJSON(w, r, platform.Fail(400, "invalid_platform", "请选择win、mac或apk平台"))
		return
	}
	current := r.URL.Query().Get("current_version")
	if _, err := VersionParts(current); err != nil {
		platform.ErrorJSON(w, r, platform.Fail(400, "invalid_version", err.Error()))
		return
	}
	v, err := scan(s.pool.QueryRow(r.Context(), "SELECT "+columns+" FROM client_releases WHERE published AND "+field+"<>'' ORDER BY major DESC,minor DESC,patch DESC LIMIT 1"))
	w.Header().Set("Cache-Control", "no-store")
	if errors.Is(err, pgx.ErrNoRows) {
		platform.JSON(w, 200, map[string]any{"release": nil, "required": false, "current_version": current})
		return
	}
	if err != nil {
		platform.ErrorJSON(w, r, err)
		return
	}
	required, err := Required(v.Version, current)
	if err != nil {
		platform.ErrorJSON(w, r, err)
		return
	}
	download := map[string]string{"win": v.WinURL, "mac": v.MacURL, "apk": v.APKURL}[target]
	platform.JSON(w, 200, map[string]any{"release": v, "required": required, "download_url": download, "current_version": current})
}

// validURL 只接受无认证信息的HTTPS下载链接，允许暂未提供的系统留空。
func validURL(value string) bool {
	if value == "" {
		return true
	}
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Fragment == "" && !strings.ContainsAny(value, "\r\n\t") && len(value) <= 2000
}

// save 保存版本与编辑者，发布状态可撤回，但版本号不能重复。
func (s *Service) save(w http.ResponseWriter, r *http.Request) {
	var v Release
	if err := platform.Decode(w, r, &v); err != nil {
		platform.ErrorJSON(w, r, err)
		return
	}
	parts, err := VersionParts(v.Version)
	if err != nil {
		platform.ErrorJSON(w, r, platform.Fail(400, "invalid_version", err.Error()))
		return
	}
	if len([]rune(v.Description)) > 5000 || !validURL(v.WinURL) || !validURL(v.MacURL) || !validURL(v.APKURL) || (v.Published && v.WinURL == "" && v.MacURL == "" && v.APKURL == "") {
		platform.ErrorJSON(w, r, platform.Fail(400, "invalid_release", "更新说明最多5000字，下载地址须为HTTPS；发布时至少填写一个下载地址"))
		return
	}
	user := platform.Current(r).UserID
	if r.Method == http.MethodPost {
		v, err = scan(s.pool.QueryRow(r.Context(), "INSERT INTO client_releases(version,major,minor,patch,description,win_url,mac_url,apk_url,published,created_by,updated_by)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$10) RETURNING "+columns, v.Version, parts[0], parts[1], parts[2], v.Description, v.WinURL, v.MacURL, v.APKURL, v.Published, user))
	} else {
		v, err = scan(s.pool.QueryRow(r.Context(), "UPDATE client_releases SET version=$2,major=$3,minor=$4,patch=$5,description=$6,win_url=$7,mac_url=$8,apk_url=$9,published=$10,updated_by=$11,updated_at=now() WHERE id::text=$1 RETURNING "+columns, r.PathValue("id"), v.Version, parts[0], parts[1], parts[2], v.Description, v.WinURL, v.MacURL, v.APKURL, v.Published, user))
	}
	if errors.Is(err, pgx.ErrNoRows) {
		err = platform.Fail(404, "release_not_found", "版本不存在")
	}
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) && pgerr.Code == "23505" {
		err = platform.Fail(409, "version_exists", "该版本号已存在")
	}
	if err != nil {
		platform.ErrorJSON(w, r, err)
		return
	}
	platform.JSON(w, 200, v)
}
