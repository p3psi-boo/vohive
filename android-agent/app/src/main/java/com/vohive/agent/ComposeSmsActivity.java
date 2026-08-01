package com.vohive.agent;

import android.app.Activity;
import android.os.Bundle;

/**
 * Declared only to satisfy Android's default-SMS role contract. Message
 * composition is intentionally handled by the authenticated web console.
 */
public final class ComposeSmsActivity extends Activity {
    @Override
    protected void onCreate(Bundle state) {
        super.onCreate(state);
        finish();
    }
}
