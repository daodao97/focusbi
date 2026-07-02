package auth

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"xproxy/dao"
)

// ReportResource 返回单个报表的权限资源串 (report.<id>)。
func ReportResource(id int) string {
	return "report." + strconv.Itoa(id)
}

// ReportReadable 判断本权限对某报表是否有 mode 权限, 沿 parent_id 祖先链向上判定:
//   - report.{id} 自身被授权, 或任一祖先文件夹被授权 (文件夹授权递归覆盖后代), 或
//   - 全局 report (经引擎 R 递归) 被授权。
//
// parents 为 reportID -> parentID 映射 (调用方可一次性 LoadReportParents 取得后复用)。
func (p *Permission) ReportReadable(id int, parents map[int]int, mode string) bool {
	if p == nil {
		return false
	}
	if p.Check(ReportResource(id), mode) {
		return true
	}
	seen := map[int]bool{id: true}
	cur := parents[id]
	for cur > 0 && !seen[cur] {
		seen[cur] = true
		if p.Check(ReportResource(cur), mode) {
			return true
		}
		cur = parents[cur]
	}
	return false
}

// CanWriteAnyReport 判断本权限是否在【任意范围】拥有报表写权限:
// 全局 report:w / 任一 report.<id>:w / 文件夹 report.<id>:Rw 皆算。
// 用于没有具体报表 id 的写入口 (模板预览、AI 改写、根目录建报表、全局定时任务管理页) ——
// 即回答"这是不是一个报表开发者"。具体某报表能否写仍由 ReportReadable(id, _, "w") 判定。
func (p *Permission) CanWriteAnyReport() bool {
	if p == nil {
		return false
	}
	if p.isAdmin {
		return true
	}
	// Check 覆盖: 显式 report:w、以及任意层级的 * / __all 通配写 (matchNode 逐层判通配)。
	if p.Check("report", "w") {
		return true
	}
	// 补: 具体某报表/文件夹的写 (report.<id>:w / report.*:w), Check("report") 不覆盖这些子节点。
	found := false
	walkLeaves(p.tree, nil, func(resource, mode string) {
		if strings.HasPrefix(resource, "report.") && hasMode(mode, "w") {
			found = true
		}
	})
	return found
}

// LoadReportParents 取全部报表的 id->parent_id 映射 (用于祖先链判权)。
func LoadReportParents() (map[int]int, error) {
	list, err := dao.ListReports()
	if err != nil {
		return nil, err
	}
	parents := make(map[int]int, len(list))
	for _, r := range list {
		parents[r.Id] = r.ParentID
	}
	return parents, nil
}

// DsnDefaultResource 是主库 (default 数据源, 不在 dsn 表) 的权限资源串。
const DsnDefaultResource = "dsn.default"

// DsnResourceByID 返回某数据源的权限资源串 (dsn.<id>)。
func DsnResourceByID(id int) string {
	return "dsn." + strconv.Itoa(id)
}

// LoadDsnNameToID 取数据源 name->id 映射 (default 源不在表内, 由调用方单独处理)。
// 每请求构建一次, 供 DsnAuthz 把引擎里的 dsn 名翻译成资源 id。
func LoadDsnNameToID() (map[string]int, error) {
	list, err := dao.ListDsn()
	if err != nil {
		return nil, err
	}
	m := make(map[string]int, len(list))
	for _, d := range list {
		m[d.Name] = d.Id
	}
	return m, nil
}

// DsnReadable 判断本权限对某数据源 (按名) 是否有读权限。
// default 源用固定资源 dsn.default; 其余按 nameToID 解析为 dsn.<id>。
// nameToID 中查不到的名字视为不存在 -> 拒绝。
//
// 兼容: 全局 dsn 资源 (无论是否带 R) 视为"所有数据源"超集 —— 历史上 {"dsn":"r"}
// 即代表可访问全部数据源, 保留此语义, 不强制用户配 R 递归。
func (p *Permission) DsnReadable(name string, nameToID map[string]int) bool {
	if p == nil {
		return false
	}
	if p.Check("dsn", "r") { // 全局 dsn:r 覆盖所有源
		return true
	}
	if name == "" || name == "default" {
		return p.Check(DsnDefaultResource, "r")
	}
	id, ok := nameToID[name]
	if !ok {
		return false
	}
	return p.Check(DsnResourceByID(id), "r")
}

// DsnAuthz 返回一个授权闭包: 给定数据源名, 有读权限返回 nil, 否则返回错误。
// 供引擎运行时按实际触达的 dsn 逐个校验 (覆盖 @dsn= 覆盖 / 脚本 / enum_sql)。
func (p *Permission) DsnAuthz(nameToID map[string]int) func(name string) error {
	return func(name string) error {
		if p.DsnReadable(name, nameToID) {
			return nil
		}
		// 区分"不存在"与"无权", 给更明确的报错。
		if name != "" && name != "default" {
			if _, ok := nameToID[name]; !ok {
				return fmt.Errorf("数据源 %q 不存在", name)
			}
		}
		dn := name
		if dn == "" {
			dn = "default"
		}
		return fmt.Errorf("无权使用数据源 %q", dn)
	}
}

// Permission 是一次请求内某用户的权限判定器。
//
// 权限来源: 用户的多个角色 (含 parent_id 继承链) 的 resource JSON。
// resource 形如 {"report":"Rr","report.5":"rw","dsn":"r"}, 值由以下字符组合:
//   - r 读, w 写
//   - R 递归: 该资源串节点的所有【更深层子资源】自动继承同样的读写权限
//
// 资源串按 "." 分段, 形成树。检查 report.5 时, 命中点包括:
//   - report.5 节点本身 (任意模式)
//   - report 节点且带 R (递归覆盖子节点)
//   - * / __all 通配
type Permission struct {
	isAdmin bool
	// 编译后的权限树: 段 -> 子树; 每个节点用 "" 键存自身 mode 字符串。
	tree permNode
}

// permNode: key 为下一段名; 特殊 key "" 存当前节点的 mode。
type permNode map[string]any

const modeKey = ""

// NewPermission 依据用户构建权限判定器。admin 直接全权。
func NewPermission(u *dao.UserRecord) (*Permission, error) {
	if u == nil {
		return &Permission{tree: permNode{}}, nil
	}
	if u.IsAdmin {
		return &Permission{isAdmin: true}, nil
	}

	// 收集用户角色 + 递归父角色, 合并各自的 resource。
	merged, err := collectResources(u.RoleIDs())
	if err != nil {
		return nil, err
	}
	p := &Permission{tree: permNode{}}
	for res, mode := range merged {
		p.add(res, mode)
	}
	return p, nil
}

// IsAdmin 是否超管。
func (p *Permission) IsAdmin() bool { return p.isAdmin }

// add 把一条 "report.5" => "rw" 写入权限树。
func (p *Permission) add(resource, mode string) {
	resource = strings.ToLower(strings.TrimSpace(resource))
	if resource == "" || mode == "" {
		return
	}
	node := p.tree
	for _, seg := range strings.Split(resource, ".") {
		child, ok := node[seg].(permNode)
		if !ok {
			child = permNode{}
			node[seg] = child
		}
		node = child
	}
	// 合并已有 mode (取并集)
	node[modeKey] = mergeMode(getMode(node), mode)
}

// Check 判断是否对 resource 拥有 mode 权限 (mode 如 "r" / "w" / "rw")。
func (p *Permission) Check(resource, mode string) bool {
	if p.isAdmin {
		return true
	}
	if mode == "" {
		mode = "r"
	}
	resource = strings.ToLower(strings.TrimSpace(resource))
	return matchNode(p.tree, strings.Split(resource, "."), mode)
}

// matchNode 在权限树中逐段匹配资源串。
func matchNode(node permNode, segments []string, want string) bool {
	// 通配: 当前层若有 * / __all 且模式满足, 直接放行 (覆盖任意子资源)
	for _, wild := range []string{"*", "__all"} {
		if child, ok := node[wild].(permNode); ok && hasMode(getMode(child), want) {
			return true
		}
	}

	if len(segments) == 0 {
		return false
	}
	seg := segments[0]
	child, ok := node[seg].(permNode)
	if !ok {
		return false
	}
	m := getMode(child)
	last := len(segments) == 1
	// 命中条件: 已到最后一段, 或该节点带 R 递归; 且读写模式满足。
	if (last || hasMode(m, "R")) && hasMode(m, want) {
		return true
	}
	return matchNode(child, segments[1:], want)
}

// Contains 判断本权限是否【完全覆盖】另一个权限 (转委校验:
// 不能授出超过自己的权限)。other 的每条资源, 本权限都需满足其读写模式。
func (p *Permission) Contains(other *Permission) bool {
	if p.isAdmin {
		return true
	}
	if other.isAdmin {
		return false // 非管理员不能授出管理员
	}
	ok := true
	walkLeaves(other.tree, nil, func(resource, mode string) {
		// other 声明了 mode (可能含 R), 本权限需对该 resource 满足相应读写。
		need := stripR(mode)
		if need == "" {
			need = "r"
		}
		if !p.Check(resource, need) {
			ok = false
		}
	})
	return ok
}

// Resources 导出已编译的扁平资源->mode (用于调试/前端展示)。
func (p *Permission) Resources() map[string]string {
	out := map[string]string{}
	walkLeaves(p.tree, nil, func(resource, mode string) {
		out[resource] = mode
	})
	return out
}

// walkLeaves 遍历权限树, 对每个带 mode 的节点回调 (resource, mode)。
func walkLeaves(node permNode, path []string, fn func(resource, mode string)) {
	if m := getMode(node); m != "" && len(path) > 0 {
		fn(strings.Join(path, "."), m)
	}
	for k, v := range node {
		if k == modeKey {
			continue
		}
		if child, ok := v.(permNode); ok {
			walkLeaves(child, append(append([]string{}, path...), k), fn)
		}
	}
}

// collectResources 收集角色 (含父角色继承链) 的 resource, 合并为 resource->mode。
func collectResources(roleIDs []int) (map[string]string, error) {
	merged := map[string]string{}
	visited := map[int]bool{}

	// BFS 展开: 角色 -> 其 parent 角色, 直到根。
	queue := append([]int{}, roleIDs...)
	for len(queue) > 0 {
		batch := queue
		queue = nil

		var pending []int
		for _, id := range batch {
			if id <= 0 || visited[id] {
				continue
			}
			visited[id] = true
			pending = append(pending, id)
		}
		if len(pending) == 0 {
			continue
		}

		roles, err := dao.ListRolesByIDs(pending)
		if err != nil {
			return nil, err
		}
		for _, role := range roles {
			for res, mode := range parseResourceJSON(role.Resource) {
				merged[res] = mergeMode(merged[res], mode)
			}
			if role.ParentID > 0 && !visited[role.ParentID] {
				queue = append(queue, role.ParentID)
			}
		}
	}
	return merged, nil
}

// parseResourceJSON 解析角色 resource 字段 ({"report":"Rr",...})。
func parseResourceJSON(s string) map[string]string {
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

// ---- mode 字符串工具 ----

func getMode(node permNode) string {
	if m, ok := node[modeKey].(string); ok {
		return m
	}
	return ""
}

// hasMode 判断 mode 串是否包含 want 的所有字符 (大小写敏感: R 为递归标记)。
func hasMode(mode, want string) bool {
	for _, c := range want {
		if !strings.ContainsRune(mode, c) {
			return false
		}
	}
	return true
}

// mergeMode 合并两个 mode 串 (字符并集, 保持 R/r/w 顺序)。
func mergeMode(a, b string) string {
	var sb strings.Builder
	for _, c := range "Rrw" {
		if strings.ContainsRune(a, c) || strings.ContainsRune(b, c) {
			sb.WriteRune(c)
		}
	}
	return sb.String()
}

// stripR 去掉递归标记 R, 仅留读写部分。
func stripR(mode string) string {
	return strings.ReplaceAll(mode, "R", "")
}
