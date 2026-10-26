package delegate

import (
	"context"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// TestCompile verifies the package compiles and the interfaces are satisfied.
func TestCompile(t *testing.T) {
	// ToolProvider interface.
	var _ core.ToolProvider = (*Delegate)(nil)
	// Plugin interface.
	var _ agent.Plugin = (*Plugin)(nil)
}

// TestTools verifies both tools are registered with correct names.
func TestTools(t *testing.T) {
	d := NewDelegate()
	tools := d.Tools()
	if _, ok := tools["delegate.task"]; !ok {
		t.Fatal("missing delegate.task tool")
	}
	if _, ok := tools["delegate.status"]; !ok {
		t.Fatal("missing delegate.status tool")
	}
	if tools["delegate.task"].Description == "" {
		t.Fatal("delegate.task has empty description")
	}
}

// TestDelegateStatus verifies delegate.status returns correct output
// when no tasks are running.
func TestDelegateStatus(t *testing.T) {
	d := NewDelegate()
	out, err := d.runDelegateStatus(context.Background(), nil)
	if err != nil {
		t.Fatalf("runDelegateStatus: %v", err)
	}
	if !strings.Contains(out, "Running: 0") {
		t.Fatalf("expected 'Running: 0', got %q", out)
	}
	if !strings.Contains(out, "No tasks") {
		t.Fatalf("expected 'No tasks', got %q", out)
	}
}

// TestPendingCount verifies pending count is 0 with no tasks.
func TestPendingCount(t *testing.T) {
	d := NewDelegate()
	if c := d.PendingCount(); c != 0 {
		t.Fatalf("expected 0 pending, got %d", c)
	}
}

// loggingPlugin satisfies the declared Requires edge in the mount
// pins: it provides the logging seam (a nil-op logger).
type loggingPlugin struct{}

func (loggingPlugin) Name() string       { return "logging" }
func (loggingPlugin) Provides() []string { return []string{"logging"} }
func (loggingPlugin) Requires() []string { return nil }
func (loggingPlugin) Mount(ctx *core.Context) error {
	ctx.Logger = stubLogger{}
	return nil
}

// stubLogger is the minimal core.LoggerProvider.
type stubLogger struct{}

func (stubLogger) Printf(format string, args ...any) {}
func (stubLogger) Errorf(format string, args ...any) {}
func (stubLogger) Close() error                      { return nil }

// TestPluginMount verifies the api mount populates Context correctly -
// the ExtensionAPI path is delegate's only mount since the v0.12
// phase-C2 migration.
func TestPluginMount(t *testing.T) {
	p := NewPlugin()
	if p.Name() != "delegate" {
		t.Fatalf("expected name 'delegate', got %q", p.Name())
	}
	plugins := []agent.Plugin{loggingPlugin{}, p}
	ctx := &core.Context{}
	if _, err := agent.MountAllAPI(plugins, ctx, nil); err != nil {
		t.Fatalf("api mount: %v", err)
	}
	if len(ctx.ToolProviders) != 1 {
		t.Fatalf("expected 1 tool provider, got %d", len(ctx.ToolProviders))
	}
	tools := ctx.ToolProviders[0].Tools()
	if _, ok := tools["delegate.task"]; !ok {
		t.Fatal("delegate.task not registered")
	}
	if _, ok := tools["delegate.status"]; !ok {
		t.Fatal("delegate.status not registered")
	}
	if _, ok := tools["delegate"]; ok {
		t.Fatal("the unprefixed consolidated delegate tool must be retired")
	}
	if ctx.InjectedMessages == nil {
		t.Fatal("InjectedMessages not set")
	}
	if ctx.PendingDelegations == nil {
		t.Fatal("PendingDelegations not set")
	}
	if ctx.PendingDelegations() != 0 {
		t.Fatalf("expected 0 pending, got %d", ctx.PendingDelegations())
	}
	// the logger capture still happens at mount time (the logging edge)
	if p.delegate.logger == nil {
		t.Fatal("mount did not capture the logger")
	}
	// the classic mount path is dead: reaching it fails loud
	if err := p.Mount(ctx); err == nil {
		t.Fatal("the retired classic mount must fail loud")
	}
}

// stubNotifier is the minimal core.NotificationsProvider for the
// emitter-delivery pin.
type stubNotifier struct{}

func (stubNotifier) Emit(core.Notification) (core.Notification, error) {
	return core.Notification{}, nil
}
func (stubNotifier) List(context.Context, bool, int, int) ([]core.Notification, int, error) {
	return nil, 0, nil
}
func (stubNotifier) MarkRead(context.Context, int64) error { return nil }
func (stubNotifier) MarkAllRead(context.Context) error     { return nil }
func (stubNotifier) Subscribe() (<-chan core.Notification, func()) {
	ch := make(chan core.Notification)
	return ch, func() {}
}

// TestEmitterDeliversSeamAtMountEnd: with the notifications module
// mounted, the registered emitter hands the seam to the delegate after
// the mount completes (the capture is gone; the hook delivered).
func TestEmitterDeliversSeamAtMountEnd(t *testing.T) {
	p := NewPlugin()
	ctx := &core.Context{}
	plugins := []agent.Plugin{loggingPlugin{}, stubNotificationsPlugin{}, p}
	if _, err := agent.MountAllAPI(plugins, ctx, nil); err != nil {
		t.Fatalf("api mount: %v", err)
	}
	if p.delegate.notifier == nil {
		t.Fatal("the delivered notifications seam did not land on the delegate")
	}
}

// stubNotificationsPlugin provides the notifications seam via a stub
// store so the emitter delivery has something to hand over.
type stubNotificationsPlugin struct{}

func (stubNotificationsPlugin) Name() string       { return "notifications" }
func (stubNotificationsPlugin) Provides() []string { return []string{"notifications"} }
func (stubNotificationsPlugin) Requires() []string { return nil }
func (stubNotificationsPlugin) Mount(ctx *core.Context) error {
	ctx.Notifications = stubNotifier{}
	return nil
}
