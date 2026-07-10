package datasource

import (
	"context"

	"xproxy/internal/runtimecfg"
)

// contextTimeout 返回带配置超时的 context, 用于约束单次查询时长。
func contextTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), runtimecfg.QueryTimeout())
}
