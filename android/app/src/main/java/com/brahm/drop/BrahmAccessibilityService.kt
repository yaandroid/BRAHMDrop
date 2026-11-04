package com.brahm.drop

import android.accessibilityservice.AccessibilityService
import android.accessibilityservice.GestureDescription
import android.graphics.Path
import android.util.DisplayMetrics
import android.view.WindowManager
import android.view.accessibility.AccessibilityEvent

/**
 * BrahmAccessibilityService injects touch gestures so a remote viewer can
 * control this phone. Android only allows gesture injection from an enabled
 * AccessibilityService (same as AnyDesk/TeamViewer), so the user turns this on
 * once in Settings -> Accessibility.
 */
class BrahmAccessibilityService : AccessibilityService() {

    companion object {
        @Volatile var instance: BrahmAccessibilityService? = null
    }

    override fun onServiceConnected() {
        super.onServiceConnected()
        instance = this
    }

    override fun onDestroy() {
        if (instance === this) instance = null
        super.onDestroy()
    }

    override fun onAccessibilityEvent(event: AccessibilityEvent?) {}
    override fun onInterrupt() {}

    private fun screenSize(): Pair<Int, Int> {
        return try {
            val wm = getSystemService(WINDOW_SERVICE) as WindowManager
            val dm = DisplayMetrics()
            @Suppress("DEPRECATION")
            wm.defaultDisplay.getRealMetrics(dm)
            Pair(dm.widthPixels, dm.heightPixels)
        } catch (e: Exception) {
            Pair(1080, 1920)
        }
    }

    /** Tap at a normalised (0..1) screen position. */
    fun tap(xNorm: Double, yNorm: Double) {
        val (w, h) = screenSize()
        val x = (xNorm * w).toFloat().coerceIn(0f, (w - 1).toFloat())
        val y = (yNorm * h).toFloat().coerceIn(0f, (h - 1).toFloat())
        val path = Path().apply { moveTo(x, y) }
        dispatch(path, 0, 60)
    }

    /** Swipe/drag from one normalised point to another. */
    fun swipe(x1: Double, y1: Double, x2: Double, y2: Double, durationMs: Long) {
        val (w, h) = screenSize()
        val sx = (x1 * w).toFloat().coerceIn(0f, (w - 1).toFloat())
        val sy = (y1 * h).toFloat().coerceIn(0f, (h - 1).toFloat())
        val ex = (x2 * w).toFloat().coerceIn(0f, (w - 1).toFloat())
        val ey = (y2 * h).toFloat().coerceIn(0f, (h - 1).toFloat())
        val path = Path().apply { moveTo(sx, sy); lineTo(ex, ey) }
        dispatch(path, 0, durationMs.coerceIn(20, 2000))
    }

    /** Hardware-style global actions the remote side may ask for. */
    fun globalBack() = performGlobalAction(GLOBAL_ACTION_BACK)
    fun globalHome() = performGlobalAction(GLOBAL_ACTION_HOME)
    fun globalRecents() = performGlobalAction(GLOBAL_ACTION_RECENTS)

    private fun dispatch(path: Path, startTime: Long, duration: Long) {
        try {
            val stroke = GestureDescription.StrokeDescription(path, startTime, duration)
            val gesture = GestureDescription.Builder().addStroke(stroke).build()
            dispatchGesture(gesture, null, null)
        } catch (e: Exception) {
            // ignore — gesture may be rejected if coordinates are invalid
        }
    }
}
