package com.vohive.agent;

import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;

public class MmsReceiver extends BroadcastReceiver {
    @Override public void onReceive(Context context, Intent intent) {
        if (intent == null || !"android.provider.Telephony.WAP_PUSH_DELIVER".equals(intent.getAction())) return;
        // SMS role qualification receiver. VoHive Agent currently transports text SMS only.
    }
}
