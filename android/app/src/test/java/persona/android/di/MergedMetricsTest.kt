package persona.android.di

import java.io.Closeable
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger
import kotlinx.coroutines.runBlocking
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import org.junit.Assert.assertEquals
import org.junit.Test
import persona.core.hub.HttpMachineMetricsApi

class MergedMetricsTest {
    @Test fun aFailedManagersHubKeepsItsNumbersWhileAnotherHubUpdates() = runBlocking {
        MetricsHub(11).use { first -> MetricsHub(22).use { second ->
            val firstCache = HttpMachineMetricsApi.Cache()
            val secondCache = HttpMachineMetricsApi.Cache()
            val groups = listOf(
                MetricsGroup(GroupNames(null), { first.api(firstCache) }, firstCache) { true },
                MetricsGroup(GroupNames("g0a1b2c3d"), { second.api(secondCache) }, secondCache) { true },
            )
            val initial = mergedMetrics(groups)
            assertEquals(listOf("same", "same~g0a1b2c3d"), initial.machines.map { it.machineId })
            val previous = initial.machines.last()
            first.cpu.set(33)
            second.refused.set(true)
            val latest = mergedMetrics(groups)
            assertEquals(listOf("same", "same~g0a1b2c3d"), latest.machines.map { it.machineId })
            assertEquals(33.0, latest.machines.first().latest!!.cpu!!, 0.0)
            assertEquals(previous, latest.machines.last())
        } }
    }
}

private class MetricsHub(cpu: Int) : Closeable {
    val cpu = AtomicInteger(cpu)
    val refused = AtomicBoolean(false)
    private val client = OkHttpClient()
    private val server = MockWebServer().apply {
        dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse = when (request.requestUrl!!.encodedPath) {
                "/api/v1/managers" -> MockResponse().setBody(
                    """{"default":"primary","managers":[{"id":"primary","name":"Primary","online":true}]}""")
                "/api/v1/metrics" -> if (refused.get()) MockResponse().setResponseCode(503) else
                    MockResponse().setBody("""{"machines":[{"machine_id":"same","online":true,"latest":{"t":1,"cpu":${this@MetricsHub.cpu.get()},"mem_used":111,"disk_used":10},"points":[]}],"step":60}""")
                else -> MockResponse().setResponseCode(404)
            }
        }
    }

    fun api(cache: HttpMachineMetricsApi.Cache) = HttpMachineMetricsApi(
        server.url("/").toString(), "fixture-token", client, cache = cache)

    override fun close() {
        server.close()
        client.dispatcher.executorService.shutdownNow()
        client.connectionPool.evictAll()
    }
}
