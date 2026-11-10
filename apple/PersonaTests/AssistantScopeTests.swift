import Foundation
import Observation
import Testing
@testable import Persona

/// The pages list the current assistant's things alone (docs/assistant/managers.md "Pages"): whose a thing is by its
/// `manager` (none, or one the roster doesn't have installed: the default's), the kept pick counting before the roster
/// is read, another group's things left alone; the model's computers, cards, sessions, dock and tab badges, the Agents
/// list's query on a hub that filters and on one that doesn't, the inbox's snapshot, the to-dos and what a change to one
/// names. Made-up values.
@MainActor
struct AssistantScopeTests {
    /// The default (`assistant`), `m-1` online, `m-2` offline, and one still installing.
    private static let roster = ManagerRoster(managers: [
        HubManager(id: "assistant", name: "助手", online: true, isDefault: true, builtin: true),
        HubManager(id: "m-1", name: "工作", online: true),
        HubManager(id: "m-2", name: "家里", online: false),
        HubManager(id: "m-9", name: "测试", online: false, installing: Date(timeIntervalSince1970: 1_800_000_000)),
    ], defaultID: "assistant")
    private static let one = ManagerRoster(managers: [HubManager(id: "assistant", name: "助手", online: true, isDefault: true)])

    private func scope(_ pick: String?, roster: ManagerRoster? = roster, awaited: Bool = true) -> AssistantScope? {
        AssistantScope(roster: roster, pick: pick, group: "", awaited: awaited)
    }

    private func machine(_ id: String, manager: String?, group: String = "") throws -> Machine {
        let stamp = manager.map { #","manager":"\#($0)""# } ?? ""
        var m = try JSON.decoder.decode(Machine.self, from: Data(#"{"id":"\#(id)","name":"\#(id)","kind":"ssh","online":true\#(stamp)}"#.utf8))
        m.group = group
        return m
    }

    private func session(_ id: String, on machine: String, manager: String?, state: String = "idle", assistant: Bool = false,
                         quiet: Bool = false, group: String = "") throws -> Session {
        let stamp = manager.map { #","manager":"\#($0)""# } ?? ""
        var s = try JSON.decoder.decode(Session.self, from: Data(
            #"{"machine_id":"\#(machine)","session_id":"\#(id)","state":"\#(state)","assistant":\#(assistant),"quiet":\#(quiet)\#(stamp)}"#.utf8))
        s.group = group
        return s
    }

    private func card(_ id: String, on machine: String, session: String, manager: String?, group: String = "") throws -> Interaction {
        let stamp = manager.map { #","manager":"\#($0)""# } ?? ""
        var ix = try JSON.decoder.decode(Interaction.self, from: Data(
            #"{"id":"\#(id)","machine_id":"\#(machine)","session_id":"\#(session)","kind":"permission"\#(stamp)}"#.utf8))
        ix.group = group
        return ix
    }

    /// A model on one made-up hub: no socket, its requests go to `hub`.
    private func model(_ hub: FakeHub = FakeHub(), roster: ManagerRoster? = roster, folder: URL? = nil) throws -> AppModel {
        let model = AppModel(stateFolder: folder ?? FileManager.default.temporaryDirectory.appending(path: "assistant-scope-" + UUID().uuidString))
        model.linkHooksForTests = (transport: try #require(hub.client.transport), socket: { SyncSocket() })
        model.connectOnly(.demo)
        model.managerRosters[""] = roster
        return model
    }

    // MARK: whose a thing is

    @Test func aThingIsItsManagersAndTheDefaultsWithoutOne() throws {
        let home = try #require(scope(nil))
        #expect(home.current == "assistant")
        #expect(home.has(nil) && home.has("") && home.has("assistant"), "no stamp: the default's")
        #expect(home.has("gone") && home.has("m-9"), "a manager deleted, or still installing: the default's, so nothing is on no page")
        #expect(!home.has("m-1") && !home.has("m-2"))

        let work = try #require(scope("m-1"))
        #expect(work.has("m-1") && !work.has(nil) && !work.has("assistant") && !work.has("gone") && !work.has("m-2"))
        #expect(scope("m-2")?.current == "m-2", "an offline one can be the current one")
        #expect(scope("gone")?.current == "assistant" && scope("m-9")?.current == "assistant", "a pick the roster hasn't installed: the default")

        #expect(work.has(nil, group: "other") && work.has("assistant", group: "other"), "another group's things are no assistant's of this hub")
        #expect(!work.has(nil, group: ""))
    }

    @Test func oneAssistantOrNoRosterFiltersNothing() {
        #expect(scope(nil, roster: Self.one) == nil && scope("m-1", roster: Self.one) == nil)
        let installing = ManagerRoster(managers: [Self.roster.managers[0], Self.roster.managers[3]])
        #expect(scope(nil, roster: installing) == nil, "one still installing is not a second assistant yet")
        #expect(scope(nil, roster: nil) == nil, "no roster and no pick: a hub from before managers, or a first launch")
        let none: AssistantScope? = nil
        #expect(none.has("m-1") && none.has(nil, group: "") && none.has("anything", group: "g"))
    }

    /// A launch that kept a pick lists its things before the roster is read, not every assistant's for a moment; a hub
    /// that answered it has no roster filters nothing.
    @Test func theKeptPickCountsBeforeTheRosterIsRead() throws {
        let work = try #require(scope("m-1", roster: nil))
        #expect(work.current == "m-1" && work.has("m-1") && !work.has(nil) && !work.has("assistant") && !work.has("m-2"))
        let home = try #require(scope("assistant", roster: nil))
        #expect(home.has(nil) && home.has("assistant") && !home.has("m-1"), "no stamp is the hub's own assistant's until the roster says otherwise")
        #expect(scope("", roster: nil) == nil)
        #expect(scope("m-1", roster: nil, awaited: false) == nil, "the hub has none: the pick names nothing there")
    }

    @Test func aHubWithoutARosterStopsTheKeptPickFromFiltering() async throws {
        let folder = FileManager.default.temporaryDirectory.appending(path: "assistant-scope-" + UUID().uuidString)
        AssistantPickStore.save(["": "m-1"], folder: folder)
        let old = FakeHub() // no roster route: 404 with no code
        let model = try model(old, roster: nil, folder: folder)
        model.replaceMachines("", [try machine("server", manager: nil)])
        #expect(model.assistantScope?.current == "m-1" && model.scopedMachines().isEmpty, "until the hub answers, the pick's alone")
        await model.refreshManagerRoster("")
        #expect(model.assistantScope == nil && model.scopedMachines().map(\.id) == ["server"])
        #expect(model.assistantPicks == ["": "m-1"], "the pick itself is kept: a roster read later names it again")
    }

    // MARK: the model's lists

    @Test func thePagesListTheCurrentAssistantsComputersCardsAndSessions() throws {
        let model = try model()
        model.replaceMachines("", [try machine("server", manager: nil), try machine("MacBook", manager: "m-1"), try machine("NAS", manager: "m-2")])
        let sessions = [try session("a", on: "server", manager: "assistant"), try session("b", on: "MacBook", manager: "m-1"),
                        try session("c", on: "NAS", manager: "m-2"), try session("d", on: "server", manager: nil)]
        model.applySessions(Dictionary(grouping: sessions, by: \.machineRef))
        model.replaceInteractions("", [try card("x1", on: "server", session: "a", manager: "assistant"),
                                       try card("x2", on: "MacBook", session: "b", manager: "m-1"),
                                       try card("x3", on: "MacBook", session: "b", manager: nil)])

        #expect(model.scopedMachines().map(\.id) == ["server"])
        #expect(Set(model.scoped(model.allSessions).map(\.sessionID)) == ["a", "d"])
        #expect(model.scopedInteractions.map(\.id) == ["x1"])

        model.selectAssistantManager("m-1")
        #expect(model.scopedMachines().map(\.id) == ["MacBook"])
        #expect(model.scoped(model.allSessions).map(\.sessionID) == ["b"])
        #expect(Set(model.scopedInteractions.map(\.id)) == ["x2", "x3"], "a card without a stamp goes by its session's manager")
        #expect(model.machines.count == 3 && model.allSessions.count == 4 && model.pendingInteractions.count == 3,
                "the lists themselves stay whole: another assistant's thing still opens from a notification")
        #expect(model.machine(MachineRef(group: "", id: "NAS")) != nil && model.latest(try #require(sessions.first).key) != nil)

        model.selectAssistantManager("m-2")
        #expect(model.scopedMachines().map(\.id) == ["NAS"] && model.scopedInteractions.isEmpty)

        model.managerRosters[""] = Self.one
        #expect(model.scopedMachines().count == 3 && model.scopedInteractions.count == 3, "one assistant: nothing is filtered")
    }

    @Test func anotherGroupsThingsAreOnEveryAssistantsPages() throws {
        let model = try model()
        model.replaceMachines("", [try machine("server", manager: nil), try machine("MacBook", manager: "m-1")])
        model.replaceMachines("other", [try machine("NAS", manager: "m-7", group: "other")])
        model.replaceInteractions("other", [try card("y", on: "NAS", session: "s", manager: "m-7", group: "other")])
        model.selectAssistantManager("m-1")
        #expect(Set(model.scopedMachines().map(\.id)) == ["MacBook", "NAS"])
        #expect(model.scopedInteractions.map(\.id) == ["y"])
    }

    /// The tabs count the current assistant's cards alone, and count again when another becomes current.
    @Test func theTabBadgesCountTheCurrentAssistantsCards() throws {
        let model = try model()
        let sessions = [try session("a", on: "server", manager: "assistant"), try session("b", on: "MacBook", manager: "m-1"),
                        try session("main", on: "MacBook", manager: "m-1", assistant: true)]
        model.applySessions(Dictionary(grouping: sessions, by: \.machineRef))
        model.replaceInteractions("", [try card("x1", on: "server", session: "a", manager: "assistant"),
                                       try card("x2", on: "MacBook", session: "b", manager: "m-1"),
                                       try card("x3", on: "MacBook", session: "b", manager: "m-1"),
                                       try card("x4", on: "MacBook", session: "main", manager: "m-1")])
        #expect(model.badges == TabBadges(assistant: 0, agents: 1))
        model.selectAssistantManager("m-1")
        #expect(model.badges == TabBadges(assistant: 1, agents: 2))
        model.selectAssistantManager(nil)
        #expect(model.badges == TabBadges(assistant: 0, agents: 1))
        model.managerRosters[""] = Self.one
        model.relistAssistant()
        #expect(model.badges == TabBadges(assistant: 1, agents: 3), "one assistant: all of them, as before")
    }

    /// The main conversation's dock shows the current assistant's agents going on.
    @Test func theDockShowsTheCurrentAssistantsAgents() throws {
        let model = try model()
        let sessions = [try session("a", on: "server", manager: "assistant", state: "working"),
                        try session("b", on: "MacBook", manager: "m-1", state: "needs_you")]
        model.applySessions(Dictionary(grouping: sessions, by: \.machineRef))
        #expect(model.goingAgents.map(\.key.sessionID) == ["a"])
        model.selectAssistantManager("m-1")
        #expect(model.goingAgents.map(\.key.sessionID) == ["b"])
    }

    // MARK: the Agents list's query

    /// A hub that answers a paged list of whatever manager it is asked for (none: every manager's), and counts.
    private func pagedHub() -> FakeHub {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/sessions") { req in
            let manager = req.query["manager"]
            if req.query["counts"] == "1" {
                return Data(#"{"total":\#(manager == nil ? 3 : 1),"machine":{},"project":{},"group":{},"state":{},"revision":"r"}"#.utf8)
            }
            guard req.query["limit"] != nil else {
                // the whole list, asked without the bounded API's keys
                return Data(#"""
                {"sessions":[{"machine_id":"server","session_id":"a","state":"idle","manager":"assistant"},
                             {"machine_id":"MacBook","session_id":"b","state":"needs_you","manager":"m-1"},
                             {"machine_id":"NAS","session_id":"c","state":"unknown","manager":"m-2"}]}
                """#.utf8)
            }
            let rows = [("server", "a", "assistant"), ("MacBook", "b", "m-1"), ("NAS", "c", "m-2")].filter { manager == nil || $0.2 == manager }
                .map { #"{"machine_id":"\#($0.0)","session_id":"\#($0.1)","state":"idle","manager":"\#($0.2)"}"# }.joined(separator: ",")
            return Data(#"{"sessions":[\#(rows)],"remove":[],"has_more":false,"revision":"r-\#(manager ?? "all")","total":\#(manager == nil ? 3 : 1)}"#.utf8)
        }
        return hub
    }

    /// The list's own requests: not its counts, nor the read of every manager's sessions in Needs You for the switcher.
    private func pages(_ hub: FakeHub) -> [LocalRequest] {
        hub.sent("GET", "/api/v1/sessions").filter { $0.query["counts"] == nil && $0.query["state"] != "needs_you" }
    }

    @Test func aHubThatFiltersIsAskedForTheCurrentAssistantsSessions() async throws {
        let hub = pagedHub()
        let model = try model(hub)
        model.link("")?.received(HubHello(features: ["sessions_list", SessionListQuery.sessionsManagerFeature]))
        await model.sessionLists.select(SessionListQuery(), hub: nil, model: model)
        #expect(pages(hub).last?.query["manager"] == "assistant" && pages(hub).last?.query["limit"] == "50")
        #expect(hub.sent("GET", "/api/v1/sessions").last { $0.query["counts"] == "1" }?.query["manager"] == "assistant", "its counts too")
        #expect(model.sessionLists.sessions.map(\.sessionID) == ["a"] && model.sessionLists.total == 1)

        // Another assistant: another list, started over (a cursor is bound to its filters).
        model.selectAssistantManager("m-1")
        await model.sessionLists.rescope("", model: model)
        #expect(pages(hub).last?.query["manager"] == "m-1" && pages(hub).last?.query["cursor"] == nil)
        #expect(model.sessionLists.sessions.map(\.sessionID) == ["b"] && model.sessionLists.total == 1)
        #expect(model.sessionLists.isPaged(""))

        // A section's own page and counts name it too.
        let section = RuntimeSessionLists.Scope(hub: "", query: SessionListQuery(machine: "MacBook"))
        await model.sessionLists.expand(section)
        #expect(pages(hub).last?.query["machine_id"] == "MacBook" && pages(hub).last?.query["manager"] == "m-1")

        // One assistant again: no manager is named.
        model.managerRosters[""] = Self.one
        await model.sessionLists.rescope("", model: model)
        #expect(pages(hub).last?.query["manager"] == nil && model.sessionLists.sessions.count == 3)
    }

    /// A hub with bounded lists but without the filter would page, total and count every manager's sessions: it is
    /// asked for the whole list instead, filtered here by each row's `manager`, so the list and its counts are right.
    @Test func aHubThatCannotFilterIsAskedForTheWholeListWhichIsFilteredHere() async throws {
        let hub = pagedHub()
        let model = try model(hub)
        model.link("")?.received(HubHello(features: ["sessions_list"]))
        model.selectAssistantManager("m-1")
        await model.sessionLists.select(SessionListQuery(), hub: nil, model: model)
        let asked = try #require(pages(hub).last)
        #expect(asked.query["manager"] == nil && asked.query["limit"] == nil, "the whole list, with no key an older hub would ignore")
        #expect(!model.sessionLists.isPaged(""))
        #expect(model.sessionLists.sessions.map(\.sessionID) == ["b"])
        #expect(model.sessionLists.total == 1 && model.sessionLists.stateCounts == nil, "counted here, of this assistant's alone")
        #expect(model.allSessions.count == 3, "the others' rows are still there for what opens them")

        // The hub is updated: the next hello has the filter, and the list is asked the bounded way.
        model.link("")?.received(HubHello(features: ["sessions_list", SessionListQuery.sessionsManagerFeature]))
        await model.sessionLists.rescope("", model: model)
        #expect(pages(hub).last?.query["manager"] == "m-1" && model.sessionLists.isPaged(""))
    }

    @Test func theQueryNamesNoManagerWhereNothingIsFiltered() async throws {
        let hub = pagedHub()
        let model = try model(hub, roster: Self.one)
        model.sessionLists.bind(model)
        model.link("")?.received(HubHello(features: ["sessions_list", SessionListQuery.sessionsManagerFeature]))
        #expect(model.sessionLists.managed(SessionListQuery(state: "working"), group: "") == SessionListQuery(state: "working"))
        model.managerRosters[""] = Self.roster
        model.selectAssistantManager("m-1")
        #expect(model.sessionLists.managed(SessionListQuery(), group: "").manager == "m-1")
        #expect(model.sessionLists.managed(SessionListQuery(), group: "other") == SessionListQuery(), "another group's hub is not this one's managers'")
        let whole = model.sessionLists.managed(SessionListQuery(legacy: true), group: "")
        #expect(whole.manager.isEmpty && whole.legacy, "the whole list is never one manager's: it replaces the app's rows of the hub")
        #expect(SessionListQuery(manager: "m-1").parameters["manager"] == "m-1" && SessionListQuery().parameters["manager"] == nil)
    }

    /// Before a hub's hello says what it has, its list is neither asked with a key it may ignore nor swapped for the
    /// whole list: the page as it is, filtered here.
    @Test func beforeTheHelloTheQueryIsLeftAsItIs() throws {
        let model = try model()
        model.sessionLists.bind(model)
        model.selectAssistantManager("m-1")
        #expect(model.sessionLists.managed(SessionListQuery(), group: "") == SessionListQuery())
    }

    // MARK: the inbox

    private func item(_ id: String, _ manager: String?, tier: String = "action", done: Bool = false, connector: Bool = false) throws -> InboxItem {
        let stamp = manager.map { #","manager":"\#($0)""# } ?? ""
        let filed = done ? #","done_at":"2026-11-04T08:00:00Z","done_by":"read""# : ""
        let source = connector ? #","connector":{"thread":"t-\#(id)","open_url":"https://example.com/\#(id)"}"# : ""
        return try JSON.decoder.decode(InboxItem.self, from: Data(#"""
        {"id":"\#(id)","source":"mail","kind":"mail","account":"acct-\#(manager ?? "none")","account_name":"\#(manager ?? "none")","title":"\#(id)","preview":"",
         "count":1,"version":"v1","tier":"\#(tier)"\#(stamp)\#(filed)\#(source)}
        """#.utf8))
    }

    @Test func theInboxListsTheCurrentAssistantsItemsAndSources() throws {
        let all = [try item("a", "assistant"), try item("b", nil, tier: "fyi"), try item("c", "m-2", tier: "fyi"),
                   try item("w", "m-1", connector: true), try item("d", "m-1", done: true), try item("e", "gone", connector: true)]
        func listed(_ pick: String?, roster: ManagerRoster = Self.roster) -> InboxProjection {
            InboxProjection(items: all, filter: .init(scope: scope(pick, roster: roster)))
        }
        let home = listed(nil)
        #expect(home.openItems.map(\.id) == ["a", "b", "e"] && home.doneItems.isEmpty, "no stamp, or one the roster lacks: the default's")
        #expect(home.byTier[.action]?.map(\.id) == ["a", "e"] && home.byTier[.fyi]?.map(\.id) == ["b"])
        #expect(home.connectorSources.map(\.id) == ["acct-gone"], "only its own items' sources are offered as filters")

        let work = listed("m-1")
        #expect(work.openItems.map(\.id) == ["w"] && work.doneItems.map(\.id) == ["d"])
        #expect(work.connectorSources.map(\.id) == ["acct-m-1"])
        #expect(listed("m-2").openItems.map(\.id) == ["c"])

        let whole = listed(nil, roster: Self.one)
        #expect(whole.openItems.count == 5 && whole.doneItems.count == 1 && whole.connectorSources.count == 2, "one assistant: the page as before")

        // The page's snapshot is made again when another assistant becomes current.
        let memo = InboxProjectionMemo()
        let first = memo(all, filter: .init(scope: scope(nil)), revision: 1)
        #expect(memo(all, filter: .init(scope: scope(nil)), revision: 1) === first)
        #expect(memo(all, filter: .init(scope: scope("m-1")), revision: 1).openItems.map(\.id) == ["w"])
    }

    @Test func historyItemsAreStampedWithTheManagerAsked() throws {
        let plain = try item("h", nil, done: true), stamped = try item("k", "m-2", done: true)
        let out = InboxStore.stamped([plain, stamped], manager: "m-1")
        #expect(out.map(\.manager) == ["m-1", "m-2"] && out[0].key == (try item("h", "m-1")).key, "one key with the merged list's copy")
        #expect(InboxStore.stamped([plain], manager: nil) == [plain], "one manager: no stamp")
    }

    @Test func theSectionsFoldWithoutAManager() {
        var folds = InboxFolds()
        #expect(folds.isOpen("action") && folds.isOpen("fyi") && !folds.isOpen("noise") && !folds.isOpen("done"))
        folds.toggle("fyi")
        folds.toggle("done")
        #expect(!folds.isOpen("fyi") && folds.isOpen("done"))
        folds.toggle("working")
        #expect(folds.isOpen("working"), "In Progress stays open")
    }

    // MARK: to-dos

    /// A store that goes by `scope` (a made-up current assistant), naming managers as the roster does.
    private func todos(_ list: [TodoItem], pick: String?) -> TodoStore {
        let current = scope(pick)
        return TodoStore(cachedTodos: list, cacheWriter: nil, hubListChanged: {},
                         managers: .init(scope: { current }, naming: { Self.roster.naming($0) }))
    }

    private static let list = [TodoItem(id: "t1", title: "home", manager: "assistant"), TodoItem(id: "t2", title: "bare"),
                               TodoItem(id: "t3", title: "work", manager: "m-1"), TodoItem(id: "t4", title: "quiet", quiet: true, manager: "m-1"),
                               TodoItem(id: "t5", title: "done", doneAt: Date(timeIntervalSince1970: 1_780_000_000), manager: "m-1")]

    @Test func theToDosListedAndCountedAreTheCurrentAssistants() throws {
        let decoded = try JSON.decoder.decode(TodoItem.self, from: Data(#"{"id":"t","title":"x","manager":"m-1"}"#.utf8))
        #expect(decoded.manager == "m-1")
        #expect(try JSON.decoder.decode(TodoItem.self, from: Data(#"{"id":"t","title":"x"}"#.utf8)).manager == nil)

        let home = todos(Self.list, pick: nil)
        #expect(home.scopedTodos.map(\.id) == ["t1", "t2"] && home.badgeCount == 2 && home.openTodos.map(\.id) == ["t1", "t2"])
        let work = todos(Self.list, pick: "m-1")
        #expect(work.scopedTodos.map(\.id) == ["t3", "t4", "t5"] && work.openTodos.map(\.id) == ["t3", "t4"])
        #expect(work.badgeCount == 1, "its open ones, a quiet task left out")
        #expect(work.todos.count == 5 && work.todo(id: "t1") != nil && work.quiet == home.quiet, "the list itself stays whole")
        let whole = TodoStore(cachedTodos: Self.list, cacheWriter: nil, hubListChanged: {})
        #expect(whole.scopedTodos.count == 5 && whole.badgeCount == 3, "one assistant: all of them")
    }

    @Test func aNewToDoIsTheCurrentAssistantsAndAChangeGoesToTheToDosOwn() async throws {
        let hub = FakeHub()
        hub.on("POST", "/api/v1/todos", json: #"{"todo":{"id":"new","title":"n"}}"#)
        hub.on("PATCH", "/api/v1/todos/t1", json: #"{"todo":{"id":"t1","title":"renamed"}}"#)
        hub.on("PATCH", "/api/v1/todos/t3", json: #"{"todo":{"id":"t3","title":"renamed"}}"#)
        hub.on("DELETE", "/api/v1/todos/t3", json: #"{"ok":true}"#)
        hub.on("PUT", "/api/v1/todos/t3/agents/MacBook/s1", json: #"{"ok":true}"#)
        hub.on("GET", "/api/v1/todos", json: #"{"todos":[{"id":"t3","title":"renamed","manager":"m-1"}]}"#)
        func named(_ method: String, _ path: String) -> [String?] { hub.sent(method, path).map { $0.headers["X-Persona-Manager"] } }

        // m-1 is current: a new one is made for it, and listed as its own at once.
        let work = todos(Self.list, pick: "m-1")
        let made = try await work.create(title: "n", api: hub.client)
        #expect(named("POST", "/api/v1/todos") == ["m-1"] && made.manager == "m-1")
        #expect(work.scopedTodos.first?.id == "new")

        // A change to another assistant's to-do (opened from a link) goes to that one; the default's names none.
        let home = try await work.update(id: "t1", title: "renamed", api: hub.client)
        let own = try await work.update(id: "t3", title: "renamed", api: hub.client)
        #expect(named("PATCH", "/api/v1/todos/t1") == [nil] && named("PATCH", "/api/v1/todos/t3") == ["m-1"])
        #expect(home.manager == "assistant" && own.manager == "m-1", "an answer has no stamp: the to-do keeps the list's")
        #expect(work.named("t3") == "m-1" && work.named("t1") == nil && work.named("unknown") == nil)

        // The list is read merged, with none named, also after a change that named one.
        try await work.linkAgent(todoID: "t3", machineID: "MacBook", sessionID: "s1", api: hub.client)
        #expect(named("PUT", "/api/v1/todos/t3/agents/MacBook/s1") == ["m-1"])
        #expect(named("GET", "/api/v1/todos") == [nil])
        try await work.delete(id: "t3", api: hub.client)
        #expect(named("DELETE", "/api/v1/todos/t3") == ["m-1"])

        // The default is current: a new one names none.
        let fresh = todos(Self.list, pick: nil)
        _ = try await fresh.create(title: "n", api: hub.client)
        #expect(named("POST", "/api/v1/todos") == ["m-1", nil])
        #expect(fresh.scopedTodos.first?.manager == "assistant")
    }

    // MARK: requests

    @Test func theKnowledgeBaseIsTheCurrentAssistants() async throws {
        #expect(APIClient.assistantRoute("/api/v1/kb/projects", query: [:]) && APIClient.assistantRoute("/api/v1/kb-search", query: ["q": "x"]))
        #expect(APIClient.assistantRoute("/api/v1/kb/chats/c1/days/2026-11-04", query: [:]) && APIClient.assistantRoute("/api/v1/kb/selection", query: [:]))
        #expect(!APIClient.assistantRoute("/api/v1/kbx", query: [:]) && !APIClient.assistantRoute("/api/v1/recordings", query: [:]),
                "this device syncs its recordings to the hub whichever assistant is current")
        #expect(!APIClient.assistantRoute("/api/v1/todos", query: [:]) && !APIClient.assistantRoute("/api/v1/inbox", query: [:])
                && !APIClient.assistantRoute("/api/v1/sessions", query: [:]), "merged lists stay whole")
        let hub = FakeHub()
        let page = hub.client.showing("m-1")
        _ = try? await page.kbList(.projects)
        _ = try? await page.kbFind("x")
        _ = try? await page.todos()
        _ = try? await hub.client.kbList(.projects)
        #expect(hub.requests.map { $0.headers["X-Persona-Manager"] } == ["m-1", "m-1", nil, nil])
    }

    /// What a conversation's feed offers (a plan's Start, a briefing question's Handled) is asked of the conversation's
    /// own manager: those routes name no conversation, so the hub would take them to the default.
    @Test func whatAConversationsFeedOffersGoesToItsOwnManager() async throws {
        let model = try model()
        let chats = [try session("c-home", on: "server", manager: "assistant", assistant: true),
                     try session("c-work", on: "MacBook", manager: "m-1", assistant: true),
                     try session("c-bare", on: "server", manager: nil, assistant: true),
                     try session("agent", on: "MacBook", manager: "m-1")]
        model.applySessions(Dictionary(grouping: chats, by: \.machineRef))
        #expect(model.feedManager(of: "c-work", group: "") == "m-1")
        #expect(model.feedManager(of: "c-home", group: "") == nil && model.feedManager(of: "c-bare", group: "") == nil, "the default's: none named")
        #expect(model.feedManager(of: "unlisted", group: "") == nil, "not listed, the default current: none")

        model.selectAssistantManager("m-2")
        #expect(model.feedManager(of: "c-work", group: "") == "m-1" && model.feedManager(of: "c-home", group: "") == nil,
                "whichever assistant is current")
        #expect(model.feedManager(of: "unlisted", group: "") == "m-2", "not listed yet: the page it shows on")
        #expect(model.feedManager(of: "agent", group: "") == "m-2", "an agent's session is no conversation")
        model.assistantItemManagers[SessionKey(group: "", machineID: "NAS", sessionID: "opened")] = "m-1"
        #expect(model.feedManager(of: "opened", group: "") == "m-1", "opened from a notification as another assistant's")

        let hub = FakeHub()
        hub.on("POST", "/api/v1/later/lt_1/handled", json: #"{"ok":true}"#)
        let actions = BriefActions(api: { _ in hub.client }, failed: { _ in }, taken: {}, named: { _ in nil },
                                   feedManager: { conversation, group in model.feedManager(of: conversation, group: group) })
        await actions.markHandled(BriefBody.Row(id: "lt_1", title: "?"), group: "", conversation: "c-work")
        #expect(hub.sent("POST", "/api/v1/later/lt_1/handled").map { $0.headers["X-Persona-Manager"] } == ["m-1"])
    }

    /// Scheduled Tasks left open while another assistant becomes current: what it writes still goes to the assistant
    /// whose tasks it shows, until it starts over on the new one.
    @Test func scheduledTasksWriteToTheAssistantTheyWereReadFrom() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/assistant/tasks") { req in
            let id = req.headers["X-Persona-Manager"] ?? "assistant"
            return Data(#"{"tasks":[{"id":"t-\#(id)","title":"\#(id)","prompt":"p","repeat":"daily","time":"09:00","weekdays":[1,2,3,4,5],"enabled":true}]}"#.utf8)
        }
        hub.on("GET", "/api/v1/assistant/runs", json: #"{"runs":[]}"#)
        hub.on("DELETE", "/api/v1/assistant/tasks/t-m-1", json: #"{"ok":true}"#)
        var client = hub.client.showing("m-1")
        let page = AssistantRoutinesModel(deps: .init(api: { client }, report: { _ in }, toast: { _ in }, open: { _ in }))
        await page.load()
        #expect(page.tasks.map(\.id) == ["t-m-1"])

        client = hub.client.showing(nil) // the default is current now
        await page.delete(try #require(page.tasks.first))
        #expect(hub.sent("DELETE", "/api/v1/assistant/tasks/t-m-1").map { $0.headers["X-Persona-Manager"] } == ["m-1"])

        await page.restart()
        #expect(page.loaded && page.tasks.map(\.id) == ["t-assistant"], "the page starts over on the one current now")
        #expect(hub.sent("GET", "/api/v1/assistant/tasks").map { $0.headers["X-Persona-Manager"] } == ["m-1", nil])
    }
}
