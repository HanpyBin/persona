import AppIntents
import Foundation

// Recording on the phone without going through the app (docs/apple/recording.md「Meeting recordings」): Siri, Shortcuts, the
// Action button and the Control start a meeting recording; the recording Live Activity's buttons stop it and bookmark it.
// Compiled into the app and the widget extension (the Control and the activity draw them); every body runs in the app.

/// Starts a meeting recording, named after the calendar event happening now. An `AudioRecordingIntent`: it may open the
/// microphone without bringing the app to the front, and the recording shows a Live Activity for as long as it lasts
/// (the system stops a recording without one). Before the microphone has ever been allowed it opens the app to ask.
struct StartMeetingRecordingIntent: AudioRecordingIntent {
    nonisolated static let title: LocalizedStringResource = "Record a Meeting"
    nonisolated static let description = IntentDescription("Starts recording a meeting in Persona, named after the calendar event happening now. The audio stays on your iPhone; its transcript goes to Knowledge.")
    nonisolated static var supportedModes: IntentModes { [.background, .foreground(.dynamic)] }

    nonisolated init() {}

    nonisolated func perform() async throws -> some IntentResult {
        #if !WIDGET_EXTENSION
        if await MeetingRecorder.needsForeground { try await continueInForeground(alwaysConfirm: false) }
        await MeetingRecorder.shared.startMeeting(inForeground: MeetingRecorder.inForeground)
        #endif
        return .result()
    }
}

/// The recording activity's Stop: from the Lock Screen too, without unlocking (as a voice memo's).
struct StopRecordingIntent: LiveActivityIntent {
    nonisolated static let title: LocalizedStringResource = "Stop Recording"
    nonisolated static let isDiscoverable = false
    nonisolated static var authenticationPolicy: IntentAuthenticationPolicy { .alwaysAllowed }

    @Parameter(title: "Recording") var recordingID: String?

    nonisolated init() {}
    nonisolated init(recordingID: String?) { self.recordingID = recordingID }

    nonisolated func perform() async throws -> some IntentResult {
        #if !WIDGET_EXTENSION
        await MeetingRecorder.shared.stop(recordingID: recordingID)
        #endif
        return .result()
    }
}

/// The recording activity's Bookmark: marks this moment (the transcript and the minutes show where it was).
struct AddRecordingBookmarkIntent: LiveActivityIntent {
    nonisolated static let title: LocalizedStringResource = "Add a Bookmark"
    nonisolated static let isDiscoverable = false
    nonisolated static var authenticationPolicy: IntentAuthenticationPolicy { .alwaysAllowed }

    @Parameter(title: "Recording") var recordingID: String?

    nonisolated init() {}
    nonisolated init(recordingID: String?) { self.recordingID = recordingID }

    nonisolated func perform() async throws -> some IntentResult {
        #if !WIDGET_EXTENSION
        await MeetingRecorder.shared.addMark(recordingID: recordingID)
        #endif
        return .result()
    }
}
