package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Runtime identifies the local inference server flavour. Ollama uses its
// native /api/chat contract; vLLM and TGI both expose the OpenAI-compatible
// /v1/chat/completions contract.
type Runtime string

const (
	RuntimeOllama Runtime = "ollama"
	RuntimeVLLM   Runtime = "vllm"
	RuntimeTGI    Runtime = "tgi"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Client struct {
	http *http.Client
}

func NewClient(timeout time.Duration) *Client {
	return &Client{http: &http.Client{Timeout: timeout}}
}

type chatOptions struct {
	Endpoint    string
	Runtime     Runtime
	Model       string
	Messages    []Message
	Temperature float64
	MaxTokens   int
	JSONMode    bool
}

type chatUsage struct {
	PromptTokens     int
	CompletionTokens int
}

// ---------------------------------------------------------------- non-stream

func (c *Client) chat(ctx context.Context, o chatOptions) (string, chatUsage, error) {
	var text strings.Builder
	usage, err := c.chatStream(ctx, o, func(chunk string) error {
		text.WriteString(chunk)
		return nil
	})
	return text.String(), usage, err
}

// ------------------------------------------------------------------- stream

// chatStream streams tokens through onChunk. Both wire protocols are handled
// natively so no response shape is faked.
func (c *Client) chatStream(ctx context.Context, o chatOptions, onChunk func(string) error) (chatUsage, error) {
	var usage chatUsage
	base := strings.TrimRight(o.Endpoint, "/")

	var url string
	var body any
	switch o.Runtime {
	case RuntimeVLLM, RuntimeTGI:
		url = base + "/v1/chat/completions"
		payload := map[string]any{
			"model":       o.Model,
			"messages":    o.Messages,
			"stream":      true,
			"temperature": o.Temperature,
		}
		if o.MaxTokens > 0 {
			payload["max_tokens"] = o.MaxTokens
		}
		if o.JSONMode {
			payload["response_format"] = map[string]string{"type": "json_object"}
		}
		body = payload
	default:
		url = base + "/api/chat"
		opts := map[string]any{"temperature": o.Temperature}
		if o.MaxTokens > 0 {
			opts["num_predict"] = o.MaxTokens
		}
		payload := map[string]any{
			"model":    o.Model,
			"messages": o.Messages,
			"stream":   true,
			"options":  opts,
		}
		if o.JSONMode {
			payload["format"] = "json"
		}
		body = payload
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return usage, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return usage, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	res, err := c.http.Do(req)
	if err != nil {
		return usage, fmt.Errorf("local inference endpoint unreachable (%s): %w", url, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return usage, fmt.Errorf("inference endpoint returned %d: %s", res.StatusCode, strings.TrimSpace(string(msg)))
	}

	reader := bufio.NewReaderSize(res.Body, 64*1024)
	for {
		line, err := reader.ReadString('\n')
		if len(strings.TrimSpace(line)) > 0 {
			payload := strings.TrimSpace(line)
			if strings.HasPrefix(payload, "data:") {
				payload = strings.TrimSpace(strings.TrimPrefix(payload, "data:"))
				if payload == "[DONE]" {
					break
				}
			}
			switch o.Runtime {
			case RuntimeVLLM, RuntimeTGI:
				var frame openAIStreamFrame
				if jsonErr := json.Unmarshal([]byte(payload), &frame); jsonErr == nil {
					for _, ch := range frame.Choices {
						if ch.Delta.Content != "" {
							if err := onChunk(ch.Delta.Content); err != nil {
								return usage, err
							}
						}
					}
					if frame.Usage != nil {
						usage.PromptTokens = frame.Usage.PromptTokens
						usage.CompletionTokens = frame.Usage.CompletionTokens
					}
				}
			default:
				var frame ollamaChatFrame
				if jsonErr := json.Unmarshal([]byte(payload), &frame); jsonErr == nil {
					if frame.Error != "" {
						return usage, fmt.Errorf("inference error: %s", frame.Error)
					}
					if frame.Message.Content != "" {
						if err := onChunk(frame.Message.Content); err != nil {
							return usage, err
						}
					}
					if frame.Done {
						usage.PromptTokens = frame.PromptEvalCount
						usage.CompletionTokens = frame.EvalCount
					}
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return usage, err
		}
	}
	return usage, nil
}

type ollamaChatFrame struct {
	Model   string `json:"model"`
	Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
	Done            bool   `json:"done"`
	Error           string `json:"error"`
	PromptEvalCount int    `json:"prompt_eval_count"`
	EvalCount       int    `json:"eval_count"`
}

type openAIStreamFrame struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// --------------------------------------------------------------- embeddings

func (c *Client) embed(ctx context.Context, endpoint string, rt Runtime, model, text string) ([]float32, error) {
	base := strings.TrimRight(endpoint, "/")
	var url string
	var payload any
	switch rt {
	case RuntimeVLLM, RuntimeTGI:
		url = base + "/v1/embeddings"
		payload = map[string]any{"model": model, "input": text}
	default:
		url = base + "/api/embeddings"
		payload = map[string]any{"model": model, "prompt": text}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding endpoint unreachable (%s): %w", url, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		return nil, fmt.Errorf("embedding endpoint returned %d: %s", res.StatusCode, strings.TrimSpace(string(msg)))
	}
	var out struct {
		Embedding []float32 `json:"embedding"`
		Data      []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Embedding) > 0 {
		return out.Embedding, nil
	}
	if len(out.Data) > 0 {
		return out.Data[0].Embedding, nil
	}
	return nil, fmt.Errorf("embedding response contained no vector")
}

// Tags lists models actually present on an Ollama host (used by the Admin
// model registry to validate a registration before saving it).
func (c *Client) Tags(ctx context.Context, endpoint string, rt Runtime) ([]string, error) {
	base := strings.TrimRight(endpoint, "/")
	url := base + "/api/tags"
	if rt == RuntimeVLLM || rt == RuntimeTGI {
		url = base + "/v1/models"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("model listing returned %d", res.StatusCode)
	}
	var payload struct {
		Models []struct {
			Name  string `json:"name"`
			Model string `json:"model"`
		} `json:"models"`
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return nil, err
	}
	names := []string{}
	for _, m := range payload.Models {
		if m.Name != "" {
			names = append(names, m.Name)
		} else if m.Model != "" {
			names = append(names, m.Model)
		}
	}
	for _, m := range payload.Data {
		names = append(names, m.ID)
	}
	return names, nil
}
