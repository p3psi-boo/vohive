package com.vohive.agent;

import android.app.PendingIntent;
import android.content.Context;
import android.content.Intent;
import android.net.Uri;
import android.os.Build;
import android.telephony.euicc.EuiccManager;

import org.json.JSONObject;

import java.util.UUID;

final class ESIMController {
    private final Context context;

    ESIMController(Context context) {
        this.context = context.getApplicationContext();
    }

    JSONObject switchProfile(int subscriptionId, int portIndex) throws Exception {
        if (Build.VERSION.SDK_INT < 28) throw new IllegalStateException("eSIM requires Android 9+");
        if (!SubscriptionIds.isValid(subscriptionId)) {
            throw new IllegalArgumentException("invalid subscription_id");
        }
        EuiccManager manager = context.getSystemService(EuiccManager.class);
        if (manager == null || !manager.isEnabled()) throw new IllegalStateException("eSIM is not enabled");
        Intent callback = new Intent(context, ESIMResultReceiver.class)
                .setAction(ESIMResultReceiver.ACTION_SWITCH_RESULT)
                .setData(new Uri.Builder().scheme("vohive-agent").authority("esim-switch")
                        .appendPath(UUID.randomUUID().toString()).build())
                .putExtra("subscription_id", subscriptionId)
                .putExtra("port_index", portIndex);
        int flags = PendingIntent.FLAG_UPDATE_CURRENT;
        if (Build.VERSION.SDK_INT >= 31) flags |= PendingIntent.FLAG_MUTABLE;
        PendingIntent pending = PendingIntent.getBroadcast(
                context, 0, callback, flags);
        if (Build.VERSION.SDK_INT >= 33) {
            manager.switchToSubscription(subscriptionId, portIndex, pending);
        } else {
            manager.switchToSubscription(subscriptionId, pending);
        }
        return new JSONObject()
                .put("state", "pending")
                .put("subscription_id", subscriptionId)
                .put("port_index", portIndex);
    }

}
