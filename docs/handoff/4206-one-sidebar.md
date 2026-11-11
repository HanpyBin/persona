# Handoff: one sidebar on iPad and the open iPhone Duo (#4206)

> Temporary. Delete this file and `4206-one-sidebar-prototype.html` before the branch merges.

Branch `feat/4206-one-sidebar`, from main `58485e49c`. Work in progress. It needs the owner's approval before it merges, because it is a UI change.

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
6. **Recent starts folded on its side.** When the open Duo is held on its side, **Recent** starts folded, and its state is remembered from then on.

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
4. **Recent**: three conversations, then "All Chats".
5. **Agents**: a heading with ⋯ and New Session, then the existing Agents list (filters, grouping, pinned, machine and repository groups).
6. A fixed foot: **New Chat**, and a capsule with search and Settings.

**Behaviour:**

- There is no Assistant | Agents switch.
- The right column shows Persona's page while no agent is open, and an agent's chat when one is tapped.
- Tapping a Persona row closes the open agent and shows that page.

## What is built (uncommitted work committed here)

**`RootView.swift`**
- On iOS, `RootTabs` (only used at regular width) shows `ChatsView` with `\.oneSidebar` instead of the `TabView`.
- It sets `\.sidebarOnItsSide` (a phone wider than tall).
- It keeps `PadRoutes` for Settings and the Inbox route.
- The `TabView` remains on macOS and visionOS.

**`SplitNavigation.swift`**
- New `home: AnyView?`. While the path is empty, the detail column shows `home` in place of the stack.
- This is needed because Persona's page has its own `NavigationStack`, and stacks can't nest.
- New `\.splitHideList`, used by the header's collapse button.

**`ChatsView.swift`**
- It passes `AssistantView()` as `home`.
- `OneSidebarChrome` adds the header and foot and hides the column's navigation bar.
- `\.oneSidebarLead` makes `AgentsList` draw `OneSidebarSections` first, and keeps the filters in the list (`inListControls`).
- The system search is off. Search is the foot's full-window search.
- `covered` ignores `selectedTab`.
- `model.selectedTab` follows the path (`.chats` when an agent is open, `.assistant` when not), and routing to Persona clears the path. Notifications and Siri still land.

**`AssistantView.swift`**
- In `oneSidebar`, it renders only `stack`.
- The top bar's show-sidebar button uses `splitShowList`, and `sidebarOut` reads `splitDetail.listHidden`.
- The open Duo gets `ShellHome` (`phoneHome`).

**New `Views/OneSidebar.swift`**
- `OneSidebarSections`: the rows. Places route through `PadRouter.show(ShellPage)`, which `AssistantView` already handles. Conversations go through `openAssistantConversation`.
- `OneSidebarHeader`.
- `OneSidebarFoot`, which uses `AssistantFullSearch` and `model.shellSettings`.

**Verified:**
- It builds for the iOS simulator.
- On the iPad Pro 13-inch simulator, against `scripts/mock/mock_hub.py` (paired with `-pairURL persona://pair?url=<encoded hub>&token=mock`):
  - home;
  - an agent opened with `-app.tab chats -openSession macbook/a1f3c2d4-0000-4000-8000-000000000006`;
  - dark mode.

## Still to do

1. **Try it on the open Duo** in both postures (see `docs/apple/ios.md` "iPhone Duo"). The inner display opens from Xcode's DeviceHub. Check:
   - that Recent starts folded on its side;
   - the header against the system's side bars;
   - the foot above the home indicator.
2. **Grouping.** The prototype has a segmented control (To-do / Machine / Repository / Project / Status) under the Agents heading. The build still shows the old "By Repository ⌃" menu.
3. **Messages (Feishu).** The Messages tab was dropped with the `TabView` on iOS regular width. Add it under More when `LarkHomePreference.shared.shows`, and decide where `selectedTab == .feishu` routes.
4. **Selection.** Highlight the open agent's row. The Persona rows already highlight.
5. **Dead code.** `PageTabSwitch`, `PageTabSwitchRow`, `PageTabSwitchInset` and the `pageTabSwitch` paths are dead on iOS now. Remove them once the layout settles.
6. **Tests.**
   - UI tests that assert the switch will fail: `DuoOpenChromeTests`, and any that use `page-tab-switch`, `shell-assistant` or `shell-agents` at regular width. Update them.
   - Add a test for the one sidebar: rows, opening an agent, back to Persona, More, Recent folded on its side.
   - Run only the tests the change touches (see the repo's testing notes).
7. **Docs.** Update `docs/apple/ios.md` ("iPad", "iPhone Duo"), and delete this handoff.
8. **Before merging:**
   - screenshots for the owner (light and dark, iPad and both Duo postures);
   - the owner's explicit approval;
   - delete `docs/handoff/4206-*`.
