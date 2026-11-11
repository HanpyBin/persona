import XCTest

/// One sidebar at regular width (#4206): iPad and the open iPhone Duo, in both postures, show Persona's places, Recent and
/// the Agents list in one column, with Persona's page or the agent's chat beside it, and no Assistant | Agents switch.
///
/// - the column's header (Persona's mark and name, the fold) and foot (New Chat; search and Settings);
/// - More opens inline to Messages (when on), Meeting Recording, Health and Machines;
/// - the grouping's segments under the Agents heading, one tap each, the same choice as the ⋯ menu's;
/// - an agent opened from the list beside Persona's page, its row the selected one; a Persona row closes it;
/// - Recent starts folded on an open Duo held on its side;
/// - upright on the open Duo the column is over the page, and a pick that changes the page slides it away (#4852); ‹ on
///   an agent opened from it goes back to Persona's page (#3641).
///
/// Against scripts/mock/mock_hub.py (PERSONA_MOCK_HUB). On the iPad simulator, or the Duo simulator opened in Xcode's
/// Device Hub, on its side (TEST_RUNNER_PERSONA_ORIENTATION=landscape) or upright; the closed Duo and other iPhones skip.
/// Taps and long presses only (upright, synthetic drags miss on the open Duo). TEST_RUNNER_PERSONA_HOLD=<seconds> holds
/// each state, named in an activity `one-sidebar <name>`, for `simctl io screenshot` (`--display=3` on the Duo).
final class OneSidebarTests: XCTestCase {
    private var app: XCUIApplication!
    private var hub = ""

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB)")
        }
        self.hub = hub
        XCUIDevice.shared.orientation = ProcessInfo.processInfo.environment["PERSONA_ORIENTATION"] == "landscape" ? .landscapeLeft : .portrait
        app = XCUIApplication()
        // Recent as a new install leaves it: folded on an open Duo on its side, open otherwise.
        app.launchArguments = ["-pairURL", pair, "-tab", "assistant", "-app.tab", "assistant", "-dev.openChat", "",
                               "-oneSidebar.recentFolded", "", "-dev.grouping", "repo"]
    }

    override func tearDown() {
        XCUIDevice.shared.orientation = .portrait
        super.tearDown()
    }

    private var pair: String {
        "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
    }

    /// Launches and waits for the column; skips where the layout is not the one sidebar (a phone at compact width).
    @MainActor private func launch(_ extra: [String] = []) throws {
        app.launchArguments += extra
        app.launchPinned()
        XCTAssertTrue(element("sidebar-main").waitForExistence(timeout: 20), "no main row:\n\(app.debugDescription)")
        guard app.windows.firstMatch.frame.width > 600 else { throw XCTSkip("regular width only (iPad, iPhone Duo open)") }
    }

    @MainActor private func element(_ id: String) -> XCUIElement { app.descendants(matching: .any)[id].firstMatch }

    /// The open Duo held on its side: Recent starts folded there.
    @MainActor private var duoOnItsSide: Bool {
        let w = app.windows.firstMatch.frame
        return UIDevice.current.userInterfaceIdiom == .phone && w.width > w.height
    }

    /// The open Duo held upright: the column slides in over the page (#4852).
    @MainActor private var duoUpright: Bool {
        let w = app.windows.firstMatch.frame
        return UIDevice.current.userInterfaceIdiom == .phone && w.height > w.width
    }

    @MainActor private func shot(_ name: String) {
        XCTContext.runActivity(named: "one-sidebar \(name)") { _ in
            if let hold = ProcessInfo.processInfo.environment["PERSONA_HOLD"].flatMap(UInt32.init) { sleep(hold) }
        }
    }

    /// The column top to bottom (header, the places, More, Recent, the Agents heading and its segments, the list) and the
    /// foot; no switch and no tab bar anywhere; Recent folded on the Duo's side and open elsewhere.
    @MainActor func testTheColumnHasThePlacesRecentAndAgents() throws {
        try launch()
        let window = app.windows.firstMatch.frame
        XCTAssertEqual(app.tabBars.count, 0, "a tab bar is still there:\n\(app.debugDescription)")
        XCTAssertFalse(element("shell-assistant").exists || element("page-tab-switch").exists, "the Assistant | Agents switch is still there")
        let collapse = element("assistant-sidebar-collapse"), main = element("sidebar-main"), inbox = element("sidebar-inbox")
        let more = element("sidebar-more"), recent = element("sidebar-recent-fold"), heading = element("sidebar-agents-heading")
        for (name, e) in [("fold", collapse), ("Inbox", inbox), ("More", more), ("Recent", recent), ("Agents heading", heading)] {
            XCTAssertTrue(e.exists, "no \(name) in the column:\n\(app.debugDescription)")
        }
        XCTAssertLessThan(collapse.frame.maxY, main.frame.minY + 1, "the fold is not over the places")
        XCTAssertLessThan(main.frame.minY, inbox.frame.minY, "Main Chat is not first")
        XCTAssertLessThan(inbox.frame.minY, more.frame.minY)
        XCTAssertLessThan(more.frame.minY, recent.frame.minY, "Recent is not under the places")
        XCTAssertLessThan(main.frame.maxX, window.midX + 100, "the column is not on the left")
        // Beside the page on the iPad and the open Duo on its side, the fold and the page's New Chat on one line (2026-11-11,
        // the owner). Upright on the open Duo the column is over the page instead (#4852): the page keeps its full width.
        let pageNew = element("assistant-new")
        XCTAssertTrue(pageNew.exists, "no New Chat on the page")
        if duoUpright {
            XCTAssertGreaterThan(pageNew.frame.maxX, window.maxX - 80, "the page is not its full width under the column")
        } else {
            let face = app.buttons.matching(NSPredicate(format: "identifier IN %@", ["assistant-menu", "persona-avatar"])).firstMatch
            XCTAssertTrue(face.exists, "no face on the page:\n\(app.debugDescription)")
            XCTAssertGreaterThan(face.frame.midX, collapse.frame.maxX + 100, "the page is under the column, not beside it")
            XCTAssertEqual(pageNew.frame.midY, collapse.frame.midY, accuracy: 2, "the fold and the page's New Chat are not on one line")
        }
        // The rarer places wait under More.
        XCTAssertFalse(element("sidebar-machines").exists, "Machines shows before More is opened")
        // The foot: New Chat on the left, search and Settings on the right, in one row at the column's foot.
        let new = element("sidebar-new"), search = element("sidebar-search"), settings = element("sidebar-settings")
        XCTAssertTrue(new.exists && search.exists && settings.exists, "no New Chat, search or Settings at the foot")
        XCTAssertEqual(new.frame.midY, settings.frame.midY, accuracy: 4, "New Chat and Settings not in one row")
        XCTAssertLessThan(new.frame.maxX, search.frame.minX)
        XCTAssertGreaterThan(new.frame.minY, window.maxY - 160, "the foot is not at the column's foot")
        // Recent: folded on the open Duo's side, open elsewhere (no stored choice).
        let recentRows = app.buttons.matching(NSPredicate(format: "identifier BEGINSWITH %@ AND identifier != %@ AND identifier != %@",
                                                          "sidebar-recent-", "sidebar-recent-fold", "sidebar-recent-all"))
        if duoOnItsSide {
            XCTAssertEqual(recentRows.count, 0, "Recent is open on the Duo's side")
        } else {
            XCTAssertGreaterThan(recentRows.count, 0, "Recent is folded:\n\(app.debugDescription)")
        }
        shot("home")
        // Folding the column and bringing it back from the page's own button.
        collapse.tap()
        let back = element("assistant-sidebar-button")
        XCTAssertTrue(back.waitForExistence(timeout: 5) && back.isHittable, "nothing on the page brings the column back")
        shot("folded")
        back.tap()
        XCTAssertTrue(collapse.waitForExistence(timeout: 5), "the column did not come back")
    }

    /// More opens inline to Meeting Recording, Health and Machines, and folds again; a place opens beside the column.
    @MainActor func testMoreOpensInlineAndAPlaceOpensBeside() throws {
        try launch()
        let more = element("sidebar-more")
        more.tap()
        let machines = element("sidebar-machines"), health = element("sidebar-health"), recordings = element("sidebar-recordings")
        XCTAssertTrue(machines.waitForExistence(timeout: 5) && health.exists && recordings.exists, "More did not open:\n\(app.debugDescription)")
        XCTAssertGreaterThan(recordings.frame.minY, more.frame.minY, "More's places are not under it")
        shot("more")
        let inbox = element("sidebar-inbox")
        inbox.tap()
        let page = element("inbox.more")
        XCTAssertTrue(page.waitForExistence(timeout: 10), "the Inbox did not open:\n\(app.debugDescription)")
        XCTAssertGreaterThan(page.frame.minX, inbox.frame.maxX - 1, "the Inbox is not beside the column")
        XCTAssertTrue(inbox.isHittable, "the column went with the Inbox")
        more.tap()
        XCTAssertTrue(machines.waitForNonExistence(timeout: 5), "More did not fold")
    }

    /// The segments under the Agents heading pick the grouping, as the ⋯ menu does (`dev.grouping`): By Repository is
    /// the one lit at launch, and Machine lights when tapped.
    @MainActor func testTheSegmentsPickTheGrouping() throws {
        try launch(["-oneSidebar.recentFolded", "1"])
        let segments = element("agents.grouping.segments")
        XCTAssertTrue(segments.waitForExistence(timeout: 10), "no grouping segments:\n\(app.debugDescription)")
        XCTAssertGreaterThan(segments.frame.minY, element("sidebar-agents-heading").frame.minY, "the segments are not under the heading")
        XCTAssertFalse(element("agents.grouping").exists, "the phone's grouping pill shows beside the segments")
        let repo = segments.buttons["仓库"].firstMatch, machine = segments.buttons["机器"].firstMatch
        XCTAssertTrue(repo.exists && machine.exists, "no 仓库 / 机器 segment:\n\(segments.debugDescription)")
        XCTAssertTrue(repo.isSelected, "仓库 is not the one lit")
        shot("segments")
        machine.tap()
        XCTAssertTrue(machine.waitForSelected(), "机器 did not light")
        XCTAssertFalse(repo.isSelected, "仓库 stayed lit")
    }

    /// An agent opened from the list shows beside the column with its row selected and Main Chat not; Main Chat closes it
    /// and is the selected row again.
    @MainActor func testAnOpenedAgentIsSelectedAndPersonaClosesIt() throws {
        try launch(["-openSession", "macbook/a1f3c2d4-0000-4000-8000-000000000002"])
        // The open header's title, or the slim one it folds to (a chat that opens scrolled to its end).
        let title = app.descendants(matching: .any).matching(NSPredicate(format: "identifier IN %@", ["chat.title", "chat.compactTitle"])).firstMatch
        XCTAssertTrue(title.waitForExistence(timeout: 20), "the agent did not open:\n\(app.debugDescription)")
        let main = element("sidebar-main")
        XCTAssertGreaterThan(title.frame.minX, main.frame.maxX - 1, "the agent is not beside the column")
        XCTAssertFalse(main.isSelected, "Main Chat is selected while an agent is open")
        let selected = app.descendants(matching: .any).matching(NSPredicate(format: "(identifier == %@ OR identifier == %@) AND isSelected == true",
                                                                             "agents.row", "agents.compactRow"))
        // Its row is under the Needs You cards and the other groups: down the column until it is built.
        let column = app.collectionViews.firstMatch
        var swipes = 0
        while selected.count == 0 && swipes < 8 { column.swipeUp(); swipes += 1 }
        XCTAssertEqual(selected.count, 1, "the open agent's row is not the one selected:\n\(app.debugDescription)")
        shot("agent")
        for _ in 0..<swipes { column.swipeDown() }
        main.tap()
        XCTAssertTrue(title.waitForNonExistence(timeout: 10), "Main Chat did not close the agent")
        XCTAssertTrue(main.waitForSelected(), "Main Chat is not the selected row")
        for _ in 0..<swipes { column.swipeUp() } // back where the agent's row was
        XCTAssertEqual(selected.count, 0, "an agent row stays selected:\n\(app.debugDescription)")
    }

    /// Messages is under More while it is switched on and the hub has Feishu chats; it opens beside the column, its row
    /// selected, and a Persona row takes the column back.
    @MainActor func testMessagesIsUnderMore() throws {
        try launch(["-integrations.lark.enabled", "YES"])
        element("sidebar-more").tap()
        let messages = element("sidebar-messages")
        XCTAssertTrue(messages.waitForExistence(timeout: 10), "no Messages under More:\n\(app.debugDescription)")
        messages.tap()
        let list = element("feishu-conversations")
        XCTAssertTrue(list.waitForExistence(timeout: 10), "Messages did not open:\n\(app.debugDescription)")
        // The list scrolls under the floating column (its frame starts at the window's edge): its rows' text is beside it.
        let text = list.staticTexts.firstMatch
        XCTAssertTrue(text.waitForExistence(timeout: 10), "Messages shows no chat:\n\(list.debugDescription)")
        XCTAssertGreaterThan(text.frame.minX, messages.frame.maxX - 1, "Messages is not beside the column")
        XCTAssertTrue(messages.waitForSelected(), "the Messages row is not selected")
        XCTAssertFalse(element("sidebar-main").isSelected, "Main Chat stays selected beside Messages")
        shot("messages")
        element("sidebar-main").tap()
        XCTAssertTrue(list.waitForNonExistence(timeout: 10), "Main Chat did not take the column back from Messages")
    }

    /// Settings: over the window on the open Duo, a sheet on the iPad; its × closes it and the column is back.
    @MainActor func testSettingsOpensAndCloses() throws {
        try launch()
        let settings = element("sidebar-settings")
        settings.tap()
        let close = app.buttons["settings-close"].firstMatch
        XCTAssertTrue(close.waitForExistence(timeout: 10), "Settings has no ×:\n\(app.debugDescription)")
        shot("settings")
        close.tap()
        XCTAssertTrue(close.waitForNonExistence(timeout: 5), "the × did not close Settings")
        XCTAssertTrue(settings.waitForExistence(timeout: 5) && settings.isHittable, "the column did not come back")
    }

    /// Search covers the window (#3705): its field takes the keyboard, a row opens its conversation beside the column.
    @MainActor func testSearchOpensAConversation() throws {
        try launch()
        element("sidebar-search").tap()
        let field = element("full-search-field")
        XCTAssertTrue(field.waitForExistence(timeout: 5), "search opened no field:\n\(app.debugDescription)")
        let rows = app.buttons.matching(identifier: "full-search-row")
        XCTAssertGreaterThan(rows.count, 0, "no conversations in the search:\n\(app.debugDescription)")
        rows.firstMatch.tap()
        XCTAssertTrue(field.waitForNonExistence(timeout: 5), "a row did not close the search")
        XCTAssertTrue(element("sidebar-main").isHittable, "the column is not back")
    }
}

private extension XCUIElement {
    /// Waits for VoiceOver's selected trait (the row the detail column shows).
    func waitForSelected(timeout: TimeInterval = 5) -> Bool {
        XCTWaiter.wait(for: [XCTNSPredicateExpectation(predicate: NSPredicate(format: "isSelected == true"), object: self)],
                       timeout: timeout) == .completed
    }
}
