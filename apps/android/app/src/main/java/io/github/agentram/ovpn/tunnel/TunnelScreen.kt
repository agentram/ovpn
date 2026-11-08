package io.github.agentram.ovpn.tunnel

import android.os.Build
import android.os.SystemClock
import androidx.activity.compose.BackHandler

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalClipboard
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.DialogProperties
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.github.agentram.ovpn.androidbridge.Androidbridge
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import androidx.compose.runtime.rememberCoroutineScope

private enum class Page(val label: Int) {
    Home(R.string.app_name), Profiles(R.string.choose_profile), Settings(R.string.settings), Access(R.string.manage_access),
}

@Composable
internal fun TunnelScreen(
    model: TunnelViewModel,
    theme: String,
    setTheme: (String) -> Unit,
    language: String,
    setLanguage: (String) -> Unit,
    connect: (String) -> Unit,
    disconnect: () -> Unit,
    retry: () -> Unit,
    scan: () -> Unit,
    chooseImage: () -> Unit,
    shareReport: (String) -> Unit,
) {
    val library by model.library.collectAsStateWithLifecycle()
    val tunnel by model.tunnel.collectAsStateWithLifecycle()
    val work by model.work.collectAsStateWithLifecycle()
    var page by rememberSaveable { mutableStateOf(Page.Home) }
    var paste by remember { mutableStateOf(false) }
    var removal by remember { mutableStateOf<Pair<String, () -> Unit>?>(null) }

    var qr by remember { mutableStateOf(false) }
    var report by remember { mutableStateOf<String?>(null) }
    BackHandler(page != Page.Home) { page = if (page == Page.Access) Page.Settings else Page.Home }

    Scaffold { insets ->
        Box(Modifier.fillMaxSize().padding(insets), contentAlignment = Alignment.TopCenter) {
            LazyColumn(
                modifier = Modifier.widthIn(max = 640.dp).fillMaxWidth().padding(horizontal = 20.dp),
                verticalArrangement = Arrangement.spacedBy(14.dp),
                contentPadding = androidx.compose.foundation.layout.PaddingValues(vertical = 20.dp),
            ) {
                item {
                    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween,
                        verticalAlignment = Alignment.CenterVertically) {
                        Text(stringResource(page.label), Modifier.weight(1f),
                            style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.SemiBold)
                        TextButton(onClick = {
                            page = when (page) {
                                Page.Home -> Page.Settings
                                Page.Access -> Page.Settings
                                else -> Page.Home
                            }
                        }) { Text(stringResource(if (page == Page.Home) R.string.settings else R.string.back)) }
                    }
                }
                if (work.working) item { LinearProgressIndicator(Modifier.fillMaxWidth()) }
                work.issue?.let { issue -> item {
                    Signal(stringResource(issueLabel(issue)), "!", LocalSignals.current.failure)
                    if (library == null) TextButton(onClick = { model.load() }) { Text(stringResource(R.string.retry)) }
                } }
                if (work.refreshed) item { Signal(stringResource(R.string.refreshed), "✓", LocalSignals.current.verified) }
                val data = library
                if (data == null && page != Page.Settings) {
                    if (work.issue == null) item { Text(stringResource(R.string.loading)) }
                } else when (page) {
                    Page.Home -> if (data != null) item {
                        Home(data, tunnel, work.working, connect, disconnect, retry,
                            enterCode = { paste = true }, scan = { qr = true }, resume = model::retrySetup,
                            showProfiles = { page = Page.Profiles },
                            showReport = {
                                report = connectionReport(BuildConfig.VERSION_NAME, Androidbridge.coreVersion(),
                                    Build.VERSION.SDK_INT, TunnelRuntime.events.value)
                            })
                    }
                    Page.Profiles -> if (data != null) {
                        items(data.profiles, key = { it.id }) { profile ->
                            ProfileCard(profile, data.selected, !work.working,
                                select = { model.select(profile.id); page = Page.Home }, remove = null, working = work.working)
                        }
                    }
                    Page.Access -> if (data != null) {
                        item {
                            Panel {
                                SectionTitle(R.string.empty_title)
                                Text(stringResource(R.string.empty_body))
                                if (data.pending != null) {
                                    Button(onClick = model::retrySetup, enabled = !work.working) { Text(stringResource(R.string.resume_setup)) }
                                }
                                SetupButtons(work.working, { paste = true }, { qr = true })
                            }
                        }
                        if (data.manual.isNotEmpty()) item { SectionTitle(R.string.manual_profiles) }
                        items(data.manual, key = { it.id }) { profile ->
                            ProfileCard(profile, data.selected, !work.working,
                                select = { model.select(profile.id) }, remove = {
                                    removal = profile.name to { model.removeProfile(profile.id) }
                                }, working = work.working)
                        }
                        data.subscriptions.forEach { source ->
                            item(key = source.id) {
                                Panel {
                                    SectionTitle(R.string.subscription)
                                    Text(source.host)
                                    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                        OutlinedButton(onClick = { model.refresh(source) }, enabled = !work.working) {
                                            Text(stringResource(R.string.refresh))
                                        }
                                        TextButton(onClick = {
                                            removal = source.host to { model.removeSubscription(source.id) }
                                        }, enabled = !work.working) { Text(stringResource(R.string.remove)) }
                                    }
                                }
                            }
                            items(source.profiles, key = { it.id }) { profile ->
                                ProfileCard(profile, data.selected, !work.working,
                                    select = { model.select(profile.id) }, remove = null, working = work.working)
                            }
                        }
                    }
                    Page.Settings -> item {
                        Settings(theme, setTheme, language, setLanguage) { page = Page.Access }
                    }
                }
            }
        }
    }
    if (paste) PasteDialog(close = { paste = false }, review = {
        paste = false
        model.preview(it)
    })
    if (qr) {
        AlertDialog(
            onDismissRequest = { qr = false },
            title = { Text(stringResource(R.string.scan_qr)) },
            text = { Text(stringResource(R.string.qr_options)) },
            confirmButton = { TextButton(onClick = { qr = false; scan() }) { Text(stringResource(R.string.use_camera)) } },
            dismissButton = { TextButton(onClick = { qr = false; chooseImage() }) { Text(stringResource(R.string.choose_image)) } },
        )
    }
    report?.let { text ->
        AlertDialog(
            onDismissRequest = { report = null },
            title = { Text(stringResource(R.string.report_title)) },
            text = {
                LazyColumn(Modifier.heightIn(max = 280.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    item { Text(stringResource(R.string.report_body)) }
                    items(text.lines()) { line -> Text(line, style = MaterialTheme.typography.bodySmall) }
                }
            },
            confirmButton = { TextButton(onClick = { shareReport(text) }) { Text(stringResource(R.string.share_report)) } },
            dismissButton = { TextButton(onClick = { report = null }) { Text(stringResource(R.string.close)) } },
        )
    }
    work.preview?.let { PreviewDialog(it, model::dismiss, model::save) }
    removal?.let { (name, remove) ->
        AlertDialog(
            onDismissRequest = { removal = null },
            title = { Text(stringResource(R.string.remove_title, name)) },
            text = { Text(stringResource(R.string.remove_body)) },
            confirmButton = { TextButton(onClick = { removal = null; remove() }) { Text(stringResource(R.string.remove)) } },
            dismissButton = { TextButton(onClick = { removal = null }) { Text(stringResource(R.string.cancel)) } },
        )
    }
}

@Composable
private fun Home(
    library: Library, tunnel: TunnelState, working: Boolean,
    connect: (String) -> Unit, disconnect: () -> Unit, retry: () -> Unit,
    enterCode: () -> Unit, scan: () -> Unit, resume: () -> Unit,
    showProfiles: () -> Unit, showReport: () -> Unit,
) {
    val selected = library.active
    val engaged = tunnel.profileId != null
    val signals = LocalSignals.current
    var now by remember { mutableStateOf(SystemClock.elapsedRealtime()) }
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { now = SystemClock.elapsedRealtime() }
    LaunchedEffect(tunnel.checkedAt) {
        now = SystemClock.elapsedRealtime()
        tunnel.checkedAt?.let {
            delay((it + TunnelRuntime.CHECK_INTERVAL - now).coerceAtLeast(0))
            now = SystemClock.elapsedRealtime()
        }
    }
    val phase = when {
        engaged && selected != null && tunnel.profileId != selected.id -> Phase.Switching
        tunnel.phase == Phase.Connected && now - (tunnel.checkedAt ?: 0L) >= TunnelRuntime.CHECK_INTERVAL -> Phase.Unverified
        else -> tunnel.phase
    }
    val (icon, color) = when (phase) {
        Phase.Idle -> "○" to MaterialTheme.colorScheme.onSurfaceVariant
        Phase.Connected -> "✓" to signals.verified
        Phase.Error -> "!" to signals.failure
        else -> "◷" to signals.pending
    }
    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Panel {
            if (library.profiles.isEmpty() && !engaged) {
                SectionTitle(R.string.empty_title)
                Text(stringResource(R.string.empty_body))
                if (library.pending != null) {
                    Button(onClick = resume, enabled = !working) { Text(stringResource(R.string.resume_setup)) }
                }
                SetupButtons(working, enterCode, scan)
            } else {
                Signal(stringResource(phase.label), icon, color)
                selected?.let { Text(it.name, style = MaterialTheme.typography.headlineSmall) }
                if (engaged || selected != null) {
                    Button(
                        onClick = { if (engaged) disconnect() else selected?.let { connect(it.id) } },
                        modifier = Modifier.fillMaxWidth().heightIn(min = 56.dp),
                        enabled = engaged || !working,
                    ) { Text(stringResource(if (engaged) R.string.disconnect else R.string.connect)) }
                }
                if (library.profiles.isNotEmpty()) {
                    OutlinedButton(onClick = showProfiles, enabled = !working, modifier = Modifier.fillMaxWidth()) {
                        Text(stringResource(if (selected == null) R.string.choose_profile else R.string.change_profile))
                    }
                }
                tunnel.issue?.let { Text(stringResource(issueLabel(it)), style = MaterialTheme.typography.bodyMedium) }
                if ((phase == Phase.Error || phase == Phase.Unverified) && selected != null) {
                    TextButton(onClick = { if (engaged) retry() else connect(selected.id) }, enabled = !working) {
                        Text(stringResource(R.string.retry))
                    }
                }
            }
        }
        TextButton(onClick = showReport) { Text(stringResource(R.string.report_title)) }
    }
}

@Composable
private fun SetupButtons(working: Boolean, enterCode: () -> Unit, scan: () -> Unit) {
    Button(onClick = enterCode, enabled = !working, modifier = Modifier.fillMaxWidth()) {
        Text(stringResource(R.string.enter_code))
    }
    OutlinedButton(onClick = scan, enabled = !working, modifier = Modifier.fillMaxWidth()) {
        Text(stringResource(R.string.scan_qr))
    }
}

@Composable
private fun ProfileCard(profile: Profile, selected: String?, canSelect: Boolean, select: () -> Unit, remove: (() -> Unit)?, working: Boolean) {
    Panel {
        Row(
            Modifier.fillMaxWidth().selectable(selected == profile.id, canSelect, Role.RadioButton, onClick = select),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            RadioButton(selected = selected == profile.id, enabled = canSelect, onClick = null)
            Column(Modifier.weight(1f)) {
                Text(profile.name, style = MaterialTheme.typography.titleMedium)
            }
        }
        if (remove != null) TextButton(onClick = remove, enabled = !working) { Text(stringResource(R.string.remove)) }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun PasteDialog(close: () -> Unit, review: (String) -> Unit) {
    var text by remember { mutableStateOf("") }
    val clipboard = LocalClipboard.current
    val scope = rememberCoroutineScope()
    AlertDialog(
        onDismissRequest = close,
        properties = DialogProperties(securePolicy = androidx.compose.ui.window.SecureFlagPolicy.SecureOn),
        title = { Text(stringResource(R.string.enter_code)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(
                    value = text, onValueChange = { if (it.length <= 1024 * 1024) text = it },
                    modifier = Modifier.fillMaxWidth(), label = { Text(stringResource(R.string.import_hint)) },
                    visualTransformation = PasswordVisualTransformation(),
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
                    singleLine = true,
                )
                TextButton(onClick = {
                    scope.launch {
                        val data = clipboard.getClipEntry()?.clipData
                        val pasted = data?.takeIf { it.itemCount > 0 }?.getItemAt(0)?.text?.toString()
                        if (pasted != null) review(pasted)
                    }
                }) { Text(stringResource(R.string.paste_link)) }
            }
        },
        confirmButton = { Button(onClick = { review(text) }, enabled = text.isNotBlank()) { Text(stringResource(R.string.review)) } },
        dismissButton = { TextButton(onClick = close) { Text(stringResource(R.string.cancel)) } },
    )
}

@Composable
private fun PreviewDialog(preview: ImportPreview, close: () -> Unit, save: () -> Unit) {
    AlertDialog(
        onDismissRequest = close,
        title = { Text(stringResource(R.string.preview_title)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text(stringResource(R.string.preview_count, preview.compatible.size, preview.rows.size))
                LazyColumn(Modifier.heightIn(max = 300.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    items(preview.rows) { row ->
                        Column {
                            Text(row.name, style = MaterialTheme.typography.titleMedium)
                            row.issue?.let { Text(stringResource(issueLabel(it)), color = LocalSignals.current.pending) }
                        }
                    }
                }
            }
        },
        confirmButton = { Button(onClick = save, enabled = preview.compatible.isNotEmpty()) { Text(stringResource(R.string.save)) } },
        dismissButton = { TextButton(onClick = close) { Text(stringResource(R.string.cancel)) } },
    )
}

@Composable
private fun Settings(
    theme: String, setTheme: (String) -> Unit, language: String, setLanguage: (String) -> Unit,
    manageAccess: () -> Unit,
) {
    Column(verticalArrangement = Arrangement.spacedBy(14.dp)) {
        OutlinedButton(onClick = manageAccess, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.manage_access)) }
        Panel {
            SectionTitle(R.string.language)
            Choice(R.string.language_en, language == "en") { setLanguage("en") }
            Choice(R.string.language_ru, language == "ru") { setLanguage("ru") }
            Choice(R.string.language_zh, language.startsWith("zh")) { setLanguage("zh-CN") }
        }
        Panel {
            SectionTitle(R.string.theme)
            Choice(R.string.theme_system, theme == "system") { setTheme("system") }
            Choice(R.string.theme_light, theme == "light") { setTheme("light") }
            Choice(R.string.theme_dark, theme == "dark") { setTheme("dark") }
        }
        Panel {
            SectionTitle(R.string.about)
            val core = remember { Androidbridge.coreVersion() }
            Text(stringResource(R.string.version_app, BuildConfig.VERSION_NAME))
            Text(stringResource(R.string.version_core, core))
        }
        Panel {
            SectionTitle(R.string.privacy)
            Text(stringResource(R.string.privacy_body))
        }
    }
}

@Composable
private fun Choice(label: Int, selected: Boolean, click: () -> Unit) {
    Row(Modifier.fillMaxWidth().heightIn(min = 48.dp).selectable(selected, role = Role.RadioButton, onClick = click),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
        RadioButton(selected = selected, onClick = null)
        Text(stringResource(label))
    }
}

@Composable
private fun Panel(content: @Composable () -> Unit) {
    Card(Modifier.fillMaxWidth(), colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surface)) {
        Column(Modifier.fillMaxWidth().padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) { content() }
    }
}

@Composable
private fun SectionTitle(id: Int) = Text(stringResource(id), style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold)

@Composable
private fun Signal(text: String, symbol: String, color: Color) {
    Surface(
        color = MaterialTheme.colorScheme.surfaceVariant,
        shape = MaterialTheme.shapes.medium,
        modifier = Modifier.fillMaxWidth().semantics { liveRegion = LiveRegionMode.Polite },
    ) {
        Row(Modifier.padding(10.dp), horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
            Text(symbol, Modifier.clearAndSetSemantics {}, color = color, fontWeight = FontWeight.Bold)
            Text(text, color = color, style = MaterialTheme.typography.bodyMedium)
        }
    }
}
