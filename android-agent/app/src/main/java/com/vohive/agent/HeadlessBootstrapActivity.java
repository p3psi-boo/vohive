package com.vohive.agent;

import android.app.Activity;
import android.content.Intent;
import android.os.Bundle;

/** Invisible ADB entry point used only to cross Android's foreground-service start boundary. */
public final class HeadlessBootstrapActivity extends Activity {
    @Override
    protected void onCreate(Bundle state) {
        super.onCreate(state);
        try {
            startForegroundService(new Intent(this, AgentService.class)
                    .setAction(AgentService.ACTION_RELOAD));
        } finally {
            finish();
        }
    }
}
