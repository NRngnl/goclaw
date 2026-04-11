package browser

import (
	"context"
	"fmt"
	"time"

	"github.com/go-rod/rod/lib/proto"
)

// Snapshot takes an accessibility snapshot of a page.
func (m *Manager) Snapshot(ctx context.Context, targetID string, opts SnapshotOptions) (*SnapshotResult, error) {
	tenantID := tenantIDFromCtx(ctx)
	m.mu.Lock()
	page, err := m.getPageForTenant(targetID, tenantID)
	m.mu.Unlock()

	if err != nil {
		return nil, err
	}

	result, err := proto.AccessibilityGetFullAXTree{}.Call(page)
	if err != nil {
		return nil, fmt.Errorf("get AX tree: %w", err)
	}

	// Resolve actual targetID — agent may omit it (single-tab mode),
	// but refs must be stored under the real page ID so act() can find them.
	resolvedTargetID := targetID
	if resolvedTargetID == "" {
		resolvedTargetID = string(page.TargetID)
	}

	snap := FormatSnapshot(result.Nodes, opts)
	info, _ := page.Info()
	snap.TargetID = resolvedTargetID
	if info != nil {
		snap.URL = info.URL
		snap.Title = info.Title
	}

	// Cache refs under resolved targetID
	m.refs.Store(resolvedTargetID, snap.Refs)

	return snap, nil
}

// Screenshot captures a page screenshot as PNG bytes.
func (m *Manager) Screenshot(ctx context.Context, targetID string, fullPage bool) ([]byte, error) {
	tenantID := tenantIDFromCtx(ctx)
	m.mu.Lock()
	page, err := m.getPageForTenant(targetID, tenantID)
	m.mu.Unlock()

	if err != nil {
		return nil, err
	}

	// Scope the page context to the caller's context so screenshots respect
	// the action timeout. Without this, page uses lifecycleCtx (never cancels)
	// and fullPage screenshots on large pages can hang indefinitely.
	scopedPage := page.Context(ctx)

	if fullPage {
		return scopedPage.Screenshot(fullPage, &proto.PageCaptureScreenshot{
			Format: proto.PageCaptureScreenshotFormatPng,
		})
	}
	return scopedPage.Screenshot(false, nil)
}

// Navigate navigates a page to a URL.
func (m *Manager) Navigate(ctx context.Context, targetID, url string) error {
	tenantID := tenantIDFromCtx(ctx)
	m.mu.Lock()
	page, err := m.getPageForTenant(targetID, tenantID)
	m.mu.Unlock()

	if err != nil {
		return err
	}

	scopedPage := page.Context(ctx)
	if err := scopedPage.Navigate(url); err != nil {
		return fmt.Errorf("navigate: %w", err)
	}
	if err := scopedPage.WaitStable(300 * time.Millisecond); err != nil {
		return fmt.Errorf("wait stable after navigate: %w", err)
	}
	return nil
}

// Close shuts down the browser if running.
func (m *Manager) Close() error {
	return m.Stop(context.Background())
}

// Refs returns the RefStore for external use (e.g. actions).
func (m *Manager) Refs() *RefStore {
	return m.refs
}
