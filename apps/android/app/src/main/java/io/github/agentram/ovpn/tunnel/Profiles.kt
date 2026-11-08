package io.github.agentram.ovpn.tunnel

import org.json.JSONArray
import org.json.JSONObject
import java.util.UUID

internal data class Profile(
    val id: String,
    val name: String,
    val host: String,
    val transport: String,
    val link: String,
)

internal data class PreviewRow(
    val name: String,
    val host: String,
    val transport: String,
    val issue: String?,
    val profile: Profile?,
)

internal data class ImportPreview(val rows: List<PreviewRow>, val source: String?) {
    val compatible: List<Profile> get() = rows.mapNotNull { it.profile }
}

internal class TunnelIssue(val code: String) : Exception(code)

internal data class Subscription(val id: String, val url: String, val profiles: List<Profile>) {
    val host: String get() = java.net.URI(url).host
}

internal data class PendingActivation(val code: String, val session: String)

internal data class Library(
    val manual: List<Profile> = emptyList(),
    val subscriptions: List<Subscription> = emptyList(),
    val selected: String? = null,
    val pending: PendingActivation? = null,
) {
    val profiles: List<Profile> get() = manual + subscriptions.flatMap { it.profiles }
    val active: Profile? get() = profiles.firstOrNull { it.id == selected }

    fun import(preview: ImportPreview): Library {
        val profiles = preview.compatible
        if (profiles.isEmpty()) throw TunnelIssue("no_compatible")
        val source = preview.source
        val next = if (source == null) {
            copy(manual = (manual + profiles).distinctBy { it.id })
        } else {
            val existing = subscriptions.firstOrNull { it.url == source }
            if (existing == null) {
                copy(subscriptions = subscriptions + scoped(UUID.randomUUID().toString(), source, profiles))
            } else {
                replace(existing, profiles)
            }
        }
        return if (this.profiles.isEmpty()) next.copy(selected = next.profiles.firstOrNull()?.id) else next
    }

    fun replace(original: Subscription, profiles: List<Profile>): Library {
        if (subscriptions.none { it.id == original.id && it.url == original.url }) throw TunnelIssue("source_removed")
        return copy(
            subscriptions = subscriptions.map {
                if (it.id == original.id) scoped(it.id, it.url, profiles) else it
            },
        ).clearInvalidSelection()
    }

    fun removeProfile(id: String) = copy(manual = manual.filterNot { it.id == id }).clearInvalidSelection()
    fun removeSubscription(id: String) =
        copy(subscriptions = subscriptions.filterNot { it.id == id }).clearInvalidSelection()

    private fun clearInvalidSelection() = if (selected != null && active == null) copy(selected = null) else this
}

private fun scoped(id: String, url: String, profiles: List<Profile>) =
    Subscription(id, url, profiles.map { it.copy(id = "$id/${it.id}") })

internal fun decodePreview(json: String, source: String?): ImportPreview {
    val obj = JSONObject(json)
    if (obj.has("issue")) throw TunnelIssue(obj.getString("issue"))
    val rows = obj.getJSONArray("profiles").objects().map { row ->
        val name = row.getString("name")
        val host = row.getString("host")
        val transport = row.getString("transport")
        val issue = row.optString("issue").ifEmpty { null }
        val profile = if (issue == null) Profile(row.getString("id"), name, host, transport, row.getString("link")) else null
        PreviewRow(name, host, transport, issue, profile)
    }
    return ImportPreview(rows, source)
}

internal fun Library.encode(): ByteArray = JSONObject()
    .put("schema", 1)
    .put("selected", selected ?: JSONObject.NULL)
    .put("activation", pending?.let { JSONObject().put("code", it.code).put("session", it.session) } ?: JSONObject.NULL)
    .put("manual", manual.toJson())
    .put("subscriptions", JSONArray(subscriptions.map {
        JSONObject().put("id", it.id).put("url", it.url).put("profiles", it.profiles.toJson())
    })).toString().toByteArray(Charsets.UTF_8)

internal fun decodeLibrary(raw: ByteArray): Library {
    val data = JSONObject(raw.toString(Charsets.UTF_8))
    check(data.getInt("schema") == 1)
    return Library(
        manual = data.getJSONArray("manual").profiles(),
        subscriptions = data.getJSONArray("subscriptions").objects().map {
            Subscription(it.getString("id"), it.getString("url"), it.getJSONArray("profiles").profiles())
        },
        selected = if (data.isNull("selected")) null else data.getString("selected"),
        pending = data.optJSONObject("activation")?.let { PendingActivation(it.getString("code"), it.getString("session")) },
    )
}

private fun List<Profile>.toJson() = JSONArray(map {
    JSONObject().put("id", it.id).put("name", it.name).put("host", it.host)
        .put("transport", it.transport).put("link", it.link)
})

private fun JSONArray.objects() = (0 until length()).map(::getJSONObject)
private fun JSONArray.profiles() = objects().map {
    Profile(it.getString("id"), it.getString("name"), it.getString("host"), it.getString("transport"), it.getString("link"))
}
