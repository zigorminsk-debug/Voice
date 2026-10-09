package by.minsk.voicecontrol

import android.content.Context
import android.net.Uri
import android.os.Build
import android.os.Handler
import android.os.Looper
import org.json.JSONObject
import java.io.ByteArrayOutputStream
import java.io.IOException
import java.io.InputStream
import java.net.HttpURLConnection
import java.net.URL
import java.util.concurrent.Executors

internal data class ReleaseUpdate(
    val versionName: String,
    val versionCode: Long,
    val apkUri: Uri
)

/** Checks the public GitHub Releases manifest without blocking the UI thread. */
internal object ReleaseUpdateChecker {
    private const val MANIFEST_URL =
        "https://github.com/zigorminsk-debug/Voice/releases/latest/download/update.json"
    private const val MAX_MANIFEST_BYTES = 32 * 1024

    private val executor = Executors.newSingleThreadExecutor()
    private val mainHandler = Handler(Looper.getMainLooper())

    fun checkLatest(context: Context, callback: (Result<ReleaseUpdate?>) -> Unit) {
        val appContext = context.applicationContext
        executor.execute {
            val result = runCatching {
                val connection = (URL(MANIFEST_URL).openConnection() as HttpURLConnection).apply {
                    connectTimeout = 10_000
                    readTimeout = 10_000
                    requestMethod = "GET"
                    instanceFollowRedirects = true
                    setRequestProperty("Accept", "application/json")
                    setRequestProperty("Cache-Control", "no-cache")
                    setRequestProperty("User-Agent", "VoiceCommander-Android")
                }

                try {
                    val status = connection.responseCode
                    if (status == HttpURLConnection.HTTP_NOT_FOUND) {
                        return@runCatching null
                    }
                    if (status != HttpURLConnection.HTTP_OK) {
                        throw IOException("Update server returned HTTP $status")
                    }

                    val manifest = JSONObject(connection.inputStream.use(::readLimited))
                    val versionName = manifest.optString("versionName")
                    val versionCode = manifest.optLong("versionCode", -1L)
                    if (!VERSION_NAME_PATTERN.matches(versionName)) {
                        throw IOException("Invalid versionName in update manifest")
                    }
                    if (versionCode <= 0L || versionCode > Int.MAX_VALUE) {
                        throw IOException("Invalid versionCode in update manifest")
                    }

                    val currentVersionCode = installedVersionCode(appContext)
                    if (versionCode <= currentVersionCode) {
                        null
                    } else {
                        ReleaseUpdate(
                            versionName = versionName,
                            versionCode = versionCode,
                            apkUri = Uri.parse(
                                "https://github.com/zigorminsk-debug/Voice/releases/download/v$versionName/app-release.apk"
                            )
                        )
                    }
                } finally {
                    connection.disconnect()
                }
            }

            mainHandler.post { callback(result) }
        }
    }

    private fun installedVersionCode(context: Context): Long {
        @Suppress("DEPRECATION")
        val packageInfo = context.packageManager.getPackageInfo(context.packageName, 0)
        return if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
            packageInfo.longVersionCode
        } else {
            @Suppress("DEPRECATION")
            packageInfo.versionCode.toLong()
        }
    }

    private fun readLimited(input: InputStream): String {
        val output = ByteArrayOutputStream()
        val buffer = ByteArray(4 * 1024)
        var total = 0
        while (true) {
            val count = input.read(buffer)
            if (count < 0) break
            total += count
            if (total > MAX_MANIFEST_BYTES) {
                throw IOException("Update manifest is too large")
            }
            output.write(buffer, 0, count)
        }
        return String(output.toByteArray(), Charsets.UTF_8)
    }

    private val VERSION_NAME_PATTERN = Regex("\\d+\\.\\d+\\.\\d+")
}
