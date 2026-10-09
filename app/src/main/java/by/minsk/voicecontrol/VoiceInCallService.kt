package by.minsk.voicecontrol

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Person
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.os.Build
import android.os.Bundle
import android.telecom.Call
import android.telecom.InCallService
import android.view.WindowManager

/** Receives carrier/system call state when this app has the Android dialer role. */
class VoiceInCallService : InCallService() {
    private val callbacks = mutableMapOf<Call, Call.Callback>()

    override fun onCreate() {
        super.onCreate()
        createCallNotificationChannel()
    }

    override fun onCallAdded(call: Call) {
        super.onCallAdded(call)
        VoiceCallRegistry.add(call)
        val callback = object : Call.Callback() {
            override fun onStateChanged(call: Call, state: Int) {
                refreshCallUi(call)
            }

            override fun onDetailsChanged(call: Call, details: Call.Details) {
                refreshCallUi(call)
            }
        }
        callbacks[call] = callback
        call.registerCallback(callback)
        refreshCallUi(call)
    }

    override fun onCallRemoved(call: Call) {
        callbacks.remove(call)?.let { call.unregisterCallback(it) }
        VoiceCallRegistry.remove(call)
        super.onCallRemoved(call)
        VoiceCallRegistry.publishState(this)
        refreshNotifications()
    }

    private fun refreshCallUi(call: Call) {
        VoiceCallRegistry.publishState(this)
        when (call.state) {
            Call.STATE_RINGING -> showCallNotification(call, incoming = true)
            Call.STATE_ACTIVE, Call.STATE_DIALING, Call.STATE_CONNECTING, Call.STATE_HOLDING ->
                showCallNotification(call, incoming = false)
            Call.STATE_DISCONNECTED, Call.STATE_DISCONNECTING -> refreshNotifications()
        }
    }

    private fun refreshNotifications() {
        val call = VoiceCallRegistry.currentCall()
        if (call == null) {
            (getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager).cancel(CALL_NOTIFICATION_ID)
        } else {
            showCallNotification(call, incoming = call.state == Call.STATE_RINGING)
        }
    }

    private fun showCallNotification(call: Call, incoming: Boolean) {
        val notificationManager = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        val caller = callerName(call)
        val screenIntent = Intent(this, InCallActivity::class.java).apply {
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_CLEAR_TOP)
        }
        val screenPendingIntent = PendingIntent.getActivity(
            this,
            810,
            screenIntent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )
        val answerIntent = actionPendingIntent(CallActionReceiver.ACTION_ANSWER, 811)
        val declineIntent = actionPendingIntent(CallActionReceiver.ACTION_REJECT, 812)
        val hangUpIntent = actionPendingIntent(CallActionReceiver.ACTION_HANG_UP, 813)

        val builder = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            Notification.Builder(this, CALL_CHANNEL_ID)
        } else {
            @Suppress("DEPRECATION")
            Notification.Builder(this)
        }
        builder
            .setSmallIcon(R.drawable.ic_phone)
            .setCategory(Notification.CATEGORY_CALL)
            .setContentIntent(screenPendingIntent)
            .setFullScreenIntent(if (incoming) screenPendingIntent else null, incoming)
            .setOngoing(incoming || call.state == Call.STATE_ACTIVE)
            .setAutoCancel(false)
            .setVisibility(Notification.VISIBILITY_PUBLIC)
            .setOnlyAlertOnce(true)

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            val person = Person.Builder().setName(caller).setImportant(true).build()
            val style = if (incoming) {
                Notification.CallStyle.forIncomingCall(person, declineIntent, answerIntent)
            } else {
                Notification.CallStyle.forOngoingCall(person, hangUpIntent)
            }
            builder.setStyle(style)
        } else {
            builder
                .setContentTitle(caller)
                .setContentText(if (incoming) "Входящий звонок" else VoiceCallRegistry.stateLabel())
                .setPriority(Notification.PRIORITY_MAX)
            if (incoming) {
                builder
                    .addAction(Notification.Action.Builder(R.drawable.ic_phone, "Отклонить", declineIntent).build())
                    .addAction(Notification.Action.Builder(R.drawable.ic_phone, "Ответить", answerIntent).build())
            } else {
                builder.addAction(Notification.Action.Builder(R.drawable.ic_phone, "Завершить", hangUpIntent).build())
            }
        }
        notificationManager.notify(CALL_NOTIFICATION_ID, builder.build())
    }

    private fun callerName(call: Call): String {
        val details = call.details
        return details?.callerDisplayName?.toString()?.takeIf { it.isNotBlank() }
            ?: details?.handle?.schemeSpecificPart?.takeIf { it.isNotBlank() }
            ?: "Телефон"
    }

    private fun actionPendingIntent(action: String, requestCode: Int): PendingIntent {
        val intent = Intent(this, CallActionReceiver::class.java).setAction(action)
        return PendingIntent.getBroadcast(
            this,
            requestCode,
            intent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )
    }

    private fun createCallNotificationChannel() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val channel = NotificationChannel(
                CALL_CHANNEL_ID,
                getString(R.string.call_channel_name),
                NotificationManager.IMPORTANCE_HIGH
            ).apply {
                description = "Уведомления о входящих и текущих звонках"
                lockscreenVisibility = Notification.VISIBILITY_PUBLIC
                enableVibration(true)
            }
            (getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager)
                .createNotificationChannel(channel)
        }
    }

    companion object {
        private const val CALL_CHANNEL_ID = "calls"
        private const val CALL_NOTIFICATION_ID = 2202
    }
}
