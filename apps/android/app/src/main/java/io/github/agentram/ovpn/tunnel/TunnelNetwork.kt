package io.github.agentram.ovpn.tunnel

import android.net.ConnectivityManager
import android.net.DnsResolver
import android.net.InetAddresses
import android.net.Network
import android.net.NetworkCapabilities
import android.os.CancellationSignal
import kotlinx.coroutines.delay
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withTimeoutOrNull
import okhttp3.Call
import okhttp3.Callback
import okhttp3.Dns
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import java.io.IOException
import java.net.InetAddress
import java.net.Proxy
import java.util.concurrent.Executor
import java.util.concurrent.TimeUnit
import kotlin.coroutines.resume

internal suspend fun resolveEndpoint(network: Network, host: String): List<String> {
    if (InetAddresses.isNumericAddress(host)) return listOf(host)
    return withTimeoutOrNull(6000) {
        suspendCancellableCoroutine { result ->
            val cancel = CancellationSignal()
            result.invokeOnCancellation { cancel.cancel() }
            DnsResolver.getInstance().query(
                network, host, DnsResolver.FLAG_EMPTY, Executor { it.run() }, cancel,
                object : DnsResolver.Callback<List<InetAddress>> {
                    override fun onAnswer(answer: List<InetAddress>, rcode: Int) {
                        result.resume(
                            answer.filterNot { it.isAnyLocalAddress || it.isMulticastAddress || it.isLinkLocalAddress }
                                .mapNotNull { it.hostAddress }.distinct(),
                        )
                    }

                    override fun onError(error: DnsResolver.DnsException) = result.resume(emptyList())
                },
            )
        }
    } ?: emptyList()
}

internal suspend fun vpnNetwork(cm: ConnectivityManager, interfaceName: String): Network? = withTimeoutOrNull(3000) {
    while (true) {
        @Suppress("DEPRECATION") // Select our TUN by its actual interface, never by the default route.
        val network = cm.allNetworks.firstOrNull {
            cm.getNetworkCapabilities(it)?.hasTransport(NetworkCapabilities.TRANSPORT_VPN) == true &&
                cm.getLinkProperties(it)?.interfaceName == interfaceName
        }
        if (network != null) return@withTimeoutOrNull network
        delay(100)
    }
    @Suppress("UNREACHABLE_CODE")
    null
}

internal suspend fun checkTunnel(vpn: Network): Boolean = suspendCancellableCoroutine { result ->
    // This client has its own connection pool and VPN-bound DNS and sockets.
    // A vanished VPN produces an error instead of falling back to Android's default network.
    val http = vpnHttpBuilder(vpn)
        .proxy(Proxy.NO_PROXY)
        .followRedirects(false)
        .followSslRedirects(false)
        .retryOnConnectionFailure(false)
        .callTimeout(8, TimeUnit.SECONDS)
        .build()
    val call = http.newCall(
        Request.Builder().url("https://www.gstatic.com/generate_204")
            .header("Cache-Control", "no-store").build(),
    )
    result.invokeOnCancellation { call.cancel() }
    call.enqueue(object : Callback {
        override fun onFailure(call: Call, e: IOException) = result.resume(false)
        override fun onResponse(call: Call, response: Response) {
            val verified = response.use { it.code == 204 }
            http.connectionPool.evictAll()
            result.resume(verified)
        }
    })
}


internal fun vpnHttpBuilder(vpn: Network): OkHttpClient.Builder = OkHttpClient.Builder()
    .socketFactory(vpn.socketFactory)
    .dns(object : Dns {
        override fun lookup(hostname: String) = vpn.getAllByName(hostname).toList()
    })
