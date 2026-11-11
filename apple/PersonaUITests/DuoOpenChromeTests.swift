import XCTest

/// iPhone Duo open, the inner screen, laid out as ChatGPT's (#2623), in the one sidebar (#4206, `OneSidebarTests` for the
/// column itself):
/// - no tab bar down the side, and the face back at the top of Persona's page (the switch that had its place is gone);
/// - the face's menu (Knowledge, Details, the copies) at the top, the main conversation's own on its row's long press;
/// - search over the whole window, its field at the foot (#3705);
/// - the iPhone's new conversation with Needs You (#3680), and Settings over the window (#3701).
///
/// A phone at regular width only: the Duo simulator opened in Xcode's Device Hub, on its side
/// (TEST_RUNNER_PERSONA_ORIENTATION=landscape) or upright. The iPad has the same column (IPadSidebarTests).
/// Taps and long presses only (upright, synthetic drags miss on the open Duo). Against scripts/mock/mock_hub.py
/// (PERSONA_MOCK_HUB).
/// TEST_RUNNER_PERSONA_HOLD=<seconds> holds each state, named in an activity `duo-shot <name>`, for
/// `simctl io --display=3 screenshot`; XCUITest's own screenshot captures the dark outer screen.
final class DuoOpenChromeTests: XCTestCase {
    private var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard UIDevice.current.userInterfaceIdiom == .phone else { throw XCTSkip("a phone at regular width only (iPhone Duo open)") }
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB)")
        }
        XCUIDevice.shared.orientation = ProcessInfo.processInfo.environment["PERSONA_ORIENTATION"] == "landscape" ? .landscapeLeft : .portrait
        app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "assistant", "-app.tab", "assistant", "-agents.list.closed", "NO"]
        app.launchPinned()
        guard app.windows.firstMatch.frame.width > 600 else { throw XCTSkip("regular width only (iPhone Duo open)") }
        // Upright the column lies over the page and may start away (#4852).
        let main = app.descendants(matching: .any)["sidebar-main"].firstMatch
        let back = app.descendants(matching: .any)["assistant-sidebar-button"].firstMatch
        _ = main.waitForExistence(timeout: 20)
        if !main.isHittable, back.waitForExistence(timeout: 5), back.isHittable {
            back.tap()
            XCTAssertTrue(main.waitForExistence(timeout: 5), "the column did not come out")
            sleep(1) // the slide
        }
    }

    /// The column out, from the page's button if it is away (upright, after a pick).
    @MainActor private func columnOut() {
        let main = element("sidebar-main"), back = element("assistant-sidebar-button")
        if main.exists && main.isHittable { return }
        if back.waitForExistence(timeout: 5), back.isHittable {
            back.tap()
            XCTAssertTrue(main.waitForExistence(timeout: 5), "the column did not come out")
            sleep(1) // the slide
        }
    }

    /// Upright: the column lies over the page (#4852) instead of beside it.
    @MainActor private var upright: Bool {
        let w = app.windows.firstMatch.frame
        return w.height > w.width
    }

    override func tearDown() {
        XCUIDevice.shared.orientation = .portrait
        super.tearDown()
    }

    @MainActor private func element(_ id: String) -> XCUIElement { app.descendants(matching: .any)[id].firstMatch }

    /// Persona's face over its name: `persona-avatar` on a hub with several assistants (the mock hub's), else `assistant-menu`.
    @MainActor private var face: XCUIElement {
        app.buttons.matching(NSPredicate(format: "identifier IN %@", ["assistant-menu", "persona-avatar"])).firstMatch
    }

    @MainActor private func shot(_ name: String) {
        XCTContext.runActivity(named: "duo-shot \(name)") { _ in
            if let hold = ProcessInfo.processInfo.environment["PERSONA_HOLD"].flatMap(UInt32.init) { sleep(hold) }
        }
    }

    /// No tab bar and no switch: the face at the top of Persona's page, the fold in the column's header over the places.
    /// Folding the column and bringing it back from the page.
    @MainActor func testTheFaceIsOnTopAndNoSwitch() {
        let main = element("sidebar-main"), face = self.face, collapse = element("assistant-sidebar-collapse")
        XCTAssertTrue(main.waitForExistence(timeout: 20), "no main row:\n\(app.debugDescription)")
        XCTAssertEqual(app.tabBars.count, 0, "the system's tab bar is still there:\n\(app.debugDescription)")
        XCTAssertFalse(element("shell-assistant").exists || element("page-tab-switch").exists, "the Assistant | Agents switch is still there")
        XCTAssertTrue(face.exists, "no face at the top of the page:\n\(app.debugDescription)")
        let window = app.windows.firstMatch.frame
        XCTAssertLessThan(face.frame.minY, window.minY + 100, "the face is not at the top")
        if !upright { XCTAssertGreaterThan(face.frame.minX, main.frame.maxX - 1, "the face is not on the page beside the column") }
        XCTAssertLessThan(collapse.frame.maxY, main.frame.minY + 1, "the fold is not at the column's top")
        let back = element("assistant-sidebar-button")
        XCTAssertFalse(back.exists && back.isHittable, "the page's Chat History shows beside the column")
        shot("chrome")

        collapse.tap()
        XCTAssertTrue(collapse.waitForNonExistence(timeout: 5), "the column did not fold")
        XCTAssertTrue(back.waitForExistence(timeout: 5) && back.isHittable, "nothing on the page brings the column back")
        shot("folded")
        back.tap()
        XCTAssertTrue(collapse.waitForExistence(timeout: 5), "the column did not come back")
    }

    /// Search covers the window, as ChatGPT's (#3705): its field at the foot over the keyboard with × beside it, the
    /// conversations above; what is typed narrows them, a row opens its conversation, × closes.
    @MainActor func testSearchCoversTheWindowWithItsFieldAtTheFoot() {
        let search = element("sidebar-search")
        XCTAssertTrue(search.waitForExistence(timeout: 20), "no Search at the column's foot:\n\(app.debugDescription)")
        search.tap()
        let field = element("full-search-field"), close = app.buttons["full-search-close"].firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 5), "Search opened no field:\n\(app.debugDescription)")
        let keyboard = app.keyboards.firstMatch
        XCTAssertTrue(keyboard.waitForExistence(timeout: 5), "the field did not take the keyboard")
        sleep(1)
        let window = app.windows.firstMatch.frame
        XCTAssertGreaterThan(field.frame.minY, window.midY - 100, "the field is not at the foot: \(field.frame) in \(window)")
        XCTAssertLessThanOrEqual(field.frame.maxY, keyboard.frame.minY + 1, "the field is under the keyboard: \(field.frame), \(keyboard.frame)")
        XCTAssertTrue(close.exists && close.frame.minX > field.frame.maxX - 1, "no × beside the field")
        let rows = app.buttons.matching(identifier: "full-search-row")
        XCTAssertGreaterThan(rows.count, 1, "no conversations above the field:\n\(app.debugDescription)")
        XCTAssertFalse(element("sidebar-new").isHittable, "the page shows beside the search")
        shot("search")
        field.typeText("韩梅梅")
        let hit = rows.firstMatch
        XCTAssertTrue(hit.waitForExistence(timeout: 5) && hit.label.contains("韩梅梅"), "typing did not narrow the list: \(hit.label)")
        shot("search-typed")
        hit.tap()
        XCTAssertTrue(field.waitForNonExistence(timeout: 5), "a row did not close the search")
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "韩梅梅")).firstMatch.waitForExistence(timeout: 10),
                      "the row did not open its conversation:\n\(app.debugDescription)")
        columnOut() // upright the pick slid it away (#4852)
        search.tap()
        XCTAssertTrue(close.waitForExistence(timeout: 5), "Search did not open again")
        close.tap()
        XCTAssertTrue(field.waitForNonExistence(timeout: 5), "× did not close the search")
    }

    /// The main conversation's long press holds its own menu (the copies), as in the drawer; the face at the top keeps
    /// Details and the copies of where it runs.
    @MainActor func testTheMainRowAndTheFaceHoldTheirMenus() {
        let main = element("sidebar-main")
        XCTAssertTrue(main.waitForExistence(timeout: 20), "no main row:\n\(app.debugDescription)")
        main.press(forDuration: 1.2)
        for label in ["拷贝会话 ID", "拷贝诊断"] {
            XCTAssertTrue(app.buttons[label].firstMatch.waitForExistence(timeout: 5), "no \(label) in the main row's menu:\n\(app.debugDescription)")
        }
        shot("main-menu")
        // The menu away through its harmless copy: a tap beside it does not land on the open Duo upright, where
        // synthetic input off an element misses.
        app.buttons["拷贝会话 ID"].firstMatch.tap()
        sleep(1) // the menu's closing (its items' names are also other buttons' on the page: no wait on them)
        // Upright the column lies over the face (#4852): folded first, if that tap did not send it away already.
        let collapse = element("assistant-sidebar-collapse")
        if upright, collapse.exists, collapse.isHittable {
            collapse.tap()
            sleep(1) // the slide, before the face takes a tap
        }
        let face = self.face
        XCTAssertTrue(face.waitForExistence(timeout: 5) && face.isHittable, "no face at the top:\n\(app.debugDescription)")
        face.tap()
        for label in ["详细信息", "拷贝目录", "拷贝信息"] {
            XCTAssertTrue(app.buttons[label].firstMatch.waitForExistence(timeout: 5), "no \(label) in the face's menu:\n\(app.debugDescription)")
        }
        shot("face-menu")
    }

    /// A new conversation is the iPhone's (#3680): what needs the user over the composer (Agents waiting, the Inbox's
    /// Needs You), as `ShellHome` shows it on every iPhone; upright as well, where New Chat slides the column away (#3641).
    @MainActor func testTheNewConversationShowsWhatNeedsYou() {
        let new = app.drawerNewChatButton
        XCTAssertTrue(new.waitForExistence(timeout: 20), "no New Chat in the sidebar:\n\(app.debugDescription)")
        new.tap()
        XCTAssertTrue(element("shell-needs-you").waitForExistence(timeout: 10), "no Needs You over the composer:\n\(app.debugDescription)")
        XCTAssertTrue(element("assistant-welcome").exists, "no welcome on the new conversation")
        shot("new-conversation")
    }

    /// Settings covers the window, as ChatGPT's account page does there, not a sheet over half of it (#3701): one column
    /// across, its × at the top right, which closes it.
    @MainActor func testSettingsCoversTheWindowAndCloses() {
        let settings = element("sidebar-settings")
        XCTAssertTrue(settings.waitForExistence(timeout: 20), "no Settings at the column's foot:\n\(app.debugDescription)")
        settings.tap()
        let close = app.buttons["settings-close"].firstMatch
        XCTAssertTrue(close.waitForExistence(timeout: 10), "Settings has no ×:\n\(app.debugDescription)")
        shot("settings")
        let window = app.windows.firstMatch.frame, list = element("settings-list")
        XCTAssertTrue(list.exists, "no settings list:\n\(app.debugDescription)")
        XCTAssertGreaterThan(list.frame.width, window.width * 0.8, "Settings does not cover the window: \(list.frame) in \(window)")
        // On its side in the system's side column at the window's edge; upright in the top bar's trailing items, which keep
        // clear of the display's top right corner.
        XCTAssertGreaterThan(close.frame.midX, window.width * (upright ? 0.6 : 0.85), "the × is not at the window's right: \(close.frame) in \(window)")
        // In the system's side column under the clock where it has one, as ChatGPT's account page has its ×.
        XCTAssertLessThan(close.frame.minY, window.minY + window.height / 4, "the × is not at the top: \(close.frame) in \(window)")
        close.tap()
        XCTAssertTrue(close.waitForNonExistence(timeout: 5), "the × did not close Settings")
        XCTAssertTrue(settings.waitForExistence(timeout: 5) && settings.isHittable, "the page did not come back")
    }
}
