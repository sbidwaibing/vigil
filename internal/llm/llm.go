package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sukrutbidwai/vigil/pkg/types"
)

// Client wraps LLM calls. If apiKey is empty, falls back to templates.
type Client struct {
	apiKey     string
	httpClient *http.Client
}

func New(apiKey string) *Client {
	return &Client{
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// Summarize returns a plain-language summary of repo drift.
// Uses templates by default, Haiku if API key is set.
func (c *Client) Summarize(state *types.RepoState) (string, error) {
	if c.apiKey == "" {
		return templateSummary(state), nil
	}
	prompt := buildSummaryPrompt(state)
	return c.callHaiku(prompt)
}

// ExplainConflict returns a plain-language explanation of overlapping changes.
func (c *Client) ExplainConflict(state *types.RepoState, diffs map[string]string) (string, error) {
	if c.apiKey == "" {
		return templateConflict(state), nil
	}
	prompt := buildConflictPrompt(state, diffs)
	return c.callHaiku(prompt)
}

// DraftSyncMessage drafts a message to send to the overlapping author.
func (c *Client) DraftSyncMessage(state *types.RepoState) (string, error) {
	if c.apiKey == "" {
		return templateSyncMsg(state), nil
	}
	prompt := buildSyncMsgPrompt(state)
	return c.callHaiku(prompt)
}

// --- Haiku API ---

type haikuReq struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	Messages  []haikuMsg `json:"messages"`
}

type haikuMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type haikuResp struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
}

func (c *Client) callHaiku(prompt string) (string, error) {
	body := haikuReq{
		Model:     "claude-haiku-4-5",
		MaxTokens: 300,
		Messages: []haikuMsg{
			{Role: "user", Content: prompt},
		},
	}
	data, _ := json.Marshal(body)

	req, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("haiku API call failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("haiku API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result haikuResp
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", err
	}
	if len(result.Content) == 0 {
		return "", fmt.Errorf("empty response from haiku")
	}
	return result.Content[0].Text, nil
}

// --- Prompt builders ---

func buildSummaryPrompt(s *types.RepoState) string {
	return fmt.Sprintf(`Summarize this git repo drift in 2-3 plain sentences for a developer. Be direct.

Branch: %s
Upstream: %s
Commits ahead: %d
Commits behind: %d
Your modified files: %s
Remote changed files: %s
Overlapping files: %s
Authors of remote commits: %s
Risk: %s`,
		s.Branch, s.Upstream, s.Ahead, s.Behind,
		strings.Join(s.DirtyFiles, ", "),
		strings.Join(s.RemoteChanged, ", "),
		strings.Join(s.OverlapFiles, ", "),
		strings.Join(s.Authors, ", "),
		string(s.ConflictRisk),
	)
}

func buildConflictPrompt(s *types.RepoState, diffs map[string]string) string {
	var diffSection strings.Builder
	for file, diff := range diffs {
		diffSection.WriteString(fmt.Sprintf("\n--- %s ---\n%s\n", file, truncate(diff, 1500)))
	}
	return fmt.Sprintf(`Explain the likely merge conflict in 3-4 sentences. Mention which functions or blocks collide and whether the changes seem compatible or contradictory.

Branch: %s
Overlapping files: %s
Authors: %s

Diffs:%s`,
		s.Branch,
		strings.Join(s.OverlapFiles, ", "),
		strings.Join(s.Authors, ", "),
		diffSection.String(),
	)
}

func buildSyncMsgPrompt(s *types.RepoState) string {
	return fmt.Sprintf(`Draft a short, direct message (3-4 lines) to coordinate with a teammate about overlapping code changes. Include the branch name and which files overlap. No fluff.

My branch: %s
Their changes on: %s
Overlapping files: %s
Authors: %s`,
		s.Branch, s.Upstream,
		strings.Join(s.OverlapFiles, ", "),
		strings.Join(s.Authors, ", "),
	)
}

// --- Templates (fallback when no API key) ---

func templateSummary(s *types.RepoState) string {
	if s.Behind == 0 {
		return fmt.Sprintf("[%s] Up to date with %s.", s.Branch, s.Upstream)
	}
	msg := fmt.Sprintf("[%s] %d commits behind %s.", s.Branch, s.Behind, s.Upstream)
	if len(s.OverlapFiles) > 0 {
		msg += fmt.Sprintf("\n⚠ %d files overlap with remote: %s",
			len(s.OverlapFiles), strings.Join(s.OverlapFiles, ", "))
		msg += fmt.Sprintf("\nAuthors: %s", strings.Join(s.Authors, ", "))
		msg += fmt.Sprintf("\nConflict risk: %s", string(s.ConflictRisk))
	}
	return msg
}

func templateConflict(s *types.RepoState) string {
	return fmt.Sprintf(
		"Potential conflict in: %s\nRemote authors: %s\nBoth sides modified these files. Rebase now to minimize conflict size.",
		strings.Join(s.OverlapFiles, ", "),
		strings.Join(s.Authors, ", "),
	)
}

func templateSyncMsg(s *types.RepoState) string {
	return fmt.Sprintf(
		"Hey — heads up, I'm working on %s and we have overlapping changes in: %s. Want to sync before either of us pushes?",
		s.Branch,
		strings.Join(s.OverlapFiles, ", "),
	)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "\n... (truncated)"
}
