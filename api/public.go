package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"xproxy/dao"
	"xproxy/internal/engine"

	"github.com/daodao97/xgo/xdb"
	"github.com/gin-gonic/gin"
)

// ---- 公开分享 (无需登录) ----

// publicGetReport 凭 share_token 返回报表的最小元信息 (仅名称)。
func publicGetReport(c *gin.Context) {
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

	result, err := engine.NewRunner(r.DSN).WithNoCache(noCacheParam(req.Params)).Run(r.Content, req.Params)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	settings := r.ParseSettings()
	result.AutoRefresh = settings.AutoRefresh       // 报表级自动刷新
	result.PrependContent = settings.PrependContent // 页面顶部 HTML
	ok(c, result)
}

// ---- 分享开关 (管理端, 需 report.manage) ----

type shareReq struct {
	Enable bool `json:"enable"`
}

// setReportShare 开启/关闭某报表的公开分享。开启时若无令牌则生成一个。
func setReportShare(c *gin.Context) {
	id, valid := paramID(c)
	if !valid {
		return
	}
	var req shareReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}

	r, err := dao.GetReportByID(id)
	if err != nil {
		fail(c, http.StatusNotFound, "报表不存在")
		return
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
