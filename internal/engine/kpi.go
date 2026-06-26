package engine

import (
	"encoding/json"
	"strings"
)

// KPI 卡片区块: 与 @chart 平行, 复用 SQL 区块产出的 rows。
//
//	-- @kpi={"items":[{"label":"GMV","value":"销售额","compare":"上期","format":"money","trend":"销售额"}]}
//
// Value/Compare/Trend 均为列名, 引擎不预算数值——下发配置, 前端按列名从 rows 自取并渲染。

// parseKpiConfig 解析 @kpi 注解为 KpiConfig。
// 支持注解直接是 {items:[...]} 对象, 或裸数组 [...] (视为 items)。
// items 缺失或 value 为空的卡片被跳过; 无有效卡片时返回 nil。
func parseKpiConfig(v any) *KpiConfig {
	var cfg KpiConfig
	switch val := v.(type) {
	case map[string]any:
		b, _ := json.Marshal(val)
		if json.Unmarshal(b, &cfg) != nil {
			return nil
		}
	case []any:
		b, _ := json.Marshal(val)
		if json.Unmarshal(b, &cfg.Items) != nil {
			return nil
		}
	case string:
		s := strings.TrimSpace(val)
		if s == "" {
			return nil
		}
		if strings.HasPrefix(s, "[") {
			if json.Unmarshal([]byte(s), &cfg.Items) != nil {
				return nil
			}
		} else if json.Unmarshal([]byte(s), &cfg) != nil {
			return nil
		}
	default:
		return nil
	}

	items := cfg.Items[:0]
	for _, it := range cfg.Items {
		if strings.TrimSpace(it.Value) == "" {
			continue // value 是必填取数列, 缺则跳过该卡片
		}
		if it.Label == "" {
			it.Label = it.Value
		}
		items = append(items, it)
	}
	if len(items) == 0 {
		return nil
	}
	cfg.Items = items
	return &cfg
}
