package usagecollector

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

type hookInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	ThreadID       string `json:"thread_id"`
}

// Collect parses response usage from the transcript referenced by a Stop hook.
// It intentionally ignores message content and returns only usage metadata.
func Collect(hookJSON []byte, agentType string) ([]entities.UsageEvent, error) {
	var input hookInput
	if err := json.Unmarshal(hookJSON, &input); err != nil {
		return nil, fmt.Errorf("parse stop hook input: %w", err)
	}
	if input.TranscriptPath == "" {
		return nil, fmt.Errorf("stop hook input does not contain transcript_path")
	}
	f, err := os.Open(input.TranscriptPath)
	if err != nil {
		return nil, fmt.Errorf("open transcript: %w", err)
	}
	defer func() { _ = f.Close() }()

	agentSessionID := input.SessionID
	if agentSessionID == "" {
		agentSessionID = input.ThreadID
	}
	var events []entities.UsageEvent
	scanner := bufio.NewScanner(f)
	lineNumber := 0
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)
	for scanner.Scan() {
		lineNumber++
		line := append([]byte(nil), scanner.Bytes()...)
		var root map[string]interface{}
		if json.Unmarshal(line, &root) != nil {
			continue
		}
		candidate := root
		if message, ok := root["message"].(map[string]interface{}); ok {
			candidate = message
		}
		usage, ok := candidate["usage"].(map[string]interface{})
		// Codex rollout transcripts emit usage as an event_msg token_count.
		// last_token_usage is the per-turn delta; total_token_usage is cumulative
		// and would over-count when every Stop hook scans the whole transcript.
		if !ok {
			if payload, payloadOK := root["payload"].(map[string]interface{}); payloadOK && text(payload, "type") == "token_count" {
				if info, infoOK := payload["info"].(map[string]interface{}); infoOK {
					usage, ok = info["last_token_usage"].(map[string]interface{})
					candidate = payload
				}
			}
		}
		if !ok {
			continue
		}
		inputTokens := number(usage, "input_tokens", "inputTokens")
		outputTokens := number(usage, "output_tokens", "outputTokens")
		cached := number(usage, "cache_read_input_tokens", "cached_input_tokens", "cachedInputTokens")
		created := number(usage, "cache_creation_input_tokens", "cache_creation_tokens", "cacheCreationTokens", "cache_write_input_tokens")
		reasoning := number(usage, "reasoning_tokens", "reasoningTokens", "reasoning_output_tokens")
		if inputTokens+outputTokens+cached+created+reasoning == 0 {
			continue
		}
		responseID := text(candidate, "id", "response_id", "responseId")
		model := text(candidate, "model")
		if model == "" {
			model = text(root, "model")
		}
		if model == "" {
			model = "unknown"
		}
		turnID := text(root, "turn_id", "turnId", "uuid")
		occurredAt := parseTime(text(root, "timestamp", "created_at", "createdAt"))
		identity := responseID
		if identity == "" {
			sum := sha256.Sum256(line)
			identity = fmt.Sprintf("%d:%s", lineNumber, hex.EncodeToString(sum[:]))
		}
		eventSum := sha256.Sum256([]byte(strings.Join([]string{agentType, agentSessionID, identity, model}, "\x00")))
		events = append(events, entities.UsageEvent{
			EventID: "sha256:" + hex.EncodeToString(eventSum[:]), AgentSessionID: agentSessionID,
			TurnID: turnID, ResponseID: responseID, AgentType: agentType, Model: model,
			InputTokens: inputTokens, OutputTokens: outputTokens, CachedInputTokens: cached,
			CacheCreationTokens: created, ReasoningTokens: reasoning, OccurredAt: occurredAt,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read transcript: %w", err)
	}
	return events, nil
}

func number(values map[string]interface{}, keys ...string) int64 {
	for _, key := range keys {
		if value, ok := values[key].(float64); ok {
			return int64(value)
		}
	}
	return 0
}

func text(values map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok {
			return value
		}
	}
	return ""
}

func parseTime(value string) time.Time {
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed
	}
	return time.Now().UTC()
}
