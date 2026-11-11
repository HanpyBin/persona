#if os(iOS)
import SwiftUI

// One sidebar at regular width (#4206): iPad and the open iPhone Duo show Persona's pages, its recent chats and the Agents
// list in one column, as ChatGPT's open layout and the web's sidebar do, with the page or the agent's chat beside it. No
// Assistant | Agents switch: the Agents page (`ChatsView`) holds the split, Persona's home is its detail column's first
// page, and these are the column's first rows (`AgentsList`), its header and its foot.

extension EnvironmentValues {
    /// The one sidebar is the layout (iPad, iPhone Duo open; `RootTabs`). False elsewhere.
    @Entry var oneSidebar = false
    /// Folds the split's list column away (the one sidebar's header button); nil outside a split.
    @Entry var splitHideList: SplitShowList? = nil
    /// The window is wider than tall (an open iPhone Duo on its side): Recent starts folded there, for the Agents to be in
    /// sight on the first screen.
    @Entry var sidebarOnItsSide = false
}

extension EnvironmentValues {
    /// The one sidebar's Persona rows for the Agents list to draw first (`ChatsView`); nil elsewhere.
    @Entry var oneSidebarLead: OneSidebarLead? = nil
}

/// What the Agents list needs to draw Persona's rows atop itself.
struct OneSidebarLead {
    /// Closes the agent open beside the column.
    var leaveAgent: () -> Void
    /// An agent's page is open beside the column (no Persona row is the selected one then).
    var agentOpen: Bool
}

/// What the detail column beside the one sidebar shows while no agent is open (`SplitNavigation.home`): Messages when its
/// row (under More) is the one picked, Persona's page otherwise. Each has a stack of its own.
struct OneSidebarHome: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        if model.selectedTab == .feishu, LarkHomePreference.shared.shows {
            LarkConversationsView()
        } else {
            AssistantView()
        }
    }
}

/// The one sidebar's column around the Agents list: Persona's header above, the foot below, no navigation bar.
struct OneSidebarChrome: ViewModifier {
    var enabled: Bool
    var leaveAgent: () -> Void

    func body(content: Content) -> some View {
        if enabled {
            content
                .toolbarVisibility(.hidden, for: .navigationBar)
                .safeAreaInset(edge: .top, spacing: 0) { OneSidebarHeader() }
                .safeAreaInset(edge: .bottom, spacing: 0) { OneSidebarFoot(leaveAgent: leaveAgent) }
        } else {
            content
        }
    }
}

/// Persona's part of the one sidebar: the main conversation and the places (the rarer ones, and Messages, under More),
/// Recent, and the Agents heading with the list's ⋯ and New Session, the grouping's segments under it. Rows of the Agents
/// list's own `List`, above its filters.
struct OneSidebarSections: View {
    @Environment(AppModel.self) private var model
    @Environment(\.padRouter) private var padRouter
    @Environment(\.sidebarOnItsSide) private var onItsSide
    /// Closes the agent open beside the column, for a Persona page to show there.
    var leaveAgent: () -> Void
    /// An agent's page is open beside the column.
    var agentOpen: Bool
    var menuActions: AgentsListMenu.Actions
    @Binding var showNew: Bool
    @State private var moreOpen = false
    @State private var showAll = false
    /// The main conversation asked to end from its long press (`MainChatRowMenu`).
    @State private var ending: Session?
    /// Recent folded or not, as the user left it ("" until then: folded on its side, open otherwise).
    @StoredValue("oneSidebar.recentFolded") private var recentFoldedChoice = ""

    private static let recentCount = 3
    private var recentFolded: Bool { recentFoldedChoice.isEmpty ? onItsSide : recentFoldedChoice == "1" }
    /// Messages is in the detail column (`OneSidebarHome`): its row is the selected one, none of Persona's.
    private var messagesOpen: Bool { !agentOpen && model.selectedTab == .feishu && LarkHomePreference.shared.shows }
    /// A Persona row is the one in the detail column: no agent and not Messages there.
    private var personaOpen: Bool { !agentOpen && !messagesOpen }

    var body: some View {
        mainRow
        place(String(localized: "Inbox"), Theme.Icon.inbox, id: "sidebar-inbox", count: InboxStore.shared.unreadCount) { show(.inbox) }
        if TodoStore.shared.isSupported {
            place(String(localized: "To-dos"), Theme.Icon.todo, id: "sidebar-todos", count: TodoStore.shared.badgeCount) { show(.todos) }
        }
        place(String(localized: "Scheduled Tasks"), Theme.Icon.schedule, id: "sidebar-routines") { show(.routines) }
        place(String(localized: "Knowledge"), Theme.Icon.knowledge, id: "sidebar-knowledge") { show(.knowledge) }
        moreRow
        if moreOpen {
            if LarkHomePreference.shared.shows {
                place(String(localized: "Messages [Quote Views 4da199]"), Theme.Icon.feishuChats, id: "sidebar-messages",
                      selected: messagesOpen, indent: true, run: openMessages)
            }
            place(String(localized: "Meeting Recording"), Theme.Icon.recording, id: "sidebar-recordings", indent: true) { show(.recordings) }
            place(String(localized: "Health"), Theme.Icon.health, id: "sidebar-health", indent: true) { show(.health) }
            place(String(localized: "Machines"), Theme.Icon.machines, id: "sidebar-machines", indent: true) { show(.machines) }
        }
        VStack { Divider() }.padding(.horizontal, Theme.Space.m).padding(.vertical, Theme.Space.s).sidebarRow() // a row lays a bare Divider out upright
        recent
        agentsHeading
        AgentsGroupingSegments()
            .padding(.horizontal, Theme.Space.m)
            .padding(.bottom, Theme.Space.s)
            .sidebarRow()
    }

    // MARK: Places

    private var mainRow: some View {
        let key = model.assistantMain
        let selected = personaOpen && model.assistantCurrent != nil && model.assistantCurrent == key
        return row(id: "sidebar-main", selected: selected, action: {
            if let key { open(key) } else { show(.newConversation) }
        }) {
            glyph(Theme.Icon.conversation)
            Text("Main Chat").font(Theme.Font.body).foregroundStyle(Color.primary).lineLimit(1)
            Spacer(minLength: 0)
            if model.badges.assistant > 0 {
                Circle().fill(Theme.accent).scaledFrame(width: Theme.Size.dot, height: Theme.Size.dot)
            }
        }
        .disabled(key == nil && model.assistantConfig == nil) // as the column's: the main one comes with the hub's config
        .contextMenu {
            if let key { MainChatRowMenu(key: key, main: model.assistantConversations.first { $0.key == key }) { ending = $0 } }
        }
        .confirmClose($ending)
    }

    /// More: Messages, Meeting Recording, Health and Machines, folded under one row (#4206: the first screen on its side is
    /// short). Open while the row picked is one of them (Messages), as the prototype's does.
    private var moreRow: some View {
        row(id: "sidebar-more", action: { withAnimation(Theme.Motion.standard) { moreOpen.toggle() } }) {
            glyph(Theme.Icon.more)
            Text(moreOpen ? String(localized: "Collapse") : String(localized: "More")).font(Theme.Font.body).foregroundStyle(Color.primary).lineLimit(1)
            Spacer(minLength: 0)
            FoldChevron(folded: !moreOpen)
        }
        .onChange(of: messagesOpen, initial: true) { _, open in if open { moreOpen = true } }
    }

    private func place(_ title: String, _ symbol: String, id: String, count: Int = 0, selected: Bool = false, indent: Bool = false,
                       run: @escaping () -> Void) -> some View {
        row(id: id, selected: selected, action: run) {
            glyph(symbol)
            Text(title).font(Theme.Font.body).foregroundStyle(Color.primary).lineLimit(1)
            Spacer(minLength: 0)
            if count > 0 { CountBadge(count: count, look: .solid(Theme.needsYou)) }
        }
        .padding(.leading, indent ? Theme.Space.l : 0)
    }

    /// A Persona page beside the column: the agent open there goes, the page comes (`PadRouter`, as the iPad's tabs did).
    private func show(_ page: ShellPage) {
        toPersona()
        padRouter?.show(page)
    }

    /// One of Persona's conversations beside the column.
    private func open(_ key: SessionKey) {
        toPersona()
        model.openAssistantConversation(key)
    }

    /// Persona's page in the detail column: no agent, not Messages. The page is built before it reads what was asked.
    private func toPersona() {
        leaveAgent()
        if model.selectedTab != .assistant { model.selectedTab = .assistant }
    }

    /// Messages in the detail column (`OneSidebarHome`), where a phone has its tab.
    private func openMessages() {
        leaveAgent()
        model.selectedTab = .feishu
    }

    // MARK: Recent

    @ViewBuilder private var recent: some View {
        let main = model.assistantMain
        let rest = AssistantSidebarGroups.newestFirst(model.assistantConversations.filter { $0.key != main && !$0.isHealth })
        row(id: "sidebar-recent-fold", action: {
            withAnimation(Theme.Motion.standard) { recentFoldedChoice = recentFolded ? "0" : "1" }
        }) {
            Text("Recent").font(Theme.Font.secondaryMedium).foregroundStyle(.secondary).lineLimit(1)
            FoldChevron(folded: recentFolded)
            Spacer(minLength: 0)
        }
        if !recentFolded {
            ForEach(showAll ? rest : Array(rest.prefix(Self.recentCount))) { s in
                let selected = personaOpen && model.assistantCurrent == s.key
                row(id: "sidebar-recent-\(s.sessionID)", selected: selected, action: { open(s.key) }) {
                    Text(s.title.isEmpty || s.title == "Claude Code" ? String(localized: "New Chat [Quick Actions ea4b29]") : s.title)
                        .font(Theme.Font.body).foregroundStyle(Color.primary).lineLimit(1)
                    Spacer(minLength: 0)
                    if let at = s.updatedAt {
                        Text(Fmt.relative(at)).font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1)
                    }
                }
            }
            if rest.count > Self.recentCount {
                row(id: "sidebar-recent-all", action: { withAnimation(Theme.Motion.standard) { showAll.toggle() } }) {
                    Text(showAll ? String(localized: "Collapse") : String(localized: "All Chats"))
                        .font(Theme.Font.secondary).foregroundStyle(.secondary).lineLimit(1)
                    Spacer(minLength: 0)
                }
            }
        }
    }

    // MARK: Agents

    /// The Agents list's name, with its ⋯ (grouping, display, machines) and New Session, as the web's sidebar heads it.
    private var agentsHeading: some View {
        HStack(spacing: Theme.Space.m) {
            Text("Agents").font(Theme.Font.secondaryMedium).foregroundStyle(.secondary).lineLimit(1)
            Spacer(minLength: 0)
            AgentsListMenu(actions: menuActions)
            Button { showNew = true } label: {
                ThemedIcon(Theme.Icon.compose).font(Theme.Font.body).foregroundStyle(Color.primary)
            }
            .buttonStyle(.plain)
            .accessibilityLabel("New Session [Chats View 58e21b]")
            .accessibilityIdentifier("sidebar-agents-new")
        }
        .padding(.horizontal, Theme.Space.m)
        .padding(.top, Theme.Space.s)
        .padding(.vertical, Theme.Sidebar.rowPadding)
        .scaledFrame(minHeight: Theme.Sidebar.row)
        .sidebarRow()
        .accessibilityIdentifier("sidebar-agents-heading")
    }

    // MARK: Rows

    private func glyph(_ symbol: String) -> some View {
        // One box for every symbol: a wide one (Knowledge's books) scales down into it instead of pushing its words.
        Image(systemName: symbol).resizable().scaledToFit().fontWeight(.medium).foregroundStyle(Color.primary)
            .scaledFrame(width: Theme.Sidebar.glyph, height: Theme.Sidebar.glyph)
            .scaledFrame(width: Theme.Sidebar.glyphColumn)
    }

    private func row<Label: View>(id: String, selected: Bool = false, action: @escaping () -> Void,
                                  @ViewBuilder label: () -> Label) -> some View {
        Button(action: action) {
            HStack(spacing: Theme.Sidebar.gap) { label() }
                .padding(.horizontal, Theme.Space.m)
                .padding(.vertical, Theme.Sidebar.rowPadding)
                .scaledFrame(minHeight: Theme.Sidebar.row)
                .background {
                    if selected { RoundedRectangle(cornerRadius: Theme.Sidebar.radius, style: .continuous).fill(Theme.Fill.well) }
                }
                .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(selected ? .isSelected : [])
        .accessibilityIdentifier(id)
        .sidebarRow()
    }
}

private extension View {
    /// A row of the Agents list drawn as the sidebar's: no cell, no line.
    func sidebarRow() -> some View {
        listRowInsets(EdgeInsets(top: 0, leading: Theme.Space.s, bottom: 0, trailing: Theme.Space.s))
            .listRowBackground(Color.clear)
            .listRowSeparator(.hidden)
    }
}

/// The one sidebar's header: Persona's mark and name, and the button that folds the column away (ChatGPT's open layout).
/// Several assistants: the current one and the way to another (`AssistantSwitcherHeader`), as the column's header has.
struct OneSidebarHeader: View {
    @Environment(AppModel.self) private var model
    @Environment(\.splitHideList) private var hideList

    var body: some View {
        HStack(spacing: Theme.Space.s) {
            if model.showsAssistantIdentity, let current = model.currentAssistant {
                AssistantSwitcherHeader(manager: current, nameFont: Theme.Font.title, openSettings: { model.showSettings() })
            } else {
                AppGlyph(size: Theme.Sidebar.glyphColumn, scaled: true)
                Text(model.persona.name).font(Theme.Font.title).foregroundStyle(Color.primary).lineLimit(1)
            }
            Spacer(minLength: Theme.Space.s)
            AssistantTopBar.circle(Theme.Icon.sidebar, label: String(localized: "Hide Sidebar"), id: "assistant-sidebar-collapse") {
                Haptics.tap()
                hideList?.show()
            }
        }
        .padding(.horizontal, Theme.Space.l)
        // The page's top bar beside it starts its round buttons this far below the status bar (`AssistantTopBar.topInset`):
        // the fold here and New Chat there on one line (2026-11-11, the owner).
        .padding(.top, AssistantTopBar.topInset)
        .padding(.bottom, Theme.Space.s)
        .background(Theme.pageBackground)
    }
}

/// The one sidebar's foot: New Chat in the Theme Color on the left; search and Settings in one capsule on the right, where
/// ChatGPT has its account.
struct OneSidebarFoot: View {
    @Environment(AppModel.self) private var model
    @Environment(\.padRouter) private var padRouter
    var leaveAgent: () -> Void
    @State private var fullSearch = false

    var body: some View {
        HStack(spacing: Theme.Space.m) {
            Button {
                toPersona()
                padRouter?.show(.newConversation)
            } label: {
                Label("New Chat [Quick Actions ea4b29]", systemImage: Theme.Icon.compose)
                    .font(Theme.Font.bodyStrong)
                    .foregroundStyle(.white)
                    .lineLimit(1)
                    .padding(.horizontal, Theme.Space.xl)
                    .scaledFrame(height: Theme.Size.avatarL)
                    .background(Theme.accent, in: .capsule)
                    .contentShape(.capsule)
            }
            .buttonStyle(.plain)
            .accessibilityIdentifier("sidebar-new")
            Spacer(minLength: Theme.Space.s)
            HStack(spacing: 0) {
                footButton(Theme.Icon.search, label: String(localized: "Search Chats [Assistant Sidebar 55c9e4]"), id: "sidebar-search") { fullSearch = true }
                footButton(Theme.Icon.settings, label: String(localized: "Settings"), id: "sidebar-settings") { model.shellSettings = true }
                    .overlay(alignment: .topTrailing) { ExtensionSettingsBadge() }
            }
            .surface(interactive: true, in: .capsule)
        }
        .padding(.horizontal, Theme.Space.l)
        .padding(.vertical, Theme.Space.m)
        .background(Theme.pageBackground) // the list scrolls under it
        .fullScreenCover(isPresented: $fullSearch) {
            AssistantFullSearch { key in
                fullSearch = false
                toPersona()
                model.openAssistantConversation(key)
            }
        }
    }

    /// Persona's page in the detail column (not an agent, not Messages), for what it is asked next.
    private func toPersona() {
        leaveAgent()
        if model.selectedTab != .assistant { model.selectedTab = .assistant }
    }

    private func footButton(_ symbol: String, label: String, id: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            ThemedIcon(symbol)
                .font(Theme.Font.heading)
                .foregroundStyle(Color.primary)
                .scaledFrame(width: Theme.Size.avatarL, height: Theme.Size.avatarL)
                .contentShape(.circle)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(label)
        .accessibilityIdentifier(id)
    }
}
#endif
