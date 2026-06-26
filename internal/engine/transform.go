package engine

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cast"
)

// applyCellTransforms 依据列配置对每一行的单元格值做转换 (移植自 dataddy):
//   - enum:     "1:成功,0:失败"  把原始值映射为标签
//   - ratio:    数值 / ratio 后追加 "%" (如 ratio=1 表示值本身是百分比)
//   - round:    保留 N 位小数
//   - date:     把日期/时间字符串按 PHP 风格格式重排 (如 "Y-m-d")
//   - time2str: 把 Unix 时间戳 (秒) 格式化为日期字符串
//
// 转换就地修改 rows。各转换互斥, 按上述优先级取第一个命中的配置。
func applyCellTransforms(cols []Column, rows []map[string]any) {
	for _, col := range cols {
		if col.Config == nil {
			continue
		}
		enumMap := parseEnumConfig(col.Config["enum"])
		ratio, hasRatio := numConfig(col.Config["ratio"])
		round, hasRound := intConfig(col.Config["round"])
		dateFmt, hasDate := strConfig(col.Config["date"])
		tsFmt, hasTime2str := strConfig(col.Config["time2str"])

		if enumMap == nil && !hasRatio && !hasRound && !hasDate && !hasTime2str {
			continue
		}
		for _, row := range rows {
			v, ok := row[col.Name]
			if !ok || v == nil {
				continue
			}
			switch {
			case enumMap != nil:
				if label, ok := enumMap[cast.ToString(v)]; ok {
					row[col.Name] = label
				}
			case hasDate:
				row[col.Name] = formatDateCell(cast.ToString(v), dateFmt)
			case hasTime2str:
				row[col.Name] = time2str(v, tsFmt)
			case hasRatio && ratio != 0:
				row[col.Name] = formatFloat(cast.ToFloat64(v)/ratio*100, 2) + "%"
			case hasRound:
				row[col.Name] = formatFloat(cast.ToFloat64(v), round)
			}
		}
	}
}

// formatDateCell 把日期/时间字符串解析后按 PHP 风格格式重排; 无法解析则原样返回。
func formatDateCell(s, format string) string {
	if format == "" {
		format = "Y-m-d"
	}
	t, ok := parseDateValue(s)
	if !ok {
		return s
	}
	return formatDate(t, format)
}

// time2str 把 Unix 时间戳 (秒) 格式化为日期字符串; 非数值原样返回。
func time2str(v any, format string) any {
	if format == "" {
		format = "Y-m-d H:i:s"
	}
	sec, err := toFloat(v)
	if err != nil {
		return v
	}
	return formatDate(time.Unix(int64(sec), 0).In(time.Local), format)
}

// strConfig 取字符串型配置; 非字符串或空返回 ("", false)。
func strConfig(v any) (string, bool) {
	s, ok := v.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return "", false
	}
	return s, true
}

// computeSummary 计算合计/平均行。仅对配置了 count=true 的数值列累加。
// sum / avg 为是否启用; 返回的 map 以列名为键。
func computeSummary(cols []Column, rows []map[string]any, wantSum, wantAvg bool) (sum, avg map[string]any) {
	if (!wantSum && !wantAvg) || len(rows) == 0 {
		return nil, nil
	}

	// 找出参与汇总的列:
	//   - 显式 count=true 的列; 若没有任何列声明 count, 则默认对全部数值列汇总
	//   - 标了 nosum=true 的列始终排除 (如比率/百分比/已聚合列)
	countCols := map[string]bool{}
	noSumCols := map[string]bool{}
	anyExplicit := false
	for _, col := range cols {
		if col.Config == nil {
			continue
		}
		if cast.ToBool(col.Config["nosum"]) {
			noSumCols[col.Name] = true
		}
		if cast.ToBool(col.Config["count"]) {
			countCols[col.Name] = true
			anyExplicit = true
		}
	}

	totals := map[string]float64{}
	counts := map[string]int{}
	for _, row := range rows {
		for _, col := range cols {
			if noSumCols[col.Name] {
				continue // 显式排除
			}
			if anyExplicit && !countCols[col.Name] {
				continue
			}
			v, ok := row[col.Name]
			if !ok || v == nil {
				continue
			}
			f, err := toFloat(v)
			if err != nil {
				continue // 非数值列跳过
			}
			totals[col.Name] += f
			counts[col.Name]++
		}
	}
	if len(totals) == 0 {
		return nil, nil
	}

	if wantSum {
		sum = map[string]any{}
		for name, t := range totals {
			sum[name] = trimFloat(t)
		}
		labelFirstCol(cols, sum, totals, "合计")
	}
	if wantAvg {
		avg = map[string]any{}
		for name, t := range totals {
			if counts[name] > 0 {
				avg[name] = formatFloat(t/float64(counts[name]), 2)
			}
		}
		labelFirstCol(cols, avg, totals, "平均")
	}
	return sum, avg
}

// labelFirstCol 在汇总行的第一列 (若它不参与求和) 放置标签 "合计"/"平均"。
func labelFirstCol(cols []Column, row map[string]any, totals map[string]float64, label string) {
	if len(cols) == 0 {
		return
	}
	first := cols[0].Name
	if _, summed := totals[first]; !summed {
		row[first] = label
	}
}

// parseEnumConfig 解析 "1:成功,0:失败" 为 map。
func parseEnumConfig(v any) map[string]string {
	s, ok := v.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return nil
	}
	m := map[string]string{}
	for _, kv := range strings.Split(s, ",") {
		seg := strings.SplitN(kv, ":", 2)
		if len(seg) == 2 {
			m[strings.TrimSpace(seg[0])] = strings.TrimSpace(seg[1])
		}
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

func numConfig(v any) (float64, bool) {
	if v == nil {
		return 0, false
	}
	return cast.ToFloat64(v), true
}

func intConfig(v any) (int, bool) {
	if v == nil {
		return 0, false
	}
	return cast.ToInt(v), true
}

// toFloat 把单元格值转为 float64; 非数值返回错误。
func toFloat(v any) (float64, error) {
	switch val := v.(type) {
	case float64:
		return val, nil
	case float32:
		return float64(val), nil
	case int, int64, int32:
		return cast.ToFloat64(val), nil
	case string:
		return strconv.ParseFloat(strings.TrimSpace(val), 64)
	default:
		return strconv.ParseFloat(cast.ToString(v), 64)
	}
}

func formatFloat(f float64, prec int) string {
	return strconv.FormatFloat(f, 'f', prec, 64)
}

// trimFloat 整数则去掉小数, 否则保留 2 位。
func trimFloat(f float64) string {
	if f == float64(int64(f)) {
		return fmt.Sprintf("%d", int64(f))
	}
	return formatFloat(f, 2)
}
