package com.brahm.drop

import android.Manifest
import android.annotation.SuppressLint
import android.content.Intent
import android.content.pm.PackageManager
import android.content.Context
import android.media.projection.MediaProjectionManager
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.Environment
import android.os.Handler
import android.os.Looper
import android.provider.Settings
import android.webkit.JavascriptInterface
import android.webkit.MimeTypeMap
import android.webkit.ValueCallback
import android.webkit.WebChromeClient
import android.webkit.WebResourceError
import android.webkit.WebResourceRequest
import android.webkit.WebSettings
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.ContextCompat
import androidx.core.content.FileProvider
import java.io.File

/**
 * MainActivity requests the media/file permissions from code, starts the
 * background server service, and shows the BRAHM Drop web UI in a WebView.
 */
class MainActivity : AppCompatActivity() {

    private lateinit var web: WebView
    private var fileCallback: ValueCallback<Array<Uri>>? = null
    private var hadAllFiles = false
    private var serverStarted = false
    private var loadRetries = 0

    // --- file picker for the WebView "Choose files" button ---
    private val filePicker = registerForActivityResult(
        ActivityResultContracts.StartActivityForResult()
    ) { result ->
        val uris: Array<Uri>? = result.data?.let { data ->
            when {
                data.clipData != null -> Array(data.clipData!!.itemCount) { i ->
                    data.clipData!!.getItemAt(i).uri
                }
                data.data != null -> arrayOf(data.data!!)
                else -> null
            }
        }
        fileCallback?.onReceiveValue(uris ?: emptyArray())
        fileCallback = null
    }

    // --- runtime permission request (media / storage) ---
    private val permReq = registerForActivityResult(
        ActivityResultContracts.RequestMultiplePermissions()
    ) {
        // Whatever the result, continue: picking files still works via the
        // system picker, and all-files access is requested separately.
        ensureAllFilesAccess()
    }

    // --- "All files access" settings return ---
    private val allFilesReq = registerForActivityResult(
        ActivityResultContracts.StartActivityForResult()
    ) {
        // Restart the server so it picks the public Downloads folder if granted.
        restartServerIfNeeded()
    }

    // --- MediaProjection (screen share) permission ---
    private val screenReq = registerForActivityResult(
        ActivityResultContracts.StartActivityForResult()
    ) { result ->
        if (result.resultCode == RESULT_OK && result.data != null) {
            val svc = Intent(this, ScreenShareService::class.java).apply {
                putExtra(ScreenShareService.EXTRA_RESULT, result.resultCode)
                putExtra(ScreenShareService.EXTRA_DATA, result.data)
            }
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) startForegroundService(svc) else startService(svc)
            web.post { web.evaluateJavascript("window.onScreenShareStarted && window.onScreenShareStarted()", null) }
        }
    }

    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        requestInitialPermissions()

        web = WebView(this)
        setContentView(web)

        web.settings.apply {
            javaScriptEnabled = true
            domStorageEnabled = true
            allowFileAccess = true
            allowContentAccess = true
            cacheMode = WebSettings.LOAD_NO_CACHE
            mediaPlaybackRequiresUserGesture = false
        }

        // Bridge so the web UI can open a received file with the right app.
        web.addJavascriptInterface(WebBridge(), "AndroidBridge")

        web.webViewClient = object : WebViewClient() {
            // Only retry when the MAIN page fails to load (e.g. the local server
            // is still booting). NEVER reload for a sub-resource hiccup such as
            // the live-events stream — that caused the page to blink and reset
            // to the Send tab.
            override fun onReceivedError(
                view: WebView?, request: WebResourceRequest?, error: WebResourceError?
            ) {
                if (request?.isForMainFrame == true && loadRetries < 12) {
                    loadRetries++
                    Handler(Looper.getMainLooper()).postDelayed({ loadApp() }, 600)
                }
            }

            override fun onPageFinished(view: WebView?, url: String?) {
                // Loaded successfully — stop retrying.
                if (url != null && url.startsWith("http://127.0.0.1")) loadRetries = 0
            }

            // Keep all navigation inside the WebView (never spawn a browser).
            override fun shouldOverrideUrlLoading(
                view: WebView?, request: WebResourceRequest?
            ): Boolean = false
        }

        web.webChromeClient = object : WebChromeClient() {
            override fun onShowFileChooser(
                webView: WebView?,
                filePathCallback: ValueCallback<Array<Uri>>?,
                fileChooserParams: FileChooserParams?
            ): Boolean {
                fileCallback?.onReceiveValue(null)
                fileCallback = filePathCallback
                val intent = Intent(Intent.ACTION_GET_CONTENT).apply {
                    type = "*/*"
                    addCategory(Intent.CATEGORY_OPENABLE)
                    putExtra(Intent.EXTRA_ALLOW_MULTIPLE, true)
                    putExtra(
                        Intent.EXTRA_MIME_TYPES,
                        arrayOf("image/*", "video/*", "audio/*", "application/*", "text/*", "*/*")
                    )
                }
                return try {
                    filePicker.launch(Intent.createChooser(intent, "Choose files"))
                    true
                } catch (e: Exception) {
                    fileCallback = null
                    false
                }
            }
        }

        hadAllFiles = hasAllFilesAccess()
        startServer()
        Handler(Looper.getMainLooper()).postDelayed({ loadApp() }, 900)
    }

    override fun onResume() {
        super.onResume()
        restartServerIfNeeded()
    }

    private fun loadApp() {
        web.loadUrl("http://127.0.0.1:${BrahmService.PORT}/")
    }

    // ---- permissions ----

    private fun requestInitialPermissions() {
        val perms = mutableListOf<String>()
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            // Android 13+
            perms += Manifest.permission.READ_MEDIA_IMAGES
            perms += Manifest.permission.READ_MEDIA_VIDEO
            perms += Manifest.permission.READ_MEDIA_AUDIO
            perms += Manifest.permission.POST_NOTIFICATIONS
        } else {
            perms += Manifest.permission.READ_EXTERNAL_STORAGE
            if (Build.VERSION.SDK_INT <= Build.VERSION_CODES.P) {
                perms += Manifest.permission.WRITE_EXTERNAL_STORAGE
            }
        }
        val missing = perms.filter {
            ContextCompat.checkSelfPermission(this, it) != PackageManager.PERMISSION_GRANTED
        }
        if (missing.isNotEmpty()) {
            permReq.launch(missing.toTypedArray())
        } else {
            ensureAllFilesAccess()
        }
    }

    private fun hasAllFilesAccess(): Boolean {
        return if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
            Environment.isExternalStorageManager()
        } else {
            ContextCompat.checkSelfPermission(
                this, Manifest.permission.WRITE_EXTERNAL_STORAGE
            ) == PackageManager.PERMISSION_GRANTED || Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q
        }
    }

    /** On Android 11+, send the user to the "All files access" screen once. */
    private fun ensureAllFilesAccess() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R && !Environment.isExternalStorageManager()) {
            try {
                val intent = Intent(
                    Settings.ACTION_MANAGE_APP_ALL_FILES_ACCESS_PERMISSION,
                    Uri.parse("package:$packageName")
                )
                allFilesReq.launch(intent)
            } catch (e: Exception) {
                try {
                    allFilesReq.launch(Intent(Settings.ACTION_MANAGE_ALL_FILES_ACCESS_PERMISSION))
                } catch (_: Exception) {
                }
            }
        }
    }

    // ---- background server service ----

    private fun startServer() {
        val svc = Intent(this, BrahmService::class.java)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            startForegroundService(svc)
        } else {
            startService(svc)
        }
        serverStarted = true
    }

    /** If "all files access" just became available, restart so received files
     *  go to the public Downloads folder. */
    private fun restartServerIfNeeded() {
        val now = hasAllFilesAccess()
        if (serverStarted && now && !hadAllFiles) {
            hadAllFiles = true
            stopService(Intent(this, BrahmService::class.java))
            Handler(Looper.getMainLooper()).postDelayed({
                startServer()
                loadApp()
            }, 500)
        } else {
            hadAllFiles = now
        }
    }

    override fun onBackPressed() {
        if (web.canGoBack()) web.goBack() else super.onBackPressed()
    }

    /** JavaScript bridge: open files, control screen sharing, accessibility. */
    inner class WebBridge {
        @JavascriptInterface
        fun openFile(path: String) {
            runOnUiThread { doOpen(path) }
        }

        // true so the web UI knows it's running inside the Android app.
        @JavascriptInterface
        fun isAndroid(): Boolean = true

        @JavascriptInterface
        fun startScreenShare() {
            runOnUiThread {
                val mpm = getSystemService(Context.MEDIA_PROJECTION_SERVICE) as MediaProjectionManager
                screenReq.launch(mpm.createScreenCaptureIntent())
            }
        }

        @JavascriptInterface
        fun stopScreenShare() {
            runOnUiThread { stopService(Intent(this@MainActivity, ScreenShareService::class.java)) }
        }

        @JavascriptInterface
        fun isControlEnabled(): Boolean = BrahmAccessibilityService.instance != null

        @JavascriptInterface
        fun openAccessibilitySettings() {
            runOnUiThread {
                try {
                    startActivity(Intent(Settings.ACTION_ACCESSIBILITY_SETTINGS))
                } catch (e: Exception) {
                }
            }
        }
    }

    private fun doOpen(path: String) {
        try {
            val f = File(path)
            if (!f.exists()) {
                android.widget.Toast.makeText(this, "File not found", android.widget.Toast.LENGTH_SHORT).show()
                return
            }
            val uri = FileProvider.getUriForFile(this, "$packageName.fileprovider", f)
            val ext = f.extension.lowercase()
            val mime = MimeTypeMap.getSingleton().getMimeTypeFromExtension(ext) ?: "*/*"
            val view = Intent(Intent.ACTION_VIEW).apply {
                setDataAndType(uri, mime)
                addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK)
            }
            startActivity(Intent.createChooser(view, "Open with").apply {
                addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            })
        } catch (e: Exception) {
            android.widget.Toast.makeText(this, "Could not open: ${e.message}", android.widget.Toast.LENGTH_SHORT).show()
        }
    }
}
