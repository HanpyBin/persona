package persona.core.hub

import java.util.concurrent.LinkedBlockingQueue
import kotlinx.coroutines.runBlocking
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import org.junit.Assert.assertEquals
import org.junit.Test

class MachineMetricsManagersTest {
    @Test fun dashboardReadsBothOnlineManagersWithoutAnUnscopedDuplicate() = runBlocking {
        val requests = LinkedBlockingQueue<RecordedRequest>()
        MockWebServer().use { server ->
            server.dispatcher = object : Dispatcher() {
                override fun dispatch(request: RecordedRequest): MockResponse {
                    if (request.path == "/" && request.getHeader("User-Agent") in setOf("jarvis-ports", "persona-ports")) {
                        return MockResponse().setResponseCode(404)
                    }
                    requests.add(request)
                    return when (request.requestUrl!!.encodedPath) {
                        "/api/v1/managers" -> MockResponse().setBody("""{"default":"primary","managers":[
                            {"id":"primary","online":true,"default":true},
                            {"id":"secondary","online":true},
                            {"id":"offline","online":false},
                            {"id":"installing","online":true,"installing":"pending"}]}""")
                        "/api/v1/metrics" -> when (request.requestUrl!!.queryParameter("manager")) {
                            null, "primary" -> MockResponse().setBody("""{"machines":[{"machine_id":"first","online":true,"latest":{"t":1,"cpu":11,"mem_used":111,"disk_used":10}}]}""")
                            "secondary" -> MockResponse().setBody("""{"machines":[{"machine_id":"second","online":true,"latest":{"t":2,"cpu":22,"mem_used":222,"disk_used":20}}]}""")
                            else -> MockResponse().setResponseCode(500)
                        }
                        else -> MockResponse().setResponseCode(404)
                    }
                }
            }
            val api = HttpMachineMetricsApi(server.url("/").toString(), "fixture-token", OkHttpClient())
            val dashboard = api.dashboard()
            assertEquals(listOf("first", "second"), dashboard.machines.map { it.machineId })
            assertEquals(listOf(11.0, 22.0), dashboard.machines.map { it.latest?.cpu })
            assertEquals(listOf(111L, 222L), dashboard.machines.map { it.latest?.memUsed })
            assertEquals(listOf(10L, 20L), dashboard.machines.map { it.latest?.diskUsed })
            val calls = requests.toList()
            assertEquals(3, calls.size)
            assertEquals("/api/v1/managers", calls.first().path)
            assertEquals(setOf("/api/v1/metrics?manager=primary", "/api/v1/metrics?manager=secondary"),
                calls.drop(1).map { it.path }.toSet())
            assertEquals(setOf("Bearer fixture-token"), calls.map { it.getHeader("Authorization") }.toSet())
            assertEquals(setOf("GET"), calls.map { it.method }.toSet())
        }
    }
}
