import XCTest

/// The iPad's sidebar is the one sidebar (docs/apple/ios.md, "iPad"; #4206): Main Chat first, then Inbox … Knowledge, the
/// rarer places under More, Recent and the Agents list, New Chat + search and Settings at the foot. No tabs: Inbox opens
/// beside the sidebar, Settings as a sheet. `OneSidebarTests` covers the rest of the column. Against scripts/mock/mock_hub.py
/// (PERSONA_MOCK_HUB), on an iPad simulator, portrait and landscape. TEST_RUNNER_PERSONA_SHOTS=<dir> keeps screenshots;
/// TEST_RUNNER_PERSONA_HOLD=<seconds> stays on each page for `simctl io screenshot` (XCUITest's own turn in landscape).
final class IPadSidebarTests: XCTestCase {
    private var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard UIDevice.current.userInterfaceIdiom == .pad else { throw XCTSkip("iPad only") }
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB)")
        }
        app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "assistant"]
    }

    override func tearDown() {
        XCUIDevice.shared.orientation = .portrait
        super.tearDown()
    }

    @MainActor private func shot(_ name: String) {
        if let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] {
            try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("ipad-sidebar-\(name).png"))
        }
        if let hold = ProcessInfo.processInfo.environment["PERSONA_HOLD"].flatMap(UInt32.init) { sleep(hold) }
    }

    @MainActor private func element(_ id: String) -> XCUIElement { app.descendants(matching: .any)[id].firstMatch }

    @MainActor func testSidebarIsTheIPhones() {
        for (name, orientation) in [("portrait", UIDeviceOrientation.portrait), ("landscape", .landscapeLeft)] {
            XCUIDevice.shared.orientation = orientation
            app.launchPinned()
            let main = element("sidebar-main"), inbox = element("sidebar-inbox"), knowledge = element("sidebar-knowledge")
            XCTAssertTrue(main.waitForExistence(timeout: 20), "\(name): no Main Chat row:\n\(app.debugDescription)")
            XCTAssertTrue(inbox.exists && knowledge.exists, "\(name): the places are missing")
            // Main Chat first, then Inbox … Knowledge (the sidebar is the left column).
            XCTAssertLessThan(main.frame.minY, inbox.frame.minY, "\(name): Main Chat is not on top")
            XCTAssertEqual(app.buttons.matching(identifier: "sidebar-main").count, 1, "\(name): the main conversation shows more than once")
            XCTAssertLessThan(inbox.frame.minY, knowledge.frame.minY)
            XCTAssertLessThan(main.frame.maxX, app.windows.firstMatch.frame.width / 2 + 100, "\(name): not in the left column")
            // New Chat and Settings at the foot, below the places, New Chat on the left.
            let new = element("sidebar-new"), settings = element("sidebar-settings")
            XCTAssertTrue(new.exists && settings.exists, "\(name): no 新对话 / 设置 at the foot")
            XCTAssertGreaterThan(new.frame.minY, knowledge.frame.maxY)
            XCTAssertEqual(new.frame.midY, settings.frame.midY, accuracy: 4, "\(name): 新对话 and 设置 in one row")
            XCTAssertLessThan(new.frame.minX, settings.frame.minX)
            // No tabs at all: Agents is the same column, Inbox and Settings its rows (#4206).
            XCTAssertEqual(app.tabBars.count, 0, "\(name): a tab bar is still there:\n\(app.debugDescription)")
            XCTAssertTrue(element("sidebar-agents-heading").exists, "\(name): no Agents in the column")
            shot("home-\(name)")

            // Inbox opens beside the sidebar, which stays.
            inbox.tap()
            let more = element("inbox.more")
            XCTAssertTrue(more.waitForExistence(timeout: 10), "\(name): no inbox:\n\(app.debugDescription)")
            XCTAssertGreaterThan(more.frame.minX, inbox.frame.maxX, "\(name): the inbox is not beside the sidebar")
            XCTAssertTrue(main.exists && inbox.isHittable, "\(name): the sidebar went away with the inbox")
            shot("inbox-\(name)")
            // Main Chat goes back to the conversation.
            main.tap()
            XCTAssertTrue(more.waitForNonExistence(timeout: 5), "\(name): Main Chat left the inbox open")

            // Settings is a sheet with its ×.
            settings.tap()
            let close = app.buttons["settings-close"].firstMatch
            XCTAssertTrue(close.waitForExistence(timeout: 10), "\(name): no 设置 sheet:\n\(app.debugDescription)")
            shot("settings-\(name)")
            close.tap()
            XCTAssertTrue(close.waitForNonExistence(timeout: 5), "\(name): 设置 did not close")
            XCTAssertTrue(main.isHittable)
            app.terminate()
        }
    }
}
