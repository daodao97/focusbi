package engine

import (
	"sort"
	"strings"

	"github.com/spf13/cast"
)

// 服务端结果排序 (移植自 dataddy plugin_sort):
// 配置为字符串, 逗号分隔多个排序项, 按书写顺序作为多级排序键 (前者优先):
//
//	-- @sort=+revenue,-count
//	-- @sort=-amount(dept>branch)
//
// 每项前缀 + 升序 / - 降序 (缺省降序)。括号内为分组字段 (> 分层),
// 分组排序时该项先按"分组聚合权重"比较 (组内数值列对该排序字段求和),
// 权重相同再按字段原值比较 —— 让同组的行聚在一起且组按总量排序。
// 排序稳定: 全部键相等时保持原相对顺序。

type sortItem struct {
	field  string
	asc    bool
	groups []string // 分组字段链 (可空)
}

// parseSortConfig 解析 @sort 注解为排序项列表。返回 nil 表示未配置/无效。
func parseSortConfig(v any) []sortItem {
	s, ok := v.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return nil
	}
	var items []sortItem
	for _, seg := range strings.Split(s, ",") {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		it := sortItem{asc: false}
		switch seg[0] {
		case '+':
			it.asc = true
			seg = seg[1:]
		case '-':
			seg = seg[1:]
		}
		// 分组: field(g1>g2)
		if i := strings.IndexByte(seg, '('); i >= 0 && strings.HasSuffix(seg, ")") {
			inner := seg[i+1 : len(seg)-1]
			it.field = strings.TrimSpace(seg[:i])
			for _, g := range strings.Split(inner, ">") {
				if g = strings.TrimSpace(g); g != "" && g != "$" {
					it.groups = append(it.groups, g)
				}
			}
		} else {
			it.field = strings.TrimSpace(seg)
		}
		if it.field != "" {
			items = append(items, it)
		}
	}
	if len(items) == 0 {
		return nil
	}
	return items
}

// applySort 依据排序项就地稳定排序 rows。
func applySort(items []sortItem, rows []map[string]any) {
	if len(items) == 0 || len(rows) < 2 {
		return
	}
	// 预计算各分组排序项的权重表: 分组键 -> 该排序字段的组内求和。
	weights := make([]map[string]float64, len(items))
	for i, it := range items {
		if len(it.groups) > 0 {
			weights[i] = groupWeights(rows, it)
		}
	}

	sort.SliceStable(rows, func(a, b int) bool {
		for i, it := range items {
			c := compareByItem(rows[a], rows[b], it, weights[i])
			if c == 0 {
				continue
			}
			if it.asc {
				return c < 0
			}
			return c > 0
		}
		return false
	})
}

// groupWeights 计算每个分组键 (按 groups 链拼接) 下 field 的数值求和, 作为该组权重。
func groupWeights(rows []map[string]any, it sortItem) map[string]float64 {
	w := map[string]float64{}
	for _, row := range rows {
		k := groupKey(row, it.groups)
		f, err := toFloat(row[it.field])
		if err == nil {
			w[k] += f
		}
	}
	return w
}

func groupKey(row map[string]any, groups []string) string {
	parts := make([]string, len(groups))
	for i, g := range groups {
		parts[i] = cast.ToString(row[g])
	}
	return strings.Join(parts, "\x00")
}

// compareByItem 按单个排序项比较两行, 返回 <0 / 0 / >0 (升序意义)。
// 分组项: 先比组权重 (大者靠前 -> 在升序意义下返回组权重的反向, 由调用方按 asc 再翻转),
// 这里统一返回"升序比较结果", 让组内总量大的排前需配合 desc 使用。
func compareByItem(ra, rb map[string]any, it sortItem, w map[string]float64) int {
	if len(it.groups) > 0 {
		ka, kb := groupKey(ra, it.groups), groupKey(rb, it.groups)
		if ka != kb {
			if d := cmpFloat(w[ka], w[kb]); d != 0 {
				return d
			}
			// 权重相等但分组不同: 按分组键字符串定序, 保证同组聚拢
			return strings.Compare(ka, kb)
		}
	}
	return compareCell(ra[it.field], rb[it.field])
}

// compareCell 数值优先比较两个单元格值, 否则按字符串。
func compareCell(a, b any) int {
	af, aErr := toFloat(a)
	bf, bErr := toFloat(b)
	if aErr == nil && bErr == nil {
		return cmpFloat(af, bf)
	}
	return strings.Compare(cast.ToString(a), cast.ToString(b))
}

func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
