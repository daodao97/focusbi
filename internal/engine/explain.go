package engine

import (
	"context"
	"strings"

	"xproxy/internal/datasource"
	"xproxy/internal/runtimecfg"
)

// ExplainBlock 是一个声明式 SQL 区块的执行计划结果。
type ExplainBlock struct {
	BlockIndex int              `json:"block_index"`
	BlockID    string           `json:"block_id,omitempty"`
	DSN        string           `json:"dsn"`
	SQL        string           `json:"sql"`
	Columns    []string         `json:"columns,omitempty"`
	Rows       []map[string]any `json:"rows,omitempty"`
	Error      string           `json:"error,omitempty"`
}

// ExplainContext 对模板中的声明式 SQL 区块执行只读 EXPLAIN。脚本、markdown 和 raw
// 区块不会执行；每个 SQL 区块独立返回错误，便于编辑器一次展示完整检查结果。
func (r *Runner) ExplainContext(parent context.Context, content string, params map[string]string) ([]ExplainBlock, error) {
	ctx, cancel := context.WithTimeout(parent, runtimecfg.ReportTimeout())
	defer cancel()

	filters, cleaned := parseFilters(content)
	resolveDefaults(filters)
	if err := validateFilterDefinitions(filters); err != nil {
		return nil, err
	}
	if err := validateFilterParams(filters, params); err != nil {
		return nil, err
	}
	macros := macroValues(filters, params)

	var plans []ExplainBlock
	blockIndex := 0
	for _, raw := range splitBlocks(cleaned) {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		blockIndex++
		rb := parseBlock(raw)
		if rb.kind != "sql" {
			continue
		}
		sql := strings.TrimSpace(applyMacros(rb.body, macros))
		if sql == "" {
			continue
		}
		dsn := r.blockDSN(rb)
		plan := ExplainBlock{BlockIndex: blockIndex, BlockID: annotationString(rb, "id"), DSN: dsn, SQL: sql}
		if err := validateReadOnlySQL(sql); err != nil {
			plan.Error = err.Error()
			plans = append(plans, plan)
			continue
		}
		if r.authz != nil {
			if err := r.authz(dsn); err != nil {
				plan.Error = err.Error()
				plans = append(plans, plan)
				continue
			}
		}
		result, err := datasource.ExplainContext(ctx, dsn, strings.TrimRight(sql, "; \t\r\n"))
		if err != nil {
			plan.Error = err.Error()
		} else {
			plan.Columns = result.Columns
			plan.Rows = result.Rows
		}
		plans = append(plans, plan)
	}
	return plans, nil
}
