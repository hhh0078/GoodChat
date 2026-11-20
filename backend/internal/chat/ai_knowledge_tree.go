// 文件作用说明：管理租户知识目录树，并为每次模型调用构建授权文档及祖先目录的完整快照。
package chat

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// aiDirectoryNode 只承载目录元数据，不把文档正文或未授权兄弟文档发给模型。
type aiDirectoryNode struct {
	ID          string             `json:"id"`
	Title       string             `json:"title"`
	Type        string             `json:"type"`
	Description string             `json:"description,omitempty"`
	Version     int64              `json:"version,omitempty"`
	Children    []*aiDirectoryNode `json:"children,omitempty"`
}

// aiKnowledgeFolders 允许所有成员查看目录，仅管理员可创建、移动和删除空目录。
func (s *Service) aiKnowledgeFolders(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	var in struct {
		Name    string `json:"name"`
		Parent  string `json:"parent_id"`
		Version int64  `json:"version"`
	}
	if r.Method == "POST" || r.Method == "PATCH" {
		if e := platform.Decode(w, r, &in); e != nil {
			replyError(w, r, e)
			return
		}
		in.Name = strings.TrimSpace(in.Name)
		if in.Name == "" || len([]rune(in.Name)) > 100 {
			returnError(w, r, "ai_title_required", "请输入标题")
			return
		}
	}
	var result any
	e := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		ctx := r.Context()
		if e := liveMember(ctx, tx, a); e != nil {
			return e
		}
		if r.Method == "GET" {
			rows, e := tx.Query(ctx, "SELECT to_jsonb(f) FROM ai_knowledge_folders f WHERE NOT archived ORDER BY name,id")
			if e != nil {
				return e
			}
			items, e := jsonRows(rows)
			result = map[string]any{"items": items}
			return e
		}
		if !isAdmin(a) {
			return platform.Fail(403, "admin_required", "该动作需要管理员权限")
		}
		if _, e := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,48))", a.TenantID); e != nil {
			return e
		}
		id := r.PathValue("id")
		if r.Method == "POST" {
			var count int
			if e := tx.QueryRow(ctx, "SELECT count(*) FROM ai_knowledge_folders WHERE NOT archived").Scan(&count); e != nil {
				return e
			}
			if count >= 1000 {
				return platform.Fail(400, "ai_resource_limit", "资源数量已达到上限")
			}
			id = newID()
		} else {
			if !uuidPattern.MatchString(id) {
				return platform.Fail(404, "not_found", "记录不存在或无权访问")
			}
			var version int64
			if e := tx.QueryRow(ctx, "SELECT version FROM ai_knowledge_folders WHERE id=$1 AND NOT archived FOR UPDATE", id).Scan(&version); e != nil {
				return e
			}
			if r.Method != "DELETE" && version != in.Version {
				return platform.Fail(409, "ai_version_conflict", "内容已更新，请刷新后重试")
			}
		}
		if r.Method == "DELETE" {
			var used bool
			if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM ai_knowledge_folders WHERE parent_id=$1 AND NOT archived) OR EXISTS(SELECT 1 FROM ai_knowledge_bases WHERE folder_id=$1 AND NOT archived)", id).Scan(&used); e != nil {
				return e
			}
			if used {
				return platform.Fail(409, "ai_folder_not_empty", "请先移出目录中的知识和子目录")
			}
			_, e := tx.Exec(ctx, "UPDATE ai_knowledge_folders SET archived=true,version=version+1,updated_at=now() WHERE id=$1", id)
			result = map[string]any{"id": id}
			return e
		}
		if in.Parent != "" {
			if !uuidPattern.MatchString(in.Parent) {
				return platform.Fail(404, "not_found", "记录不存在或无权访问")
			}
			var exists bool
			if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM ai_knowledge_folders WHERE id=$1 AND NOT archived)", in.Parent).Scan(&exists); e != nil {
				return e
			}
			if !exists {
				return platform.Fail(404, "not_found", "记录不存在或无权访问")
			}
			var cycle bool
			if e := tx.QueryRow(ctx, `WITH RECURSIVE ancestors AS(SELECT id,parent_id FROM ai_knowledge_folders WHERE id=$1 AND NOT archived UNION SELECT f.id,f.parent_id FROM ai_knowledge_folders f JOIN ancestors a ON a.parent_id=f.id) SELECT EXISTS(SELECT 1 FROM ancestors WHERE id=$2)`, in.Parent, id).Scan(&cycle); e != nil {
				return e
			}
			if cycle {
				return platform.Fail(400, "ai_folder_cycle", "不能将目录移动到自己或自己的子目录中")
			}
		}
		_, e := tx.Exec(ctx, `INSERT INTO ai_knowledge_folders(id,tenant_id,name,parent_id) VALUES($1,$2,$3,NULLIF($4,'')::uuid) ON CONFLICT(id) DO UPDATE SET name=excluded.name,parent_id=excluded.parent_id,version=ai_knowledge_folders.version+1,updated_at=now()`, id, a.TenantID, in.Name, in.Parent)
		result = map[string]any{"id": id}
		return e
	})
	if e != nil {
		replyError(w, r, e)
		return
	}
	platform.JSON(w, 200, result)
}

// aiValidateFolder 在目录事务锁内验证目标，空编号表示根目录，阻止跨租户或已移除目录归档文档。
func aiValidateFolder(ctx context.Context, tx pgx.Tx, tenant, id string) error {
	if _, e := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,48))", tenant); e != nil {
		return e
	}
	if id == "" {
		return nil
	}
	if !uuidPattern.MatchString(id) {
		return platform.Fail(404, "not_found", "记录不存在或无权访问")
	}
	var exists bool
	if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM ai_knowledge_folders WHERE id=$1 AND NOT archived)", id).Scan(&exists); e != nil {
		return e
	}
	if !exists {
		return platform.Fail(404, "not_found", "记录不存在或无权访问")
	}
	return nil
}

// aiBuildDirectory 构建所有已授权文档的完整祖先树，按稳定标题排序，损坏或循环引用明确失败。
func aiBuildDirectory(ctx context.Context, tx pgx.Tx, knowledge []aiKnowledge) ([]*aiDirectoryNode, error) {
	type folder struct{ id, parent, name string }
	folders := map[string]folder{}
	rows, e := tx.Query(ctx, "SELECT id::text,coalesce(parent_id::text,''),name FROM ai_knowledge_folders WHERE NOT archived")
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var f folder
		if e = rows.Scan(&f.id, &f.parent, &f.name); e != nil {
			rows.Close()
			return nil, e
		}
		folders[f.id] = f
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return nil, e
	}
	nodes := map[string]*aiDirectoryNode{}
	roots := []*aiDirectoryNode{}
	var ensure func(string, map[string]bool) (*aiDirectoryNode, error)
	// ensure 递归加入授权文档的祖先，未包含正文的兄弟文档始终不可见。
	ensure = func(id string, seen map[string]bool) (*aiDirectoryNode, error) {
		if id == "" {
			return nil, nil
		}
		if seen[id] {
			return nil, errors.New("ai_directory_invalid")
		}
		seen[id] = true
		if n := nodes[id]; n != nil {
			return n, nil
		}
		f, ok := folders[id]
		if !ok {
			return nil, errors.New("ai_directory_invalid")
		}
		parent, e := ensure(f.parent, seen)
		if e != nil {
			return nil, e
		}
		n := &aiDirectoryNode{ID: id, Title: f.name, Type: "folder"}
		nodes[id] = n
		if parent == nil {
			roots = append(roots, n)
		} else {
			parent.Children = append(parent.Children, n)
		}
		return n, nil
	}
	for _, k := range knowledge {
		parent, e := ensure(k.FolderID, map[string]bool{})
		if e != nil {
			return nil, e
		}
		n := &aiDirectoryNode{ID: k.ID, Title: k.Title, Description: k.Description, Version: k.Version, Type: "document"}
		if parent == nil {
			roots = append(roots, n)
		} else {
			parent.Children = append(parent.Children, n)
		}
	}
	var order func([]*aiDirectoryNode)
	// order 将文件夹排在文档前，保持每次请求目录顺序一致。
	order = func(items []*aiDirectoryNode) {
		sort.Slice(items, func(i, j int) bool {
			if items[i].Type != items[j].Type {
				return items[i].Type == "folder"
			}
			if items[i].Title != items[j].Title {
				return items[i].Title < items[j].Title
			}
			return items[i].ID < items[j].ID
		})
		for _, n := range items {
			order(n.Children)
		}
	}
	order(roots)
	return roots, nil
}
