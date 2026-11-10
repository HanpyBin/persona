# PR 2999 screenshots

Evidence for the web Assistant tray entries drawn as the old dock capsules. Real Chromium, production builds, synthetic
HTTP/WebSocket data from `web/e2e/assistant-task-trays.py`; no real accounts or messages.

- `wide-*`, `phone-*`: the PR head at 1280 px and 390 px, light and dark, collapsed and with a tray open (selected capsule).
- `height-comparison-*`: the same data in three builds, top to bottom: before #2920 (old All / task chips), current `main`
  (#2920 entries), this PR. Old All chip 32 px, #2920 entry 36 px, new entries 32 px.
