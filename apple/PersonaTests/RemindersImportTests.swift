import Foundation
import Testing
@testable import Persona

/// Reminders imported as to-dos (#4009): how a reminder's due date is sent, and when a batch counts as unchanged.
struct RemindersImportTests {
    @Test func allDayReminderSendsTheDay() {
        let due = RemindersImport.due(DateComponents(year: 2026, month: 3, day: 7))
        #expect(due.date == "2026-03-07")
        #expect(due.at == nil)
    }

    @Test func timedReminderSendsTheMomentInUTC() {
        var c = DateComponents(year: 2026, month: 11, day: 11, hour: 9, minute: 30)
        c.timeZone = TimeZone(identifier: "Asia/Shanghai")
        let due = RemindersImport.due(c)
        #expect(due.at == "2026-11-11T01:30:00Z")
        #expect(due.date == nil)
    }

    @Test func noDueDateSendsNeither() {
        let due = RemindersImport.due(nil)
        #expect(due.at == nil && due.date == nil)
    }

    @Test func batchHashIgnoresOrderButNotContent() {
        let a = RemindersImport.Item(externalID: "a", title: "Milk")
        let b = RemindersImport.Item(externalID: "b", title: "Bank", dueDate: "2026-11-12")
        let one = RemindersImport.hash(RemindersImport.batch([a, b]))
        #expect(one == RemindersImport.hash(RemindersImport.batch([b, a])))
        var changed = b
        changed.title = "Call the bank"
        #expect(one != RemindersImport.hash(RemindersImport.batch([a, changed])))
    }

    @Test func batchKeepsAtMostWhatTheHubTakes() {
        let items = (0..<1200).map { RemindersImport.Item(externalID: String(format: "%04d", $0), title: "x") }
        #expect(RemindersImport.batch(items).items.count == 1000)
    }
}
