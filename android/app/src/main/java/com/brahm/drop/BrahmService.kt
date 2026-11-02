package com.brahm.drop

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Context
import android.content.Intent
import android.net.wifi.WifiManager
import android.os.Build
import android.os.Environment
import android.os.IBinder
import android.util.Log
import java.io.File

/**
 * BrahmService runs the bundled BRAHM Drop server (a native Go binary shipped
 * as libbrahmdrop.so) as a foreground service, and holds a Wi-Fi multicast lock
 * so UDP device discovery works.
 */
class BrahmService : Service() {

    companion object {
        const val PORT = 53330
        private const val CHANNEL = "brahmdrop"
        private const val NOTIF_ID = 1
        private const val TAG = "BrahmService"
        @Volatile var running = false
    }

    private var process: Process? = null
    private var multicastLock: WifiManager.MulticastLock? = null

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        startForeground(NOTIF_ID, buildNotification())
        acquireMulticast()
        startServer()
        return START_STICKY
    }

    private fun acquireMulticast() {
        try {
            val wifi = applicationContext.getSystemService(Context.WIFI_SERVICE) as WifiManager
            multicastLock = wifi.createMulticastLock("brahmdrop").apply {
                setReferenceCounted(true)
                acquire()
            }
        } catch (e: Exception) {
            Log.w(TAG, "multicast lock failed: ${e.message}")
        }
    }

    private fun startServer() {
        if (running) return
        try {
            val bin = File(applicationInfo.nativeLibraryDir, "libbrahmdrop.so")
            if (!bin.exists()) {
                Log.e(TAG, "server binary missing at ${bin.absolutePath}")
                return
            }
            val saveDir = chooseSaveDir()
            val name = "${Build.MANUFACTURER} ${Build.MODEL}".trim()

            val pb = ProcessBuilder(
                bin.absolutePath,
                "--no-browser",
                "--auto-accept",
                "--port", PORT.toString(),
                "--dir", saveDir.absolutePath,
                "--name", name
            )
            pb.redirectErrorStream(true)
            pb.environment()["HOME"] = filesDir.absolutePath
            process = pb.start()
            running = true

            // Drain output to logcat so we can diagnose issues.
            Thread {
                try {
                    process?.inputStream?.bufferedReader()?.forEachLine { Log.i(TAG, it) }
                } catch (_: Exception) {
                }
                running = false
            }.start()
        } catch (e: Exception) {
            Log.e(TAG, "failed to start server: ${e.message}")
            running = false
        }
    }

    /**
     * Pick where received files are saved. If "all files access" is granted,
     * use the public Downloads/BRAHM Drop folder so files show up in the phone's
     * file manager. Otherwise fall back to the app-private external folder.
     */
    private fun chooseSaveDir(): File {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R && Environment.isExternalStorageManager()) {
            val dl = File(
                Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_DOWNLOADS),
                "BRAHM Drop"
            )
            if (dl.mkdirs() || dl.isDirectory) return dl
        }
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.R) {
            val dl = File(
                Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_DOWNLOADS),
                "BRAHM Drop"
            )
            if (dl.mkdirs() || dl.isDirectory) return dl
        }
        return File(getExternalFilesDir(null), "BRAHM Drop").apply { mkdirs() }
    }

    override fun onDestroy() {
        try { process?.destroy() } catch (_: Exception) {}
        try { multicastLock?.release() } catch (_: Exception) {}
        running = false
        super.onDestroy()
    }

    private fun buildNotification(): Notification {
        val nm = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val ch = NotificationChannel(CHANNEL, getString(R.string.service_channel),
                NotificationManager.IMPORTANCE_LOW)
            nm.createNotificationChannel(ch)
        }
        val builder = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O)
            Notification.Builder(this, CHANNEL) else @Suppress("DEPRECATION") Notification.Builder(this)
        return builder
            .setContentTitle(getString(R.string.app_name))
            .setContentText(getString(R.string.service_text))
            .setSmallIcon(R.mipmap.ic_launcher)
            .setOngoing(true)
            .build()
    }
}
