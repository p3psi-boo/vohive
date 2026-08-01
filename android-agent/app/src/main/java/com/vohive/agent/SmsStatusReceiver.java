package com.vohive.agent;

import android.app.Activity;
import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;

import org.json.JSONObject;

import java.time.Instant;

public class SmsStatusReceiver extends BroadcastReceiver {
    @Override
    public void onReceive(Context context, Intent intent) {
        if (intent == null) return;
        boolean delivered = SmsController.ACTION_DELIVERED.equals(intent.getAction());
        int result = getResultCode();
        String state = delivered
                ? (result == Activity.RESULT_OK ? "delivered" : "delivery_failed")
                : (result == Activity.RESULT_OK ? "sent" : "send_failed");
        int part = intent.getIntExtra(SmsController.EXTRA_PART, 0);
        int total = intent.getIntExtra(SmsController.EXTRA_PARTS_TOTAL, 0);
        int subId = intent.getIntExtra(SmsController.EXTRA_SUBSCRIPTION_ID, -1);
        try {
            JSONObject event = new JSONObject()
                    .put("message_id", intent.getStringExtra(SmsController.EXTRA_MESSAGE_ID))
                    .put("part", part)
                    .put("parts_total", total)
                    .put("state", state)
                    .put("result_code", result)
                    .put("subscription_id", subId)
                    .put("timestamp", Instant.now().toString());
            EventStore.enqueue(context, "sms_status", event);
            boolean allSent = !delivered && SmsController.recordSentPart(context,
                    intent.getStringExtra(SmsController.EXTRA_MESSAGE_ID), part, total,
                    result == Activity.RESULT_OK);
            if (allSent && SmsController.isDefaultSmsApp(context)) {
                SmsController.insertSent(context,
                        intent.getStringExtra(SmsController.EXTRA_TO),
                        intent.getStringExtra(SmsController.EXTRA_BODY),
                        System.currentTimeMillis(), subId);
            }
            AgentService.wakeForEvents(context);
        } catch (Exception ignored) {
        }
    }
}
