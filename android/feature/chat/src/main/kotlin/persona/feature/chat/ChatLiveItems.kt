package persona.feature.chat

import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.repeatOnLifecycle
import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.launch
import persona.core.contracts.Item

/**
 * Complete-text consumers' view of a structural chat snapshot. A holder update changes one value, not the base list;
 * [Revision.items] applies those values only when an item is read. The page itself must not collect [revisions].
 */
internal class ChatLiveItems {
    internal class Revision internal constructor(
        val base: List<Item>,
        val values: Map<String, String>,
        val changed: Map<String, Item>,
        val structural: Boolean,
        val lastItemId: String?,
    ) {
        val items: List<Item> = overlay(base)
        /** True only when the revision proves that the last item's text, and nothing else's structure, changed. */
        val lastTextOnly: Boolean = !structural && changed.size == 1 && lastItemId == changed.keys.single()

        fun overlay(items: List<Item>): List<Item> = if (values.isEmpty()) items else LiveItemList(items, values)
        fun current(): List<Item> = if (values.isEmpty()) base else items.toList()
    }

    private class LiveItemList(
        private val base: List<Item>,
        private val values: Map<String, String>,
    ) : AbstractList<Item>() {
        override val size: Int get() = base.size
        override fun get(index: Int): Item {
            val item = base[index]
            val text = values[item.id] ?: return item
            return if (item.text.orEmpty() == text) item else item.copy(text = text)
        }
    }

    private var byId = emptyMap<String, Item>()
    private var holders = emptyMap<String, StateFlow<String>>()
    private val mutableRevisions = MutableStateFlow(Revision(emptyList(), emptyMap(), emptyMap(), structural = true, lastItemId = null))
    val revisions: StateFlow<Revision> = mutableRevisions

    /** A structural snapshot is the only operation that indexes the conversation. */
    fun reset(items: List<Item>, liveText: Map<String, StateFlow<String>>) {
        byId = items.associateBy { it.id }
        holders = liveText
        mutableRevisions.value = Revision(items, liveText.mapValues { it.value.value }, emptyMap(), structural = true,
            lastItemId = items.lastOrNull()?.id)
    }

    /** Ignore a cancelled collector from an older holder for the same row. */
    fun update(id: String, holder: StateFlow<String>, text: String) {
        if (holders[id] !== holder) return
        val item = byId[id] ?: return
        val previous = mutableRevisions.value
        if (previous.values[id] == text) return
        mutableRevisions.value = Revision(previous.base, previous.values + (id to text),
            mapOf(id to item.copy(text = text)), structural = false, lastItemId = previous.lastItemId)
    }

    /** User actions such as opening search or freezing the timeline read the current text on demand. */
    fun current(): List<Item> = mutableRevisions.value.current()
}

@Composable
internal fun rememberChatLiveItems(
    items: List<Item>,
    liveText: Map<String, StateFlow<String>>,
): ChatLiveItems {
    val live = remember { ChatLiveItems() }
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    LaunchedEffect(live, lifecycle, items, liveText) {
        live.reset(items, liveText)
        lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            coroutineScope {
                liveText.forEach { (id, holder) ->
                    launch { holder.collect { live.update(id, holder, it) } }
                }
                awaitCancellation()
            }
        }
    }
    return live
}
