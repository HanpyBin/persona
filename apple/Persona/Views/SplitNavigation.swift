import OSLog
import SwiftUI

/// A list and the pages it opens (Agents, Inbox, Settings), laid out for the width there is (docs/apple/ios.md「iPhone Duo」).
///
/// Compact width — every iPhone, and iPhone Duo closed — is one stack with the list at its root, exactly as before.
/// Regular width — iPhone Duo open — keeps the list as a sidebar column and opens its pages in the detail column,
/// the way Mail shows a list and a message side by side; the split view also keeps its columns clear of the fold
/// when the device is partly folded. Both share `path`, so folding or unfolding keeps the page that was open.
///
/// In the split, the list's own links (`NavigationLink(value:)`) replace what the detail column shows; code in the list
/// should do the same (`path = NavigationPath([value])`, which in the single stack is the same as appending).
struct SplitNavigation<Sidebar: View, Empty: View, Destinations: ViewModifier>: View {
    @Environment(\.horizontalSizeClass) private var widthClass
    /// Whether the list column shows (its button in the toolbar closes it).
    @State private var columns = SplitColumns.shown
    @Binding var path: NavigationPath
    /// iOS: the list column's width and closing dragged on the line beside it and remembered under this key (#2624);
    /// nil: the system's column.
    var listKey: String? = nil
    /// The pages the list opens (`navigationDestination`s), registered in both layouts.
    var destinations: Destinations
    /// iOS beside the one sidebar (#4206): what the detail column shows while nothing is open, in place of the stack.
    /// Persona's page has a stack of its own (its pushed pages), which can't be inside this one; an agent opened takes
    /// the column back to the stack.
    var home: AnyView? = nil
    @ViewBuilder var sidebar: Sidebar
    /// The detail column before anything is open.
    @ViewBuilder var empty: Empty
    #if os(iOS)
    /// Where the split, its list column's trailing edge and its page start (global x), as last laid out.
    @State private var splitStart: CGFloat = 0
    @State private var listEnd: CGFloat = 0
    @State private var pageStart: CGFloat = 0
    /// The list column slides in over the page instead of beside it: the system's choice where the page would be left too
    /// narrow (the open iPhone Duo held upright, #4852). Read while the list is out, kept while it is away; nil: not read
    /// yet (the split's own first moves, out or away, are no one's choice).
    @State private var overlays: Bool?
    /// The system's bars down the side beside the one sidebar (the open Duo on its side): nothing is above the columns.
    @Environment(\.oneSidebarSideBars) private var sideBars
    /// Each column's own top safe area, outside the margin it is given (the page's has the system's bar room under side
    /// bars, the list's does not).
    @State private var listSafeTop: CGFloat = 0
    @State private var pageSafeTop: CGFloat = 0
    #endif

    init(path: Binding<NavigationPath>, listKey: String? = nil, destinations: Destinations, home: AnyView? = nil,
         @ViewBuilder sidebar: () -> Sidebar, @ViewBuilder empty: () -> Empty) {
        _path = path
        self.listKey = listKey
        self.destinations = destinations
        self.home = home
        self.sidebar = sidebar()
        self.empty = empty()
        // A list closed last time starts closed (#2624). Closed only once it appeared, the split put it back out as it
        // first laid itself out, and the list came back on every launch.
        _columns = State(initialValue: SplitDividerHandle.startsClosed(listKey) ? .detailOnly : SplitColumns.shown)
    }

    var body: some View {
        layout.environment(\.splitPush, SplitPush { [path = $path] value in path.wrappedValue.append(value) })
    }

    @ViewBuilder private var layout: some View {
        if widthClass == .regular {
            #if os(macOS)
            // The list in the content column (flush; the sidebar column floats as a glass card on macOS 26 and stays
            // empty). A value link in the content column replaces the detail column's stack, as one in the sidebar did.
            NavigationSplitView(columnVisibility: $columns) {
                SplitColumns.noSidebar
            } content: {
                // The look the list had in the sidebar column (no row lines, footers as before) on the window's white,
                // as the Persona and Agents lists beside the sections are; lists that pick a style keep it. Inside the
                // destinations, so the style stays in this column whichever environment the pushed pages are built in.
                sidebar
                    .listStyle(.sidebar)
                    .scrollContentBackground(.hidden)
                    .modifier(destinations)
                    .modifier(SidebarWidth())
            } detail: {
                NavigationStack(path: $path) {
                    empty.modifier(destinations)
                }
                // The page keeps a width it can be read at; narrowing the window narrows the list first (#1200).
                .navigationSplitViewColumnWidth(min: Theme.Size.macDetailMin, ideal: Theme.Size.macWindowWidth)
                .onGeometryChange(for: Bool.self) { $0.size.width < Theme.Size.macSplitSquished } action: { squished in
                    SplitColumns.log.notice("detail squished: \(squished) (columns \(String(describing: columns), privacy: .public), depth \(path.count))")
                }
            }
            .navigationSplitViewStyle(.balanced)
            .environment(\.splitDetail, SplitDetail(isFirstPage: path.count <= 1))
            // The detail column once came out a few points wide, the chat squeezed into it (2026-11-02, not reproduced):
            // what the columns and the stack did, for the next time (`log show --predicate 'category == "split"'`).
            .onChange(of: columns) { old, new in
                SplitColumns.log.notice("columns \(String(describing: old), privacy: .public) → \(String(describing: new), privacy: .public)")
            }
            .onChange(of: path.count) { old, new in SplitColumns.log.notice("depth \(old) → \(new)") }
            #else
            NavigationSplitView(columnVisibility: $columns) {
                // No stack of its own: a value link in the sidebar column replaces the detail column's stack. A sidebar
                // stack that refused its pushes (a path binding that always read empty) first slid the list away, then
                // back: the sidebar folded and unfolded on every tap.
                sidebar.modifier(destinations)
                    #if os(iOS)
                    .safeAreaPadding(.top, topPad(listSafeTop))
                    .onGeometryChange(for: CGFloat.self) { $0.safeAreaInsets.top } action: { listSafeTop = $0 }
                    #endif
                    // Last, on the column's root: under the top margin's padding the dragged width no longer reached the
                    // split (SplitDividerTests on the Duo on its side).
                    .modifier(SidebarWidth())
            } detail: {
                if let home {
                    // Both kept in one root that never changes kind: when the column's root went from the stack to the
                    // home, the split kept the stack's controller and the chat that had been open stayed on screen (#4206).
                    // The stack stays, at its root and out of sight, under the home while nothing is open.
                    ZStack {
                        NavigationStack(path: $path) {
                            empty.modifier(destinations)
                        }
                        .opacity(path.isEmpty ? 0 : 1)
                        .allowsHitTesting(!path.isEmpty)
                        .accessibilityHidden(path.isEmpty)
                        if path.isEmpty { home }
                    }
                    #if os(iOS)
                    .safeAreaPadding(.top, topPad(pageSafeTop))
                    .onGeometryChange(for: CGFloat.self) { $0.safeAreaInsets.top } action: { pageSafeTop = $0 }
                    .onGeometryChange(for: CGFloat.self) { $0.frame(in: .global).minX } action: { pageStart = $0; notePosture() }
                    #endif
                } else {
                    NavigationStack(path: $path) {
                        empty.modifier(destinations)
                    }
                    #if os(iOS)
                    .onGeometryChange(for: CGFloat.self) { $0.frame(in: .global).minX } action: { pageStart = $0; notePosture() }
                    #endif
                }
            }
            .navigationSplitViewStyle(.balanced)
            #if os(iOS)
            .onPreferenceChange(SidebarEdge.self) { listEnd = $0; notePosture() }
            .onGeometryChange(for: CGFloat.self) { $0.frame(in: .global).minX } action: { splitStart = $0; notePosture() }
            // An agent picked, or anything else that changes the page: a list laid over the page goes (#4852).
            .onChange(of: path) { hideOverlay() }
            // Laid over the page the list comes out with nothing else moving: read it once it is out.
            .onChange(of: columns) { _, now in if now != .detailOnly { Task { notePosture() } } }
            #endif
            .modifier(SidebarTopFill(shown: columns != .detailOnly, color: topFillColor))
            .modifier(SplitDividerHandle(columns: $columns, key: listKey, overlays: overlaysNow))
            // On the whole split, not the detail stack: its pages are built from the destinations the sidebar
            // registered, in the sidebar's environment.
            .environment(\.splitDetail, SplitDetail(isFirstPage: path.count <= 1, listHidden: columns == .detailOnly))
            .environment(\.splitShowList, SplitShowList { [columns = $columns] in
                withAnimation(Theme.Motion.layout) { columns.wrappedValue = SplitColumns.shown }
            })
            #if os(iOS)
            .environment(\.splitHideList, SplitShowList { [columns = $columns] in
                withAnimation(Theme.Motion.layout) { columns.wrappedValue = .detailOnly }
            })
            .environment(\.splitHideOverlay, SplitShowList { hideOverlay() })
            #endif
            #endif
        } else {
            NavigationStack(path: $path) {
                sidebar.modifier(destinations)
            }
        }
    }

    /// The one sidebar's column has a color of its own on the iPad (#4855): the strip over it matches; nil, the system's.
    private var topFillColor: Color? {
        #if os(iOS)
        home == nil ? nil : OneSidebarMetrics.padColumn
        #else
        nil
        #endif
    }

    #if os(iOS)
    /// Beside the one sidebar under side bars both columns start lower, together (#4853): at least the margin, and as low
    /// as the lower of their own tops (the system gives the page room for its bar there), so the fold and the page's face
    /// stay on one line. Set inside each column, as the split's columns do not take a safe area given around it.
    private func topPad(_ own: CGFloat) -> CGFloat {
        let margin = home == nil ? 0 : OneSidebarMetrics.topMargin(sideBars: sideBars)
        guard margin > 0 else { return 0 }
        return max(0, max(margin, listSafeTop, pageSafeTop) - own)
    }
    #endif

    private var overlaysNow: Bool? {
        #if os(iOS)
        overlays
        #else
        false
        #endif
    }

    #if os(iOS)
    /// The posture: the page starting before the list ends is the page under the list. Read only while the list is out
    /// (away, both postures look the same).
    private func notePosture() {
        // As the split first lays itself out the list is narrower than any it takes, and the page slides in from before
        // it: read then, an iPad's list looked laid over its page, and its closing went unremembered.
        guard columns != .detailOnly, listEnd - splitStart >= Theme.Size.sidebarMinPhone, pageStart >= splitStart else { return }
        let now = pageStart < listEnd - 1
        if overlays != now { overlays = now }
    }

    /// A pick in a list laid over the page (the open Duo upright) changed what the page shows: the list slides away, as
    /// the system's sidebar does on an iPad in portrait. Beside the page (on its side, the iPad) it stays.
    private func hideOverlay() {
        guard overlays == true, columns != .detailOnly else { return }
        withAnimation(Theme.Motion.layout) { columns = .detailOnly }
    }
    #endif
}

/// What a split shows when its list is out: the list beside the page. On the Mac the list is the content column of a
/// three-column split whose sidebar column stays empty (`noSidebar`): macOS 26 draws the sidebar column as a glass card
/// floating off the window's edges, the window's backdrop showing round it (2026-11-02 user: like floating on a backdrop, with a gap).
/// The shell's sections column is the window's sidebar there.
enum SplitColumns {
    #if os(macOS)
    static let shown = NavigationSplitViewVisibility.doubleColumn

    /// The empty sidebar column, and no system button to bring it out.
    static var noSidebar: some View { Color.clear.toolbar(removing: .sidebarToggle) }

    static let log = Logger(subsystem: "persona", category: "split")
    #else
    static let shown = NavigationSplitViewVisibility.all
    #endif
}

/// The list column's width. iPad (and a Mac window): wider than the system's sidebar, which is narrow for the agents'
/// cards (they wrapped every title); dragged on the line beside it (`SplitDividerHandle`, #2624), that width. One
/// expression whatever the width: a modifier coming or going would build the list again (its filters, its scroll).
/// iPhone Duo: none of our own (the system sizes the columns, and partly folded it moves the divide onto the fold) until
/// one is kept from a drag; that takes the column once, at the drag's end, never during it.
struct SidebarWidth: ViewModifier {
    @Environment(\.splitListWidth) private var dragged

    func body(content: Content) -> some View {
        if UIDevice.current.userInterfaceIdiom == .pad || UIDevice.current.userInterfaceIdiom == .mac {
            content.navigationSplitViewColumnWidth(min: dragged ?? Theme.Size.sidebarMin, ideal: dragged ?? Theme.Size.sidebar,
                                                   max: dragged ?? Theme.Size.sidebarMax)
                .background(GeometryReader { g in Color.clear.preference(key: SidebarEdge.self, value: g.frame(in: .global).maxX) })
        } else if UIDevice.current.userInterfaceIdiom == .phone {
            Group {
                if let dragged { content.navigationSplitViewColumnWidth(dragged) } else { content }
            }
            // Where the column ends, for the handle on the line beside it.
            .background(GeometryReader { g in Color.clear.preference(key: SidebarEdge.self, value: g.frame(in: .global).maxX) })
        } else {
            content
        }
    }
}

/// Where the list column ends, for `SidebarTopFill` and the handle on the line beside it (`SplitDividerHandle`).
struct SidebarEdge: PreferenceKey {
    static let defaultValue: CGFloat = 0
    static func reduce(value: inout CGFloat, nextValue: () -> CGFloat) { value = max(value, nextValue()) }
}

/// iPad: the list column starts below the status bar (its background cannot reach above it), so the strip over it was
/// the window's white and the list looked cut off at the top (2026-10-31). The split fills that strip with the
/// column's color, as wide as the column is.
struct SidebarTopFill: ViewModifier {
    /// The column is open (closed, its last frame stays reported: the fill must go by this).
    var shown: Bool
    /// The column's own color, where it has one (the one sidebar on the iPad, #4855); nil: the system sidebar's.
    var color: Color? = nil
    @State private var edge: CGFloat = 0

    func body(content: Content) -> some View {
        if UIDevice.current.userInterfaceIdiom == .pad {
            content
                .onPreferenceChange(SidebarEdge.self) { edge = $0 }
                .background(alignment: .topLeading) {
                    (color ?? (AppStyle.current.usesGlass ? Theme.systemSidebar : Theme.pageBackground))
                        .frame(width: shown ? max(edge, 0) : 0)
                        .animation(Theme.Motion.layout, value: shown)
                        .ignoresSafeArea()
                }
        } else {
            content
        }
    }
}

/// Set on the detail column of a `SplitNavigation` in regular width; nil in a single stack.
struct SplitDetail: Equatable {
    /// The page is the first one the list opened: there is nothing to go back to (the list is beside it).
    var isFirstPage: Bool
    /// The list column is closed (iOS: its button in the list's toolbar closed it). A first page with its own header
    /// (an Agents chat) has no system button to bring it back: its back does (`splitShowList`).
    var listHidden = false
    /// The page is a window of its own (the Mac's chat window), not a split's detail column: the window's title bar has
    /// its title.
    var ownWindow = false
}

extension EnvironmentValues {
    /// The width the list column was dragged to on the line beside it (#2624, `SplitDividerHandle`); nil: the system's.
    @Entry var splitListWidth: CGFloat?
    @Entry var splitDetail: SplitDetail?
    /// Pushes a page by value onto the stack the view is in (an extension's `push`, docs/extensions/extensions.md「Page navigation」): its
    /// destination is registered with the list's.
    @Entry var splitPush: SplitPush?
    /// Opens the list column of the split the page is in again (iOS); nil in a single stack.
    @Entry var splitShowList: SplitShowList?
}

/// Brings a `SplitNavigation`'s list column back.
struct SplitShowList {
    var show: () -> Void
}

/// Appends a value to a `SplitNavigation`'s path.
struct SplitPush {
    var push: (any Hashable) -> Void
}

extension View {
    /// A page the list opens: in the split's detail column its first page has no back button. `ownsBack`: the page shows or
    /// hides the system's back itself (an Agents chat); on iOS this leaves it alone, as an outer
    /// `navigationBarBackButtonHidden(false)` wins over the page's own `true` (iPhone Duo's side bars, 2026-11-04: the
    /// system's back beside the chat's own, two backs). On the Mac the title row draws no ‹ for it: the chat's header
    /// has its own, shown only with a page under the chat (#1337).
    func splitDetailPage(ownsBack: Bool = false) -> some View { modifier(SplitDetailPage(ownsBack: ownsBack)) }
}

private struct SplitDetailPage: ViewModifier {
    @Environment(\.splitDetail) private var split
    var ownsBack = false

    func body(content: Content) -> some View {
        #if os(macOS)
        content.modifier(MacDetailBack(shown: !ownsBack && split?.isFirstPage == false))
        #else
        // `ownsBack` is fixed for a page (no identity change as the path grows).
        if ownsBack { content } else { content.navigationBarBackButtonHidden(split?.isFirstPage == true) }
        #endif
    }
}

/// The detail column before anything is open: a quiet hint.
struct SplitPlaceholder: View {
    @Environment(\.splitDetail) private var split
    @Environment(\.splitShowList) private var showList
    #if os(iOS)
    @Environment(\.oneSidebarSideBars) private var sideBars
    #endif
    var symbol: String
    var title: String
    var message: String

    var body: some View {
        ContentUnavailableView {
            Label(title, systemImage: symbol)
        } description: {
            Text(message)
        } actions: {
            // The list closed with nothing open: this page hides its bar, where the system's button to open the list
            // would be, so it filled the screen with no way back to the list.
            if split?.listHidden == true, let showList {
                Button { showList.show() } label: { Text("Show List").lineLimit(1) }.themedButton()
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Theme.pageBackground)
        // Under the one sidebar's home (#4206), out of sight: on its side the bar there is the home's (New Chat, #4854).
        .toolbarVisibility(homeHasBar ? .automatic : .hidden, for: .navigationBar)
    }

    private var homeHasBar: Bool {
        #if os(iOS)
        sideBars
        #else
        false
        #endif
    }
}

/// The line between a split's list and its page on iOS (#2624, iPad and iPhone Duo open), as Feishu's: a handle on it
/// drags the list wider or narrower (`least`…`most`), the page taking the rest; dragged well past the least it closes the
/// list, and from the page's leading edge it brings the list back; a double tap gives the column back to the system.
/// The width and the closing are remembered under `key` (nil: no handle, the system's column, as before). The column's
/// own button closes and opens it as well, remembered the same. Not on the Mac or Apple Vision Pro.
struct SplitDividerHandle: ViewModifier {
    @Binding var columns: NavigationSplitViewVisibility
    let key: String?
    /// The list is laid over the page (the open iPhone Duo upright, #4852): its going away is a pick's, or a tap on the
    /// page, not a choice to keep it closed. nil: not known yet, nothing is kept until it is; once beside the page, the
    /// state then is kept.
    var overlays: Bool? = false
    /// The width dragged to; 0: never dragged (or given back), the system's.
    @StoredValue private var width: Double
    @StoredValue private var closed: Bool
    /// The split's frame (global), and the column's width as last laid out at rest.
    @State private var origin: CGFloat = 0
    @State private var total: CGFloat = 0
    @State private var measured: CGFloat = 0
    /// While the handle is held: where the drag began, the width it would leave, whether letting go closes the list.
    /// Gesture state: a drag the system cancels leaves nothing behind.
    @GestureState private var live: Live?

    struct Live: Equatable {
        var start: CGFloat
        var width: CGFloat
        var closing: Bool
    }

    init(columns: Binding<NavigationSplitViewVisibility>, key: String?, overlays: Bool? = false) {
        _columns = columns
        self.key = key
        self.overlays = overlays
        _width = StoredValue(wrappedValue: 0, (key ?? "split.list") + ".width")
        _closed = StoredValue(wrappedValue: false, (key ?? "split.list") + ".closed")
    }

    /// Whether the list kept under `key` was closed when last left; the split starts so.
    static func startsClosed(_ key: String?) -> Bool {
        #if os(iOS)
        guard let key, UIDevice.current.userInterfaceIdiom == .pad || UIDevice.current.userInterfaceIdiom == .phone else { return false }
        return UserDefaults.standard.bool(forKey: key + ".closed")
        #else
        false
        #endif
    }

    private var shown: Bool { columns != .detailOnly }
    private var phone: Bool { UIDevice.current.userInterfaceIdiom == .phone }

    /// The least and the most the list is dragged to: an iPhone Duo's open display is narrower than an iPad; the page
    /// keeps `splitDetailMin` beside it.
    private var least: CGFloat { phone ? Theme.Size.sidebarMinPhone : Theme.Size.sidebarMin }
    private var most: CGFloat { max(least, min(Theme.Size.sidebarMax, total - Theme.Size.splitDetailMin)) }
    private func fitted(_ w: CGFloat) -> CGFloat { min(max(w, least), most) }

    /// The width the list has now: the one kept (fitted to the room there is, which a turn or another window changes),
    /// else the system's as laid out.
    private var current: CGFloat { width > 0 ? fitted(CGFloat(width)) : max(least, measured) }

    /// The width the column is given: the one being dragged to, else the one kept; nil: the system's. On iPhone Duo a
    /// column with no width yet takes one only when the drag ends (`SidebarWidth`).
    private var applied: CGFloat? {
        if let live, !(phone && width == 0) { return live.width }
        return width > 0 ? current : nil
    }

    func body(content: Content) -> some View {
        #if os(iOS)
        if let key, UIDevice.current.userInterfaceIdiom == .pad || phone {
            content
                .environment(\.splitListWidth, applied)
                .onPreferenceChange(SidebarEdge.self) { edge in if live == nil { measured = edge - origin } }
                .onGeometryChange(for: CGRect.self) { $0.frame(in: .global) } action: { origin = $0.minX; total = $0.width }
                .overlay(alignment: .leading) {
                    handle
                        .offset(x: shown ? (live?.width ?? current) - Theme.Split.handleWidth / 2 : 0)
                        .accessibilityIdentifier("split.divider." + key)
                        // Halfway down the screen, not the safe area: under the status bar the safe area's middle sat
                        // visibly low on the open Duo upright (#4852).
                        .frame(maxHeight: .infinity)
                        .ignoresSafeArea(.container, edges: .vertical)
                }
                .onAppear { if closed, shown { columns = .detailOnly } }
                .onChange(of: columns) { _, now in if overlays == false { closed = now == .detailOnly } }
                .onChange(of: overlays) { _, now in if now == false { closed = !shown } }
        } else {
            content
        }
        #else
        content
        #endif
    }

    /// The handle: a short bar on the line (at the page's edge while the list is closed), its touch area wider than it.
    private var handle: some View {
        Capsule()
            .fill(live?.closing == true ? Theme.danger : live == nil ? Theme.Split.handle : Theme.Split.handleHeld)
            .frame(width: Theme.Size.barWide, height: Theme.Size.tapTarget)
            .scaledFrame(width: Theme.Split.handleWidth, height: Theme.Split.handleHeight)
            .contentShape(.rect)
            .gesture(drag)
            .onTapGesture(count: 2) { width = 0 } // the system's column again
            .animation(Theme.Motion.standard, value: live?.closing)
            .accessibilityElement()
            .accessibilityLabel(shown ? String(localized: "Resize the list") : String(localized: "Show List"))
            .accessibilityAddTraits(.isButton)
            .accessibilityAction { if !shown { withAnimation(Theme.Motion.layout) { columns = SplitColumns.shown } } }
            .accessibilityAdjustableAction { direction in
                guard shown else { return }
                switch direction {
                case .increment: width = Double(fitted(current + Theme.Split.step))
                case .decrement: width = Double(fitted(current - Theme.Split.step))
                @unknown default: break
                }
            }
            .accessibilityActions {
                if shown {
                    Button(String(localized: "Hide List")) { withAnimation(Theme.Motion.layout) { columns = .detailOnly } }
                }
            }
    }

    private var drag: some Gesture {
        DragGesture(minimumDistance: Theme.Split.slop, coordinateSpace: .global)
            .updating($live) { g, state, _ in
                // Mostly sideways, or it is the list's or the page's scroll and taps, not this.
                guard shown, state != nil || abs(g.translation.width) > abs(g.translation.height) else { return }
                let start = state?.start ?? current
                let wanted = start + g.translation.width
                state = Live(start: start, width: fitted(wanted), closing: wanted < least - Theme.Split.closesPast)
            }
            .onEnded { g in
                guard abs(g.translation.width) > abs(g.translation.height) else { return }
                if shown {
                    let wanted = current + g.translation.width
                    if wanted < least - Theme.Split.closesPast {
                        withAnimation(Theme.Motion.layout) { columns = .detailOnly }
                    } else if abs(g.translation.width) > Theme.Split.slop {
                        width = Double(fitted(wanted))
                    }
                } else if g.translation.width > Theme.Split.opensPast {
                    withAnimation(Theme.Motion.layout) { columns = SplitColumns.shown }
                }
            }
    }
}
