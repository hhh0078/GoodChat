// 文件作用说明：在真实生图前用内存占位图校验完整静态候选，仅对明确文件结构错误进行最多两次文本修复并累计实际用量。
package decoration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"path"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// assistantCandidatePackage 合并固定基础和候选文件；预检使用真实可解码占位图，最终打包只读取已完成的真实图片。
func assistantCandidatePackage(ctx context.Context, tx pgx.Tx, work *assistantWork, output assistantOutput, placeholder bool) (*staticPackage, error) {
	files, assets, err := assistantBaseFiles(ctx, tx, work.BaseDesign, work.Foundation.Scripts)
	if err != nil {
		return nil, err
	}
	fileMap := map[string]TemplateFile{}
	for _, file := range files {
		fileMap[file.Path] = file
	}
	for _, file := range output.Files {
		fileMap[file.Path] = file
	}
	assetMap := map[string]packageFile{}
	for _, asset := range assets {
		assetMap[asset.Path] = asset
	}
	for _, task := range output.ImageTasks {
		var content []byte
		if placeholder {
			var buffer bytes.Buffer
			pixel := image.NewNRGBA(image.Rect(0, 0, 1, 1))
			pixel.Pix[3] = 255
			if strings.ToLower(path.Ext(task.Path)) == ".png" {
				err = png.Encode(&buffer, pixel)
			} else {
				err = jpeg.Encode(&buffer, pixel, &jpeg.Options{Quality: 75})
			}
			if err != nil {
				return nil, err
			}
			content = buffer.Bytes()
		} else {
			var state string
			if err := tx.QueryRow(ctx, "SELECT content,status FROM assistant_image_tasks WHERE job_id=$1 AND task_key=$2", work.ID, task.ID).Scan(&content, &state); err != nil {
				return nil, err
			}
			if state != "succeeded" {
				return nil, platform.Fail(409, "assistant_images_incomplete", "仍有图片未完成，请重试失败图片阶段")
			}
		}
		file, err := checkStaticFile(task.Path, content)
		if err != nil {
			return nil, err
		}
		assetMap[task.Path] = file
	}
	files = nil
	assets = nil
	for _, file := range fileMap {
		files = append(files, file)
	}
	for _, asset := range assetMap {
		assets = append(assets, asset)
	}
	return buildTemplatePackageAssets(output.Entrypoint, files, work.Foundation.Scripts, assets)
}

// assistantSourceCallReady 每次文本调用前重新检查任务状态、权限及取消信号，禁止取消后继续自动修复。
func (s *Service) assistantSourceCallReady(ctx context.Context, work *assistantWork) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return platform.TenantTx(ctx, s.pool, work.Tenant, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx, "SELECT status FROM assistant_jobs WHERE id=$1 FOR UPDATE", work.ID).Scan(&status); err != nil {
			return err
		}
		if status != "running" {
			return platform.Fail(409, "assistant_not_running", "任务已结束，不再调用文本模型")
		}
		if err := s.assistantPermission(ctx, tx, work); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "UPDATE assistant_jobs SET billing_status='uncertain' WHERE id=$1", work.ID)
		return err
	})
}

// assistantAddUsage 累加供应商已经报告的非负整数用量，防止整数溢出；未报告的调用仍由billing标记待核对。
func assistantAddUsage(total map[string]int64, raw json.RawMessage) {
	var usage map[string]int64
	if json.Unmarshal(raw, &usage) != nil {
		return
	}
	const maximum = int64(1<<63 - 1)
	for _, key := range []string{"prompt_tokens", "completion_tokens", "total_tokens", "input_tokens", "output_tokens"} {
		value, ok := usage[key]
		if !ok || value < 0 {
			continue
		}
		if total[key] > maximum-value {
			total[key] = maximum
		} else {
			total[key] += value
		}
	}
}

// assistantRecordSourceAttempt 在再次修复前持久保存已报告用量和最后文件候选，进程中断也不会丢失已完成的文本阶段证据。
func (s *Service) assistantRecordSourceAttempt(ctx context.Context, work *assistantWork, candidate assistantOutput, usage json.RawMessage, billing string) error {
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	return platform.TenantTx(saveCtx, s.pool, work.Tenant, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(saveCtx, "SELECT status FROM assistant_jobs WHERE id=$1 FOR UPDATE", work.ID).Scan(&status); err != nil {
			return err
		}
		var raw json.RawMessage
		if candidate.Type == "files" && status == "running" {
			raw, _ = json.Marshal(candidate)
		}
		if _, err := tx.Exec(saveCtx, "UPDATE assistant_jobs SET usage=$2,billing_status=$3,source_result=coalesce($4::jsonb,source_result) WHERE id=$1", work.ID, usage, billing, raw); err != nil {
			return err
		}
		if status != "running" {
			return platform.Fail(409, "assistant_not_running", "任务已结束，不继续文件修复")
		}
		return nil
	})
}

// assistantGenerateCheckedSource 对明确invalid_design文件错误反馈完整候选及原因，最多修复两次；网络、授权等错误不会自动再次计费。
func (s *Service) assistantGenerateCheckedSource(ctx context.Context, work *assistantWork, messages []map[string]any) (assistantOutput, json.RawMessage, string, error) {
	var candidate assistantOutput
	total := map[string]int64{}
	billing := "not_called"
	var cause error
	for attempt := 0; attempt < 3; attempt++ {
		if err := s.assistantSourceCallReady(ctx, work); err != nil {
			cause = err
			break
		}
		output, usage, callBilling, err := s.assistantTextCall(ctx, work.Configuration, messages)
		assistantAddUsage(total, usage)
		if callBilling == "uncertain" {
			billing = "uncertain"
		} else if callBilling == "reported" && billing == "not_called" {
			billing = "reported"
		}
		if output.Type == "files" {
			candidate = output
		}
		if saveErr := s.assistantRecordSourceAttempt(ctx, work, candidate, assistantMarshalUsage(total), billing); saveErr != nil {
			cause = saveErr
			break
		}
		if output.Type != "files" && err == nil && attempt == 0 {
			return output, assistantMarshalUsage(total), billing, nil
		} else if output.Type != "files" && err == nil {
			err = invalidDesign("文件修复必须返回files对象并保留原设计与图片任务")
		}
		if err == nil {
			err = platform.TenantTx(ctx, s.pool, work.Tenant, func(tx pgx.Tx) error {
				_, validationErr := assistantCandidatePackage(ctx, tx, work, output, true)
				return validationErr
			})
		}
		if err == nil {
			return output, assistantMarshalUsage(total), billing, nil
		}
		cause = err
		var api *platform.APIError
		if candidate.Type != "files" || !errors.As(err, &api) || api.Code != "invalid_design" || attempt == 2 || ctx.Err() != nil {
			break
		}
		raw, _ := json.Marshal(candidate)
		messages = append(messages, map[string]any{"role": "assistant", "content": string(raw)}, map[string]any{"role": "user", "content": "程序在真实生图前发现静态文件校验错误：" + api.Message + "。请只修复该结构或安全问题，保持原设计需求与image_tasks及其本地路径。必须重新返回完整files JSON，包含上一候选全部修改文件，不要只返回局部补丁，不得修改平台SDK或降低校验约束。"})
	}
	return candidate, assistantMarshalUsage(total), billing, cause
}

// assistantMarshalUsage 将累计的真实整数用量编码为安全JSON。
func assistantMarshalUsage(total map[string]int64) json.RawMessage {
	raw, _ := json.Marshal(total)
	return raw
}
