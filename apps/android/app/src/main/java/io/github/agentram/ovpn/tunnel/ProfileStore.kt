package io.github.agentram.ovpn.tunnel

import android.app.Application
import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import java.io.File
import java.io.FileOutputStream
import java.nio.file.Files
import java.nio.file.StandardCopyOption
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

internal class TunnelApp : Application() {
    internal val profiles by lazy { ProfileStore(this) }
}

internal val Context.profiles: ProfileStore get() = (applicationContext as TunnelApp).profiles

internal class ProfileStore(context: Context) {
    private val file = File(context.noBackupFilesDir, "profiles.v1")
    private val mutable = MutableStateFlow<Library?>(null)
    val state = mutable.asStateFlow()

    @Synchronized
    fun load(): Library {
        state.value?.let { return it }
        if (!file.exists()) return Library().also { mutable.value = it }
        require(file.length() in (NONCE_BYTES + 16L)..(MAX_BYTES + NONCE_BYTES + 16L))
        val data = file.readBytes()
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.DECRYPT_MODE, key(create = false), GCMParameterSpec(128, data, 0, NONCE_BYTES))
        return decodeLibrary(cipher.doFinal(data, NONCE_BYTES, data.size - NONCE_BYTES)).also { mutable.value = it }
    }

    @Synchronized
    fun update(change: (Library) -> Library): Library {
        val current = load()
        val next = change(current)
        if (next == current) return current
        val payload = next.encode()
        if (payload.size > MAX_BYTES) throw TunnelIssue("storage_full")
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.ENCRYPT_MODE, key(create = true))
        check(cipher.iv.size == NONCE_BYTES)
        val encrypted = cipher.doFinal(payload)
        val pending = File(file.parentFile, "profiles.pending")
        try {
            FileOutputStream(pending).use {
                it.write(cipher.iv)
                it.write(encrypted)
                it.fd.sync()
            }
            Files.move(pending.toPath(), file.toPath(), StandardCopyOption.ATOMIC_MOVE, StandardCopyOption.REPLACE_EXISTING)
        } finally {
            pending.delete()
        }
        mutable.value = next
        return next
    }

    private fun key(create: Boolean): SecretKey {
        val store = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        (store.getKey(KEY, null) as? SecretKey)?.let { return it }
        check(create) { "Profile key unavailable" }
        val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore")
        generator.init(
            KeyGenParameterSpec.Builder(KEY, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                .build(),
        )
        return generator.generateKey()
    }

    private companion object {
        const val KEY = "ovpn.profile-store.v1"
        const val NONCE_BYTES = 12
        const val MAX_BYTES = 8 * 1024 * 1024
    }
}
