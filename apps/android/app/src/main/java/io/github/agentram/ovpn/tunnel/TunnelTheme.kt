package io.github.agentram.ovpn.tunnel

import android.app.Activity
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalView
import androidx.core.view.WindowCompat

private val Dark = darkColorScheme(
    primary = Color(0xFFA6C9D5), onPrimary = Color(0xFF122B35),
    secondary = Color(0xFF96C9AC), secondaryContainer = Color(0xFF1F3C31), onSecondaryContainer = Color(0xFFBEEBD0),
    background = Color(0xFF0B0E10), onBackground = Color(0xFFEDF0EB),
    surface = Color(0xFF111619), onSurface = Color(0xFFEDF0EB),
    surfaceVariant = Color(0xFF171D21), onSurfaceVariant = Color(0xFFB6C2C6),
    outline = Color(0xFF75848C), error = Color(0xFFFFABA7),
)

private val Light = lightColorScheme(
    primary = Color(0xFF245567), onPrimary = Color.White,
    secondary = Color(0xFF226344), secondaryContainer = Color(0xFFD9F2E5), onSecondaryContainer = Color(0xFF14432F),
    background = Color(0xFFF3F7F7), onBackground = Color(0xFF14252A),
    surface = Color(0xFFFFFFFF), onSurface = Color(0xFF14252A),
    surfaceVariant = Color(0xFFE4ECEE), onSurfaceVariant = Color(0xFF455960),
    outline = Color(0xFF637980), error = Color(0xFF9E2727),
)

internal data class SignalColors(val verified: Color, val pending: Color, val failure: Color)
internal val LocalSignals = staticCompositionLocalOf { SignalColors(Color.Unspecified, Color.Unspecified, Color.Unspecified) }

@Composable
internal fun TunnelTheme(theme: String, content: @Composable () -> Unit) {
    val dark = theme == "dark" || (theme != "light" && isSystemInDarkTheme())
    val view = LocalView.current
    SideEffect {
        val window = (view.context as? Activity)?.window
        if (window != null) {
            val controller = WindowCompat.getInsetsController(window, view)
            controller.isAppearanceLightStatusBars = !dark
            controller.isAppearanceLightNavigationBars = !dark
        }
    }
    val signals = if (dark) {
        SignalColors(Color(0xFF96C9AC), Color(0xFFE0BA7F), Color(0xFFFFABA7))
    } else {
        SignalColors(Color(0xFF226344), Color(0xFF855300), Color(0xFF9E2727))
    }
    CompositionLocalProvider(LocalSignals provides signals) {
        MaterialTheme(colorScheme = if (dark) Dark else Light, content = content)
    }
}
