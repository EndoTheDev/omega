package provider

import (
	"testing"

	"github.com/EndoTheDev/omega/ai"
)

// TestImagesOllamaWire: the ollama /api/chat lane expects images as a
// raw base64 STRING array, not the object shape ai.User marshals - a
// live probe (glm-5.3, 2026-10-20) 400s on the object form with
// "cannot unmarshal object into Alias.images.0". Stream() must convert.
func TestImagesOllamaWire(t *testing.T) {
	msgs := []ai.Message{
		ai.NewSystem("sys"),
		ai.NewUserWithImages("look", []ai.ImageContent{
			{MediaType: "image/png", Base64: "iVBORw0KGgo="},
			{MediaType: "image/jpeg", Base64: "/9j/"},
		}),
	}

	// drive the wire conversion the way Stream does without the network
	msgJSON := messagesToJSON(msgs, false)
	imagesToOllamaWire(msgJSON)

	user := msgJSON[1]
	images, ok := user["images"].([]string)
	if !ok {
		t.Fatalf("images type = %T, want []string", user["images"])
	}
	if len(images) != 2 || images[0] != "iVBORw0KGgo=" || images[1] != "/9j/" {
		t.Fatalf("images = %v, want the raw base64 strings", images)
	}
	if user["content"] != "look" {
		t.Fatalf("content = %v, want look", user["content"])
	}

	// text-only messages: no images key, nothing rewritten
	textOnly := messagesToJSON([]ai.Message{ai.NewUser("hi")}, false)
	if _, has := textOnly[0]["images"]; has {
		t.Fatal("text-only user carries an images key")
	}
	imagesToOllamaWire(textOnly) // must not panic
}

// TestImagesAnthropicWire: images become Anthropic image source blocks
// in the user message content; the text becomes a text block.
func TestImagesAnthropicWire(t *testing.T) {
	msgs := []ai.Message{
		ai.NewUserWithImages("look", []ai.ImageContent{
			{MediaType: "image/png", Base64: "iVBORw0KGgo="},
		}),
	}
	// anthropicConvertMessages consumes the messagesToJSON output - the
	// role key comes from that layer, not a bare marshal of the typed
	// messages (ai.User JSON carries no role field)
	maps := messagesToJSON(msgs, false)
	system, result := anthropicConvertMessages(maps)
	_ = system
	if len(result) != 1 || result[0]["role"] != "user" {
		t.Fatalf("result = %v", result)
	}
	content, ok := result[0]["content"].([]any)
	if !ok {
		t.Fatalf("content type = %T, want []any", result[0]["content"])
	}
	if len(content) != 2 {
		t.Fatalf("blocks = %d, want image + text", len(content))
	}
	block, ok := content[0].(map[string]any)
	if !ok || block["type"] != "image" {
		t.Fatalf("first block = %v, want an image block", content[0])
	}
	source, ok := block["source"].(map[string]any)
	if !ok || source["type"] != "base64" || source["media_type"] != "image/png" || source["data"] != "iVBORw0KGgo=" {
		t.Fatalf("source = %v", source)
	}
	text, ok := content[1].(map[string]any)
	if !ok || text["type"] != "text" || text["text"] != "look" {
		t.Fatalf("second block = %v, want a text block", content[1])
	}

	// imageless user stays a plain string
	textOnlyMsgs := []ai.Message{ai.NewUser("hi")}
	maps = messagesToJSON(textOnlyMsgs, false)
	_, result = anthropicConvertMessages(maps)
	if result[0]["content"] != "hi" {
		t.Fatalf("imageless content = %v, want the plain string", result[0]["content"])
	}
}