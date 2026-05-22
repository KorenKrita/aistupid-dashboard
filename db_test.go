package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestInitDB 验证 InitDB 的以下行为：
// 1. 创建所有必需的数据库表（models、scores_history 等 9 张表）
// 2. 创建必要的索引（idx_scores_history_model、idx_scores_history_timestamp）
// 3. 重复调用 InitDB 是幂等的（不会报错）
func TestInitDB(t *testing.T) {
	dbPath := "./test_aistupid.db"
	defer os.Remove(dbPath)
	defer CloseDB()

	err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}

	// 验证 9 张核心表都已创建
	expectedTables := []string{
		"models", "scores_history", "degradations", "alerts",
		"global_index", "provider_reliability", "recommendations",
		"transparency", "model_freshness",
	}

	for _, table := range expectedTables {
		var name string
		err = DB.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Errorf("Table %q not found: %v", table, err)
		}
	}

	// 验证 2 个关键索引已创建
	expectedIndexes := []string{
		"idx_scores_history_model",
		"idx_scores_history_timestamp",
	}

	for _, idx := range expectedIndexes {
		var name string
		err = DB.QueryRow("SELECT name FROM sqlite_master WHERE type='index' AND name=?", idx).Scan(&name)
		if err != nil {
			t.Errorf("Index %q not found: %v", idx, err)
		}
	}

	// 验证 InitDB 可重复调用（幂等性）
	err = InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed when called a second time: %v", err)
	}
}

// TestInitDB_InvalidPath 验证在无效路径上调用 InitDB 应返回错误。
// 使用一个不存在的目录路径来触发文件系统错误。
func TestInitDB_InvalidPath(t *testing.T) {
	invalidPath := filepath.Join("/nonexistent-directory-12345", "test.db")
	err := InitDB(invalidPath)
	if err == nil {
		CloseDB()
		t.Error("Expected error when initializing DB at invalid path, but got nil")
	}
}

// TestCloseDB 验证 CloseDB 的正确行为：
// 1. 正常关闭已打开的数据库
// 2. 关闭后再次查询应返回错误（而非静默成功）
func TestCloseDB(t *testing.T) {
	dbPath := "./test_close.db"
	defer os.Remove(dbPath)

	err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}

	// 执行关闭操作
	err = CloseDB()
	if err != nil {
		t.Fatalf("CloseDB failed: %v", err)
	}

	// 验证关闭后查询失败
	var val int
	err = DB.QueryRow("SELECT 1").Scan(&val)
	if err == nil {
		t.Error("Expected query to fail after CloseDB, but it succeeded")
	}
}

// TestForeignKeys 验证外键约束和级联删除行为：
// 1. 引用不存在的 model_id 插入 scores_history 应失败
// 2. 插入父模型后，子表（scores_history、degradations）可以正常插入
// 3. 删除父模型后，子表记录应被级联删除
func TestForeignKeys(t *testing.T) {
	dbPath := "./test_fk.db"
	defer os.Remove(dbPath)
	defer CloseDB()

	err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}

	// 验证外键约束：插入引用不存在的模型的 scores_history 应收到外键冲突错误
	_, err = DB.Exec("INSERT INTO scores_history (model_id, timestamp, score) VALUES ('nonexistent-model', '2026-05-22 00:00:00', 95)")
	if err == nil {
		t.Error("Expected foreign key violation error, but insert succeeded")
	}

	// 插入父模型
	_, err = DB.Exec("INSERT INTO models (id, name, provider, vendor) VALUES ('gpt-4o', 'GPT-4o', 'openai', 'openai')")
	if err != nil {
		t.Fatalf("Failed to insert model: %v", err)
	}

	// 插入子表 scores_history（此时外键约束应满足）
	_, err = DB.Exec("INSERT INTO scores_history (model_id, timestamp, score) VALUES ('gpt-4o', '2026-05-22 00:00:00', 95)")
	if err != nil {
		t.Fatalf("Failed to insert scores_history: %v", err)
	}

	// 插入子表 degradations
	_, err = DB.Exec("INSERT INTO degradations (model_id, drop_percentage, severity, detected_at, type, message) VALUES ('gpt-4o', 10, 'high', '2026-05-22 00:00:00', 'score_drop', 'drop')")
	if err != nil {
		t.Fatalf("Failed to insert degradation: %v", err)
	}

	// 验证子记录存在
	var count int
	err = DB.QueryRow("SELECT COUNT(*) FROM scores_history WHERE model_id = 'gpt-4o'").Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("Expected 1 score_history record, got %d (err: %v)", count, err)
	}

	err = DB.QueryRow("SELECT COUNT(*) FROM degradations WHERE model_id = 'gpt-4o'").Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("Expected 1 degradation record, got %d (err: %v)", count, err)
	}

	// 删除父模型
	_, err = DB.Exec("DELETE FROM models WHERE id = 'gpt-4o'")
	if err != nil {
		t.Fatalf("Failed to delete model: %v", err)
	}

	// 验证子表记录被级联删除（ON DELETE CASCADE）
	err = DB.QueryRow("SELECT COUNT(*) FROM scores_history WHERE model_id = 'gpt-4o'").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query scores_history: %v", err)
	}
	if count != 0 {
		t.Error("Expected scores_history to be deleted due to cascade")
	}

	err = DB.QueryRow("SELECT COUNT(*) FROM degradations WHERE model_id = 'gpt-4o'").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query degradations: %v", err)
	}
	if count != 0 {
		t.Error("Expected degradations to be deleted due to cascade")
	}
}
