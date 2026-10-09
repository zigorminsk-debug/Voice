package by.minsk.voicecontrol

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

class CallActionReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        when (intent.action) {
            ACTION_ANSWER -> VoiceCallRegistry.answer()
            ACTION_REJECT -> VoiceCallRegistry.reject()
            ACTION_HANG_UP -> VoiceCallRegistry.disconnect()
        }
        VoiceCallRegistry.publishState(context)
    }

    companion object {
        const val ACTION_ANSWER = "by.minsk.voicecontrol.CALL_ANSWER"
        const val ACTION_REJECT = "by.minsk.voicecontrol.CALL_REJECT"
        const val ACTION_HANG_UP = "by.minsk.voicecontrol.CALL_HANG_UP"
    }
}
