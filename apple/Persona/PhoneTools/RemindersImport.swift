import EventKit
import Foundation

/// Reminders imported as to-dos, one way (#4009, docs/assistant/todos.md "Imported from Reminders"): the incomplete
/// reminders of the lists the user picked go to the hub, which makes a to-do of each new one and follows changes of
/// their title, notes and due date. Nothing is written back. Opt-in (Settings › Phone Data), off by default.
///
/// It runs only while the app does: on launch and return to the foreground, and when the Reminders database changes
/// while the app is in front. A batch the hub already has (same hash, same hub) is not sent again.
nonisolated enum RemindersImport {
    /// Settings › Phone Data › Import into To-dos.
    static let enabledKey = "reminders.importTodos"
    /// The lists to import (`EKCalendar.calendarIdentifier`); none stored: every list. Written when the user picks.
    static let listsKey = "reminders.importLists"
    /// The hash of the last batch the hub took, by hub group (`StateCache`).
    static let sentCache = "reminders-import-sent"

    /// One reminder as `POST /api/v1/todos/import/reminders` takes it.
    struct Item: Encodable, Equatable, Sendable {
        var externalID: String
        var title: String
        var notes: String?
        var dueAt: String?
        var dueDate: String?

        enum CodingKeys: String, CodingKey {
            case externalID = "external_id", title, notes, dueAt = "due_at", dueDate = "due_date"
        }
    }

    struct Batch: Encodable, Sendable { var items: [Item] }

    /// A reminder's due date the way the hub keeps one: a moment (RFC 3339, UTC) when it has a time, else the day.
    static func due(_ components: DateComponents?, calendar: Calendar = .current) -> (at: String?, date: String?) {
        guard let c = components, let year = c.year, let month = c.month, let day = c.day else { return (nil, nil) }
        if c.hour == nil {
            return (nil, String(format: "%04d-%02d-%02d", year, month, day))
        }
        var cal = calendar
        if let zone = c.timeZone { cal.timeZone = zone }
        guard let date = cal.date(from: c) else { return (nil, nil) }
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime]
        return (f.string(from: date), nil)
    }

    /// The batch for these reminders: in a stable order (so its hash only changes when a reminder does), at most the
    /// hub's 1000.
    static func batch(_ items: [Item]) -> Batch {
        Batch(items: Array(items.sorted { $0.externalID < $1.externalID }.prefix(1000)))
    }

    /// A stable hash of a batch (FNV-1a over its JSON), to tell whether it changed since it was last sent.
    static func hash(_ batch: Batch) -> String {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        guard let data = try? encoder.encode(batch) else { return "" }
        var h: UInt64 = 0xcbf2_9ce4_8422_2325
        for byte in data {
            h ^= UInt64(byte)
            h = h &* 0x100_0000_01b3
        }
        return String(h, radix: 16)
    }

    static var isEnabled: Bool { UserDefaults.standard.bool(forKey: enabledKey) }

    /// The lists picked; nil: all of them.
    static var lists: Set<String>? {
        (UserDefaults.standard.array(forKey: listsKey) as? [String]).map(Set.init)
    }

    /// A Reminders list the user can pick.
    struct ReminderList: Identifiable, Sendable {
        var id: String
        var title: String
    }

    /// The Reminders lists, by name; read off the main thread (EventKit's calls are synchronous, #1337).
    static func availableLists() async -> [ReminderList] {
        await Task.detached {
            EKEventStore().calendars(for: .reminder)
                .map { ReminderList(id: $0.calendarIdentifier, title: $0.title) }
                .sorted { $0.title.localizedStandardCompare($1.title) == .orderedAscending }
        }.value
    }

    /// The incomplete reminders of the picked lists, as items. EventKit calls back on its own queue, so the block is
    /// `@Sendable` and turns its objects into values there.
    static func read(lists picked: Set<String>?) async -> [Item] {
        let store = EKEventStore()
        let all = store.calendars(for: .reminder)
        let chosen = picked.map { ids in all.filter { ids.contains($0.calendarIdentifier) } } ?? all
        if chosen.isEmpty { return [] }
        let predicate = store.predicateForIncompleteReminders(withDueDateStarting: nil, ending: nil, calendars: chosen)
        return await withCheckedContinuation { (continuation: CheckedContinuation<[Item], Never>) in
            store.fetchReminders(matching: predicate) { @Sendable reminders in
                let items = (reminders ?? []).compactMap { r -> Item? in
                    guard let id = r.calendarItemExternalIdentifier, !id.isEmpty else { return nil }
                    let (at, day) = due(r.dueDateComponents)
                    return Item(externalID: id, title: r.title ?? "", notes: r.notes, dueAt: at, dueDate: day)
                }
                continuation.resume(returning: items)
            }
        }
    }
}

#if os(iOS)
/// Sends the import when the app comes to the front (`PersonaApp`'s scene phase), when the Reminders database changes
/// while it is there, and when the user turns it on or picks lists.
@MainActor final class RemindersImporter {
    static let shared = RemindersImporter()
    private var running: Task<Void, Never>?
    private var again = false
    private weak var app: AppModel?
    /// Kept while the import is on: EventKit posts `EKEventStoreChanged` only to a store that is alive.
    private var store: EKEventStore?
    private var observer: NSObjectProtocol?
    private var sent: [String: String] = StateCache.load(RemindersImport.sentCache, as: [String: String].self) ?? [:]

    private init() {}

    /// Back in front (or turned on): follows changes of the Reminders database while the import is on, and imports now.
    func activate(_ app: AppModel, force: Bool = false) {
        self.app = app
        if RemindersImport.isEnabled, observer == nil {
            let store = EKEventStore()
            self.store = store
            observer = NotificationCenter.default.addObserver(forName: .EKEventStoreChanged, object: store, queue: .main) { @Sendable _ in
                Task { @MainActor in RemindersImporter.shared.changed() }
            }
        } else if !RemindersImport.isEnabled, let observer {
            NotificationCenter.default.removeObserver(observer)
            self.observer = nil
            store = nil
        }
        sync(app, force: force)
    }

    private func changed() {
        if let app { sync(app) }
    }

    /// Imports now; `force` sends even a batch the hub already took (the user changed the lists or turned it on).
    func sync(_ app: AppModel, force: Bool = false) {
        if force { sent.removeAll() }
        if running != nil {
            again = true // the change lands after the batch on its way: one more
            return
        }
        running = Task {
            defer {
                running = nil
                if again {
                    again = false
                    sync(app)
                }
            }
            await importOnce(app)
        }
    }

    private func importOnce(_ app: AppModel) async {
        guard RemindersImport.isEnabled, PhoneDataSource.reminders.isActive,
              let link = app.hub, !link.isDemo, let api = app.api else { return }
        let batch = RemindersImport.batch(await RemindersImport.read(lists: RemindersImport.lists))
        let hash = RemindersImport.hash(batch)
        guard sent[link.group] != hash else { return }
        do {
            _ = try await api.post("/api/v1/todos/import/reminders", body: batch)
            sent[link.group] = hash
            StateCache.save(RemindersImport.sentCache, sent)
        } catch {
            // An older hub (404) or no connection: the next foreground or change tries again.
        }
    }
}
#endif
