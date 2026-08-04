package stream

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/safe"
	"github.com/tmaxmax/go-sse"
)

// ErrEmptyUpstreamStream marks 200 SSE streams that ended without forwarding
// any payload (all events skipped by transform or no events at all).
// Relay should fail over to another channel.
var ErrEmptyUpstreamStream = errors.New("upstream stream ended without forwarding any payload")

// StreamSource abstracts different event sources (SSE, WebSocket, raw bytes).
type StreamSource interface {
	// ReadEvent blocks until the next event is available or returns an error.
	// Returns io.EOF when the stream ends normally.
	ReadEvent(ctx context.Context) ([]byte, error)

	// Close releases resources. Must be idempotent.
	Close() error
}

// StreamTransform converts raw event data to the client's expected format.
// Returns nil/empty slice to skip writing (e.g., keep-alive events).
// For passthrough, set to nil in StreamConfig.
type StreamTransform func(ctx context.Context, data []byte) ([]byte, error)

// StreamWriter abstracts the HTTP/WebSocket response writer.
type StreamWriter interface {
	Write(data []byte) (int, error)
	Flush()
	Written() bool
	Header() http.Header
	WriteHeader(code int)
}

// StreamConfig configures a StreamProcessor instance.
type StreamConfig struct {
	// Core dependencies
	Source    StreamSource
	Transform StreamTransform // nil for passthrough
	Writer    StreamWriter
	Context   context.Context

	// Timeout & heartbeat
	FirstTokenTimeout time.Duration // 0 to disable
	HeartbeatInterval time.Duration // 0 to disable

	// Callbacks
	OnFirstToken func()                                            // Called when first payload written
	OnFinish     func(ctx context.Context, rawStream []byte) error // Called on stream end

	// Passthrough-specific
	BufferRawStream bool                // Enable raw stream buffering for metrics
	TerminalEvents  map[string]struct{} // Protocol terminal events for early completion
}

// Termination describes how the transport loop ended. It is deliberately
// separate from the request outcome: a client disconnect can happen after the
// protocol has already emitted its terminal event.
type Termination string

const (
	TerminationUnknown                       Termination = "unknown"
	TerminationUpstreamEOF                   Termination = "upstream_eof"
	TerminationProtocolTerminal              Termination = "protocol_terminal"
	TerminationClientCanceled                Termination = "client_canceled"
	TerminationClientDisconnectedAfterFinish Termination = "client_disconnected_after_finish"
	TerminationFirstTokenTimeout             Termination = "first_token_timeout"
	TerminationReadError                     Termination = "read_error"
	TerminationWriteError                    Termination = "write_error"
	TerminationTransformError                Termination = "transform_error"
)

// Result is the structured completion evidence produced by StreamProcessor.
// Callers should use this instead of deriving business success from err alone.
type Result struct {
	PayloadWritten bool        `json:"payload_written"`
	TerminalEvent  string      `json:"terminal_event,omitempty"`
	Termination    Termination `json:"termination"`
}

// StreamProcessor unifies all stream handling logic.
type StreamProcessor struct {
	config StreamConfig

	// State
	rawBuffer      bytes.Buffer
	payloadWritten bool
	firstToken     bool
	terminalEvent  string
	termination    Termination
}

// NewStreamProcessor creates a processor from config.
func NewStreamProcessor(config StreamConfig) *StreamProcessor {
	return &StreamProcessor{
		config:      config,
		firstToken:  true,
		termination: TerminationUnknown,
	}
}

// Run executes the unified stream processing loop.
func (p *StreamProcessor) Run() error {
	// Set SSE response headers
	headers := p.config.Writer.Header()
	headers.Set("Content-Type", "text/event-stream")
	headers.Set("Cache-Control", "no-cache")
	headers.Set("Connection", "keep-alive")
	headers.Set("X-Accel-Buffering", "no")

	// Setup heartbeat ticker
	var heartbeatTicker *time.Ticker
	var heartbeatC <-chan time.Time
	if p.config.HeartbeatInterval > 0 {
		heartbeatTicker = time.NewTicker(p.config.HeartbeatInterval)
		heartbeatC = heartbeatTicker.C
		defer heartbeatTicker.Stop()
	}

	// Setup first token timeout
	var firstTokenTimer *time.Timer
	var firstTokenC <-chan time.Time
	if p.firstToken && p.config.FirstTokenTimeout > 0 {
		firstTokenTimer = time.NewTimer(p.config.FirstTokenTimeout)
		firstTokenC = firstTokenTimer.C
		defer func() {
			if firstTokenTimer != nil {
				firstTokenTimer.Stop()
			}
		}()
	}

	// Async read from source — use a derived context so we can unblock on any exit.
	readCtx, readCancel := context.WithCancel(p.config.Context)
	defer readCancel()
	defer p.config.Source.Close()

	type readResult struct {
		data []byte
		err  error
	}
	results := make(chan readResult, 1)
	safe.Go("stream-processor-read", func() {
		defer close(results)
		for {
			data, err := p.config.Source.ReadEvent(readCtx)
			select {
			case results <- readResult{data: data, err: err}:
			case <-readCtx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	})

	// Main event loop
	for {
		select {
		case <-p.config.Context.Done():
			return p.handleDisconnect()

		case <-firstTokenC:
			p.termination = TerminationFirstTokenTimeout
			return p.handleFirstTokenTimeout()

		case <-heartbeatC:
			if err := p.writeHeartbeat(); err != nil {
				return err
			}

		case r, ok := <-results:
			if !ok {
				// The reader can exit through readCtx.Done without publishing its
				// final context error. Do not reinterpret that close as upstream EOF.
				if p.config.Context.Err() != nil {
					return p.handleDisconnect()
				}
				p.termination = p.successfulTermination()
				return p.finalize()
			}

			if r.err != nil {
				if r.err == io.EOF {
					p.termination = p.successfulTermination()
					return p.finalize()
				}
				if p.config.Context.Err() != nil &&
					(errors.Is(r.err, context.Canceled) || errors.Is(r.err, context.DeadlineExceeded)) {
					return p.handleDisconnect()
				}
				p.termination = TerminationReadError
				return fmt.Errorf("stream read error: %w", r.err)
			}

			if len(r.data) == 0 {
				continue
			}

			// Buffer raw data if enabled
			if p.config.BufferRawStream {
				p.rawBuffer.Write(r.data)
			}

			// Transform and write
			if err := p.processEvent(r.data); err != nil {
				return err
			}

			// First token handling
			if p.firstToken && p.payloadWritten {
				p.firstToken = false
				if p.config.OnFirstToken != nil {
					p.config.OnFirstToken()
				}
				if firstTokenTimer != nil {
					if !firstTokenTimer.Stop() {
						select {
						case <-firstTokenTimer.C:
						default:
						}
					}
					firstTokenTimer = nil
					firstTokenC = nil
				}
			}
		}
	}
}

// processEvent transforms and writes a single event.
func (p *StreamProcessor) processEvent(data []byte) error {
	var output []byte
	var err error

	if p.config.Transform != nil {
		output, err = p.config.Transform(p.config.Context, data)
		if err != nil {
			p.termination = TerminationTransformError
			return fmt.Errorf("transform error: %w", err)
		}
		if len(output) == 0 {
			return nil // Skip empty output
		}
	} else {
		output = data // Passthrough
	}

	// Record protocol terminal evidence before writing. A downstream write can
	// fail immediately after the upstream emitted its terminal frame; losing
	// the evidence would incorrectly turn a completed response into a generic
	// transport failure.
	if p.terminalEvent == "" {
		p.terminalEvent = terminalEventFromSSE(output, p.config.TerminalEvents)
	}
	if _, err := p.config.Writer.Write(output); err != nil {
		p.termination = TerminationWriteError
		if p.terminalEvent != "" && p.config.OnFinish != nil && p.config.BufferRawStream {
			// The normal finalize path is skipped on a write error, but the
			// already observed terminal frame still carries usage/response
			// metadata needed for accounting.
			_ = p.config.OnFinish(context.Background(), p.rawBuffer.Bytes())
		}
		return fmt.Errorf("write error: %w", err)
	}

	p.payloadWritten = true
	p.config.Writer.Flush()
	return nil
}

// writeHeartbeat sends SSE heartbeat (comment line).
func (p *StreamProcessor) writeHeartbeat() error {
	if _, err := p.config.Writer.Write([]byte(":\n\n")); err != nil {
		return err
	}
	p.config.Writer.Flush()
	return nil
}

// handleDisconnect handles context cancellation or timeout.
func (p *StreamProcessor) handleDisconnect() error {
	// Check both transformed output (recorded during processEvent) and buffered
	// source bytes. The former is essential for Chat/Anthropic -> Responses
	// conversions where only the client-facing stream contains response.completed.
	if p.terminalEvent == "" && p.config.BufferRawStream && len(p.config.TerminalEvents) > 0 {
		p.terminalEvent = p.streamTerminalEvent()
	}
	if p.terminalEvent != "" {
		p.termination = TerminationClientDisconnectedAfterFinish
		log.Debugf("client disconnected after terminal event %s, treating as success", p.terminalEvent)
		return p.finalizeWithContext(context.Background())
	}

	err := p.config.Context.Err()
	p.termination = TerminationClientCanceled
	log.Debugf("client disconnected, stopping stream: written=%t first_token_seen=%t err=%v",
		p.payloadWritten, !p.firstToken, err)

	if p.config.BufferRawStream && p.rawBuffer.Len() > 0 {
		// Still call OnFinish to collect partial metrics
		if p.config.OnFinish != nil {
			_ = p.config.OnFinish(context.Background(), p.rawBuffer.Bytes())
		}
	}

	return err
}

// handleFirstTokenTimeout returns first token timeout error.
func (p *StreamProcessor) handleFirstTokenTimeout() error {
	log.Warnf("first token timeout (%v), switching channel", p.config.FirstTokenTimeout)
	return fmt.Errorf("first token timeout after %v", p.config.FirstTokenTimeout)
}

// finalize completes the stream and calls OnFinish callback.
func (p *StreamProcessor) finalize() error {
	return p.finalizeWithContext(p.config.Context)
}

func (p *StreamProcessor) finalizeWithContext(ctx context.Context) error {
	if !p.payloadWritten {
		return ErrEmptyUpstreamStream
	}

	log.Debugf("stream end (payload_written=%t)", p.payloadWritten)

	if p.config.OnFinish != nil {
		rawStream := p.rawBuffer.Bytes()
		if err := p.config.OnFinish(ctx, rawStream); err != nil {
			return err
		}
	}

	return nil
}

// PayloadWritten returns whether any payload has been written to the client.
func (p *StreamProcessor) PayloadWritten() bool {
	return p.payloadWritten
}

// Result returns the completion evidence accumulated by the processor.
func (p *StreamProcessor) Result() Result {
	return Result{
		PayloadWritten: p.payloadWritten,
		TerminalEvent:  p.terminalEvent,
		Termination:    p.termination,
	}
}

func (p *StreamProcessor) successfulTermination() Termination {
	if p.terminalEvent == "" && p.config.BufferRawStream {
		p.terminalEvent = p.streamTerminalEvent()
	}
	if p.terminalEvent != "" {
		return TerminationProtocolTerminal
	}
	return TerminationUpstreamEOF
}

// streamTerminalEvent checks if buffered stream contains a terminal event.
func (p *StreamProcessor) streamTerminalEvent() string {
	if p.rawBuffer.Len() == 0 {
		return ""
	}
	return terminalEventFromSSE(p.rawBuffer.Bytes(), p.config.TerminalEvents)
}

func terminalEventFromSSE(payload []byte, terminalEvents map[string]struct{}) string {
	if len(payload) == 0 || len(terminalEvents) == 0 {
		return ""
	}
	readCfg := &sse.ReadConfig{MaxEventSize: 32 * 1024 * 1024}
	for ev, err := range sse.Read(bytes.NewReader(payload), readCfg) {
		if err != nil {
			break
		}

		// Extract event type
		typ := strings.TrimSpace(ev.Type)
		if typ == "" {
			data := strings.TrimSpace(ev.Data)
			if data == "[DONE]" {
				typ = "[DONE]"
			}
			if len(data) > 0 && data[0] == '{' {
				typ = extractJSONType(data)
			}
		}

		if _, ok := terminalEvents[typ]; ok {
			return typ
		}
	}

	return ""
}

// extractJSONType extracts "type" field from JSON without full unmarshaling.
func extractJSONType(data string) string {
	// Simple extraction: find "type":"value"
	if idx := strings.Index(data, `"type"`); idx >= 0 {
		rest := data[idx+6:]
		if idx := strings.IndexByte(rest, ':'); idx >= 0 {
			rest = rest[idx+1:]
			rest = strings.TrimSpace(rest)
			if len(rest) > 0 && rest[0] == '"' {
				rest = rest[1:]
				if idx := strings.IndexByte(rest, '"'); idx >= 0 {
					return rest[:idx]
				}
			}
		}
	}
	return ""
}
