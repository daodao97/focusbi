package job

import (
	"xproxy/internal/schedule"

	"github.com/daodao97/xgo/xapp"
	"github.com/daodao97/xgo/xcron"
	"github.com/daodao97/xgo/xredis"
)

func NewCronServer() xapp.NewServer {
	return func() xapp.Server {
		return xcron.New2(
			xcron.WithJobs(
				// 报表定时任务: 每分钟扫描启用的任务, 到期则跑报表并推送 (飞书/企微)。
				xcron.Job{
					Name:           "ReportScheduleTick",
					Spec:           "0 * * * * *", // xcron 含秒, 6 段; 每分钟第 0 秒
					Func:           schedule.Tick,
					EnableDistLock: true, // 多实例只执行一次
				},
			),
			xcron.WithRdb(xredis.Get()),
		)
	}
}
