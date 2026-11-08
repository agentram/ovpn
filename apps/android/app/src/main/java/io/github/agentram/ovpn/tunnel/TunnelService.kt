package io.github.agentram.ovpn.tunnel

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.net.VpnService
import android.os.Build
import android.os.ParcelFileDescriptor
import android.os.SystemClock
import androidx.core.app.ServiceCompat
import io.github.agentram.ovpn.androidbridge.Androidbridge
import io.github.agentram.ovpn.androidbridge.SocketProtector
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.time.Instant
import java.util.concurrent.atomic.AtomicBoolean

internal enum class Phase { Idle, Connecting, Verifying, Connected, Unverified, Reconnecting, Switching, Error }

internal data class TunnelState(
    val phase: Phase = Phase.Idle,
    val profileId: String? = null,
    val issue: String? = null,
    val checkPassed: Boolean? = null,
    val checkedAt: Long? = null,
    val checkedProfile: String? = null,
    val vpnInterface: String = "",
    val transport: ReportTransport = ReportTransport.Unknown,
    val network: ConnectionNetwork = ConnectionNetwork.Unknown,
)

internal object TunnelRuntime {
    const val CHECK_INTERVAL = 120_000L
    private val mutable = MutableStateFlow(TunnelState())
    val state = mutable.asStateFlow()
    private val reports = MutableStateFlow<List<ReportEvent>>(emptyList())
    val events = reports.asStateFlow()

    fun publish(state: TunnelState) {
        reports.value = reportEvents(reports.value, Instant.now(), state.phase, state.network, state.transport, state.issue)
        mutable.value = state
    }
}

class TunnelService : VpnService() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private val commands = Channel<Command>(Channel.UNLIMITED)
    private val cm by lazy { getSystemService(ConnectivityManager::class.java) }
    private val notifications by lazy { getSystemService(NotificationManager::class.java) }
    private val networks = mutableMapOf<Network, NetworkCapabilities>()
    private var selectedNetwork: Network? = null
    private var active: Profile? = null
    private var tun: ParcelFileDescriptor? = null
    private var interfaceName = ""
    private var worker: Job? = null
    private val nativeLock = Any()
    private var destroyed = false
    private var observing = false
    private var foreground = false
    private var lastStartId = 0

    private sealed interface Command {
        data class Start(val id: String) : Command
        data class Stop(val issue: String? = null) : Command
        data class Capabilities(val network: Network, val caps: NetworkCapabilities?) : Command
        data object LibraryChanged : Command
        data object Retry : Command
    }

    private val networkCallback = object : ConnectivityManager.NetworkCallback() {
        override fun onCapabilitiesChanged(network: Network, caps: NetworkCapabilities) {
            commands.trySend(Command.Capabilities(network, caps))
        }

        override fun onLost(network: Network) { commands.trySend(Command.Capabilities(network, null)) }
    }

    override fun onCreate() {
        super.onCreate()
        scope.launch {
            profiles.state.collect { commands.trySend(Command.LibraryChanged) }
        }
        scope.launch {
            for (command in commands) {
                try {
                    when (command) {
                        is Command.Start -> connect(command.id)
                        is Command.Stop -> disconnect(command.issue)
                        is Command.Capabilities -> networkChanged(command)
                        Command.LibraryChanged -> libraryChanged()
                        Command.Retry -> if (active != null) restart(true)
                    }
                } catch (e: CancellationException) {
                    throw e
                } catch (e: TunnelIssue) {
                    disconnect(e.code)
                } catch (_: Exception) {
                    disconnect("service_failed")
                }
            }
        }
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        lastStartId = startId
        when (intent?.action) {
            START -> {
                if (!foreground) {
                    try {
                        notifications.createNotificationChannel(
                            NotificationChannel(CHANNEL, getString(R.string.app_name), NotificationManager.IMPORTANCE_LOW),
                        )
                        ServiceCompat.startForeground(
                            this, NOTIFICATION_ID, notification(R.string.connecting),
                            if (Build.VERSION.SDK_INT >= 34) ServiceInfo.FOREGROUND_SERVICE_TYPE_SYSTEM_EXEMPTED else 0,
                        )
                        foreground = true
                    } catch (_: Exception) {
                        TunnelRuntime.publish(TunnelState(Phase.Error, issue = "foreground_failed"))
                        stopSelfResult(startId)
                        return START_NOT_STICKY
                    }
                }
                val id = intent.getStringExtra(PROFILE)
                commands.trySend(if (id == null) Command.Stop("invalid_profile") else Command.Start(id))
            }
            STOP -> commands.trySend(Command.Stop())
            RETRY -> commands.trySend(Command.Retry)
            else -> stopSelfResult(startId)
        }
        return START_NOT_STICKY
    }

    private suspend fun connect(id: String) {
        if (active != null) return
        val profile = withContext(Dispatchers.IO) { profiles.load().profiles.firstOrNull { it.id == id } }
            ?: throw TunnelIssue("profile_changed")
        if (prepare(this) != null) throw TunnelIssue("permission_denied")
        active = profile
        status(Phase.Connecting)
        val device = Builder().setSession(getString(R.string.app_name))
            .setMtu(1280)
            .addAddress("198.18.0.1", 30)
            .addAddress("fd6d:9b2a:4f00::1", 126)
            .addRoute("0.0.0.0", 0)
            .addRoute("::", 0)
            .addDnsServer("1.1.1.1")
            .setMetered(false)
            .establish() ?: throw TunnelIssue("tun_failed")
        tun = device
        interfaceName = Androidbridge.interfaceName(device.fd)
        if (interfaceName.isEmpty()) throw TunnelIssue("tun_failed")
        cm.registerNetworkCallback(
            NetworkRequest.Builder().addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
                .addCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN).build(),
            networkCallback,
        )
        observing = true
        restart(false)
    }

    private suspend fun libraryChanged() {
        val running = active ?: return
        val library = profiles.state.value ?: return
        if (library.profiles.none { it.id == running.id }) {
            disconnect("profile_changed")
            return
        }
        val selected = library.active ?: return
        if (selected.id == running.id) return
        active = selected
        restart(reconnecting = true, switching = true)
    }

    private suspend fun networkChanged(change: Command.Capabilities) {
        if (active == null) return
        if (change.caps == null) networks.remove(change.network) else networks[change.network] = change.caps
        val best = networks.entries.maxWithOrNull(
            compareBy<Map.Entry<Network, NetworkCapabilities>> {
                it.value.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED)
            }.thenBy {
                it.value.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) ||
                    it.value.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET)
            }.thenBy { it.key == selectedNetwork },
        )?.key
        if (best == selectedNetwork) return
        selectedNetwork = best
        restart(reconnecting = TunnelRuntime.state.value.checkedAt != null)
    }

    private suspend fun restart(reconnecting: Boolean, switching: Boolean = false) {
        if (switching) status(Phase.Switching)
        else if (reconnecting) status(Phase.Reconnecting)
        stopEngine()
        val profile = active ?: return
        val fd = tun?.fd ?: return
        val network = selectedNetwork
        if (network == null) {
            setUnderlyingNetworks(emptyArray())
            status(Phase.Reconnecting, "network_missing")
            return
        }
        if (!setUnderlyingNetworks(arrayOf(network))) {
            status(Phase.Error, "socket_protection")
            return
        }
        worker = scope.launch {
            try {
                runTunnel(profile, fd, network, reconnecting, switching)
            } catch (e: CancellationException) {
                throw e
            } catch (_: Exception) {
                withContext(Dispatchers.IO) { Androidbridge.stop() }
                status(Phase.Error, "service_failed")
            }
        }
    }

    private suspend fun runTunnel(profile: Profile, fd: Int, network: Network, reconnecting: Boolean, switching: Boolean) {
        val blocked = AtomicBoolean(false)
        val protector = SocketProtector { socket ->
            val allowed = runCatching {
                protect(socket) && ParcelFileDescriptor.fromFd(socket).use {
                    network.bindSocket(it.fileDescriptor)
                    true
                }
            }.getOrDefault(false)
            if (!allowed) blocked.set(true)
            allowed
        }

        var issue: String? = null
        for ((attempt, pause) in RETRY_DELAYS.withIndex()) {
            status(when {
                switching -> Phase.Switching
                attempt == 0 && !reconnecting -> Phase.Connecting
                else -> Phase.Reconnecting
            }, issue)
            delay(pause)
            val endpoints = resolveEndpoint(network, profile.host)
            if (endpoints.isEmpty()) {
                issue = "endpoint_dns"
                continue
            }
            val endpoint = endpoints[attempt % endpoints.size]
            blocked.set(false)
            issue = withContext(Dispatchers.IO) {
                synchronized(nativeLock) {
                    if (destroyed) "service_failed" else Androidbridge.start(profile.link, endpoint, fd, protector)
                }
            }.ifEmpty { null }
            if (issue == null) {
                status(Phase.Verifying)
                val verified = verify()
                issue = if (blocked.get()) "socket_protection" else "probe_failed"
                if (verified || (!blocked.get() && attempt == RETRY_DELAYS.lastIndex)) {
                    monitor(verified)
                    return
                }
                status(Phase.Reconnecting, issue, checked = false)
            }
            withContext(Dispatchers.IO) { Androidbridge.stop() }
            if (issue in PERMANENT_ERRORS) break
        }
        status(Phase.Error, issue ?: "engine_start")
    }

    private suspend fun monitor(initial: Boolean) {
        var verified = initial
        while (true) {
            status(if (verified) Phase.Connected else Phase.Unverified,
                if (verified) null else "probe_failed", checked = verified)
            delay(TunnelRuntime.CHECK_INTERVAL)
            status(Phase.Verifying)
            verified = verify()
        }
    }

    private suspend fun verify(): Boolean {
        if (Androidbridge.status() != "running") return false
        val network = vpnNetwork(cm, interfaceName) ?: return false
        return checkTunnel(network)
    }

    private suspend fun stopEngine() {
        worker?.cancelAndJoin()
        worker = null
        withContext(Dispatchers.IO) { Androidbridge.stop() }
    }

    private suspend fun disconnect(issue: String?) {
        stopEngine()
        releaseTun()
        TunnelRuntime.publish(TunnelRuntime.state.value.copy(
            phase = if (issue == null) Phase.Idle else Phase.Error, profileId = null, issue = issue, vpnInterface = "",
        ))
        ServiceCompat.stopForeground(this, ServiceCompat.STOP_FOREGROUND_REMOVE)
        foreground = false
        stopSelfResult(lastStartId)
    }

    private fun releaseTun() {
        if (observing) {
            cm.unregisterNetworkCallback(networkCallback)
            observing = false
        }
        runCatching { tun?.close() }
        tun = null
        active = null
        networks.clear()
        selectedNetwork = null
    }

    private fun status(phase: Phase, issue: String? = null, checked: Boolean? = null) {
        val before = TunnelRuntime.state.value
        TunnelRuntime.publish(
            TunnelState(
                phase, active?.id, issue,
                checked ?: before.checkPassed,
                if (checked == null) before.checkedAt else SystemClock.elapsedRealtime(),
                if (checked == null) before.checkedProfile else active?.id,
                interfaceName,
                when (active?.transport) {
                    "tls" -> ReportTransport.Tls
                    "reality" -> ReportTransport.Reality
                    else -> ReportTransport.Unknown
                },
                connectionNetwork(),
            ),
        )
        if (foreground) notifications.notify(NOTIFICATION_ID, notification(phase.label))
    }

    private fun connectionNetwork(): ConnectionNetwork {
        val caps = networks[selectedNetwork] ?: return ConnectionNetwork.Unknown
        return when {
            caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) -> ConnectionNetwork.Wifi
            caps.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) -> ConnectionNetwork.Mobile
            caps.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET) -> ConnectionNetwork.Wired
            else -> ConnectionNetwork.Unknown
        }
    }

    private fun notification(label: Int): Notification {
        val open = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
        val stop = PendingIntent.getService(this, 1, Intent(this, TunnelService::class.java).setAction(STOP), PendingIntent.FLAG_IMMUTABLE)
        return Notification.Builder(this, CHANNEL)
            .setSmallIcon(R.drawable.ic_vpn_notification)
            .setContentTitle(getString(R.string.app_name)).setContentText(getString(label))
            .setContentIntent(open).setVisibility(Notification.VISIBILITY_PRIVATE).setOngoing(true)
            .addAction(Notification.Action.Builder(null, getString(R.string.disconnect), stop).build())
            .build()
    }

    override fun onRevoke() { commands.trySend(Command.Stop("permission_revoked")) }

    override fun onDestroy() {
        scope.cancel()
        commands.close()
        // Native start/stop are serialized by the bridge. Close the TUN only
        // after the native duplicate and its packet stack have been stopped.
        synchronized(nativeLock) {
            destroyed = true
            Androidbridge.stop()
            releaseTun()
        }
        val state = TunnelRuntime.state.value
        if (state.profileId != null) TunnelRuntime.publish(state.copy(phase = Phase.Idle, profileId = null, issue = null, vpnInterface = ""))
        super.onDestroy()
    }

    companion object {
        private const val START = "start"
        private const val STOP = "stop"
        private const val RETRY = "retry"
        private const val PROFILE = "profile"
        private const val CHANNEL = "ovpn_tunnel"
        private const val NOTIFICATION_ID = 1
        private val RETRY_DELAYS = listOf(0L, 1000L, 3000L, 10000L)
        private val PERMANENT_ERRORS = setOf("invalid_profile", "engine_config", "tun_failed")

        internal fun start(context: Context, id: String) {
            context.startForegroundService(Intent(context, TunnelService::class.java).setAction(START).putExtra(PROFILE, id))
        }

        internal fun stop(context: Context) {
            context.startService(Intent(context, TunnelService::class.java).setAction(STOP))
        }

        internal fun retry(context: Context) {
            context.startService(Intent(context, TunnelService::class.java).setAction(RETRY))
        }
    }
}
