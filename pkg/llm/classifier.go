package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// ClassifyYouTubeTitle asks Ollama to classify a YouTube title.
// It returns a standard category like "learning", "music", "entertainment", or "motivation".
func ClassifyYouTubeTitle(title string) string {
	prompt := fmt.Sprintf("You are an AI categorizer. Categorize the following YouTube video title into exactly one of these words: LEARNING, MUSIC, ENTERTAINMENT, MOTIVATION.\n\nTitle: %q\n\nCategory:", title)
	
	reqBody := OllamaGenerateRequest{
		Model:  "qwen2.5:3b",
		Prompt: prompt,
		Stream: false,
		KeepAlive: 300, // Keep loaded for 5 mins to make subsequent classifications fast
		Options: map[string]interface{}{
			"temperature": 0.1,
			"num_predict": 10,
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "entertainment" // fallback
	}

	resp, err := http.Post("http://localhost:11434/api/generate", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return "entertainment"
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "entertainment"
	}

	var ollamaResp OllamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return "entertainment"
	}

	cat := strings.TrimSpace(strings.ToUpper(ollamaResp.Response))
	// Clean up any extra text the LLM might have returned
	if strings.Contains(cat, "LEARNING") {
		return "learning"
	} else if strings.Contains(cat, "MUSIC") {
		return "music"
	} else if strings.Contains(cat, "MOTIVATION") {
		return "motivation"
	} else {
		return "entertainment" // fallback for everything else
	}
}
