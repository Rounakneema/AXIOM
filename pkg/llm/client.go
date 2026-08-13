package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// OllamaGenerateRequest represents the payload sent to Ollama
type OllamaGenerateRequest struct {
	Model     string                 `json:"model"`
	Prompt    string                 `json:"prompt"`
	Stream    bool                   `json:"stream"`
	KeepAlive int                    `json:"keep_alive"` // 0 unloads the model immediately
	Options   map[string]interface{} `json:"options"`
}

// OllamaResponse represents the response from Ollama
type OllamaResponse struct {
	Model    string `json:"model"`
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

// AskAxiom sends a prompt to the local Ollama instance (qwen3:4b).
// It implements the dual-mode architecture:
// requireDeepLogic=true uses full reasoning context.
// requireDeepLogic=false uses fast mascot roasting mode.
func AskAxiom(trigger string, context string, requireDeepLogic bool) (string, error) {
	systemPrompt := context
	var fullPrompt string
	var numPredict int
	var numCtx int

	if requireDeepLogic {
		// Thinking mode: let Qwen3 reason deeply, larger budget
		systemPrompt += "\nReason step by step using the data. Prove your conclusions with specific numbers."
		fullPrompt = systemPrompt + "\n\nTrigger: " + trigger
		numPredict = 400
		numCtx = 2048
	} else {
		// Mascot mode: DISABLE thinking for instant speed
		// Qwen3 respects /no_think tag to skip internal reasoning
		systemPrompt += "\nBe fast, brutal, and concise (max 3 sentences). Use specific numbers."
		fullPrompt = "/no_think\n" + systemPrompt + "\n\nTrigger: " + trigger
		numPredict = 150
		numCtx = 512
	}

	reqBody := OllamaGenerateRequest{
		Model:     "qwen2.5:3b",
		Prompt:    fullPrompt,
		Stream:    false,
		KeepAlive: 0, // TTL: 0 — model unloads instantly after response
		Options: map[string]interface{}{
			"temperature": 0.7,
			"num_predict": numPredict,
			"num_ctx":     numCtx,
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	resp, err := http.Post("http://localhost:11434/api/generate", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to contact Ollama: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ollama returned error: %s", string(body))
	}

	var ollamaResp OllamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return "", err
	}

	return ollamaResp.Response, nil
}
