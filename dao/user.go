package dao

import (
	"fmt"
	"strings"
	"time"

	"github.com/daodao97/xgo/xdb"
)

var User xdb.Model

type UserRecord struct {
	Id        int       `json:"id"`
	Username  string    `json:"username"`
	Password  string    `json:"-"` // 不输出到 JSON
	Nick      string    `json:"nick"`
	Roles     string    `json:"roles"` // 逗号分隔的角色 id
	IsAdmin   bool      `json:"is_admin"`
	Email     string    `json:"email"`
	Avatar    string    `json:"avatar"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (u *UserRecord) Record() xdb.Record {
	return xdb.Record{
		"username": strings.TrimSpace(u.Username),
		"password": u.Password,
		"nick":     strings.TrimSpace(u.Nick),
		"roles":    strings.TrimSpace(u.Roles),
		"is_admin": boolToInt(u.IsAdmin),
		"email":    strings.TrimSpace(u.Email),
		"avatar":   strings.TrimSpace(u.Avatar),
	}
}

func (u *UserRecord) FromRecord(r xdb.Record) {
	u.Id = r.GetInt("id")
	u.Username = r.GetString("username")
	u.Password = r.GetString("password")
	u.Nick = r.GetString("nick")
	u.Roles = r.GetString("roles")
	u.IsAdmin = r.GetInt("is_admin") != 0
	u.Email = r.GetString("email")
	u.Avatar = r.GetString("avatar")
	if t := r.GetTime("created_at"); t != nil {
		u.CreatedAt = *t
	}
	if t := r.GetTime("updated_at"); t != nil {
		u.UpdatedAt = *t
	}
}

// RoleIDs 把逗号分隔的 roles 字段解析为 id 切片。
func (u *UserRecord) RoleIDs() []int {
	return splitIntList(u.Roles)
}

func CountUsers() (int64, error) {
	if User == nil {
		return 0, fmt.Errorf("user model not initialized")
	}
	return User.Count()
}

func CreateUser(u *UserRecord) (int64, error) {
	if User == nil {
		return 0, fmt.Errorf("user model not initialized")
	}
	if strings.TrimSpace(u.Username) == "" {
		return 0, fmt.Errorf("username is empty")
	}
	return User.Insert(u.Record())
}

func GetUserByID(id int) (*UserRecord, error) {
	if User == nil {
		return nil, fmt.Errorf("user model not initialized")
	}
	r, err := User.First(xdb.WhereEq("id", id))
	if err != nil {
		return nil, err
	}
	u := &UserRecord{}
	u.FromRecord(r)
	return u, nil
}

func GetUserByName(username string) (*UserRecord, error) {
	if User == nil {
		return nil, fmt.Errorf("user model not initialized")
	}
	r, err := User.First(xdb.WhereEq("username", strings.TrimSpace(username)))
	if err != nil {
		return nil, err
	}
	u := &UserRecord{}
	u.FromRecord(r)
	return u, nil
}

func ListUsers() ([]*UserRecord, error) {
	if User == nil {
		return nil, fmt.Errorf("user model not initialized")
	}
	records, err := User.Selects(xdb.OrderByDesc("id"))
	if err != nil {
		return nil, err
	}
	out := make([]*UserRecord, 0, len(records))
	for _, r := range records {
		u := &UserRecord{}
		u.FromRecord(r)
		out = append(out, u)
	}
	return out, nil
}

func UpdateUserByID(id int, updates xdb.Record) error {
	if User == nil {
		return fmt.Errorf("user model not initialized")
	}
	if len(updates) == 0 {
		return fmt.Errorf("updates is empty")
	}
	_, err := User.Update(updates, xdb.WhereEq("id", id))
	return err
}

func DeleteUserByID(id int) error {
	if User == nil {
		return fmt.Errorf("user model not initialized")
	}
	_, err := User.Delete(xdb.WhereEq("id", id))
	return err
}

// splitIntList 解析 "1,2,5" 为 []int, 跳过空与非法项。
func splitIntList(s string) []int {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var out []int
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n := 0
		ok := true
		for _, r := range p {
			if r < '0' || r > '9' {
				ok = false
				break
			}
			n = n*10 + int(r-'0')
		}
		if ok {
			out = append(out, n)
		}
	}
	return out
}
