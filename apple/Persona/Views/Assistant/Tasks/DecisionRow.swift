import SwiftUI

/// Answering a decision on its row (#3804, docs/assistant/todos.md "Decisions the owner can answer"): which option of
/// a round is armed, which answer is on its way, and what became of it. Kept here by round, not in the row, so the same
/// decision drawn in the conversation's card, the Needs You tray and the task board says the same thing, and a row
/// drawn again (scrolled away and back) still says what happened. Every answer goes through
/// `POST /api/v1/todos/{id}/accept`, as the recommendation's Take always did.
@MainActor @Observable final class DecisionAnswers {
    /// Where answering a round stands.
    enum State: Equatable, Sendable {
        /// Its request is out: `answer` names the button that shows it ("option:1", "response:explain_first").
        case busy(answer: String)
        /// The hub took it; the row says this instead of its buttons.
        case answered(String)
        /// 409: that round was over (asked again since, or answered elsewhere).
        case alreadyHandled
    }

    static let shared = DecisionAnswers()

    private(set) var states: [String: State] = [:]
    /// The option a first tap armed, for a decision that takes two (`TodoAsk.needsConfirmation`).
    private(set) var armed: [String: Int] = [:]
    /// The hub of the conversation the row is in.
    @ObservationIgnored private let api: (String) -> APIClient?
    /// Says why an answer did not go; the buttons work again.
    @ObservationIgnored private let failed: (String) -> Void
    /// After an answer was taken (or found already handled): the to-do's state changed on the hub.
    @ObservationIgnored private let taken: () -> Void

    init(api: @escaping (String) -> APIClient? = { AppModel.shared.api($0) },
         failed: @escaping (String) -> Void = { AppModel.shared.toast = Toast($0, style: .error) },
         taken: @escaping () -> Void = { TasksStore.shared.reloadTodos() }) {
        self.api = api
        self.failed = failed
        self.taken = taken
    }

    /// A round of a to-do on a hub: the same round on two surfaces is one answer.
    static func key(todo: String, ask: TodoAsk, group: String) -> String { "\(group)|\(todo)|\(ask.askedAt)" }

    func state(todo: String, ask: TodoAsk, group: String) -> State? { states[Self.key(todo: todo, ask: ask, group: group)] }
    func armedOption(todo: String, ask: TodoAsk, group: String) -> Int? { armed[Self.key(todo: todo, ask: ask, group: group)] }

    /// A tap on option `index`. A step that cannot be undone, or that other people will see, takes two: the first
    /// arms the option ("Confirm: …"), the second on the same one sends it. Any other decision sends at once.
    func choose(_ index: Int, of ask: TodoAsk, todo: String, group: String) async {
        let options = ask.decisionOptions
        guard options.indices.contains(index) else { return }
        let key = Self.key(todo: todo, ask: ask, group: group)
        guard states[key] == nil else { return }
        if ask.needsConfirmation, armed[key] != index {
            armed[key] = index
            return
        }
        let label = options[index].label
        await send(key, group: group, answer: "option:\(index)", done: String(localized: "Answered: \(label)")) {
            try await $0.acceptTodo(todo, askedAt: ask.askedAt, option: label)
        }
    }

    /// One of the fixed answers ("You Decide", "This Isn't What I Asked For", "Tell Me the Background First"): never armed,
    /// nothing is taken, the task's supervisor hears it.
    func respond(_ response: TodoAsk.Response, to ask: TodoAsk, todo: String, group: String) async {
        let key = Self.key(todo: todo, ask: ask, group: group)
        guard states[key] == nil else { return }
        await send(key, group: group, answer: "response:\(response.rawValue)", done: String(localized: "Sent back to Persona")) {
            try await $0.acceptTodo(todo, askedAt: ask.askedAt, response: response.rawValue)
        }
    }

    private func send(_ key: String, group: String, answer: String, done: String, _ call: (APIClient) async throws -> Void) async {
        guard let api = api(group) else {
            let why = String(localized: "Not connected to a hub [Todo Store 04f822]")
            failed(String(localized: "Couldn't send your answer. \(why)"))
            return
        }
        states[key] = .busy(answer: answer)
        do {
            try await call(api)
            states[key] = .answered(done)
            armed[key] = nil
            taken()
        } catch let e as APIError where e.status == 409 {
            states[key] = .alreadyHandled
            armed[key] = nil
            taken()
        } catch {
            // A 502 (the choice did not reach the task's agent), a 503 or no answer: nothing was taken. The option
            // stays armed, so the next tap sends it again.
            states[key] = nil
            failed(String(localized: "Couldn't send your answer. \(error.localizedDescription)"))
        }
    }
}

/// A to-do's open ask where the app lists what needs the owner (the conversation's task card, the Needs You tray, the
/// task board): a decision with options is answered on its row (`DecisionRow`); any other ask keeps its one button
/// (`BriefAcceptRow`), as does a decision from a hub that sends no options.
struct TodoAskRow: View {
    var todo: TodoItem
    var ask: TodoAsk
    var group: String
    var open: (() -> Void)? = nil

    var body: some View {
        #if os(macOS)
        // The native Mac app is frozen (docs/apple/mac.md): it keeps the one button it had.
        BriefAcceptRow(row: ask.row(for: todo), group: group, open: open)
        #else
        if ask.decisionOptions.isEmpty {
            BriefAcceptRow(row: ask.row(for: todo), group: group, open: open)
        } else {
            DecisionRow(todo: todo, ask: ask, group: group, open: open)
        }
        #endif
    }
}

/// A decision that can be answered on its row (#3804; the web's views/assistant/DecisionRow.tsx): what is to be decided,
/// where the task came from and where it stands, what each option leads to (the recommended one first and marked),
/// what happens when nobody answers, then the options as buttons (the recommended one the only solid one), as many as
/// fit, and "Other…", which opens the task to answer in words. The options that do not fit and the answers that are no
/// option ("You Decide", "This Isn't What I Asked For", "Tell Me the Background First") are in its ⋯ menu.
struct DecisionRow: View {
    var todo: TodoItem
    var ask: TodoAsk
    var group: String
    var open: (() -> Void)? = nil
    var answers: DecisionAnswers = .shared
    @Environment(\.dynamicTypeSize) private var typeSize

    private var options: [TodoAsk.Option] { ask.decisionOptions }
    private var state: DecisionAnswers.State? { answers.state(todo: todo.id, ask: ask, group: group) }
    private var armed: Int? { answers.armedOption(todo: todo.id, ask: ask, group: group) }
    private var busy: Bool { if case .busy = state { true } else { false } }

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.xs) {
            Text(todo.title)
                .font(Theme.Font.caption)
                .foregroundStyle(.secondary)
                .lineLimit(1)
            Text(ask.questionLine)
                .font(Theme.Font.secondaryMedium)
                .fixedSize(horizontal: false, vertical: true)
                .textSelection(.enabled)
                .accessibilityIdentifier("decision.question")
            if let background = ask.background, !background.isEmpty {
                Text(background)
                    .font(Theme.Font.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
                    .accessibilityIdentifier("decision.background")
            }
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                ForEach(Array(options.enumerated()), id: \.offset) { index, option in
                    leads(option, recommended: index == 0)
                }
            }
            .padding(.top, Theme.Space.xxs)
            if let waits = waitingLine {
                Text(waits)
                    .font(Theme.Font.caption)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
                    .accessibilityIdentifier("decision.default")
            }
            footer.padding(.top, Theme.Space.xs)
        }
        .padding(.vertical, Theme.Space.xs)
        .accessibilityElement(children: .contain)
    }

    /// An option and what choosing it leads to, the recommended one marked.
    private func leads(_ option: TodoAsk.Option, recommended: Bool) -> some View {
        VStack(alignment: .leading, spacing: Theme.Space.flush) {
            HStack(alignment: .firstTextBaseline, spacing: Theme.Space.sm) {
                Text(option.label).font(Theme.Font.captionStrong).lineLimit(2)
                if recommended { RecommendedTag() }
            }
            if let leadsTo = option.leadsTo, !leadsTo.isEmpty {
                Text(leadsTo).font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(2)
            }
        }
        .accessibilityElement(children: .combine)
    }

    /// "Taken by default at 17:30 · If it waits: …": what happens when nobody answers, and what waiting costs.
    private var waitingLine: String? {
        let fallback = ask.defaultLine()
        let cost = ask.costOfWaiting.flatMap { $0.isEmpty ? nil : String(localized: "If it waits: \($0)") }
        switch (fallback, cost) {
        case (let fallback?, let cost?): return String(localized: "\(fallback) · \(cost)")
        case (let line?, nil), (nil, let line?): return line
        case (nil, nil): return nil
        }
    }

    // MARK: Answers

    @ViewBuilder private var footer: some View {
        switch state {
        case .answered(let text)?:
            outcome(text)
        case .alreadyHandled?:
            outcome(String(localized: "Already handled"))
        case .busy?, nil:
            VStack(alignment: .leading, spacing: Theme.Space.xs) {
                // "Tell Me the Background First" was given: the decision still waits, and can still be answered.
                if ask.response != nil {
                    Text("Sent back to Persona").font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1)
                }
                if typeSize.isAccessibilitySize {
                    // At the accessibility sizes the whole group is one column, the recommended option first, each
                    // label still on one line (DESIGN_SYSTEM.md 1.2 rule 3).
                    VStack(alignment: .leading, spacing: Theme.Space.s) { controls(shown: options.count) }
                } else {
                    ViewThatFits(in: .horizontal) {
                        ForEach(Array(stride(from: options.count, to: 1, by: -1)), id: \.self) { shown in
                            HStack(spacing: Theme.Space.s) { controls(shown: shown) }
                                .fixedSize(horizontal: true, vertical: false)
                        }
                        // Alone and still wider than the row, the button gives way and cuts its label; it never wraps.
                        HStack(spacing: Theme.Space.s) { controls(shown: 1) }
                    }
                }
            }
        }
    }

    /// What the answer came to, in the buttons' place.
    private func outcome(_ text: String) -> some View {
        Text(text)
            .font(Theme.Font.captionMedium)
            .foregroundStyle(.secondary)
            .lineLimit(1)
            .accessibilityIdentifier("decision.outcome.\(todo.id)")
    }

    /// The first `shown` options as buttons, "Other…", and the menu with the rest.
    @ViewBuilder private func controls(shown: Int) -> some View {
        ForEach(Array(options.prefix(shown).enumerated()), id: \.offset) { index, option in
            optionButton(option, index: index)
        }
        Button {
            if let open { open() } else { TasksStore.shared.detailTask = todo.id }
        } label: {
            Text("Other…").lineLimit(1)
        }
        .font(Theme.Font.captionStrong)
        .themedButton()
        .disabled(busy)
        .accessibilityLabel(String(localized: "Open \(todo.title) to answer another way"))
        .accessibilityIdentifier("decision.other.\(todo.id)")
        Menu {
            menuItems(from: shown)
        } label: {
            // An icon-only themed button takes its glyph's size (docs/lessons/ios.md): a line of the buttons' own
            // text gives it their height, and the icon token its width.
            Text(verbatim: " ").lineLimit(1)
                .scaledFrame(width: Theme.Size.icon)
                .overlay { ThemedIcon(Theme.Icon.more) }
        }
        .font(Theme.Font.captionStrong)
        .themedButton()
        .disabled(busy)
        .accessibilityLabel("More answers")
        .accessibilityIdentifier("decision.more.\(todo.id)")
    }

    private func label(_ option: TodoAsk.Option, index: Int) -> String {
        armed == index ? String(localized: "Confirm: \(option.label)") : option.label
    }

    private func optionButton(_ option: TodoAsk.Option, index: Int) -> some View {
        let sending = state == .busy(answer: "option:\(index)")
        return Button {
            Task { await answers.choose(index, of: ask, todo: todo.id, group: group) }
        } label: {
            Text(label(option, index: index)).lineLimit(1).opacity(sending ? 0 : 1)
                .overlay { if sending { ProgressView().controlSize(.small) } }
        }
        .font(Theme.Font.captionStrong)
        .themedButton(prominent: index == 0)
        .disabled(busy)
        .accessibilityLabel(index == 0 ? String(localized: "Choose \(option.label), the recommended option, for \(todo.title)")
                                       : String(localized: "Choose \(option.label) for \(todo.title)"))
        .accessibilityValue(armed == index ? String(localized: "Tap again to confirm") : "")
        .accessibilityIdentifier(armed == index ? "decision.option.\(index).armed" : "decision.option.\(index)")
    }

    /// The options that have no button, each with what it leads to; the fixed answers, each with what it does; and
    /// Complete Directly (#3038), which ends the task without sending anything to its agent.
    @ViewBuilder private func menuItems(from shown: Int) -> some View {
        ForEach(Array(options.enumerated()).filter { $0.offset >= shown }, id: \.offset) { index, option in
            Button {
                Task { await answers.choose(index, of: ask, todo: todo.id, group: group) }
            } label: {
                Text(label(option, index: index))
                if let leadsTo = option.leadsTo, !leadsTo.isEmpty { Text(leadsTo) }
            }
        }
        let responses = ask.fixedResponses
        if !responses.isEmpty {
            Section {
                ForEach(responses, id: \.self) { response in
                    Button {
                        Task { await answers.respond(response, to: ask, todo: todo.id, group: group) }
                    } label: {
                        Text(Self.title(response))
                        Text(Self.detail(response))
                    }
                }
            }
        }
        Section {
            Button {
                Task { await BriefActions.shared.completeDirectly(ask.row(for: todo), group: group) }
            } label: {
                Text("Complete Directly")
            }
        }
    }

    static func title(_ response: TodoAsk.Response) -> String {
        switch response {
        case .decideYourself: String(localized: "You Decide")
        case .notWhatIAsked: String(localized: "This Isn't What I Asked For")
        case .explainFirst: String(localized: "Tell Me the Background First")
        }
    }

    static func detail(_ response: TodoAsk.Response) -> String {
        switch response {
        case .decideYourself: String(localized: "Persona decides and records what it assumed.")
        case .notWhatIAsked: String(localized: "The work pauses until Persona checks it against your request.")
        case .explainFirst: String(localized: "Persona asks again with what this is and why.")
        }
    }
}
