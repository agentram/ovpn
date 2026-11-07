package io.github.agentram.ovpn.tunnel

import android.Manifest
import android.app.Activity
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.appcompat.app.AppCompatDelegate
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.core.content.edit
import androidx.core.os.LocaleListCompat
import androidx.lifecycle.ViewModelProvider
import com.google.zxing.client.android.Intents
import com.journeyapps.barcodescanner.ScanContract
import com.journeyapps.barcodescanner.ScanOptions

class MainActivity : AppCompatActivity() {
    private lateinit var model: TunnelViewModel
    private var pendingProfile: String? = null
    private var theme by mutableStateOf("system")

    private val permission = registerForActivityResult(ActivityResultContracts.StartActivityForResult()) { result ->
        val id = pendingProfile
        pendingProfile = null
        if (result.resultCode == Activity.RESULT_OK && id != null) {
            startTunnel(id)
        } else {
            model.showIssue("permission_denied")
        }
    }

    private val notifications = registerForActivityResult(ActivityResultContracts.RequestPermission()) {}

    private val scanner = registerForActivityResult(ScanContract()) { result ->
        val text = result.contents
        if (text != null) {
            model.preview(text)
        } else if (result.originalIntent?.getBooleanExtra(Intents.Scan.MISSING_CAMERA_PERMISSION, false) == true) {
            model.showIssue("camera_failed")
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        model = ViewModelProvider(this)[TunnelViewModel::class.java]
        pendingProfile = savedInstanceState?.getString("pending_profile")
        val settings = getSharedPreferences("settings", Context.MODE_PRIVATE)
        theme = settings.getString("theme", "system") ?: "system"
        if (AppCompatDelegate.getApplicationLocales().isEmpty) changeLanguage("en")
        enableEdgeToEdge()
        setContent {
            TunnelTheme(theme) {
                TunnelScreen(
                    model = model,
                    theme = theme,
                    setTheme = {
                        settings.edit { putString("theme", it) }
                        theme = it
                    },
                    language = AppCompatDelegate.getApplicationLocales()[0]?.language ?: "en",
                    setLanguage = ::changeLanguage,
                    connect = ::connect,
                    disconnect = { TunnelService.stop(this) },
                    retry = { TunnelService.retry(this) },
                    scan = ::scan,
                )
            }
        }
    }

    private fun changeLanguage(language: String) {
        AppCompatDelegate.setApplicationLocales(LocaleListCompat.forLanguageTags(language))
    }

    private fun connect(id: String) {
        if (pendingProfile != null) return
        model.dismiss()
        try {
            val request: Intent? = VpnService.prepare(this)
            if (request == null) {
                startTunnel(id)
            } else {
                pendingProfile = id
                permission.launch(request)
            }
        } catch (_: Exception) {
            pendingProfile = null
            model.showIssue("permission_denied")
        }
    }

    private fun startTunnel(id: String) {
        try {
            TunnelService.start(this, id)
            requestNotifications()
        } catch (_: Exception) {
            model.showIssue("foreground_failed")
        }
    }

    private fun requestNotifications() {
        if (Build.VERSION.SDK_INT < 33 ||
            checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) == PackageManager.PERMISSION_GRANTED) return
        val settings = getSharedPreferences("settings", Context.MODE_PRIVATE)
        if (!settings.getBoolean("notification_prompted", false)) {
            settings.edit { putBoolean("notification_prompted", true) }
            notifications.launch(Manifest.permission.POST_NOTIFICATIONS)
        }
    }

    private fun scan() {
        try {
            scanner.launch(
                ScanOptions().setDesiredBarcodeFormats(ScanOptions.QR_CODE)
                    .setPrompt(getString(R.string.scan_prompt))
                    .setBeepEnabled(false).setBarcodeImageEnabled(false).setOrientationLocked(false),
            )
        } catch (_: Exception) {
            model.showIssue("camera_failed")
        }
    }

    override fun onSaveInstanceState(outState: Bundle) {
        pendingProfile?.let { outState.putString("pending_profile", it) }
        super.onSaveInstanceState(outState)
    }
}
