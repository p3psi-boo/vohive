package com.vohive.agent;

import android.app.Service;
import android.content.Intent;
import android.net.Uri;
import android.os.IBinder;
import android.telephony.SubscriptionManager;

public class RespondViaMessageService extends Service {
    @Override public IBinder onBind(Intent intent) { return null; }

    @Override public int onStartCommand(Intent intent, int flags, int startId) {
        if (intent != null) {
            Uri data = intent.getData();
            String to = data == null ? "" : data.getSchemeSpecificPart();
            String body = intent.getStringExtra(Intent.EXTRA_TEXT);
            try {
                TelephonyRepository repo = new TelephonyRepository(this, null);
                new SmsController(this).send(repo.selectedSubscriptionId(), to, body);
            } catch (Exception ignored) {
            }
        }
        stopSelf(startId);
        return START_NOT_STICKY;
    }
}
