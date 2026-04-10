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
// 当前只维护一张表：group_health_probes（分组级黑盒探测时序）。
//
// 历史：原先还有一张 health_probes（账号级探测），Step 2 重构后废弃，在同一段
// migration 里 DROP。保留 DROP 语句是为了让升级路径干净：老部署的 airgate-health
// 插件在热重载时会自动清理掉旧表，不需要管理员手工介入。
//
// 选型说明：
//   - 与 epay 一样手写 DDL 而不是引入 ent，避免代码生成步骤；表结构稳定。
//   - 表名前缀 group_health_ 防止与 core 表冲突，同时避开原 health_probes 的旧名。
//   - 索引覆盖最常见查询：
//     按 (group_id, probed_at DESC) 取单分组时间线
//     按 (platform, probed_at DESC) 做平台维度聚合
//     按 (probed_at) 做 retention 清理
//
// 注意：本插件不写 core 表，account 状态机仍由 core scheduler 自己根据真实流量维护；
// 探测过程中通过 HostService.ProbeForward 间接让 scheduler 状态机受益。
const schemaSQL = `
CREATE TABLE IF NOT EXISTS group_health_probes (
    id           BIGSERIAL PRIMARY KEY,
    group_id     BIGINT NOT NULL,
    platform     VARCHAR(64) NOT NULL,
    probed_at    TIMESTAMPTZ NOT NULL,
    success      BOOLEAN NOT NULL,
    latency_ms   INTEGER NOT NULL DEFAULT 0,
    status_code  INTEGER NOT NULL DEFAULT 0,
    account_id   BIGINT NOT NULL DEFAULT 0,
    model        VARCHAR(128) NOT NULL DEFAULT '',
    error_kind   VARCHAR(32) NOT NULL DEFAULT '',
    error_msg    VARCHAR(512) NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_ghp_group_time    ON group_health_probes(group_id, probed_at DESC);
CREATE INDEX IF NOT EXISTS idx_ghp_platform_time ON group_health_probes(platform, probed_at DESC);
CREATE INDEX IF NOT EXISTS idx_ghp_probed_at     ON group_health_probes(probed_at);

DROP TABLE IF EXISTS health_probes;
DROP TABLE IF EXISTS health_settings;
`

// openDB 打开 core 数据库连接，复用 lib/pq 驱动。
//
// 与 epay 共享一个模式：插件和 core 共用同一个 PostgreSQL 实例。
// 这里的连接同时承担：
//   - 写 group_health_probes（插件自有）
//   - 读 groups（core 表，只读；用于列举探测目标 + groups.note 作为运维备注展示）
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
// 与 core 的 ent migrate 互不干扰：表名都带 group_health_ 前缀。
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
	res, err := db.ExecContext(ctx, `DELETE FROM group_health_probes WHERE probed_at < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("清理过期探测记录失败: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
