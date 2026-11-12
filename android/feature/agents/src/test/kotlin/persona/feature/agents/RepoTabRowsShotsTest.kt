package persona.feature.agents

import androidx.activity.ComponentActivity
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.hasContentDescription
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.test.performClick
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.emptyPreferences
import androidx.navigation3.runtime.NavKey
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.serialization.json.JsonObject
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.designsystem.AppStyle
import persona.core.designsystem.HomeChrome
import persona.core.designsystem.LocalHomeChrome
import persona.core.designsystem.PersonaTheme
import persona.core.domain.sort.AgentGrouping
import persona.core.navigation.AppTab
import persona.core.navigation.Navigator
import java.io.File
import java.time.Duration
import java.time.Instant
import java.time.temporal.ChronoUnit

/**
 * By Repository's tabs of several agents on the Agents page (#4338 A11, iOS `TabGroupRow` in `entryRows`): the main
 * checkout holds a tab of four agents (a starter and three panes it split off, two running the same agent), a tab of two
 * idle peers and an agent alone in its tab; a worktree holds a tab of two. Every style, light and dark, as it opens, and
 * on Glass with the idle agents opened and under By Project, where the agents of a tab are named the same way; PNGs in
 * `build/repo-tab-rows-shots/`.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en-w400dp-h1700dp")
@org.robolectric.annotation.GraphicsMode(org.robolectric.annotation.GraphicsMode.Mode.NATIVE)
class RepoTabRowsShotsTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    // Whole hours ago, at least one, so the words stay the same through the hour.
    private val now = Instant.now().truncatedTo(ChronoUnit.HOURS)
    private fun ago(hours: Long) = now.minus(Duration.ofHours(hours)).toString()
    private fun at(id: String, title: String, state: String, cwd: String, hoursAgo: Long, agent: String, slot: Int, pane: Int?,
                   tab: String? = null, extra: String = "") =
        """"machine_id":"laptop","session_id":"$id","title":"$title","state":"$state","cwd":"$cwd","updated_at":"${ago(hoursAgo)}",""" +
            """"agent":"$agent","space_id":"w1","space_name":"persona","slot_id":"t$slot","slot_index":$slot,"pane_id":"w1:p$id"""" +
            (pane?.let { ""","pane_index":$it""" } ?: "") + (tab?.let { ""","slot_name":"$it"""" } ?: "") + extra
    private val repo = "/home/me/code/persona"
    private val sessions = listOf(
        at("starter", "Draft the release notes", "working", repo, 1, "claude", 1, 0, tab = "release"),
        at("migration", "Review the migration", "needs_you", repo, 1, "codex", 1, 1, tab = "release"),
        at("shots", "Update the screenshots", "unread", repo, 2, "claude", 1, 2, tab = "release"),
        at("proof", "Proofread the notes", "idle", repo, 3, "claude", 1, 3, tab = "release"),
        at("cache", "Compare the cache libraries", "idle", repo, 5, "claude", 2, 1),
        at("bench", "Benchmark the importer", "idle", repo, 6, "claude", 2, 2),
        at("login", "Fix the login bug", "working", repo, 1, "claude", 3, 0, tab = "build"),
        at("bisect", "Bisect the crash", "working", "$repo/.worktrees/bisect", 1, "claude", 4, 0, tab = "bisect",
            extra = ""","background_waiting":true"""),
        at("regress", "Write the regression test", "working", "$repo/.worktrees/bisect", 1, "codex", 4, 1, tab = "bisect"),
    ).map { JsonCodec.decodeFromString(Session.serializer(), "{$it}") }

    @Test fun glassLight() = shoot(AppStyle.Glass, dark = false)
    @Test fun glassDark() = shoot(AppStyle.Glass, dark = true)
    @Test fun briefLight() = shoot(AppStyle.Brief, dark = false)
    @Test fun briefDark() = shoot(AppStyle.Brief, dark = true)
    @Test fun boardLight() = shoot(AppStyle.Board, dark = false)
    @Test fun boardDark() = shoot(AppStyle.Board, dark = true)
    @Test fun glassOpenLight() = shoot(AppStyle.Glass, dark = false, open = true)
    @Test fun glassOpenDark() = shoot(AppStyle.Glass, dark = true, open = true)
    @Test fun glassByProjectLight() = shoot(AppStyle.Glass, dark = false, byProject = true)
    @Test fun glassByProjectDark() = shoot(AppStyle.Glass, dark = true, byProject = true)

    /** On Glass a tab's line is one button TalkBack reads as the tab, its count and state; a tap folds its agents and opens them again. */
    @Test fun aTabsLineFoldsItsAgentsAndOpensThemAgain() {
        shoot(AppStyle.Glass, dark = false, save = false)
        val line = "Tab release, 4 agents, Needs you"
        rule.onNode(hasText("Proofread the notes")).assertExists()
        rule.onNodeWithContentDescription(line).performClick()
        rule.waitForIdle()
        rule.onNode(hasText("Proofread the notes")).assertDoesNotExist()
        rule.onNodeWithContentDescription(line).performClick()
        rule.waitForIdle()
        rule.onNode(hasText("Proofread the notes")).assertExists()
    }

    private fun shoot(style: AppStyle, dark: Boolean, open: Boolean = false, byProject: Boolean = false, save: Boolean = true) {
        val model = AgentsViewModel(object : SessionsRepository {
            override val sessions = MutableStateFlow(this@RepoTabRowsShotsTest.sessions)
            override val machines = MutableStateFlow(listOf(Machine("laptop", "Laptop", online = true)))
            override suspend fun refresh() = Unit
        }, object : InteractionCenter {
            override val pending = MutableStateFlow<List<Interaction>>(emptyList())
            override suspend fun refresh() = Unit
            override suspend fun resolve(id: String, resolution: Resolution) = Unit
        }, object : HubConnection {
            override val state = MutableStateFlow(ConnectionState.Online)
            override val events = MutableSharedFlow<ServerEvent>()
            override fun reconnect() = Unit
            override fun send(message: JsonObject) = true
        }, object : MessageSender {
            override val outbox = MutableStateFlow<List<OutboxMessage>>(emptyList())
            override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) = Unit
        }, AgentsPreferences(object : DataStore<Preferences> {
            override val data = MutableStateFlow<Preferences>(emptyPreferences())
            override suspend fun updateData(transform: suspend (Preferences) -> Preferences) =
                transform(data.value).also { data.value = it }
        }, object : HubPreferences {
            override val preferences = MutableStateFlow(HubPreferencesSnapshot())
            override suspend fun refresh() = Unit
            override suspend fun togglePin(path: String, space: Boolean) = Unit
            override suspend fun toggleMute(path: String, space: Boolean) = Unit
            override suspend fun markSeen(session: SessionKey, version: String) = Unit
        }))
        if (byProject) model.grouping(AgentGrouping.MachineSpace)
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                CompositionLocalProvider(LocalHomeChrome provides HomeChrome(emptyList(), "Agents", onSelect = {}, leading = {})) {
                    AgentsScreen(model, Nowhere, AgentsMetadata())
                }
            }
        }
        rule.waitUntil(5_000) { model.page.value.loaded && (!byProject || model.page.value.preferences.grouping == AgentGrouping.MachineSpace) }
        rule.waitForIdle()
        if (open) {
            // The main checkout's idle agents, opened: the idle tab's line with its two peers, and "Hide 2 Idle" (on main
            // before #4338 A11 the busy tab's idle agent was behind the line too: three of them).
            rule.onNode(hasContentDescription("Show 2 idle agents") or hasContentDescription("Show 3 idle agents")).performClick()
            rule.waitForIdle()
        }
        if (!save) return
        val out = File(System.getProperty("persona.shots") ?: "build/repo-tab-rows-shots").also { it.mkdirs() }
        rule.onRoot().captureToImage().asAndroidBitmap().let { bitmap ->
            val variant = if (open) "-open" else if (byProject) "-by-project" else ""
            out.resolve("repo-tab-rows-${style.name.lowercase()}$variant-${if (dark) "dark" else "light"}.png").outputStream()
                .use { bitmap.compress(android.graphics.Bitmap.CompressFormat.PNG, 100, it) }
        }
    }

    private object Nowhere : Navigator {
        override fun navigate(key: NavKey) = Unit
        override fun back() = Unit
        override fun selectTab(tab: AppTab) = Unit
    }
}
