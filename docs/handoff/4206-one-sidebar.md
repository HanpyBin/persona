# Handoff: one sidebar on iPad and the open iPhone Duo (#4206)

> Temporary. Delete this file and `4206-one-sidebar-prototype.html` before the branch merges.

Branch `feat/4206-one-sidebar`, with origin/main merged in at `53232dd61` and again at `14bf19004` (merges, no rebase; never rebase or force push this branch). Work in progress. It needs the owner's approval before it merges, because it is a UI change.

## What the owner decided

**The problem (#4206).** The open iPhone Duo showed the Assistant | Agents switch at the top centre, where neither thumb reaches it. The rounds below went in this order:

1. **Switch at the foot of the right column.** Settled first, built, then dropped.
2. **Upright stays at the top.** The open Duo held upright keeps the switch at the top, like a plain iPhone.
3. **One sidebar instead of a switch (adopted).** The owner then proposed a single sidebar, like the web app. It applies on **iPad and the open Duo, in both postures**.
4. **Layout follows ChatGPT on the open Duo.** The owner's reference is ChatGPT's open-Duo sidebar:
   - the app's mark and name at the top, with a collapse button;
   - a list of places;
   - a line, then the lists;
   - at the foot, New Chat on the left and one capsule with search and settings on the right.
5. **Rarer places under More.** Meeting Recording, Health and Machines fold into an inline **More** row, so the first screen on its side is not all places.
6. ~~**Recent starts folded on its side.**~~ Superseded on 2026-11-12 (see "Later decisions" 7): there is no Recent.

**Approved prototype:** `docs/handoff/4206-one-sidebar-prototype.html`. It is v3; open it in a browser, and it has device, dark and fold toggles.

**Sidebar, top to bottom:**

1. A header: Persona's mark and name, with a collapse button.
2. The places:
   - Main Chat (with an unread dot);
   - Inbox (with its count);
   - To-dos (with its count);
   - Scheduled Tasks;
   - Knowledge;
   - More, which opens inline to Meeting Recording, Health and Machines.
3. A line.
4. **Pinned** (the directions under their names, with "N working · N need you"), then **All Chats** (over the window). Recent is gone (2026-11-12, decision 7).
5. **Agents**: a heading with ⋯ and New Session, then the existing Agents list (filters, grouping, pinned, machine and repository groups).
6. A fixed foot: **New Chat**, and a capsule with search and Settings.

**Behaviour:**

- There is no Assistant | Agents switch.
- The right column shows Persona's page while no agent is open, and an agent's chat when one is tapped.
- Tapping a Persona row closes the open agent and shows that page.

- On the open Duo **held upright** the column lies **over** the page; see #4852 below.

## Later decisions (owner, 2026-11-11), one by one

1. **Fold button and the page's New Chat on one line.** The column header's fold button and the page's top-right New Chat must sit on the same horizontal line. **Done** (in `79322e978`): `OneSidebarHeader` pads its top by `AssistantTopBar.topInset` and its bottom by `Theme.Space.s`. Checked on the iPad: both span y 67–166 px in a screenshot. `OneSidebarTests.testTheColumnHasThePlacesRecentAndAgents` asserts it (midY within 2 pt) on the iPad and on the Duo on its side. Upright the column is over the page, so the test does not check it there.
2. **#4852: upright overlay with an animated retract.** On the open Duo held upright (669 pt wide):
   - the column lies over the page, and the page keeps its full width. The owner rejected `4724529fb`, which gave the detail a 300 pt minimum so the column pushed the page aside. They said this squeezed both.
   - A pick that changes the page slides the column away with an animation: the main row, a place, a Pinned row or a conversation from All Chats (Recent before decision 7), like the morning brief, Messages, or an agent.
   - Filters and groupings do **not** close it: Needs You / All, and the grouping segments (Project, …).
   - The drag handle on the column's edge is centred vertically.
   - On its side and on the iPad nothing changes; the column stays beside the page.
   - Status: **code committed in this handoff's commit, compiled, NOT yet verified** on the simulator or by UI test (see "Half done").
3. **#4853: the top on its side is too high.** On the open Duo on its side, the top UI sits too close to the top edge, and the display's rounded corner clips the column's avatar/mark at the top left. The owner wants a margin like ChatGPT's app, for the mark and anything else near the top. **Not started.** Likely places:
   - `OneSidebarHeader`'s top padding in `OneSidebar.swift`;
   - `AssistantTopBar.topInset` / height 70 in `AssistantTopBar.swift`;
   - `SidebarTopFill` in `SplitNavigation.swift`.
   Keep decision 1 (one line) true after the change.
4. **#4854: New Chat to the right column on its side.** On the open Duo on its side, move the home page's top-right New Chat (`assistant-new`) into the system's right side-bar column, as ChatGPT does. If the Dynamic Island grows (Live Activity), the button must move rather than sit under it. Read the island or safe area, not a fixed offset. **Not started.** See `docs/apple/ios.md` "iPhone Duo" (`sideBars`, the system's side-bar columns). Also see how the chat already puts its actions there (#3680).
5. **#4855: the iPad column's background.** The owner finds the iPad column's grey-blue ugly and points to ChatGPT's (a near-white/neutral grey). They asked whether the change is global or iPad only. **Conclusion: iPad only. Done** (see #4855 below).
   - The grey-blue on the iPad (glass style) is the system sidebar column's own colour plus `SidebarTopFill` in `SplitNavigation.swift`. That modifier fills the top only when `UIDevice.current.userInterfaceIdiom == .pad`, with `Theme.systemSidebar` = `Color(light: 0xE6E9EE, dark: 0x191C20)`.
   - The open Duo's column is already white.
   - `Theme.systemSidebar` is also the Mac's sidebar colour (`MacRootView`), so **do not change the token**. Give the iPad column its own background (and the matching `SidebarTopFill` colour), e.g. `Theme.Shell.drawer` (0xF9F9F9 / 0x171717), behind an iPad-only check.
   - Show light and dark screenshots before and after.
6. **#3641: two checks upright** (the supervisor's request; do not reply in #3641):
   - **① ‹ on an agent opened from the list must go back, not slide the column out.** By reading the code, `ChatView.backShowsList` was true on the split's first page with the list folded. So upright, ‹ brought the list back instead of going back. **Fixed in code** (`!oneSidebar` added, in this handoff's commit): in the one sidebar, Persona's page is under the agent's, so ‹ goes back to it. Compiled, **not yet run** on the simulator. No UI test yet.
   - **② The new conversation shows Needs You.** On its side, `DuoOpenChromeTests.testTheNewConversationShowsWhatNeedsYou` asserts `shell-needs-you` and passed. **Upright: not yet checked.**

7. **Recent follows #4610 (owner, 2026-11-12).** The column has no Recent: the main row, then **Pinned** (the directions' conversations under their names with "N working · N need you"), then **All Chats**, as every other client. It reuses #4610's iOS data: `AssistantSidebarGroups.directions`, `model.assistantLeaders`, `model.directionNames`, `model.directionCounts`, `DirectionCountsText`; All Chats is `AutoConversationsView` in a sheet, as the drawer's. The on-its-side "Recent starts folded" rule and `sidebarOnItsSide` / `oneSidebar.recentFolded` are gone. In #4852's picks, Recent rows are replaced by Pinned rows and conversations from All Chats. **Done** (see "Done").

On GitHub, #4852–#4855 and #3641 only take `Refs` until a fix is verified (no `Fixes` for unfinished work). Do **not** comment in #4206, #3641 or #4852–#4855.

## Done (pushed)

- `53232dd61`: merge of origin/main (conflict in `AssistantView.swift`: kept the one-sidebar props, dropped main's removed `switcherHeight`).
- `79322e978`, the feature:
  - one sidebar on iPad and the open Duo;
  - grouping as segments (`AgentsGroupingSegments` in `AgentsMenus.swift`, `DevGrouping.segments/.segment` in `ChatsView.swift`, strings `agents.grouping.segment.*`);
  - Messages under More when `LarkHomePreference.shared.shows` (`OneSidebarHome` shows `LarkConversationsView` while `selectedTab == .feishu`);
  - the open agent's row highlighted;
  - `MainChatRowMenu` on the main row;
  - dead code removed: the page-tab switch, `MainChatPlaceItems`, collapse/footer search in `AssistantSidebar.swift`, the iOS `FeishuRootTab` (now visionOS only);
  - `PadRouter.handled` serial;
  - `SplitNavigation`'s ZStack, so Main Chat closes an agent: the stack stays under `home` at opacity 0, without hit testing and hidden from accessibility;
  - tests: `OneSidebarTests` (new), `DuoOpenChromeTests` (switch tests removed; the face is `persona-avatar` or `assistant-menu`), `IPadSidebarTests`, `SidebarMetricsTests` (adds `OneSidebar.swift`);
  - `docs/apple/ios.md` "One sidebar".
- `14bf19004`: second merge of origin/main (138 commits). Conflict in `ChatsView.swift`: kept the branch's `openBeside` (main's By Repository rule plus the one sidebar). Main removed `AppModel.showsAssistantIdentity` (#4712): `OneSidebarHeader` now checks `model.managerRoster?.isMulti == true`, as the column's header does.
  - **Open question for the owner (not changed):** main's #4610 removed Recent from Persona's chats on every client (main chat + Pinned leads, then All chats). The one sidebar still has its own Recent (three rows, then All Chats), as the approved prototype has. Reported to the supervisor; keep it until the owner says otherwise.
- `4724529fb`: upright tiling (300 pt detail minimum). **Rejected by the owner and reverted** by this handoff's commit.
- This handoff's commit (see `git log`). It contains #4852 and #3641 ① as described above, compiled only.

## Step 2 and #4853 / #4854 / #4855 (this session, 2026-11-12)

- **Pinned instead of Recent** (decision 7): `OneSidebarSections.pinned` / `pinnedRow` / `allChatsRow` in `OneSidebar.swift`; `RootTabs` no longer reads the posture for Recent.
- **Messages upright**: after its pick slid the column away there was no way back but the edge handle (its own stack has no system button). `OneSidebarHome` now shows Show Sidebar (`feishu-sidebar-button`) at its top corner while the column is away.
- **#4853**: `RootTabs` reads the system's bar edge at the root (`readsVerticalBarEdge`, as the shell does) and, under side bars (the open Duo on its side), gives both columns `safeAreaPadding(.top, OneSidebarMetrics.topMargin)` = `Theme.Space.l`. Both columns move together, so decision 1 holds; content insets and the fog follow the safe area.
- **#4854**: under side bars beside the one sidebar, `AssistantView` shows the system's navigation bar with New Chat as a `.topBarTrailing` item (title removed), so the system places it in its side column under the clock and the camera and moves it with the Dynamic Island; `AssistantTopBar(newInSideBar:)` leaves its own out (an empty spot keeps the face where it was). The New Chat action is `AssistantView.newConversationAction` (Health's page in a health conversation, as before).
  - **Decision 1 on its side** now reads: the fold and the page's top (the face) start on one line; New Chat is in the side column. The test asserts `face.minY == collapse.minY`.
- **#4855**: on the iPad with the glass style, the column (`OneSidebarColumnColor`: list background hidden, `Theme.Shell.drawer` behind), its header and foot (`OneSidebarMetrics.chromeBackground`) and the status-bar strip above it (`SplitNavigation.topFillColor` into `SidebarTopFill(color:)`) are one neutral color: F9F9F9 light, 171717 dark. Before, the list was `Theme.systemSidebar` (E6E9EE / 191C20) and the header, foot and strip were white / black. `Theme.systemSidebar` is unchanged (the Mac and the open Duo keep it). Shots: `~/Documents/persona-4206-shots/4855-{before,after}-{light,dark}.png` (iPad, zh-Hans, glass).
- **Tests**:
  - `OneSidebarTests`: `testTheColumnHasThePlacesPinnedAndAgents` (Pinned / All Chats, no Recent; on its side the margin, the face line and New Chat in the side column), `testUprightAPickSlidesTheColumnAwayAndFiltersDoNot`, `testUprightBackFromAnAgentGoesToPersonasPage` (#3641 ①; opens the agent from a Needs You card, since synthetic drags miss upright), and the older tests adapted upright (the column slides away after a pick; brought out from the page's button, the system's Show Sidebar or `feishu-sidebar-button`). The mock hub must run with `MOCK_LEADERS=1`.
  - `DuoOpenChromeTests`: upright aware (the column is brought out in setUp, folded before the face is tapped; Settings' × upright is in the top bar's trailing items, left of the display's top-right corner). `testTheNewConversationShowsWhatNeedsYou` passes upright (#3641 ②).

## Status of the runs

- Duo upright: `OneSidebarTests` + `DuoOpenChromeTests` all pass (the last two fixed in later runs: ‹ from an agent, #3641 ①; the main row's and the face's menus).
- iPad: `OneSidebarTests` (8 pass, the 2 upright-only skip), `IPadSidebarTests`, `SplitDividerTests` (2) and `PersonaTests/SidebarMetricsTests` (5) pass. `SidebarMetricsTests` first caught the Pinned heading (a `.frame` between its padding and `minHeight`), fixed.
- **The handle's closed state was not kept on the iPad** (found while checking step 2 ④): the split lays itself out first with a ~117 pt list and the page sliding in from before it, so `notePosture` read it as laid over (`overlays`), the close was not written, and the split put the list back out right after `onAppear` closed it. Fixed in `SplitNavigation`: the split starts from the kept state (`SplitDividerHandle.startsClosed`), and the posture is only read once the list is at least `sidebarMinPhone` wide and the page does not start before the split. `SplitDividerTests.testDraggedPastItsLeastTheListClosesAndComesBackFromTheEdge` now relaunches and checks the list stays closed. `testTheAssistantsHistoryIsDraggedToo` is removed: Persona's page has no history column of its own beside the one sidebar.
- Duo on its side: (not yet)

## Not done

- Runs on the Duo on its side and on the iPad (`OneSidebarTests`, `DuoOpenChromeTests`; `OneSidebarTests`, `IPadSidebarTests`, `PersonaTests/SidebarMetricsTests`), and checking the handle still opens / closes and remembers its state there.
- Merge origin/main once more before the final screenshots (the supervisor: main moved again; no rush before that).
- **Screenshots for the owner, light and dark**: iPad, Duo on its side, Duo upright, into `~/Documents/persona-4206-shots/` (`ipad-{light,dark}.png`, `duo-landscape-{light,dark}.png`, `duo-portrait-{light,dark}.png`; the old `duo-landscape-light.png` is stale, overwrite it), plus #4855 before / after. Report the paths to the supervisor.
- The PR, then **stop and wait for the owner's explicit approval**. After approval: delete `docs/handoff/4206-*`, then merge with `scripts/hooks/merge-pr.sh`. Do not merge before approval.

## Environment (this Mac mini)

- **Simulators**
  - Duo: `Persona Duo 4206`, UDID `125B7423-F653-4043-A8D6-6F5A0637FF1E`, iOS 27.1. 27.1 only has the Duo.
  - iPad: iPad Pro 13-inch (M5), UDID `C0FF6FB0-FB9B-493A-8E9A-EE6D77CE3F6C`, iOS 27.0.
  - Created with `scripts/app/sim-boot.sh --profile stock --device-type … --runtime … '<name>'`.
- **Config**: `~/.config/persona/config.json` (private, 0600, minimal: dev bundle id `<dev bundle id>`, simulator `Persona Duo 4206`). Then `eval "$(python3 scripts/persona_config.py env)"` and `python3 scripts/persona_config.py xcconfig`. After adding a file: `cd apple && xcodegen generate -q` (xcodegen from brew).
- **Mock hub**: running with leaders (port 18842, log `/tmp/persona-4206-mock.log`). If it is gone, restart it from the repo root with `MOCK_LEADERS=1 PORT=18842 nohup python3 scripts/mock/mock_hub.py > /tmp/persona-4206-mock.log 2>&1 &` (stop it by its port's pid, never `pkill -f mock_hub.py`).
- **Build** (from `apple/`). SwiftPM's GitHub clones time out here without the local proxy:
  ```sh
  https_proxy=http://127.0.0.1:7897 http_proxy=http://127.0.0.1:7897 xcodebuild -project Persona.xcodeproj -scheme Persona \
    -destination 'platform=iOS Simulator,id=<UDID>' -derivedDataPath ../build/DerivedData build-for-testing
  ```
- **UI tests**:
  ```sh
  TEST_RUNNER_PERSONA_MOCK_HUB=http://127.0.0.1:18842 [TEST_RUNNER_PERSONA_ORIENTATION=landscape] xcodebuild -project Persona.xcodeproj -scheme Persona \
    -destination 'platform=iOS Simulator,id=<UDID>' -derivedDataPath ../build/DerivedData test-without-building \
    -only-testing:PersonaUITests/OneSidebarTests -only-testing:PersonaUITests/DuoOpenChromeTests \
    [-only-testing:PersonaUITests/IPadSidebarTests -only-testing:PersonaTests/SidebarMetricsTests] -resultBundlePath /tmp/<name>.xcresult
  ```
  - The Duo tests need the Duo **open** in DeviceHub.
  - `ORIENTATION=landscape` is for on its side. For upright, rotate the Duo in DeviceHub and leave the variable out.
- **Manual run**:
  - `xcrun simctl install <UDID> build/DerivedData/Build/Products/Debug-iphonesimulator/Persona.app`
  - `xcrun simctl launch <UDID> <dev bundle id> -AppleLanguages '(zh-Hans)' -AppleLocale zh_CN -pairURL 'persona://pair?url=http%3A%2F%2F127.0.0.1%3A18842&token=mock' -tab assistant -app.tab assistant -dev.openChat '' -dev.grouping repo -agents.list.closed NO -ui.style glass`
  - Appearance: `xcrun simctl ui <UDID> appearance light|dark`.
  - If the column stays closed: `xcrun simctl spawn <UDID> defaults write <dev bundle id> agents.list.closed -bool NO`.
- **Screenshots**: `xcrun simctl io <UDID> screenshot --display=3 f.png` for the Duo's inner (open) display. Plain `screenshot` works for the iPad. This needs no screen-recording permission.
- **DeviceHub (Xcode's device window), by System Events** (Accessibility is granted). Send one call at a time and don't retry in a loop. Never restart herdr or Ghostty: that stops every agent on this machine.
  - Select the Duo: `tell application "System Events" to tell process "DeviceHub" to set selected of row 3 of outline 1 of scroll area 1 of group 1 of splitter group 1 of group 1 of splitter group 1 of group 1 of window 1 to true`. The window title shows the selected device.
  - The buttons are in `group 3 of splitter group 1 of group 1 of splitter group 1 of group 1 of window 1`:
    - posture: `Closed` / `Partially Open` / `Open`;
    - rotate: the button whose `help` is `Rotate Right`.
    - Click one by looping over `every button of` that group and matching `help`. `/tmp/p4206/rotate.applescript` does this but may be gone.
  - The Duo is currently **open and upright**.
- **Latest screenshots** (`/tmp/p4206/`, scratch):
  - `up1.png`: Duo upright, this build's overlay; not yet assessed.
  - `ipad-now.png`: iPad, header aligned.
  - `duo-land-tile4.png`: Duo on its side.
  - `duo-port-light2.png`: the old upright overlay, before the retract.
  - `duo-port-290.png` / `duo-up-tile*.png`: the rejected tiling.

## Pitfalls

- **Upright on the open Duo, synthetic drags miss** (swipes and press-and-drag do nothing): reach rows below the first screen by tapping an element that exists (XCUITest scrolls it in first). The column's agent rows are not built until scrolled to, so the upright test opens an agent from a Needs You card instead.
- **Upright, the column may start away** right after an install; tests bring it out from the page's button (`assistant-sidebar-button`), the system's Show Sidebar (a page pushed on Persona's page) or `feishu-sidebar-button` (Messages).
- `xcodebuild test-without-building` can sit for minutes after the last test before it exits; run it in the background and wait on `pgrep -f xcodebuild`.
- **`DuoOpenChromeTests` face**: the mock hub has several assistants, so the page's face is `persona-avatar`, not `assistant-menu`. Match either: `identifier IN {assistant-menu, persona-avatar}`.
- **`apple/Persona.xcodeproj/` is untracked** (generated by xcodegen). Never `git add` it; stage files by path. A new test file does not run until `xcodegen generate`.
- An agent chat's title is `chat.title` or `chat.compactTitle`. Agent rows can be offscreen in the column, so swipe up to find them.
- The Messages list's frame starts at the window edge, so check its first static text's `minX`, not the list's.
- **main's `apple/PersonaTests/JSONDateTests.swift` (#4931) does not compile with Xcode 27.1** (static `ISO8601DateFormatter`s are not concurrency-safe), so `build-for-testing` fails. Locally it carries an **uncommitted** `nonisolated(unsafe)` on its two statics; never stage it (stage by path). The supervisor: CI builds with Xcode 26.0.1, so only newer local Xcode fails and main's CI is not blocked; keep the local patch, do not commit it into #4206, no issue.
- **Pre-existing check failures, also on origin/main, not ours**:
  - `test_apple_perf_patterns` (`AppModel+Managers.swift` file read);
  - `test_apple_state_initial_values` (`AssistantSwitcher.swift` `ManagersModel`).
  - `test_doc_paths` fails only while this handoff exists, which is expected; it goes when the handoff is deleted.
- Commits: `git -c user.name=zake111000 -c user.email=14012239+zake111000@users.noreply.github.com commit`, English, ending with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. No internal hosts or names in public text.
