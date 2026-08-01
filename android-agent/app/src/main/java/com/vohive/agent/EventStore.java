package com.vohive.agent;

import android.annotation.SuppressLint;
import android.content.Context;
import android.content.SharedPreferences;

import org.json.JSONArray;
import org.json.JSONObject;

import java.time.Instant;
import java.util.ArrayList;
import java.util.List;
import java.util.UUID;

@SuppressLint("ApplySharedPref") // Event delivery requires synchronous durable writes before ACK/reconnect.
final class EventStore {
    private static final Object LOCK = new Object();
    private static final String PREFS = "vohive-agent-events";
    private static final String KEY_EVENTS = "events";
    private static final int MAX_EVENTS = 250;

    private EventStore() {}

    static void enqueue(Context context, String type, JSONObject result) {
        if (context == null || result == null) return;
        synchronized (LOCK) {
            SharedPreferences prefs = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE);
            JSONArray existing;
            try {
                existing = new JSONArray(prefs.getString(KEY_EVENTS, "[]"));
            } catch (Exception ignored) {
                existing = new JSONArray();
            }
            JSONArray next = new JSONArray();
            int start = Math.max(0, existing.length() - MAX_EVENTS + 1);
            for (int i = start; i < existing.length(); i++) {
                next.put(existing.opt(i));
            }
            JSONObject envelope = new JSONObject();
            try {
                envelope.put("type", type);
                envelope.put("protocol_version", 1);
                envelope.put("event_id", UUID.randomUUID().toString());
                envelope.put("result", result);
                envelope.put("timestamp", Instant.now().toString());
            } catch (Exception ignored) {
                return;
            }
            next.put(envelope);
            prefs.edit().putString(KEY_EVENTS, next.toString()).commit();
        }
    }

    static List<JSONObject> pending(Context context) {
        synchronized (LOCK) {
            SharedPreferences prefs = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE);
            JSONArray events;
            try {
                events = new JSONArray(prefs.getString(KEY_EVENTS, "[]"));
            } catch (Exception ignored) {
                events = new JSONArray();
            }
            List<JSONObject> out = new ArrayList<>(events.length());
            boolean changed = false;
            for (int i = 0; i < events.length(); i++) {
                JSONObject event = events.optJSONObject(i);
                if (event != null) {
                    if (event.optString("event_id").isEmpty()) {
                        try {
                            event.put("event_id", UUID.randomUUID().toString());
                            changed = true;
                        } catch (Exception ignored) {
                        }
                    }
                    out.add(event);
                }
            }
            if (changed) prefs.edit().putString(KEY_EVENTS, events.toString()).commit();
            return out;
        }
    }

    static void acknowledge(Context context, String eventId) {
        if (context == null || eventId == null || eventId.isEmpty()) return;
        synchronized (LOCK) {
            SharedPreferences prefs = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE);
            JSONArray events;
            try {
                events = new JSONArray(prefs.getString(KEY_EVENTS, "[]"));
            } catch (Exception ignored) {
                events = new JSONArray();
            }
            JSONArray remaining = new JSONArray();
            for (int i = 0; i < events.length(); i++) {
                JSONObject event = events.optJSONObject(i);
                if (event != null && !eventId.equals(event.optString("event_id"))) {
                    remaining.put(event);
                }
            }
            SharedPreferences.Editor edit = prefs.edit();
            if (remaining.length() == 0) edit.remove(KEY_EVENTS);
            else edit.putString(KEY_EVENTS, remaining.toString());
            edit.commit();
        }
    }
}
