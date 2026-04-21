package openai

import (
	"testing"

	"github.com/samber/lo"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

// O-H6: Responses outbound re-emits file parts using whichever
// representation the internal model carries (uploaded id wins when both are
// present because that is the OpenAI-preferred passthrough path).
func TestConvertUserMessageToResponsesEmitsInputFileByID(t *testing.T) {
	msg := model.Message{
		Role: "user",
		Content: model.MessageContent{MultipleContent: []model.MessageContentPart{
			{
				Type: "file",
				File: &model.File{FileID: "file-abc"},
			},
		}},
	}
	item := convertUserMessageToResponses(msg)
	if item.Content == nil || len(item.Content.Items) != 1 {
		t.Fatalf("expected 1 content item, got %#v", item.Content)
	}
	sub := item.Content.Items[0]
	if sub.Type != "input_file" {
		t.Fatalf("expected input_file, got %q", sub.Type)
	}
	if sub.FileID == nil || *sub.FileID != "file-abc" {
		t.Fatalf("expected file_id forwarded, got %#v", sub.FileID)
	}
}

// O-H6: inline base64 files get re-emitted with filename + file_data.
func TestConvertUserMessageToResponsesEmitsInputFileInline(t *testing.T) {
	data := "data:application/pdf;base64,JVBERi0"
	msg := model.Message{
		Role: "user",
		Content: model.MessageContent{MultipleContent: []model.MessageContentPart{
			{
				Type: "file",
				File: &model.File{Filename: "report.pdf", FileData: data},
			},
		}},
	}
	item := convertUserMessageToResponses(msg)
	sub := item.Content.Items[0]
	if sub.Type != "input_file" {
		t.Fatalf("expected input_file, got %q", sub.Type)
	}
	if sub.Filename == nil || *sub.Filename != "report.pdf" {
		t.Fatalf("expected filename forwarded, got %#v", sub.Filename)
	}
	if sub.FileData == nil || *sub.FileData != data {
		t.Fatalf("expected file_data forwarded, got %#v", sub.FileData)
	}
}

// O-H6: audio parts must ride in the nested `input_audio` object, not the
// flat Text/ImageURL fields.
func TestConvertUserMessageToResponsesEmitsInputAudio(t *testing.T) {
	msg := model.Message{
		Role: "user",
		Content: model.MessageContent{MultipleContent: []model.MessageContentPart{
			{
				Type:  "input_audio",
				Audio: &model.Audio{Data: "UklGRi...", Format: "wav"},
			},
		}},
	}
	item := convertUserMessageToResponses(msg)
	sub := item.Content.Items[0]
	if sub.Type != "input_audio" {
		t.Fatalf("expected input_audio, got %q", sub.Type)
	}
	if sub.InputAudio == nil || sub.InputAudio.Format != "wav" || sub.InputAudio.Data != "UklGRi..." {
		t.Fatalf("expected nested audio forwarded, got %#v", sub.InputAudio)
	}
}

// O-H6: empty file/audio parts should be dropped rather than emitting an
// input_file with no fields (which the API would reject with 400).
func TestConvertUserMessageToResponsesDropsEmptyMultimodal(t *testing.T) {
	msg := model.Message{
		Role: "user",
		Content: model.MessageContent{MultipleContent: []model.MessageContentPart{
			{Type: "file", File: &model.File{}},
			{Type: "input_audio", Audio: nil},
			{Type: "text", Text: lo.ToPtr("survives")},
		}},
	}
	item := convertUserMessageToResponses(msg)
	if item.Content == nil || len(item.Content.Items) != 1 {
		t.Fatalf("expected 1 content item after drops, got %#v", item.Content)
	}
	if item.Content.Items[0].Type != "input_text" {
		t.Fatalf("expected surviving text, got %q", item.Content.Items[0].Type)
	}
}
