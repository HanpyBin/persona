package persona.core.contracts.api

import kotlinx.coroutines.flow.flowOf
import org.junit.Assert.assertSame
import org.junit.Test
import persona.core.contracts.Item
import persona.core.contracts.SessionKey

class ChatRepositoryTest {
    @Test fun structuralObservationDefaultsToTheCompleteTextObserver() {
        val snapshot = ChatSnapshot(items = listOf(Item("reply", 1, role = "assistant", kind = "text", text = "chunk")))
        val complete = flowOf(snapshot)
        val repository = object : ChatRepository {
            override fun observe(session: SessionKey) = complete
            override suspend fun loadOlder(session: SessionKey) = Unit
            override suspend fun expandFold(session: SessionKey, fromSeq: Int, toSeq: Int) = emptyList<Item>()
        }

        assertSame(complete, repository.observeStructure(SessionKey("machine", "session")))
    }
}
