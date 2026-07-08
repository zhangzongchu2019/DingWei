package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAICompatibleProvider calls a chat-completions endpoint compatible with
// OpenAI's /chat/completions shape, used by DeepSeek for P2 triage.
type OpenAICompatibleProvider struct {
	ID      string
	BaseURL string
	APIKey  string
	Model   string
	Client  *http.Client
}

func NewDeepSeek(apiKey, baseURL, model string) *OpenAICompatibleProvider {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://api.deepseek.com/v1"
	}
	if strings.TrimSpace(model) == "" {
		model = "deepseek-chat"
	}
	return &OpenAICompatibleProvider{
		ID:      "deepseek",
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		Model:   model,
		Client:  &http.Client{Timeout: 60 * time.Second},
	}
}

func (p *OpenAICompatibleProvider) Name() string {
	if p.ID != "" {
		return p.ID
	}
	return "openai-compatible"
}

func (p *OpenAICompatibleProvider) Complete(ctx context.Context, system, user string) (string, error) {
	if strings.TrimSpace(p.APIKey) == "" {
		return "", errors.New("llm provider api key not configured")
	}
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	payload := map[string]any{
		"model": p.Model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"temperature":     0,
		"response_format": map[string]string{"type": "json_object"},
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("llm http %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 || strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return "", errors.New("llm response missing content")
	}
	return out.Choices[0].Message.Content, nil
}
