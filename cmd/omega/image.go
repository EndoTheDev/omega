package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/EndoTheDev/omega/ai"
)

// parseFileArgs splits args into image content and text prompt. Args
// starting with "@" are treated as file references: image files are
// loaded as base64 ImageContent, text files are inlined into the prompt.
// Non-file args become the text prompt.
func parseFileArgs(args []string) (string, []ai.ImageContent, error) {
	var promptParts []string
	var images []ai.ImageContent
	for _, arg := range args {
		if !strings.HasPrefix(arg, "@") {
			promptParts = append(promptParts, arg)
			continue
		}
		path := arg[1:]
		img, err := ai.DetectImage(path)
		if err != nil {
			return "", nil, err
		}
		if img != nil {
			images = append(images, *img)
			promptParts = append(promptParts, "["+img.MediaType+": "+path+"]")
			continue
		}
		// Not an image: inline the file contents into the prompt.
		data, err := os.ReadFile(path)
		if err != nil {
			return "", nil, fmt.Errorf("read %s: %w", path, err)
		}
		promptParts = append(promptParts, string(data))
	}
	return strings.Join(promptParts, " "), images, nil
}
