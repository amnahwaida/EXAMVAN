# EXAMVAN ProGuard Rules

# Keep Gson model classes
-keep class com.examvan.app.model.** { *; }

# OkHttp
-dontwarn okhttp3.**
-dontwarn okio.**
-keep class okhttp3.** { *; }

# Keep annotations
-keepattributes *Annotation*
-keepattributes Signature
