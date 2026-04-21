package openai

import (
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

// O-H6: an `input_file` item referenced by uploaded file id must survive the
// Responses → internal transformation so that downstream processors (and a
// Responses round-trip) keep the reference.
func TestConvertInputToMessageContentFileByID(t *testing.T) {
	input := ResponsesInput{Items: []ResponsesItem{
		{Type: "input_file", FileID: stringPtr("file-abc123")},
	}}
	content := convertInputToMessageContent(input)
	if len(content.MultipleContent) != 1 {
		t.Fatalf("expected 1 part, got %d", len(content.MultipleContent))
	}
	p := content.MultipleContent[0]
	if p.Type != "file" || p.File == nil {
		t.Fatalf("expected file part, got %#v", p)
	}
	if p.File.FileID != "file-abc123" {
		t.Fatalf("expected file_id preserved, got %q", p.File.FileID)
	}
}

// O-H6: inline base64 files travel as filename + file_data; the data is
// preserved verbatim so the raw data URL can round-trip.
func TestConvertInputToMessageContentFileInline(t *testing.T) {
	data := "data:application/pdf;base64,JVBERi0"
	input := ResponsesInput{Items: []ResponsesItem{
		{
			Type:     "input_file",
			Filename: stringPtr("report.pdf"),
			FileData: &data,
		},
	}}
	content := convertInputToMessageContent(input)
	if len(content.MultipleContent) != 1 {
		t.Fatalf("expected 1 part, got %d", len(content.MultipleContent))
	}
	p := content.MultipleContent[0]
	if p.File == nil || p.File.FileData != data || p.File.Filename != "report.pdf" {
		t.Fatalf("expected inline file preserved, got %#v", p.File)
	}
}

// O-H6: URL-referenced files should survive too.
func TestConvertInputToMessageContentFileByURL(t *testing.T) {
	input := ResponsesInput{Items: []ResponsesItem{
		{Type: "input_file", FileURL: stringPtr("https://example.com/report.pdf")},
	}}
	content := convertInputToMessageContent(input)
	if len(content.MultipleContent) != 1 || content.MultipleContent[0].File == nil {
		t.Fatalf("expected file part, got %#v", content)
	}
	if got := content.MultipleContent[0].File.FileURL; got != "https://example.com/report.pdf" {
		t.Fatalf("expected file_url preserved, got %q", got)
	}
}

// O-H6: audio items arrive with a nested `input_audio` object ({ data,
// format }); map that onto the internal Audio struct.
func TestConvertInputToMessageContentAudio(t *testing.T) {
	input := ResponsesInput{Items: []ResponsesItem{
		{
			Type: "input_audio",
			InputAudio: &ResponsesInputAudio{
				Data:   "UklGRi...",
				Format: "wav",
			},
		},
	}}
	content := convertInputToMessageContent(input)
	if len(content.MultipleContent) != 1 {
		t.Fatalf("expected 1 part, got %d", len(content.MultipleContent))
	}
	p := content.MultipleContent[0]
	if p.Type != "input_audio" || p.Audio == nil {
		t.Fatalf("expected audio part, got %#v", p)
	}
	if p.Audio.Format != "wav" || p.Audio.Data != "UklGRi..." {
		t.Fatalf("expected audio fields preserved, got %#v", p.Audio)
	}
}

// O-H6: malformed items (nil audio / empty file fields) must be silently
// dropped so we don't surface empty parts downstream.
func TestConvertInputToMessageContentDropsEmptyMultimodal(t *testing.T) {
	input := ResponsesInput{Items: []ResponsesItem{
		{Type: "input_file"}, // no id/url/data
		{Type: "input_audio"}, // nil input_audio
	}}
	content := convertInputToMessageContent(input)
	if len(content.MultipleContent) != 0 {
		t.Fatalf("expected empty multimodal parts dropped, got %#v", content.MultipleContent)
	}
}

// Sanity guard: the existing text+image paths still work.
func TestConvertInputToMessageContentKeepsTextAndImage(t *testing.T) {
	input := ResponsesInput{Items: []ResponsesItem{
		{Type: "input_text", Text: stringPtr("hi")},
		{Type: "input_image", ImageURL: stringPtr("https://example.com/pic.png"), Detail: stringPtr("high")},
	}}
	content := convertInputToMessageContent(input)
	if len(content.MultipleContent) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(content.MultipleContent))
	}
	if content.MultipleContent[0].Type != "text" {
		t.Fatalf("expected first part text, got %s", content.MultipleContent[0].Type)
	}
	if content.MultipleContent[1].Type != "image_url" || content.MultipleContent[1].ImageURL == nil {
		t.Fatalf("expected image_url part, got %#v", content.MultipleContent[1])
	}
	// Ensure File / Audio model types stay untouched on text+image paths.
	if content.MultipleContent[0].File != nil || content.MultipleContent[0].Audio != nil {
		t.Fatalf("expected no extraneous file/audio on text part, got %#v", content.MultipleContent[0])
	}
	_ = (*model.File)(nil)
}
