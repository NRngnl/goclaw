# Browser Backends

Goclaw's browser automation tool (`pkg/browser/`) connects to any CDP-compatible browser. Two backends are supported:

| Backend | Image | Overlay | Status |
|---|---|---|---|
| **Chrome** (default) | `chromedp/headless-shell:latest` | `docker-compose.browser.yml` | Stable |
| **Lightpanda** | `lightpanda/browser:latest` | `docker-compose.lightpanda.yml` | Experimental |

## Switching backends

Both overlays set `GOCLAW_BROWSER_REMOTE_URL` to their respective sidecar. Only one should be active at a time.

```bash
# Chrome (default)
docker compose -f docker-compose.yml -f docker-compose.postgres.yml -f docker-compose.browser.yml up -d

# Lightpanda
docker compose -f docker-compose.yml -f docker-compose.postgres.yml -f docker-compose.lightpanda.yml up -d
```

Set `GOCLAW_BROWSER_BACKEND=chrome|lightpanda` to pick the backend explicitly. If unset, goclaw probes `/json/version` on the remote and auto-detects from the `Browser` field.

## Compatibility matrix

| Feature | Chrome | Lightpanda | Notes |
|---|---|---|---|
| Navigate / reload | ✅ | ✅ | |
| AX snapshot (`Accessibility.getFullAXTree`) | ✅ | ⚠️ | Lightpanda upstream bug: returns `nodeId` as a JSON number, but CDP spec defines `AXNodeId` as a string. go-rod's typed decoder rejects it. Tracked as a known gap (see below) |
| Click / type / hover / press | ✅ | ✅ | |
| Wait (text / URL / stable) | ✅ | ✅ | |
| Evaluate JS | ✅ | ⚠️ | Lightpanda upstream bug: go-rod wraps eval JS in a function-apply call (`(function(){…}).apply(this, args)`); Lightpanda's runtime rejects it with "is not a function". Direct `Runtime.evaluate` works; go-rod's `Page.Eval` does not |
| Screenshot (`Page.captureScreenshot`) | ✅ | ❌ | Lightpanda returns a placeholder image. The tool returns an error on Lightpanda directing the agent to use `snapshot` instead |
| Multiple tabs per connection | ✅ | ❌ | Lightpanda: 1 CDP connection = 1 tab. Goclaw opens a fresh connection per tab transparently |
| Browser contexts / incognito | ✅ | Implicit | On Lightpanda every connection is already a fresh browser — isolation is automatic, no `Target.createBrowserContext` multiplexing |
| Cookies / localStorage shared across tabs | ✅ within a context | ❌ | Lightpanda: each tab is a fresh browser. A login on one tab is not visible to another |
| List open tabs from server | ✅ | ❌ | Lightpanda: no `/json/list`. Goclaw tracks tabs in its local map (URL/title cached at OpenTab time, since `page.Info()` is also unreliable post-open) |
| Auto-reconnect on WS drop | ✅ | ❌ | Lightpanda: connection death = that tab is gone server-side. Goclaw drops the tab from the map and surfaces a clear error |

## Known upstream Lightpanda gaps

The two ⚠️ rows above are **Lightpanda-side bugs** that goclaw cannot work around without writing a custom CDP decoder. They prevent the agent's primary "see the page" workflow from functioning on Lightpanda today:

1. **`Accessibility.getFullAXTree` non-conformance.** CDP defines `AXNodeId` as a string; Lightpanda emits it as a JSON number. The `Snapshot` tool action fails with `cannot unmarshal number into Go struct field AccessibilityAXNode.nodes.nodeId of type proto.AccessibilityAXNodeID`.
2. **`Runtime.evaluate` function-wrapper.** go-rod's `page.Eval()` issues `Runtime.callFunctionOn` with a wrapped function expression. Lightpanda's runtime cannot invoke it (`TypeError: ... is not a function`). The `Evaluate` tool action and any internal `WaitStable`/`WaitNavigation` paths that use eval are affected.

Both gaps are exercised by `TestLightpanda_KnownUpstreamGaps` in `tests/integration/browser_lightpanda_test.go` — when Lightpanda fixes them, that test will start logging "remove this gap test" and we can drop the workarounds.

## When to choose which

**Lightpanda:**
- Memory-constrained deployments (desktop / Lite edition, small VPS).
- Stateless workflows — navigate, snapshot, extract, done.
- Cases where per-tab isolation is a feature (every tab is a fresh browser).

**Chrome:**
- Multi-tab flows (OAuth popup → main window, tab-to-tab navigation).
- Long-lived sessions with shared login / cookies / localStorage.
- Screenshot-based workflows.
- Anything requiring full JS engine fidelity.

## References

- Lightpanda: https://lightpanda.io
- Lightpanda + go-rod demos: https://github.com/lightpanda-io/demo/tree/main/rod
- Tracking issue: https://github.com/nextlevelbuilder/goclaw/issues/223
