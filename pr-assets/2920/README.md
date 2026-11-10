# PR #2920 screenshot evidence

All data is synthetic. This branch contains screenshot artifacts only and must not be merged into main. The feature PR and main contain no added screenshots.

## Provenance and limitations

- Final feature source: `50891f7a74f2af9ac9547082ae5af4d5ead4ec17`.
- Android: Compose/Robolectric **Native JVM** rendering of the real ChatScreen/ChatPage and message/feed renderers, SDK 35. These are not emulator or physical-device screenshots. The 8 tray and 6 conversation screenshot tests were forcibly rerun after final verification.
- Android counts: 30 tray captures, 36 conversation/audit/locale captures; all three styles in light and dark. Before captures reuse the retained legacy message/feed renderers with the same synthetic transcript, not an old production APK.
- A bounded software-emulator attempt remained unusable after Launcher/System UI ANRs, Wait/Launcher retries and cold boot. The actual display remained in startup and diagnostic APK installation timed out. Failed startup/ANR frames are not published as product evidence.
- Web: 24 real-browser captures of the built application, using synthetic HTTP/WebSocket data, widths 1280 and 390, light and dark. The build and browser verification were performed at `008c50592ff26e620cbb016311c1220b06ceffea`; the final feature commit differs only by an Android test-environment lesson, not application source.
- No production deployment, signed Android release, physical-device validation or production package publication was performed.

## Android: all five tray states

Each contact sheet contains light/dark collapsed, Pending, Progress, Agents and cleared/gray Pending. Original full-size PNGs are alongside this file, named `android-trays-<state>-<style>-<appearance>.png`.

| Style | Full matrix |
| --- | --- |
| Glass | [Light/dark matrix](contact-android-trays-glass.png) |
| Brief | [Light/dark matrix](contact-android-trays-brief.png) |
| Board | [Light/dark matrix](contact-android-trays-board.png) |

## Android: conversation before/after and manual inspection

Each sheet contains before, after and manually expanded tools in light/dark. Original PNGs are `android-conversation-<phase>-<style>-<appearance>-<locale>.png`; the artifact directory also includes English/Japanese captures and the actual audit dialog's window-decor captures.

| Style | Comparison |
| --- | --- |
| Glass | [Before/after/expanded](contact-android-conversation-glass.png) |
| Brief | [Before/after/expanded](contact-android-conversation-brief.png) |
| Board | [Before/after/expanded](contact-android-conversation-board.png) |

## Web browser evidence

Original PNGs are `web-<width>-<appearance>-<state>.png`. States are collapsed, Pending, owner Pending, Progress, Agents and a live accepted task row.

| Viewport | Light | Dark |
| --- | --- | --- |
| Narrow Progress | [390 light](web-390-light-progress.png) | [390 dark](web-390-dark-progress.png) |
| Narrow live acceptance | [390 light](web-390-light-accepted-live.png) | [390 dark](web-390-dark-accepted-live.png) |
| Wide Pending | [1280 light](web-1280-light-pending.png) | [1280 dark](web-1280-dark-pending.png) |
| Wide Agents | [1280 light](web-1280-light-agents.png) | [1280 dark](web-1280-dark-agents.png) |

[Browse all original captures and contact sheets](./).

## Verification retained with the feature PR

- Android complete local CI: 4,281 tests, 4,272 passed and 9 skipped; zero failures/errors. All-module lint, translation/token checks and debug APK build passed. Forced Native screenshot tests: 14 passed.
- The preceding Android resource failure is retained: 115 Settings failures due to the default temporary filesystem running out of space. The complete rerun passed with a task-owned temporary directory applied to every JVM through `JAVA_TOOL_OPTIONS`; no tests were excluded and no baselines were raised.
- Web full local CI: 7,411 passed, 1 skipped; lint/typecheck/build/privacy and wide/narrow light/dark browser interaction verification passed. The existing build chunk-size warning remains.
- Server manager tests with Postgres 18 and server/Windows builds, vet, schema/SDK and Go SDK tests passed.
- Privacy, documentation and synthetic-mock checks passed. Hosted CI was not used as the merge gate.
