package health

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "github.com/lib/pq"
)

// schemaSQL 包含健康监控插件自有表的建表语句。
//
// 选型说明：
//   - 与 epay 一样手写 DDL 而不是引入 ent，避免代码生成步骤；表结构稳定。
//   - 表名前缀 health_ 防止与 core 表冲突。
//   - 索引覆盖最常见查询：按 (account_id, probed_at DESC) 取单账号时间线，
//     按 (platform, probed_at DESC) 做平台维度聚合。
//
// 注意：本插件不写 core 表，account 状态机仍由 core scheduler 自己根据真实流量维护。
const schemaSQL = `
CREATE TABLE IF NOT EXISTS health_probes (
    id           BIGSERIAL PRIMARY KEY,
    account_id   BIGINT NOT NULL,
    platform     VARCHAR(64) NOT NULL,
    probed_at    TIMESTAMPTZ NOT NULL,
    success      BOOLEAN NOT NULL,
    latency_ms   INTEGER NOT NULL DEFAULT 0,
    status_code  INTEGER NOT NULL DEFAULT 0,
    error_kind   VARCHAR(32) NOT NULL DEFAULT '',
    error_msg    VARCHAR(512) NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_health_probes_account_time  ON health_probes(account_id, probed_at DESC);
CREATE INDEX IF NOT EXISTS idx_health_probes_platform_time ON health_probes(platform, probed_at DESC);
CREATE INDEX IF NOT EXISTS idx_health_probes_probed_at     ON health_probes(probed_at);

DROP TABLE IF EXISTS health_settings;
`

// openDB 打开 core 数据库连接，复用 lib/pq 驱动。
//
// 与 epay 共享一个模式：插件和 core 共用同一个 PostgreSQL 实例。
// 这里的连接同时承担：
//   - 写 health_probes（插件自有）
//   - 读 accounts / groups / account_groups（core 表，只读；groups.note 用于 GroupHealth 备注展示）
func openDB(dsn string) (*sql.DB, error) {
	if dsn == "" {
		return nil, errors.New("db_dsn 未配置")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("数据库连通性检测失败: %w", err)
	}
	return db, nil
}

// migrate 创建插件自有表。
// 与 core 的 ent migrate 互不干扰：表名都带 health_ 前缀。
func migrate(db *sql.DB) error {
	if _, err := db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("创建插件表失败: %w", err)
	}
	return nil
}

// purgeOldProbes 删除超过 retentionDays 的原始探测记录。
// 由后台任务定时调用；返回删除的行数。
func purgeOldProbes(ctx context.Context, db *sql.DB, retentionDays int) (int64, error) {
	if retentionDays <= 0 {
		return 0, nil
	}
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	res, err := db.ExecContext(ctx, `DELETE FROM health_probes WHERE probed_at < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("清理过期探测记录失败: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
