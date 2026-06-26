package dao

import (
	"encoding/json"
	"strings"

	"github.com/daodao97/xgo/xdb"
	"github.com/daodao97/xgo/xlog"
)

// BackfillRoleDsnPerm 是一次性数据回填 (幂等):
// 引入按数据源权限 (dsn.<id>) 后, 旧角色若"能读报表却没有任何 dsn 权限",
// 升级后会跑不动报表 (运行时强制 dsn 授权)。这里给这类角色补全局 dsn:r,
// 保持升级前行为 (可访问所有数据源), 管理员之后再逐源收紧。
//
// 仅当角色 resource 含任一 report 读键 (report 或 report.*, 模式含 r) 且
// 完全没有 dsn 相关键 (dsn / dsn.*) 时才补; 已配过 dsn 的角色一律不动。
func BackfillRoleDsnPerm() error {
	if Role == nil {
		return nil
	}
	roles, err := ListRoles()
	if err != nil {
		return err
	}
	var patched []string
	for _, role := range roles {
		res := parseRoleResource(role.Resource)
		if res == nil {
			continue
		}
		if hasAnyDsnKey(res) || !hasReportReadKey(res) {
			continue
		}
		res["dsn"] = "r"
		b, err := json.Marshal(res)
		if err != nil {
			continue
		}
		if err := UpdateRoleByID(role.Id, xdb.Record{"resource": string(b)}); err != nil {
			xlog.Error("backfill role dsn perm failed", xlog.Int("role", role.Id), xlog.String("err", err.Error()))
			continue
		}
		patched = append(patched, role.Name)
	}
	if len(patched) > 0 {
		xlog.Info("backfilled dsn:r for roles (kept all-datasource access)", xlog.Any("roles", patched))
	}
	return nil
}

func parseRoleResource(s string) map[string]string {
	s = strings.TrimSpace(s)
	if s == "" || s == "{}" {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil
	}
	return m
}

// hasAnyDsnKey 判断 resource 是否含任一 dsn 相关键 (dsn 或 dsn.*)。
func hasAnyDsnKey(res map[string]string) bool {
	for k := range res {
		if k == "dsn" || strings.HasPrefix(k, "dsn.") {
			return true
		}
	}
	return false
}

// hasReportReadKey 判断 resource 是否含任一 report 读键 (report 或 report.*, 模式含 r)。
func hasReportReadKey(res map[string]string) bool {
	for k, mode := range res {
		if (k == "report" || strings.HasPrefix(k, "report.")) && strings.Contains(mode, "r") {
			return true
		}
	}
	return false
}
