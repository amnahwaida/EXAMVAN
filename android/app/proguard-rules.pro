# EXAMVAN ProGuard Rules

# Keep Gson model classes
-keep class com.examvan.app.model.** { *; }

# Keep Gson TypeToken (used for generic type deserialization)
-keep class com.google.gson.reflect.TypeToken { *; }
-keepclassmembers class * extends com.google.gson.reflect.TypeToken { *; }

# Security Crypto — MasterKey and EncryptedSharedPreferences use reflection internally
-keep class androidx.security.crypto.** { *; }

# Keep ViewModel classes
-keep class * extends androidx.lifecycle.ViewModel { *; }

# Keep AppPrefs singleton
-keep class com.examvan.app.AppPrefs { *; }

# Keep DeviceIdResolver
-keep class com.examvan.app.DeviceIdResolver { *; }

# Coroutines — required for release builds with minification
-keep class kotlinx.coroutines.** { *; }

# OkHttp (minimal keep, not blanket)
-keep class okhttp3.internal.platform.** { *; }
-dontwarn okhttp3.**
-dontwarn okio.**

# Keep annotations
-keepattributes *Annotation*
-keepattributes Signature
-keepattributes EnclosingMethod
