package io.github.agentram.ovpn.tunnel

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Test

class ProfilesTest {
    private val endpoint = "https://subscriptions.example.test/sub/fixture"
    private val tls = Profile("tls-id", "TLS", "example.test", "tls", "fixture-tls")
    private val reality = Profile("reality-id", "REALITY", "example.test", "reality", "fixture-reality")

    @Test fun refreshKeepsRenamedProfileButClearsChangedOrRemovedSelection() {
        val initial = Library().import(
            ImportPreview(listOf(tls.row(), reality.row(), PreviewRow("XHTTP", tls.host, "xhttp", "xhttp_unencrypted", null)), endpoint),
        )
        val source = initial.subscriptions.single()
        val renamed = initial.replace(source, listOf(tls.copy(name = "New name"), reality))
        assertEquals(initial.selected, renamed.selected)
        assertEquals("New name", renamed.active?.name)
        val changed = initial.replace(source, listOf(tls.copy(id = "new-id"), reality))
        assertNull(changed.selected)
        assertNull(initial.replace(source, emptyList()).selected)
        assertEquals(initial, decodeLibrary(initial.encode()))
    }

    @Test fun refreshCannotRestoreDeletedSubscriptionOrMatchAnIndependentProfile() {
        val manual = Library().import(ImportPreview(listOf(tls.row()), null))
        val withSource = manual.import(ImportPreview(listOf(tls.row()), endpoint))
        assertNotEquals(withSource.manual.single().id, withSource.subscriptions.single().profiles.single().id)
        val source = withSource.subscriptions.single()
        val removed = withSource.removeSubscription(source.id)
        assertThrows(TunnelIssue::class.java) { removed.replace(source, listOf(reality)) }
        assertEquals(manual, removed)
    }

    @Test fun oldProfilesLoadAndAnInterruptedActivationKeepsItsReceipt() {
        val saved = Library().import(ImportPreview(listOf(tls.row()), null))
        val legacy = JSONObject(saved.encode().toString(Charsets.UTF_8)).apply { remove("activation") }
        assertEquals(saved, decodeLibrary(legacy.toString().toByteArray(Charsets.UTF_8)))
        val pending = saved.copy(pending = PendingActivation("fixture-code", "fixture-receipt"))
        assertEquals(pending, decodeLibrary(pending.encode()))
    }

    private fun Profile.row() = PreviewRow(name, host, transport, null, this)
}
