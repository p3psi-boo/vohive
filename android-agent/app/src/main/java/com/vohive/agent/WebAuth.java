package com.vohive.agent;

import android.annotation.SuppressLint;
import android.content.Context;
import android.content.SharedPreferences;
import android.util.Base64;

import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.SecureRandom;

import javax.crypto.SecretKeyFactory;
import javax.crypto.spec.PBEKeySpec;

@SuppressLint("ApplySharedPref") // New credentials must be durable before sessions are invalidated.
final class WebAuth {
    static final int ITERATIONS = 210_000;
    private static final int SALT_BYTES = 16;
    private static final int KEY_BITS = 256;
    private static final SecureRandom RANDOM = new SecureRandom();

    private WebAuth() {}

    static void setPassword(Context context, String password) {
        if (password == null || password.length() < 12) {
            throw new IllegalArgumentException("password must contain at least 12 characters");
        }
        byte[] salt = new byte[SALT_BYTES];
        RANDOM.nextBytes(salt);
        byte[] hash = derive(password, salt, ITERATIONS);
        AgentConfig.prefs(context).edit()
                .putString(AgentConfig.KEY_WEB_PASSWORD_SALT, encode(salt))
                .putString(AgentConfig.KEY_WEB_PASSWORD_HASH, encode(hash))
                .putInt(AgentConfig.KEY_WEB_PASSWORD_ITERATIONS, ITERATIONS)
                .commit();
    }

    static boolean verify(Context context, String username, String password) {
        SharedPreferences prefs = AgentConfig.prefs(context);
        String expectedUser = prefs.getString(
                AgentConfig.KEY_WEB_USERNAME, AgentConfig.DEFAULT_WEB_USERNAME);
        String saltText = prefs.getString(AgentConfig.KEY_WEB_PASSWORD_SALT, "");
        String hashText = prefs.getString(AgentConfig.KEY_WEB_PASSWORD_HASH, "");
        int iterations = prefs.getInt(AgentConfig.KEY_WEB_PASSWORD_ITERATIONS, ITERATIONS);
        if (username == null || password == null || saltText == null || hashText == null
                || saltText.isEmpty() || hashText.isEmpty() || iterations < 100_000) {
            return false;
        }
        try {
            byte[] suppliedUser = username.getBytes(StandardCharsets.UTF_8);
            byte[] configuredUser = expectedUser == null
                    ? new byte[0] : expectedUser.getBytes(StandardCharsets.UTF_8);
            boolean userMatches = MessageDigest.isEqual(suppliedUser, configuredUser);
            byte[] expected = decode(hashText);
            byte[] actual = derive(password, decode(saltText), iterations);
            return userMatches && MessageDigest.isEqual(actual, expected);
        } catch (Exception ignored) {
            return false;
        }
    }

    private static byte[] derive(String password, byte[] salt, int iterations) {
        PBEKeySpec spec = new PBEKeySpec(password.toCharArray(), salt, iterations, KEY_BITS);
        try {
            return SecretKeyFactory.getInstance("PBKDF2WithHmacSHA256")
                    .generateSecret(spec).getEncoded();
        } catch (Exception error) {
            throw new IllegalStateException("PBKDF2 is unavailable", error);
        } finally {
            spec.clearPassword();
        }
    }

    private static String encode(byte[] value) {
        return Base64.encodeToString(value, Base64.NO_WRAP);
    }

    private static byte[] decode(String value) {
        return Base64.decode(value, Base64.NO_WRAP);
    }
}
