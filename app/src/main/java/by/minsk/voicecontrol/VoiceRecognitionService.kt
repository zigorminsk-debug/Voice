package by.minsk.voicecontrol

import android.Manifest
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.content.pm.ServiceInfo
import android.hardware.camera2.CameraAccessException
import android.hardware.camera2.CameraCharacteristics
import android.hardware.camera2.CameraManager
import android.media.AudioDeviceInfo
import android.media.AudioManager
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.speech.RecognitionListener
import android.speech.RecognizerIntent
import android.speech.SpeechRecognizer
import java.util.Locale

/** Foreground speech listener. Android SpeechRecognizer is restarted after each phrase. */
class VoiceRecognitionService : Service() {
    private val mainHandler = Handler(Looper.getMainLooper())
    private var recognizer: SpeechRecognizer? = null
    private var active = false
    private var recognitionActive = false
    private var recognizerUsesOnDevice = false
    private var wakeWord = AppPrefs.DEFAULT_WAKE_WORD
    private var awaitingCommand = false
    private var commandTimeoutTask: Runnable? = null
    private var restartTask: Runnable? = null

    private var audioManager: AudioManager? = null
    private var originalAudioMode: Int = AudioManager.MODE_NORMAL
    private var originalCommunicationDevice: AudioDeviceInfo? = null
    private var legacyScoRequested = false
    private var bluetoothRouteSelected = false

    override fun onCreate() {
        super.onCreate()
        createNotificationChannel()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_STOP -> {
                stopListening()
                stopSelf(startId)
                return START_NOT_STICKY
            }
            ACTION_START, null -> startListening()
        }
        return START_NOT_STICKY
    }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onDestroy() {
        stopListening()
        super.onDestroy()
    }

    private fun startListening() {
        if (active) {
            setStatus("Прослушиваю кодовое слово «$wakeWord»")
            return
        }
        if (checkSelfPermission(Manifest.permission.RECORD_AUDIO) != PackageManager.PERMISSION_GRANTED) {
            setStatus("Нет разрешения на микрофон")
            stopSelf()
            return
        }

        active = true
        wakeWord = normalizePhrase(
            getSharedPreferences(AppPrefs.FILE, MODE_PRIVATE)
                .getString(AppPrefs.KEY_WAKE_WORD, AppPrefs.DEFAULT_WAKE_WORD)
                ?: AppPrefs.DEFAULT_WAKE_WORD
        ).ifBlank { AppPrefs.DEFAULT_WAKE_WORD }

        val notification = buildServiceNotification("Подготовка микрофона…")
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            startForeground(
                SERVICE_NOTIFICATION_ID,
                notification,
                ServiceInfo.FOREGROUND_SERVICE_TYPE_MICROPHONE
            )
        } else {
            startForeground(SERVICE_NOTIFICATION_ID, notification)
        }

        val routeDescription = prepareBluetoothAudioRoute()
        setStatus("$routeDescription. Скажите «$wakeWord»")
        val startDelay = if (legacyScoRequested) 1_500L else 450L
        scheduleRecognitionStart(startDelay)
    }

    private fun stopListening() {
        if (!active && recognizer == null) return
        active = false
        recognitionActive = false
        awaitingCommand = false
        restartTask?.let(mainHandler::removeCallbacks)
        restartTask = null
        commandTimeoutTask?.let(mainHandler::removeCallbacks)
        commandTimeoutTask = null

        try {
            recognizer?.cancel()
            recognizer?.destroy()
        } catch (_: Exception) {
        }
        recognizer = null
        restoreAudioRoute()
        stopForeground(STOP_FOREGROUND_REMOVE)
        setStatus("Остановлено", updateNotification = false)
    }

    private fun prepareBluetoothAudioRoute(): String {
        val manager = getSystemService(Context.AUDIO_SERVICE) as AudioManager
        audioManager = manager
        originalAudioMode = manager.mode
        bluetoothRouteSelected = false
        legacyScoRequested = false

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            try {
                originalCommunicationDevice = manager.communicationDevice
                val bluetoothDevice = manager.availableCommunicationDevices.firstOrNull {
                    it.type == AudioDeviceInfo.TYPE_BLUETOOTH_SCO ||
                        it.type == AudioDeviceInfo.TYPE_BLE_HEADSET
                }
                if (bluetoothDevice != null) {
                    manager.mode = AudioManager.MODE_IN_COMMUNICATION
                    bluetoothRouteSelected = manager.setCommunicationDevice(bluetoothDevice)
                    if (bluetoothRouteSelected) return "Гарнитура подключена, использую её микрофон"
                    manager.clearCommunicationDevice()
                    manager.mode = originalAudioMode
                }
            } catch (_: SecurityException) {
                // Permission can be denied; recognition will use Android's default input route.
            } catch (_: Exception) {
            }
            return "Гарнитура не выбрана, использую доступный микрофон"
        }

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val hasScoDevice = try {
                manager.getDevices(AudioManager.GET_DEVICES_INPUTS)
                    .any { it.type == AudioDeviceInfo.TYPE_BLUETOOTH_SCO }
            } catch (_: Exception) {
                false
            }
            if (hasScoDevice) {
                try {
                    manager.mode = AudioManager.MODE_IN_COMMUNICATION
                    manager.startBluetoothSco()
                    @Suppress("DEPRECATION")
                    run { manager.isBluetoothScoOn = true }
                    legacyScoRequested = true
                    bluetoothRouteSelected = true
                    return "Bluetooth-гарнитура подключается"
                } catch (_: Exception) {
                }
            }
        }
        return "Гарнитура не выбрана, использую доступный микрофон"
    }

    private fun restoreAudioRoute() {
        val manager = audioManager ?: return
        try {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
                val previousDevice = originalCommunicationDevice
                if (previousDevice != null) manager.setCommunicationDevice(previousDevice)
                else manager.clearCommunicationDevice()
            } else if (legacyScoRequested) {
                manager.stopBluetoothSco()
                @Suppress("DEPRECATION")
                run { manager.isBluetoothScoOn = false }
            }
        } catch (_: Exception) {
        }
        try {
            manager.mode = originalAudioMode
        } catch (_: Exception) {
        }
        originalCommunicationDevice = null
        audioManager = null
        legacyScoRequested = false
        bluetoothRouteSelected = false
    }

    private fun getRecognizer(): SpeechRecognizer? {
        if (recognizer != null) return recognizer
        if (!SpeechRecognizer.isRecognitionAvailable(this)) {
            setStatus("На устройстве нет службы распознавания речи")
            return null
        }
        return try {
            val created = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S &&
                SpeechRecognizer.isOnDeviceRecognitionAvailable(this)
            ) {
                recognizerUsesOnDevice = true
                SpeechRecognizer.createOnDeviceSpeechRecognizer(this)
            } else {
                recognizerUsesOnDevice = false
                SpeechRecognizer.createSpeechRecognizer(this)
            }
            created.setRecognitionListener(recognitionListener)
            recognizer = created
            created
        } catch (_: Exception) {
            recognizerUsesOnDevice = false
            try {
                SpeechRecognizer.createSpeechRecognizer(this).also {
                    it.setRecognitionListener(recognitionListener)
                    recognizer = it
                }
            } catch (_: Exception) {
                setStatus("Не удалось запустить распознавание речи")
                null
            }
        }
    }

    private val recognitionListener = object : RecognitionListener {
        override fun onReadyForSpeech(params: Bundle?) = Unit
        override fun onBeginningOfSpeech() = Unit
        override fun onRmsChanged(rmsdB: Float) = Unit
        override fun onBufferReceived(buffer: ByteArray?) = Unit
        override fun onEndOfSpeech() = Unit
        override fun onPartialResults(partialResults: Bundle?) = Unit
        override fun onEvent(eventType: Int, params: Bundle?) = Unit

        override fun onResults(results: Bundle?) {
            recognitionActive = false
            val phrases = results?.getStringArrayList(SpeechRecognizer.RESULTS_RECOGNITION).orEmpty()
            processRecognizedPhrases(phrases)
            scheduleRecognitionStart(250L)
        }

        override fun onError(error: Int) {
            recognitionActive = false
            if (recognizerUsesOnDevice &&
                (error == SpeechRecognizer.ERROR_LANGUAGE_UNAVAILABLE ||
                    error == SpeechRecognizer.ERROR_NETWORK ||
                    error == SpeechRecognizer.ERROR_SERVER)
            ) {
                switchToDefaultRecognizer()
            }
            val delay = if (error == SpeechRecognizer.ERROR_RECOGNIZER_BUSY) 900L else 450L
            scheduleRecognitionStart(delay)
        }
    }

    private fun switchToDefaultRecognizer() {
        try {
            recognizer?.cancel()
            recognizer?.destroy()
        } catch (_: Exception) {
        }
        recognizer = try {
            recognizerUsesOnDevice = false
            SpeechRecognizer.createSpeechRecognizer(this).also {
                it.setRecognitionListener(recognitionListener)
            }
        } catch (_: Exception) {
            null
        }
    }

    private fun scheduleRecognitionStart(delayMs: Long) {
        if (!active) return
        restartTask?.let(mainHandler::removeCallbacks)
        val task = Runnable { startRecognition() }
        restartTask = task
        mainHandler.postDelayed(task, delayMs)
    }

    private fun startRecognition() {
        if (!active || recognitionActive) return
        val service = getRecognizer() ?: run {
            scheduleRecognitionStart(2_000L)
            return
        }
        val intent = Intent(RecognizerIntent.ACTION_RECOGNIZE_SPEECH).apply {
            putExtra(RecognizerIntent.EXTRA_LANGUAGE_MODEL, RecognizerIntent.LANGUAGE_MODEL_FREE_FORM)
            putExtra(RecognizerIntent.EXTRA_LANGUAGE, "ru-RU")
            putExtra(RecognizerIntent.EXTRA_MAX_RESULTS, 5)
            putExtra(RecognizerIntent.EXTRA_PARTIAL_RESULTS, false)
        }
        try {
            recognitionActive = true
            service.startListening(intent)
        } catch (_: Exception) {
            recognitionActive = false
            scheduleRecognitionStart(1_000L)
        }
    }

    private fun processRecognizedPhrases(phrases: List<String>) {
        if (!active) return
        val now = System.currentTimeMillis()
        if (awaitingCommand && now > commandDeadline) {
            awaitingCommand = false
        }

        val normalized = phrases.map(::normalizePhrase).filter { it.isNotBlank() }
        if (awaitingCommand) {
            val command = normalized.firstOrNull() ?: return
            awaitingCommand = false
            commandTimeoutTask?.let(mainHandler::removeCallbacks)
            commandTimeoutTask = null
            executeCommand(command)
            return
        }

        for (phrase in normalized) {
            val suffix = textAfterWakeWord(phrase, wakeWord) ?: continue
            if (suffix.isBlank()) {
                awaitCommand()
            } else {
                executeCommand(suffix)
            }
            return
        }
    }

    private var commandDeadline: Long = 0L

    private fun awaitCommand() {
        awaitingCommand = true
        commandDeadline = System.currentTimeMillis() + COMMAND_WINDOW_MS
        commandTimeoutTask?.let(mainHandler::removeCallbacks)
        val timeout = Runnable {
            if (awaitingCommand) {
                awaitingCommand = false
                setStatus("Команда не прозвучала. Скажите «$wakeWord» и команду ещё раз")
            }
        }
        commandTimeoutTask = timeout
        mainHandler.postDelayed(timeout, COMMAND_WINDOW_MS)
        setStatus("Слушаю команду…")
    }

    private fun executeCommand(rawCommand: String) {
        val command = normalizePhrase(rawCommand)
        val hasFlash = command.contains("фонар") || command.contains("свет")
        val asksOff = listOf("выключ", "погас", "отключ").any { command.contains(it) }
        val asksOn = listOf("включ", "зажг").any { command.contains(it) }

        if (hasFlash && asksOff) {
            setTorch(false)
            return
        }
        if (hasFlash && asksOn) {
            setTorch(true)
            return
        }

        if (listOf("ответ", "прими", "подними трубку", "сними трубку").any { command.contains(it) }) {
            if (VoiceCallRegistry.answer()) setStatus("Отвечаю на звонок")
            else setStatus("Входящий звонок не найден. Проверьте роль приложения телефона")
            return
        }
        if (listOf("отклони", "сброс", "отбой", "откаж").any { command.contains(it) }) {
            if (VoiceCallRegistry.reject()) setStatus("Отклоняю звонок")
            else setStatus("Входящий звонок не найден")
            return
        }

        setStatus("Команда не распознана: «$rawCommand». Повторите после слова «$wakeWord»")
    }

    private fun setTorch(enabled: Boolean) {
        if (checkSelfPermission(Manifest.permission.CAMERA) != PackageManager.PERMISSION_GRANTED) {
            setStatus("Для фонарика нужно разрешение на камеру")
            return
        }
        try {
            val cameraManager = getSystemService(Context.CAMERA_SERVICE) as CameraManager
            val cameraId = cameraManager.cameraIdList.firstOrNull { id ->
                val characteristics = cameraManager.getCameraCharacteristics(id)
                characteristics.get(CameraCharacteristics.FLASH_INFO_AVAILABLE) == true &&
                    characteristics.get(CameraCharacteristics.LENS_FACING) == CameraCharacteristics.LENS_FACING_BACK
            } ?: cameraManager.cameraIdList.firstOrNull { id ->
                cameraManager.getCameraCharacteristics(id).get(CameraCharacteristics.FLASH_INFO_AVAILABLE) == true
            }
            if (cameraId == null) {
                setStatus("На этом телефоне не найден фонарик")
                return
            }
            cameraManager.setTorchMode(cameraId, enabled)
            setStatus(if (enabled) "Фонарик включён" else "Фонарик выключен")
        } catch (_: CameraAccessException) {
            setStatus("Не удалось управлять фонариком: камера занята")
        } catch (_: SecurityException) {
            setStatus("Android запретил доступ к фонарику")
        } catch (e: Exception) {
            setStatus("Ошибка фонарика: ${e.localizedMessage ?: "недоступно"}")
        }
    }

    private fun textAfterWakeWord(text: String, key: String): String? {
        if (key.isBlank()) return null
        var from = 0
        while (from <= text.length) {
            val position = text.indexOf(key, from)
            if (position < 0) return null
            val end = position + key.length
            val hasLeftBoundary = position == 0 || text[position - 1] == ' '
            val hasRightBoundary = end == text.length || text[end] == ' '
            if (hasLeftBoundary && hasRightBoundary) return text.substring(end).trim()
            from = position + 1
        }
        return null
    }

    private fun normalizePhrase(value: String): String = value.lowercase(Locale.forLanguageTag("ru-RU"))
        .replace(Regex("[^\\p{L}\\p{N}]+"), " ")
        .trim()
        .replace(Regex("\\s+"), " ")

    private fun setStatus(status: String, updateNotification: Boolean = true) {
        getSharedPreferences(AppPrefs.FILE, MODE_PRIVATE)
            .edit()
            .putString(AppPrefs.KEY_STATUS, status)
            .apply()
        sendBroadcast(
            Intent(AppPrefs.ACTION_STATUS)
                .setPackage(packageName)
                .putExtra(AppPrefs.EXTRA_STATUS, status)
        )
        if (active && updateNotification) {
            try {
                (getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager)
                    .notify(SERVICE_NOTIFICATION_ID, buildServiceNotification(status))
            } catch (_: Exception) {
            }
        }
    }

    private fun buildServiceNotification(status: String): Notification {
        val builder = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            Notification.Builder(this, SERVICE_CHANNEL_ID)
        } else {
            @Suppress("DEPRECATION")
            Notification.Builder(this)
        }
        val openAppIntent = PendingIntent.getActivity(
            this,
            901,
            Intent(this, MainActivity::class.java),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )
        val stopIntent = PendingIntent.getService(
            this,
            902,
            Intent(this, VoiceRecognitionService::class.java).setAction(ACTION_STOP),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )
        return builder
            .setSmallIcon(R.drawable.ic_phone)
            .setContentTitle("Voice Commander активен")
            .setContentText(status)
            .setContentIntent(openAppIntent)
            .setCategory(Notification.CATEGORY_SERVICE)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .addAction(Notification.Action.Builder(R.drawable.ic_phone, "Остановить", stopIntent).build())
            .build()
    }

    private fun createNotificationChannel() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val channel = NotificationChannel(
                SERVICE_CHANNEL_ID,
                getString(R.string.voice_channel_name),
                NotificationManager.IMPORTANCE_LOW
            ).apply {
                description = "Показывает, что микрофон слушает кодовое слово"
                setShowBadge(false)
            }
            (getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager)
                .createNotificationChannel(channel)
        }
    }

    companion object {
        const val ACTION_START = "by.minsk.voicecontrol.START_LISTENING"
        const val ACTION_STOP = "by.minsk.voicecontrol.STOP_LISTENING"
        private const val SERVICE_CHANNEL_ID = "voice_control"
        private const val SERVICE_NOTIFICATION_ID = 2201
        private const val COMMAND_WINDOW_MS = 8_000L
    }
}
