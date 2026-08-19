package agent

// NewRegistry returns an empty tool map. Built-in tools are now
// provided by the core-tools extension. This function is kept for
// API compatibility but no longer registers any tools.
func NewRegistry() map[string]Tool {
	return map[string]Tool{}
}
