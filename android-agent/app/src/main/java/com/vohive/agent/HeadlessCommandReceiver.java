package com.vohive.agent;

import android.annotation.SuppressLint;
import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;

@SuppressLint("ApplySharedPref") // Provisioning must complete before the foreground service starts.
public final class HeadlessCommandReceiver extends BroadcastReceiver {
    public static final String ACTION_PROVISION = "com.vohive.agent.PROVISION";

    @Override
    public void onReceive(Context context, Intent intent) {
        if (intent == null || !ACTION_PROVISION.equals(intent.getAction())) return;
        provision(context, intent);
    }

    private static void provision(Context context, Intent intent) {
        AgentConfig.ensureInitialized(context);
        SharedPreferences.Editor edit = AgentConfig.prefs(context).edit();
        putString(intent, edit, "server_url", AgentConfig.KEY_SERVER_URL);
        putString(intent, edit, "device_id", AgentConfig.KEY_DEVICE_ID);
        putString(intent, edit, "agent_id", AgentConfig.KEY_AGENT_ID);
        putString(intent, edit, "web_username", AgentConfig.KEY_WEB_USERNAME);
        if (intent.hasExtra("pair_token")) {
            String token = intent.getStringExtra("pair_token");
            edit.putString(AgentConfig.KEY_PAIR_TOKEN, token == null ? "" : token.trim())
                    .remove(AgentConfig.KEY_AGENT_TOKEN);
        }
        if (intent.hasExtra("http_port")) {
            int port = intent.getIntExtra("http_port", AgentConfig.DEFAULT_HTTP_PORT);
            if (port <= 0 || port > 65535) throw new IllegalArgumentException("invalid http_port");
            edit.putInt(AgentConfig.KEY_HTTP_PORT, port);
        }
        if (intent.hasExtra("auto_start")) {
            edit.putBoolean(AgentConfig.KEY_AUTO_START,
                    intent.getBooleanExtra("auto_start", true));
        }
        if (intent.hasExtra("agent_enabled")) {
            edit.putBoolean(AgentConfig.KEY_AGENT_ENABLED,
                    intent.getBooleanExtra("agent_enabled", true));
        }
        edit.commit();
        if (intent.hasExtra("web_password")) {
            WebAuth.setPassword(context, intent.getStringExtra("web_password"));
        }
    }

    private static void putString(Intent intent, SharedPreferences.Editor edit,
                                  String extra, String key) {
        if (!intent.hasExtra(extra)) return;
        String value = intent.getStringExtra(extra);
        edit.putString(key, value == null ? "" : value.trim());
    }
}
