package schedule

import (
	"errors"
	"time"

	"xproxy/dao"
	"xproxy/internal/runtimecfg"

	"github.com/daodao97/xgo/xlog"
	"github.com/robfig/cron/v3"
)

// nowFunc 便于测试覆写时间源。
var nowFunc = time.Now

// 5 段标准 cron 解析器 (分 时 日 月 周, 不含秒)。
var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// Tick 每分钟调度一次: 扫描所有启用定时任务, 到期的执行并推送。
// 由 job 注册到 xcron (每分钟触发, 带分布式锁)。
func Tick() {
	if !runtimecfg.ScheduleEnabled() {
		return
	}
	minute := nowFunc().Truncate(time.Minute)
	subs, err := dao.ListEnabledSchedules()
	if err != nil {
		xlog.Error("schedule tick: list failed", xlog.String("err", err.Error()))
		return
	}
	for _, sub := range subs {
		if !dueAt(sub.Cron, minute) {
			continue
		}
		// 原子抢占本分钟执行权: 仅抢到的实例执行, 杜绝多实例/重入重复推送。
		claimed, err := dao.ClaimScheduleRun(sub.Id, minute)
		if err != nil {
			xlog.Error("schedule claim failed", xlog.Int("id", sub.Id), xlog.String("err", err.Error()))
			continue
		}
		if !claimed {
			continue // 已被其他实例抢占
		}
		runOne(sub)
	}
}

// runOne 执行单条定时任务并记录结果, 失败不影响其他定时任务。执行权已在调用前抢占。
func runOne(sub *dao.ScheduleRecord) {
	status := "ok"
	if err := Execute(sub); err != nil {
		status = err.Error()
		// 条件未命中 / 静默期内是正常跳过, 不算错误。
		if !errors.Is(err, ErrNotTriggered) && !errors.Is(err, ErrSilenced) {
			xlog.Error("schedule execute failed",
				xlog.Int("id", sub.Id), xlog.String("err", status))
		}
	}
	if err := dao.FinishScheduleRun(sub.Id, status); err != nil {
		xlog.Error("schedule finish failed", xlog.Int("id", sub.Id), xlog.String("err", err.Error()))
	}
}

// dueAt 判断 cron 表达式是否在给定的整分钟触发。
// 解析失败的表达式视为不触发 (避免错误配置导致每分钟报错)。
func dueAt(spec string, minute time.Time) bool {
	sched, err := cronParser.Parse(spec)
	if err != nil {
		return false
	}
	// 从"上一分钟的末尾"求下一次触发, 若正好落在本分钟则到期。
	next := sched.Next(minute.Add(-time.Second))
	return next.Truncate(time.Minute).Equal(minute)
}
