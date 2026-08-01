package com.vohive.agent;

import android.telephony.SubscriptionManager;

final class SubscriptionIds {
    private SubscriptionIds() {}

    static boolean isValid(int subscriptionId) {
        return subscriptionId >= 0 && subscriptionId != SubscriptionManager.INVALID_SUBSCRIPTION_ID;
    }
}
