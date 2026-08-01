package com.vohive.agent;

import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;

public class BootReceiver extends BroadcastReceiver {
    @Override
    public void onReceive(Context context, Intent intent) {
        if (intent == null || !Intent.ACTION_BOOT_COMPLETED.equals(intent.getAction())) return;
        AgentConfig.ensureInitialized(context);
        boolean autoStart = AgentConfig.prefs(context)
                .getBoolean(AgentConfig.KEY_AUTO_START, true);
        if (!autoStart) return;
        Intent service = new Intent(context, AgentService.class).setAction(AgentService.ACTION_START);
        context.startForegroundService(service);
    }
}
