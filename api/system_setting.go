package api

import (
	"net/http"
	"strconv"

	"xproxy/internal/runtimecfg"

	"github.com/gin-gonic/gin"
)

type systemSettingsReq struct {
	ScriptFetch        *string `json:"script_fetch"`
	QueryTimeout       *string `json:"query_timeout"`
	QueryConcurrency   *int    `json:"query_concurrency"`
	ScriptTimeout      *string `json:"script_timeout"`
	ScheduleEnabled    *bool   `json:"schedule_enabled"`
	PublicShareEnabled *bool   `json:"public_share_enabled"`
}

func getSystemSettings(c *gin.Context) {
	ok(c, currentSystemSettings())
}

func updateSystemSettings(c *gin.Context) {
	var req systemSettingsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	values := make(map[string]string)
	if req.ScriptFetch != nil {
		values[runtimecfg.ScriptFetchKey] = *req.ScriptFetch
	}
	if req.QueryTimeout != nil {
		values[runtimecfg.QueryTimeoutKey] = *req.QueryTimeout
	}
	if req.QueryConcurrency != nil {
		values[runtimecfg.QueryConcurrencyKey] = strconv.Itoa(*req.QueryConcurrency)
	}
	if req.ScriptTimeout != nil {
		values[runtimecfg.ScriptTimeoutKey] = *req.ScriptTimeout
	}
	if req.ScheduleEnabled != nil {
		values[runtimecfg.ScheduleEnabledKey] = strconv.FormatBool(*req.ScheduleEnabled)
	}
	if req.PublicShareEnabled != nil {
		values[runtimecfg.PublicShareEnabledKey] = strconv.FormatBool(*req.PublicShareEnabled)
	}
	if len(values) == 0 {
		fail(c, http.StatusBadRequest, "没有可更新的系统设置")
		return
	}
	if err := runtimecfg.Update(values); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, currentSystemSettings())
}

func currentSystemSettings() gin.H {
	values, sources := runtimecfg.Snapshot()
	queryConcurrency, _ := strconv.Atoi(values[runtimecfg.QueryConcurrencyKey])
	scheduleEnabled, _ := strconv.ParseBool(values[runtimecfg.ScheduleEnabledKey])
	publicShareEnabled, _ := strconv.ParseBool(values[runtimecfg.PublicShareEnabledKey])
	return gin.H{
		"script_fetch":         values[runtimecfg.ScriptFetchKey],
		"query_timeout":        values[runtimecfg.QueryTimeoutKey],
		"query_concurrency":    queryConcurrency,
		"script_timeout":       values[runtimecfg.ScriptTimeoutKey],
		"schedule_enabled":     scheduleEnabled,
		"public_share_enabled": publicShareEnabled,
		"sources":              sources,
	}
}
