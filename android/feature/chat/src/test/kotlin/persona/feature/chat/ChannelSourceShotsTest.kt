package persona.feature.chat

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.runtime.snapshots.Snapshot
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithContentDescription
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import kotlinx.coroutines.flow.MutableStateFlow
import persona.core.contracts.ExternalOrigin
import persona.core.contracts.Interaction
import persona.core.contracts.Item
import persona.core.contracts.Machine
import persona.core.contracts.Resolution
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.contracts.api.ChatRepository
import persona.core.contracts.api.ChatSnapshot
import persona.core.contracts.api.HubChannel
import persona.core.contracts.api.InteractionCenter
import persona.core.contracts.api.MessageSender
import persona.core.contracts.api.OutboxMessage
import persona.core.contracts.api.SessionsRepository
import persona.core.designsystem.*
import java.io.File

/**
 * "From WeChat" under a user message that came from a channel (docs/extensions/channels.md "Source markers"), drawn on the JVM with
 * Robolectric's native graphics, in every style, light and dark: one from WeChat (a monogram), one from the Feishu bot
 * (its mark), one the app sent (no line); and the line under the message that opened a conversation an extension opened
 * (#3758). PNGs in `build/channel-shots/` (or `-Dpersona.shots=<dir>`).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h1200dp-xhdpi")
class ChannelSourceShotsTest {
    @get:Rule val rule = createComposeRule()

    private val channels = listOf(
        HubChannel("app", "Persona", "app", "builtin"),
        HubChannel("wechat", "微信", "im", "plugin"),
        HubChannel("feishu", "飞书机器人", "im", "builtin"),
    )
    private fun message(id: String, text: String, clientMsgId: String) =
        Item(id, role = "user", kind = "text", text = text, ts = "2026-11-04T10:32:00Z", clientMsgId = clientMsgId)

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/channel-shots").also { it.mkdirs() }

    @Test fun drawsTheSourceLineInEveryStyle() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                CompositionLocalProvider(LocalChannels provides channels) {
                    Column(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background).padding(Tokens.Space.l)) {
                        MergedUserBubble(listOf(message("u1", "明天上午十点提醒我开周会", "wechat-msg-17")))
                        MergedUserBubble(listOf(message("u2", "把刚才那份周报再精简一点", "feishu-om_1")))
                        MergedUserBubble(listOf(message("u3", "这一条是在 App 里发的", "3f2a9c1d-0b4e-4c55-9a1e-6f0d2c7b8a90")))
                    }
                }
            }
        }
        rule.onNodeWithContentDescription("来自 微信").assertExists()
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            rule.waitForIdle()
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            val background = bitmap.getPixel(1, 1)
            var other = 0
            for (y in 0 until bitmap.height step 7) for (x in 0 until bitmap.width step 7) if (bitmap.getPixel(x, y) != background) other++
            assertTrue("drew nothing", other > 100)
            File(out(), "channel-chat-${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream()
                .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
    }

    // #3758: conversations an extension opened, on the real chat page as the Persona tab embeds it. The Lark /agent one opens
    // with its context blocks and the request after them; the other extension's with plain words. Each has a second message.
    private val larkAgent = Session(machineId = "hub", sessionId = "lark-1", title = "1105 | 研究 | 群里的需求汇总", assistant = true,
        origin = "lark_agent", status = "idle", state = "idle")
    private val byExtension = Session(machineId = "hub", sessionId = "ext-1", title = "1104 | 文档 | 周报草稿", assistant = true,
        origin = "extension", status = "idle", state = "idle")
    private val larkRequest = "<persona-context kind=\"instructions\" title=\"给 Agent 的说明\">\n示例用户在飞书「发布群」里用 /agent 叫你。\n</persona-context>\n" +
        "<persona-context kind=\"context\" title=\"请求的位置\">\n位置：群聊「发布群」。\n</persona-context>\n把群里这周提的需求汇总一下"
    private fun conversation(session: Session, request: String, reply: String) = listOf(
        Item("${session.sessionId}-u1", 1, role = "user", kind = "text", text = request, ts = "2026-11-05T10:30:00Z"),
        Item("${session.sessionId}-a1", 2, role = "assistant", kind = "text", text = reply, ts = "2026-11-05T10:31:00Z"),
        Item("${session.sessionId}-u2", 3, role = "user", kind = "text", text = "再按优先级排一下", ts = "2026-11-05T10:40:00Z"),
        Item("${session.sessionId}-a2", 4, role = "assistant", kind = "text", text = "排好了：发布相关的两条最先。", ts = "2026-11-05T10:41:00Z"),
    )
    private val unlisted = SessionKey("hub", "lark-2")
    private val pages = mapOf(
        larkAgent.key to conversation(larkAgent, larkRequest, "汇总好了，已经回复到群里：这周一共 5 条需求，2 条和发布有关，1 条要你拍板。"),
        unlisted to conversation(larkAgent.copy(sessionId = unlisted.sessionId), larkRequest, "汇总好了。"),
        byExtension.key to conversation(byExtension, "把这周的工作记录整理成周报草稿", "周报草稿写好了：完成 4 件，进行中 2 件，下周计划 3 件。"),
    )
    private val olderToLoad = mutableStateOf(false)
    private val chats = object : ChatRepository {
        override fun observe(session: SessionKey) = snapshotFlow {
            ChatSnapshot(items = pages[session].orEmpty(), loading = false, hasOlder = olderToLoad.value)
        }
        override suspend fun loadOlder(session: SessionKey) = Unit
        override suspend fun expandFold(session: SessionKey, fromSeq: Int, toSeq: Int): List<Item> = emptyList()
    }
    private val listed = MutableStateFlow(listOf(larkAgent, byExtension))
    private val sessions = object : SessionsRepository {
        override val machines = MutableStateFlow(emptyList<Machine>())
        override val sessions = listed
        override suspend fun refresh() = Unit
    }
    private val sender = object : MessageSender {
        override val outbox = MutableStateFlow(emptyList<OutboxMessage>())
        override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) = Unit
    }
    private val interactions = object : InteractionCenter {
        override val pending = MutableStateFlow(emptyList<Interaction>())
        override suspend fun refresh() = Unit
        override suspend fun resolve(id: String, resolution: Resolution) = Unit
    }

    /** The lines are built off the main thread: real time for them, as ChatWaitingTurnTest waits. */
    private fun waitForText(text: String) {
        val deadline = System.currentTimeMillis() + 60_000
        while (rule.onAllNodesWithText(text, substring = true, useUnmergedTree = true).fetchSemanticsNodes().isEmpty()) {
            check(System.currentTimeMillis() < deadline) { "$text never showed" }
            rule.mainClock.advanceTimeByFrame()
            Thread.sleep(20)
        }
        settle()
    }

    private fun settle() {
        Snapshot.sendApplyNotifications()
        repeat(5) { rule.mainClock.advanceTimeByFrame() }
        rule.mainClock.advanceTimeBy(300)
        rule.waitForIdle()
    }

    @Test fun saysWhereAnExternalConversationCameFrom() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var open by mutableStateOf(larkAgent.key)
        // What the Persona tab passes on (AssistantChatOptions.origin) for a conversation the live list doesn't carry.
        var passed by mutableStateOf<String?>(null)
        rule.mainClock.autoAdvance = false
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                Box(Modifier.testTag("page").width(400.dp).height(900.dp).background(PersonaTheme.palette.background)) {
                    ChatScreen(open, chats, sender, interactions, sessions, onBack = {},
                        renderStyle = ChatRenderStyle.Assistant, embedded = true, showTopBar = false, origin = passed)
                }
            }
        }
        // The merged tree: a line reads as one element (its mark and words, said once).
        fun lines(from: String) = rule.onAllNodesWithContentDescription(from).fetchSemanticsNodes().size
        for ((session, request, from) in listOf(Triple(larkAgent, "把群里这周提的需求汇总一下", "来自 飞书 /agent"),
            Triple(byExtension, "把这周的工作记录整理成周报草稿", "来自扩展"))) {
            open = session.key
            waitForText(request)
            // Under the message that opened it, not under the second one; nothing in the page's top bar (there is none here).
            assertEquals(from, 1, lines(from))
            for (s in AppStyle.entries) for (d in listOf(false, true)) {
                style = s; dark = d
                settle()
                val bitmap = rule.onNodeWithTag("page").captureToImage().asAndroidBitmap()
                File(out(), "external-chat-${session.origin}-${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream()
                    .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
            }
            style = AppStyle.Glass; dark = false
        }
        // Older items still to load: the first message here is not the one that opened it, so no line.
        open = larkAgent.key
        waitForText("把群里这周提的需求汇总一下")
        olderToLoad.value = true
        settle()
        assertEquals(0, lines("来自 飞书 /agent"))
        olderToLoad.value = false
        settle()
        assertEquals(1, lines("来自 飞书 /agent"))
        // One the live list never carried: the origin the Persona tab passed on says it; without that, nothing.
        open = unlisted
        waitForText("把群里这周提的需求汇总一下")
        assertEquals(0, lines("来自 飞书 /agent"))
        passed = "lark_agent"
        settle()
        assertEquals(1, lines("来自 飞书 /agent"))
        // The live list's origin comes first: a conversation it says the user opened says nothing.
        open = larkAgent.key
        listed.value = listOf(larkAgent.copy(origin = "user"), byExtension)
        waitForText("把群里这周提的需求汇总一下")
        assertEquals(0, lines("来自 飞书 /agent"))
    }

    // #3758: a message that came in from a channel keeps its own line even when it opened an external conversation.
    @Test fun aChannelsOwnLineWinsOverTheExternalOne() {
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                CompositionLocalProvider(LocalChannels provides channels) {
                    Column(Modifier.width(400.dp)) {
                        MergedUserBubble(listOf(message("c1", "把群里这周提的需求汇总一下", "feishu-om_2")), openedFrom = ExternalOrigin.LarkAgent)
                        MergedUserBubble(listOf(message("c2", "把这周的工作记录整理成周报草稿", "3f2a9c1d-0b4e-4c55-9a1e-6f0d2c7b8a91")),
                            openedFrom = ExternalOrigin.Extension)
                    }
                }
            }
        }
        rule.onNodeWithContentDescription("来自 飞书机器人").assertExists()
        assertEquals(0, rule.onAllNodesWithContentDescription("来自 飞书 /agent").fetchSemanticsNodes().size)
        rule.onNodeWithContentDescription("来自扩展").assertExists()
    }
}
