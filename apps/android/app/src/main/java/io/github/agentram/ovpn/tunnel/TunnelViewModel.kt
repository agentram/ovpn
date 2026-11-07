package io.github.agentram.ovpn.tunnel

import android.app.Application
import android.net.ConnectivityManager
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import io.github.agentram.ovpn.androidbridge.Androidbridge
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import okhttp3.HttpUrl

internal data class WorkState(
    val working: Boolean = false,
    val preview: ImportPreview? = null,
    val issue: String? = null,
    val refreshed: Boolean = false,
)

internal class TunnelViewModel(application: Application) : AndroidViewModel(application) {
    private val store = application.profiles
    private val subscriptions = SubscriptionClient()
    private val connectivity = application.getSystemService(ConnectivityManager::class.java)
    val library = store.state
    val tunnel = TunnelRuntime.state
    private val mutable = MutableStateFlow(WorkState())
    val work = mutable.asStateFlow()

    init { load() }

    fun load() = operate("storage_failed") { store.load(); WorkState() }

    fun preview(input: String) = operate("invalid_import") {
        val raw = input.trim()
        val url = if (raw.startsWith("https:", ignoreCase = true)) {
            subscriptionUrl(raw) ?: throw TunnelIssue("invalid_url")
        } else {
            if (raw.startsWith("http:", ignoreCase = true)) throw TunnelIssue("invalid_url")
            null
        }
        val data = if (url == null) raw else fetch(url)
        WorkState(preview = decodePreview(Androidbridge.inspect(data), url?.toString()))
    }

    fun save() {
        val preview = work.value.preview ?: return
        operate("storage_failed") {
            store.update { it.import(preview) }
            WorkState()
        }
    }

    fun refresh(source: Subscription) = operate("storage_failed") {
        val url = subscriptionUrl(source.url) ?: throw TunnelIssue("invalid_url")
        val preview = decodePreview(Androidbridge.inspect(fetch(url)), source.url)
        store.update { it.replace(source, preview.compatible) }
        WorkState(refreshed = true)
    }

    private suspend fun fetch(url: HttpUrl): String {
        val state = tunnel.value
        if (state.profileId == null) return subscriptions.fetch(url)
        if (state.vpnInterface.isEmpty()) throw TunnelIssue("subscription_network")
        val vpn = vpnNetwork(connectivity, state.vpnInterface) ?: throw TunnelIssue("subscription_network")
        return SubscriptionClient(vpnHttpBuilder(vpn).build()).fetch(url)
    }

    fun select(id: String) = operate("storage_failed") {
        if (TunnelRuntime.state.value.profileId == null) {
            store.update { library ->
                if (library.profiles.none { it.id == id }) throw TunnelIssue("profile_changed")
                library.copy(selected = id)
            }
        }
        WorkState()
    }

    fun removeProfile(id: String) = operate("storage_failed") {
        store.update { it.removeProfile(id) }
        WorkState()
    }

    fun removeSubscription(id: String) = operate("storage_failed") {
        store.update { it.removeSubscription(id) }
        WorkState()
    }

    fun dismiss() {
        if (!work.value.working) mutable.value = WorkState()
    }

    fun showIssue(code: String) { mutable.value = WorkState(issue = code) }

    private fun operate(fallback: String, block: suspend () -> WorkState) {
        if (work.value.working) return
        mutable.value = WorkState(working = true)
        viewModelScope.launch {
            mutable.value = try {
                withContext(Dispatchers.IO) { block() }
            } catch (e: CancellationException) {
                throw e
            } catch (e: TunnelIssue) {
                WorkState(issue = e.code)
            } catch (_: Exception) {
                WorkState(issue = fallback)
            }
        }
    }
}
