package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestInjectGroupSystemPrompt(t *testing.T) {
	const prompt = "你好，这是系统提示词"
	tests := []struct {
		name     string
		protocol GroupSystemPromptProtocol
		body     string
		want     string
	}{
		{"Chat Completions", GroupPromptChatCompletions,
			`{"messages":[{"role":"developer","content":"client"},{"role":"user","content":"question"}],"stream":true}`,
			`{"messages":[{"role":"system","content":"你好，这是系统提示词"},{"role":"developer","content":"client"},{"role":"user","content":"question"}],"stream":true}`},
		{"Responses", GroupPromptResponses,
			`{"instructions":"client","input":"question","previous_response_id":"resp_123"}`,
			`{"instructions":"你好，这是系统提示词\n\nclient","input":"question","previous_response_id":"resp_123"}`},
		{"Anthropic string", GroupPromptAnthropic,
			`{"system":"client","messages":[]}`, `{"system":"你好，这是系统提示词\n\nclient","messages":[]}`},
		{"Anthropic blocks", GroupPromptAnthropic,
			`{"system":[{"type":"text","text":"client","cache_control":{"type":"ephemeral"}}]}`,
			`{"system":[{"type":"text","text":"你好，这是系统提示词"},{"type":"text","text":"client","cache_control":{"type":"ephemeral"}}]}`},
		{"Gemini camel case", GroupPromptGemini,
			`{"contents":[]}`, `{"contents":[],"systemInstruction":{"parts":[{"text":"你好，这是系统提示词"}]}}`},
		{"Gemini snake case", GroupPromptGemini,
			`{"system_instruction":{"role":"user","parts":[{"text":"client"}]},"contents":[]}`,
			`{"systemInstruction":{"role":"user","parts":[{"text":"你好，这是系统提示词"},{"text":"client"}]},"contents":[]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := InjectGroupSystemPrompt([]byte(test.body), test.protocol, prompt)
			require.NoError(t, err)
			require.JSONEq(t, test.want, string(got))
		})
	}
	untouched := []byte(`{"instructions":null}`)
	got, err := InjectGroupSystemPrompt(untouched, GroupPromptResponses, "")
	require.NoError(t, err)
	require.Equal(t, untouched, got)
	_, err = InjectGroupSystemPrompt([]byte(`{"messages":"client"}`), GroupPromptChatCompletions, prompt)
	require.Error(t, err)
}

func TestGroupSystemPromptResponsesWebSocketPreservesFrame(t *testing.T) {
	frame := []byte(`{"type":"response.create","instructions":"client","input":[],"previous_response_id":"resp_123"}`)
	body, err := injectResponsesWSGroupPrompt(frame, &OpenAIWSIngressHooks{GroupSystemPrompt: "group"})
	require.NoError(t, err)
	require.JSONEq(t, `{"type":"response.create","instructions":"group\n\nclient","input":[],"previous_response_id":"resp_123"}`, string(body))
}

func TestGroupSystemPromptPreservesLegacyResponsesInstructions(t *testing.T) {
	body := []byte(`{"model":"gpt-5","messages":[{"role":"system","content":"client"},{"role":"user","content":"question"}]}`)
	got, err := InjectGroupSystemPrompt(body, GroupPromptResponses, "group")
	require.NoError(t, err)
	normalized, changed, err := normalizeOpenAIResponsesLegacyIngress(got)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "group", gjson.GetBytes(normalized, "instructions").String())
	require.Equal(t, "system", gjson.GetBytes(normalized, "input.0.role").String())
	require.Equal(t, "client", gjson.GetBytes(normalized, "input.0.content").String())
}
