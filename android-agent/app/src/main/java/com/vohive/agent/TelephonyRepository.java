package com.vohive.agent;

import android.Manifest;
import android.annotation.SuppressLint;
import android.annotation.TargetApi;
import android.content.Context;
import android.content.Intent;
import android.content.IntentFilter;
import android.content.SharedPreferences;
import android.content.pm.PackageManager;
import android.net.ConnectivityManager;
import android.net.LinkAddress;
import android.net.LinkProperties;
import android.net.Network;
import android.net.NetworkCapabilities;
import android.net.NetworkRequest;
import android.net.TelephonyNetworkSpecifier;
import android.os.BatteryManager;
import android.os.Build;
import android.provider.Settings;
import android.telephony.AccessNetworkConstants;
import android.telephony.CellSignalStrength;
import android.telephony.CellSignalStrengthGsm;
import android.telephony.CellSignalStrengthLte;
import android.telephony.CellSignalStrengthNr;
import android.telephony.NetworkRegistrationInfo;
import android.telephony.PhoneStateListener;
import android.telephony.ServiceState;
import android.telephony.SignalStrength;
import android.telephony.SubscriptionInfo;
import android.telephony.SubscriptionManager;
import android.telephony.TelephonyCallback;
import android.telephony.TelephonyDisplayInfo;
import android.telephony.TelephonyManager;
import android.telephony.euicc.EuiccManager;
import android.util.Log;

import org.json.JSONArray;
import org.json.JSONObject;

import java.net.Inet4Address;
import java.net.Inet6Address;
import java.time.Instant;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.Executor;

final class TelephonyRepository {
    private static final String TAG = "VoHiveTelephony";
    interface Listener { void onChanged(); }

    static final String KEY_SELECTED_SUBSCRIPTION = "selected_subscription_id";
    private final Context context;
    private final SharedPreferences prefs;
    private final ConnectivityManager connectivity;
    private final SubscriptionManager subscriptions;
    private final TelephonyManager telephony;
    private final Listener listener;

    private volatile Network cellularNetwork;
    private volatile LinkProperties linkProperties;
    private volatile SignalStrength signalStrength;
    private volatile ServiceState serviceState;
    private volatile TelephonyDisplayInfo displayInfo;
    private ConnectivityManager.NetworkCallback networkCallback;
    private TelephonyManager callbackTelephony;
    private TelephonyCallback modernCallback;
    private PhoneStateListener legacyCallback;

    TelephonyRepository(Context context, Listener listener) {
        this.context = context.getApplicationContext();
        this.listener = listener;
        this.prefs = AgentConfig.prefs(context);
        this.connectivity = context.getSystemService(ConnectivityManager.class);
        this.subscriptions = context.getSystemService(SubscriptionManager.class);
        this.telephony = context.getSystemService(TelephonyManager.class);
    }

    void start() {
        registerTelephonyCallbacks();
        requestSelectedCellularNetwork();
    }

    void stop() {
        if (connectivity != null && networkCallback != null) {
            try { connectivity.unregisterNetworkCallback(networkCallback); } catch (Exception ignored) {}
            networkCallback = null;
        }
        TelephonyManager selected = callbackTelephony;
        if (selected != null && Build.VERSION.SDK_INT >= 31 && modernCallback != null) {
            try { selected.unregisterTelephonyCallback(modernCallback); } catch (Exception ignored) {}
        }
        if (selected != null && legacyCallback != null) {
            try { selected.listen(legacyCallback, PhoneStateListener.LISTEN_NONE); } catch (Exception ignored) {}
        }
        modernCallback = null;
        legacyCallback = null;
        callbackTelephony = null;
        signalStrength = null;
        serviceState = null;
        displayInfo = null;
        cellularNetwork = null;
        linkProperties = null;
    }

    synchronized void selectSubscription(int subscriptionId) {
        boolean found = false;
        for (SubscriptionInfo info : activeSubscriptions()) {
            if (info.getSubscriptionId() == subscriptionId) {
                found = true;
                break;
            }
        }
        if (!found) throw new IllegalArgumentException("subscription is not active: " + subscriptionId);
        stop();
        prefs.edit().putInt(KEY_SELECTED_SUBSCRIPTION, subscriptionId).apply();
        start();
        changed();
    }

    synchronized void refresh() {
        stop();
        start();
        changed();
    }

    int selectedSubscriptionId() {
        int configured = prefs.getInt(KEY_SELECTED_SUBSCRIPTION, SubscriptionManager.INVALID_SUBSCRIPTION_ID);
        List<SubscriptionInfo> active = activeSubscriptions();
        for (SubscriptionInfo info : active) {
            if (info.getSubscriptionId() == configured) return configured;
        }
        int data = SubscriptionManager.getDefaultDataSubscriptionId();
        for (SubscriptionInfo info : active) {
            if (info.getSubscriptionId() == data) return data;
        }
        int sms = SubscriptionManager.getDefaultSmsSubscriptionId();
        for (SubscriptionInfo info : active) {
            if (info.getSubscriptionId() == sms) return sms;
        }
        return active.isEmpty() ? SubscriptionManager.INVALID_SUBSCRIPTION_ID : active.get(0).getSubscriptionId();
    }

    Network selectedNetwork() { return cellularNetwork; }

    TelephonyManager telephonyForSelected() { return selectedTelephonyManager(); }

    synchronized void releaseSelectedCellularNetwork() {
        if (connectivity != null && networkCallback != null) {
            try { connectivity.unregisterNetworkCallback(networkCallback); } catch (Exception ignored) {}
            networkCallback = null;
        }
        cellularNetwork = null;
        linkProperties = null;
        changed();
    }

    @SuppressLint({"MissingPermission", "HardwareIds"})
    JSONObject snapshot() throws Exception {
        int selectedID = selectedSubscriptionId();
        TelephonyManager tm = selectedTelephonyManager();
        SubscriptionInfo selected = findSubscription(selectedID);
        JSONObject snap = new JSONObject();

        if (tm != null) {
            putNonEmpty(snap, "imei", safe(() -> tm.getImei()));
            putNonEmpty(snap, "imsi", safe(() -> tm.getSubscriberId()));
            String iccid = selected == null ? "" : safe(selected::getIccId);
            if (iccid.isEmpty()) iccid = safe(tm::getSimSerialNumber);
            putNonEmpty(snap, "iccid", iccid);
            putNonEmpty(snap, "msisdn", phoneNumber(selectedID, tm));
            putNonEmpty(snap, "operator", safe(tm::getNetworkOperatorName));
            putNonEmpty(snap, "network_mode", networkTypeName(safeInt(tm::getDataNetworkType)));
            String operator = safe(tm::getNetworkOperator);
            if (operator.length() >= 5) {
                try {
                    snap.put("mcc", Integer.parseInt(operator.substring(0, 3)));
                    snap.put("mnc", Integer.parseInt(operator.substring(3)));
                } catch (NumberFormatException ignored) {}
            }
            snap.put("sim_inserted", safeInt(tm::getSimState) == TelephonyManager.SIM_STATE_READY);
        }
        putNonEmpty(snap, "firmware", Build.DISPLAY + " / Android " + Build.VERSION.RELEASE
                + " (" + Build.VERSION.INCREMENTAL + ")");
        putNonEmpty(snap, "baseband", safe(Build::getRadioVersion));
        SignalStrength currentSignal = signalStrength;
        ServiceState currentServiceState = serviceState;
        if (tm != null) {
            if (currentSignal == null && Build.VERSION.SDK_INT >= 28) {
                try { currentSignal = tm.getSignalStrength(); } catch (Exception ignored) {}
            }
            if (currentServiceState == null) {
                try { currentServiceState = tm.getServiceState(); } catch (Exception ignored) {}
            }
        }
        appendSignal(snap, currentSignal);
        appendServiceState(snap, currentServiceState);
        if (Build.VERSION.SDK_INT >= 31) appendDisplayInfo(snap, displayInfo);
        appendBattery(snap);
        appendAddresses(snap);
        appendESIM(snap);
        appendAccess(snap, tm);
        snap.put("data_connected", cellularNetwork != null);
        snap.put("selected_subscription_id", selectedID);
        snap.put("subscriptions", subscriptionsJSON());
        snap.put("updated_at", Instant.now().toString());
        return snap;
    }

    JSONArray subscriptionsJSON() {
        JSONArray out = new JSONArray();
        int selected = selectedSubscriptionId();
        int defaultData = SubscriptionManager.getDefaultDataSubscriptionId();
        int defaultSMS = SubscriptionManager.getDefaultSmsSubscriptionId();
        int defaultVoice = SubscriptionManager.getDefaultVoiceSubscriptionId();
        for (SubscriptionInfo info : accessibleSubscriptions()) {
            try {
                int subID = info.getSubscriptionId();
                TelephonyManager tm = telephony == null ? null : telephony.createForSubscriptionId(subID);
                JSONObject item = new JSONObject()
                        .put("subscription_id", subID)
                        .put("slot_index", info.getSimSlotIndex())
                        .put("port_index", Build.VERSION.SDK_INT >= 33 ? info.getPortIndex() : 0)
                        .put("carrier_name", string(info.getCarrierName()))
                        .put("display_name", string(info.getDisplayName()))
                        .put("iccid", safe(info::getIccId))
                        .put("imsi", tm == null ? "" : safe(tm::getSubscriberId))
                        .put("imei", tm == null ? "" : safe(tm::getImei))
                        .put("msisdn", tm == null ? "" : phoneNumber(subID, tm))
                        .put("country_iso", info.getCountryIso() == null ? "" : info.getCountryIso())
                        .put("embedded", Build.VERSION.SDK_INT >= 28 && info.isEmbedded())
                        .put("opportunistic", Build.VERSION.SDK_INT >= 29 && info.isOpportunistic())
                        .put("active", isActive(subID))
                        .put("selected", subID == selected)
                        .put("default_data", subID == defaultData)
                        .put("default_sms", subID == defaultSMS)
                        .put("default_voice", subID == defaultVoice);
                if (Build.VERSION.SDK_INT >= 29) {
                    item.put("mcc", empty(info.getMccString()));
                    item.put("mnc", empty(info.getMncString()));
                } else {
                    item.put("mcc", Integer.toString(info.getMcc()));
                    item.put("mnc", Integer.toString(info.getMnc()));
                }
                out.put(item);
            } catch (Exception ignored) {
            }
        }
        return out;
    }

    @SuppressLint("MissingPermission")
    private List<SubscriptionInfo> activeSubscriptions() {
        if (subscriptions == null) return new ArrayList<>();
        try {
            List<SubscriptionInfo> list = subscriptions.getActiveSubscriptionInfoList();
            return list == null ? new ArrayList<>() : list;
        } catch (Exception ignored) {
            return new ArrayList<>();
        }
    }

    @SuppressLint("MissingPermission")
    private List<SubscriptionInfo> accessibleSubscriptions() {
        Map<Integer, SubscriptionInfo> merged = new LinkedHashMap<>();
        for (SubscriptionInfo info : activeSubscriptions()) merged.put(info.getSubscriptionId(), info);
        if (subscriptions != null && Build.VERSION.SDK_INT >= 29) {
            try {
                List<SubscriptionInfo> accessible = subscriptions.getAccessibleSubscriptionInfoList();
                if (accessible != null) {
                    for (SubscriptionInfo info : accessible) merged.put(info.getSubscriptionId(), info);
                }
            } catch (Exception ignored) {
            }
        }
        if (subscriptions != null && Build.VERSION.SDK_INT >= 30) {
            try {
                List<SubscriptionInfo> completeActive = subscriptions.getCompleteActiveSubscriptionInfoList();
                if (completeActive != null) {
                    for (SubscriptionInfo info : completeActive) merged.put(info.getSubscriptionId(), info);
                }
            } catch (Exception ignored) {
            }
        }
        if (subscriptions != null && Build.VERSION.SDK_INT >= 35) {
            try {
                List<SubscriptionInfo> all = subscriptions.getAllSubscriptionInfoList();
                if (all != null) {
                    for (SubscriptionInfo info : all) merged.put(info.getSubscriptionId(), info);
                }
            } catch (Exception ignored) {
            }
        }
        return new ArrayList<>(merged.values());
    }

    private SubscriptionInfo findSubscription(int id) {
        for (SubscriptionInfo info : accessibleSubscriptions()) {
            if (info.getSubscriptionId() == id) return info;
        }
        return null;
    }

    private boolean isActive(int id) {
        for (SubscriptionInfo info : activeSubscriptions()) {
            if (info.getSubscriptionId() == id) return true;
        }
        return false;
    }

    private TelephonyManager selectedTelephonyManager() {
        if (telephony == null) return null;
        int id = selectedSubscriptionId();
        return SubscriptionIds.isValid(id) ? telephony.createForSubscriptionId(id) : telephony;
    }

    @SuppressLint("MissingPermission")
    private String phoneNumber(int subID, TelephonyManager tm) {
        if (subscriptions != null && Build.VERSION.SDK_INT >= 33 && SubscriptionIds.isValid(subID)) {
            String number = safe(() -> subscriptions.getPhoneNumber(subID));
            if (!number.isEmpty()) return number;
        }
        return safe(tm::getLine1Number);
    }

    @SuppressLint("MissingPermission")
    private synchronized void registerTelephonyCallbacks() {
        TelephonyManager tm = selectedTelephonyManager();
        if (tm == null) return;
        callbackTelephony = tm;
        if (Build.VERSION.SDK_INT >= 31) {
            ModernTelephonyCallback callback = new ModernTelephonyCallback();
            modernCallback = callback;
            try { tm.registerTelephonyCallback(context.getMainExecutor(), callback); } catch (Exception ignored) {}
        } else {
            legacyCallback = new PhoneStateListener() {
                @Override public void onSignalStrengthsChanged(SignalStrength value) {
                    if (legacyCallback != this) return;
                    signalStrength = value;
                    changed();
                }
                @Override public void onServiceStateChanged(ServiceState value) {
                    if (legacyCallback != this) return;
                    serviceState = value;
                    changed();
                }
            };
            try {
                tm.listen(legacyCallback, PhoneStateListener.LISTEN_SIGNAL_STRENGTHS
                        | PhoneStateListener.LISTEN_SERVICE_STATE
                        | PhoneStateListener.LISTEN_DATA_CONNECTION_STATE);
            } catch (Exception ignored) {}
        }
    }

    @SuppressLint("UseRequiresApi")
    @TargetApi(31)
    private final class ModernTelephonyCallback extends TelephonyCallback implements
            TelephonyCallback.SignalStrengthsListener,
            TelephonyCallback.ServiceStateListener,
            TelephonyCallback.DisplayInfoListener,
            TelephonyCallback.DataConnectionStateListener {
        @Override public void onSignalStrengthsChanged(SignalStrength value) {
            if (modernCallback != this) return;
            signalStrength = value;
            changed();
        }
        @Override public void onServiceStateChanged(ServiceState value) {
            if (modernCallback != this) return;
            serviceState = value;
            changed();
        }
        @Override public void onDisplayInfoChanged(TelephonyDisplayInfo value) {
            if (modernCallback != this) return;
            displayInfo = value;
            changed();
        }
        @Override public void onDataConnectionStateChanged(int state, int networkType) {
            if (modernCallback == this) changed();
        }
    }

    @SuppressLint("MissingPermission")
    synchronized void requestSelectedCellularNetwork() {
        if (connectivity == null) return;
        if (networkCallback != null) {
            try { connectivity.unregisterNetworkCallback(networkCallback); } catch (Exception ignored) {}
        }
        int selected = selectedSubscriptionId();
        networkCallback = new ConnectivityManager.NetworkCallback() {
            @Override public void onAvailable(Network network) {
                if (networkCallback != this) return;
                cellularNetwork = network;
                linkProperties = connectivity.getLinkProperties(network);
                changed();
            }
            @Override public void onLinkPropertiesChanged(Network network, LinkProperties properties) {
                if (networkCallback != this) return;
                if (network.equals(cellularNetwork)) linkProperties = properties;
                changed();
            }
            @Override public void onLost(Network network) {
                if (networkCallback != this) return;
                if (network.equals(cellularNetwork)) {
                    cellularNetwork = null;
                    linkProperties = null;
                }
                changed();
            }
        };
        // A generic cellular request is the most reliable way to bring up the
        // default-data PDN on stock Android. A request carrying an explicit
        // TelephonyNetworkSpecifier can be accepted without throwing while
        // remaining unsatisfied indefinitely when the caller has no carrier
        // privileges (observed on the ZTE A202ZT stock Android 12 build).
        boolean selectedIsDefaultData = selected == SubscriptionManager.getDefaultDataSubscriptionId();
        if (Build.VERSION.SDK_INT >= 30 && SubscriptionIds.isValid(selected) && !selectedIsDefaultData) {
            NetworkRequest selectedRequest = cellularInternetRequest()
                    .setNetworkSpecifier(new TelephonyNetworkSpecifier.Builder()
                            .setSubscriptionId(selected).build())
                    .build();
            try {
                connectivity.requestNetwork(selectedRequest, networkCallback);
                return;
            } catch (Exception error) {
                Log.w(TAG, "specific subscription network request failed for " + selected, error);
                networkCallback = null;
                changed();
                return;
            }
        }
        try {
            // Stock Android builds can reject a subscription-specific request
            // without carrier privileges. For the default-data subscription,
            // the generic cellular request preserves the requested route.
            connectivity.requestNetwork(cellularInternetRequest().build(), networkCallback);
        } catch (Exception error) {
            Log.w(TAG, "default cellular network request failed", error);
            networkCallback = null;
            changed();
        }
    }

    private static NetworkRequest.Builder cellularInternetRequest() {
        return new NetworkRequest.Builder()
                .addTransportType(NetworkCapabilities.TRANSPORT_CELLULAR)
                .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET);
    }

    private void appendSignal(JSONObject snap, SignalStrength strength) throws Exception {
        if (strength == null) return;
        if (Build.VERSION.SDK_INT >= 29) {
            List<CellSignalStrength> cells = strength.getCellSignalStrengths();
            CellSignalStrengthNr nr = null;
            CellSignalStrengthLte lte = null;
            CellSignalStrength fallback = null;
            for (CellSignalStrength cell : cells) {
                if (fallback == null && available(cell.getDbm())) fallback = cell;
                if (cell instanceof CellSignalStrengthNr && nr == null) {
                    CellSignalStrengthNr candidate = (CellSignalStrengthNr) cell;
                    if (available(candidate.getSsRsrp()) || available(candidate.getCsiRsrp())) nr = candidate;
                }
                if (cell instanceof CellSignalStrengthLte && lte == null) {
                    lte = (CellSignalStrengthLte) cell;
                }
            }

            // NSA 5G exposes NR quality and LTE anchor RSSI simultaneously. Keep
            // RSSI sourced from LTE while reporting NR RSRP/RSRQ/SINR below.
            if (lte != null && available(lte.getRssi())) {
                snap.put("signal_dbm", lte.getRssi());
            } else if (Build.VERSION.SDK_INT >= 30 && fallback instanceof CellSignalStrengthGsm
                    && available(((CellSignalStrengthGsm) fallback).getRssi())) {
                snap.put("signal_dbm", ((CellSignalStrengthGsm) fallback).getRssi());
            } else if (fallback != null && available(fallback.getDbm())) {
                snap.put("signal_dbm", fallback.getDbm());
            }

            if (nr != null) {
                int rsrp = available(nr.getSsRsrp()) ? nr.getSsRsrp() : nr.getCsiRsrp();
                int rsrq = available(nr.getSsRsrq()) ? nr.getSsRsrq() : nr.getCsiRsrq();
                int sinr = available(nr.getSsSinr()) ? nr.getSsSinr() : nr.getCsiSinr();
                if (available(rsrp)) {
                    snap.put("signal_rsrp", rsrp);
                    snap.put("nr5g_rsrp", rsrp);
                }
                if (available(rsrq)) {
                    snap.put("signal_rsrq", rsrq);
                    snap.put("nr5g_rsrq", rsrq);
                }
                if (available(sinr)) {
                    snap.put("signal_sinr", sinr);
                    snap.put("nr5g_sinr", sinr);
                }
            } else if (lte != null) {
                if (available(lte.getRssi())) snap.put("signal_dbm", lte.getRssi());
                if (available(lte.getRsrp())) snap.put("signal_rsrp", lte.getRsrp());
                if (available(lte.getRsrq())) snap.put("signal_rsrq", lte.getRsrq());
                if (available(lte.getRssnr())) snap.put("signal_sinr", Math.round(lte.getRssnr() / 10.0f));
            }
        } else {
            int gsm = strength.getGsmSignalStrength();
            if (gsm >= 0 && gsm <= 31) snap.put("signal_dbm", -113 + 2 * gsm);
        }
    }

    @TargetApi(31)
    @SuppressLint("UseRequiresApi")
    private void appendDisplayInfo(JSONObject snap, TelephonyDisplayInfo info) throws Exception {
        if (info == null) return;
        int override = info.getOverrideNetworkType();
        if (override == TelephonyDisplayInfo.OVERRIDE_NETWORK_TYPE_NR_NSA
                || override == TelephonyDisplayInfo.OVERRIDE_NETWORK_TYPE_NR_ADVANCED) {
            snap.put("network_mode", "NR5G_NSA");
            return;
        }
        String mode = networkTypeName(info.getNetworkType());
        if (!"UNKNOWN".equals(mode)) snap.put("network_mode", mode);
    }

    private void appendServiceState(JSONObject snap, ServiceState state) throws Exception {
        if (state == null) return;
        boolean roaming = state.getRoaming();
        snap.put("service_state", state.getState());
        int regStatus;
        String text;
        switch (state.getState()) {
            case ServiceState.STATE_IN_SERVICE:
                regStatus = roaming ? 5 : 1;
                text = roaming ? "roaming" : "registered";
                break;
            case ServiceState.STATE_EMERGENCY_ONLY:
                regStatus = 0;
                text = "emergency_only";
                break;
            case ServiceState.STATE_POWER_OFF:
                regStatus = 0;
                text = "radio_off";
                break;
            default:
                regStatus = 2;
                text = "searching_or_out_of_service";
        }
        boolean psAttached = false;
        if (Build.VERSION.SDK_INT >= 30) {
            NetworkRegistrationInfo info = null;
            JSONArray details = new JSONArray();
            for (NetworkRegistrationInfo candidate : state.getNetworkRegistrationInfoList()) {
                JSONObject detail = new JSONObject()
                        .put("domain", registrationDomain(candidate.getDomain()))
                        .put("transport", candidate.getTransportType() == AccessNetworkConstants.TRANSPORT_TYPE_WWAN ? "WWAN" : "WLAN")
                        .put("registered", candidate.isRegistered())
                        .put("roaming", candidate.isRoaming())
                        .put("searching", candidate.isSearching())
                        .put("registered_plmn", empty(candidate.getRegisteredPlmn()))
                        .put("network_mode", networkTypeName(candidate.getAccessNetworkTechnology()))
                        .put("available_services", new JSONArray(candidate.getAvailableServices()));
                if (Build.VERSION.SDK_INT >= 35) {
                    detail.put("reject_cause", candidate.getRejectCause());
                }
                if (candidate.getCellIdentity() != null) {
                    detail.put("cell_identity", candidate.getCellIdentity().toString());
                }
                details.put(detail);
                if ((candidate.getDomain() & NetworkRegistrationInfo.DOMAIN_PS) != 0
                        && candidate.getTransportType() == AccessNetworkConstants.TRANSPORT_TYPE_WWAN) {
                    info = candidate;
                }
            }
            snap.put("registration_details", details);
            if (info != null) {
                psAttached = info.isRegistered();
                if (info.isRegistered()) {
                    boolean dataRoaming = info.isRoaming();
                    regStatus = dataRoaming ? 5 : 1;
                    text = dataRoaming ? "registered_roaming" : "registered_home";
                } else if (info.isSearching()) {
                    regStatus = 2;
                    text = "searching";
                } else if (state.getState() == ServiceState.STATE_OUT_OF_SERVICE) {
                    regStatus = 0;
                    text = "not_registered";
                }
                String mode = networkTypeName(info.getAccessNetworkTechnology());
                if (!mode.isEmpty() && !"UNKNOWN".equals(mode)) snap.put("network_mode", mode);
            }
            int channel = state.getChannelNumber();
            if (channel > 0) snap.put("radio_channel", channel);
        }
        snap.put("reg_status", regStatus);
        snap.put("reg_status_text", text);
        snap.put("ps_attached", psAttached);
        snap.put("roaming", roaming);
        snap.put("emergency_only", state.getState() == ServiceState.STATE_EMERGENCY_ONLY);
    }

    private static String registrationDomain(int domain) {
        if (domain == NetworkRegistrationInfo.DOMAIN_CS_PS) return "CS_PS";
        if (domain == NetworkRegistrationInfo.DOMAIN_PS) return "PS";
        if (domain == NetworkRegistrationInfo.DOMAIN_CS) return "CS";
        return "UNKNOWN";
    }

    private void appendBattery(JSONObject snap) throws Exception {
        Intent battery = context.registerReceiver(null, new IntentFilter(Intent.ACTION_BATTERY_CHANGED));
        if (battery == null) return;
        int level = battery.getIntExtra(BatteryManager.EXTRA_LEVEL, -1);
        int scale = battery.getIntExtra(BatteryManager.EXTRA_SCALE, 100);
        int status = battery.getIntExtra(BatteryManager.EXTRA_STATUS, BatteryManager.BATTERY_STATUS_UNKNOWN);
        if (level >= 0 && scale > 0) snap.put("battery_pct", Math.round(level * 100f / scale));
        snap.put("battery_charging", status == BatteryManager.BATTERY_STATUS_CHARGING
                || status == BatteryManager.BATTERY_STATUS_FULL);
    }

    private void appendAddresses(JSONObject snap) throws Exception {
        LinkProperties properties = linkProperties;
        if (properties == null) return;
        for (LinkAddress address : properties.getLinkAddresses()) {
            if (address.getAddress().isLoopbackAddress() || address.getAddress().isLinkLocalAddress()) continue;
            if (address.getAddress() instanceof Inet4Address) snap.put("private_ip", address.getAddress().getHostAddress());
            if (address.getAddress() instanceof Inet6Address) snap.put("private_ipv6", address.getAddress().getHostAddress());
        }
    }

    private void appendESIM(JSONObject snap) throws Exception {
        boolean supported = context.getPackageManager().hasSystemFeature("android.hardware.telephony.euicc");
        snap.put("esim_supported", supported);
        if (!supported || Build.VERSION.SDK_INT < 28) return;
        EuiccManager manager = context.getSystemService(EuiccManager.class);
        if (manager == null) return;
        snap.put("esim_enabled", safeBool(manager::isEnabled));
        putNonEmpty(snap, "eid", safe(manager::getEid));
    }

    private void appendAccess(JSONObject snap, TelephonyManager tm) throws Exception {
        JSONObject access = new JSONObject()
                .put("default_sms_app", SmsController.isDefaultSmsApp(context))
                .put("carrier_privileges", tm != null && safeBool(tm::hasCarrierPrivileges))
                .put("read_phone_state", granted(Manifest.permission.READ_PHONE_STATE))
                .put("read_phone_numbers", granted(Manifest.permission.READ_PHONE_NUMBERS))
                .put("read_sms", granted(Manifest.permission.READ_SMS))
                .put("send_sms", granted(Manifest.permission.SEND_SMS))
                .put("receive_sms", granted(Manifest.permission.RECEIVE_SMS))
                .put("location", granted(Manifest.permission.ACCESS_FINE_LOCATION)
                        || granted(Manifest.permission.ACCESS_COARSE_LOCATION))
                .put("read_privileged_phone_state", granted("android.permission.READ_PRIVILEGED_PHONE_STATE"))
                .put("modify_phone_state", granted("android.permission.MODIFY_PHONE_STATE"))
                .put("write_embedded_subscriptions", granted("android.permission.WRITE_EMBEDDED_SUBSCRIPTIONS"))
                .put("write_sms", granted("android.permission.WRITE_SMS"));
        snap.put("access", access);
    }

    private boolean granted(String permission) {
        return context.checkSelfPermission(permission) == PackageManager.PERMISSION_GRANTED;
    }

    private static boolean available(int value) { return value != Integer.MAX_VALUE; }

    private static String networkTypeName(int type) {
        switch (type) {
            case TelephonyManager.NETWORK_TYPE_NR: return "NR5G";
            case TelephonyManager.NETWORK_TYPE_LTE: return "LTE";
            case TelephonyManager.NETWORK_TYPE_HSPAP:
            case TelephonyManager.NETWORK_TYPE_HSPA:
            case TelephonyManager.NETWORK_TYPE_HSDPA:
            case TelephonyManager.NETWORK_TYPE_HSUPA:
            case TelephonyManager.NETWORK_TYPE_UMTS: return "WCDMA";
            case TelephonyManager.NETWORK_TYPE_EDGE:
            case TelephonyManager.NETWORK_TYPE_GPRS:
            case TelephonyManager.NETWORK_TYPE_GSM: return "GSM";
            case TelephonyManager.NETWORK_TYPE_CDMA:
            case TelephonyManager.NETWORK_TYPE_1xRTT:
            case TelephonyManager.NETWORK_TYPE_EVDO_0:
            case TelephonyManager.NETWORK_TYPE_EVDO_A:
            case TelephonyManager.NETWORK_TYPE_EVDO_B: return "CDMA";
            default: return "UNKNOWN";
        }
    }

    private void changed() { if (listener != null) listener.onChanged(); }
    private interface StringCall { String get() throws Exception; }
    private interface IntCall { int get() throws Exception; }
    private interface BoolCall { boolean get() throws Exception; }
    private static String safe(StringCall call) {
        try { return empty(call.get()); } catch (Exception ignored) { return ""; }
    }
    private static int safeInt(IntCall call) {
        try { return call.get(); } catch (Exception ignored) { return 0; }
    }
    private static boolean safeBool(BoolCall call) {
        try { return call.get(); } catch (Exception ignored) { return false; }
    }
    private static String empty(String value) { return value == null ? "" : value; }
    private static String string(CharSequence value) { return value == null ? "" : value.toString(); }
    private static void putNonEmpty(JSONObject object, String key, String value) throws Exception {
        if (value != null && !value.isEmpty()) object.put(key, value);
    }
}
