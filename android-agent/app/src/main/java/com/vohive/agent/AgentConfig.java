package com.vohive.agent;

import android.annotation.SuppressLint;
import android.content.Context;
import android.content.SharedPreferences;
import android.util.Log;

import java.util.UUID;

@SuppressLint("ApplySharedPref") // Initialization must complete before service startup reads preferences.
final class AgentConfig {
    static final String PREFS = "vohive-agent";
    static final String KEY_SERVER_URL = "server_url";
    static final String KEY_DEVICE_ID = "device_id";
    static final String KEY_AGENT_ID = "agent_id";
    static final String KEY_PAIR_TOKEN = "pair_token";
    static final String KEY_AGENT_TOKEN = "agent_token";
    static final String KEY_DISCOVERED_SERVER_URL = "discovered_server_url";
    static final String KEY_AUTO_START = "auto_start";
    static final String KEY_AGENT_ENABLED = "agent_enabled";
    static final String KEY_CONNECTION_STATE = "connection_state";
    static final String KEY_DIAGNOSTICS_JSON = "diagnostics_json";
    static final String KEY_HTTP_PORT = "http_port";
    static final String KEY_WEB_USERNAME = "web_username";
    static final String KEY_WEB_PASSWORD_SALT = "web_password_salt";
    static final String KEY_WEB_PASSWORD_HASH = "web_password_hash";
    static final String KEY_WEB_PASSWORD_ITERATIONS = "web_password_iterations";

    static final int DEFAULT_HTTP_PORT = 8765;
    static final String DEFAULT_WEB_USERNAME = "admin";
    static final String DEFAULT_WEB_PASSWORD = "admin";

    private static final String TAG = "VoHiveAgent";

    private AgentConfig() {}

    static SharedPreferences prefs(Context context) {
        return context.getSharedPreferences(PREFS, Context.MODE_PRIVATE);
    }

    static synchronized void ensureInitialized(Context context) {
        SharedPreferences prefs = prefs(context);
        SharedPreferences.Editor edit = prefs.edit();
        boolean changed = false;
        if (empty(prefs.getString(KEY_AGENT_ID, ""))) {
            edit.putString(KEY_AGENT_ID, UUID.randomUUID().toString());
            changed = true;
        }
        if (!prefs.contains(KEY_HTTP_PORT)) {
            edit.putInt(KEY_HTTP_PORT, DEFAULT_HTTP_PORT);
            changed = true;
        }
        if (empty(prefs.getString(KEY_WEB_USERNAME, ""))) {
            edit.putString(KEY_WEB_USERNAME, DEFAULT_WEB_USERNAME);
            changed = true;
        }
        if (!prefs.contains(KEY_AGENT_ENABLED)) {
            edit.putBoolean(KEY_AGENT_ENABLED, hasUpstreamCredentials(prefs));
            changed = true;
        }
        if (changed) edit.commit();

        prefs = prefs(context);
        if (empty(prefs.getString(KEY_WEB_PASSWORD_HASH, ""))) {
            // 出厂默认密码 admin，用户可在本地网页的设置页修改。
            WebAuth.setPassword(context, DEFAULT_WEB_PASSWORD);
            Log.i(TAG, "Initialized default web credentials (password: admin)");
        }
    }

    static int httpPort(Context context) {
        int port = prefs(context).getInt(KEY_HTTP_PORT, DEFAULT_HTTP_PORT);
        return port > 0 && port <= 65535 ? port : DEFAULT_HTTP_PORT;
    }

    static boolean agentEnabled(Context context) {
        SharedPreferences prefs = prefs(context);
        return prefs.getBoolean(KEY_AGENT_ENABLED, hasUpstreamCredentials(prefs));
    }

    private static boolean hasUpstreamCredentials(SharedPreferences prefs) {
        return !empty(prefs.getString(KEY_SERVER_URL, ""))
                && (!empty(prefs.getString(KEY_AGENT_TOKEN, ""))
                || !empty(prefs.getString(KEY_PAIR_TOKEN, "")));
    }

    private static boolean empty(String value) {
        return value == null || value.trim().isEmpty();
    }
}
