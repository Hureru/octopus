package model

import "strings"

// AlternationProvider identifies the upstream provider whose message
// alternation rules should be enforced. The zero value ("" / "unknown")
// applies the loosest OpenAI-style ruleset (no merging, no pivot).
type AlternationProvider string

const (
	AlternationProviderUnknown   AlternationProvider = ""
	AlternationProviderAnthropic AlternationProvider = "anthropic"
	AlternationProviderGemini    AlternationProvider = "gemini"
	AlternationProviderOpenAI    AlternationProvider = "openai"
)

// continuedPivotText is the placeholder content inserted as a pivot when
// the upstream schema requires a user turn but the internal conversation
// doesn't have one at the expected position.
const continuedPivotText = "(continued)"

// FlattenUnsupportedBlocks rewrites MessageContentPart entries whose Type
// is not natively supported by the target provider into text hints or
// drops them outright. Intended for outbound paths whose upstream rejects
// unknown block types (OpenAI chat completions rejects anything other than
// text / image_url / input_audio / file today). Anthropic and Gemini
// outbound paths build their own wire shapes and do not need this.
//
// The rewrite is destructive: the affected parts are replaced in-place so
// downstream JSON marshalling sees only supported types. Document blocks
// are collapsed into a text hint containing title/context/body; server
// tool blocks are dropped silently.
func (r *InternalLLMRequest) FlattenUnsupportedBlocks(provider AlternationProvider) {
	if provider != AlternationProviderOpenAI {
		return
	}
	for i := range r.Messages {
		r.Messages[i].flattenUnsupportedBlocksForOpenAI()
	}
}

// flattenUnsupportedBlocksForOpenAI is the per-message worker for
// FlattenUnsupportedBlocks. It compacts MultipleContent in place: document
// blocks become text parts, server_tool_use / server_tool_result blocks
// are dropped (they carry no value for OpenAI-shaped clients).
func (m *Message) flattenUnsupportedBlocksForOpenAI() {
	if len(m.Content.MultipleContent) == 0 {
		return
	}
	filtered := m.Content.MultipleContent[:0]
	for _, part := range m.Content.MultipleContent {
		switch part.Type {
		case "document":
			if hint := documentTextHint(part.Document); hint != "" {
				text := hint
				filtered = append(filtered, MessageContentPart{
					Type: "text",
					Text: &text,
				})
			}
		case "server_tool_use", "server_tool_result":
			// OpenAI has no direct equivalent; drop.
		default:
			filtered = append(filtered, part)
		}
	}
	m.Content.MultipleContent = filtered
}

// documentTextHint produces a best-effort plain-text representation of a
// document block so providers without native document support still see
// the metadata and body text.
func documentTextHint(doc *DocumentSource) string {
	if doc == nil {
		return ""
	}
	parts := make([]string, 0, 4)
	if doc.Title != "" {
		parts = append(parts, "Title: "+doc.Title)
	}
	if doc.Context != "" {
		parts = append(parts, "Context: "+doc.Context)
	}
	switch doc.Type {
	case "url":
		if doc.URL != "" {
			parts = append(parts, "Document URL: "+doc.URL)
		}
	case "text":
		if doc.Text != "" {
			parts = append(parts, doc.Text)
		} else if doc.Data != "" {
			parts = append(parts, doc.Data)
		}
	case "base64":
		if doc.MediaType != "" {
			parts = append(parts, "Attached document ("+doc.MediaType+")")
		}
	}
	return strings.Join(parts, "\n\n")
}

// EnforceAlternation returns a copy of msgs with consecutive same-role
// turns merged so Anthropic's and Gemini's strict user/assistant
// alternation holds. System and developer messages are preserved verbatim
// (callers extract them separately before dispatch). For Anthropic and
// Gemini, tool-role messages are treated as user turns since tool results
// ride inside user messages on the wire.
//
// Merging preserves all content parts — structured parts (image / audio /
// tool_use / tool_result / thinking) are concatenated into MultipleContent;
// pure-text runs are joined with "\n\n". When the first non-system message
// is an assistant turn, a "(continued)" user pivot is prepended so the
// Anthropic schema accepts the opening shape.
//
// OpenAI and unknown providers get a no-op (return the original slice) —
// OpenAI tolerates repeated roles and there is no value in paying the copy
// cost.
func EnforceAlternation(msgs []Message, provider AlternationProvider) []Message {
	if len(msgs) == 0 {
		return msgs
	}
	switch provider {
	case AlternationProviderAnthropic, AlternationProviderGemini:
	default:
		return msgs
	}

	// Copy system / developer messages through unchanged, collecting the
	// conversational subset into a working slice we can freely rewrite.
	type rolePos struct {
		role string
		idx  int
	}

	out := make([]Message, 0, len(msgs)+1)
	pending := make([]Message, 0, len(msgs))
	roles := make([]rolePos, 0, len(msgs))

	for _, msg := range msgs {
		if msg.Role == "system" || msg.Role == "developer" {
			out = append(out, msg)
			continue
		}
		effective := effectiveRole(msg.Role, provider)
		pending = append(pending, msg)
		roles = append(roles, rolePos{role: effective, idx: len(pending) - 1})
	}

	if len(pending) == 0 {
		return out
	}

	// Anthropic requires the first conversational turn to be user. Insert a
	// (continued) pivot user turn if the conversation opens with assistant.
	if provider == AlternationProviderAnthropic && roles[0].role == "assistant" {
		pivot := makeContinuedUserPivot()
		pending = append([]Message{pivot}, pending...)
		roles = append([]rolePos{{role: "user", idx: 0}}, roles...)
		for i := range roles[1:] {
			roles[i+1].idx++
		}
	}

	// Merge consecutive same-role runs.
	merged := make([]Message, 0, len(pending))
	i := 0
	for i < len(roles) {
		startRole := roles[i].role
		runStart := i
		j := i + 1
		for j < len(roles) && roles[j].role == startRole {
			j++
		}
		if j-runStart == 1 {
			merged = append(merged, pending[roles[runStart].idx])
		} else {
			group := make([]Message, 0, j-runStart)
			for k := runStart; k < j; k++ {
				group = append(group, pending[roles[k].idx])
			}
			merged = append(merged, mergeSameRole(group, startRole))
		}
		i = j
	}

	out = append(out, merged...)
	return out
}

// effectiveRole normalises Message.Role to the two-role alphabet
// (user / assistant) that Anthropic and Gemini expect on the wire. Tool
// messages are treated as user turns since tool_result / functionResponse
// payloads ride inside user messages.
func effectiveRole(role string, provider AlternationProvider) string {
	switch role {
	case "tool":
		return "user"
	case "model":
		// Gemini uses "model" on the wire but the internal representation
		// stays "assistant"; treat the wire alias as assistant here.
		return "assistant"
	case "":
		return "user"
	default:
		return role
	}
}

// mergeSameRole collapses a run of consecutive same-effective-role messages
// into a single message. Text content gets joined with a blank line; every
// other content part (images, tool_use, tool_result, thinking) is preserved
// by flattening the inputs into MultipleContent. Tool calls and reasoning
// blocks are concatenated across the run so no upstream-visible state is
// lost.
func mergeSameRole(group []Message, effectiveRole string) Message {
	if len(group) == 1 {
		return group[0]
	}

	combined := Message{Role: group[0].Role}
	// If any message in the run already sets the canonical role, carry it.
	for _, m := range group {
		if m.Role != "" && m.Role != "tool" {
			combined.Role = m.Role
			break
		}
	}
	// Tool-role collapses to user on the wire; we keep the internal role
	// consistent so downstream conversion uses the user branch.
	if effectiveRole == "user" {
		for _, m := range group {
			if m.Role == "tool" {
				combined.Role = "tool"
				break
			}
		}
	}

	// Walk the run, flattening content into a single MultipleContent slice.
	// Pure-text messages get materialised as text parts so they can be
	// joined into the same structured stream.
	parts := make([]MessageContentPart, 0, len(group)*2)
	var textBuf []string
	flushText := func() {
		if len(textBuf) == 0 {
			return
		}
		joined := strings.Join(textBuf, "\n\n")
		parts = append(parts, MessageContentPart{Type: "text", Text: &joined})
		textBuf = textBuf[:0]
	}
	for _, m := range group {
		switch {
		case m.Content.Content != nil && *m.Content.Content != "":
			textBuf = append(textBuf, *m.Content.Content)
		case len(m.Content.MultipleContent) > 0:
			flushText()
			parts = append(parts, m.Content.MultipleContent...)
		}
		if len(m.ToolCalls) > 0 {
			combined.ToolCalls = append(combined.ToolCalls, m.ToolCalls...)
		}
		if len(m.ReasoningBlocks) > 0 {
			combined.ReasoningBlocks = append(combined.ReasoningBlocks, m.ReasoningBlocks...)
		}
		if m.ToolCallID != nil && combined.ToolCallID == nil {
			id := *m.ToolCallID
			combined.ToolCallID = &id
		}
	}
	flushText()

	switch len(parts) {
	case 0:
		placeholder := " "
		combined.Content = MessageContent{Content: &placeholder}
	case 1:
		if parts[0].Type == "text" && parts[0].Text != nil {
			combined.Content = MessageContent{Content: parts[0].Text}
		} else {
			combined.Content = MessageContent{MultipleContent: parts}
		}
	default:
		combined.Content = MessageContent{MultipleContent: parts}
	}
	return combined
}

// makeContinuedUserPivot builds the "(continued)" placeholder user message
// used to satisfy Anthropic's "first conversational message must be user"
// requirement.
func makeContinuedUserPivot() Message {
	text := continuedPivotText
	return Message{
		Role:    "user",
		Content: MessageContent{Content: &text},
	}
}
