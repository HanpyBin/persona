import XCTest

/// The sidebar's assistant switcher (docs/assistant/managers.md "Clients"): what its list presents once it has gone away.
/// New Assistant asks for a name and then shows the install code, Manage Assistants… opens Settings › Assistants, and a
/// row still installing shows its install code. Against scripts/mock/mock_hub.py (three installed managers and one
/// installing by default): `TEST_RUNNER_PERSONA_SWITCHER_HUB=http://127.0.0.1:<port>`. `TEST_RUNNER_PERSONA_SHOTS=<dir>`
/// keeps the screens, named by `TEST_RUNNER_PERSONA_SHOTS_APPEARANCE` (light / dark).
final class AssistantSwitcherUITests: XCTestCase {
    private var env: [String: String] { ProcessInfo.processInfo.environment }
    private var hub = ""

    override func setUpWithError() throws {
        continueAfterFailure = false
        XCUIDevice.shared.orientation = .portrait
        guard let h = env["PERSONA_SWITCHER_HUB"] else { throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_SWITCHER_HUB)") }
        hub = h
    }

    @MainActor func testNewAssistantAsksForANameThenShowsItsCode() throws {
        let app = openList()
        element(app, "assistant-switcher-new").tap()
        let alert = app.alerts.firstMatch
        XCTAssertTrue(alert.waitForExistence(timeout: 10), "no name alert after the list closed:\n\(app.debugDescription)")
        let field = alert.textFields.firstMatch
        field.tap()
        field.typeText("书房")
        shot(app, "switcher-new-alert")
        alert.buttons["创建"].tap()
        XCTAssertTrue(element(app, "managerCode.countdown").waitForExistence(timeout: 10), "no install code:\n\(app.debugDescription)")
        shot(app, "switcher-new-code")
    }

    @MainActor func testManageAssistantsOpensTheirSettingsPage() throws {
        let app = openList()
        element(app, "assistant-switcher-manage").tap()
        XCTAssertTrue(element(app, "managers.row.m-3fa21c").waitForExistence(timeout: 10), "Settings › Assistants did not open:\n\(app.debugDescription)")
        shot(app, "switcher-manage")
    }

    @MainActor func testARowStillInstallingShowsItsCode() throws {
        let app = openList()
        element(app, "assistant-switcher-row-m-c1e9f0").tap()
        XCTAssertTrue(element(app, "managerCode.countdown").waitForExistence(timeout: 10), "no install code:\n\(app.debugDescription)")
        shot(app, "switcher-installing-code")
    }

    // MARK: -

    /// The Persona page's sidebar out, on the default assistant, with the switcher's list open.
    @MainActor private func openList() -> XCUIApplication {
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "assistant", "-app.tab", "assistant", "-openAssistantSidebar", "YES",
                               "-openAssistantManager", "assistant"]
        app.launchPinned()
        // A fresh install asks for notifications first, and the app waits inactive (no roster) until it is answered.
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        for label in ["允许", "Allow"] where springboard.buttons[label].waitForExistence(timeout: 2) { springboard.buttons[label].tap() }
        // The drawer's identifier can cover its header's on a phone (`drawerSettingsButton`): the default's name then.
        let header = app.buttons.matching(NSPredicate(format: "identifier == %@ OR (identifier == %@ AND label BEGINSWITH %@)",
                                                      "assistant-switcher", "assistant-sidebar", "助手")).firstMatch
        XCTAssertTrue(header.waitForExistence(timeout: 20), "no switcher in the sidebar's header:\n\(app.debugDescription)")
        header.tap()
        XCTAssertTrue(element(app, "assistant-switcher-new").waitForExistence(timeout: 10), "the list did not open:\n\(app.debugDescription)")
        return app
    }

    private func element(_ app: XCUIApplication, _ id: String) -> XCUIElement {
        app.descendants(matching: .any).matching(identifier: id).firstMatch
    }

    private func shot(_ app: XCUIApplication, _ name: String) {
        let png = app.screenshot()
        let a = XCTAttachment(screenshot: png)
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        guard let dir = env["PERSONA_SHOTS"] else { return }
        let look = env["PERSONA_SHOTS_APPEARANCE"] ?? "light"
        try? png.pngRepresentation.write(to: URL(fileURLWithPath: dir).appending(path: "\(name)-\(look).png"))
    }
}
