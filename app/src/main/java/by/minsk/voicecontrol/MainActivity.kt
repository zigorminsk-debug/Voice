package by.minsk.voicecontrol

import android.Manifest
import android.app.Activity
import android.app.AlertDialog
import android.app.DownloadManager
import android.app.role.RoleManager
import android.content.ActivityNotFoundException
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.Environment
import android.provider.Settings
import android.telecom.TelecomManager
import android.view.Gravity
import android.view.View
import android.view.ViewGroup
import android.widget.Button
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView
import android.widget.Toast
import androidx.core.content.ContextCompat
import java.util.Locale
import kotlin.math.roundToInt

class MainActivity : Activity() {
    private lateinit var statusView: TextView
    private lateinit var roleStatusView: TextView
    private lateinit var wakeWordEdit: EditText
    private var receiverRegistered = false

    private val statusReceiver = object : BroadcastReceiver() {
        override fun onReceive(context: Context?, intent: Intent?) {
            if (intent?.action == AppPrefs.ACTION_STATUS) {
                statusView.text = intent.getStringExtra(AppPrefs.EXTRA_STATUS) ?: "Голосовое управление активно"
            }
        }
    }

    private var updateReceiverRegistered = false
    private var updateCheckInProgress = false
    private var updateDialogShown = false
    private var pendingReleaseUpdate: ReleaseUpdate? = null
    private var pendingInstallerUri: Uri? = null

    private val updateDownloadReceiver = object : BroadcastReceiver() {
        override fun onReceive(context: Context?, intent: Intent?) {
            if (intent?.action != DownloadManager.ACTION_DOWNLOAD_COMPLETE) return
            val downloadId = intent.getLongExtra(DownloadManager.EXTRA_DOWNLOAD_ID, -1L)
            if (downloadId > 0L) processUpdateDownload(downloadId)
        }
    }

    override fun onWindowFocusChanged(hasFocus: Boolean) {
        super.onWindowFocusChanged(hasFocus)
        if (hasFocus) {
            pendingReleaseUpdate?.let { update ->
                pendingReleaseUpdate = null
                showUpdateDialog(update)
            }
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        pendingInstallerUri = getSharedPreferences(AppPrefs.FILE, MODE_PRIVATE)
            .getString(PREF_UPDATE_INSTALL_URI, null)
            ?.let(Uri::parse)
        window.statusBarColor = android.graphics.Color.rgb(244, 246, 250)
        window.navigationBarColor = android.graphics.Color.rgb(244, 246, 250)

        val prefs = getSharedPreferences(AppPrefs.FILE, MODE_PRIVATE)
        val page = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(22), dp(18), dp(22), dp(28))
            setBackgroundColor(android.graphics.Color.rgb(244, 246, 250))
        }

        val title = text("Voice Commander", 28f, 0xFF17223B.toInt(), bold = true).apply {
            setPadding(0, 0, 0, dp(6))
        }
        page.addView(title, matchWrap())
        page.addView(text("Управление звонками и фонариком без рук", 15f, 0xFF596579.toInt()), matchWrap())

        val statusCard = card()
        statusCard.addView(text("СОСТОЯНИЕ", 12f, 0xFF69758A.toInt(), bold = true), matchWrap())
        statusView = text(
            prefs.getString(AppPrefs.KEY_STATUS, "Остановлено") ?: "Остановлено",
            17f,
            0xFF17223B.toInt(),
            bold = true
        ).apply { setPadding(0, dp(7), 0, 0) }
        statusCard.addView(statusView, matchWrap())
        page.addView(statusCard, matchWrap(top = 22))

        page.addView(text("Кодовое слово", 17f, 0xFF17223B.toInt(), bold = true), matchWrap(top = 22))
        wakeWordEdit = EditText(this).apply {
            setSingleLine(true)
            hint = "Например: телефон"
            textSize = 17f
            setPadding(dp(14), dp(10), dp(14), dp(10))
            setText(prefs.getString(AppPrefs.KEY_WAKE_WORD, AppPrefs.DEFAULT_WAKE_WORD))
            contentDescription = "Кодовое слово"
        }
        page.addView(wakeWordEdit, matchWrap(top = 8))

        page.addView(
            text(
                "Скажите кодовое слово, затем в течение 8 секунд — команду. Например: " +
                    "«телефон» → «ответить» или «телефон, фонарик включи».",
                14f,
                0xFF596579.toInt()
            ).apply { setPadding(0, dp(8), 0, 0) },
            matchWrap()
        )

        page.addView(actionButton("Сохранить кодовое слово") {
            saveWakeWord(showToast = true)
        }, matchWrap(top = 14))

        page.addView(actionButton("Включить голосовое управление") {
            if (saveWakeWord(showToast = false)) requestPermissionsAndStart()
        }, matchWrap(top = 10))

        page.addView(actionButton("Остановить прослушивание", secondary = true) {
            startService(Intent(this, VoiceRecognitionService::class.java).setAction(VoiceRecognitionService.ACTION_STOP))
            statusView.text = "Останавливаю…"
        }, matchWrap(top = 10))

        val roleCard = card()
        roleCard.addView(text("КОМАНДЫ ДЛЯ ЗВОНКОВ", 12f, 0xFF69758A.toInt(), bold = true), matchWrap())
        roleStatusView = text("Проверка приложения телефона…", 15f, 0xFF17223B.toInt(), bold = true).apply {
            setPadding(0, dp(7), 0, 0)
        }
        roleCard.addView(roleStatusView, matchWrap())
        roleCard.addView(
            text(
                "Чтобы отвечать и отклонять звонки голосом, Android попросит назначить это приложение " +
                    "приложением телефона по умолчанию. Это системное подтверждение обязательно; " +
                    "его можно отменить в настройках телефона.",
                14f,
                0xFF596579.toInt()
            ).apply { setPadding(0, dp(7), 0, 0) },
            matchWrap()
        )
        page.addView(roleCard, matchWrap(top = 22))
        page.addView(actionButton("Настроить управление звонками") { requestDialerRole() }, matchWrap(top = 10))

        page.addView(
            text(
                "Команды: «ответить», «отклонить», «фонарик включи», «фонарик выключи». " +
                    "Микрофон работает через видимое постоянное уведомление. Для распознавания Android " +
                    "может использовать установленный сервис речи и интернет; поддержка гарнитур зависит " +
                    "от версии Android и производителя телефона.",
                13f,
                0xFF69758A.toInt()
            ).apply { setPadding(0, dp(20), 0, 0) },
            matchWrap()
        )
        page.addView(
            text(
                "Обновления проверяются при открытии приложения. Android потребует подтвердить установку " +
                    "скачанного APK; для этого может понадобиться разрешить установку неизвестных приложений.",
                13f,
                0xFF69758A.toInt()
            ).apply { setPadding(0, dp(16), 0, 0) },
            matchWrap()
        )
        page.addView(actionButton("Проверить обновления", secondary = true) {
            checkForUpdates(manual = true)
        }, matchWrap(top = 10))

        val scroll = ScrollView(this).apply {
            isFillViewport = true
            addView(page)
        }
        setContentView(scroll)
        refreshDialerRoleStatus()
        checkForUpdates(manual = false)
    }

    override fun onStart() {
        super.onStart()
        val filter = IntentFilter(AppPrefs.ACTION_STATUS)
        ContextCompat.registerReceiver(this, statusReceiver, filter, ContextCompat.RECEIVER_NOT_EXPORTED)
        receiverRegistered = true

        val updateFilter = IntentFilter(DownloadManager.ACTION_DOWNLOAD_COMPLETE)
        // DownloadManager sends this system broadcast; the download ID is verified before use.
        ContextCompat.registerReceiver(this, updateDownloadReceiver, updateFilter, ContextCompat.RECEIVER_EXPORTED)
        updateReceiverRegistered = true
        processPendingUpdateDownload()

        val savedStatus = getSharedPreferences(AppPrefs.FILE, MODE_PRIVATE)
            .getString(AppPrefs.KEY_STATUS, null)
        if (!savedStatus.isNullOrBlank()) statusView.text = savedStatus
        refreshDialerRoleStatus()
    }

    override fun onStop() {
        if (receiverRegistered) {
            unregisterReceiver(statusReceiver)
            receiverRegistered = false
        }
        if (updateReceiverRegistered) {
            unregisterReceiver(updateDownloadReceiver)
            updateReceiverRegistered = false
        }
        super.onStop()
    }

    private fun checkForUpdates(manual: Boolean) {
        if (updateCheckInProgress) {
            if (manual) Toast.makeText(this, "Проверка обновлений уже выполняется", Toast.LENGTH_SHORT).show()
            return
        }
        updateCheckInProgress = true
        ReleaseUpdateChecker.checkLatest(this) { result ->
            updateCheckInProgress = false
            if (isFinishing || isDestroyed) return@checkLatest

            result.fold(
                onSuccess = { update ->
                    if (update == null) {
                        if (manual) Toast.makeText(this, "Установлена последняя версия", Toast.LENGTH_SHORT).show()
                    } else {
                        showUpdateDialog(update)
                    }
                },
                onFailure = {
                    if (manual) {
                        Toast.makeText(this, "Не удалось проверить обновления. Проверьте интернет.", Toast.LENGTH_LONG).show()
                    }
                }
            )
        }
    }

    private fun showUpdateDialog(update: ReleaseUpdate) {
        if (updateDialogShown || isFinishing || isDestroyed) return
        if (!hasWindowFocus()) {
            pendingReleaseUpdate = update
            return
        }
        updateDialogShown = true
        AlertDialog.Builder(this)
            .setTitle("Доступно обновление ${update.versionName}")
            .setMessage(
                "Скачать обновление? После загрузки Android откроет системную установку и попросит подтвердить её."
            )
            .setPositiveButton("Скачать") { _, _ -> downloadUpdate(update) }
            .setNegativeButton("Позже", null)
            .show()
    }

    private fun downloadUpdate(update: ReleaseUpdate) {
        val downloadManager = getSystemService(DownloadManager::class.java)
        if (downloadManager == null) {
            Toast.makeText(this, "Сервис загрузки Android недоступен", Toast.LENGTH_LONG).show()
            return
        }

        val preferences = getSharedPreferences(AppPrefs.FILE, MODE_PRIVATE)
        if (preferences.getLong(PREF_UPDATE_DOWNLOAD_ID, -1L) > 0L) {
            Toast.makeText(this, "Обновление уже загружается", Toast.LENGTH_SHORT).show()
            return
        }

        try {
            val request = DownloadManager.Request(update.apkUri)
                .setTitle("Voice Commander ${update.versionName}")
                .setDescription("Загрузка обновления")
                .setMimeType(APK_MIME_TYPE)
                .setNotificationVisibility(DownloadManager.Request.VISIBILITY_VISIBLE_NOTIFY_COMPLETED)
                .setAllowedOverMetered(true)
                .setAllowedOverRoaming(false)
                .setDestinationInExternalFilesDir(
                    this,
                    Environment.DIRECTORY_DOWNLOADS,
                    "voice-commander-${update.versionName}.apk"
                )
            val downloadId = downloadManager.enqueue(request)
            preferences.edit().putLong(PREF_UPDATE_DOWNLOAD_ID, downloadId).apply()
            processUpdateDownload(downloadId)
            Toast.makeText(this, "Загружаю обновление…", Toast.LENGTH_LONG).show()
        } catch (e: Exception) {
            Toast.makeText(this, "Не удалось начать загрузку: ${e.localizedMessage ?: "ошибка"}", Toast.LENGTH_LONG).show()
        }
    }

    private fun processPendingUpdateDownload() {
        val downloadId = getSharedPreferences(AppPrefs.FILE, MODE_PRIVATE)
            .getLong(PREF_UPDATE_DOWNLOAD_ID, -1L)
        if (downloadId > 0L) processUpdateDownload(downloadId)
    }

    private fun processUpdateDownload(downloadId: Long) {
        val preferences = getSharedPreferences(AppPrefs.FILE, MODE_PRIVATE)
        if (preferences.getLong(PREF_UPDATE_DOWNLOAD_ID, -1L) != downloadId) return
        val downloadManager = getSystemService(DownloadManager::class.java) ?: return
        val cursor = downloadManager.query(DownloadManager.Query().setFilterById(downloadId)) ?: return

        cursor.use {
            if (!it.moveToFirst()) {
                preferences.edit().remove(PREF_UPDATE_DOWNLOAD_ID).apply()
                return
            }
            when (it.getInt(it.getColumnIndexOrThrow(DownloadManager.COLUMN_STATUS))) {
                DownloadManager.STATUS_PENDING,
                DownloadManager.STATUS_PAUSED,
                DownloadManager.STATUS_RUNNING -> Unit

                DownloadManager.STATUS_SUCCESSFUL -> {
                    val downloadedApk = downloadManager.getUriForDownloadedFile(downloadId)
                    preferences.edit().remove(PREF_UPDATE_DOWNLOAD_ID).apply()
                    if (downloadedApk == null) {
                        Toast.makeText(this, "Загрузка завершилась без APK-файла", Toast.LENGTH_LONG).show()
                    } else {
                        openUpdateInstaller(downloadedApk)
                    }
                }

                DownloadManager.STATUS_FAILED -> {
                    val reason = it.getInt(it.getColumnIndexOrThrow(DownloadManager.COLUMN_REASON))
                    preferences.edit().remove(PREF_UPDATE_DOWNLOAD_ID).apply()
                    Toast.makeText(this, "Не удалось загрузить обновление (код $reason)", Toast.LENGTH_LONG).show()
                }
            }
        }
    }

    private fun openUpdateInstaller(apkUri: Uri) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O && !packageManager.canRequestPackageInstalls()) {
            pendingInstallerUri = apkUri
            getSharedPreferences(AppPrefs.FILE, MODE_PRIVATE)
                .edit()
                .putString(PREF_UPDATE_INSTALL_URI, apkUri.toString())
                .apply()
            try {
                val settingsIntent = Intent(
                    Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES,
                    Uri.parse("package:$packageName")
                )
                startActivityForResult(settingsIntent, REQUEST_INSTALL_SOURCE)
            } catch (_: Exception) {
                pendingInstallerUri = null
                Toast.makeText(this, "Разрешите установку неизвестных приложений в настройках Android", Toast.LENGTH_LONG).show()
            }
            return
        }
        launchUpdateInstaller(apkUri)
    }

    private fun launchUpdateInstaller(apkUri: Uri) {
        getSharedPreferences(AppPrefs.FILE, MODE_PRIVATE)
            .edit()
            .remove(PREF_UPDATE_INSTALL_URI)
            .apply()
        val installIntent = Intent(Intent.ACTION_VIEW).apply {
            setDataAndType(apkUri, APK_MIME_TYPE)
            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
        }
        try {
            startActivity(installIntent)
        } catch (_: ActivityNotFoundException) {
            Toast.makeText(this, "На устройстве не найден установщик APK", Toast.LENGTH_LONG).show()
        } catch (_: SecurityException) {
            Toast.makeText(this, "Android не разрешил открыть установщик APK", Toast.LENGTH_LONG).show()
        }
    }

    private fun requestPermissionsAndStart() {
        val wanted = mutableListOf(Manifest.permission.RECORD_AUDIO)
        if (checkSelfPermission(Manifest.permission.CAMERA) != PackageManager.PERMISSION_GRANTED) {
            wanted += Manifest.permission.CAMERA
        }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S &&
            checkSelfPermission(Manifest.permission.BLUETOOTH_CONNECT) != PackageManager.PERMISSION_GRANTED
        ) {
            wanted += Manifest.permission.BLUETOOTH_CONNECT
        }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) {
            wanted += Manifest.permission.POST_NOTIFICATIONS
        }

        val missing = wanted.distinct().filter {
            checkSelfPermission(it) != PackageManager.PERMISSION_GRANTED
        }
        if (missing.isNotEmpty()) {
            requestPermissions(missing.toTypedArray(), REQUEST_PERMISSIONS)
        } else {
            startListeningService()
        }
    }

    @Deprecated("Use runtime permissions result callback")
    override fun onRequestPermissionsResult(requestCode: Int, permissions: Array<out String>, grantResults: IntArray) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        if (requestCode != REQUEST_PERMISSIONS) return
        if (checkSelfPermission(Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED) {
            startListeningService()
        } else {
            statusView.text = "Нужно разрешение на микрофон"
            Toast.makeText(this, "Без доступа к микрофону голосовое управление не запустится", Toast.LENGTH_LONG).show()
        }
    }

    private fun startListeningService() {
        val intent = Intent(this, VoiceRecognitionService::class.java).setAction(VoiceRecognitionService.ACTION_START)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) startForegroundService(intent) else startService(intent)
        statusView.text = "Запускаю голосовое управление…"
    }

    private fun saveWakeWord(showToast: Boolean): Boolean {
        val normalized = normalize(wakeWordEdit.text.toString())
        if (normalized.isBlank()) {
            wakeWordEdit.error = "Введите кодовое слово"
            return false
        }
        getSharedPreferences(AppPrefs.FILE, MODE_PRIVATE)
            .edit()
            .putString(AppPrefs.KEY_WAKE_WORD, normalized)
            .apply()
        wakeWordEdit.setText(normalized)
        if (showToast) Toast.makeText(this, "Кодовое слово сохранено", Toast.LENGTH_SHORT).show()
        return true
    }

    private fun requestDialerRole() {
        try {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
                val roleManager = getSystemService(RoleManager::class.java)
                if (roleManager == null || !roleManager.isRoleAvailable(RoleManager.ROLE_DIALER)) {
                    Toast.makeText(this, "На этом устройстве роль телефона недоступна", Toast.LENGTH_LONG).show()
                    return
                }
                if (roleManager.isRoleHeld(RoleManager.ROLE_DIALER)) {
                    Toast.makeText(this, "Приложение уже управляет звонками", Toast.LENGTH_SHORT).show()
                    refreshDialerRoleStatus()
                    return
                }
                startActivityForResult(roleManager.createRequestRoleIntent(RoleManager.ROLE_DIALER), REQUEST_DIALER_ROLE)
            } else {
                val intent = Intent(TelecomManager.ACTION_CHANGE_DEFAULT_DIALER)
                    .putExtra(TelecomManager.EXTRA_CHANGE_DEFAULT_DIALER_PACKAGE_NAME, packageName)
                startActivityForResult(intent, REQUEST_DIALER_ROLE)
            }
        } catch (e: Exception) {
            Toast.makeText(this, "Не удалось открыть настройки звонков: ${e.localizedMessage ?: "ошибка"}", Toast.LENGTH_LONG).show()
        }
    }

    @Deprecated("Role result callback")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        if (requestCode == REQUEST_INSTALL_SOURCE) {
            val apkUri = pendingInstallerUri ?: getSharedPreferences(AppPrefs.FILE, MODE_PRIVATE)
                .getString(PREF_UPDATE_INSTALL_URI, null)
                ?.let(Uri::parse)
            pendingInstallerUri = null
            getSharedPreferences(AppPrefs.FILE, MODE_PRIVATE)
                .edit()
                .remove(PREF_UPDATE_INSTALL_URI)
                .apply()
            if (apkUri != null && Build.VERSION.SDK_INT >= Build.VERSION_CODES.O && packageManager.canRequestPackageInstalls()) {
                launchUpdateInstaller(apkUri)
            } else {
                Toast.makeText(this, "Для обновления разрешите установку неизвестных приложений", Toast.LENGTH_LONG).show()
            }
            return
        }
        if (requestCode == REQUEST_DIALER_ROLE) {
            refreshDialerRoleStatus()
            if (resultCode == Activity.RESULT_OK) {
                Toast.makeText(this, "Управление звонками включено", Toast.LENGTH_SHORT).show()
            }
        }
    }

    private fun isDefaultDialer(): Boolean = try {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            getSystemService(RoleManager::class.java)?.isRoleHeld(RoleManager.ROLE_DIALER) == true
        } else {
            getSystemService(TelecomManager::class.java)?.defaultDialerPackage == packageName
        }
    } catch (_: Exception) {
        false
    }

    private fun refreshDialerRoleStatus() {
        if (!::roleStatusView.isInitialized) return
        roleStatusView.text = if (isDefaultDialer()) {
            "✅ Приложение назначено приложением телефона"
        } else {
            "Не назначено — голосовые команды звонков недоступны"
        }
    }

    private fun actionButton(label: String, secondary: Boolean = false, action: () -> Unit): Button =
        Button(this).apply {
            text = label
            isAllCaps = false
            textSize = 15f
            minHeight = dp(50)
            gravity = Gravity.CENTER
            setOnClickListener { action() }
            if (secondary) {
                setTextColor(0xFF3157D5.toInt())
            }
        }

    private fun card(): LinearLayout = LinearLayout(this).apply {
        orientation = LinearLayout.VERTICAL
        setPadding(dp(16), dp(15), dp(16), dp(15))
        setBackgroundColor(android.graphics.Color.WHITE)
        elevation = dp(1).toFloat()
    }

    private fun text(value: String, size: Float, color: Int, bold: Boolean = false): TextView = TextView(this).apply {
        text = value
        textSize = size
        setTextColor(color)
        if (bold) setTypeface(typeface, android.graphics.Typeface.BOLD)
    }

    private fun matchWrap(top: Int = 0): LinearLayout.LayoutParams =
        LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT).apply {
            if (top > 0) topMargin = dp(top)
        }

    private fun dp(value: Int): Int = (resources.displayMetrics.density * value).roundToInt()

    private fun normalize(value: String): String = value.trim().lowercase(Locale.forLanguageTag("ru-RU"))
        .replace(Regex("\\s+"), " ")

    companion object {
        private const val REQUEST_PERMISSIONS = 4101
        private const val REQUEST_DIALER_ROLE = 4102
        private const val REQUEST_INSTALL_SOURCE = 4103
        private const val PREF_UPDATE_DOWNLOAD_ID = "update_download_id"
        private const val PREF_UPDATE_INSTALL_URI = "update_install_uri"
        private const val APK_MIME_TYPE = "application/vnd.android.package-archive"
    }
}
