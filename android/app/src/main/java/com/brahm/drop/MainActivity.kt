package com.brahm.drop

import android.Manifest
import android.annotation.SuppressLint
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.Environment
import android.os.Handler
import android.os.Looper
import android.provider.Settings
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

/**
 * MainActivity requests the media/file permissions from code, starts the
 * background server service, and shows the BRAHM Drop web UI in a WebView.
 */
class MainActivity : AppCompatActivity() {

    private lateinit var web: WebView
    private var fileCallback: ValueCallback<Array<Uri>>? = null
    private var hadAllFiles = false
    private var serverStarted = false

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

        web.webViewClient = object : WebViewClient() {
            override fun onReceivedError(
                view: WebView?, request: WebResourceRequest?, error: WebResourceError?
            ) {
                Handler(Looper.getMainLooper()).postDelayed({ loadApp() }, 700)
            }
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
}
