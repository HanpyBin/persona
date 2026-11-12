# Handoff: one sidebar on iPad and the open iPhone Duo (#4206)

> Temporary. Delete this file and `4206-one-sidebar-prototype.html` before the branch merges.

Branch `feat/4206-one-sidebar`. origin/main has been merged in four times (`53232dd61`, `14bf19004`, `dde9de38e`, `90d6a3b79`): merges only, **never rebase or force push this branch**. The work moves to another machine here; this file is meant to be enough to carry on without the earlier conversation. It is a UI change: **the owner must approve light / dark screenshots before it merges**.

## What is being built

#4206: the open iPhone Duo showed an Assistant | Agents switch at the top centre, out of either thumb's reach. The owner chose **one sidebar instead of a switch**, like the web app, on **iPad and the open Duo, in both postures** (on its side and upright). Approved prototype: `docs/handoff/4206-one-sidebar-prototype.html` (v3; open it in a browser; device, dark and fold toggles). The layout follows ChatGPT's on the open Duo.

The column, top to bottom:

1. Header: Persona's mark and name, and a fold button.
2. Places: Main Chat (unread dot), Inbox (count), To-dos (count), Scheduled Tasks, Knowledge, and **More**, which opens inline to Meeting Recording, Health, Machines (and Messages while it is switched on).
3. A line.
4. **Pinned**: the directions' conversations under their names, with "N working · N need you"; then **All Chats** (a sheet). This follows #4610 (owner, decision 7 below); there is no Recent.
5. **Agents**: a heading with ⋯ and New Session, then the Agents list (filters, grouping, pinned, machine and repository groups).
6. A fixed foot: **New Chat**, and a capsule with Search and Settings.

The right column shows Persona's page while no agent is open, and an agent's chat when one is tapped; a Persona row closes the open agent. There is no Assistant | Agents switch. `docs/apple/ios.md` "One sidebar" describes the shipped behaviour and is kept up to date.

### The owner's later decisions

1. **The fold button and the page's top on one line** (`OneSidebarHeader`'s top padding against `AssistantTopBar.topInset`). On its side the page's New Chat moved into the system's side column (#4854), so there it reads: the fold and the page's face start on one line.
2. **#4852, upright:** on the open Duo upright (669 pt wide) the column lies **over** the page (the page keeps its full width; a rejected try, `4724529fb`, gave the detail a 300 pt minimum and was reverted). A pick that changes the page (the main row, a place, a Pinned row, a conversation from All Chats, an agent) slides the column away with an animation; filters and groupings do not. The drag handle on the column's edge is centred vertically. On its side and on the iPad the column stays beside the page.
3. **#4853:** on its side the top was too close to the screen's edge (the rounded corner clipped the mark). It needs a margin like ChatGPT's, keeping decision 1.
4. **#4854:** on its side, the page's top-right New Chat (`assistant-new`) goes into the system's right side column, and moves with the Dynamic Island (read the island / safe area, no fixed offset).
5. **#4855:** the iPad column's grey-blue is replaced with a neutral grey, **iPad only**. `Theme.systemSidebar` is the Mac's sidebar colour too and **must not change**.
6. **#3641, two checks upright:** ① ‹ on an agent opened from the column goes back to Persona's page (it used to slide the column out instead); ② the new conversation shows what needs you (`shell-needs-you`).
7. **Recent follows #4610:** no Recent; Pinned (with its counts) and All Chats, from #4610's iOS data (`AssistantSidebarGroups.directions`, `model.assistantLeaders`, `model.directionNames`, `model.directionCounts`, `DirectionCountsText`; All Chats is `AutoConversationsView` in a sheet). The old "Recent starts folded on its side" rule is dropped.

On GitHub #4852–#4855 and #3641 take **`Refs` only, never `Fixes`**. **Do not comment** in #4206, #3641 or #4852–#4855.

## Status by issue (at `40d04943f`, the last code commit)

| Item | Status | Where, and how it was verified |
|---|---|---|
| #4852 upright overlay, retract on picks, filters stay, centred handle | Done and verified | `SplitNavigation` (overlay posture, `hideOverlay`), `SplitDividerHandle`. `OneSidebarTests.testUprightAPickSlidesTheColumnAwayAndFiltersDoNot` passes on the Duo upright, also on the narrow-column fallbacks. Upright only by design; on its side and on the iPad the other tests pass with the column beside the page. |
| #3641 ① ‹ goes back to Persona's page | Done; verified up to `90d6a3b79`, **not re-verified on `40d04943f`** | `ChatView.backShowsList` (`!oneSidebar`). `testUprightBackFromAnAgentGoesToPersonasPage` passed on the Duo upright on `dde9de38e`, `a928f17c2`, `90d6a3b79`; on `40d04943f` it fails **before** it reaches ‹ (see "The open failure"). The ‹ code has not changed since. |
| #3641 ② new conversation shows Needs You upright | Done and verified | `DuoOpenChromeTests.testTheNewConversationShowsWhatNeedsYou` passes on the Duo upright (`dde9de38e`, `a928f17c2`, `90d6a3b79`, and on `40d04943f`'s code) and on its side. |
| #4853 top margin on its side, one line kept | Done and verified on its side up to `a928f17c2` | Per-column `safeAreaPadding(.top, topPad(…))` in `SplitNavigation`, at least `OneSidebarMetrics.topMargin` (`Theme.Space.l`). `testTheColumnHasThePlacesPinnedAndAgents` asserts the margin and `face.minY == collapse.minY`. Not rerun on its side since `90d6a3b79`. |
| #4854 New Chat in the system's side column | Done and verified on its side up to `a928f17c2`, **except under a Live Activity** | `AssistantView` shows the system bar with New Chat as a `.topBarTrailing` item under side bars (`newInSideBar`); `RootBarHidden` (coordinator), `SplitPlaceholder`, `ChatView` let the bar show. The test asserts New Chat sits in the side column. The expanded island (a Live Activity) was never shot. The item is the system's own, so it should move as the system's bar does. |
| #4855 iPad column colour | Done; tests pass; **before/after shots must be retaken** | `OneSidebarMetrics.padColumn` / `chromeBackground`, `SplitNavigation.topFillColor` into `SidebarTopFill(color:)`: `Theme.Shell.drawer` (F9F9F9 / 171717) on the iPad with the glass style; `Theme.systemSidebar` unchanged. iPad suites and `SidebarMetricsTests` pass on `dde9de38e`. The owner has not seen the shots yet. |
| Recent → Pinned + All Chats (#4610) | Done and verified | `OneSidebarSections.pinned` / `pinnedRow` / `allChatsRow`. `testTheColumnHasThePlacesPinnedAndAgents` passes in all three postures on `dde9de38e`. |
| Foot New Chat cut to "…" on its side | Done and verified on its side (`a928f17c2`) | `OneSidebarFoot`: the name while it fits, the icon alone otherwise (`ViewThatFits`). |
| Narrow-column fallbacks (`40d04943f`) | Done and compiled; **verified only partly on the Duo upright, not on its side or the iPad, no screenshots** | In the system-width column of the open Duo a Pinned row's name and counts, and the five grouping segments, were cut to "…". Now a Pinned row puts its counts on a second line under the name (`ViewThatFits`, `Theme.Space.xs` more above and below), and `AgentsGroupingSegments` falls back to the grouping pill (`AgentsGroupingMenu`, `agents.grouping`). `testTheSegmentsPickTheGrouping` and the upright pick test take either. |

## Test runs by commit

Suites: "Duo three" = `PersonaUITests/OneSidebarTests` + `DuoOpenChromeTests` + `SplitDividerTests`; "iPad set" = `OneSidebarTests` + `IPadSidebarTests` + `SplitDividerTests` (+ `PersonaTests/SidebarMetricsTests`). Upright-only tests skip on its side and on the iPad; one beside-only test skips upright.

| Commit | iPad | Duo upright | Duo on its side |
|---|---|---|---|
| `dde9de38e` (3rd merge) | iPad set: 12 run, 2 skipped, all pass; `SidebarMetricsTests` 5 pass | Duo three: 16, 1 skipped, all pass | Duo three: 16, 2 skipped, all pass |
| `a928f17c2` (foot New Chat) | `OneSidebarTests` + `IPadSidebarTests`: 10, 2 skipped, pass (the flake below failed twice first) | `OneSidebarTests` + `DuoOpenChromeTests`: 14, 1 skipped, pass | same two: 14, 2 skipped, pass |
| `90d6a3b79` (4th merge) | not run | Duo three: 16, 1 skipped, all pass | not run |
| `40d04943f` (narrow-column fallbacks; run on the same code just before it was committed) | **not run** | Duo three: 16, 1 skipped, **1 failure**: `testUprightBackFromAnAgentGoesToPersonasPage` (twice: in the full run and alone) | **not run** |

### Flaky

- `OneSidebarTests.testAnOpenedAgentIsSelectedAndPersonaClosesIt` (iPad and Duo on its side): sometimes fails at "Main Chat did not close the agent" (after the tap the agent stays open and Main Chat is not lit), then passes on a rerun with the same timing. Its swipes up and back down do not always land in the same place; the Main Chat tap comes about 2.7 s after the swipe back down. Cause not found (no screen recording was available).

### The open failure: `testUprightBackFromAnAgentGoesToPersonasPage`

Fails at `OneSidebarTests.swift:314`, "no Needs You card in the column", on the Duo upright after the narrow-column fallbacks. Upright, synthetic drags do nothing, so the test reaches an agent by tapping a Needs You card (`agents.interaction`) in the column. The column's list is lazy: the cards do not exist until they scroll into view.

What the failure's hierarchy shows (column about 199 pt wide):

- two Pinned rows now take two lines (52.7 pt instead of 44);
- the segments row became the grouping pill's row;
- the filter chips wrap to three lines (y 825–955);
- the fixed foot starts at y 859.

The first screen ends inside the chips, so no Needs You card is built. The test's fallback taps the lowest "需要你" text left of the column's edge. Without a Needs You section header, that is the label inside the already selected filter chip (`agents.filter.needsYou`), and tapping it does not scroll anything.

**Judgment:** the test fails because it cannot reach a card in the taller column, not because ‹ is broken. The product code for ‹ is unchanged, and the test passed on `90d6a3b79`. Not fixed. Options:

- launch the test with a wider stored column (`-agents.list.width`), so the rows and segments fit on one line again (check that `SidebarWidth` applies upright);
- find another element that exists below the first screen to tap so XCUITest scrolls (not the filter chips: the foot covers them);
- open the agent another way.

Also worth showing the owner: upright, the Needs You cards are now entirely below the first screen.

## Not done yet

- The narrow-column fallbacks have **not been run on the Duo on its side or the iPad**.
- **Screenshots:** all earlier screenshots stayed on the previous machine and are stale (they predate the fourth merge and the fallbacks). Retake light and dark for the iPad, the Duo upright and the Duo on its side, plus #4855 before / after (below).
- **origin/main is 24 commits ahead** of this branch (fetched at this handoff), not merged. `git merge-tree --write-tree HEAD origin/main` shows no conflicts.
- #4854 under a Live Activity: not shot.
- No PR is open yet.

## How to run

### Tools and simulators

- Xcode 27.1, and `xcodegen` (Homebrew).
- Simulators:
  - **iPhone Duo**: device type `iPhone Duo`, on the **iOS 27.1** runtime (only 27.1 has it).
  - **iPad Pro 13-inch (M5)** on **iOS 27.0**.
  - Create or reuse one with `scripts/app/sim-boot.sh --profile stock --device-type "iPhone Duo" --runtime <iOS 27.1 runtime> "<name>"`; it prints the UDID. Use your own UDIDs below.
- Private config: `scripts/persona_config.py` reads it (see that script). Then, from the repo root:
  - `eval "$(python3 scripts/persona_config.py env)"` (gives `PERSONA_BUNDLE_ID`, used below);
  - `python3 scripts/persona_config.py xcconfig`;
  - `cd apple && xcodegen generate -q` (again after adding a file; `apple/Persona.xcodeproj/` is generated and untracked: never commit it).
- If SwiftPM's GitHub clones time out, give `xcodebuild` your network's proxy through `https_proxy` / `http_proxy`.

### Mock hub

From the repo root (the tests need the leader conversations, so `MOCK_LEADERS=1`):

```sh
MOCK_LEADERS=1 PORT=18842 nohup python3 scripts/mock/mock_hub.py > <some log> 2>&1 &
```

Stop it by the PID listening on port 18842 (`lsof -ti tcp:18842`), not with `pkill -f mock_hub.py`, which also kills other people's hubs.

### Build and UI tests (from `apple/`)

```sh
xcodebuild -project Persona.xcodeproj -scheme Persona -destination 'platform=iOS Simulator,id=<UDID>' \
  -derivedDataPath ../build/DerivedData build-for-testing

# Duo upright (the Duo rotated upright in DeviceHub, no ORIENTATION variable):
TEST_RUNNER_PERSONA_MOCK_HUB=http://127.0.0.1:18842 xcodebuild -project Persona.xcodeproj -scheme Persona \
  -destination 'platform=iOS Simulator,id=<Duo UDID>' -derivedDataPath ../build/DerivedData test-without-building \
  -only-testing:PersonaUITests/OneSidebarTests -only-testing:PersonaUITests/DuoOpenChromeTests \
  -only-testing:PersonaUITests/SplitDividerTests -resultBundlePath <somewhere>/duo-up.xcresult

# Duo on its side: rotate it in DeviceHub first AND add TEST_RUNNER_PERSONA_ORIENTATION=landscape; same suites.

# iPad:
TEST_RUNNER_PERSONA_MOCK_HUB=http://127.0.0.1:18842 xcodebuild ... -destination 'platform=iOS Simulator,id=<iPad UDID>' \
  ... test-without-building -only-testing:PersonaUITests/OneSidebarTests -only-testing:PersonaUITests/IPadSidebarTests \
  -only-testing:PersonaUITests/SplitDividerTests -only-testing:PersonaTests/SidebarMetricsTests
```

- A single test: `-only-testing:PersonaUITests/OneSidebarTests/testUprightBackFromAnAgentGoesToPersonasPage`.
- `test-without-building` can sit for minutes after the last test before it exits. Run it in the background and wait on the process.

### The Duo's posture (DeviceHub)

The Duo tests need the Duo **Open** and in the right posture. `TEST_RUNNER_PERSONA_ORIENTATION=landscape` alone does **not** turn the open Duo on its side (a run once went upright that way); rotate it in Xcode's device window, whose process is `DeviceHub`.

By hand, select the Duo there, set its posture to Open and use Rotate Right / Rotate Left. Each click turns it a quarter.

By script, give the terminal Accessibility access. Send one click at a time and check the result before the next; never loop retries. The buttons sit in one group of the window:

```applescript
tell application "System Events"
  tell process "DeviceHub"
    set g to group 3 of splitter group 1 of group 1 of splitter group 1 of group 1 of window 1
    repeat with b in (every button of g)
      try
        if help of b is "Rotate Right" then
          click b
          return "clicked"
        end if
      end try
    end repeat
    return "not found"
  end tell
end tell
```

- The posture buttons (Closed / Partially Open / Open) are in the same group. List them with `get {description, help} of every button of g` before you match one.
- Selecting the Duo: set `selected` of its row in `outline 1 of scroll area 1 of group 1 of splitter group 1 of group 1 of splitter group 1 of group 1 of window 1` to true. The row's index depends on the machine's device list; the window's title shows the selected device.
- Check the posture from a screenshot of the inner display: `xcrun simctl io <Duo UDID> screenshot --display=3 f.png`, then `sips -g pixelWidth -g pixelHeight f.png`:
  - on its side: **2853 × 2007**;
  - upright: **2007 × 2853**.

### Screenshots (light and dark)

For each device, after a `build-for-testing` (or a build) of the branch's HEAD:

1. `xcrun simctl install <UDID> ../build/DerivedData/Build/Products/Debug-iphonesimulator/Persona.app`
2. For each of `light` and `dark`:
   - `xcrun simctl terminate <UDID> "$PERSONA_BUNDLE_ID"`
   - `xcrun simctl ui <UDID> appearance <light|dark>`
   - `xcrun simctl spawn <UDID> defaults write "$PERSONA_BUNDLE_ID" agents.list.closed -bool NO` (so the column is out)
   - launch:
     ```sh
     xcrun simctl launch <UDID> "$PERSONA_BUNDLE_ID" -AppleLanguages '(zh-Hans)' -AppleLocale zh_CN \
       -pairURL 'persona://pair?url=http%3A%2F%2F127.0.0.1%3A18842&token=mock' -tab assistant -app.tab assistant \
       -dev.openChat '' -dev.grouping repo -agents.list.closed NO -ui.style glass
     ```
   - wait about 9 s for the mock hub's data;
   - Duo: `xcrun simctl io <UDID> screenshot --display=3 <file>` (the inner display); iPad: `xcrun simctl io <UDID> screenshot <file>`.
   - Upright, the column may start away: bring it out with the page's sidebar button in the Simulator window before the shot.
3. Names: `ipad-{light,dark}.png`, `duo-portrait-{light,dark}.png`, `duo-landscape-{light,dark}.png`. Keep them out of the repo, and look at each one before reporting.
4. **#4855 before / after** (iPad, light and dark):
   - "after" is HEAD.
   - For "before", make `OneSidebarMetrics.padColumn` return `nil` in a scratch build. Don't commit it. That gives the old look: the system sidebar's `Theme.systemSidebar` list, a white / black header and foot, and the system's strip.
   - Names: `4855-{before,after}-{light,dark}.png`.

## Next steps, in order

1. Set up the simulators, the config, `xcodegen` and the mock hub (`MOCK_LEADERS=1`, port 18842); build `feat/4206-one-sidebar` HEAD.
2. Fix `testUprightBackFromAnAgentGoesToPersonasPage` on the Duo upright (see "The open failure"), then run the Duo three upright until green.
3. Run the Duo three **on its side** (rotated in DeviceHub + `TEST_RUNNER_PERSONA_ORIENTATION=landscape`) and the iPad set on the fallbacks; rerun the known flake once if it fails.
4. Merge origin/main (a merge, no rebase; 24+ commits, no conflicts at handoff time), build, rerun the three postures, push.
5. Take the light / dark screenshots (iPad, Duo upright, Duo on its side) and #4855 before / after; check the Pinned rows, the grouping pill and the foot in the narrow column; optionally #4854 under a Live Activity.
6. Update this handoff and push; report to the supervisor with the shots, then **stop and wait for the owner's explicit approval** (UI change).
7. After approval only: delete `docs/handoff/4206-*`, push, open the PR (`Refs` for #4852–#4855 and #3641), merge with `scripts/hooks/merge-pr.sh`.

## Pitfalls

- **Upright on the open Duo, synthetic drags miss** (swipes and press-and-drag do nothing). Reach things below the first screen by tapping an element that exists (XCUITest scrolls it in). The column's agent rows and Needs You cards are not built until scrolled to.
- **Upright, the column may start away** after an install; tests bring it out from the page's button (`assistant-sidebar-button`), the system's Show Sidebar (a page pushed on Persona's page) or `feishu-sidebar-button` (Messages).
- **The handle's closed state**: the split starts from the kept state (`SplitDividerHandle.startsClosed`). It reads the posture only once the list is at least `sidebarMinPhone` wide and the page does not start before the split, and again right after the list comes out (`onChange(of: columns)`). `overlays` stays nil until it is read. Laid over the page (upright), closing is not kept, so `SplitDividerTests`' relaunch check skips upright.
- **`SidebarWidth` must stay the column's outermost modifier**: under the top padding the dragged width stopped reaching the split (320 → 320).
- `DuoOpenChromeTests`' face: the mock hub has several assistants, so the page's face is `persona-avatar`, not `assistant-menu`. Match either.
- An agent chat's title is `chat.title` or `chat.compactTitle`. The Messages list's frame starts at the window edge, so check its first text's `minX`.
- `apple/PersonaTests/JSONDateTests.swift` failed to compile with Xcode 27.1 on an older main (static `ISO8601DateFormatter`s). Main fixed it; CI builds with Xcode 26.0.1. If an older main ever shows it, patch it locally only. Don't commit that patch to this branch, and don't open an issue.
- Pre-existing check failures, also on origin/main, not this branch's:
  - `test_apple_perf_patterns` (`AppModel+Managers.swift` file read);
  - `test_apple_state_initial_values` (`AssistantSwitcher.swift` `ManagersModel`);
  - `test_doc_paths` fails only while this handoff exists.
- `dde9de38e`'s trailer names the wrong model. It is pushed, so it is left as is (no force push).
- Commits on this branch:
  - `git -c user.name=zake111000 -c user.email=14012239+zake111000@users.noreply.github.com commit`;
  - English messages ending with the `Co-Authored-By:` trailer;
  - stage files by path;
  - run `python3 scripts/privacy_check.py` first;
  - no internal hosts or names in public text.
