package by.minsk.voicecontrol

import android.Manifest
import android.app.Activity
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Bundle
import android.telecom.TelecomManager
import android.view.Gravity
import android.view.ViewGroup
import android.widget.Button
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.TextView
import android.widget.Toast
import kotlin.math.roundToInt

/** Minimal ACTION_DIAL screen so the app can request the dialer role. */
class DialerActivity : Activity() {
    private lateinit var numberEdit: EditText

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        window.statusBarColor = 0xFFF4F6FA.toInt()
        window.navigationBarColor = 0xFFF4F6FA.toInt()

        val root = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(22), dp(24), dp(22), dp(24))
            gravity = Gravity.TOP
            setBackgroundColor(0xFFF4F6FA.toInt())
        }
        root.addView(TextView(this).apply {
            text = "Телефон"
            textSize = 27f
            setTextColor(0xFF17223B.toInt())
            setTypeface(typeface, android.graphics.Typeface.BOLD)
        }, matchWrap())
        root.addView(TextView(this).apply {
            text = "Введите номер для исходящего звонка"
            textSize = 15f
            setTextColor(0xFF596579.toInt())
            setPadding(0, dp(6), 0, dp(14))
        }, matchWrap())
        numberEdit = EditText(this).apply {
            hint = "Номер телефона"
            inputType = android.text.InputType.TYPE_CLASS_PHONE
            setSingleLine(true)
            textSize = 20f
            setPadding(dp(14), dp(12), dp(14), dp(12))
        }
        intent?.data?.schemeSpecificPart?.let { numberEdit.setText(it) }
        root.addView(numberEdit, matchWrap())

        root.addView(Button(this).apply {
            text = "Позвонить"
            isAllCaps = false
            setOnClickListener { placeOrRequestPermission() }
        }, matchWrap(top = 16))
        root.addView(Button(this).apply {
            text = "Назад"
            isAllCaps = false
            setOnClickListener { finish() }
        }, matchWrap(top = 8))
        setContentView(root)
    }

    private fun placeOrRequestPermission() {
        if (checkSelfPermission(Manifest.permission.CALL_PHONE) != PackageManager.PERMISSION_GRANTED) {
            requestPermissions(arrayOf(Manifest.permission.CALL_PHONE), REQUEST_CALL_PERMISSION)
        } else {
            placeCall()
        }
    }

    private fun placeCall() {
        val number = numberEdit.text.toString().filter { it.isDigit() || it == '+' || it == '*' || it == '#' }
        if (number.isBlank()) {
            numberEdit.error = "Введите номер"
            return
        }
        try {
            val telecomManager = getSystemService(TelecomManager::class.java)
            telecomManager.placeCall(Uri.fromParts("tel", number, null), Bundle())
        } catch (e: SecurityException) {
            Toast.makeText(this, "Android не разрешил выполнить звонок", Toast.LENGTH_LONG).show()
        } catch (e: Exception) {
            Toast.makeText(this, "Не удалось начать звонок: ${e.localizedMessage ?: "ошибка"}", Toast.LENGTH_LONG).show()
        }
    }

    @Deprecated("Use runtime permissions result callback")
    override fun onRequestPermissionsResult(requestCode: Int, permissions: Array<out String>, grantResults: IntArray) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        if (requestCode == REQUEST_CALL_PERMISSION &&
            checkSelfPermission(Manifest.permission.CALL_PHONE) == PackageManager.PERMISSION_GRANTED
        ) {
            placeCall()
        }
    }

    private fun matchWrap(top: Int = 0): LinearLayout.LayoutParams = LinearLayout.LayoutParams(
        ViewGroup.LayoutParams.MATCH_PARENT,
        ViewGroup.LayoutParams.WRAP_CONTENT
    ).apply {
        if (top > 0) topMargin = dp(top)
    }

    private fun dp(value: Int): Int = (resources.displayMetrics.density * value).roundToInt()

    companion object {
        private const val REQUEST_CALL_PERMISSION = 4201
    }
}
