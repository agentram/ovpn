package io.github.agentram.ovpn.tunnel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.Instant

class ConnectionReportTest {
    @Test fun reportOnlyIncludesKnownIssuesAndBoundsHistory() {
        var events = emptyList<ReportEvent>()
        for (i in 0..20) {
            events = reportEvents(events, Instant.EPOCH.plusSeconds(i.toLong()),
                if (i % 2 == 0) Phase.Switching else Phase.Error,
                ConnectionNetwork.Wifi, ReportTransport.Reality, "https://example.test/private")
        }
        assertEquals(16, events.size)
        val report = connectionReport("test", "test", 36, events)
        assertTrue(report.contains("Switching · Wifi · Reality · unknown"))
        assertFalse(report.contains("example.test"))
        assertEquals(events, reportEvents(events, Instant.EPOCH.plusSeconds(21), Phase.Switching,
            ConnectionNetwork.Wifi, ReportTransport.Reality, "another unexpected value"))
    }
}
