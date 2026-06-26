package engine

import (
	"encoding/json"
	"strings"

	"github.com/spf13/cast"
)

// 行列转置 (移植自 dataddy plugin_flip):
// 把"少列多行"的表转成"多列少行"便于横向对比。配置:
//
//	-- @flip={"key":"product"}
//
// key 指定保留为行标签的列 (逗号分隔多列); 其余每列转成一行,
// 新表首列 "名称" 为原列名, 数据列名为 key 值 (多 key 用 "k[v]" 拼接)。
// 行数上限 50 (超出报错放弃)。转置后合计/平均无意义, 调用方据返回的 ok 跳过。

const flipMaxRows = 50

type flipConfig struct {
	Key string `json:"key"`
}

// parseFlipConfig 解析 @flip 注解。注解为 true (裸 -- @flip) 视为无 key 全转置。
func parseFlipConfig(v any) (cfg flipConfig, ok bool) {
	switch val := v.(type) {
	case bool:
		return flipConfig{}, val
	case map[string]any:
		b, _ := json.Marshal(val)
		_ = json.Unmarshal(b, &cfg)
		return cfg, true
	case string:
		s := strings.TrimSpace(val)
		if s == "" {
			return flipConfig{}, true
		}
		if json.Unmarshal([]byte(s), &cfg) == nil {
			return cfg, true
		}
	}
	return cfg, false
}

// applyFlip 转置表; 返回新列、新行与是否成功。失败 (超限/无列) 时返回原表与 false。
func applyFlip(cfg flipConfig, cols []string, rows []map[string]any) (newCols []string, newRows []map[string]any, ok bool) {
	if len(rows) == 0 || len(rows) > flipMaxRows {
		return cols, rows, false
	}

	var keyCols []string
	for _, k := range strings.Split(cfg.Key, ",") {
		if k = strings.TrimSpace(k); k != "" && contains(cols, k) {
			keyCols = append(keyCols, k)
		}
	}
	keySet := map[string]bool{}
	for _, k := range keyCols {
		keySet[k] = true
	}

	// 待转置的数据列 (排除 key 列), 保持原顺序。
	var dataCols []string
	for _, c := range cols {
		if !keySet[c] {
			dataCols = append(dataCols, c)
		}
	}
	if len(dataCols) == 0 {
		return cols, rows, false
	}

	// 新列: "名称" + 每行对应的列名 (按行顺序)。
	const nameCol = "名称"
	newCols = []string{nameCol}
	colKeys := make([]string, len(rows))
	seen := map[string]int{}
	for i, row := range rows {
		colKeys[i] = flipColKey(row, keyCols, i)
		// 重名兜底: 追加序号保证列唯一
		if n, dup := seen[colKeys[i]]; dup {
			seen[colKeys[i]] = n + 1
			colKeys[i] = colKeys[i] + "." + cast.ToString(n+1)
		} else {
			seen[colKeys[i]] = 1
		}
		newCols = append(newCols, colKeys[i])
	}

	// 每个数据列 -> 一行。
	for _, dc := range dataCols {
		nr := map[string]any{nameCol: dc}
		for i, row := range rows {
			nr[colKeys[i]] = row[dc]
		}
		newRows = append(newRows, nr)
	}
	return newCols, newRows, true
}

// flipColKey 生成转置后某数据列的列名。无 key 时用 "列N"。
func flipColKey(row map[string]any, keyCols []string, idx int) string {
	if len(keyCols) == 0 {
		return "列" + cast.ToString(idx+1)
	}
	parts := make([]string, len(keyCols))
	for i, k := range keyCols {
		parts[i] = k + "[" + cast.ToString(row[k]) + "]"
	}
	return strings.Join(parts, ",")
}
