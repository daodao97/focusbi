package api

import (
	"net/http"
	"strings"
	"time"

	"xproxy/dao"
	"xproxy/internal/auth"

	"github.com/daodao97/xgo/xdb"
	"github.com/gin-gonic/gin"
)

// ---- 认证 ----

type loginReq struct {
	Username       string `json:"username"`
	Password       string `json:"password"`
	TurnstileToken string `json:"turnstile_token"`
}

// authBootstrap 返回系统是否还没有任何用户 (前端据此显示注册/登录)。
func authBootstrap(c *gin.Context) {
	n, err := dao.CountUsers()
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"need_register": n == 0, "turnstile": turnstilePublicConfig()})
}

// register 注册。仅当系统尚无用户时开放, 首位注册者自动成为超管。
func register(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		fail(c, http.StatusBadRequest, "用户名和密码不能为空")
		return
	}
	if err := verifyTurnstile(c.Request.Context(), req.TurnstileToken, c.ClientIP()); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}

	n, err := dao.CountUsers()
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if n > 0 {
		fail(c, http.StatusForbidden, "系统已初始化, 注册已关闭, 请联系管理员建号")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	u := &dao.UserRecord{
		Username: req.Username,
		Password: hash,
		Nick:     req.Username,
		IsAdmin:  true, // 首位即超管
	}
	id, err := dao.CreateUser(u)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	issueAndReturn(c, int(id), req.Username, true)
}

func login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := verifyTurnstile(c.Request.Context(), req.TurnstileToken, c.ClientIP()); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	// 失败退避: 连续失败达阈值后锁定, 时长指数增长 (防暴力破解)。
	if locked, wait := loginLocked(c.ClientIP(), req.Username); locked {
		fail(c, http.StatusTooManyRequests, "登录失败次数过多, 请 "+wait.String()+" 后再试")
		return
	}
	u, err := dao.GetUserByName(strings.TrimSpace(req.Username))
	if err != nil {
		if err == xdb.ErrNotFound {
			loginFailed(c.ClientIP(), req.Username)
			fail(c, http.StatusUnauthorized, "用户名或密码错误")
			return
		}
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if !auth.CheckPassword(u.Password, req.Password) {
		loginFailed(c.ClientIP(), req.Username)
		fail(c, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	loginSucceeded(c.ClientIP(), req.Username)
	issueAndReturn(c, u.Id, u.Username, u.IsAdmin)
}

func issueAndReturn(c *gin.Context, uid int, username string, isAdmin bool) {
	token, err := auth.IssueToken(uid, username, isAdmin)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	secure := c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(auth.SessionCookieName, token, int((7 * 24 * time.Hour).Seconds()), "/", "", secure, true)
	ok(c, gin.H{"ok": true})
}

// me 返回当前用户信息与已编译的权限资源 (供前端做按钮显隐)。
func me(c *gin.Context) {
	u := auth.UserOf(c)
	if u == nil {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	p := auth.PermOf(c)
	res := map[string]string{}
	if p != nil {
		res = p.Resources()
	}
	ok(c, gin.H{
		"id": u.Id, "username": u.Username, "nick": u.Nick,
		"is_admin": u.IsAdmin, "email": u.Email, "avatar": u.Avatar,
		"resources": res,
	})
}

func logout(c *gin.Context) {
	secure := c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(auth.SessionCookieName, "", -1, "/", "", secure, true)
	ok(c, gin.H{"ok": true})
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ---- 用户管理 (仅管理员) ----

type userReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Nick     string `json:"nick"`
	Roles    string `json:"roles"`
	IsAdmin  bool   `json:"is_admin"`
	Email    string `json:"email"`
}

func listUsersAPI(c *gin.Context) {
	users, err := dao.ListUsers()
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, users)
}

func createUserAPI(c *gin.Context) {
	var req userReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.Username) == "" || req.Password == "" {
		fail(c, http.StatusBadRequest, "用户名和密码不能为空")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	u := &dao.UserRecord{
		Username: req.Username, Password: hash, Nick: req.Nick,
		Roles: req.Roles, IsAdmin: req.IsAdmin, Email: req.Email,
	}
	id, err := dao.CreateUser(u)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"id": id})
}

func updateUserAPI(c *gin.Context) {
	id, valid := paramID(c)
	if !valid {
		return
	}
	var req userReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	updates := xdb.Record{
		"nick":     strings.TrimSpace(req.Nick),
		"roles":    strings.TrimSpace(req.Roles),
		"is_admin": boolToInt(req.IsAdmin),
		"email":    strings.TrimSpace(req.Email),
	}
	// 密码非空才更新
	if req.Password != "" {
		hash, err := auth.HashPassword(req.Password)
		if err != nil {
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
		updates["password"] = hash
	}
	if err := dao.UpdateUserByID(id, updates); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"id": id})
}

func deleteUserAPI(c *gin.Context) {
	id, valid := paramID(c)
	if !valid {
		return
	}
	// 禁止删除自己
	if u := auth.UserOf(c); u != nil && u.Id == id {
		fail(c, http.StatusBadRequest, "不能删除当前登录用户")
		return
	}
	if err := dao.DeleteUserByID(id); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"id": id})
}

// ---- 角色管理 (仅管理员) ----

type roleReq struct {
	Name     string `json:"name"`
	ParentID int    `json:"parent_id"`
	Resource string `json:"resource"` // JSON 字符串
	Remark   string `json:"remark"`
}

func listRolesAPI(c *gin.Context) {
	roles, err := dao.ListRoles()
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, roles)
}

func createRoleAPI(c *gin.Context) {
	var req roleReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	r := &dao.RoleRecord{Name: req.Name, ParentID: req.ParentID, Resource: req.Resource, Remark: req.Remark}
	id, err := dao.CreateRole(r)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"id": id})
}

func updateRoleAPI(c *gin.Context) {
	id, valid := paramID(c)
	if !valid {
		return
	}
	var req roleReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	r := &dao.RoleRecord{Name: req.Name, ParentID: req.ParentID, Resource: req.Resource, Remark: req.Remark}
	if err := dao.UpdateRoleByID(id, r.Record()); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"id": id})
}

func deleteRoleAPI(c *gin.Context) {
	id, valid := paramID(c)
	if !valid {
		return
	}
	if err := dao.DeleteRoleByID(id); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"id": id})
}
