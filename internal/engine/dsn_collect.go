package engine

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// CollectDSNs statically collects datasource names referenced by a template.
// It does not execute SQL, script query(), or fetch(); dynamic script datasource
// expressions are intentionally not collected and will be constrained at runtime
// by the approved allowlist.
func CollectDSNs(defaultDSN, content string) []string {
	if strings.TrimSpace(defaultDSN) == "" {
		defaultDSN = "default"
	}
	seen := map[string]bool{}
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			name = defaultDSN
		}
		seen[name] = true
	}
	add(defaultDSN)

	filters, cleaned := parseFilters(content)
	for _, f := range filters {
		if f.optionSQL != "" {
			add(f.optionDSN)
		}
	}
	for _, rawText := range splitBlocks(cleaned) {
		if strings.TrimSpace(rawText) == "" {
			continue
		}
		rb := parseBlock(rawText)
		switch rb.kind {
		case "sql":
			add(annotationString(rb, "dsn"))
		case "script":
			for _, dsn := range collectScriptDSNs(stripMarker(rb.body)) {
				add(dsn)
			}
		}
	}

	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

var (
	scriptDSNOptionRes = []*regexp.Regexp{
		regexp.MustCompile(`(?is)\bdsn\s*:\s*'([^']+)'`),
		regexp.MustCompile(`(?is)\bdsn\s*:\s*"([^"]+)"`),
		regexp.MustCompile("(?is)\\bdsn\\s*:\\s*`([^`]+)`"),
	}
	scriptQueryDSNRes = []*regexp.Regexp{
		regexp.MustCompile(`(?is)\bquery\s*\([^)]*,[^)]*,\s*'([^']+)'`),
		regexp.MustCompile(`(?is)\bquery\s*\([^)]*,[^)]*,\s*"([^"]+)"`),
		regexp.MustCompile("(?is)\\bquery\\s*\\([^)]*,[^)]*,\\s*`([^`]+)`"),
	}
)

func collectScriptDSNs(code string) []string {
	seen := map[string]bool{}
	for _, re := range scriptDSNOptionRes {
		for _, m := range re.FindAllStringSubmatch(code, -1) {
			if len(m) >= 2 {
				seen[strings.TrimSpace(m[1])] = true
			}
		}
	}
	for _, re := range scriptQueryDSNRes {
		for _, m := range re.FindAllStringSubmatch(code, -1) {
			if len(m) >= 2 {
				seen[strings.TrimSpace(m[1])] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		if name != "" {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// AllowlistAuthz returns an authz closure that permits only approved datasource
// names. Empty approved lists fail closed.
func AllowlistAuthz(approved []string) func(string) error {
	allowed := map[string]bool{}
	for _, name := range approved {
		name = strings.TrimSpace(name)
		if name == "" {
			name = "default"
		}
		allowed[name] = true
	}
	return func(name string) error {
		name = strings.TrimSpace(name)
		if name == "" {
			name = "default"
		}
		if allowed[name] {
			return nil
		}
		return fmt.Errorf("数据源 %q 未获公开/调度预授权", name)
	}
}
