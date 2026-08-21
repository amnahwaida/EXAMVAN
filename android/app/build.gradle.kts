plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "com.examvan.app"
    compileSdk = 35

    defaultConfig {
        applicationId = "com.examvan.app"
        minSdk = 24
        targetSdk = 35
        versionCode = 38
        versionName = "2.7.2"
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }

    testOptions {
        // JVM unit tests call android.util.Log (via ApiClient) — no-op stubs.
        unitTests.isReturnDefaultValues = true
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
        isCoreLibraryDesugaringEnabled = true
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = "17"
    }
}

dependencies {
    implementation("androidx.core:core-ktx:1.13.1")
    implementation("androidx.activity:activity-ktx:1.9.3")
    implementation("androidx.appcompat:appcompat:1.7.0")
    implementation("com.google.android.material:material:1.12.0")
    implementation("androidx.constraintlayout:constraintlayout:2.1.4")
    implementation("androidx.swiperefreshlayout:swiperefreshlayout:1.1.0")

    // Coroutines — already available transitively, declared explicitly for visibility
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.8.1")

    // Lifecycle ViewModel + Runtime for ViewModel and lifecycleScope
    implementation("androidx.lifecycle:lifecycle-viewmodel-ktx:2.6.2")
    implementation("androidx.lifecycle:lifecycle-runtime-ktx:2.6.2")

    // Encrypted SharedPreferences for sensitive data (questions, tokens)
    // Note: 1.1.0-alpha06 is the only cached version available in this build env.
    // For production, prefer the latest stable release from Maven Central.
    implementation("androidx.security:security-crypto:1.1.0-alpha06")

    // OkHttp for networking
    implementation("com.squareup.okhttp3:okhttp:4.12.0")

    // Gson for JSON parsing
    implementation("com.google.code.gson:gson:2.11.0")

    // Core library desugaring — enables java.time.* on API < 26 (minSdk = 24)
    coreLibraryDesugaring("com.android.tools:desugar_jdk_libs:2.0.4")

    // JVM unit tests (test source set: src/test)
    testImplementation("junit:junit:4.13.2")
    // MockWebServer — simulates the EXAMVAN server contract in JVM tests
    // (ApiClientFlowSimulationTest). Same version as okhttp.
    testImplementation("com.squareup.okhttp3:mockwebserver:4.12.0")
    // Real org.json on the JVM test classpath — otherwise the android.jar
    // stub (with isReturnDefaultValues) returns null from every method.
    testImplementation("org.json:json:20240303")

    // Instrumentation tests (androidTest source set) — run on device/emulator
    // (Espresso). These simulate the closed-test student flow against a local
    // MockWebServer; see android/app/src/androidTest.
    androidTestImplementation("androidx.test:runner:1.6.2")
    androidTestImplementation("androidx.test:rules:1.6.1")
    androidTestImplementation("androidx.test.ext:junit:1.2.1")
    androidTestImplementation("androidx.test.espresso:espresso-core:3.6.1")
    androidTestImplementation("com.squareup.okhttp3:mockwebserver:4.12.0")
}
