package com.vohive.agent;

import android.app.PendingIntent;
import android.content.ContentResolver;
import android.content.ContentUris;
import android.content.ContentValues;
import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.database.Cursor;
import android.net.Uri;
import android.provider.BaseColumns;
import android.provider.Telephony;
import android.telephony.SmsManager;
import android.telephony.SubscriptionManager;

import org.json.JSONArray;
import org.json.JSONObject;

import java.time.Instant;
import java.util.ArrayList;
import java.util.HashSet;
import java.util.Set;
import java.util.UUID;

final class SmsController {
    static final String ACTION_SENT = "com.vohive.agent.SMS_SENT";
    static final String ACTION_DELIVERED = "com.vohive.agent.SMS_DELIVERED";
    static final String EXTRA_MESSAGE_ID = "message_id";
    static final String EXTRA_PART = "part";
    static final String EXTRA_PARTS_TOTAL = "parts_total";
    static final String EXTRA_SUBSCRIPTION_ID = "subscription_id";
    static final String EXTRA_TO = "to";
    static final String EXTRA_BODY = "body";

    private final Context context;

    SmsController(Context context) {
        this.context = context.getApplicationContext();
    }

    JSONObject send(int subscriptionId, String to, String body) throws Exception {
        to = to == null ? "" : to.trim();
        if (to.isEmpty()) throw new IllegalArgumentException("to is required");
        if (body == null || body.isEmpty()) throw new IllegalArgumentException("body is required");

        SmsManager base = context.getSystemService(SmsManager.class);
        if (base == null) throw new IllegalStateException("SmsManager is unavailable");
        if (!SubscriptionIds.isValid(subscriptionId)) {
            subscriptionId = SubscriptionManager.getDefaultSmsSubscriptionId();
        }
        SmsManager manager = SubscriptionIds.isValid(subscriptionId)
                ? SmsManager.getSmsManagerForSubscriptionId(subscriptionId) : base;

        String messageId = UUID.randomUUID().toString();
        ArrayList<String> parts = manager.divideMessage(body);
        if (parts == null || parts.isEmpty()) {
            parts = new ArrayList<>();
            parts.add(body);
        }
        ArrayList<PendingIntent> sent = new ArrayList<>(parts.size());
        ArrayList<PendingIntent> delivered = new ArrayList<>(parts.size());
        for (int i = 0; i < parts.size(); i++) {
            sent.add(statusIntent(ACTION_SENT, messageId, i + 1, parts.size(), subscriptionId, to, body));
            delivered.add(statusIntent(ACTION_DELIVERED, messageId, i + 1, parts.size(), subscriptionId, to, body));
        }
        manager.sendMultipartTextMessage(to, null, parts, sent, delivered);

        return new JSONObject()
                .put("message_id", messageId)
                .put("parts_total", parts.size())
                .put("subscription_id", subscriptionId)
                .put("state", "queued");
    }

    private PendingIntent statusIntent(String action, String messageId, int part, int total,
                                       int subscriptionId, String to, String body) {
        Intent intent = new Intent(context, SmsStatusReceiver.class)
                .setAction(action)
                .setData(new Uri.Builder().scheme("vohive-agent").authority("sms-status")
                        .appendPath(messageId).appendPath(action).appendPath(Integer.toString(part)).build())
                .putExtra(EXTRA_MESSAGE_ID, messageId)
                .putExtra(EXTRA_PART, part)
                .putExtra(EXTRA_PARTS_TOTAL, total)
                .putExtra(EXTRA_SUBSCRIPTION_ID, subscriptionId)
                .putExtra(EXTRA_TO, to)
                .putExtra(EXTRA_BODY, body);
        int flags = PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE;
        return PendingIntent.getBroadcast(context, 0, intent, flags);
    }

    JSONObject list(int limit) throws Exception {
        int safeLimit = Math.max(1, Math.min(limit <= 0 ? 500 : limit, 2000));
        JSONArray messages = new JSONArray();
        try (Cursor cursor = context.getContentResolver().query(
                Telephony.Sms.CONTENT_URI,
                new String[]{BaseColumns._ID, Telephony.Sms.ADDRESS, Telephony.Sms.BODY,
                        Telephony.Sms.DATE, Telephony.Sms.TYPE, Telephony.Sms.READ,
                        Telephony.Sms.SUBSCRIPTION_ID},
                null, null, Telephony.Sms.DEFAULT_SORT_ORDER + " LIMIT " + safeLimit)) {
            if (cursor != null) {
                while (cursor.moveToNext()) {
                    long id = cursor.getLong(0);
                    String address = empty(cursor.getString(1));
                    int type = cursor.getInt(4);
                    int read = cursor.getInt(5);
                    messages.put(new JSONObject()
                            .put("index", id)
                            .put("sender", type == Telephony.Sms.MESSAGE_TYPE_INBOX ? address : "")
                            .put("recipient", type == Telephony.Sms.MESSAGE_TYPE_INBOX ? "" : address)
                            .put("content", empty(cursor.getString(2)))
                            .put("timestamp", Instant.ofEpochMilli(cursor.getLong(3)).toString())
                            .put("type", type)
                            .put("tag", tagFor(type, read))
                            .put("subscription_id", cursor.getInt(6)));
                }
            }
        }
        return new JSONObject().put("messages", messages);
    }

    JSONObject read(long id) throws Exception {
        try (Cursor cursor = context.getContentResolver().query(
                ContentUris.withAppendedId(Telephony.Sms.CONTENT_URI, id),
                new String[]{BaseColumns._ID, Telephony.Sms.ADDRESS, Telephony.Sms.BODY,
                        Telephony.Sms.DATE, Telephony.Sms.TYPE, Telephony.Sms.READ,
                        Telephony.Sms.SUBSCRIPTION_ID},
                null, null, null)) {
            if (cursor == null || !cursor.moveToFirst()) throw new IllegalArgumentException("SMS not found: " + id);
            int type = cursor.getInt(4);
            String address = empty(cursor.getString(1));
            return new JSONObject()
                    .put("index", cursor.getLong(0))
                    .put("sender", type == Telephony.Sms.MESSAGE_TYPE_INBOX ? address : "")
                    .put("recipient", type == Telephony.Sms.MESSAGE_TYPE_INBOX ? "" : address)
                    .put("content", empty(cursor.getString(2)))
                    .put("timestamp", Instant.ofEpochMilli(cursor.getLong(3)).toString())
                    .put("type", type)
                    .put("tag", tagFor(type, cursor.getInt(5)))
                    .put("subscription_id", cursor.getInt(6));
        }
    }

    JSONObject delete(long id) throws Exception {
        int count = context.getContentResolver().delete(
                ContentUris.withAppendedId(Telephony.Sms.CONTENT_URI, id), null, null);
        return new JSONObject().put("deleted", count);
    }

    JSONObject deleteAll() throws Exception {
        int count = context.getContentResolver().delete(Telephony.Sms.CONTENT_URI, null, null);
        return new JSONObject().put("deleted", count);
    }

    static long insertIncoming(Context context, String sender, String body, long timestamp, int subscriptionId) {
        ContentValues values = new ContentValues();
        values.put(Telephony.Sms.ADDRESS, sender);
        values.put(Telephony.Sms.BODY, body);
        values.put(Telephony.Sms.DATE, timestamp);
        values.put(Telephony.Sms.DATE_SENT, timestamp);
        values.put(Telephony.Sms.READ, 0);
        values.put(Telephony.Sms.SEEN, 0);
        values.put(Telephony.Sms.TYPE, Telephony.Sms.MESSAGE_TYPE_INBOX);
        if (SubscriptionIds.isValid(subscriptionId)) {
            values.put(Telephony.Sms.SUBSCRIPTION_ID, subscriptionId);
        }
        Uri uri = context.getContentResolver().insert(Telephony.Sms.Inbox.CONTENT_URI, values);
        return uri == null ? -1 : ContentUris.parseId(uri);
    }

    static void insertSent(Context context, String to, String body, long timestamp, int subscriptionId) {
        ContentValues values = new ContentValues();
        values.put(Telephony.Sms.ADDRESS, to);
        values.put(Telephony.Sms.BODY, body);
        values.put(Telephony.Sms.DATE, timestamp);
        values.put(Telephony.Sms.DATE_SENT, timestamp);
        values.put(Telephony.Sms.READ, 1);
        values.put(Telephony.Sms.SEEN, 1);
        values.put(Telephony.Sms.TYPE, Telephony.Sms.MESSAGE_TYPE_SENT);
        if (SubscriptionIds.isValid(subscriptionId)) {
            values.put(Telephony.Sms.SUBSCRIPTION_ID, subscriptionId);
        }
        context.getContentResolver().insert(Telephony.Sms.Sent.CONTENT_URI, values);
    }

    static boolean isDefaultSmsApp(Context context) {
        String current = Telephony.Sms.getDefaultSmsPackage(context);
        return context.getPackageName().equals(current);
    }

    static synchronized boolean recordSentPart(Context context, String messageId, int part,
                                                int total, boolean succeeded) {
        if (messageId == null || messageId.isEmpty()) return false;
        String key = "sent:" + messageId;
        SharedPreferences prefs = context.getSharedPreferences("vohive-sms-send-state", Context.MODE_PRIVATE);
        if (!succeeded || part <= 0 || total <= 0) {
            prefs.edit().remove(key).apply();
            return false;
        }
        Set<String> completed = new HashSet<>(prefs.getStringSet(key, new HashSet<>()));
        completed.add(Integer.toString(part));
        if (completed.size() >= total) {
            prefs.edit().remove(key).apply();
            return true;
        }
        prefs.edit().putStringSet(key, completed).apply();
        return false;
    }

    private static int tagFor(int type, int read) {
        if (type == Telephony.Sms.MESSAGE_TYPE_INBOX) return read == 0 ? 1 : 0;
        if (type == Telephony.Sms.MESSAGE_TYPE_SENT) return 2;
        return 3;
    }

    private static String empty(String value) { return value == null ? "" : value; }
}
