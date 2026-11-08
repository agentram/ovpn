package io.github.agentram.ovpn.tunnel

import kotlinx.coroutines.runBlocking
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.tls.HandshakeCertificates
import okhttp3.tls.HeldCertificate
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.assertThrows
import org.junit.Test
import java.net.InetAddress
import org.json.JSONObject

class SubscriptionsTest {
    @Test fun acceptsHttpsAndRejectsCredentialsFragmentsOrMalformedPorts() {
        assertEquals("example.test", subscriptionUrl("https://example.test/sub/token")?.host)
        for (url in listOf(
            "http://example.test/sub/token", "https://user@example.test", "https://example.test#secret",
            "https://example.test:0", "https://example.test:70000", "https:/missing-host",
        )) assertNull(subscriptionUrl(url))
    }

    @Test fun boundedHttpsAndSnapshotPreservingFailures() {
        val certificate = HeldCertificate.Builder().commonName("127.0.0.1").addSubjectAlternativeName("127.0.0.1").build()
        val serverTls = HandshakeCertificates.Builder().heldCertificate(certificate).build()
        val clientTls = HandshakeCertificates.Builder().addTrustedCertificate(certificate.certificate).build()
        MockWebServer().use { server ->
            server.useHttps(serverTls.sslSocketFactory(), false)
            server.start(InetAddress.getByAddress(byteArrayOf(127, 0, 0, 1)), 0)
            val url = server.url("/sub/fixture").newBuilder().host("127.0.0.1").build()
            val client = SubscriptionClient(OkHttpClient.Builder()
                .sslSocketFactory(clientTls.sslSocketFactory(), clientTls.trustManager).build())

            server.enqueue(MockResponse().setBody("fixture"))
            assertEquals("fixture", runBlocking { client.fetch(url) })
            server.enqueue(MockResponse().setResponseCode(302).setHeader("Location", server.url("/redirected")))
            assertFailure("subscription_redirect") { client.fetch(url) }
            assertEquals(2, server.requestCount)

            server.enqueue(MockResponse().setResponseCode(404))
            assertFailure("subscription_unavailable") { client.fetch(url) }
            server.enqueue(MockResponse().setChunkedBody("a".repeat(1024 * 1024 + 1), 4096))
            assertFailure("too_large") { client.fetch(url) }

            assertFailure("subscription_tls") { SubscriptionClient().fetch(url) }
        }
    }

    @Test fun activationUsesAnHttpsOriginAndPostsSecretsInTheBody() {
        val secret = newSession()
        val receipt = newSession()
        assertNull(inviteCode("OT1:http://example.test#$secret"))
        assertNull(inviteCode("OT1:https://example.test/path#$secret"))
        assertNull(inviteCode("OT1:https://example.test#short"))
        val certificate = HeldCertificate.Builder().addSubjectAlternativeName("127.0.0.1").build()
        val serverTls = HandshakeCertificates.Builder().heldCertificate(certificate).build()
        val clientTls = HandshakeCertificates.Builder().addTrustedCertificate(certificate.certificate).build()
        MockWebServer().use { server ->
            server.useHttps(serverTls.sslSocketFactory(), false)
            server.start(InetAddress.getByAddress(byteArrayOf(127, 0, 0, 1)), 0)
            val invite = checkNotNull(inviteCode("OT1:https://127.0.0.1:${server.port}#$secret"))
            val client = SubscriptionClient(OkHttpClient.Builder()
                .sslSocketFactory(clientTls.sslSocketFactory(), clientTls.trustManager).build())
            server.enqueue(MockResponse().setResponseCode(204))
            runBlocking { client.activate(invite, receipt) }
            val request = server.takeRequest()
            assertEquals("POST", request.method)
            assertEquals("/activate", request.path)
            val body = JSONObject(request.body.readUtf8())
            assertEquals(secret, body.getString("code"))
            assertEquals(receipt, body.getString("session"))
            assertTrue(invite.deviceUrl(receipt).encodedPath.startsWith("/device/"))
            assertEquals(receipt, invite.deviceUrl(receipt).pathSegments.last())
            server.enqueue(MockResponse().setResponseCode(404))
            assertFailure("code_unavailable") { client.activate(invite, receipt); "" }
        }
    }

    private fun assertFailure(code: String, request: suspend () -> String) {
        val error = assertThrows(TunnelIssue::class.java) { runBlocking { request() } }
        assertEquals(code, error.code)
    }
}
