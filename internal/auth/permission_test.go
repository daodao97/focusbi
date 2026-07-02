package auth

import "testing"

func perm(res map[string]string) *Permission {
	p := &Permission{tree: permNode{}}
	for resource, mode := range res {
		p.add(resource, mode)
	}
	return p
}

func TestCheckExactResource(t *testing.T) {
	p := perm(map[string]string{"report.5": "rw"})
	if !p.Check("report.5", "r") {
		t.Error("should have read on report.5")
	}
	if !p.Check("report.5", "w") {
		t.Error("should have write on report.5")
	}
	if !p.Check("report.5", "rw") {
		t.Error("should have rw on report.5")
	}
	if p.Check("report.6", "r") {
		t.Error("should NOT have report.6")
	}
}

func TestCheckReadOnly(t *testing.T) {
	p := perm(map[string]string{"report.5": "r"})
	if !p.Check("report.5", "r") {
		t.Error("read should pass")
	}
	if p.Check("report.5", "w") {
		t.Error("write should fail on read-only")
	}
}

func TestRecursivePermission(t *testing.T) {
	// report 带 R 递归读 => 所有 report.{id} 都可读
	p := perm(map[string]string{"report": "Rr"})
	if !p.Check("report.5", "r") {
		t.Error("recursive read should cover report.5")
	}
	if !p.Check("report.999", "r") {
		t.Error("recursive read should cover report.999")
	}
	if p.Check("report.5", "w") {
		t.Error("recursive read should NOT grant write")
	}
	// 不带 R 时, report 不覆盖 report.5
	p2 := perm(map[string]string{"report": "r"})
	if p2.Check("report.5", "r") {
		t.Error("non-recursive report should NOT cover report.5")
	}
}

func TestWildcard(t *testing.T) {
	p := perm(map[string]string{"*": "rw"})
	if !p.Check("report.5", "rw") || !p.Check("dsn", "r") {
		t.Error("wildcard should grant everything")
	}
	p2 := perm(map[string]string{"*": "r"})
	if p2.Check("report.5", "w") {
		t.Error("wildcard read should not grant write")
	}
}

func TestMergeMultipleRoles(t *testing.T) {
	// 模拟两个角色合并: report.5 读 + report.5 写 => rw
	p := perm(map[string]string{"report.5": "r"})
	p.add("report.5", "w")
	if !p.Check("report.5", "rw") {
		t.Errorf("merged should be rw, got %v", p.Resources())
	}
}

func TestContainsDelegation(t *testing.T) {
	boss := perm(map[string]string{"report": "Rrw", "dsn": "rw"})

	// 下属请求子集 -> 允许
	sub1 := perm(map[string]string{"report.5": "r"})
	if !boss.Contains(sub1) {
		t.Error("boss should contain report.5:r (covered by report:Rrw)")
	}
	// 下属请求 dsn 读 -> 允许
	if !boss.Contains(perm(map[string]string{"dsn": "r"})) {
		t.Error("boss should contain dsn:r")
	}
	// 下属请求超出范围 (admin 资源) -> 拒绝
	if boss.Contains(perm(map[string]string{"admin": "rw"})) {
		t.Error("boss should NOT contain admin:rw")
	}
	// 非管理员不能授出管理员
	adminP := &Permission{isAdmin: true}
	if boss.Contains(adminP) {
		t.Error("non-admin should not contain admin权限")
	}
}

func TestAdminAllowsEverything(t *testing.T) {
	admin := &Permission{isAdmin: true}
	if !admin.Check("anything.deep.here", "rw") {
		t.Error("admin should pass any check")
	}
	if !admin.Contains(perm(map[string]string{"report": "Rrw"})) {
		t.Error("admin contains everything")
	}
}

func TestParseResourceJSON(t *testing.T) {
	m := parseResourceJSON(`{"report":"Rr","report.5":"rw"}`)
	if m["report"] != "Rr" || m["report.5"] != "rw" {
		t.Fatalf("parse failed: %+v", m)
	}
	if parseResourceJSON("") != nil || parseResourceJSON("{}") != nil {
		t.Error("empty/{} should be nil")
	}
}

// ---- 按数据源权限 (dsn.<id> / dsn.default) ----

func TestReportReadableWriteMode(t *testing.T) {
	// 树: 10(folder) -> 20(report); 30(report) 独立
	parents := map[int]int{10: 0, 20: 10, 30: 0}

	// 直接授 report.20:w -> 可写 20, 不可写 30
	p := perm(map[string]string{"report.20": "w"})
	if !p.ReportReadable(20, parents, "w") {
		t.Error("report.20:w 应可写 20")
	}
	if p.ReportReadable(30, parents, "w") {
		t.Error("不应可写未授权的 30")
	}

	// 授文件夹 report.10:Rw (递归) -> 覆盖后代 20 的写
	pf := perm(map[string]string{"report.10": "Rw"})
	if !pf.ReportReadable(20, parents, "w") {
		t.Error("文件夹 report.10:Rw 应递归覆盖后代 20 的写")
	}

	// 只读授权不产生写权限
	pr := perm(map[string]string{"report.20": "r"})
	if pr.ReportReadable(20, parents, "w") {
		t.Error("report.20:r 不应有写权限")
	}
}

// CanWriteAnyReport: 任意范围有报表写即为开发者 (供无 id 的写入口判权)。
func TestCanWriteAnyReport(t *testing.T) {
	cases := []struct {
		name  string
		perms map[string]string
		want  bool
	}{
		{"全局递归写", map[string]string{"report": "Rrw"}, true},
		{"单报表写", map[string]string{"report.5": "rw"}, true},
		{"文件夹递归写", map[string]string{"report.10": "Rw"}, true},
		{"仅只读", map[string]string{"report": "Rr", "report.5": "r"}, false},
		{"无报表权限", map[string]string{"dsn": "r"}, false},
		{"空权限", map[string]string{}, false},
		{"通配符写", map[string]string{"*": "rw"}, true},
		{"__all 写", map[string]string{"__all": "rw"}, true},
		{"通配符只读", map[string]string{"*": "r"}, false},
	}
	for _, c := range cases {
		if got := perm(c.perms).CanWriteAnyReport(); got != c.want {
			t.Errorf("%s: CanWriteAnyReport()=%v, want %v", c.name, got, c.want)
		}
	}
}

func TestDsnResourceByID(t *testing.T) {
	if DsnResourceByID(5) != "dsn.5" {
		t.Errorf("DsnResourceByID(5) = %q, want dsn.5", DsnResourceByID(5))
	}
	if DsnDefaultResource != "dsn.default" {
		t.Errorf("DsnDefaultResource = %q", DsnDefaultResource)
	}
}

func TestDsnReadableExactID(t *testing.T) {
	nameToID := map[string]int{"sales": 5, "finance": 6}
	p := perm(map[string]string{"dsn.5": "r"})
	if !p.DsnReadable("sales", nameToID) {
		t.Error("应能读 sales (dsn.5)")
	}
	if p.DsnReadable("finance", nameToID) {
		t.Error("不应能读 finance (dsn.6 未授)")
	}
	// 未知名 -> 拒绝
	if p.DsnReadable("ghost", nameToID) {
		t.Error("未知数据源应拒绝")
	}
}

func TestDsnReadableGlobalRecursive(t *testing.T) {
	nameToID := map[string]int{"sales": 5, "finance": 6}
	// 全局 dsn 带 R 递归 -> 覆盖所有具体源 + default
	p := perm(map[string]string{"dsn": "Rr"})
	if !p.DsnReadable("sales", nameToID) || !p.DsnReadable("finance", nameToID) {
		t.Error("dsn:Rr 应覆盖所有具体源")
	}
	if !p.DsnReadable("default", nameToID) || !p.DsnReadable("", nameToID) {
		t.Error("dsn:Rr 应覆盖 default")
	}
}

func TestDsnReadableDefaultKey(t *testing.T) {
	nameToID := map[string]int{}
	p := perm(map[string]string{"dsn.default": "r"})
	if !p.DsnReadable("default", nameToID) || !p.DsnReadable("", nameToID) {
		t.Error("dsn.default:r 应允许 default / 空名")
	}
}

func TestDsnAuthz(t *testing.T) {
	nameToID := map[string]int{"sales": 5}
	authz := perm(map[string]string{"dsn.5": "r"}).DsnAuthz(nameToID)
	if err := authz("sales"); err != nil {
		t.Errorf("sales 应放行: %v", err)
	}
	if err := authz("ghost"); err == nil {
		t.Error("不存在的数据源应报错")
	}
	// 有 sales 但无 default
	if err := authz("default"); err == nil {
		t.Error("无 default 权限应被拒")
	}
	// 越权: 未授的已知源
	nameToID["finance"] = 6
	if err := authz("finance"); err == nil {
		t.Error("finance 未授应被拒")
	}
}
