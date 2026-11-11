package persona.feature.chat

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.chatlist.ChatReadingPosition
import persona.core.contracts.Item
import persona.core.contracts.SessionKey
import persona.core.contracts.api.ChatRepository
import persona.core.contracts.api.ChatSnapshot
import persona.core.domain.lines.chatLines
import persona.core.domain.lines.LineOptions
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.Json
import org.junit.Assert.*
import org.junit.Test

@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class ChatSearchTest {
    private val res get() = ApplicationProvider.getApplicationContext<Context>().resources
    @Test fun searchesNestedStringsAndAllReadingCategoriesWithLiteralHighlightsAndLimit() = runTest {
        val items = listOf(
            Item("text", kind = "text", text = "正文 Needle"),
            Item("thought", kind = "thinking", text = "思考 needle"),
            Item("call", kind = "tool_use", toolName = "NeedleTool", toolInput =
                Json.parseToJsonElement("""{"nested":[{"value":"前文 NEEdle 后文"}],"number":123,"flag":true}""")),
            Item("result", kind = "tool_result", text = "输出 needle"),
            Item("notice", kind = "notice", text = "事件 needle"),
        )
        val results = searchChat(res, items, " needle ")
        assertEquals(setOf("正文", "思考", "工具名", "工具输入", "工具输出", "系统事件"), results.hits.map { it.category }.toSet())
        results.hits.forEach { hit -> hit.ranges.forEach { range -> assertEquals("needle", hit.snippet.substring(range).lowercase()) } }
        assertTrue(searchChat(res, items, "123").hits.isEmpty())
        assertTrue(searchChat(res, items, "true").hits.isEmpty())
        assertTrue(searchChat(res, items, " ").hits.isEmpty())
        val literal = searchChat(res, listOf(Item("literal", kind = "text", text = "😀前文 a.* a.*")), "a.*").hits.single()
        assertEquals(2, literal.ranges.size)
        val capped = searchChat(res, (1..101).map { Item("item-$it", it, kind = "text", text = "needle") }, "needle")
        assertEquals(100, capped.hits.size)
        assertTrue(capped.truncated)
        assertEquals("item-101", capped.hits.first().itemId)
        assertFalse(searchChat(res, (1..100).map { Item("item-$it", it, kind = "text", text = "needle") }, "needle").truncated)
    }

    @Test fun resolvesFoldedTargetsAndLoadsOnlyContinuousBoundedPagesWithoutLosingReturnAnchor() = runTest {
        val items = listOf(
            Item("user", 30, role = "user", kind = "text", text = "开始"),
            Item("call", 31, kind = "tool_use", toolUseId = "tool", toolName = "Read"),
            Item("result", 32, kind = "tool_result", toolUseId = "tool", text = "结果"),
            Item("reply", 33, role = "assistant", kind = "text", text = "完成"),
        )
        val lines = chatLines(items.map(Item::asLineItem)).lines
        val target = searchTarget(lines, "result")!!
        assertEquals(setOf("w-user"), target.expand)
        val opened = chatLines(items.map(Item::asLineItem), LineOptions(openWork = target.expand)).lines
        assertEquals(target.rowId, searchTarget(opened, "result")?.rowId)
        assertTrue(opened.any { it.id == target.rowId })
        val state = ChatSearchState()
        val origin = ChatReadingPosition("reply", -75, false, setOf("existing"))
        state.origin = origin
        state.choose(ChatSearchHit("result", "工具输出", "结果", emptyList()))
        state.choose(ChatSearchHit("user", "正文", "开始", emptyList()))
        assertEquals(origin, state.origin)
        val key = SessionKey("machine", "session")
        val snapshot = MutableStateFlow(ChatSnapshot(items = items, loading = false, hasOlder = true))
        var calls = 0
        var fail = false
        val repository = object : ChatRepository {
            override fun observe(session: SessionKey) = snapshot
            override suspend fun expandFold(session: SessionKey, fromSeq: Int, toSeq: Int) = emptyList<Item>()
            override suspend fun loadOlder(session: SessionKey) {
                calls++
                if (fail) { snapshot.value = snapshot.value.copy(error = "失败"); return }
                val seq = snapshot.value.items.minOf { it.seq } - 1
                snapshot.value = snapshot.value.copy(items = listOf(Item("older-$seq", seq, kind = "text")) + snapshot.value.items)
            }
        }
        val pages = mutableListOf<Int>()
        assertNotNull(loadEarlierForSearch(res, repository, key, pages::add))
        assertEquals((1..20).toList(), pages)
        assertEquals(20, calls)
        assertEquals(10, snapshot.value.items.minOf { it.seq })
        fail = true
        assertNotNull(loadEarlierForSearch(res, repository, key) { org.junit.Assert.fail("Failed page must not advance progress") })
        assertEquals(21, calls)
    }

    @Test fun liveChunksUpdateOnlyTheirFrozenWindowHitsAndMatchACompleteSearch() = runTest {
        val old = Item("old", 1, role = "assistant", kind = "text", text = "Settled needle text")
        val reply = Item("reply", 2, role = "assistant", kind = "text", text = "No match yet")
        val frozen = listOf(old, reply)
        val ordered = orderedChatSearchItems(frozen)
        val order = ordered.mapIndexed { index, item -> item.id to index }.toMap()
        val before = searchChatInOrder(res, ordered, "needle")
        val oldHit = before.hits.single()

        val first = reply.copy(text = "A chunk now contains needle")
        val firstResult = searchChatDelta(res, listOf(old, first), order, before, mapOf(first.id to first), "needle")
        assertEquals(searchChat(res, listOf(old, first), "needle"), firstResult)
        assertSame(oldHit, firstResult.hits.single { it.itemId == old.id })
        assertEquals("reply", firstResult.hits.first().itemId)

        val second = reply.copy(text = "A later needle chunk changes the snippet and its highlights")
        val secondResult = searchChatDelta(res, listOf(old, second), order, firstResult, mapOf(second.id to second), "needle")
        assertEquals(searchChat(res, listOf(old, second), "needle"), secondResult)
        assertSame(oldHit, secondResult.hits.single { it.itemId == old.id })
        secondResult.hits.single { it.itemId == reply.id }.ranges.forEach {
            assertEquals("needle", secondResult.hits.single { hit -> hit.itemId == reply.id }.snippet.substring(it))
        }

        // A structural item outside the frozen search window cannot add itself to the existing results.
        val outside = Item("new", 3, role = "assistant", kind = "text", text = "needle outside")
        assertSame(secondResult, searchChatDelta(res, listOf(old, second, outside), order, secondResult,
            mapOf(outside.id to outside), "needle"))
    }

    @Test fun aTruncatedSearchIsRebuiltWhenALiveChangeCanRevealTheNextHit() = runTest {
        val original = (1..101).map { Item("item-$it", it, role = "assistant", kind = "text", text = "needle") }
        val ordered = orderedChatSearchItems(original)
        val order = ordered.mapIndexed { index, item -> item.id to index }.toMap()
        val before = searchChatInOrder(res, ordered, "needle")
        assertTrue(before.truncated)
        assertEquals(100, before.hits.size)
        assertFalse(before.hits.any { it.itemId == "item-1" })

        val changed = original.last().copy(text = "the newest reply no longer matches")
        val current = original.dropLast(1) + changed
        val after = searchChatDelta(res, current, order, before, mapOf(changed.id to changed), "needle")

        assertEquals(searchChat(res, current, "needle"), after)
        assertEquals(100, after.hits.size)
        assertFalse(after.truncated)
        assertTrue(after.hits.any { it.itemId == "item-1" })
    }
}
