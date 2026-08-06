package com.vohive.agent;

import android.annotation.SuppressLint;
import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;
import android.telephony.euicc.EuiccManager;

import org.json.JSONObject;

import java.time.Instant;

public class ESIMResultReceiver extends BroadcastReceiver {
    static final String ACTION_SWITCH_RESULT = "com.vohive.agent.ESIM_SWITCH_RESULT";
    static final String ACTION_RESOLUTION_FAILED = "com.vohive.agent.ESIM_RESOLUTION_FAILED";
    private static final String CHANNEL_ID = "vohive-esim";
    private static final int NOTIFICATION_ID = 7101;

    @Override
    @SuppressLint({"InlinedApi", "ApplySharedPref"}) // Persist SIM choice before the service refresh.
    public void onReceive(Context context, Intent intent) {
        if (intent == null) return;
        if (ACTION_RESOLUTION_FAILED.equals(intent.getAction())) {
            enqueueFailure(context, intent.getStringExtra("error"));
            return;
        }
        int result = getResultCode();
        String state;
        if (result == EuiccManager.EMBEDDED_SUBSCRIPTION_RESULT_OK) state = "completed";
        else if (result == EuiccManager.EMBEDDED_SUBSCRIPTION_RESULT_RESOLVABLE_ERROR) state = "user_resolution_required";
        else state = "failed";
        if (result == EuiccManager.EMBEDDED_SUBSCRIPTION_RESULT_RESOLVABLE_ERROR) {
            postResolutionNotification(context, intent);
        } else {
            NotificationManager notifications = context.getSystemService(NotificationManager.class);
            if (notifications != null) notifications.cancel(NOTIFICATION_ID);
        }
        if (result == EuiccManager.EMBEDDED_SUBSCRIPTION_RESULT_OK) {
            int selected = intent.getIntExtra("subscription_id", -1);
            if (SubscriptionIds.isValid(selected)) {
                AgentConfig.prefs(context).edit()
                        .putInt(TelephonyRepository.KEY_SELECTED_SUBSCRIPTION, selected).commit();
            }
            AgentService.refreshTelephony(context);
        }
        try {
            JSONObject event = new JSONObject()
                    .put("operation", "switch")
                    .put("state", state)
                    .put("result_code", result)
                    .put("subscription_id", intent.getIntExtra("subscription_id", -1))
                    .put("port_index", intent.getIntExtra("port_index", -1))
                    .put("detailed_code", intent.getIntExtra(
                            EuiccManager.EXTRA_EMBEDDED_SUBSCRIPTION_DETAILED_CODE, 0))
                    .put("timestamp", Instant.now().toString());
            EventStore.enqueue(context, "esim_status", event);
            AgentService.wakeForEvents(context);
        } catch (Exception ignored) {
        }
    }

    private static void postResolutionNotification(Context context, Intent resultIntent) {
        NotificationManager notifications = context.getSystemService(NotificationManager.class);
        if (notifications == null) return;
        notifications.createNotificationChannel(new NotificationChannel(
                CHANNEL_ID, "VoHive eSIM", NotificationManager.IMPORTANCE_HIGH));
        Intent resolution = new Intent(context, ESIMResolutionActivity.class)
                .putExtra(ESIMResolutionActivity.EXTRA_RESULT_INTENT, resultIntent)
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_CLEAR_TOP);
        PendingIntent pending = PendingIntent.getActivity(context, 7102, resolution,
                PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE);
        Notification notification = new Notification.Builder(context, CHANNEL_ID)
                .setSmallIcon(android.R.drawable.stat_sys_warning)
                .setContentTitle("eSIM 切换待确认")
                .setContentText("点按以在系统界面完成授权")
                .setContentIntent(pending)
                .setAutoCancel(true)
                .build();
        notifications.notify(NOTIFICATION_ID, notification);
    }

    private static void enqueueFailure(Context context, String error) {
        try {
            JSONObject event = new JSONObject()
                    .put("operation", "switch")
                    .put("state", "resolution_failed")
                    .put("error", error == null ? "operation failed" : error)
                    .put("timestamp", Instant.now().toString());
            EventStore.enqueue(context, "esim_status", event);
            AgentService.wakeForEvents(context);
        } catch (Exception ignored) {
        }
    }
}
