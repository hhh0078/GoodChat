// 文件作用说明：提供仅申请管理员可访问的异步导出任务，筛选与同步导出共享，结果24小时后失效。
package chat

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

const asyncExportRows = 100000
const asyncExportBytes = 64 << 20
const asyncExportTenantBytes = 256 << 20
const asyncExportDailyLimit = 20

// 列表显式投影元数据，禁止先把大bytea转JSON再移除造成双倍内存。
const exportJobView = "jsonb_build_object('id',j.id,'tenant_id',j.tenant_id,'requested_by',j.requested_by,'client_request_id',j.client_request_id,'filters',j.filters,'status',j.status,'row_count',j.row_count,'timezone',j.timezone,'size_bytes',j.size_bytes,'error_code',j.error_code,'error_message',j.error_message,'created_at',j.created_at,'started_at',j.started_at,'completed_at',j.completed_at,'expires_at',j.expires_at)"

// registerAsyncExports 为管理员注册任务与带重新授权的短期下载接口。
func (s *Service) registerAsyncExports(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/tenant/export-jobs", s.auth.RequireAdmin(s.exportJobs))
	mux.HandleFunc("POST /api/v1/tenant/export-jobs", s.auth.RequireAdmin(s.exportJobs))
	mux.HandleFunc("POST /api/v1/tenant/export-jobs/{id}/cancel", s.auth.RequireAdmin(s.cancelExportJob))
	mux.HandleFunc("GET /api/v1/tenant/export-jobs/{id}/download", s.auth.RequireAdmin(s.downloadExportJob))
}

// exportFilters 规范化允许的筛选字段，拒绝重复值、未知参数与无意义内部备注组合。
func exportFilters(raw string) (string, error) {
	if len(raw) > 4000 {
		return "", platform.Fail(400, "invalid_export_filter", "导出筛选条件过长")
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return "", platform.Fail(400, "invalid_export_filter", "导出筛选编码不正确")
	}
	allowed := fieldSet("query status agent_id contact_id channel_code from to include_messages include_internal")
	for key, items := range values {
		if !allowed[key] || len(items) != 1 {
			return "", platform.Fail(400, "invalid_export_filter", "导出包含未知或重复筛选条件")
		}
	}
	r := &http.Request{URL: &url.URL{RawQuery: values.Encode()}}
	messages, err := exportFlag(r, "include_messages")
	if err != nil {
		return "", err
	}
	internal, err := exportFlag(r, "include_internal")
	if err != nil {
		return "", err
	}
	if internal && !messages {
		return "", platform.Fail(400, "invalid_export_option", "内部备注选项仅适用于正文导出")
	}
	return values.Encode(), nil
}

// exportPermission 在任何读取结果前重新检查当前管理关系和有效套餐。
func (s *Service) exportPermission(ctx context.Context, tx pgx.Tx, a actor) error {
	if !isAdmin(a) {
		return platform.Fail(403, "admin_required", "导出需要当前租户管理员权限")
	}
	if err := liveMember(ctx, tx, a); err != nil {
		return err
	}
	return s.checkFeature(ctx, tx, a.TenantID, "export")
}

// exportJobs 列出本人任务或幂等创建，租户最多两个同时等待或运行的任务。
func (s *Service) exportJobs(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	var result any
	status := http.StatusOK
	var input struct {
		ClientID string `json:"client_request_id"`
		Filters  string `json:"filters"`
	}
	if r.Method == http.MethodPost {
		if err := platform.Decode(w, r, &input); err != nil {
			replyError(w, r, err)
			return
		}
		if !uuidPattern.MatchString(input.ClientID) {
			returnError(w, r, "invalid_request_id", "导出需要有效的幂等编号")
			return
		}
		var err error
		input.Filters, err = exportFilters(input.Filters)
		if err != nil {
			replyError(w, r, err)
			return
		}
	}
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		ctx := r.Context()
		if err := s.exportPermission(ctx, tx, a); err != nil {
			return err
		}
		if r.Method == http.MethodGet {
			var err error
			result, err = txJSON(ctx, tx, "SELECT jsonb_build_object('items',coalesce(jsonb_agg(item),'[]'::jsonb),'row_limit',$2::int,'byte_limit',$3::int) FROM (SELECT "+exportJobView+" AS item FROM chat_export_jobs j WHERE requested_by=$1 ORDER BY created_at DESC,id LIMIT 50) listed", a.MemberID, asyncExportRows, asyncExportBytes)
			return err
		}
		var stored string
		var existing string
		err := tx.QueryRow(ctx, "SELECT id::text,filters FROM chat_export_jobs WHERE requested_by=$1 AND client_request_id=$2", a.MemberID, input.ClientID).Scan(&existing, &stored)
		if err == nil {
			if stored != input.Filters {
				return platform.Fail(409, "idempotency_conflict", "同一导出编号不能更换筛选条件")
			}
			result, err = txJSON(ctx, tx, "SELECT "+exportJobView+" FROM chat_export_jobs j WHERE id=$1", existing)
			return err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		// 即使未安装套餐校验钩子，测试和独立装配也必须串行保护租户任务上限。
		if _, err = tx.Exec(ctx, "SELECT id FROM tenants WHERE id=$1 FOR UPDATE", a.TenantID); err != nil {
			return err
		}
		// 等待租户锁后再读幂等键，避免同一请求并发创建两次或误报队列已满。
		err = tx.QueryRow(ctx, "SELECT id::text,filters FROM chat_export_jobs WHERE requested_by=$1 AND client_request_id=$2", a.MemberID, input.ClientID).Scan(&existing, &stored)
		if err == nil {
			if stored != input.Filters {
				return platform.Fail(409, "idempotency_conflict", "同一导出编号不能更换筛选条件")
			}
			result, err = txJSON(ctx, tx, "SELECT "+exportJobView+" FROM chat_export_jobs j WHERE id=$1", existing)
			return err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		request := r.Clone(ctx)
		request.URL = &url.URL{RawQuery: input.Filters}
		var zone string
		if _, zone, err = conversationFilterArgs(ctx, tx, request, a); err != nil {
			return err
		}
		var pending, daily int
		var retained int64
		if err = tx.QueryRow(ctx, "SELECT count(*) FILTER(WHERE status IN('queued','running') AND expires_at>now()),coalesce(sum(CASE WHEN expires_at>now() AND status IN('queued','running') THEN $1::bigint WHEN expires_at>now() AND status='completed' THEN size_bytes ELSE 0 END),0),count(*) FILTER(WHERE created_at>=date_trunc('day',now(),$2::text)) FROM chat_export_jobs", asyncExportBytes, zone).Scan(&pending, &retained, &daily); err != nil {
			return err
		}
		if pending >= 2 {
			return platform.Fail(409, "export_queue_full", "本租户已有两个导出等待处理，请完成或取消后再提交")
		}
		if retained+asyncExportBytes > asyncExportTenantBytes {
			return platform.Fail(409, "export_storage_limit", "本租户临时导出结果及待执行预留已达256MiB，请等待结果到期后再提交")
		}
		if daily >= asyncExportDailyLimit {
			return platform.Fail(429, "export_daily_limit", "本租户当日最多提交20次批量导出，失败或取消不退提交次数")
		}

		id := newID()
		if _, err = tx.Exec(ctx, "INSERT INTO chat_export_jobs(id,tenant_id,requested_by,client_request_id,filters) VALUES($1,$2,$3,$4,$5)", id, a.TenantID, a.MemberID, input.ClientID, input.Filters); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "INSERT INTO chat_export_dispatch(job_id,tenant_id) VALUES($1,$2)", id, a.TenantID); err != nil {
			return err
		}
		if err = audit(ctx, tx, a, "chat_export.requested", id, map[string]any{"filter_hash": hash(input.Filters), "row_limit": asyncExportRows}); err != nil {
			return err
		}
		result, err = txJSON(ctx, tx, "SELECT "+exportJobView+" FROM chat_export_jobs j WHERE id=$1", id)
		status = http.StatusCreated
		return err
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, status, result)
}

// cancelExportJob 可在等待和运行时取消，旧worker完成后不能写回文件。
func (s *Service) cancelExportJob(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	id := r.PathValue("id")
	if !uuidPattern.MatchString(id) {
		returnError(w, r, "invalid_job", "任务编号不正确")
		return
	}
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		ctx := r.Context()
		if _, err := tx.Exec(ctx, "SELECT id FROM tenants WHERE id=$1 FOR UPDATE", a.TenantID); err != nil {
			return err
		}
		if err := liveMember(ctx, tx, a); err != nil {
			return err
		}
		var state string
		err := tx.QueryRow(ctx, "SELECT status FROM chat_export_jobs WHERE id=$1 AND requested_by=$2 FOR UPDATE", id, a.MemberID).Scan(&state)
		if errors.Is(err, pgx.ErrNoRows) {
			return platform.Fail(404, "export_not_found", "导出任务不存在")
		}
		if err != nil {
			return err
		}
		if state == "completed" || state == "failed" {
			return platform.Fail(409, "export_finished", "导出已经结束，请查看任务结果")
		}
		if _, err = tx.Exec(ctx, "UPDATE chat_export_jobs SET status='cancelled',csv_body=NULL,completed_at=now() WHERE id=$1", id); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "DELETE FROM chat_export_dispatch WHERE job_id=$1", id); err != nil {
			return err
		}
		return audit(ctx, tx, a, "chat_export.cancelled", id, nil)
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, map[string]any{"cancelled": true})
}

// downloadExportJob 每次下载重新校验申请者、管理权限、权益与有效期，不发可绕过权限的公开直链。
func (s *Service) downloadExportJob(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	id := r.PathValue("id")
	if !uuidPattern.MatchString(id) {
		returnError(w, r, "invalid_job", "任务编号不正确")
		return
	}
	var data []byte
	var rows int
	var zone, filters string
	redacted := false
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		ctx := r.Context()
		if err := s.exportPermission(ctx, tx, a); err != nil {
			return err
		}
		var state string
		var valid bool
		err := tx.QueryRow(ctx, "SELECT status,expires_at>now(),csv_body,row_count,timezone,filters FROM chat_export_jobs WHERE id=$1 AND requested_by=$2", id, a.MemberID).Scan(&state, &valid, &data, &rows, &zone, &filters)
		if errors.Is(err, pgx.ErrNoRows) {
			return platform.Fail(404, "export_not_found", "导出任务不存在")
		}
		if err != nil {
			return err
		}
		if !valid {
			return platform.Fail(410, "export_expired", "导出已过期，请重新创建任务")
		}
		if state != "completed" || len(data) == 0 {
			return platform.Fail(409, "export_not_ready", "导出文件尚不可下载，请查看任务状态")
		}
		query, _ := url.ParseQuery(filters)
		if query.Get("include_messages") == "true" {
			var changed bool
			if err := tx.QueryRow(ctx, "SELECT (SELECT count(*) FROM messages WHERE withdrawn_at IS NOT NULL)<>withdrawn_count FROM chat_export_jobs WHERE id=$1", id).Scan(&changed); err != nil {
				return err
			}
			if changed {
				if _, err := tx.Exec(ctx, "UPDATE chat_export_jobs SET status='failed',csv_body=NULL,size_bytes=0,error_code='export_redacted',error_message='消息发生撤回，旧正文文件已失效，请重新创建导出' WHERE id=$1", id); err != nil {
					return err
				}
				redacted = true
				data = nil
				return audit(ctx, tx, a, "chat_export.redacted", id, nil)
			}
		}
		return audit(ctx, tx, a, "chat_export.downloaded", id, map[string]any{"rows": rows})
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	if redacted {
		replyError(w, r, platform.Fail(409, "export_redacted", "消息发生撤回，旧正文文件已失效，请重新创建导出"))
		return
	}
	kind := "conversations"
	q, _ := url.ParseQuery(filters)
	if strings.EqualFold(q.Get("include_messages"), "true") {
		kind = "messages"
	}
	writeCSVDownload(w, data, rows, zone, kind, asyncExportRows, asyncExportBytes)
}
