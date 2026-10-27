// 文件作用说明：通过不可变快照、页面锁和预期线上版本实现原子发布、链接绑定与回滚审计。
package decoration

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// releaseInput 定义发布和回滚的并发保护字段。
type releaseInput struct {
	Version           int64    `json:"version,omitempty"`
	ExpectedReleaseID string   `json:"expected_release_id"`
	LinkIDs           []string `json:"link_ids,omitempty"`
	ReleaseID         string   `json:"release_id,omitempty"`
}

// releaseRequest 校验发布目标及受限链接清单，禁止重复或畸形编号。
func releaseRequest(input releaseInput, rollback bool) error {
	if input.ExpectedReleaseID != "" {
		if err := idValue(input.ExpectedReleaseID); err != nil {
			return err
		}
	}
	if rollback {
		if len(input.LinkIDs) > 0 || input.Version != 0 {
			return platform.Fail(400, "unsupported_field", "回滚不接受草稿版本或新增绑定")
		}
		return idValue(input.ReleaseID)
	}
	if input.ReleaseID != "" {
		return platform.Fail(400, "unsupported_field", "发布版本由服务器创建")
	}
	if input.Version < 1 {
		return conflict()
	}
	if len(input.LinkIDs) > 200 {
		return platform.Fail(400, "too_many_links", "单次发布最多绑定200个链接")
	}
	seen := map[string]bool{}
	for _, id := range input.LinkIDs {
		if err := idValue(id); err != nil {
			return err
		}
		if seen[id] {
			return platform.Fail(400, "duplicate_link", "发布链接编号重复")
		}
		seen[id] = true
	}
	return nil
}

// publishPage 固定当前草稿并在同一事务内切换页面和选中的链接。
func (s *Service) publishPage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := idValue(id); err != nil {
		respond(w, r, nil, err)
		return
	}
	var input releaseInput
	if err := platform.Decode(w, r, &input); err != nil {
		respond(w, r, nil, err)
		return
	}
	if err := releaseRequest(input, false); err != nil {
		respond(w, r, nil, err)
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(key) > 128 {
		respond(w, r, nil, platform.Fail(400, "invalid_idempotency_key", "幂等标识过长"))
		return
	}
	raw, _ := json.Marshal(input)
	requestHash := digest(raw)
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		ctx, p := r.Context(), platform.Current(r)
		var mode, current string
		var design json.RawMessage
		var version int64
		if err := tx.QueryRow(ctx, "SELECT render_mode,draft,version,coalesce(current_release_id::text,'') FROM decoration_pages WHERE id=$1 FOR UPDATE", id).Scan(&mode, &design, &version, &current); err != nil {
			return nil, err
		}
		if result, ok, err := replayAction(ctx, tx, id, "publish", key, requestHash); ok || err != nil {
			return result, err
		}
		if version != input.Version || current != input.ExpectedReleaseID {
			return nil, conflict()
		}
		if err := s.checkFeature(ctx, tx, p.TenantID, decorationFeature(mode)); err != nil {
			return nil, err
		}
		design, err := validateDesign(mode, design)
		if err != nil {
			return nil, err
		}
		links, err := publicationLinks(ctx, tx, id, input.LinkIDs)
		if err != nil {
			return nil, err
		}
		linksJSON, _ := json.Marshal(links)
		releaseID, err := newID()
		if err != nil {
			return nil, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO decoration_releases(id,tenant_id,page_id,render_mode,design,draft_version,content_hash,created_by,idempotency_key,request_hash,affected_link_ids) VALUES($1,$2,$3,$4,$5,$6,$7,$8,nullif($9,''),$10,$11)`, releaseID, p.TenantID, id, mode, design, version, digest(design), p.MembershipID, key, requestHash, linksJSON)
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, "UPDATE decoration_pages SET current_release_id=$2,updated_at=now() WHERE id=$1", id, releaseID); err != nil {
			return nil, err
		}
		if len(input.LinkIDs) > 0 {
			if _, err := tx.Exec(ctx, "UPDATE access_links SET page_id=$1,binding_mode='FOLLOW_PAGE',pinned_release_id=NULL WHERE id=ANY($2::uuid[])", id, input.LinkIDs); err != nil {
				return nil, err
			}
		}
		if err := saveAction(ctx, tx, p, id, "publish", current, releaseID, key, requestHash, linksJSON); err != nil {
			return nil, err
		}
		return releaseResult(ctx, tx, id, releaseID)
	})
}

// publicationLinks 锁定所有显式目标并计算实际受影响的跟随链接。
func publicationLinks(ctx context.Context, tx pgx.Tx, pageID string, selected []string) ([]string, error) {
	if len(selected) > 0 {
		rows, err := tx.Query(ctx, "SELECT id::text FROM access_links WHERE id=ANY($1::uuid[]) ORDER BY id FOR UPDATE", selected)
		if err != nil {
			return nil, err
		}
		count := 0
		for rows.Next() {
			count++
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if count != len(selected) {
			return nil, platform.Fail(404, "link_not_found", "部分链接不存在或不属于当前租户")
		}
	}
	rows, err := tx.Query(ctx, "SELECT id::text FROM access_links WHERE (page_id=$1 AND binding_mode='FOLLOW_PAGE') OR id=ANY($2::uuid[]) ORDER BY id", pageID, selected)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	links := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		links = append(links, id)
	}
	return links, rows.Err()
}

// replayAction 同一幂等标识只允许重放完全相同的发布或回滚请求。
func replayAction(ctx context.Context, tx pgx.Tx, pageID, action, key, requestHash string) (any, bool, error) {
	if key == "" {
		return nil, false, nil
	}
	var releaseID, stored string
	err := tx.QueryRow(ctx, "SELECT release_id::text,request_hash FROM decoration_actions WHERE page_id=$1 AND action=$2 AND idempotency_key=$3", pageID, action, key).Scan(&releaseID, &stored)
	if err == pgx.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if stored != requestHash {
		return nil, true, platform.Fail(409, "idempotency_conflict", "同一幂等标识对应了不同请求")
	}
	result, err := releaseResult(ctx, tx, pageID, releaseID)
	return result, true, err
}

// saveAction 保存操作人、旧版本、目标版本与实际链接影响的原子审计。
func saveAction(ctx context.Context, tx pgx.Tx, p platform.Principal, pageID, action, previous, releaseID, key, requestHash string, links json.RawMessage) error {
	_, err := tx.Exec(ctx, `INSERT INTO decoration_actions(tenant_id,page_id,action,previous_release_id,release_id,actor_id,affected_link_ids,idempotency_key,request_hash) VALUES($1,$2,$3,nullif($4,'')::uuid,$5,$6,$7,nullif($8,''),$9)`, p.TenantID, pageID, action, previous, releaseID, p.MembershipID, links, key, requestHash)
	return err
}

// releaseResult 返回发布快照和本次操作结果，避免重放指向后续版本。
func releaseResult(ctx context.Context, tx pgx.Tx, pageID, releaseID string) (any, error) {
	item, err := record(ctx, tx, "SELECT to_jsonb(r) FROM decoration_releases r WHERE page_id=$1 AND id=$2", pageID, releaseID)
	return map[string]any{"release_id": releaseID, "current_release_id": releaseID, "page_id": pageID, "release": item}, err
}

// listReleases 列出当前页面可回滚的发布历史，不暴露其他租户内容。
func (s *Service) listReleases(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := idValue(id); err != nil {
		respond(w, r, nil, err)
		return
	}
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		var exists string
		if err := tx.QueryRow(r.Context(), "SELECT id::text FROM decoration_pages WHERE id=$1", id).Scan(&exists); err != nil {
			return nil, err
		}
		rows, err := tx.Query(r.Context(), "SELECT to_jsonb(r) FROM decoration_releases r WHERE page_id=$1 ORDER BY created_at DESC,id LIMIT 100", id)
		if err != nil {
			return nil, err
		}
		items, err := jsonRows(rows)
		return map[string]any{"items": items}, err
	})
}

// rollbackPage 只切换到本页面历史快照，保留当前编辑草稿与访客会话。
func (s *Service) rollbackPage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := idValue(id); err != nil {
		respond(w, r, nil, err)
		return
	}
	var input releaseInput
	if err := platform.Decode(w, r, &input); err != nil {
		respond(w, r, nil, err)
		return
	}
	if err := releaseRequest(input, true); err != nil {
		respond(w, r, nil, err)
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(key) > 128 {
		respond(w, r, nil, platform.Fail(400, "invalid_idempotency_key", "幂等标识过长"))
		return
	}
	raw, _ := json.Marshal(input)
	requestHash := digest(raw)
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		ctx, p := r.Context(), platform.Current(r)
		var current, mode string
		if err := tx.QueryRow(ctx, "SELECT coalesce(current_release_id::text,'') FROM decoration_pages WHERE id=$1 FOR UPDATE", id).Scan(&current); err != nil {
			return nil, err
		}
		if result, ok, err := replayAction(ctx, tx, id, "rollback", key, requestHash); ok || err != nil {
			return result, err
		}
		if current != input.ExpectedReleaseID {
			return nil, conflict()
		}
		if err := tx.QueryRow(ctx, "SELECT render_mode FROM decoration_releases WHERE page_id=$1 AND id=$2", id, input.ReleaseID).Scan(&mode); err != nil {
			return nil, err
		}
		if err := s.checkFeature(ctx, tx, p.TenantID, decorationFeature(mode)); err != nil {
			return nil, err
		}
		links, err := publicationLinks(ctx, tx, id, nil)
		if err != nil {
			return nil, err
		}
		linksJSON, _ := json.Marshal(links)
		if _, err := tx.Exec(ctx, "UPDATE decoration_pages SET current_release_id=$2,updated_at=now() WHERE id=$1", id, input.ReleaseID); err != nil {
			return nil, err
		}
		if err := saveAction(ctx, tx, p, id, "rollback", current, input.ReleaseID, key, requestHash, linksJSON); err != nil {
			return nil, err
		}
		return releaseResult(ctx, tx, id, input.ReleaseID)
	})
}
