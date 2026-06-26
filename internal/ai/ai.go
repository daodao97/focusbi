// Package ai 提供对话式报表修改能力。
//
// 交互为多轮对话: 带历史指令与历次产出的模板; AI 以 SEARCH/REPLACE 块返回局部修改
// (见 diff.go), 引擎应用到当前模板; 失配则回退整模板重写。
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"xproxy/conf"
	"xproxy/docs"
)

// Message 是一条对话消息 (传给 provider)。
type Message struct {
	Role    string // user / assistant
	Content string
}

// Turn 是前端传来的一轮历史: 用户指令 + 该轮 AI 产出的模板。
type Turn struct {
	Instruction string `json:"instruction"`
	Template    string `json:"template"`
}

// Proposal 是 AI 生成的待确认修改。
type Proposal struct {
	Content  string `json:"content"`
	Patch    string `json:"patch"`
	Raw      string `json:"raw,omitempty"`
	Applied  int    `json:"applied"`
	ToolCall bool   `json:"tool_call"`
}

// systemPrompt 内嵌完整的报表模板语法文档 (docs/SYNTAX.md) 作为权威依据,
// 与前端文档按钮展示的是同一份, 保证 AI 生成与人工编写遵循一致的规则。
// 协议: 要求 AI 以 SEARCH/REPLACE 块返回局部修改, 而非整模板。
var systemPrompt = "你是一个报表模板助手。下面是【报表模板语法】的完整说明, 你必须严格按它生成或修改模板:\n\n" +
	docs.SyntaxMarkdown +
	"\n\n---\n\n" + diffProtocol

// diffProtocol 说明 AI 应如何以 SEARCH/REPLACE 块回复。
// 标记行通过拼接生成, 避免 git diff --check 把示例误判为冲突标记。
var diffProtocol = "修改模板时, 只返回需要改动的部分, 用如下 SEARCH/REPLACE 块格式 (可多个):\n\n" +
	markSearch + "\n" +
	"要被替换的原文片段 (必须与当前模板逐字一致)\n" +
	markSep + "\n" +
	"替换后的新内容\n" +
	markReplace + "\n\n" +
	"规则:\n" +
	"- SEARCH 片段必须是当前模板里真实存在的、足够唯一的连续文本 (逐字, 含缩进)。\n" +
	"- 新增内容: SEARCH 留空 (===== 上方为空), REPLACE 为要追加的新区块, 会加到模板末尾。\n" +
	"- 输出顺序必须是: 先输出一个普通文本块, 用一两句话说明将修改哪些内容; 然后调用 propose_template_patch 工具提交 patch。\n" +
	"- 普通文本块会流式展示给用户, 必须存在; 不要直接从工具调用开始。\n" +
	"- 最终必须通过工具 propose_template_patch 的 patch 参数提交上述 SEARCH/REPLACE 或 FULL 内容。\n" +
	"- 不要用 markdown 代码块包裹 patch; 工具参数里只放 SEARCH/REPLACE 或 FULL 内容, 不放说明文字。\n" +
	"- 仅当改动过大、难以用 diff 表达时, 才用整模板兜底: 输出\n" +
	"  " + markFull + "\n" +
	"  完整新模板\n" +
	"  " + markFullEnd

const requestTimeout = 90 * time.Second

// ErrNotConfigured 表示未配置 AI 服务。
var ErrNotConfigured = fmt.Errorf("AI 服务未配置 (请在 conf.ai 设置 base_url 与 api_key)")

// ModifyTemplate 根据对话历史与本轮指令修改报表模板, 返回新的完整模板内容。
//   - history: 之前各轮 (指令 + 当时产出的模板), 提供多轮连续性。
//   - current: 当前模板 (本轮的修改基准)。
//   - instruction: 本轮指令。
//   - schema: 可选的数据源/表结构上下文。
func ModifyTemplate(ctx context.Context, history []Turn, current, instruction, schema string) (string, error) {
	proposal, err := ProposeTemplate(ctx, history, current, instruction, schema, nil)
	if err != nil {
		return "", err
	}
	return proposal.Content, nil
}

// ProposeTemplate 根据对话历史与本轮指令生成待确认修改。
func ProposeTemplate(ctx context.Context, history []Turn, current, instruction, schema string, onDelta func(string) error) (*Proposal, error) {
	c := conf.Get().AI
	if c.APIKey == "" || c.BaseURL == "" {
		return nil, ErrNotConfigured
	}

	messages := buildMessages(history, current, instruction, schema)

	out, toolCalls, err := callLLM(ctx, c, systemPrompt, messages, onDelta)
	if err != nil {
		return nil, err
	}

	patch, toolCall := patchFromToolCalls(toolCalls)
	if patch == "" {
		patch = out
	}

	// 解析 AI 输出: 整模板兜底通道优先; 否则按 SEARCH/REPLACE 应用到当前模板。
	blocks, full, ok := parseDiff(patch)
	if !ok {
		return nil, fmt.Errorf("AI 未按 diff/tool 协议返回修改内容")
	}
	if full != "" {
		return &Proposal{Content: full, Patch: patch, Raw: out, ToolCall: toolCall}, nil
	}
	result, applied := applyDiff(current, blocks)
	if applied == 0 {
		// 所有 SEARCH 都失配: 回退当前模板 (不报错, 保证有可用输出)
		return &Proposal{Content: current, Patch: patch, Raw: out, Applied: applied, ToolCall: toolCall}, nil
	}
	return &Proposal{Content: result, Patch: patch, Raw: out, Applied: applied, ToolCall: toolCall}, nil
}

// buildMessages 把历史与本轮组装成 provider 的 messages 列表。
func buildMessages(history []Turn, current, instruction, schema string) []Message {
	var msgs []Message
	for _, t := range history {
		if strings.TrimSpace(t.Instruction) == "" {
			continue
		}
		msgs = append(msgs, Message{Role: "user", Content: t.Instruction})
		if strings.TrimSpace(t.Template) != "" {
			// 历史的 assistant 内容用当时模板表示该轮结果, 给模型连续上下文。
			msgs = append(msgs, Message{Role: "assistant", Content: "已更新, 当时模板:\n```\n" + t.Template + "\n```"})
		}
	}

	var sb strings.Builder
	if strings.TrimSpace(schema) != "" {
		fmt.Fprintf(&sb, "可用的数据源表结构 (供参考, 请使用其中真实的表名与字段名):\n%s\n\n", schema)
	}
	fmt.Fprintf(&sb, "当前模板:\n```\n%s\n```\n\n修改要求: %s", current, instruction)
	msgs = append(msgs, Message{Role: "user", Content: sb.String()})
	return msgs
}

var fenceRe = regexp.MustCompile("(?s)^```[a-zA-Z]*\\n(.*)\\n```$")

// stripCodeFence 去掉模型可能附加的 ``` 代码块包裹。
func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if m := fenceRe.FindStringSubmatch(s); m != nil {
		return strings.TrimSpace(m[1])
	}
	return s
}

func patchFromToolCalls(calls []ToolCall) (string, bool) {
	for _, call := range calls {
		if call.Name != proposePatchToolName {
			continue
		}
		var args struct {
			Patch string `json:"patch"`
		}
		if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
			continue
		}
		if strings.TrimSpace(args.Patch) != "" {
			return args.Patch, true
		}
	}
	return "", false
}
