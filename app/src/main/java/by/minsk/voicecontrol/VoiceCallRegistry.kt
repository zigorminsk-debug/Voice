package by.minsk.voicecontrol

import android.content.Context
import android.content.Intent
import android.telecom.Call
import android.telecom.VideoProfile
import java.util.concurrent.CopyOnWriteArrayList

/** Small process-local bridge between Telecom's InCallService and voice/UI actions. */
internal object VoiceCallRegistry {
    private val calls = CopyOnWriteArrayList<Call>()

    fun add(call: Call) {
        if (!calls.contains(call)) calls.add(call)
    }

    fun remove(call: Call) {
        calls.remove(call)
    }

    fun ringingCall(): Call? = calls.firstOrNull { it.state == Call.STATE_RINGING }

    fun currentCall(): Call? = ringingCall() ?: calls.firstOrNull {
        it.state != Call.STATE_DISCONNECTED && it.state != Call.STATE_DISCONNECTING
    }

    fun answer(): Boolean {
        return try {
            val call = ringingCall() ?: return false
            call.answer(VideoProfile.STATE_AUDIO_ONLY)
            true
        } catch (_: Exception) {
            false
        }
    }

    fun reject(): Boolean {
        return try {
            val call = ringingCall() ?: return false
            call.reject(false, null)
            true
        } catch (_: Exception) {
            false
        }
    }

    fun disconnect(): Boolean {
        return try {
            val call = currentCall() ?: return false
            call.disconnect()
            true
        } catch (_: Exception) {
            false
        }
    }

    fun callLabel(): String {
        val call = currentCall() ?: return "Нет активного звонка"
        val details = call.details
        val displayName = details?.callerDisplayName?.toString()?.takeIf { it.isNotBlank() }
        val number = details?.handle?.schemeSpecificPart?.takeIf { it.isNotBlank() }
        return displayName ?: number ?: "Неизвестный номер"
    }

    fun stateLabel(): String {
        val call = currentCall() ?: return "Нет активного звонка"
        return when (call.state) {
            Call.STATE_RINGING -> "Входящий звонок"
            Call.STATE_DIALING, Call.STATE_CONNECTING -> "Вызов…"
            Call.STATE_ACTIVE -> "Разговор"
            Call.STATE_HOLDING -> "Звонок удерживается"
            Call.STATE_DISCONNECTING -> "Завершение звонка…"
            else -> "Звонок"
        }
    }

    fun publishState(context: Context) {
        val intent = Intent(AppPrefs.ACTION_CALL_STATE)
            .setPackage(context.packageName)
            .putExtra(AppPrefs.EXTRA_CALL_STATE, stateLabel())
        context.sendBroadcast(intent)
    }
}
