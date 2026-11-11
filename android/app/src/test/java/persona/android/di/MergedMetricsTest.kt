package persona.android.di

import com.sun.net.httpserver.HttpExchange
import com.sun.net.httpserver.HttpServer
import java.io.Closeable
import java.net.InetSocketAddress
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger
import kotlinx.coroutines.runBlocking
import okhttp3.OkHttpClient
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
    private val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0).apply {
        createContext("/") { exchange ->
            when (exchange.requestURI.path) {
                "/api/v1/managers" -> reply(exchange, 200,
                    """{"default":"primary","managers":[{"id":"primary","name":"Primary","online":true}]}""")
                "/api/v1/metrics" -> if (refused.get()) reply(exchange, 503, """{"error":"Unavailable"}""") else
                    reply(exchange, 200, """{"machines":[{"machine_id":"same","online":true,"latest":{"t":1,"cpu":${this@MetricsHub.cpu.get()},"mem_used":111,"disk_used":10},"points":[]}],"step":60}""")
                else -> reply(exchange, 404, """{"error":"Not found"}""")
            }
        }
        start()
    }

    fun api(cache: HttpMachineMetricsApi.Cache) = HttpMachineMetricsApi(
        "http://127.0.0.1:${server.address.port}/", "fixture-token", client, cache = cache)

    private fun reply(exchange: HttpExchange, status: Int, body: String) {
        val bytes = body.toByteArray(Charsets.UTF_8)
        exchange.responseHeaders.set("Content-Type", "application/json")
        exchange.sendResponseHeaders(status, bytes.size.toLong())
        exchange.responseBody.use { it.write(bytes) }
    }

    override fun close() {
        server.stop(0)
        client.dispatcher.executorService.shutdownNow()
        client.connectionPool.evictAll()
    }
}
