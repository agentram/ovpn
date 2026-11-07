plugins {
    id("com.android.application") version "8.13.2"
    id("org.jetbrains.kotlin.android") version "2.3.0"
    id("org.jetbrains.kotlin.plugin.compose") version "2.3.0"
}

android {
    namespace = "io.github.agentram.ovpn.tunnel"
    compileSdk = 36
    buildToolsVersion = "36.0.0"

    defaultConfig {
        applicationId = "io.github.agentram.ovpn.tunnel"
        minSdk = 29
        targetSdk = 36
        versionCode = 1
        versionName = "0.1.0"
        ndk { abiFilters += listOf("arm64-v8a", "x86_64") }
    }

    buildFeatures {
        compose = true
        buildConfig = true
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlin { jvmToolchain(17) }

    // CI and local development can build an unsigned release bundle. Upload
    // signing is enabled only when an external keystore is explicitly supplied.
    val uploadStore = providers.environmentVariable("OVPN_ANDROID_KEYSTORE").orNull
    if (uploadStore != null) {
        signingConfigs.create("upload") {
            storeFile = file(uploadStore)
            storePassword = providers.environmentVariable("OVPN_ANDROID_STORE_PASSWORD").get()
            keyAlias = providers.environmentVariable("OVPN_ANDROID_KEY_ALIAS").get()
            keyPassword = providers.environmentVariable("OVPN_ANDROID_KEY_PASSWORD").get()
        }
        buildTypes.getByName("release").signingConfig = signingConfigs.getByName("upload")
    }
}

dependencies {
    implementation(files("libs/androidbridge.aar"))
    implementation(platform("androidx.compose:compose-bom:2025.10.01"))
    implementation("androidx.activity:activity-compose:1.11.0")
    implementation("androidx.appcompat:appcompat:1.7.1")
    implementation("androidx.compose.material3:material3")
    implementation("androidx.lifecycle:lifecycle-viewmodel-compose:2.9.4")
    implementation("androidx.lifecycle:lifecycle-runtime-compose:2.9.4")
    implementation("com.journeyapps:zxing-android-embedded:4.3.0")
    implementation("com.squareup.okhttp3:okhttp:4.12.0")
    testImplementation("junit:junit:4.13.2")
    testImplementation("org.json:json:20250517")
    testImplementation("com.squareup.okhttp3:mockwebserver:4.12.0")
    testImplementation("com.squareup.okhttp3:okhttp-tls:4.12.0")
}
