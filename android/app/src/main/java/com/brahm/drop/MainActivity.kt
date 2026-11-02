package com.brahm.drop

import android.Manifest
import android.annotation.SuppressLint
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.webkit.ValueCallback
import android.webkit.WebChromeClient
import android.webkit.WebResourceError
import android.webkit.WebResourceRequest
import android.webkit.WebSettings
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.core.app.ActivityCompat
import androidx.core.content.ContextCompat

/**
 * MainActivity starts the background server service and shows the BRAHM Drop
 * web UI in a WebView pointed at the local server.
 */
class MainActivity : AppCompatActivity() {

    private lateinit var web: WebView
    private var fileCallback: ValueCallback<Array<Uri>>? = null

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

    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        requestNotifPermissionIfNeeded()
        startServer()

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
                // Server may still be booting — retry shortly.
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

        // Give the server a moment to bind, then load.
        Handler(Looper.getMainLooper()).postDelayed({ loadApp() }, 900)
    }

    private fun loadApp() {
        web.loadUrl("http://127.0.0.1:${BrahmService.PORT}/")
    }

    private fun startServer() {
        val svc = Intent(this, BrahmService::class.java)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            startForegroundService(svc)
        } else {
            startService(svc)
        }
    }

    private fun requestNotifPermissionIfNeeded() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            if (ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS)
                != PackageManager.PERMISSION_GRANTED
            ) {
                ActivityCompat.requestPermissions(
                    this, arrayOf(Manifest.permission.POST_NOTIFICATIONS), 1
                )
            }
        }
    }

    override fun onBackPressed() {
        if (web.canGoBack()) web.goBack() else super.onBackPressed()
    }
}
