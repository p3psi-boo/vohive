package com.vohive.agent;

import android.annotation.SuppressLint;
import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;
import android.provider.Telephony;
import android.telephony.SmsMessage;
import android.telephony.SubscriptionManager;

import org.json.JSONObject;

import java.time.Instant;
import java.util.UUID;

public class SmsReceiver extends BroadcastReceiver {
    @Override
    @SuppressLint("InlinedApi")
    public void onReceive(Context context, Intent intent) {
        if (intent == null) return;
        boolean defaultApp = SmsController.isDefaultSmsApp(context);
        String action = intent.getAction();
        if (defaultApp && !Telephony.Sms.Intents.SMS_DELIVER_ACTION.equals(action)) return;
        if (!defaultApp && !Telephony.Sms.Intents.SMS_RECEIVED_ACTION.equals(action)) return;

        SmsMessage[] parts = Telephony.Sms.Intents.getMessagesFromIntent(intent);
        if (parts == null || parts.length == 0) return;
        String sender = parts[0].getDisplayOriginatingAddress();
        StringBuilder body = new StringBuilder();
        long timestamp = parts[0].getTimestampMillis();
        for (SmsMessage part : parts) {
            if (part != null && part.getDisplayMessageBody() != null) body.append(part.getDisplayMessageBody());
        }

        int subId = intent.getIntExtra(SubscriptionManager.EXTRA_SUBSCRIPTION_INDEX,
                intent.getIntExtra("subscription", SubscriptionManager.INVALID_SUBSCRIPTION_ID));
        int slotIndex = intent.getIntExtra(SubscriptionManager.EXTRA_SLOT_INDEX,
                intent.getIntExtra("slot", -1));
        long providerID = -1;
        if (defaultApp) {
            try {
                providerID = SmsController.insertIncoming(context, sender, body.toString(), timestamp, subId);
            } catch (Exception ignored) {
            }
        }
        String messageId = providerID >= 0 ? Long.toString(providerID)
                : "rx-" + timestamp + "-" + UUID.nameUUIDFromBytes((sender + body).getBytes());
        try {
            JSONObject event = new JSONObject()
                    .put("message_id", messageId)
                    .put("sender", sender == null ? "" : sender)
                    .put("content", body.toString())
                    .put("subscription_id", subId)
                    .put("slot_index", slotIndex)
                    .put("timestamp", Instant.ofEpochMilli(timestamp).toString());
            EventStore.enqueue(context, "sms_received", event);
            AgentService.wakeForEvents(context);
        } catch (Exception ignored) {
        }
    }
}
