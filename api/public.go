package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"

	"xproxy/dao"
	"xproxy/internal/engine"
	"xproxy/internal/runtimecfg"

	"github.com/daodao97/xgo/xdb"
	"github.com/gin-gonic/gin"
)

// ---- 公开分享 (无需登录) ----

// publicGetReport 凭 share_token 返回报表的最小元信息 (仅名称)。
func publicGetReport(c *gin.Context) {
	if !runtimecfg.PublicShareEnabled() {
		fail(c, http.StatusForbidden, "公开分享已被系统管理员关闭")
		return
	}
	r, err := dao.GetReportByShareToken(c.Param("token"))
	if err != nil {
		if err == xdb.ErrNotFound {
			fail(c, http.StatusNotFound, "分享链接无效或已关闭")
			return
		}
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"id": r.Id, "name": r.Name})
}

// publicRunReport 凭 share_token 执行报表 (公开访问可带过滤参数重查)。
func publicRunReport(c *gin.Context) {
	if !runtimecfg.PublicShareEnabled() {
		fail(c, http.StatusForbidden, "公开分享已被系统管理员关闭")
		return
	}
	r, err := dao.GetReportByShareToken(c.Param("token"))
	if err != nil {
		if err == xdb.ErrNotFound {
			fail(c, http.StatusNotFound, "分享链接无效或已关闭")
			return
		}
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	var req runRequest
	_ = c.ShouldBindJSON(&req)

	settings := r.ParseSettings()
	if len(settings.ApprovedDSNs) == 0 {
		fail(c, http.StatusForbidden, "分享报表未完成数据源预授权, 请重新开启分享")
		return
	}
	result, err := engine.NewRunner(r.DSN).
		WithCacheScope("report:"+strconv.Itoa(r.Id)).
		WithNoCache(noCacheParam(req.Params)).
		WithAuthz(engine.AllowlistAuthz(settings.ApprovedDSNs)).
		Run(r.Content, req.Params)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	result.AutoRefresh = settings.AutoRefresh       // 报表级自动刷新
	result.PrependContent = settings.PrependContent // 页面顶部 HTML
	ok(c, result)
}

// ---- 分享开关 (管理端, 需 report.{id}:w) ----

type shareReq struct {
	Enable bool `json:"enable"`
}

// setReportShare 开启/关闭某报表的公开分享。开启时若无令牌则生成一个。
func setReportShare(c *gin.Context) {
	id, valid := paramID(c)
	if !valid {
		return
	}
	if !requireReportWrite(c, id) {
		return
	}
	var req shareReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if req.Enable && !runtimecfg.PublicShareEnabled() {
		fail(c, http.StatusForbidden, "公开分享已被系统管理员关闭")
		return
	}

	r, err := dao.GetReportByID(id)
	if err != nil {
		fail(c, http.StatusNotFound, "报表不存在")
		return
	}
	if req.Enable && !r.IsFolder() {
		dsns, ok := authorizeReportContentDSNs(c, r.DSN, r.Content)
		if !ok {
			return
		}
		if err := approveReportDSNs(id, r.Settings, dsns); err != nil {
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
	}

	token := r.ShareToken
	if req.Enable && token == "" {
		token = genShareToken()
	}
	if err := dao.SetReportShare(id, req.Enable, token); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}

	resp := gin.H{"is_public": req.Enable}
	if req.Enable {
		resp["share_token"] = token
	}
	ok(c, resp)
}

// genShareToken 生成 32 字符 (16 字节) 的随机十六进制令牌。
func genShareToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
