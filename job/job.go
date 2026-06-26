package job

import (
	"xproxy/internal/subscription"

	"github.com/daodao97/xgo/xapp"
	"github.com/daodao97/xgo/xcron"
	"github.com/daodao97/xgo/xredis"
)

func NewCronServer() xapp.NewServer {
	return func() xapp.Server {
		return xcron.New2(
			xcron.WithJobs(
				// 报表定时订阅: 每分钟扫描启用的订阅, 到期则跑报表并推送 (飞书/企微)。
				xcron.Job{
					Name:           "ReportSubscriptionTick",
					Spec:           "0 * * * * *", // xcron 含秒, 6 段; 每分钟第 0 秒
					Func:           subscription.Tick,
					EnableDistLock: true, // 多实例只执行一次
				},
			),
			xcron.WithRdb(xredis.Get()),
		)
	}
}
