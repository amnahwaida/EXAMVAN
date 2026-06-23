plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "com.examvan.app"
    compileSdk = 35

    defaultConfig {
        applicationId = "com.examvan.app"
        minSdk = 21
        targetSdk = 35
        versionCode = 31
        versionName = "2.2.0"
    }

    signingConfigs {
        create("release") {
            // NOTE: Ganti dengan keystore rilis Anda sendiri!
            // Buat keystore: keytool -genkey -v -keystore release.keystore -alias releasekey -keyalg RSA -keysize 2048 -validity 10000
            // Jangan commit password ke git — gunakan environment variable
            keyAlias = System.getenv("EXAMVAN_KEY_ALIAS") ?: "releasekey"
            keyPassword = System.getenv("EXAMVAN_KEY_PASSWORD") ?: ""
            storeFile = file(System.getenv("EXAMVAN_KEYSTORE_PATH") ?: "release.keystore")
            storePassword = System.getenv("EXAMVAN_STORE_PASSWORD") ?: ""
            enableV1Signing = true
            enableV2Signing = true
            enableV3Signing = true
            enableV4Signing = true
        }
        getByName("debug") {
            keyAlias = "androiddebugkey"
            keyPassword = "android"
            storeFile = file("debug.keystore")
            storePassword = "android"
        }
    }

    flavorDimensions += "mode"
    productFlavors {
        create("student") {
            dimension = "mode"
            // Clean APK for student personal phones (Vivo, Oppo, etc.)
            // No DeviceAdminReceiver — avoids Vivo/FuntouchOS security block
        }
        create("kiosk") {
            dimension = "mode"
            // Full APK for school-owned tablets with Device Owner / Kiosk mode
            applicationIdSuffix = ".kiosk"
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro"
            )
            signingConfig = signingConfigs.getByName("release")
        }
        debug {
            signingConfig = signingConfigs.getByName("debug")
        }
    }

    buildFeatures {
        viewBinding = true
        buildConfig = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = "17"
    }
}

dependencies {
    implementation("androidx.core:core-ktx:1.13.1")
    implementation("androidx.appcompat:appcompat:1.7.0")
    implementation("com.google.android.material:material:1.12.0")
    implementation("androidx.constraintlayout:constraintlayout:2.2.0")
    implementation("androidx.swiperefreshlayout:swiperefreshlayout:1.1.0")

    // Encrypted SharedPreferences for sensitive data (questions, tokens)
    implementation("androidx.security:security-crypto:1.1.0-alpha06")

    // OkHttp for networking
    implementation("com.squareup.okhttp3:okhttp:4.12.0")

    // Gson for JSON parsing
    implementation("com.google.code.gson:gson:2.11.0")
}
