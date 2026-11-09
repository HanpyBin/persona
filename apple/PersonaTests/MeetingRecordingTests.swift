import AppIntents
import Foundation
import Testing
@testable import Persona

/// One-tap meeting recordings (docs/apple/recording.md「Meeting recordings」): which calendar event names one, what its
/// Live Activity shows, its intents' rules, and what a meeting adds to a recording.
@MainActor struct MeetingRecordingTests {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)

    private func event(_ title: String, from start: Double, to end: Double, allDay: Bool = false, declined: Bool = false) -> MeetingCalendar.Candidate {
        MeetingCalendar.Candidate(id: title, title: title, start: now.addingTimeInterval(start * 60), end: now.addingTimeInterval(end * 60),
                                  allDay: allDay, declined: declined)
    }

    // MARK: The calendar's meeting

    @Test func picksTheMeetingOnNow() {
        let events = [event("Earlier", from: -120, to: -60), event("Weekly", from: -10, to: 50), event("Later", from: 30, to: 60)]
        #expect(MeetingCalendar.pick(events, at: now)?.title == "Weekly")
    }

    /// All day, declined or untitled: not a meeting in the room.
    @Test func skipsAllDayDeclinedAndUntitled() {
        let events = [event("Holiday", from: -600, to: 600, allDay: true), event("Declined", from: -5, to: 25, declined: true),
                      event("  ", from: -5, to: 25)]
        #expect(MeetingCalendar.pick(events, at: now) == nil)
    }

    /// Back to back: the one that just began, not the one running over.
    @Test func prefersTheOneThatStartedLast() {
        let events = [event("Overrunning", from: -60, to: 5), event("Next", from: -1, to: 30)]
        #expect(MeetingCalendar.pick(events, at: now)?.title == "Next")
    }

    /// People sit down a few minutes early; not ten.
    @Test func countsAMeetingAboutToStart() {
        #expect(MeetingCalendar.pick([event("Soon", from: 4, to: 34)], at: now)?.title == "Soon")
        #expect(MeetingCalendar.pick([event("Too soon", from: 10, to: 40)], at: now) == nil)
        #expect(MeetingCalendar.pick([event("Just ended", from: -30, to: 0)], at: now) == nil)
    }

    @Test func namesOrganizerFirstEachOnce() {
        let names = MeetingCalendar.names(organizer: " Alex ", attendees: ["Sam", "Alex", nil, "", "Kim"])
        #expect(names == ["Alex", "Sam", "Kim"])
        #expect(MeetingCalendar.names(organizer: nil, attendees: (0..<80).map { "P\($0)" }).count == 50)
    }

    @Test func meetingCopiesTheEvent() {
        var e = event("Weekly", from: -10, to: 50)
        e.location = "Room 3"
        e.attendees = ["Alex"]
        let meeting = RecordingMeeting(e)
        #expect(meeting.eventID == "Weekly")
        #expect(meeting.title == "Weekly")
        #expect(meeting.location == "Room 3")
        #expect(meeting.attendees == ["Alex"])
        #expect(meeting.end == now.addingTimeInterval(50 * 60))
    }

    // MARK: The recording

    @Test func olderRecordingsDecodeWithoutAMeeting() throws {
        let entry = RecordingEntry(id: UUID(), createdAt: now, source: .phone)
        var json = try JSONSerialization.jsonObject(with: JSONEncoder().encode(entry)) as! [String: Any]
        json.removeValue(forKey: "meeting")
        let decoded = try JSONDecoder().decode(RecordingEntry.self, from: JSONSerialization.data(withJSONObject: json))
        #expect(decoded.meeting == nil)
        #expect(!decoded.isMeeting)
    }

    @Test func aMeetingIsKeptAndNamed() throws {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: root) }
        let lib = RecordingLibrary(root: root)
        let untitled = try lib.create(source: .phone, meeting: RecordingMeeting())
        #expect(untitled.isMeeting)
        #expect(untitled.displayTitle == String(localized: "Meeting · \(untitled.createdAt.formatted(date: .abbreviated, time: .shortened))"))
        let named = try lib.create(source: .phone, title: "Weekly", meeting: RecordingMeeting(eventID: "e1", title: "Weekly", attendees: ["Alex"]))
        let reread = RecordingLibrary(root: root).entry(named.id)
        #expect(reread?.title == "Weekly")
        #expect(reread?.meeting?.eventID == "e1")
        #expect(reread?.meeting?.attendees == ["Alex"])
        let note = try lib.create(source: .phone)
        #expect(!note.isMeeting)
    }

    /// The hub writes minutes for `kind: meeting`; the calendar's event goes with it when there was one (#2710).
    @Test func uploadMarksAMeeting() throws {
        func json(_ e: RecordingEntry) throws -> [String: Any] {
            try JSONSerialization.jsonObject(with: JSON.encoder.encode(RecordingUpload(e))) as! [String: Any]
        }
        var e = RecordingEntry(id: UUID(), createdAt: now, source: .phone, title: "Weekly")
        let note = try json(e)
        #expect(note["kind"] == nil)
        #expect(note["calendar"] == nil)
        e.meeting = RecordingMeeting()
        let bare = try json(e)
        #expect(bare["kind"] as? String == "meeting")
        #expect(bare["calendar"] == nil)
        e.meeting = RecordingMeeting(eventID: "e1", title: "Weekly", start: now, end: now.addingTimeInterval(3600), location: "Room 3",
                                     attendees: ["Alex", "Sam"])
        let calendar = try #require(try json(e)["calendar"] as? [String: Any])
        #expect(calendar["id"] as? String == "e1")
        #expect(calendar["title"] as? String == "Weekly")
        #expect(calendar["attendees"] as? [String] == ["Alex", "Sam"])
        #expect((calendar["start"] as? String)?.hasPrefix("2027-01-15T") == true)
    }

    // MARK: The Live Activity

    private func entry(marks: [Double] = [], meeting: Bool = true) -> RecordingEntry {
        var e = RecordingEntry(id: UUID(), createdAt: now, source: .phone, title: "Weekly")
        e.marks = marks
        if meeting { e.meeting = RecordingMeeting(title: "Weekly") }
        return e
    }

    /// Recording: the system counts from `now − elapsed`; later saves of the same running recording change nothing.
    @Test func runningTimeIsCountedBySystem() {
        let first = RecordingActivity.content(entry: entry(), recording: true, interrupted: false, elapsed: 100, previous: nil, now: now)
        #expect(first.phase == .recording)
        #expect(first.runningSince == now.timeIntervalSince1970 - 100)
        let later = RecordingActivity.content(entry: entry(), recording: true, interrupted: false, elapsed: 160, previous: first,
                                              now: now.addingTimeInterval(60.3))
        #expect(later == first)
    }

    @Test func pausedAndInterruptedStandStill() {
        let running = RecordingActivity.content(entry: entry(), recording: true, interrupted: false, elapsed: 100, previous: nil, now: now)
        let paused = RecordingActivity.content(entry: entry(), recording: false, interrupted: false, elapsed: 130, previous: running, now: now)
        #expect(paused.phase == .paused)
        #expect(paused.runningSince == nil)
        #expect(paused.elapsed == 130)
        let call = RecordingActivity.content(entry: entry(), recording: false, interrupted: true, elapsed: 130, previous: running, now: now)
        #expect(call.phase == .interrupted)
        // Resumed: counted again from the time recorded, not from before the pause.
        let resumed = RecordingActivity.content(entry: entry(), recording: true, interrupted: false, elapsed: 130, previous: call,
                                                now: now.addingTimeInterval(600))
        #expect(resumed.runningSince == now.addingTimeInterval(600).timeIntervalSince1970 - 130)
    }

    @Test func showsTitleMeetingAndBookmarks() {
        let content = RecordingActivity.content(entry: entry(marks: [10, 70]), recording: true, interrupted: false, elapsed: 80,
                                                previous: nil, now: now)
        #expect(content.title == "Weekly")
        #expect(content.meeting)
        #expect(content.marks == 2)
        let note = RecordingActivity.content(entry: entry(meeting: false), recording: true, interrupted: false, elapsed: 0, previous: nil, now: now)
        #expect(!note.meeting)
    }

    /// Small enough for ActivityKit's 4 KB, with a long title.
    @Test func contentStaysSmall() throws {
        var e = entry(marks: Array(repeating: 1, count: 500))
        e.title = String(repeating: "长", count: 200)
        let content = RecordingActivity.content(entry: e, recording: true, interrupted: false, elapsed: 7200, previous: nil, now: now)
        #expect(try JSONEncoder().encode(content).count < 1024)
    }

    @Test func linkOpensRecordings() {
        #expect(RecordingLink.matches(RecordingLink.page))
        #expect(!RecordingLink.matches(URL(string: "persona://open")!))
        #expect(!RecordingLink.matches(URL(string: "https://example.com/record")!))
    }

    // MARK: Intents

    /// The activity's buttons work locked (as a voice memo's) and stay out of Shortcuts.
    @Test func activityButtonsWorkLockedOutOfShortcuts() {
        #expect(StopRecordingIntent.authenticationPolicy == .alwaysAllowed)
        #expect(AddRecordingBookmarkIntent.authenticationPolicy == .alwaysAllowed)
        #expect(StopRecordingIntent.isDiscoverable == false)
        #expect(AddRecordingBookmarkIntent.isDiscoverable == false)
    }

    /// Siri and the Control start it without opening the app; the first time it may come to the front to ask for the microphone.
    @Test func startRunsInTheBackground() {
        #expect(StartMeetingRecordingIntent.supportedModes.contains(.background))
    }

    /// A button restored without its recording (or for one that ended) does nothing and doesn't throw.
    @Test func buttonsWithoutARecordingDoNothing() async throws {
        #expect(RecordingCapture.shared.state == .idle)
        _ = try await StopRecordingIntent().perform()
        _ = try await AddRecordingBookmarkIntent(recordingID: UUID().uuidString).perform()
        #expect(RecordingCapture.shared.state == .idle)
    }
}
