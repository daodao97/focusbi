package ai

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	claudemodel "github.com/cloudwego/eino-ext/components/model/claude"
	openaimodel "github.com/cloudwego/eino-ext/components/model/openai"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"xproxy/conf"
)

const (
	defaultClaudeModel   = "claude-sonnet-4-6"
	defaultOpenAIModel   = "gpt-4o-mini"
	proposePatchToolName = "propose_template_patch"
)

var streamIntroPrompt = "你是报表模板助手。请根据用户当前模板和修改要求, 只用一两句话说明你准备修改哪些内容。不要输出 patch, 不要调用工具。"

// ToolCall 是模型返回的工具调用。
type ToolCall struct {
	Name      string
	Arguments string
}

// userAgent 覆盖底层 SDK 默认的 "Anthropic/Go x.y.z"。
// 某些自建/代理网关 (如 api.aicoding.sh) 前置 Cloudflare, 其 bot 规则会拦截
// 该默认 UA 返回 403; 改成普通 UA 即可正常通过。
const userAgent = "focusbi/1.0"

// uaTransport 在每个请求上强制改写 User-Agent (set 而非 add, 避免出现两个 UA 头)。
type uaTransport struct{ base http.RoundTripper }

func (t uaTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", userAgent)
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

// httpClient 是 provider 共用的 HTTP 客户端。
var httpClient = &http.Client{
	Timeout:   requestTimeout,
	Transport: uaTransport{base: http.DefaultTransport},
}

// callLLM 通过 Eino 的统一 ChatModel 接口调用模型。
func callLLM(ctx context.Context, c conf.AIConf, system string, msgs []Message, onDelta func(string) error) (string, []ToolCall, error) {
	provider := strings.ToLower(strings.TrimSpace(c.Provider))
	model, client, options, err := newLLMClient(ctx, provider, c)
	if err != nil {
		return "", nil, err
	}

	baseOptions := append(options,
		einomodel.WithMaxTokens(8192),
		einomodel.WithTemperature(0.2),
		einomodel.WithModel(model),
	)

	// 交互上要求先展示自然语言说明, 再展示 tool call 生成的 patch。
	// 因此流式阶段只生成说明, patch 阶段强制使用 tool call, 避免模型直接从工具调用开始。
	if onDelta != nil {
		if err := streamIntro(ctx, client, provider, model, msgs, baseOptions, onDelta); err != nil {
			return "", nil, err
		}
		if err := onDelta("\n\n"); err != nil {
			return "", nil, err
		}
	}

	tool := proposePatchTool()
	if !isOpenAIProvider(provider) {
		tool = claudemodel.SetToolInfoCacheControl(tool, &claudemodel.CacheControl{TTL: claudemodel.CacheTTL1h})
	}
	toolModel, err := client.WithTools([]*schema.ToolInfo{tool})
	if err != nil {
		return "", nil, fmt.Errorf("绑定 AI 工具失败: %w", err)
	}

	lm := toEinoMessages(provider, system, msgs)
	callOptions := append([]einomodel.Option{}, baseOptions...)
	callOptions = append(callOptions, einomodel.WithToolChoice(schema.ToolChoiceForced, proposePatchToolName))
	resp, err := toolModel.Generate(ctx, lm, callOptions...)
	if err != nil {
		return "", nil, err
	}

	return contentAndToolCalls(resp)
}

func streamIntro(ctx context.Context, client einomodel.ToolCallingChatModel, provider, model string, msgs []Message, baseOptions []einomodel.Option, onDelta func(string) error) error {
	lm := toEinoMessages(provider, streamIntroPrompt, msgs)
	options := append([]einomodel.Option{}, baseOptions...)
	options = append(options,
		einomodel.WithMaxTokens(512),
		einomodel.WithModel(model),
	)

	stream, err := client.Stream(ctx, lm, options...)
	if err != nil {
		return fmt.Errorf("生成流式说明失败: %w", err)
	}
	defer stream.Close()

	var chunks []*schema.Message
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("读取流式说明失败: %w", err)
		}
		chunks = append(chunks, chunk)
		if chunk.Content != "" {
			if err := onDelta(chunk.Content); err != nil {
				return err
			}
		}
	}
	if len(chunks) == 0 {
		return fmt.Errorf("AI 未返回说明内容")
	}
	return nil
}

func contentAndToolCalls(resp *schema.Message) (string, []ToolCall, error) {
	if resp == nil {
		return "", nil, fmt.Errorf("AI 未返回内容")
	}
	toolCalls := make([]ToolCall, 0, len(resp.ToolCalls))
	for _, call := range resp.ToolCalls {
		toolCalls = append(toolCalls, ToolCall{
			Name:      call.Function.Name,
			Arguments: call.Function.Arguments,
		})
	}
	if strings.TrimSpace(resp.Content) == "" && len(toolCalls) == 0 {
		return "", nil, fmt.Errorf("AI 未返回内容")
	}
	return resp.Content, toolCalls, nil
}

func proposePatchTool() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: proposePatchToolName,
		Desc: "Return the report template modification as SEARCH/REPLACE blocks or one FULL replacement block. This tool only proposes a patch; the application will ask the user before applying it.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"patch": {
				Type:     schema.String,
				Desc:     "SEARCH/REPLACE blocks using the exact protocol from the system prompt, or a <<<FULL>>> complete template block.",
				Required: true,
			},
		}),
	}
}

func newLLMClient(ctx context.Context, provider string, c conf.AIConf) (string, einomodel.ToolCallingChatModel, []einomodel.Option, error) {
	if isOpenAIProvider(provider) {
		model := c.Model
		if model == "" {
			model = defaultOpenAIModel
		}
		client, err := openaimodel.NewChatModel(ctx, &openaimodel.ChatModelConfig{
			APIKey:     c.APIKey,
			BaseURL:    openAIBaseURL(c.BaseURL),
			Model:      model,
			HTTPClient: httpClient,
		})
		if err != nil {
			return "", nil, nil, fmt.Errorf("初始化 OpenAI provider 失败: %w", err)
		}
		return model, client, nil, nil
	}

	model := c.Model
	if model == "" {
		model = defaultClaudeModel
	}
	baseURL := anthropicBaseURL(c.BaseURL)
	var baseURLPtr *string
	if baseURL != "" {
		baseURLPtr = &baseURL
	}
	temp := float32(0.2)
	claudeConfig := &claudemodel.Config{
		APIKey:      c.APIKey,
		BaseURL:     baseURLPtr,
		Model:       model,
		MaxTokens:   8192,
		Temperature: &temp,
		HTTPClient:  httpClient,
	}
	if isCustomAnthropicBaseURL(baseURL) {
		claudeConfig.AuthToken = c.APIKey
	}
	client, err := claudemodel.NewChatModel(ctx, claudeConfig)
	if err != nil {
		return "", nil, nil, fmt.Errorf("初始化 Claude provider 失败: %w", err)
	}
	return model, client, []einomodel.Option{
		claudemodel.WithAutoCacheControl(&claudemodel.CacheControl{TTL: claudemodel.CacheTTL1h}),
	}, nil
}

func toEinoMessages(provider, system string, msgs []Message) []*schema.Message {
	out := make([]*schema.Message, 0, len(msgs)+1)
	systemMsg := schema.SystemMessage(system)
	if !isOpenAIProvider(provider) {
		systemMsg = claudemodel.SetMessageCacheControl(systemMsg, &claudemodel.CacheControl{TTL: claudemodel.CacheTTL1h})
	}
	out = append(out, systemMsg)

	for _, m := range msgs {
		if strings.EqualFold(m.Role, "assistant") {
			out = append(out, schema.AssistantMessage(m.Content, nil))
			continue
		}
		out = append(out, schema.UserMessage(m.Content))
	}
	return out
}

func isOpenAIProvider(provider string) bool {
	return provider == "openai" || provider == "gpt"
}

func isCustomAnthropicBaseURL(base string) bool {
	base = strings.ToLower(strings.TrimSpace(base))
	if base == "" {
		return false
	}
	base = strings.TrimPrefix(base, "https://")
	base = strings.TrimPrefix(base, "http://")
	return !strings.HasPrefix(base, "api.anthropic.com")
}

func anthropicBaseURL(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return ""
	}
	base = strings.TrimSuffix(base, "/messages")
	base = strings.TrimSuffix(base, "/v1")
	return base
}

func openAIBaseURL(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return ""
	}
	base = strings.TrimSuffix(base, "/chat/completions")
	if strings.HasSuffix(base, "/v1") {
		return base
	}
	return base + "/v1"
}
