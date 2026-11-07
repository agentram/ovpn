package io.github.agentram.ovpn.tunnel

import kotlinx.coroutines.suspendCancellableCoroutine
import okhttp3.Call
import okhttp3.Callback
import okhttp3.ConnectionPool
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import java.io.IOException
import java.net.Proxy
import java.net.URI
import java.nio.ByteBuffer
import java.nio.charset.CodingErrorAction
import java.util.concurrent.TimeUnit
import javax.net.ssl.SSLException
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException

internal fun subscriptionUrl(input: String): HttpUrl? {
    if (input.length > 8192) return null
    val uri = runCatching { URI(input) }.getOrNull() ?: return null
    if (!uri.scheme.equals("https", ignoreCase = true) || uri.host == null ||
        uri.rawUserInfo != null || uri.rawFragment != null || uri.port !in -1..65535 || uri.port == 0
    ) return null
    return input.toHttpUrlOrNull()
}

internal class SubscriptionClient(transport: OkHttpClient = OkHttpClient()) {
    private val http = transport.newBuilder()
        .followRedirects(false)
        .followSslRedirects(false)
        .retryOnConnectionFailure(false)
        .proxy(Proxy.NO_PROXY)
        .cache(null)
        .connectionPool(ConnectionPool(0, 1, TimeUnit.SECONDS))
        .callTimeout(15, TimeUnit.SECONDS)
        .build()

    suspend fun fetch(url: HttpUrl): String = suspendCancellableCoroutine { result ->
        val request = Request.Builder().url(url)
            .header("Cache-Control", "no-store")
            .header("Accept", "text/plain")
            .build()
        val call = http.newCall(request)
        result.invokeOnCancellation { call.cancel() }
        call.enqueue(object : Callback {
            override fun onFailure(call: Call, e: IOException) {
                result.resumeWithException(TunnelIssue(if (e is SSLException) "subscription_tls" else "subscription_network"))
            }

            override fun onResponse(call: Call, response: Response) {
                val payload = try {
                    response.use(::readSubscription)
                } catch (e: TunnelIssue) {
                    result.resumeWithException(e)
                    return
                } catch (_: IOException) {
                    result.resumeWithException(TunnelIssue("subscription_network"))
                    return
                }
                result.resume(payload)
            }
        })
    }
}

private fun readSubscription(response: Response): String {
    when (response.code) {
        200 -> Unit
        404, 410 -> throw TunnelIssue("subscription_unavailable")
        in 300..399 -> throw TunnelIssue("subscription_redirect")
        else -> throw TunnelIssue("subscription_http")
    }
    val body = response.body ?: throw TunnelIssue("invalid_import")
    val limit = 1024L * 1024L
    if (body.contentLength() > limit || body.source().request(limit + 1)) throw TunnelIssue("too_large")
    return try {
        Charsets.UTF_8.newDecoder().onMalformedInput(CodingErrorAction.REPORT)
            .decode(ByteBuffer.wrap(body.source().readByteArray())).toString()
    } catch (_: java.nio.charset.CharacterCodingException) {
        throw TunnelIssue("invalid_import")
    }
}
