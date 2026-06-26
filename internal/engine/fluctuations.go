package engine

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cast"
)

// 数据波动检测 (移植自 dataddy plugin_data_fluctuations):
// 对时序表按第一列 (通常是日期) 降序取最近两期, 对配置字段算环比波动,
// 绝对值超过 threshold_percent 的字段汇总成一条波动消息, 写入 block.Messages。
// 这些消息随后由订阅链路读取并推送 (见 internal/subscription)。
//
//	-- @data_fluctuations={"field":["consume","orders"],"threshold_percent":50}
//	-- @data_fluctuations={"field":"consume"}            // threshold 缺省 50
//
// 约定: 表至少两行才比较; 第一列作为期次标识 (日期), 降序后 rows[0] 为最新期。

const defaultFluctuationThreshold = 50.0

// parseFluctuationsConfig 解析 @data_fluctuations 注解。
// 返回字段列表与阈值; ok=false 表示未配置或字段为空。
func parseFluctuationsConfig(v any) (fields []string, threshold float64, ok bool) {
	m, isMap := v.(map[string]any)
	if !isMap {
		return nil, 0, false
	}
	switch f := m["field"].(type) {
	case string:
		if f != "" {
			fields = []string{f}
		}
	case []any:
		for _, item := range f {
			if s := cast.ToString(item); s != "" {
				fields = append(fields, s)
			}
		}
	}
	if len(fields) == 0 {
		return nil, 0, false
	}
	threshold = defaultFluctuationThreshold
	if t, has := m["threshold_percent"]; has {
		if tf, err := toFloat(t); err == nil {
			threshold = tf
		}
	}
	return fields, threshold, true
}

// detectFluctuations 计算最近两期波动, 返回波动消息 (可能为空)。
// cols[0] 作为期次列 (日期), 按其降序排序后取前两行比较。
func detectFluctuations(cols []string, rows []map[string]any, fields []string, threshold float64) []string {
	if len(cols) == 0 || len(rows) < 2 {
		return nil
	}
	dateKey := cols[0]

	// 降序: 最新期在前。复制一份索引排序, 不改动原 rows 顺序。
	ordered := make([]map[string]any, len(rows))
	copy(ordered, rows)
	sort.SliceStable(ordered, func(a, b int) bool {
		return compareCell(ordered[a][dateKey], ordered[b][dateKey]) > 0
	})

	latest, prev := ordered[0], ordered[1]

	var parts []string
	for _, field := range fields {
		cur, _ := toFloat(latest[field])
		old, _ := toFloat(prev[field])
		// 对齐 dataddy: 最新期为 0 (或缺失) 视为数据缺失。
		if _, exists := latest[field]; !exists || cur == 0 {
			parts = append(parts, fmt.Sprintf("%s: 数据缺失", field))
			continue
		}
		var diff float64
		if old == 0 {
			diff = 1 // 上期为 0, 视为 +100%
		} else {
			diff = (cur - old) / old
		}
		diffPercent := int(diff * 100)
		if diffPercent < 0 {
			diffPercent = -diffPercent
		}
		if float64(diffPercent) > threshold {
			sign := "+"
			if diff < 0 {
				sign = "-"
			}
			parts = append(parts, fmt.Sprintf("%s: %s => %s [%s%d%%]",
				field, cast.ToString(prev[field]), cast.ToString(latest[field]), sign, diffPercent))
		}
	}

	if len(parts) == 0 {
		return nil
	}
	msg := fmt.Sprintf("%s 数据相较 %s 浮动: %s",
		cast.ToString(latest[dateKey]), cast.ToString(prev[dateKey]), strings.Join(parts, ", "))
	return []string{msg}
}
