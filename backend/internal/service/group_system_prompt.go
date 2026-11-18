package service

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type GroupSystemPromptProtocol string

const (
	GroupPromptChatCompletions GroupSystemPromptProtocol = "chat_completions"
	GroupPromptResponses       GroupSystemPromptProtocol = "responses"
	GroupPromptAnthropic       GroupSystemPromptProtocol = "anthropic"
	GroupPromptGemini          GroupSystemPromptProtocol = "gemini"
)

func InjectGroupSystemPrompt(body []byte, protocol GroupSystemPromptProtocol, prompt string) ([]byte, error) {
	if prompt == "" {
		return body, nil
	}
	if !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() {
		return nil, errors.New("expected a JSON request object")
	}

	switch protocol {
	case GroupPromptChatCompletions:
		if !gjson.GetBytes(body, "messages").IsArray() {
			return nil, errors.New("expected a messages array")
		}
		return prependGroupPromptItem(body, "messages", map[string]string{"role": "system", "content": prompt})
	case GroupPromptResponses:
		return prependGroupPromptText(body, "instructions", prompt)
	case GroupPromptAnthropic:
		if gjson.GetBytes(body, "system").IsArray() {
			return prependGroupPromptItem(body, "system", map[string]string{"type": "text", "text": prompt})
		}
		return prependGroupPromptText(body, "system", prompt)
	case GroupPromptGemini:
		const field = "systemInstruction"
		instruction := gjson.GetBytes(body, field)
		if alternate := gjson.GetBytes(body, "system_instruction"); alternate.Exists() {
			if instruction.Exists() {
				return nil, errors.New("expected only one system instruction field")
			}
			var err error
			body, err = sjson.DeleteBytes(body, "system_instruction")
			if err != nil {
				return nil, err
			}
			instruction = alternate
			if instruction.Type != gjson.Null {
				body, err = sjson.SetRawBytes(body, field, []byte(instruction.Raw))
				if err != nil {
					return nil, err
				}
			}
		}
		if instruction.Exists() && instruction.Type != gjson.Null && !instruction.IsObject() {
			return nil, errors.New("expected a system instruction object")
		}
		return prependGroupPromptItem(body, field+".parts", map[string]string{"text": prompt})
	default:
		return body, nil
	}
}

func injectResponsesWSGroupPrompt(body []byte, hooks *OpenAIWSIngressHooks) ([]byte, error) {
	if hooks == nil || hooks.GroupSystemPrompt == "" {
		return body, nil
	}
	return InjectGroupSystemPrompt(body, GroupPromptResponses, hooks.GroupSystemPrompt)
}

func prependGroupPromptText(body []byte, path, prompt string) ([]byte, error) {
	existing := gjson.GetBytes(body, path)
	if existing.Exists() && existing.Type != gjson.Null && existing.Type != gjson.String {
		return nil, errors.New("expected text instructions")
	}
	if existing.String() != "" {
		prompt += "\n\n" + existing.String()
	}
	return sjson.SetBytes(body, path, prompt)
}

func prependGroupPromptItem(body []byte, path string, item map[string]string) ([]byte, error) {
	existing := gjson.GetBytes(body, path)
	if existing.Exists() && existing.Type != gjson.Null && !existing.IsArray() {
		return nil, errors.New("expected an instruction array")
	}
	encoded, err := json.Marshal(item)
	if err != nil {
		return nil, err
	}
	array := append([]byte{'['}, encoded...)
	if existing.IsArray() {
		items := bytes.TrimSpace([]byte(existing.Raw[1 : len(existing.Raw)-1]))
		if len(items) != 0 {
			array = append(array, ',')
			array = append(array, items...)
		}
	}
	return sjson.SetRawBytes(body, path, append(array, ']'))
}
