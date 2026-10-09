package by.minsk.voicecontrol

import android.app.Activity
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.view.Gravity
import android.view.ViewGroup
import android.view.WindowManager
import android.widget.Button
import android.widget.LinearLayout
import android.widget.TextView
import androidx.core.content.ContextCompat
import kotlin.math.roundToInt

/** Minimal incoming/ongoing call screen required by the dialer role. */
class InCallActivity : Activity() {
    private lateinit var titleView: TextView
    private lateinit var callerView: TextView
    private lateinit var answerButton: Button
    private lateinit var declineButton: Button
    private val mainHandler = Handler(Looper.getMainLooper())
    private var receiverRegistered = false

    private val stateReceiver = object : BroadcastReceiver() {
        override fun onReceive(context: Context?, intent: Intent?) {
            if (intent?.action == AppPrefs.ACTION_CALL_STATE) refresh()
        }
    }

    private val refreshTask = object : Runnable {
        override fun run() {
            refresh()
            mainHandler.postDelayed(this, 700)
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O_MR1) {
            setShowWhenLocked(true)
            setTurnScreenOn(true)
        } else {
            @Suppress("DEPRECATION")
            window.addFlags(WindowManager.LayoutParams.FLAG_SHOW_WHEN_LOCKED or WindowManager.LayoutParams.FLAG_TURN_SCREEN_ON)
        }
        window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        window.statusBarColor = 0xFF17223B.toInt()
        window.navigationBarColor = 0xFF17223B.toInt()
        buildScreen()
        refresh()
    }

    override fun onStart() {
        super.onStart()
        val filter = IntentFilter(AppPrefs.ACTION_CALL_STATE)
        ContextCompat.registerReceiver(this, stateReceiver, filter, ContextCompat.RECEIVER_NOT_EXPORTED)
        receiverRegistered = true
        mainHandler.post(refreshTask)
    }

    override fun onStop() {
        mainHandler.removeCallbacks(refreshTask)
        if (receiverRegistered) {
            unregisterReceiver(stateReceiver)
            receiverRegistered = false
        }
        super.onStop()
    }

    private fun buildScreen() {
        val root = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            gravity = Gravity.CENTER
            setPadding(dp(26), dp(32), dp(26), dp(32))
            setBackgroundColor(0xFF17223B.toInt())
        }
        titleView = TextView(this).apply {
            textSize = 18f
            setTextColor(0xFFB7C7EA.toInt())
            gravity = Gravity.CENTER
        }
        callerView = TextView(this).apply {
            textSize = 28f
            setTextColor(android.graphics.Color.WHITE)
            gravity = Gravity.CENTER
            setPadding(0, dp(18), 0, dp(32))
        }
        answerButton = Button(this).apply {
            text = "Ответить"
            isAllCaps = false
            setOnClickListener {
                VoiceCallRegistry.answer()
                refresh()
            }
        }
        declineButton = Button(this).apply {
            text = "Отклонить"
            isAllCaps = false
            setOnClickListener {
                if (VoiceCallRegistry.ringingCall() != null) VoiceCallRegistry.reject()
                else VoiceCallRegistry.disconnect()
                finish()
            }
        }
        root.addView(titleView, fullWidthWrap())
        root.addView(callerView, fullWidthWrap())
        root.addView(answerButton, fullWidthWrap())
        root.addView(declineButton, fullWidthWrap().apply { topMargin = dp(12) })
        setContentView(root)
    }

    private fun refresh() {
        if (!::titleView.isInitialized) return
        val call = VoiceCallRegistry.currentCall()
        if (call == null) {
            titleView.text = "Звонок завершён"
            callerView.text = ""
            answerButton.visibility = android.view.View.GONE
            declineButton.visibility = android.view.View.GONE
            mainHandler.postDelayed({ if (!isFinishing) finish() }, 900)
            return
        }
        titleView.text = VoiceCallRegistry.stateLabel()
        callerView.text = VoiceCallRegistry.callLabel()
        val isRinging = call.state == android.telecom.Call.STATE_RINGING
        answerButton.visibility = if (isRinging) android.view.View.VISIBLE else android.view.View.GONE
        answerButton.isEnabled = isRinging
        declineButton.text = if (isRinging) "Отклонить" else "Завершить звонок"
        declineButton.visibility = android.view.View.VISIBLE
    }

    private fun fullWidthWrap() = LinearLayout.LayoutParams(
        ViewGroup.LayoutParams.MATCH_PARENT,
        ViewGroup.LayoutParams.WRAP_CONTENT
    )

    private fun dp(value: Int): Int = (resources.displayMetrics.density * value).roundToInt()
}
