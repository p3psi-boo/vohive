package com.vohive.agent;

import android.app.Activity;
import android.app.PendingIntent;
import android.content.Intent;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
import android.telephony.euicc.EuiccManager;

import java.util.UUID;

public class ESIMResolutionActivity extends Activity {
    static final String EXTRA_RESULT_INTENT = "euicc_result_intent";

    @Override
    @SuppressWarnings("deprecation")
    protected void onCreate(Bundle state) {
        super.onCreate(state);
        try {
            if (Build.VERSION.SDK_INT < 28) throw new IllegalStateException("eSIM requires Android 9+");
            Intent resultIntent = getIntent().getParcelableExtra(EXTRA_RESULT_INTENT);
            if (resultIntent == null) throw new IllegalArgumentException("eSIM result intent is missing");
            int subscriptionId = resultIntent.getIntExtra("subscription_id", -1);
            int portIndex = resultIntent.getIntExtra("port_index", -1);
            Intent callback = new Intent(this, ESIMResultReceiver.class)
                    .setAction(ESIMResultReceiver.ACTION_SWITCH_RESULT)
                    .setData(new Uri.Builder().scheme("vohive-agent").authority("esim-resolution")
                            .appendPath(UUID.randomUUID().toString()).build())
                    .putExtra("subscription_id", subscriptionId)
                    .putExtra("port_index", portIndex);
            int flags = PendingIntent.FLAG_UPDATE_CURRENT;
            if (Build.VERSION.SDK_INT >= 31) flags |= PendingIntent.FLAG_MUTABLE;
            PendingIntent pending = PendingIntent.getBroadcast(this, 0, callback, flags);
            EuiccManager manager = getSystemService(EuiccManager.class);
            if (manager == null) throw new IllegalStateException("EuiccManager is unavailable");
            manager.startResolutionActivity(this, 7002, resultIntent, pending);
        } catch (Exception error) {
            Intent failure = new Intent(this, ESIMResultReceiver.class)
                    .setAction(ESIMResultReceiver.ACTION_RESOLUTION_FAILED)
                    .putExtra("error", error.getMessage());
            sendBroadcast(failure);
            finish();
        }
    }

    @Override
    protected void onActivityResult(int requestCode, int resultCode, Intent data) {
        super.onActivityResult(requestCode, resultCode, data);
        finish();
    }
}
