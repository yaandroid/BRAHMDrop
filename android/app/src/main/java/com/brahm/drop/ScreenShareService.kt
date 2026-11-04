package com.brahm.drop

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Context
import android.content.Intent
import android.graphics.Bitmap
import android.graphics.PixelFormat
import android.hardware.display.DisplayManager
import android.hardware.display.VirtualDisplay
import android.media.ImageReader
import android.media.projection.MediaProjection
import android.media.projection.MediaProjectionManager
import android.os.Build
import android.os.IBinder
import android.util.DisplayMetrics
import android.util.Log
import org.json.JSONArray
import java.io.ByteArrayOutputStream
import java.io.OutputStream
import java.net.HttpURLConnection
import java.net.URL
import kotlin.math.abs
import kotlin.math.max
import kotlin.math.min

/**
 * ScreenShareService captures this phone's screen with MediaProjection, streams
 * JPEG frames to the local BRAHM Drop server (which relays them to the remote
 * viewer), and polls the server for input events to inject as touch gestures.
 */
class ScreenShareService : Service() {

    companion object {
        const val EXTRA_RESULT = "result_code"
        const val EXTRA_DATA = "result_data"
        private const val CH = "brahm_screen"
        private const val NOTIF = 2
        private const val TAG = "BrahmScreen"
        private const val BASE = "http://127.0.0.1:${BrahmService.PORT}"
        @Volatile var running = false
    }

    private var projection: MediaProjection? = null
    private var vdisplay: VirtualDisplay? = null
    private var reader: ImageReader? = null
    private var w = 0
    private var h = 0
    private var lastFrame = 0L
    @Volatile private var alive = false

    // gesture state across input polls
    private var isDown = false
    private var downX = 0.0; private var downY = 0.0
    private var lastX = 0.0; private var lastY = 0.0

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        startForeground(NOTIF, notif())
        val code = intent?.getIntExtra(EXTRA_RESULT, 0) ?: 0
        val data = intent?.getParcelableExtra<Intent>(EXTRA_DATA)
        if (code == 0 || data == null) {
            stopSelf(); return START_NOT_STICKY
        }
        startCapture(code, data)
        return START_STICKY
    }

    private fun startCapture(resultCode: Int, data: Intent) {
        try {
            val mpm = getSystemService(Context.MEDIA_PROJECTION_SERVICE) as MediaProjectionManager
            projection = mpm.getMediaProjection(resultCode, data)

            val dm = DisplayMetrics()
            @Suppress("DEPRECATION")
            (getSystemService(WINDOW_SERVICE) as android.view.WindowManager).defaultDisplay.getRealMetrics(dm)
            val maxDim = 820
            val scale = min(1f, maxDim.toFloat() / max(dm.widthPixels, dm.heightPixels))
            w = ((dm.widthPixels * scale).toInt()) and 0x7FFFFFFE.toInt()
            h = ((dm.heightPixels * scale).toInt()) and 0x7FFFFFFE.toInt()
            if (w < 2) w = dm.widthPixels
            if (h < 2) h = dm.heightPixels

            reader = ImageReader.newInstance(w, h, PixelFormat.RGBA_8888, 2)
            vdisplay = projection!!.createVirtualDisplay(
                "brahm-screen", w, h, dm.densityDpi,
                DisplayManager.VIRTUAL_DISPLAY_FLAG_AUTO_MIRROR,
                reader!!.surface, null, null
            )
            reader!!.setOnImageAvailableListener({ onFrame() }, null)
            alive = true
            running = true
            Thread { inputLoop() }.start()
            Log.i(TAG, "screen capture started ${w}x${h}")
        } catch (e: Exception) {
            Log.e(TAG, "startCapture failed: ${e.message}")
            stopSelf()
        }
    }

    private fun onFrame() {
        val r = reader ?: return
        val image = try { r.acquireLatestImage() } catch (e: Exception) { null } ?: return
        try {
            val now = System.currentTimeMillis()
            if (now - lastFrame < 120) { image.close(); return } // ~8 fps cap
            lastFrame = now

            val plane = image.planes[0]
            val buffer = plane.buffer
            val pixelStride = plane.pixelStride
            val rowStride = plane.rowStride
            val rowPadding = rowStride - pixelStride * w
            val bmpW = w + if (pixelStride > 0) rowPadding / pixelStride else 0
            val bmp = Bitmap.createBitmap(bmpW, h, Bitmap.Config.ARGB_8888)
            bmp.copyPixelsFromBuffer(buffer)
            val cropped = if (bmpW != w) Bitmap.createBitmap(bmp, 0, 0, w, h) else bmp

            val baos = ByteArrayOutputStream()
            cropped.compress(Bitmap.CompressFormat.JPEG, 45, baos)
            postFrame(baos.toByteArray())

            if (cropped !== bmp) cropped.recycle()
            bmp.recycle()
        } catch (e: Exception) {
            Log.w(TAG, "frame error: ${e.message}")
        } finally {
            image.close()
        }
    }

    private fun postFrame(bytes: ByteArray) {
        try {
            val conn = URL("$BASE/api/remote/android/frame").openConnection() as HttpURLConnection
            conn.requestMethod = "POST"
            conn.doOutput = true
            conn.setRequestProperty("Content-Type", "image/jpeg")
            conn.connectTimeout = 2000
            conn.readTimeout = 2000
            val os: OutputStream = conn.outputStream
            os.write(bytes); os.flush(); os.close()
            conn.inputStream.close()
            conn.disconnect()
        } catch (e: Exception) {
            // viewer may not be connected yet; ignore
        }
    }

    private fun inputLoop() {
        while (alive) {
            try {
                val conn = URL("$BASE/api/remote/android/input").openConnection() as HttpURLConnection
                conn.connectTimeout = 2000
                conn.readTimeout = 3000
                val txt = conn.inputStream.bufferedReader().readText()
                conn.disconnect()
                val arr = org.json.JSONObject(txt).optJSONArray("events") ?: JSONArray()
                for (i in 0 until arr.length()) {
                    handleEvent(arr.getJSONObject(i))
                }
            } catch (e: Exception) {
                // ignore and keep polling
            }
            try { Thread.sleep(80) } catch (e: InterruptedException) { break }
        }
    }

    private fun handleEvent(e: org.json.JSONObject) {
        val type = e.optString("type")
        val x = e.optDouble("x", 0.0)
        val y = e.optDouble("y", 0.0)
        val acc = BrahmAccessibilityService.instance
        when (type) {
            "down" -> { isDown = true; downX = x; downY = y; lastX = x; lastY = y }
            "move" -> { lastX = x; lastY = y }
            "up" -> {
                if (isDown) {
                    isDown = false
                    val moved = abs(lastX - downX) > 0.01 || abs(lastY - downY) > 0.01
                    if (moved) acc?.swipe(downX, downY, lastX, lastY, 200)
                    else acc?.tap(downX, downY)
                }
            }
            "click" -> acc?.tap(x, y)
            "key" -> {
                when (e.optString("code")) {
                    "Escape", "BrowserBack" -> acc?.globalBack()
                    "Home" -> acc?.globalHome()
                }
            }
        }
    }

    override fun onDestroy() {
        alive = false
        running = false
        try { vdisplay?.release() } catch (_: Exception) {}
        try { reader?.close() } catch (_: Exception) {}
        try { projection?.stop() } catch (_: Exception) {}
        super.onDestroy()
    }

    private fun notif(): Notification {
        val nm = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            nm.createNotificationChannel(NotificationChannel(CH, "Screen sharing", NotificationManager.IMPORTANCE_LOW))
        }
        val b = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O)
            Notification.Builder(this, CH) else @Suppress("DEPRECATION") Notification.Builder(this)
        return b.setContentTitle("BRAHM Drop — screen sharing")
            .setContentText("Your screen is being shared")
            .setSmallIcon(R.mipmap.ic_launcher)
            .setOngoing(true).build()
    }
}
