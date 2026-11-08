package io.github.agentram.ovpn.tunnel

import java.time.Instant

internal enum class ConnectionNetwork { Wifi, Mobile, Wired, Unknown }
internal enum class ReportTransport { Tls, Reality, Unknown }

internal data class ReportEvent(
    val time: Instant,
    val phase: Phase,
    val network: ConnectionNetwork,
    val transport: ReportTransport,
    val issue: String?,
)

private val issueCodes = setOf(
    "profile_changed", "permission_denied", "permission_revoked", "foreground_failed", "tun_failed",
    "endpoint_dns", "network_missing", "socket_protection", "probe_failed", "engine_start",
    "engine_config", "invalid_profile", "service_failed",
)

internal fun reportEvents(
    events: List<ReportEvent>, time: Instant, phase: Phase, network: ConnectionNetwork,
    transport: ReportTransport, issue: String?,
): List<ReportEvent> {
    val safe = ReportEvent(time, phase, network, transport, issue?.let {
        if (it in issueCodes) it else "unknown"
    })
    val last = events.lastOrNull()
    return if (last?.copy(time = time) == safe) events else (events + safe).takeLast(16)
}

internal fun connectionReport(app: String, core: String, sdk: Int, events: List<ReportEvent>): String = buildString {
    appendLine("O Tunnel · Connection report")
    appendLine("App: $app · Engine: $core · Android API: $sdk")
    for (event in events.asReversed()) {
        append(event.time)
        append(" · ${event.phase.name} · ${event.network.name} · ${event.transport.name}")
        event.issue?.let { append(" · $it") }
        appendLine()
    }
}
