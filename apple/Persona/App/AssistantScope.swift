import Foundation

// What the pages list once a hub has several assistants (docs/assistant/managers.md "Clients"): the current one's
// computers, agents, cards, inbox items and to-dos, with no manager headings. The hub's lists and pushes stay whole, each
// thing carrying its `manager`, so a notification or a link still opens another assistant's item as itself; the pages
// filter what they list (the web's `current-assistant.ts`, Android's `CurrentAssistant`). The same file counts what
// waits at each assistant, for the switcher's badges and the mark on the sidebar's head.

/// The current assistant of the hub-level group, as a page asks whose a thing is. There is none (nil) where nothing is
/// filtered: one assistant, or a hub without a roster.
nonisolated struct AssistantScope: Equatable, Sendable {
    /// The group whose hub these assistants are of. Another group's things are no assistant's of this one: they are on
    /// every assistant's pages, as before.
    var group: String
    /// The current assistant's id.
    var current: String
    /// Whose a thing without `manager` is: the roster's default.
    var defaultID: String
    /// The installed managers' ids: a thing stamped with any other (deleted, still installing) is the default's. nil
    /// before the roster is read on a launch that kept a pick: every stamp is then its own.
    var installed: Set<String>?

    init(group: String, current: String, defaultID: String, installed: Set<String>?) {
        (self.group, self.current, self.defaultID, self.installed) = (group, current, defaultID, installed)
    }

    /// `pick`: what this device kept for the group (`AppModel.assistantPicks`). Until `roster` is read after a launch
    /// the pick already counts, so the pages don't list every assistant's things for a moment; `awaited` false: the hub
    /// answered that it has no roster, and the pick names nothing there.
    init?(roster: ManagerRoster?, pick: String?, group: String, awaited: Bool = true) {
        if let roster {
            guard roster.isMulti else { return nil }
            self.init(group: group, current: roster.owner(pick), defaultID: roster.defaultID, installed: Set(roster.installed.map(\.id)))
        } else {
            guard awaited, let pick, !pick.isEmpty else { return nil }
            self.init(group: group, current: pick, defaultID: ManagerRoster.assistant, installed: nil)
        }
    }

    /// The installed manager a thing stamped `manager` is listed under (`ManagerRoster.owner`).
    func owner(_ manager: String?) -> String {
        guard let manager, !manager.isEmpty, installed?.contains(manager) != false else { return defaultID }
        return manager
    }

    /// Whether a thing of this hub stamped `manager` (an inbox item, a to-do) is the current assistant's.
    func has(_ manager: String?) -> Bool { owner(manager) == current }

    /// The same for a thing that says which group it is of (a computer, a session, a card).
    func has(_ manager: String?, group: String) -> Bool { group != self.group || has(manager) }
}

nonisolated extension Optional where Wrapped == AssistantScope {
    /// Whether a page lists a thing stamped `manager`: everything, where nothing is filtered.
    func has(_ manager: String?) -> Bool { self?.has(manager) ?? true }
    func has(_ manager: String?, group: String) -> Bool { self?.has(manager, group: group) ?? true }
}

/// What waits for the user at each assistant (docs/assistant/managers.md "Counts", the same rule on every client).
nonisolated enum AssistantBadges {
    /// The agents waiting, by manager id: a session of the manager that is not a quiet task's and not muted, with a
    /// pending card or in Needs You, once however many cards it has; a pending card on no session by itself.
    /// - `sessions`: the hub's sessions the app knows to be in Needs You or not (`AppModel.countBadges`). Persona's own
    ///   conversations count by their cards only, as the Agents list leaves them out.
    /// - `counts`: whether an agent counts at all (not quiet, not muted), by its key and its row when `sessions` has it.
    static func waiting(roster: ManagerRoster, group: String, sessions: [Session], interactions: some Sequence<Interaction>,
                        counts: (SessionKey, Session?) -> Bool) -> [String: Int] {
        var bySession: [SessionKey: Session] = [:]
        for s in sessions where s.group == group && bySession[s.key] == nil { bySession[s.key] = s }
        var agents: [String: Set<SessionKey>] = [:]
        var tally: [String: Int] = [:]
        for ix in interactions where ix.group == group {
            if ix.isSessionless {
                tally[roster.owner(ix.manager), default: 0] += 1
                continue
            }
            let key = ix.sessionKey
            let session = bySession[key]
            guard counts(key, session) else { continue }
            agents[roster.owner(ix.manager ?? session?.manager), default: []].insert(key)
        }
        for s in bySession.values where !s.assistant && s.state == .needsYou && counts(s.key, s) {
            agents[roster.owner(s.manager), default: []].insert(s.key)
        }
        for (manager, keys) in agents { tally[manager, default: 0] += keys.count }
        return tally
    }

    /// Each installed assistant's count: its agents waiting plus its inbox's Needs You as the roster told it
    /// (`inbox_needs_you`; none told counts as none). Only the ones with something waiting are in it.
    static func counts(roster: ManagerRoster, waiting: [String: Int], inbox: [String: Int]) -> [String: Int] {
        var out: [String: Int] = [:]
        for m in roster.installed {
            let n = (waiting[m.id] ?? 0) + max(0, inbox[m.id] ?? 0)
            if n > 0 { out[m.id] = n }
        }
        return out
    }

    /// What VoiceOver says for a count: "3 things waiting for you".
    static func spoken(_ count: Int) -> String { String(localized: "\(count) things waiting for you") }
}

extension AppModel {
    /// What the pages list of the hub-level group's things: the current assistant's (`AssistantScope`); nil where
    /// nothing is filtered.
    var assistantScope: AssistantScope? {
        guard let group = hub?.group else { return nil }
        return AssistantScope(roster: managerRosters[group], pick: assistantPicks[group], group: group, awaited: !rosterless.contains(group))
    }

    /// The pending cards the pages list: the current assistant's agents' (a card is its own manager's, else its
    /// session's), and every other group's.
    var scopedInteractions: [Interaction] {
        guard let scope = assistantScope else { return pendingInteractions }
        return pendingInteractions.filter { scope.has($0.manager ?? latest($0.sessionKey)?.manager, group: $0.group) }
    }

    /// The computers the pages list: the current assistant's, and every other group's.
    func scopedMachines(inFront: Bool = true) -> [Machine] {
        let all = machines(inFront: inFront)
        guard let scope = assistantScope else { return all }
        return all.filter { scope.has($0.manager, group: $0.group) }
    }

    /// `sessions` as the pages list them: the current assistant's, and every other group's.
    func scoped(_ sessions: [Session]) -> [Session] {
        guard let scope = assistantScope else { return sessions }
        return sessions.filter { scope.has($0.manager, group: $0.group) }
    }

    /// Each installed assistant's count for the switcher (`AssistantBadges.counts`), by manager id; empty with one
    /// assistant.
    var assistantBadges: [String: Int] {
        guard let group = hub?.group, let roster = managerRosters[group], roster.isMulti else { return [:] }
        return AssistantBadges.counts(roster: roster, waiting: assistantWaiting, inbox: inboxNeedsYou[group] ?? [:])
    }

    /// Whether an assistant other than the current one has something waiting: the mark on the sidebar's head.
    var othersWaiting: Bool {
        guard let current = currentAssistant?.id else { return false }
        return assistantBadges.contains { $0.key != current && $0.value > 0 }
    }
}
