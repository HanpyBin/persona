package persona.feature.chat

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.Item
import kotlin.random.Random

/** markdownPrewarm as it was before it kept its split from one call to the next (0453b49bc). */
internal fun splitAfresh(items: List<Item>, limit: Int = 80): Pair<List<MarkdownText>, List<MarkdownText>> {
    val replies = items.filter { it.kind == "text" && it.role == "assistant" }.takeLast(limit)
    val lastUser = items.lastOrNull { it.role == "user" && it.kind == "text" }
    fun texts(item: Item) = messageParts(item.text.orEmpty().let { if (item.fullSize != null) cutText(it) else it }, item.from)
        .mapIndexedNotNull { index, part -> (part as? MessagePart.Text)?.let { MarkdownText("${item.id}/$index", it.text) } }
    val (latest, earlier) = replies.partition { lastUser == null || chatItemOrder.compare(it, lastUser) > 0 }
    return latest.flatMap(::texts) to earlier.asReversed().flatMap(::texts)
}

class MarkdownPrewarmTest {
    private val texts = listOf("## Done", "", "   ", "a\n\nb", "before <cross-session-message from-name=\"x\">theirs</cross-session-message> after",
        "<cross-session-message from-name='y'>only theirs</cross-session-message>", "- one\n- two\n…（已截断，共 9000 字节）")

    private fun item(r: Random, n: Int) = Item(
        id = "i$n#${r.nextInt(3)}", seq = r.nextInt(0, 40),
        role = listOf("assistant", "assistant", "user", "system").random(r),
        kind = listOf("text", "text", "text", "tool_use", "thinking").random(r),
        text = texts.random(r).takeIf { r.nextInt(8) > 0 }, fullSize = if (r.nextInt(5) == 0) 9000 else null,
        from = if (r.nextInt(6) == 0) "other" else null,
    )

    @Test fun keptSplitsMatchSplittingAfreshThroughStreamingAndOtherChanges() {
        for (seed in 1..60) {
            val r = Random(seed)
            val limit = if (seed % 3 == 0) r.nextInt(0, 4) else 80
            val prewarm = MarkdownPrewarm(limit)
            var next = 0
            var items = List(r.nextInt(0, 12)) { item(r, next++) }
            var split: Pair<List<MarkdownText>, List<MarkdownText>>? = null
            repeat(80) { step ->
                val change = r.nextInt(10)
                items = when (change) {
                    // The last item streams on: mostly what happens.
                    0, 1, 2, 3 -> items.dropLast(1) + listOfNotNull(items.lastOrNull()?.let { it.copy(text = it.text.orEmpty() + texts.random(r)) })
                    4 -> items + item(r, next++)
                    5 -> if (items.isEmpty()) items else items.toMutableList().apply { removeAt(r.nextInt(size)) }
                    6 -> items.shuffled(r)
                    // The last item becomes another row under the same id, or someone else's.
                    7 -> items.dropLast(1) + listOfNotNull(items.lastOrNull()?.let {
                        it.copy(role = listOf("user", "assistant").random(r), kind = "text", seq = it.seq + r.nextInt(2))
                    })
                    8 -> items.mapIndexed { i, it -> if (i == r.nextInt(items.size)) it.copy(text = "changed") else it }
                    else -> items.map { it.copy() }
                }
                val before = split
                split = prewarm.split(items)
                assertEquals("seed $seed, step $step", splitAfresh(items, limit), split)
                // Only the streaming item is split again: every other reply's parts are the ones the last split made.
                if (change < 4 && before != null && items.isNotEmpty()) {
                    val streamed = "${items.last().id}/"
                    fun others(parts: Pair<List<MarkdownText>, List<MarkdownText>>) = (parts.first + parts.second).filterNot { it.key.startsWith(streamed) }
                    val (was, now) = others(before) to others(split)
                    assertEquals("seed $seed, step $step", was.size, now.size)
                    was.indices.forEach { assertSame("seed $seed, step $step", was[it], now[it]) }
                }
            }
        }
    }

    @Test fun splitLastMatchesAFreshSplitAndRetainsEveryOtherReplyPart() {
        var items = (1..30).map { n ->
            Item(if (n % 2 == 0) "reply-$n" else "user-$n", n, role = if (n % 2 == 0) "assistant" else "user",
                kind = "text", text = if (n % 2 == 0) "Settled **reply** $n" else "Question $n")
        }
        val prewarm = MarkdownPrewarm()
        var previous = prewarm.split(items)
        repeat(30) { n ->
            val streamed = items.last()
            items = items.dropLast(1) + streamed.copy(text = texts[n % texts.size] + "\n\nchunk $n")
            val next = prewarm.splitLast(items)
            assertEquals(splitAfresh(items), next)
            val prefix = "${streamed.id}/"
            val before = (previous.first + previous.second).filterNot { it.key.startsWith(prefix) }
            val after = (next.first + next.second).filterNot { it.key.startsWith(prefix) }
            assertEquals(before.size, after.size)
            before.indices.forEach { assertSame(before[it], after[it]) }
            previous = next
        }

        // The fast path is only for the same last row. A structural change falls back to the general split.
        items = items.dropLast(1) + items.last().copy(role = "user")
        assertEquals(splitAfresh(items), prewarm.splitLast(items))
    }

    @Test fun conflatedStructuralResetAndLiveDeltaCannotReuseTheOldPrefix() {
        val old = Item("old", 1, role = "assistant", kind = "text", text = "Old **prefix**")
        val reply = Item("reply", 2, role = "assistant", kind = "text", text = "First")
        val holder = kotlinx.coroutines.flow.MutableStateFlow("First")
        val live = ChatLiveItems()
        live.reset(listOf(old, reply), mapOf(reply.id to holder))
        val prewarm = MarkdownPrewarm()
        prewarm.split(live.revisions.value)

        // A StateFlow collector can miss this reset and observe only the immediately following holder update.
        live.reset(listOf(old.copy(text = "Structurally changed **prefix**"), reply.copy(text = "Reset")),
            mapOf(reply.id to holder))
        live.update(reply.id, holder, "Streamed")
        val revision = live.revisions.value
        assertFalse(revision.structural)
        assertTrue(revision.lastTextOnly)

        assertEquals(splitAfresh(revision.items), prewarm.split(revision))
    }
}
