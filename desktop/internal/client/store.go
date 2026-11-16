// 文件作用说明：使用本机SQLite原子保存发送队列、消息事件和服务端签名游标，不保存令牌。
package client

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Store 保存桌面持久数据，分区键始终包含服务器、账号和租户。
type Store struct{ db *sql.DB }

// Pending 表示已经落盘但尚未得到服务器确认的消息。
type Pending struct {
	ID           string          `json:"id"`
	Conversation string          `json:"conversation"`
	Body         json.RawMessage `json:"body"`
	State        string          `json:"state"`
	Error        string          `json:"error"`
}

// openStore 初始化本机数据库及中文结构说明，不涉及服务端PostgreSQL迁移。
func openStore(dir string) (*Store, error) {
	path := filepath.Join(dir, "messages.sqlite")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	if err = os.Chmod(path, 0600); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA busy_timeout=5000;
-- 发送队列：分区、客户端编号、会话、原始消息体、状态、错误和创建时间。
CREATE TABLE IF NOT EXISTS pending(scope TEXT NOT NULL,id TEXT NOT NULL,conversation TEXT NOT NULL,body BLOB NOT NULL,state TEXT NOT NULL DEFAULT 'pending',error TEXT NOT NULL DEFAULT '',created_at INTEGER NOT NULL DEFAULT(unixepoch()),PRIMARY KEY(scope,id));
-- 恢复水位：分区、会话及服务端签名游标，与事件在同一事务内推进。
CREATE TABLE IF NOT EXISTS cursors(scope TEXT NOT NULL,conversation TEXT NOT NULL,cursor TEXT NOT NULL,PRIMARY KEY(scope,conversation));
-- 最近消息事件缓存：分区、会话、稳定事件编号和载荷；不作为权限凭证。
CREATE TABLE IF NOT EXISTS events(scope TEXT NOT NULL,conversation TEXT NOT NULL,id TEXT NOT NULL,payload BLOB NOT NULL,PRIMARY KEY(scope,conversation,id));`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// enqueue 保留原消息编号和内容，重复编号携带不同内容时拒绝覆盖。
func (s *Store) enqueue(scope string, item Pending) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var body []byte
	var conversation string
	err = tx.QueryRow("SELECT body,conversation FROM pending WHERE scope=? AND id=?", scope, item.ID).Scan(&body, &conversation)
	if err == nil {
		var old, next any
		if json.Unmarshal(body, &old) != nil || json.Unmarshal(item.Body, &next) != nil {
			return errors.New("消息暂存损坏，请重新核对")
		}
		a, _ := json.Marshal(old)
		b, _ := json.Marshal(next)
		if string(a) != string(b) || conversation != item.Conversation {
			return errors.New("相同消息编号不能用于不同内容")
		}
		_, err = tx.Exec("UPDATE pending SET state='pending',error='' WHERE scope=? AND id=?", scope, item.ID)
	} else if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.Exec("INSERT INTO pending(scope,id,conversation,body) VALUES(?,?,?,?)", scope, item.ID, item.Conversation, []byte(item.Body))
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

// listPending 返回当前分区的待处理和失败消息，不能读取其他身份记录。
func (s *Store) listPending(scope string) ([]Pending, error) {
	rows, err := s.db.Query("SELECT id,conversation,body,state,error FROM pending WHERE scope=? ORDER BY created_at,id", scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Pending{}
	for rows.Next() {
		var item Pending
		if err := rows.Scan(&item.ID, &item.Conversation, &item.Body, &item.State, &item.Error); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// cursor 读取只有本地事务处理完成后才保存的恢复位置。
func (s *Store) cursor(scope, conversation string) (string, error) {
	var value string
	err := s.db.QueryRow("SELECT cursor FROM cursors WHERE scope=? AND conversation=?", scope, conversation).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

// checkpoint 将一批事件和游标原子落盘，重建快照时清除过期缓存。
func (s *Store) checkpoint(scope, conversation, cursor string, items []json.RawMessage, reset bool) error {
	if cursor == "" {
		return errors.New("服务端未返回恢复游标")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if reset {
		if _, err = tx.Exec("DELETE FROM events WHERE scope=? AND conversation=?", scope, conversation); err != nil {
			return err
		}
	}
	for _, raw := range items {
		var item struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &item) != nil || item.ID == "" {
			return errors.New("服务端消息事件编号无效")
		}
		if _, err = tx.Exec("INSERT OR REPLACE INTO events(scope,conversation,id,payload) VALUES(?,?,?,?)", scope, conversation, item.ID, []byte(raw)); err != nil {
			return err
		}
	}
	_, err = tx.Exec("INSERT INTO cursors(scope,conversation,cursor) VALUES(?,?,?) ON CONFLICT(scope,conversation) DO UPDATE SET cursor=excluded.cursor", scope, conversation, cursor)
	if err != nil {
		return err
	}
	// 每个会话仅保留最近1000条缓存，完整历史仍从服务端鉴权读取。
	_, err = tx.Exec("DELETE FROM events WHERE scope=? AND conversation=? AND rowid NOT IN (SELECT rowid FROM events WHERE scope=? AND conversation=? ORDER BY rowid DESC LIMIT 1000)", scope, conversation, scope, conversation)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// forgetConversation 删除已失权会话的本机事件与恢复水位。
func (s *Store) forgetConversation(scope, conversation string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("DELETE FROM events WHERE scope=? AND conversation=?", scope, conversation); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM cursors WHERE scope=? AND conversation=?", scope, conversation); err != nil {
		return err
	}
	return tx.Commit()
}
