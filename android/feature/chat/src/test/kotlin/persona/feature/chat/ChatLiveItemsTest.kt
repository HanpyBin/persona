package persona.feature.chat

import kotlinx.coroutines.flow.MutableStateFlow
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.Item

class ChatLiveItemsTest {
    private class CountingList(private val items: List<Item>) : AbstractList<Item>() {
        var reads = 0
        override val size: Int get() = items.size
        override fun get(index: Int): Item = items[index].also { reads++ }
    }

    @Test fun anOrdinaryDeltaChangesOnlyItsItemWithoutReadingOrCopyingHistory() {
        val history = (1..200).map { Item("old-$it", it, role = "assistant", kind = "text", text = "settled $it") }
        val reply = Item("reply", 201, role = "assistant", kind = "text", text = "first")
        val base = CountingList(history + reply)
        val holder = MutableStateFlow("first")
        val live = ChatLiveItems()
        live.reset(base, mapOf("reply" to holder))
        val readsAfterReset = base.reads

        live.update("reply", holder, "first and a new chunk")

        assertEquals("a live update must not iterate the base list", readsAfterReset, base.reads)
        val revision = live.revisions.value
        assertFalse(revision.structural)
        assertTrue(revision.lastTextOnly)
        assertEquals(setOf("reply"), revision.changed.keys)
        assertSame(history.first(), revision.items.first())
        assertSame(history.last(), revision.items[199])
        assertEquals("first and a new chunk", revision.items.last().text)
        assertEquals("first and a new chunk", live.current().last().text)
        assertTrue("materializing for a user action reads the whole list", base.reads > readsAfterReset)
    }

    @Test fun aFrozenWindowUsesCurrentTextWithoutChangingItsMembers() {
        val old = Item("old", 1, role = "assistant", kind = "text", text = "settled")
        val reply = Item("reply", 2, role = "assistant", kind = "text", text = "before search")
        val holder = MutableStateFlow("before search")
        val live = ChatLiveItems()
        live.reset(listOf(old, reply), mapOf("reply" to holder))
        val frozen = listOf(old, reply)
        live.update("reply", holder, "a URL arrived https://example.com/stream")

        val overlaid = live.revisions.value.overlay(frozen)
        assertSame(old, overlaid.first())
        assertEquals("a URL arrived https://example.com/stream", overlaid.last().text)
        assertEquals(listOf("old", "reply"), overlaid.map { it.id })
        assertEquals(listOf("old", "reply"), live.revisions.value.base.map { it.id })
    }

    @Test fun staleAndDuplicateHolderUpdatesCannotOverwriteTheCurrentRevision() {
        val item = Item("reply", 1, role = "assistant", kind = "text", text = "current")
        val oldHolder = MutableStateFlow("old")
        val holder = MutableStateFlow("current")
        val live = ChatLiveItems()
        live.reset(listOf(item), mapOf("reply" to oldHolder))
        live.reset(listOf(item), mapOf("reply" to holder))
        val revision = live.revisions.value

        live.update("reply", oldHolder, "stale")
        assertSame(revision, live.revisions.value)
        live.update("reply", holder, "current")
        assertSame(revision, live.revisions.value)
        live.update("reply", holder, "next")
        assertEquals("next", live.revisions.value.items.single().text)
    }
}
