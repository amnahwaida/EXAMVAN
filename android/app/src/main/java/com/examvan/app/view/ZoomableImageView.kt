package com.examvan.app.view

import android.content.Context
import android.graphics.Matrix
import android.graphics.PointF
import android.util.AttributeSet
import android.view.GestureDetector
import android.view.MotionEvent
import android.view.ScaleGestureDetector
import android.view.View
import androidx.appcompat.widget.AppCompatImageView

/**
 * Custom ZoomableImageView that supports:
 * 1. Pinch to Zoom
 * 2. Drag & Pan
 * 3. Double Tap to Zoom
 * 4. Boundary constraints (keeps image within screen boundaries)
 */
class ZoomableImageView @JvmOverloads constructor(
    context: Context,
    attrs: AttributeSet? = null,
    defStyleAttr: Int = 0
) : AppCompatImageView(context, attrs, defStyleAttr), View.OnTouchListener,
    ScaleGestureDetector.OnScaleGestureListener {

    private var myMatrix = Matrix()
    private var mode = NONE

    private var last = PointF()
    private var start = PointF()
    private var minScale = 1f
    private var maxScale = 4f

    private var width = 0f
    private var height = 0f
    private var saveScale = 1f
    private var right = 0f
    private var bottom = 0f
    private var origWidth = 0f
    private var origHeight = 0f
    // Fit-to-screen scale computed in onMeasure, used by double-tap zoom-out
    private var fitScreenScale = 1f

    private var mScaleDetector: ScaleGestureDetector = ScaleGestureDetector(context, this)
    private var mGestureDetector: GestureDetector

    companion object {
        private const val NONE = 0
        private const val DRAG = 1
        private const val ZOOM = 2
        private const val CLICK = 3
        private const val SCROLL_THRESHOLD = 0.4f // 40% of view width triggers navigation
        private const val FLING_THRESHOLD = 100f
    }

    interface OnSwipeListener {
        fun onSwipeLeft()
        fun onSwipeRight()
    }

    var swipeListener: OnSwipeListener? = null

    init {
        super.setClickable(true)
        setOnTouchListener(this)
        imageMatrix = myMatrix
        scaleType = ScaleType.MATRIX
        mGestureDetector = GestureDetector(context, object : GestureDetector.SimpleOnGestureListener() {
            override fun onDoubleTap(e: MotionEvent): Boolean {
                // Toggle between fit-to-screen and 2.5x zoom
                val nearFitScale = Math.abs(saveScale - fitScreenScale) < 0.05f
                val targetScale = if (nearFitScale || saveScale <= fitScreenScale + 0.05f) 2.5f else fitScreenScale
                val scaleFactor = targetScale / saveScale
                zoomTo(scaleFactor, e.x, e.y)
                return true
            }

            override fun onScroll(
                e1: MotionEvent?, e2: MotionEvent,
                distanceX: Float, distanceY: Float
            ): Boolean {
                // Slow-drag fallback: trigger page navigation when dragged past threshold
                if (Math.abs(saveScale - 1f) < 0.01f && mode == DRAG && e1 != null) {
                    val diffX = e2.x - e1.x
                    val diffY = e2.y - e1.y
                    if (Math.abs(diffX) > Math.abs(diffY) &&
                        Math.abs(diffX) > width * SCROLL_THRESHOLD) {
                        if (diffX < 0) {
                            swipeListener?.onSwipeLeft()
                        } else {
                            swipeListener?.onSwipeRight()
                        }
                        return true
                    }
                }
                return false
            }

            override fun onFling(
                e1: MotionEvent?,
                e2: MotionEvent,
                velocityX: Float,
                velocityY: Float
            ): Boolean {
                if (Math.abs(saveScale - 1f) < 0.01f) {
                    val x1 = e1?.x ?: return false
                    val y1 = e1.y
                    val diffX = e2.x - x1
                    val diffY = e2.y - y1
                    if (Math.abs(diffX) > Math.abs(diffY)) {
                        if (Math.abs(diffX) > 100 && Math.abs(velocityX) > 100) {
                            if (diffX < 0) {
                                swipeListener?.onSwipeLeft()
                            } else {
                                swipeListener?.onSwipeRight()
                            }
                            return true
                        }
                    }
                }
                return false
            }
        })
    }

    private fun zoomTo(scaleFactor: Float, focusX: Float, focusY: Float) {
        val origScale = saveScale
        saveScale *= scaleFactor
        if (saveScale > maxScale) {
            saveScale = maxScale
            val f = maxScale / origScale
            myMatrix.postScale(f, f, focusX, focusY)
        } else if (saveScale < minScale) {
            saveScale = minScale
            val f = minScale / origScale
            myMatrix.postScale(f, f, focusX, focusY)
        } else {
            myMatrix.postScale(scaleFactor, scaleFactor, focusX, focusY)
        }
        fixTrans()
    }

    override fun onTouch(v: View, event: MotionEvent): Boolean {
        mScaleDetector.onTouchEvent(event)
        mGestureDetector.onTouchEvent(event)
        val curr = PointF(event.x, event.y)

        when (event.action) {
            MotionEvent.ACTION_DOWN -> {
                last.set(curr)
                start.set(last)
                mode = DRAG
            }
            MotionEvent.ACTION_MOVE -> {
                if (mode == DRAG) {
                    val deltaX = curr.x - last.x
                    val deltaY = curr.y - last.y
                    val fixTransX = getFixDragTrans(deltaX, width, origWidth * saveScale)
                    val fixTransY = getFixDragTrans(deltaY, height, origHeight * saveScale)
                    myMatrix.postTranslate(fixTransX, fixTransY)
                    fixTrans()
                    last.set(curr.x, curr.y)
                }
            }
            MotionEvent.ACTION_UP -> {
                mode = NONE
                val xDiff = Math.abs(curr.x - start.x).toInt()
                val yDiff = Math.abs(curr.y - start.y).toInt()
                if (xDiff < CLICK && yDiff < CLICK) {
                    performClick()
                }
            }
            MotionEvent.ACTION_POINTER_UP -> {
                mode = NONE
            }
        }

        imageMatrix = myMatrix
        invalidate()
        return true
    }

    override fun performClick(): Boolean {
        return super.performClick()
    }

    override fun onScaleBegin(detector: ScaleGestureDetector): Boolean {
        mode = ZOOM
        return true
    }

    override fun onScale(detector: ScaleGestureDetector): Boolean {
        var mScaleFactor = detector.scaleFactor
        val origScale = saveScale
        saveScale *= mScaleFactor
        if (saveScale > maxScale) {
            saveScale = maxScale
            mScaleFactor = maxScale / origScale
        } else if (saveScale < minScale) {
            saveScale = minScale
            mScaleFactor = minScale / origScale
        }

        if (origWidth * saveScale <= width || origHeight * saveScale <= height) {
            myMatrix.postScale(mScaleFactor, mScaleFactor, width / 2, height / 2)
        } else {
            myMatrix.postScale(mScaleFactor, mScaleFactor, detector.focusX, detector.focusY)
        }
        fixTrans()
        return true
    }

    override fun onScaleEnd(detector: ScaleGestureDetector) {}

    private fun fixTrans() {
        val m = FloatArray(9)
        myMatrix.getValues(m)
        val transX = m[Matrix.MTRANS_X]
        val transY = m[Matrix.MTRANS_Y]
        val fixTransX = getFixTrans(transX, width, origWidth * saveScale)
        val fixTransY = getFixTrans(transY, height, origHeight * saveScale)
        if (fixTransX != 0f || fixTransY != 0f) {
            myMatrix.postTranslate(fixTransX, fixTransY)
        }
    }

    private fun getFixTrans(trans: Float, viewSize: Float, contentSize: Float): Float {
        val minTrans: Float
        val maxTrans: Float
        if (contentSize <= viewSize) {
            minTrans = 0f
            maxTrans = viewSize - contentSize
        } else {
            minTrans = viewSize - contentSize
            maxTrans = 0f
        }
        if (trans < minTrans) return -trans + minTrans
        if (trans > maxTrans) return -trans + maxTrans
        return 0f
    }

    private fun getFixDragTrans(delta: Float, viewSize: Float, contentSize: Float): Float {
        if (contentSize <= viewSize) {
            return 0f
        }
        return delta
    }

    fun resetZoom() {
        saveScale = 1f
        fitScreenScale = 1f
        myMatrix.reset()
        imageMatrix = myMatrix
        invalidate()
        requestLayout()
    }

    override fun onMeasure(widthMeasureSpec: Int, heightMeasureSpec: Int) {
        super.onMeasure(widthMeasureSpec, heightMeasureSpec)
        width = MeasureSpec.getSize(widthMeasureSpec).toFloat()
        height = MeasureSpec.getSize(heightMeasureSpec).toFloat()

        val drawable = drawable ?: return
        if (drawable.intrinsicWidth == 0 || drawable.intrinsicHeight == 0) return

        val bmWidth = drawable.intrinsicWidth
        val bmHeight = drawable.intrinsicHeight

        val scaleX = width / bmWidth
        val scaleY = height / bmHeight
        val scale = Math.min(scaleX, scaleY)
        myMatrix.setScale(scale, scale)

        val redundantYSpace = (height - scale * bmHeight) / 2f
        val redundantXSpace = (width - scale * bmWidth) / 2f

        myMatrix.postTranslate(redundantXSpace, redundantYSpace)

        origWidth = width - 2 * redundantXSpace
        origHeight = height - 2 * redundantYSpace
        saveScale = 1f
        fitScreenScale = 1f // Reset saved fit-to-screen scale
        imageMatrix = myMatrix
        fixTrans()
    }
}
