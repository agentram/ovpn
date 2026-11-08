package io.github.agentram.ovpn.tunnel

internal val Phase.label: Int
    get() = when (this) {
        Phase.Idle -> R.string.disconnected
        Phase.Connecting -> R.string.connecting
        Phase.Verifying -> R.string.verifying
        Phase.Connected -> R.string.connected
        Phase.Unverified -> R.string.unverified
        Phase.Reconnecting -> R.string.reconnecting
        Phase.Switching -> R.string.switching
        Phase.Error -> R.string.connection_error
    }

internal fun issueLabel(code: String) = when (code) {
    "invalid_code" -> R.string.invalid_code
    "code_unavailable" -> R.string.code_unavailable
    "qr_unreadable" -> R.string.qr_unreadable
    "too_large" -> R.string.too_large
    "too_many" -> R.string.too_many
    "invalid_url" -> R.string.invalid_url
    "invalid_import" -> R.string.invalid_import
    "invalid_profile" -> R.string.invalid_profile
    "unsupported_profile" -> R.string.unsupported_profile
    "xhttp_deferred" -> R.string.xhttp_deferred
    "xhttp_unencrypted" -> R.string.xhttp_unencrypted
    "no_compatible" -> R.string.no_compatible
    "subscription_unavailable" -> R.string.subscription_unavailable
    "subscription_tls" -> R.string.subscription_tls
    "subscription_redirect" -> R.string.subscription_redirect
    "subscription_http" -> R.string.subscription_http
    "subscription_network" -> R.string.subscription_network
    "source_removed" -> R.string.source_removed
    "profile_changed" -> R.string.profile_changed
    "permission_denied" -> R.string.permission_denied
    "permission_revoked" -> R.string.permission_revoked
    "foreground_failed" -> R.string.foreground_failed
    "tun_failed" -> R.string.tun_failed
    "endpoint_dns" -> R.string.endpoint_dns
    "network_missing" -> R.string.network_missing
    "socket_protection" -> R.string.socket_protection
    "probe_failed" -> R.string.probe_failed
    "engine_config", "engine_start", "engine_busy" -> R.string.engine_failed
    "storage_failed" -> R.string.storage_failed
    "storage_full" -> R.string.storage_full
    "camera_failed" -> R.string.camera_failed
    else -> R.string.service_failed
}
