package agent

// DefaultToolProvider wraps a static tool map. Extension tools are
// merged by the agent on top of these.
type DefaultToolProvider struct {
	ToolsMap map[string]Tool
}

// Tools returns the tool map.
func (d DefaultToolProvider) Tools() map[string]Tool { return d.ToolsMap }
