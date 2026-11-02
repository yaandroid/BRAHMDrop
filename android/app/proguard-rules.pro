# BRAHM Drop — keep WebView JS interface and activity/service entry points.
-keep class com.brahm.drop.** { *; }
-keepclassmembers class * {
    @android.webkit.JavascriptInterface <methods>;
}
