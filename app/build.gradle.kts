plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

val releaseKeyStorePath = System.getenv("ANDROID_KEYSTORE_PATH")
val releaseKeyStorePassword = System.getenv("ANDROID_KEYSTORE_PASSWORD")
val releaseKeyAlias = System.getenv("ANDROID_KEY_ALIAS")
val releaseKeyPassword = System.getenv("ANDROID_KEY_PASSWORD")
val buildVersionCode = System.getenv("VERSION_CODE")?.toIntOrNull() ?: 1
val buildVersionName = System.getenv("VERSION_NAME") ?: "1.0"

android {
    namespace = "by.minsk.voicecontrol"
    compileSdk = 35

    defaultConfig {
        applicationId = "by.minsk.voicecontrol"
        minSdk = 26
        targetSdk = 35
        versionCode = buildVersionCode
        versionName = buildVersionName
    }

    signingConfigs {
        create("release") {
            if (!releaseKeyStorePath.isNullOrBlank()) {
                storeFile = file(releaseKeyStorePath)
            }
            storePassword = releaseKeyStorePassword
            keyAlias = releaseKeyAlias
            keyPassword = releaseKeyPassword
        }
    }

    buildTypes {
        getByName("release") {
            signingConfig = signingConfigs.getByName("release")
            isMinifyEnabled = false
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = "17"
    }
}
