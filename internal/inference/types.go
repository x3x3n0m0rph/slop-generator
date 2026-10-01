// Package inference implements OpenAI-compatible HTTP inference.
package inference

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type Usage struct {
	Prompt     int `json:"prompt_tokens"`
	Completion int `json:"completion_tokens"`
	Total      int `json:"total_tokens"`
}
type Request struct{ Messages []Message }
type Result struct {
	Content string
	Usage   *Usage
}
