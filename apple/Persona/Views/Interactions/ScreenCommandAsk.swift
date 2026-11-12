import SwiftUI

/// What a screen card shows of a command's permission prompt read off the terminal, in plain words (#5266), under its
/// heading ("Claude Wants to Run a Command") and over its answers: the agent's own words for it, the command in code type
/// (a long one folded, Show All), the warning in plain words with Claude Code's own under Show Original, and the
/// countdown running. The default style's `ScreenForm` and Beautiful UI's `BUIScreenForm` both lay it out.
struct ScreenCommandAskBody: View {
    var ask: CommandAsk
    @State private var showsAll = false
    @State private var showsOriginal = false

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.m) {
            if let description = ask.description {
                Text(description)
                    .font(Theme.Font.body).lineHeight(.body)
                    .fixedSize(horizontal: false, vertical: true)
            }
            command
            if let risk = ask.riskLine { warning(risk) }
            if ask.deadlineAt != nil { ScreenCountdown(ask: ask) }
        }
    }

    /// The command as the permission card shows one (`PermissionForm`): code type in its box, selectable.
    private var command: some View {
        VStack(alignment: .leading, spacing: Theme.Space.xs) {
            Text(ask.command)
                .font(Theme.Font.code)
                .lineLimit(ask.isLong && !showsAll ? CommandAsk.foldedLines : nil)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(Theme.Space.m)
                .background(Theme.codeBackground, in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
                .accessibilityIdentifier("screen-command")
            if ask.isLong {
                Button {
                    withAnimation(Theme.Motion.standard) { showsAll.toggle() }
                } label: {
                    Text(showsAll ? String(localized: "Collapse") : String(localized: "Show All")).lineLimit(1)
                }
                .font(Theme.Font.captionMedium)
                .buttonStyle(.plain)
                .foregroundStyle(Theme.accent)
                .accessibilityIdentifier("screen-command-all")
            }
        }
    }

    /// The warning in plain words; Claude Code's own (and its countdown sentence) under Show Original.
    private func warning(_ risk: String) -> some View {
        VStack(alignment: .leading, spacing: Theme.Space.xs) {
            HStack(alignment: .firstTextBaseline, spacing: Theme.Space.s) {
                ThemedIcon(Theme.Icon.errorFilled).font(Theme.Font.captionMedium)
                Text(risk).font(Theme.Font.captionMedium).fixedSize(horizontal: false, vertical: true)
            }
            .foregroundStyle(Theme.cautionText)
            .accessibilityElement(children: .combine)
            if let original = ask.original {
                Button {
                    withAnimation(Theme.Motion.standard) { showsOriginal.toggle() }
                } label: {
                    Text(showsOriginal ? String(localized: "Hide Original") : String(localized: "Show Original")).lineLimit(1)
                }
                .font(Theme.Font.captionMedium)
                .buttonStyle(.plain)
                .foregroundStyle(Theme.accent)
                .accessibilityIdentifier("screen-warning-original")
                if showsOriginal {
                    Text(original)
                        .font(Theme.Font.caption)
                        .foregroundStyle(.secondary)
                        .textSelection(.enabled)
                        .fixedSize(horizontal: false, vertical: true)
                        .transition(.opacity)
                }
            }
        }
    }
}

/// The countdown Claude Code runs before it answers the prompt itself ("Denied automatically in 1:44"): the time drawn
/// by the system (`Text(timerInterval:)`, no timer of ours), and the words once it ran out, the view drawn again just
/// then (`.explicit`, one date).
struct ScreenCountdown: View {
    var ask: CommandAsk

    var body: some View {
        if let end = ask.deadlineAt {
            TimelineView(.explicit([end])) { context in
                HStack(alignment: .firstTextBaseline, spacing: Theme.Space.s) {
                    ThemedIcon(Theme.Icon.time)
                    if context.date < end {
                        let time = Text(timerInterval: context.date...end, countsDown: true)
                        if ask.deadlineAllows {
                            Text("Allowed automatically in \(time)")
                        } else {
                            Text("Denied automatically in \(time)")
                        }
                    } else {
                        Text(ask.expiredLine)
                    }
                }
                .font(Theme.Font.captionMedium.monospacedDigit())
                .foregroundStyle(.secondary)
                .accessibilityElement(children: .combine)
                .accessibilityIdentifier("screen-countdown")
            }
        }
    }
}
