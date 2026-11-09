import Foundation

/// The hub's minutes of a meeting recording (#2710, docs/apple/recording.md「Minutes」): what it was about, the key points,
/// decisions, action items and open questions, each pointing at when it was said, in seconds into the recording, so a tap
/// lands on that line of the transcript. Decoded from `GET /api/v1/kb/recordings/{id}`'s `detail.minutes`; every field
/// is optional, so a hub that adds one or leaves one out still shows the rest.
nonisolated struct RecordingMinutes: Codable, Hashable, Sendable {
    nonisolated struct Item: Codable, Hashable, Sendable {
        var text: String
        /// Seconds into the recording where it was said; nil when it isn't one moment.
        var at: Double?
        /// An action item's owner and due date, as said.
        var owner: String?
        var due: String?

        init(text: String, at: Double? = nil, owner: String? = nil, due: String? = nil) {
            self.text = text; self.at = at; self.owner = owner; self.due = due
        }

        enum CodingKeys: String, CodingKey { case text, at, refs, owner, due, task }

        init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            text = try c.decodeIfPresent(String.self, forKey: .text) ?? c.decodeIfPresent(String.self, forKey: .task) ?? ""
            // One moment (`at`), or the transcript times it cites (`refs`): the first is where a tap goes.
            at = (try? c.decodeIfPresent(Double.self, forKey: .at)) ?? (try? c.decodeIfPresent([Double].self, forKey: .refs))?.first
            owner = try c.decodeIfPresent(String.self, forKey: .owner)
            due = try c.decodeIfPresent(String.self, forKey: .due)
        }

        func encode(to encoder: Encoder) throws {
            var c = encoder.container(keyedBy: CodingKeys.self)
            try c.encode(text, forKey: .text)
            try c.encodeIfPresent(at, forKey: .at)
            try c.encodeIfPresent(owner, forKey: .owner)
            try c.encodeIfPresent(due, forKey: .due)
        }
    }

    var title: String?
    var topic: String?
    var attendees: [String] = []
    var keyPoints: [Item] = []
    var decisions: [Item] = []
    var actionItems: [Item] = []
    var openQuestions: [Item] = []
    /// The hub's own rendering (what the web shows); Copy / Share send it when there is one.
    var markdown: String?
    /// The recording revision they were written from.
    var revision: Int64?

    init(title: String? = nil, topic: String? = nil, attendees: [String] = [], keyPoints: [Item] = [], decisions: [Item] = [],
         actionItems: [Item] = [], openQuestions: [Item] = [], markdown: String? = nil, revision: Int64? = nil) {
        self.title = title; self.topic = topic; self.attendees = attendees; self.keyPoints = keyPoints; self.decisions = decisions
        self.actionItems = actionItems; self.openQuestions = openQuestions; self.markdown = markdown; self.revision = revision
    }

    enum CodingKeys: String, CodingKey {
        case title, topic, attendees, decisions, markdown, revision
        case keyPoints = "key_points", actionItems = "action_items", openQuestions = "open_questions"
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        title = try c.decodeIfPresent(String.self, forKey: .title)
        topic = try c.decodeIfPresent(String.self, forKey: .topic)
        attendees = (try? c.decodeIfPresent([String].self, forKey: .attendees)) ?? []
        keyPoints = try c.decodeIfPresent([Item].self, forKey: .keyPoints) ?? []
        decisions = try c.decodeIfPresent([Item].self, forKey: .decisions) ?? []
        actionItems = try c.decodeIfPresent([Item].self, forKey: .actionItems) ?? []
        openQuestions = try c.decodeIfPresent([Item].self, forKey: .openQuestions) ?? []
        markdown = try c.decodeIfPresent(String.self, forKey: .markdown)
        revision = try c.decodeIfPresent(Int64.self, forKey: .revision)
    }

    var isEmpty: Bool {
        (topic ?? "").isEmpty && keyPoints.isEmpty && decisions.isEmpty && actionItems.isEmpty && openQuestions.isEmpty
    }

    /// The transcript line said at `seconds`: the last one that started by then (a second's grace: a point is often
    /// stamped as its sentence begins), else the first.
    static func sentenceIndex(at seconds: Double, in sentences: [RecordingSentence]) -> Int? {
        guard !sentences.isEmpty, seconds.isFinite else { return nil }
        return sentences.lastIndex { $0.start <= seconds + 1 } ?? 0
    }

    /// What Copy and Share send: the hub's Markdown when it wrote one, else this, rendered the same way (headed by the
    /// recording's name and `subtitle`, the meeting's time and place).
    func text(title fallback: String, subtitle: String?) -> String {
        if let markdown, !markdown.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty { return markdown }
        var out = ["# " + (title?.isEmpty == false ? title! : fallback)]
        if let subtitle, !subtitle.isEmpty { out.append(subtitle) }
        if !attendees.isEmpty { out.append(String(localized: "Attendees: \(ListFormatter.localizedString(byJoining: attendees))")) }
        if let topic, !topic.isEmpty { out += ["", topic] }
        func section(_ heading: String, _ items: [Item]) {
            guard !items.isEmpty else { return }
            out += ["", "## " + heading]
            for item in items {
                var line = "- "
                if let owner = item.owner, !owner.isEmpty { line += owner + ": " }
                line += item.text
                if let due = item.due, !due.isEmpty { line += " · " + due }
                if let at = item.at { line += " (" + RecordingFormat.clock(at) + ")" }
                out.append(line)
            }
        }
        section(String(localized: "Key Points"), keyPoints)
        section(String(localized: "Decisions"), decisions)
        section(String(localized: "Action Items"), actionItems)
        section(String(localized: "Open Questions"), openQuestions)
        return out.joined(separator: "\n")
    }
}
