package by.minsk.voicecontrol

internal object AppPrefs {
    const val FILE = "voice_control"
    const val KEY_WAKE_WORD = "wake_word"
    const val KEY_STATUS = "service_status"
    const val DEFAULT_WAKE_WORD = "телефон"

    const val ACTION_STATUS = "by.minsk.voicecontrol.ACTION_STATUS"
    const val EXTRA_STATUS = "status"

    const val ACTION_CALL_STATE = "by.minsk.voicecontrol.ACTION_CALL_STATE"
    const val EXTRA_CALL_STATE = "call_state"
}
