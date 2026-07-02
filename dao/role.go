package dao

import (
	"fmt"
	"strings"
	"time"

	"github.com/daodao97/xgo/xdb"
)

var Role xdb.Model

type RoleRecord struct {
	Id        int       `json:"id"`
	Name      string    `json:"name"`
	ParentID  int       `json:"parent_id"`
	Resource  string    `json:"resource"` // JSON: {"report":"Rr","report.5":"rw"}
	Remark    string    `json:"remark"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (r *RoleRecord) Record() xdb.Record {
	res := strings.TrimSpace(r.Resource)
	if res == "" {
		res = "{}"
	}
	return xdb.Record{
		"name":      strings.TrimSpace(r.Name),
		"parent_id": r.ParentID,
		"resource":  res,
		"remark":    strings.TrimSpace(r.Remark),
	}
}

func (r *RoleRecord) FromRecord(rec xdb.Record) {
	r.Id = rec.GetInt("id")
	r.Name = rec.GetString("name")
	r.ParentID = rec.GetInt("parent_id")
	r.Resource = rec.GetString("resource")
	r.Remark = rec.GetString("remark")
	if t := rec.GetTime("created_at"); t != nil {
		r.CreatedAt = *t
	}
	if t := rec.GetTime("updated_at"); t != nil {
		r.UpdatedAt = *t
	}
}

func CreateRole(r *RoleRecord) (int64, error) {
	if Role == nil {
		return 0, fmt.Errorf("role model not initialized")
	}
	if strings.TrimSpace(r.Name) == "" {
		return 0, fmt.Errorf("role name is empty")
	}
	return Role.Insert(r.Record())
}

func ListRoles() ([]*RoleRecord, error) {
	if Role == nil {
		return nil, fmt.Errorf("role model not initialized")
	}
	records, err := Role.Selects(xdb.OrderByDesc("id"))
	if err != nil {
		return nil, err
	}
	out := make([]*RoleRecord, 0, len(records))
	for _, rec := range records {
		r := &RoleRecord{}
		r.FromRecord(rec)
		out = append(out, r)
	}
	return out, nil
}

// ListRolesByIDs 按 id 列表批量取角色 (用于构建用户权限)。
func ListRolesByIDs(ids []int) ([]*RoleRecord, error) {
	if Role == nil {
		return nil, fmt.Errorf("role model not initialized")
	}
	if len(ids) == 0 {
		return nil, nil
	}
	anyIDs := make([]any, len(ids))
	for i, id := range ids {
		anyIDs[i] = id
	}
	records, err := Role.Selects(xdb.WhereIn("id", anyIDs))
	if err != nil {
		return nil, err
	}
	out := make([]*RoleRecord, 0, len(records))
	for _, rec := range records {
		r := &RoleRecord{}
		r.FromRecord(rec)
		out = append(out, r)
	}
	return out, nil
}

func UpdateRoleByID(id int, updates xdb.Record) error {
	if Role == nil {
		return fmt.Errorf("role model not initialized")
	}
	if len(updates) == 0 {
		return fmt.Errorf("updates is empty")
	}
	_, err := Role.Update(updates, xdb.WhereEq("id", id))
	return err
}

func DeleteRoleByID(id int) error {
	if Role == nil {
		return fmt.Errorf("role model not initialized")
	}
	_, err := Role.Delete(xdb.WhereEq("id", id))
	return err
}
