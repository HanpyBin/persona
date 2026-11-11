package persona.core.chatlist

import org.junit.Assert.*
import org.junit.Test

class ChatListTest {
    private data class Message(val id: String, val seq: Int, val text: String = "原文", val incoming: Boolean = true)

    @Test fun frozenWindowAdmitsPagesAndInteriorHolesButDefersNewIncomingMessages() {
        val window = ChatListWindow<Message>({ it.id }, compareBy { it.seq }, { it.incoming })
        val first = Message("first", 10)
        val last = Message("last", 30)
        window.freeze(listOf(first, last))
        val old = Message("old", 1)
        val hole = Message("hole", 20)
        val new = Message("new", 31)
        val own = Message("own", 32, incoming = false)
        val live = listOf(old, first.copy(text = "修改"), hole, last, new, new, own)
        assertEquals(listOf(old, first, hole, last), window.items(live))
        assertEquals(1, window.newMessageCount(live))
        assertEquals(2, window.newMessageCount(live, below = hole))
        assertEquals(1, window.newMessageCount(live, below = hole, lastRead = last))
        window.freeze(live)
        assertFalse(window.items(live).any { it.id == "new" })
        assertTrue(window.items(listOf(last)).any { it.id == "hole" })
        window.follow()
        assertEquals(live, window.items(live))
        assertEquals(0, window.newMessageCount(live))
    }

    @Test fun aStreamingTurnHoldsTheFrozenCountWithoutRescanningThenSettlesExactly() {
        var reads = 0
        val window = ChatListWindow<Message>({ it.id }, compareBy { it.seq }, { reads++; it.incoming })
        val first = Message("first", 10)
        val last = Message("last", 30)
        window.freeze(listOf(first, last))
        assertEquals(0, window.newMessageCount(listOf(first, last), defer = true))
        val before = reads

        val older = Message("older", 1)
        val streamed = Message("streamed", 31, text = "first chunk")
        val streaming = listOf(older, first, last, streamed)
        assertEquals(0, window.newMessageCount(streaming, below = older, defer = true))
        assertEquals(0, window.newMessageCount(listOf(older, first, last, streamed.copy(text = "a later chunk")), defer = true))
        assertEquals(before, reads)

        val own = Message("own", 32, incoming = false)
        val another = Message("another", 33)
        val settled = listOf(older, first, last, streamed.copy(text = "complete"), own, another, another)
        assertEquals(2, window.newMessageCount(settled, below = last))
        assertEquals(1, window.newMessageCount(settled, below = last, lastRead = streamed))
        window.follow()
        assertEquals(0, window.newMessageCount(settled, defer = true))
    }

    @Test fun prefetchRequiresUserIntentWhileLastLineUsesMeasuredExtent() {
        assertFalse(shouldPrefetch(false, true, false, 95, 100, 5))
        assertFalse(shouldPrefetch(true, true, true, 95, 100, 5))
        assertFalse(shouldPrefetch(true, false, false, 95, 100, 5))
        assertFalse(shouldPrefetch(true, true, false, 93, 100, 5))
        assertTrue(shouldPrefetch(true, true, false, 94, 100, 5))
        assertEquals(-672, lastLineScrollOffset(700, 28))
        assertEquals(0, lastLineScrollOffset(20, 28))
        // Under a 100px top bar the last line sits right below it.
        assertEquals(-572, lastLineScrollOffset(700, 28, topPadding = 100))
        assertEquals(0, lastLineScrollOffset(120, 28, topPadding = 100))
    }
}
