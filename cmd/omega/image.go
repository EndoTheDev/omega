package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/llm"
)

// parseFileArgs splits args into image content and text prompt.
func parseFileArgs(args []string) (string, []core.ImageContent, error) {
	var promptParts []string
	var images []core.ImageContent
	for _, arg := range args {
		if !strings.HasPrefix(arg, "@") {
			promptParts = append(promptParts, arg)
			continue
		}
		path := arg[1:]
		img, err := llm.DetectImage(path)
		if err != nil {
			return "", nil, err
		}
		if img != nil {
			images = append(images, *img)
			promptParts = append(promptParts, "["+img.MediaType+": "+path+"]")
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", nil, fmt.Errorf("read %s: %w", path, err)
		}
		promptParts = append(promptParts, string(data))
	}
	return strings.Join(promptParts, " "), images, nil
}
