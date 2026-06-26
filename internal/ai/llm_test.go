package ai

import (
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestProviderBaseURLNormalization(t *testing.T) {
	cases := []struct {
		name string
		fn   func(string) string
		in   string
		want string
	}{
		{name: "anthropic root", fn: anthropicBaseURL, in: "https://api.anthropic.com", want: "https://api.anthropic.com"},
		{name: "anthropic v1", fn: anthropicBaseURL, in: "https://api.anthropic.com/v1", want: "https://api.anthropic.com"},
		{name: "anthropic endpoint", fn: anthropicBaseURL, in: "https://api.anthropic.com/v1/messages", want: "https://api.anthropic.com"},
		{name: "openai root", fn: openAIBaseURL, in: "https://api.openai.com", want: "https://api.openai.com/v1"},
		{name: "openai v1", fn: openAIBaseURL, in: "https://api.openai.com/v1", want: "https://api.openai.com/v1"},
		{name: "openai endpoint", fn: openAIBaseURL, in: "https://api.openai.com/v1/chat/completions", want: "https://api.openai.com/v1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.fn(c.in); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestToEinoMessages(t *testing.T) {
	msgs := toEinoMessages("claude", "sys", []Message{
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1"},
	})
	if len(msgs) != 3 {
		t.Fatalf("len=%d", len(msgs))
	}
	if msgs[0].Role != schema.System || msgs[1].Role != schema.User || msgs[2].Role != schema.Assistant {
		t.Fatalf("roles=%v,%v,%v", msgs[0].Role, msgs[1].Role, msgs[2].Role)
	}
	if len(msgs[0].Extra) == 0 {
		t.Fatal("claude system prompt should carry cache-control metadata")
	}
}

func TestIsOpenAIProvider(t *testing.T) {
	if !isOpenAIProvider("openai") || !isOpenAIProvider("gpt") {
		t.Fatal("openai aliases not recognized")
	}
	if isOpenAIProvider("claude") || isOpenAIProvider("anthropic") || isOpenAIProvider("") {
		t.Fatal("non-openai provider recognized as openai")
	}
}

func TestIsCustomAnthropicBaseURL(t *testing.T) {
	if isCustomAnthropicBaseURL("https://api.anthropic.com") {
		t.Fatal("official anthropic endpoint should not be custom")
	}
	if isCustomAnthropicBaseURL("https://api.anthropic.com/v1") {
		t.Fatal("official anthropic v1 endpoint should not be custom")
	}
	if !isCustomAnthropicBaseURL("https://api.aicoding.sh") {
		t.Fatal("proxy endpoint should be custom")
	}
}

func TestPatchFromToolCalls(t *testing.T) {
	patch, ok := patchFromToolCalls([]ToolCall{{
		Name:      proposePatchToolName,
		Arguments: `{"patch":"<<<<<<< SEARCH\nold\n=======\nnew\n>>>>>>> REPLACE"}`,
	}})
	if !ok || patch == "" {
		t.Fatalf("patch=%q ok=%v", patch, ok)
	}

	patch, ok = patchFromToolCalls([]ToolCall{{Name: "other", Arguments: `{"patch":"x"}`}})
	if ok || patch != "" {
		t.Fatalf("unexpected patch=%q ok=%v", patch, ok)
	}
}

func TestProposePatchToolSchema(t *testing.T) {
	tool := proposePatchTool()
	if tool.Name != proposePatchToolName {
		t.Fatalf("tool name=%q", tool.Name)
	}
	js, err := tool.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	if len(js.Required) != 1 || js.Required[0] != "patch" {
		t.Fatalf("required=%v", js.Required)
	}
}

func TestContentAndToolCalls(t *testing.T) {
	out, calls, err := contentAndToolCalls(schema.AssistantMessage("说明", []schema.ToolCall{{
		Function: schema.FunctionCall{Name: proposePatchToolName, Arguments: `{"patch":"x"}`},
	}}))
	if err != nil {
		t.Fatal(err)
	}
	if out != "说明" || len(calls) != 1 || calls[0].Name != proposePatchToolName {
		t.Fatalf("out=%q calls=%v", out, calls)
	}
}
