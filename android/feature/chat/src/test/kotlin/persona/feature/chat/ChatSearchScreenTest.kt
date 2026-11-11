package persona.feature.chat

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.Snapshot
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOf
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Item
import persona.core.contracts.SessionKey
import persona.core.contracts.api.ChatRepository
import persona.core.contracts.api.ChatSnapshot
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme

@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
class ChatSearchScreenTest {
    @get:Rule val rule = createComposeRule()

    private val repository = object : ChatRepository {
        override fun observe(session: SessionKey): Flow<ChatSnapshot> = flowOf(ChatSnapshot())
        override suspend fun loadOlder(session: SessionKey) = Unit
        override suspend fun expandFold(session: SessionKey, fromSeq: Int, toSeq: Int) = emptyList<Item>()
    }

    private fun shown(text: String): Boolean = rule.onAllNodesWithText(text).fetchSemanticsNodes().isNotEmpty()

    private fun waitForText(text: String) {
        val deadline = System.nanoTime() + 10_000_000_000L
        while (!shown(text) && System.nanoTime() < deadline) {
            Thread.sleep(20)
            Snapshot.sendApplyNotifications()
            rule.mainClock.advanceTimeBy(100)
        }
        rule.onNodeWithText(text).assertExists()
    }

    private fun showLiveChunkBeforeAnotherDebounce(text: String) {
        var elapsed = 0
        while (!shown(text) && elapsed < 200) {
            // The search itself runs on Dispatchers.Default; allow real time for it without advancing the query clock.
            Thread.sleep(20)
            Snapshot.sendApplyNotifications()
            rule.mainClock.advanceTimeByFrame()
            elapsed += 16
        }
        assertTrue("a live chunk must not wait for another 200 ms debounce", elapsed < 200)
        rule.onNodeWithText(text).assertExists()
    }

    @Test fun liveChunksKeepVisibleHitsAndDoNotRestartTheQueryDebounce() {
        val old = Item("old", 1, role = "assistant", kind = "text", text = "Settled needle text")
        val reply = Item("reply", 2, role = "assistant", kind = "text", text = "No match yet")
        val state = ChatSearchState().apply { query = "needle" }
        var searchItems by mutableStateOf(ChatSearchItems(listOf(old, reply)))
        rule.mainClock.autoAdvance = false
        rule.setContent {
            PersonaTheme(style = AppStyle.Brief, darkTheme = false, reduceMotion = true) {
                ChatSearchScreen(state, searchItems, 2, hasOlder = false, repository,
                    SessionKey("computer", "conversation"), onClose = {})
            }
        }
        rule.mainClock.advanceTimeBy(250)
        waitForText("Settled needle text")

        val first = reply.copy(text = "Streaming needle chunk one")
        rule.runOnIdle {
            searchItems = ChatSearchItems(listOf(old, first), structural = false, changed = mapOf(first.id to first))
        }
        showLiveChunkBeforeAnotherDebounce("Streaming needle chunk one")
        rule.onNodeWithText("Settled needle text").assertExists()

        val second = reply.copy(text = "Streaming needle chunk two")
        rule.runOnIdle {
            searchItems = ChatSearchItems(listOf(old, second), structural = false, changed = mapOf(second.id to second))
        }
        showLiveChunkBeforeAnotherDebounce("Streaming needle chunk two")
        rule.onNodeWithText("Settled needle text").assertExists()
        rule.onNodeWithText("Streaming needle chunk one").assertDoesNotExist()

        val queried = second.copy(text = "A new query now finds other text")
        rule.runOnIdle {
            searchItems = ChatSearchItems(listOf(old, queried), structural = false, changed = mapOf(queried.id to queried))
            state.query = "other"
        }
        Snapshot.sendApplyNotifications()
        rule.mainClock.advanceTimeByFrame()
        rule.onNodeWithText("Settled needle text").assertDoesNotExist()
        rule.onNodeWithText("Streaming needle chunk two").assertDoesNotExist()
        rule.mainClock.advanceTimeBy(150)
        rule.onNodeWithText("A new query now finds other text").assertDoesNotExist()
        rule.mainClock.advanceTimeBy(50)
        waitForText("A new query now finds other text")
    }
}
