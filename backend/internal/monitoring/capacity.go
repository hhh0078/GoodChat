// 文件作用说明：管理总管理员录入的压测基线，并按安全系数与资源指纹计算保守在线余量。
package monitoring

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// resourceSpec 只保存相对稳定的部署规格，排除会随负载变化的用量和Go堆指标。
type resourceSpec struct {
	Scope         string  `json:"scope"`
	CgroupPath    string  `json:"cgroup_path,omitempty"`
	CPUCores      float64 `json:"cpu_cores"`
	MemoryLimit   *uint64 `json:"memory_limit_bytes"`
	VisibleMemory *uint64 `json:"visible_memory_bytes"`
}

// baseline 保存当前部署已完成压测的人工记录，不将录入人编号导出到前端。
type baseline struct {
	TestedPeople int64        `json:"tested_people"`
	SafetyFactor float64      `json:"safety_factor"`
	TestDate     string       `json:"test_date"`
	Notes        string       `json:"notes"`
	Fingerprint  string       `json:"-"`
	Spec         resourceSpec `json:"resource_spec"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

// capacityReport 区分未校准、有效、规格已变和读取失败；未知余量始终为null。
type capacityReport struct {
	Status        string    `json:"status"`
	Reason        string    `json:"reason"`
	CurrentOnline int64     `json:"current_online_people"`
	SafeOnline    *int64    `json:"safe_online_people"`
	Additional    *int64    `json:"estimated_additional_people"`
	Baseline      *baseline `json:"baseline"`
}

// specification 取实际可见资源边界，无独立内存配额时记录环境可见内存，防止扩缩容沿用旧基线。
func specification(resources Resources) resourceSpec {
	spec := resourceSpec{Scope: resources.Scope, CgroupPath: resources.CgroupPath, CPUCores: resources.CPUCores, MemoryLimit: resources.MemoryLimit}
	if spec.MemoryLimit == nil {
		spec.VisibleMemory = resources.VisibleMemory
	}
	return spec
}

// specFingerprint 对固定字段序列生成稳定指纹，不用当前资源利用率判断规格变化。
func specFingerprint(spec resourceSpec) string {
	raw, _ := json.Marshal(spec)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

// evaluateCapacity 只在规格相同的人工压测记录内估算，安全容量向下取整且不超过实测人数。
func evaluateCapacity(resources Resources, stored *baseline, current int64) capacityReport {
	result := capacityReport{Status: "uncalibrated", Reason: "尚无当前部署的业务压测基线，无法可靠估算还可同时在线人数", CurrentOnline: current, Baseline: stored}
	if stored == nil {
		return result
	}
	if specFingerprint(specification(resources)) != stored.Fingerprint {
		result.Status = "stale"
		result.Reason = "运行范围或CPU、内存规格已变化，请在当前部署重新压测后记录基线"
		return result
	}
	if stored.TestedPeople < 1 || stored.SafetyFactor < 0.3 || stored.SafetyFactor > 0.9 || math.IsNaN(stored.SafetyFactor) || math.IsInf(stored.SafetyFactor, 0) {
		result.Status = "unavailable"
		result.Reason = "压测记录无效，请重新校准"
		return result
	}
	safe := min(stored.TestedPeople, int64(math.Floor(float64(stored.TestedPeople)*stored.SafetyFactor)))
	additional := max(int64(0), safe-max(int64(0), current))
	result.Status = "calibrated"
	result.Reason = "按管理员录入的已完成业务压测记录乘以安全系数估算，系统未自动验证该测试"
	result.SafeOnline = &safe
	result.Additional = &additional
	return result
}

// readBaseline 读取平台单例记录，未录入与数据库不可用分开处理。
func (s *Service) readBaseline(ctx context.Context) (*baseline, error) {
	var stored baseline
	var raw []byte
	err := s.pool.QueryRow(ctx, "SELECT tested_people,safety_factor,test_date::text,notes,resource_fingerprint,resource_spec,updated_at FROM platform_capacity_baseline WHERE singleton").Scan(&stored.TestedPeople, &stored.SafetyFactor, &stored.TestDate, &stored.Notes, &stored.Fingerprint, &raw, &stored.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &stored.Spec); err != nil {
		return nil, err
	}
	return &stored, nil
}

// capacityState 返回当前规格对应的基线估计，查询失败时不降格为“未配置”。
func (s *Service) capacityState(ctx context.Context, resources Resources, current int64) capacityReport {
	stored, err := s.readBaseline(ctx)
	if err != nil {
		return capacityReport{Status: "unavailable", Reason: "压测基线暂时无法读取，请稍后重试", CurrentOnline: current}
	}
	return evaluateCapacity(resources, stored, current)
}

// capacity 仅接受总管理员记录或清除人工基线，当前资源指纹始终由服务端获取。
func (s *Service) capacity(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		if _, err := s.pool.Exec(r.Context(), "DELETE FROM platform_capacity_baseline WHERE singleton"); err != nil {
			platform.ErrorJSON(w, r, err)
			return
		}
	}
	if r.Method == http.MethodPatch {
		var input struct {
			TestedPeople int64    `json:"tested_people"`
			SafetyFactor *float64 `json:"safety_factor"`
			TestDate     string   `json:"test_date"`
			Notes        string   `json:"notes"`
		}
		if err := platform.Decode(w, r, &input); err != nil {
			platform.ErrorJSON(w, r, err)
			return
		}
		factor := 0.7
		if input.SafetyFactor != nil {
			factor = *input.SafetyFactor
		}
		input.Notes = strings.TrimSpace(input.Notes)
		date, err := time.Parse("2006-01-02", input.TestDate)
		if err != nil || date.Year() < 2000 || date.After(time.Now().UTC().Add(24*time.Hour)) || input.TestedPeople < 1 || input.TestedPeople > 10_000_000 || factor < 0.3 || factor > 0.9 || math.IsNaN(factor) || math.IsInf(factor, 0) || len([]rune(input.Notes)) < 1 || len([]rune(input.Notes)) > 2000 {
			platform.ErrorJSON(w, r, platform.Fail(400, "invalid_capacity_baseline", "请填写已完成的压测日期、1至1000万实测人数、0.3至0.9安全系数和1至2000字说明"))
			return
		}
		resources, _, _ := s.sampler.Snapshot()
		spec := specification(resources)
		raw, _ := json.Marshal(spec)
		if _, err = s.pool.Exec(r.Context(), `INSERT INTO platform_capacity_baseline(singleton,tested_people,safety_factor,test_date,notes,resource_fingerprint,resource_spec,recorded_by) VALUES(true,$1,$2,$3::date,$4,$5,$6,$7) ON CONFLICT(singleton) DO UPDATE SET tested_people=EXCLUDED.tested_people,safety_factor=EXCLUDED.safety_factor,test_date=EXCLUDED.test_date,notes=EXCLUDED.notes,resource_fingerprint=EXCLUDED.resource_fingerprint,resource_spec=EXCLUDED.resource_spec,recorded_by=EXCLUDED.recorded_by,updated_at=now()`, input.TestedPeople, factor, input.TestDate, input.Notes, specFingerprint(spec), raw, platform.Current(r).UserID); err != nil {
			platform.ErrorJSON(w, r, err)
			return
		}
	}
	resources, _, _ := s.sampler.Snapshot()
	var current int64
	if s.connections != nil {
		current = s.connections()["online_people"]
	}
	platform.JSON(w, 200, s.capacityState(r.Context(), resources, current))
}
