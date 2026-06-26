package subscription

import (
	"errors"
	"time"

	"xproxy/dao"

	"github.com/daodao97/xgo/xlog"
	"github.com/robfig/cron/v3"
)

// nowFunc 便于测试覆写时间源。
var nowFunc = time.Now

// 5 段标准 cron 解析器 (分 时 日 月 周, 不含秒)。
var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// Tick 每分钟调度一次: 扫描所有启用订阅, 到期的执行并推送。
// 由 job 注册到 xcron (每分钟触发, 带分布式锁)。
func Tick() {
	minute := nowFunc().Truncate(time.Minute)
	subs, err := dao.ListEnabledSubscriptions()
	if err != nil {
		xlog.Error("subscription tick: list failed", xlog.String("err", err.Error()))
		return
	}
	for _, sub := range subs {
		if !dueAt(sub.Cron, minute) {
			continue
		}
		// 原子抢占本分钟执行权: 仅抢到的实例执行, 杜绝多实例/重入重复推送。
		claimed, err := dao.ClaimSubscriptionRun(sub.Id, minute)
		if err != nil {
			xlog.Error("subscription claim failed", xlog.Int("id", sub.Id), xlog.String("err", err.Error()))
			continue
		}
		if !claimed {
			continue // 已被其他实例抢占
		}
		runOne(sub)
	}
}

// runOne 执行单条订阅并记录结果, 失败不影响其他订阅。执行权已在调用前抢占。
func runOne(sub *dao.SubscriptionRecord) {
	status := "ok"
	if err := Execute(sub); err != nil {
		status = err.Error()
		// 条件未命中是正常跳过, 不算错误。
		if !errors.Is(err, ErrNotTriggered) {
			xlog.Error("subscription execute failed",
				xlog.Int("id", sub.Id), xlog.String("err", status))
		}
	}
	if err := dao.FinishSubscriptionRun(sub.Id, status); err != nil {
		xlog.Error("subscription finish failed", xlog.Int("id", sub.Id), xlog.String("err", err.Error()))
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
