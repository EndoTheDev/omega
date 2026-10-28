package tui

import (
	"context"

	"github.com/EndoTheDev/omega/core"
)

// minimalStore is a StoreProvider test double shared by tui tests
// (variant tests and any store-backed flow). Every method is a no-op;
// tests needing configurable behavior wrap their own.
type minimalStore struct{}

func (m *minimalStore) Open(string) error                                           { return nil }
func (m *minimalStore) Close() error                                                { return nil }
func (m *minimalStore) CreateSession(context.Context, string, string, string) error { return nil }
func (m *minimalStore) GetSession(context.Context, string) (core.Session, error) {
	return core.Session{}, nil
}
func (m *minimalStore) ListSessions(context.Context) ([]core.Session, error) { return nil, nil }
func (m *minimalStore) DeleteSession(context.Context, string) error          { return nil }
func (m *minimalStore) UpdateSession(context.Context, string, string) error  { return nil }
func (m *minimalStore) AppendMessage(context.Context, string, core.Message) error {
	return nil
}
func (m *minimalStore) GetMessages(context.Context, string) ([]core.Message, error) {
	return nil, nil
}
func (m *minimalStore) GetMessagesWithTimes(context.Context, string) ([]core.StoredMessage, error) {
	return nil, nil
}

var _ core.StoreProvider = (*minimalStore)(nil)

func (m *minimalStore) GetActiveView(context.Context, string) (core.ActiveView, error) {
	return core.ActiveView{}, nil
}
func (m *minimalStore) GetActiveMessages(context.Context, string) ([]core.Message, error) {
	return nil, nil
}
func (m *minimalStore) GetSessionTree(context.Context) ([]*core.SessionNode, error) {
	return nil, nil
}
func (m *minimalStore) GetAncestorMessages(context.Context, string) ([]core.Message, error) {
	return nil, nil
}
func (m *minimalStore) SearchMessages(context.Context, string) ([]core.SearchResult, error) {
	return nil, nil
}
func (m *minimalStore) SearchSessions(context.Context, string) ([]core.SearchResult, error) {
	return nil, nil
}
func (m *minimalStore) ComputeInsights(context.Context, int) (*core.Insights, error) {
	return nil, nil
}
func (m *minimalStore) CountMessages(context.Context, string) (int, error) { return 0, nil }
func (m *minimalStore) SessionCounts(context.Context) (map[string]core.SessionCounts, error) {
	return nil, nil
}
func (m *minimalStore) AddSessionTokens(context.Context, string, int, int) error { return nil }
func (m *minimalStore) SetSessionTitle(context.Context, string, string, bool) error {
	return nil
}
func (m *minimalStore) SetSessionLastRun(context.Context, string, *core.LastRunStats) error {
	return nil
}
func (m *minimalStore) SetSessionPinned(context.Context, string, bool) error { return nil }
func (m *minimalStore) SetSessionRead(context.Context, string) error         { return nil }
func (m *minimalStore) SetSessionArchived(context.Context, string, bool) error {
	return nil
}
func (m *minimalStore) SessionModels(context.Context) (map[string]string, error) {
	return nil, nil
}