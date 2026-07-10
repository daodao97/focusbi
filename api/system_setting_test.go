package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xproxy/conf"
	"xproxy/dao"
	"xproxy/internal/runtimecfg"

	"github.com/gin-gonic/gin"
)

func TestCurrentSystemSettingsUsesTypedConfigDefaults(t *testing.T) {
	oldConf, oldModel := conf.ConfInstance, dao.SystemSetting
	disabled := false
	conf.ConfInstance = &conf.Conf{
		Engine:   conf.EngineConf{QueryTimeout: "45s", QueryConcurrency: 2, ScriptTimeout: "20s"},
		Schedule: conf.ScheduleConf{Enabled: &disabled},
		Security: conf.SecurityConf{PublicShareEnabled: &disabled},
	}
	dao.SystemSetting = nil
	defer func() {
		conf.ConfInstance, dao.SystemSetting = oldConf, oldModel
		runtimecfg.Invalidate()
	}()
	runtimecfg.Invalidate()

	got := currentSystemSettings()
	if got["query_timeout"] != "45s" || got["query_concurrency"] != 2 || got["script_timeout"] != "20s" {
		t.Fatalf("engine settings = %#v", got)
	}
	if got["schedule_enabled"] != false || got["public_share_enabled"] != false {
		t.Fatalf("feature settings = %#v", got)
	}
}

func TestUpdateSystemSettingsRejectsInvalidValueBeforeDB(t *testing.T) {
	oldModel := dao.SystemSetting
	dao.SystemSetting = nil
	defer func() {
		dao.SystemSetting = oldModel
		runtimecfg.Invalidate()
	}()
	runtimecfg.Invalidate()

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/system/settings", strings.NewReader(`{"query_timeout":"500ms"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	updateSystemSettings(c)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "1s") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
