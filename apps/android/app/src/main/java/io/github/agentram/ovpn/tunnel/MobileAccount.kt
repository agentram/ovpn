package io.github.agentram.ovpn.tunnel

import android.content.Context
import android.content.Intent
import android.net.Uri
import kotlinx.coroutines.suspendCancellableCoroutine
import net.openid.appauth.AuthState
import net.openid.appauth.AuthorizationException
import net.openid.appauth.AuthorizationRequest
import net.openid.appauth.AuthorizationResponse
import net.openid.appauth.AuthorizationService
import net.openid.appauth.AuthorizationServiceConfiguration
import net.openid.appauth.ResponseTypeValues
import okhttp3.HttpUrl
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException

internal class MobileAccount(context: Context) {
    private val service = AuthorizationService(context.applicationContext)
    private val issuer = Uri.parse(BuildConfig.ACCOUNT_ISSUER)
    val origin: HttpUrl? = subscriptionUrl(BuildConfig.ACCOUNT_ORIGIN)?.takeIf {
        it.encodedPath == "/" && it.query == null
    }

    fun owns(url: HttpUrl): Boolean =
        origin?.let { it.scheme == url.scheme && it.host == url.host && it.port == url.port } == true

    suspend fun loginIntent(): Intent {
        if (issuer.scheme != "https" || origin == null) throw TunnelIssue("account_failed")
        val config = suspendCancellableCoroutine { result ->
            AuthorizationServiceConfiguration.fetchFromIssuer(issuer) { config, _ ->
                if (config == null) result.resumeWithException(TunnelIssue("account_failed"))
                else result.resume(config)
            }
        }
        if (config.discoveryDoc?.issuer != issuer.toString() ||
            config.authorizationEndpoint.scheme != "https" || config.tokenEndpoint.scheme != "https") {
            throw TunnelIssue("account_failed")
        }
        val request = AuthorizationRequest.Builder(config, BuildConfig.ACCOUNT_CLIENT_ID,
            ResponseTypeValues.CODE, Uri.parse("io.github.agentram.ovpn.tunnel:/oauth"))
            .setScope("openid email profile").setPrompt("login").build()
        return service.getAuthorizationRequestIntent(request)
    }

    suspend fun finish(data: Intent): AuthState {
        val response = AuthorizationResponse.fromIntent(data) ?: throw TunnelIssue("account_failed")
        val request = response.request
        if (request.clientId != BuildConfig.ACCOUNT_CLIENT_ID ||
            request.configuration.discoveryDoc?.issuer != issuer.toString() ||
            request.configuration.tokenEndpoint.scheme != "https") throw TunnelIssue("account_failed")
        val state = AuthState(response, AuthorizationException.fromIntent(data))
        return suspendCancellableCoroutine { result ->
            service.performTokenRequest(response.createTokenExchangeRequest()) { token, error ->
                state.update(token, error)
                if (token?.idToken == null || !state.isAuthorized) {
                    result.resumeWithException(TunnelIssue("account_failed"))
                } else result.resume(state)
            }
        }
    }

    suspend fun fresh(state: AuthState): String = suspendCancellableCoroutine { result ->
        state.performActionWithFreshTokens(service) { _, token, error ->
            if (token == null || error != null) result.resumeWithException(TunnelIssue("account_failed"))
            else result.resume(token)
        }
    }

    fun close() = service.dispose()
}

internal val accountRequired get() = BuildConfig.ACCOUNT_ISSUER.isNotEmpty()
