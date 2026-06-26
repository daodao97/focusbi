package datasource

import (
	"context"
	"time"
)

// contextTimeout 返回带 30s 超时的 context, 用于约束单次查询时长。
func contextTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}
