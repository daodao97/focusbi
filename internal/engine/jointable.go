package engine

import (
	"strings"

	"github.com/spf13/cast"
)

// 块间合并 (移植自 dataddy Data_ArrayTable + joinReport):
// 连续若干 SQL 块标注 @join / @union 时, 它们的结果被合并进同一张表,
// 只产出一个合并后的区块。第一个块作为基底, 其后带 @join/@union 的块依次并入。
//
//	-- @join=day,channel          // 按 day,channel 左连接到基底 (缺 on 列时按列交集)
//	-- @join={"on":"day","full":true}  // 全连接 (并入右表未匹配行)
//	-- @union                     // 纵向并入 (列取并集, 缺列补空)
//
// 语义对齐 dataddy: @join/@union 标在"被并入的块"上, 展示注解 (chart/sum/title)
// 取自基底块。

// arrayTable 是一张可增量 join/union 的内存表, 保留列顺序。
type arrayTable struct {
	cols []string
	rows []map[string]any
}

func newArrayTable(cols []string, rows []map[string]any) *arrayTable {
	t := &arrayTable{cols: append([]string(nil), cols...)}
	for _, r := range rows {
		t.rows = append(t.rows, cloneRow(r))
	}
	return t
}

func cloneRow(r map[string]any) map[string]any {
	out := make(map[string]any, len(r))
	for k, v := range r {
		out[k] = v
	}
	return out
}

func (t *arrayTable) hasCol(name string) bool {
	for _, c := range t.cols {
		if c == name {
			return true
		}
	}
	return false
}

// union 纵向并入 dataset: 列取并集 (基底缺的列补空), 追加全部行。
func (t *arrayTable) union(cols []string, rows []map[string]any) {
	if len(rows) == 0 {
		return
	}
	// 右表多出的列, 先给基底所有行补空。
	for _, c := range cols {
		if !t.hasCol(c) {
			t.cols = append(t.cols, c)
			for _, r := range t.rows {
				if _, ok := r[c]; !ok {
					r[c] = ""
				}
			}
		}
	}
	// 追加右表行, 按合并后的列集补齐缺列。
	for _, r := range rows {
		nr := make(map[string]any, len(t.cols))
		for _, c := range t.cols {
			if v, ok := r[c]; ok {
				nr[c] = v
			} else {
				nr[c] = ""
			}
		}
		t.rows = append(t.rows, nr)
	}
}

// join 按 onKeys 把 dataset 的非 key 列并入基底 (左连接)。
// onKeys 为空时取两表列交集为连接键。full=true 时把右表未匹配行也并入。
func (t *arrayTable) join(cols []string, rows []map[string]any, onKeys []string, full bool) {
	if len(rows) == 0 {
		return
	}
	if len(onKeys) == 0 {
		// 列交集作为连接键 (对齐 dataddy)。
		for _, c := range cols {
			if t.hasCol(c) {
				onKeys = append(onKeys, c)
			}
		}
	}
	if len(onKeys) == 0 {
		return
	}

	// 右表非 key 列 = 要并进来的新列。
	keySet := map[string]bool{}
	for _, k := range onKeys {
		keySet[k] = true
	}
	var extra []string
	for _, c := range cols {
		if !keySet[c] {
			extra = append(extra, c)
		}
	}

	// 右表按连接键建索引。
	index := make(map[string]map[string]any, len(rows))
	for _, r := range rows {
		index[rowKey(r, onKeys)] = r
	}

	// 基底新增列。
	for _, c := range extra {
		if !t.hasCol(c) {
			t.cols = append(t.cols, c)
		}
	}

	// 左表逐行补右表非 key 列。
	matched := map[string]bool{}
	for _, r := range t.rows {
		k := rowKey(r, onKeys)
		right := index[k]
		matched[k] = true
		for _, c := range extra {
			if right != nil {
				r[c] = right[c]
			} else if _, ok := r[c]; !ok {
				r[c] = ""
			}
		}
	}

	// 全连接: 右表未匹配行补进来 (key 列取右表值, 基底独有列补空)。
	if full {
		for _, r := range rows {
			k := rowKey(r, onKeys)
			if matched[k] {
				continue
			}
			matched[k] = true
			nr := make(map[string]any, len(t.cols))
			for _, c := range t.cols {
				if v, ok := r[c]; ok {
					nr[c] = v
				} else {
					nr[c] = ""
				}
			}
			t.rows = append(t.rows, nr)
		}
	}
}

// rowKey 用 onKeys 各列值拼接成行键 (缺列以空串参与)。
func rowKey(r map[string]any, keys []string) string {
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = cast.ToString(r[k])
	}
	return strings.Join(parts, "\x00")
}

// joinSpec 描述一个块的合并方式 (解析自 @join / @union 注解)。
type joinSpec struct {
	isUnion bool
	onKeys  []string
	full    bool
}

// parseJoinConfig 解析 @join / @union 注解。
// rb.annotations 里 join/union 任一存在即参与合并; union 优先。
// 返回 ok=false 表示该块不参与合并。
func parseJoinConfig(annotations map[string]any) (joinSpec, bool) {
	if v, has := annotations["union"]; has {
		spec := joinSpec{isUnion: true}
		// @union={"on":...} 这类对象目前 union 不需要参数, 忽略其值。
		_ = v
		return spec, true
	}
	v, has := annotations["join"]
	if !has {
		return joinSpec{}, false
	}
	spec := joinSpec{}
	switch cfg := v.(type) {
	case string:
		spec.onKeys = splitKeys(cfg)
	case bool:
		// @join (无值): 列交集连接。
	case map[string]any:
		if on, ok := cfg["on"]; ok {
			switch o := on.(type) {
			case string:
				spec.onKeys = splitKeys(o)
			case []any:
				for _, k := range o {
					if s := strings.TrimSpace(cast.ToString(k)); s != "" {
						spec.onKeys = append(spec.onKeys, s)
					}
				}
			}
		}
		spec.full = cast.ToBool(cfg["full"])
	}
	return spec, true
}

// splitKeys 把 "day,channel" 切成列名切片。
func splitKeys(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
