package com.vohive.agent;

import android.Manifest;
import android.annotation.SuppressLint;
import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.Service;
import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.content.pm.PackageManager;
import android.net.Network;
import android.net.Uri;
import android.os.Build;
import android.os.Handler;
import android.os.IBinder;
import android.os.Looper;
import android.os.PowerManager;
import android.os.SystemClock;
import android.telephony.IccOpenLogicalChannelResponse;
import android.telephony.SubscriptionManager;
import android.telephony.TelephonyManager;
import android.util.Log;

import org.json.JSONArray;
import org.json.JSONObject;

import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.InetSocketAddress;
import java.net.InetAddress;
import java.net.NetworkInterface;
import java.net.Socket;
import java.net.URI;
import java.time.Instant;
import java.util.Base64;
import java.util.Collections;
import java.util.Enumeration;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.RejectedExecutionException;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;

import okhttp3.OkHttpClient;
import okhttp3.Request;
import okhttp3.Response;
import okhttp3.WebSocket;
import okhttp3.WebSocketListener;

@SuppressLint("ApplySharedPref") // Web config commits before reconnect/rebind operations are scheduled.
public class AgentService extends Service {
    public static final String ACTION_START = "com.vohive.agent.START";
    public static final String ACTION_STOP = "com.vohive.agent.STOP";
    public static final String ACTION_STOP_DAEMON = "com.vohive.agent.STOP_DAEMON";
    public static final String ACTION_RELOAD = "com.vohive.agent.RELOAD";
    public static final String ACTION_FLUSH_EVENTS = "com.vohive.agent.FLUSH_EVENTS";
    public static final String ACTION_REFRESH_TELEPHONY = "com.vohive.agent.REFRESH_TELEPHONY";

    private static final int PROTOCOL_VERSION = 1;
    private static final String CHANNEL_ID = "vohive-agent";
    private static final int NOTIFICATION_ID = 1001;
    private static final String TAG = "VoHiveAgent";

    private final Handler mainHandler = new Handler(Looper.getMainLooper());
    private final ExecutorService ioPool = Executors.newCachedThreadPool();
    private final Map<String, StreamBridge> streams = new ConcurrentHashMap<>();
    private final AtomicBoolean statusScheduled = new AtomicBoolean();
    private final Runnable heartbeat = new Runnable() {
        @Override public void run() {
            if (!running) return;
            sendStatus("heartbeat");
            flushEvents();
            mainHandler.postDelayed(this, 15_000);
        }
    };
    private final Runnable reconnect = this::connectWebSocket;

    private OkHttpClient client;
    private volatile WebSocket webSocket;
    private TelephonyRepository telephony;
    private SmsController sms;
    private ESIMController esim;
    private LocalHttpServer httpServer;
    private LanDiscovery lanDiscovery;
    private volatile boolean running;
    private volatile boolean upstreamEnabled;
    private volatile boolean connected;
    private volatile String webServerState = "stopped";
    private long startedAtElapsed;
    private int reconnectAttempt;

    public static void wakeForEvents(Context context) {
        Intent intent = new Intent(context, AgentService.class).setAction(ACTION_FLUSH_EVENTS);
        try {
            context.startForegroundService(intent);
        } catch (Exception ignored) {
        }
    }

    public static void refreshTelephony(Context context) {
        Intent intent = new Intent(context, AgentService.class).setAction(ACTION_REFRESH_TELEPHONY);
        try {
            context.startForegroundService(intent);
        } catch (Exception ignored) {
        }
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        String action = intent == null ? ACTION_START : intent.getAction();
        if (ACTION_STOP_DAEMON.equals(action)) {
            stopDaemon();
            stopSelf();
            return START_NOT_STICKY;
        }
        AgentConfig.ensureInitialized(this);
        createNotificationChannel();
        startForeground(NOTIFICATION_ID, notification("starting"));
        startDaemon();
        if (ACTION_STOP.equals(action)) stopUpstream(true);
        if (ACTION_RELOAD.equals(action)) reloadConfiguration();
        if (ACTION_FLUSH_EVENTS.equals(action)) flushEvents();
        if (ACTION_REFRESH_TELEPHONY.equals(action) && telephony != null) {
            TelephonyRepository repo = telephony;
            repo.refresh();
            mainHandler.postDelayed(() -> {
                if (running && telephony == repo) repo.refresh();
            }, 3_000);
        }
        return START_STICKY;
    }

    @Override public IBinder onBind(Intent intent) { return null; }

    @Override
    public void onDestroy() {
        stopDaemon();
        ioPool.shutdownNow();
        super.onDestroy();
    }

    private synchronized void startDaemon() {
        if (running) {
            ensureHttpServer();
            return;
        }
        running = true;
        startedAtElapsed = SystemClock.elapsedRealtime();
        createNotificationChannel();
        sms = new SmsController(this);
        esim = new ESIMController(this);
        telephony = new TelephonyRepository(this, this::scheduleStatus);
        telephony.start();
        client = new OkHttpClient.Builder()
                .pingInterval(20, TimeUnit.SECONDS)
                .retryOnConnectionFailure(true)
                .build();
        ensureHttpServer();
        lanDiscovery = new LanDiscovery(this, new LanDiscovery.Listener() {
            @Override public void onServerFound(String serverURL) {
                AgentConfig.prefs(AgentService.this).edit()
                        .putString(AgentConfig.KEY_DISCOVERED_SERVER_URL, serverURL).apply();
            }

            @Override public void onPairingApproved(String serverURL, String deviceID,
                                                     String pairCode) {
                mainHandler.post(() -> applyPairing(serverURL, deviceID, pairCode));
            }
        });
        lanDiscovery.start();
        upstreamEnabled = AgentConfig.agentEnabled(this);
        if (upstreamEnabled) {
            setConnectionState("connecting");
            connectWebSocket();
        } else {
            setConnectionState("stopped");
        }
        mainHandler.post(heartbeat);
    }

    private synchronized void stopDaemon() {
        if (!running && client == null) return;
        running = false;
        mainHandler.removeCallbacks(heartbeat);
        mainHandler.removeCallbacks(reconnect);
        shutdownUpstream("stopped");
        LocalHttpServer server = httpServer;
        httpServer = null;
        if (server != null) server.close();
        LanDiscovery discovery = lanDiscovery;
        lanDiscovery = null;
        if (discovery != null) discovery.close();
        webServerState = "stopped";
        if (telephony != null) telephony.stop();
        telephony = null;
        if (client != null) {
            client.dispatcher().executorService().shutdown();
            client.connectionPool().evictAll();
            client = null;
        }
        setConnectionState("stopped");
        stopForeground(STOP_FOREGROUND_REMOVE);
    }

    private synchronized void ensureHttpServer() {
        int port = AgentConfig.httpPort(this);
        if (httpServer != null && httpServer.isRunning() && httpServer.port() == port) return;
        LocalHttpServer old = httpServer;
        httpServer = null;
        if (old != null) old.close();
        try {
            LocalHttpServer server = new LocalHttpServer(this, port);
            server.start();
            httpServer = server;
            webServerState = "listening";
        } catch (Exception error) {
            webServerState = "error: " + error.getMessage();
            Log.e(TAG, "Unable to start HTTP management server", error);
        }
        updateNotification();
    }

    private synchronized void reloadConfiguration() {
        LocalHttpServer old = httpServer;
        httpServer = null;
        if (old != null) old.close();
        ensureHttpServer();
        boolean enabled = AgentConfig.agentEnabled(this);
        if (enabled) reconnectUpstream(false);
        else stopUpstream(false);
    }

    private synchronized void startUpstream(boolean persist) {
        upstreamEnabled = true;
        if (persist) AgentConfig.prefs(this).edit()
                .putBoolean(AgentConfig.KEY_AGENT_ENABLED, true).apply();
        reconnectAttempt = 0;
        connectWebSocket();
    }

    private synchronized void stopUpstream(boolean persist) {
        upstreamEnabled = false;
        if (persist) AgentConfig.prefs(this).edit()
                .putBoolean(AgentConfig.KEY_AGENT_ENABLED, false).apply();
        shutdownUpstream("stopped");
    }

    private synchronized void reconnectUpstream(boolean persist) {
        upstreamEnabled = true;
        if (persist) AgentConfig.prefs(this).edit()
                .putBoolean(AgentConfig.KEY_AGENT_ENABLED, true).apply();
        shutdownUpstream("reconnecting");
        reconnectAttempt = 0;
        mainHandler.post(this::connectWebSocket);
    }

    private void shutdownUpstream(String state) {
        connected = false;
        mainHandler.removeCallbacks(reconnect);
        closeStreams();
        WebSocket ws = webSocket;
        webSocket = null;
        if (ws != null) ws.close(1000, state);
        setConnectionState(state);
    }

    private void connectWebSocket() {
        if (!running || !upstreamEnabled || client == null || connected || webSocket != null) return;
        mainHandler.removeCallbacks(reconnect);
        try {
            String url = websocketURL();
            Request request = new Request.Builder().url(url)
                    .header("Authorization", websocketAuthorization()).build();
            webSocket = client.newWebSocket(request, new AgentListener());
            setConnectionState("connecting");
        } catch (Exception error) {
            String message = error.getMessage() == null ? "invalid configuration" : error.getMessage();
            setConnectionState("waiting: " + message);
            if (!(error instanceof IllegalArgumentException)) scheduleReconnect();
        }
    }

    private String websocketURL() {
        SharedPreferences prefs = AgentConfig.prefs(this);
        String base = prefs.getString(AgentConfig.KEY_SERVER_URL, "");
        if (base == null || base.trim().isEmpty()) throw new IllegalArgumentException("Server URL is empty");
        base = base.trim();
        if (!base.contains("://")) base = "http://" + base;
        URI uri = URI.create(base);
        if (uri.getRawAuthority() == null || uri.getRawAuthority().isEmpty()) {
            throw new IllegalArgumentException("Server URL has no host");
        }
        String scheme = "https".equalsIgnoreCase(uri.getScheme()) ? "wss" : "ws";
        String path = uri.getRawPath();
        if (path == null || "/".equals(path)) path = "";
        while (path.endsWith("/") && !path.isEmpty()) path = path.substring(0, path.length() - 1);
        String agentID = prefs.getString(AgentConfig.KEY_AGENT_ID, "");
        return scheme + "://" + uri.getRawAuthority() + path + "/api/android-agent/connect?agent_id="
                + Uri.encode(agentID);
    }

    private String websocketAuthorization() {
        SharedPreferences prefs = AgentConfig.prefs(this);
        String agentToken = prefs.getString(AgentConfig.KEY_AGENT_TOKEN, "");
        String pairToken = prefs.getString(AgentConfig.KEY_PAIR_TOKEN, "");
        if (agentToken != null && !agentToken.isEmpty()) return "Bearer " + agentToken;
        if (pairToken == null || pairToken.isEmpty()) throw new IllegalArgumentException("Pair token is empty");
        return "VoHivePair " + pairToken;
    }

    private void scheduleReconnect() {
        if (!running || !upstreamEnabled) return;
        connected = false;
        int exponent = Math.min(reconnectAttempt++, 6);
        long delay = Math.min(60_000L, 1_000L << exponent);
        mainHandler.removeCallbacks(reconnect);
        mainHandler.postDelayed(reconnect, delay);
        setConnectionState("reconnecting in " + (delay / 1000) + "s");
    }

    private void sendHello() {
        SharedPreferences prefs = AgentConfig.prefs(this);
        try {
            JSONObject message = envelope("hello")
                    .put("agent_id", prefs.getString(AgentConfig.KEY_AGENT_ID, ""))
                    .put("device_id", prefs.getString(AgentConfig.KEY_DEVICE_ID, ""))
                    .put("model", Build.MANUFACTURER + " " + Build.MODEL)
                    .put("app_version", appVersion())
                    .put("capabilities", new JSONArray()
                            .put("status").put("sms").put("subscriptions")
                            .put("esim").put("remote_dialer"));
            send(message);
        } catch (Exception ignored) {
        }
    }

    private void scheduleStatus() {
        if (!running || !statusScheduled.compareAndSet(false, true)) return;
        mainHandler.postDelayed(() -> {
            statusScheduled.set(false);
            sendStatus("status_snapshot");
        }, 500);
    }

    private void sendStatus(String type) {
        TelephonyRepository repo = telephony;
        if (repo == null || !connected) return;
        ioPool.submit(() -> {
            try { send(envelope(type).put("snapshot", repo.snapshot())); }
            catch (Exception ignored) {}
        });
    }

    private void flushEvents() {
        if (!connected) return;
        List<JSONObject> events = EventStore.pending(this);
        if (events.isEmpty()) return;
        for (JSONObject event : events) {
            if (!send(event)) break;
        }
    }

    private void handleMessage(String text) {
        try {
            JSONObject message = new JSONObject(text);
            String type = message.optString("type");
            if ("pairing_complete".equals(type)) {
                String token = message.optString("token");
                if (!token.isEmpty()) {
                    SharedPreferences.Editor edit = AgentConfig.prefs(this).edit()
                            .putString(AgentConfig.KEY_AGENT_TOKEN, token)
                            .remove(AgentConfig.KEY_PAIR_TOKEN);
                    String pairedDeviceID = message.optString("device_id").trim();
                    if (!pairedDeviceID.isEmpty()) {
                        edit.putString(AgentConfig.KEY_DEVICE_ID, pairedDeviceID);
                    }
                    edit.apply();
                }
            } else if ("event_ack".equals(type)) {
                EventStore.acknowledge(this, message.optString("event_id"));
            } else if ("stream_open".equals(type)) {
                openStream(message);
            } else if ("stream_data".equals(type)) {
                StreamBridge bridge = streams.get(message.optString("stream_id"));
                if (bridge != null) bridge.writeAsync(Base64.getDecoder().decode(message.optString("data")));
            } else if ("stream_close".equals(type) || "stream_error".equals(type)) {
                StreamBridge bridge = streams.remove(message.optString("stream_id"));
                if (bridge != null) bridge.closeAfterWrites();
            } else if ("rpc_request".equals(type)) {
                ioPool.submit(() -> handleRpc(message));
            }
        } catch (Exception ignored) {
        }
    }

    private void handleRpc(JSONObject message) {
        String requestID = message.optString("request_id");
        String method = message.optString("method");
        JSONObject params = message.optJSONObject("params");
        if (params == null) params = new JSONObject();
        try {
            JSONObject result;
            switch (method) {
                case "sms.send":
                    int smsSub = params.optInt("subscription_id", telephony.selectedSubscriptionId());
                    result = sms.send(smsSub, params.optString("to"), params.optString("body"));
                    break;
                case "sms.list": result = sms.list(params.optInt("limit", 500)); break;
                case "sms.read": result = sms.read(params.getLong("index")); break;
                case "sms.delete": result = sms.delete(params.getLong("index")); break;
                case "sms.delete_all": result = sms.deleteAll(); break;
                case "subscriptions.list": result = new JSONObject().put("subscriptions", telephony.subscriptionsJSON()); break;
                case "subscriptions.select":
                    int selected = params.getInt("subscription_id");
                    telephony.selectSubscription(selected);
                    result = new JSONObject().put("subscription_id", selected);
                    break;
                case "esim.switch":
                    result = esim.switchProfile(params.getInt("subscription_id"), params.optInt("port_index", 0));
                    break;
                case "network.connect":
                    telephony.requestSelectedCellularNetwork();
                    result = new JSONObject().put("state", "requested");
                    break;
                case "network.disconnect":
                    telephony.releaseSelectedCellularNetwork();
                    result = new JSONObject().put("state", "released");
                    break;
                case "network.rotate":
                    telephony.releaseSelectedCellularNetwork();
                    telephony.requestSelectedCellularNetwork();
                    result = new JSONObject().put("state", "requested");
                    break;
                case "radio.set_mode":
                    setRadioMode(params.getInt("mode"));
                    result = new JSONObject();
                    break;
                case "device.reboot":
                    PowerManager power = getSystemService(PowerManager.class);
                    if (power == null) throw new IllegalStateException("PowerManager is unavailable");
                    power.reboot(null);
                    result = new JSONObject();
                    break;
                case "sim.open_channel": result = openLogicalChannel(params.getString("aid")); break;
                case "sim.close_channel": result = closeLogicalChannel(params.getInt("channel_id")); break;
                case "sim.transmit_apdu": result = transmitAPDU(params.getInt("channel_id"), params.getString("command")); break;
                default: throw new IllegalArgumentException("unsupported rpc: " + method);
            }
            send(envelope("rpc_response").put("request_id", requestID).put("result", result));
        } catch (Exception error) {
            String detail = error.getClass().getSimpleName() + ": " + (error.getMessage() == null ? "operation failed" : error.getMessage());
            try { send(envelope("rpc_response").put("request_id", requestID).put("error", detail)); }
            catch (Exception ignored) {}
        }
    }

    private void setRadioMode(int mode) throws Exception {
        TelephonyManager tm = telephony.telephonyForSelected();
        if (tm == null) throw new IllegalStateException("TelephonyManager is unavailable");
        boolean enabled = mode == 1;
        java.lang.reflect.Method method = TelephonyManager.class.getMethod("setRadioPower", boolean.class);
        Object result = method.invoke(tm, enabled);
        if (result instanceof Boolean && !((Boolean) result)) throw new IllegalStateException("radio mode rejected");
    }

    private JSONObject openLogicalChannel(String aid) throws Exception {
        TelephonyManager tm = telephony.telephonyForSelected();
        if (tm == null) throw new IllegalStateException("TelephonyManager is unavailable");
        IccOpenLogicalChannelResponse response = tm.iccOpenLogicalChannel(aid, 0);
        if (response == null || response.getStatus() != IccOpenLogicalChannelResponse.STATUS_NO_ERROR) {
            throw new IllegalStateException("open logical channel failed, status=" + (response == null ? -1 : response.getStatus()));
        }
        return new JSONObject().put("channel_id", response.getChannel())
                .put("select_response", response.getSelectResponse() == null ? "" : bytesToHex(response.getSelectResponse()));
    }

    private JSONObject closeLogicalChannel(int channel) throws Exception {
        TelephonyManager tm = telephony.telephonyForSelected();
        if (tm == null || !tm.iccCloseLogicalChannel(channel)) throw new IllegalStateException("close logical channel failed");
        return new JSONObject();
    }

    private JSONObject transmitAPDU(int channel, String command) throws Exception {
        byte[] apdu = hexToBytes(command);
        if (apdu.length < 4) throw new IllegalArgumentException("APDU requires at least 4 bytes");
        int p3 = apdu.length >= 5 ? apdu[4] & 0xff : -1;
        String data = apdu.length > 5 ? bytesToHex(apdu, 5, apdu.length - 5) : "";
        TelephonyManager tm = telephony.telephonyForSelected();
        if (tm == null) throw new IllegalStateException("TelephonyManager is unavailable");
        String response = tm.iccTransmitApduLogicalChannel(channel,
                apdu[0] & 0xff, apdu[1] & 0xff, apdu[2] & 0xff, apdu[3] & 0xff, p3, data);
        return new JSONObject().put("response", response == null ? "" : response);
    }

    private void openStream(JSONObject message) {
        String streamID = message.optString("stream_id");
        String address = message.optString("address");
        ioPool.submit(() -> {
            try {
                Socket socket = openCellularSocket(address);
                StreamBridge bridge = new StreamBridge(streamID, socket);
                streams.put(streamID, bridge);
                send(envelope("stream_opened").put("stream_id", streamID));
                bridge.start();
            } catch (Exception error) {
                try { send(envelope("stream_error").put("stream_id", streamID).put("error", error.getMessage())); }
                catch (Exception ignored) {}
            }
        });
    }

    private Socket openCellularSocket(String address) throws IOException {
        HostPort target = HostPort.parse(address);
        Network network = telephony == null ? null : telephony.selectedNetwork();
        if (network == null) throw new IOException("selected cellular network is unavailable");
        IOException lastError = null;
        InetAddress[] addresses = network.getAllByName(target.host);
        for (InetAddress resolved : addresses) {
            Socket socket = network.getSocketFactory().createSocket();
            try {
                socket.connect(new InetSocketAddress(resolved, target.port), 30_000);
                return socket;
            } catch (IOException error) {
                lastError = error;
                try { socket.close(); } catch (IOException ignored) {}
            }
        }
        throw lastError == null ? new IOException("cellular DNS returned no addresses") : lastError;
    }

    private JSONObject envelope(String type) throws Exception {
        return new JSONObject().put("type", type).put("protocol_version", PROTOCOL_VERSION);
    }

    private String appVersion() {
        try {
            String version = getPackageManager().getPackageInfo(getPackageName(), 0).versionName;
            return version == null ? "unknown" : version;
        } catch (Exception ignored) {
            return "unknown";
        }
    }

    private boolean send(JSONObject message) {
        WebSocket ws = webSocket;
        return ws != null && connected && ws.send(message.toString());
    }

    private void setConnectionState(String state) {
        AgentConfig.prefs(this).edit()
                .putString(AgentConfig.KEY_CONNECTION_STATE, state).apply();
        updateNotification();
    }

    private Notification notification(String text) {
        createNotificationChannel();
        Notification.Builder builder = new Notification.Builder(this, CHANNEL_ID);
        return builder.setContentTitle("VoHive Android Agent")
                .setContentText("Web 0.0.0.0:" + AgentConfig.httpPort(this) + " · " + text)
                .setSmallIcon(android.R.drawable.stat_sys_upload)
                .setCategory(Notification.CATEGORY_SERVICE)
                .setOngoing(true).build();
    }

    private void updateNotification() {
        if (!running) return;
        NotificationManager nm = getSystemService(NotificationManager.class);
        if (nm == null) return;
        String connection = AgentConfig.prefs(this).getString(
                AgentConfig.KEY_CONNECTION_STATE, "stopped");
        String detail = "listening".equals(webServerState)
                ? connection : webServerState + " · " + connection;
        nm.notify(NOTIFICATION_ID, notification(detail));
    }

    private void createNotificationChannel() {
        NotificationManager nm = getSystemService(NotificationManager.class);
        if (nm != null) nm.createNotificationChannel(new NotificationChannel(
                CHANNEL_ID, "VoHive Agent", NotificationManager.IMPORTANCE_LOW));
    }

    JSONObject webStatus() throws Exception {
        SharedPreferences prefs = AgentConfig.prefs(this);
        JSONObject status = new JSONObject()
                .put("service", new JSONObject()
                        .put("running", running)
                        .put("uptime_ms", running
                                ? Math.max(0, SystemClock.elapsedRealtime() - startedAtElapsed) : 0)
                        .put("app_version", appVersion())
                        .put("model", Build.MANUFACTURER + " " + Build.MODEL)
                        .put("android_version", Build.VERSION.RELEASE)
                        .put("sdk", Build.VERSION.SDK_INT))
                .put("web", new JSONObject()
                        .put("bind", "0.0.0.0")
                        .put("port", AgentConfig.httpPort(this))
                        .put("state", webServerState)
                        .put("authentication", "session")
                        .put("urls", managementUrls()))
                .put("upstream", new JSONObject()
                        .put("enabled", upstreamEnabled)
                        .put("connected", connected)
                        .put("state", prefs.getString(
                                AgentConfig.KEY_CONNECTION_STATE, "stopped"))
                        .put("configured", upstreamConfigured(prefs)))
                .put("permissions", permissionSnapshot())
                .put("default_sms_app", SmsController.isDefaultSmsApp(this))
                .put("timestamp", Instant.now().toString());
        TelephonyRepository repo = telephony;
        if (repo != null) {
            try {
                status.put("telephony", repo.snapshot());
            } catch (Exception error) {
                status.put("telephony_error", detail(error));
            }
        }
        return status;
    }

    JSONObject webConfig() throws Exception {
        SharedPreferences prefs = AgentConfig.prefs(this);
        TelephonyRepository repo = telephony;
        return new JSONObject()
                .put("server_url", prefs.getString(AgentConfig.KEY_SERVER_URL, ""))
                .put("device_id", prefs.getString(AgentConfig.KEY_DEVICE_ID, ""))
                .put("agent_id", prefs.getString(AgentConfig.KEY_AGENT_ID, ""))
                .put("pair_token_configured", !empty(
                        prefs.getString(AgentConfig.KEY_PAIR_TOKEN, "")))
                .put("paired", !empty(prefs.getString(AgentConfig.KEY_AGENT_TOKEN, "")))
                .put("discovered_server_url", prefs.getString(
                        AgentConfig.KEY_DISCOVERED_SERVER_URL, ""))
                .put("auto_start", prefs.getBoolean(AgentConfig.KEY_AUTO_START, true))
                .put("agent_enabled", AgentConfig.agentEnabled(this))
                .put("http_bind", "0.0.0.0")
                .put("http_port", AgentConfig.httpPort(this))
                .put("web_username", prefs.getString(
                        AgentConfig.KEY_WEB_USERNAME, AgentConfig.DEFAULT_WEB_USERNAME))
                .put("selected_subscription_id", repo == null
                        ? prefs.getInt(TelephonyRepository.KEY_SELECTED_SUBSCRIPTION,
                        SubscriptionManager.INVALID_SUBSCRIPTION_ID)
                        : repo.selectedSubscriptionId());
    }

    synchronized JSONObject updateWebConfig(JSONObject body) throws Exception {
        SharedPreferences prefs = AgentConfig.prefs(this);
        int oldPort = AgentConfig.httpPort(this);
        SharedPreferences.Editor edit = prefs.edit();
        boolean reconnectNeeded = false;
        if (body.has("server_url")) {
            String serverURL = bounded(body.optString("server_url", "").trim(), 2048,
                    "server_url");
            validateServerURL(serverURL);
            edit.putString(AgentConfig.KEY_SERVER_URL, serverURL);
            reconnectNeeded = true;
        }
        if (body.has("device_id")) {
            edit.putString(AgentConfig.KEY_DEVICE_ID,
                    bounded(body.optString("device_id", "").trim(), 128, "device_id"));
            reconnectNeeded = true;
        }
        if (body.has("agent_id")) {
            String agentID = bounded(body.optString("agent_id", "").trim(), 128, "agent_id");
            if (agentID.isEmpty()) throw new IllegalArgumentException("agent_id is required");
            edit.putString(AgentConfig.KEY_AGENT_ID, agentID);
            reconnectNeeded = true;
        }
        if (body.has("pair_token")) {
            String token = bounded(body.optString("pair_token", "").trim(), 4096,
                    "pair_token");
            edit.putString(AgentConfig.KEY_PAIR_TOKEN, token);
            if (!token.isEmpty()) edit.remove(AgentConfig.KEY_AGENT_TOKEN);
            reconnectNeeded = true;
        }
        if (body.has("auto_start")) {
            edit.putBoolean(AgentConfig.KEY_AUTO_START, body.optBoolean("auto_start", true));
        }
        if (body.has("agent_enabled")) {
            edit.putBoolean(AgentConfig.KEY_AGENT_ENABLED,
                    body.optBoolean("agent_enabled", true));
        }
        int newPort = oldPort;
        if (body.has("http_port")) {
            newPort = body.optInt("http_port", -1);
            if (newPort < 1024 || newPort > 65535) {
                throw new IllegalArgumentException("http_port must be between 1024 and 65535");
            }
            edit.putInt(AgentConfig.KEY_HTTP_PORT, newPort);
        }
        edit.commit();

        boolean enabled = AgentConfig.agentEnabled(this);
        if (body.has("agent_enabled") || reconnectNeeded) {
            if (enabled) mainHandler.post(() -> reconnectUpstream(false));
            else mainHandler.post(() -> stopUpstream(false));
        }
        boolean webRestart = newPort != oldPort;
        if (webRestart) mainHandler.postDelayed(this::ensureHttpServer, 500);
        return webConfig().put("web_restart_scheduled", webRestart);
    }

    private synchronized void applyPairing(String serverURL, String deviceID, String pairCode) {
        AgentConfig.prefs(this).edit()
                .putString(AgentConfig.KEY_SERVER_URL, serverURL)
                .putString(AgentConfig.KEY_DEVICE_ID, deviceID)
                .putString(AgentConfig.KEY_PAIR_TOKEN, pairCode)
                .remove(AgentConfig.KEY_AGENT_TOKEN)
                .putBoolean(AgentConfig.KEY_AGENT_ENABLED, true)
                .apply();
        reconnectUpstream(false);
    }

    String appVersionForDiscovery() {
        return appVersion();
    }

    JSONObject startUpstreamFromWeb() throws Exception {
        mainHandler.post(() -> startUpstream(true));
        return operation("starting");
    }

    JSONObject stopUpstreamFromWeb() throws Exception {
        mainHandler.post(() -> stopUpstream(true));
        return operation("stopping");
    }

    JSONObject reconnectUpstreamFromWeb() throws Exception {
        mainHandler.post(() -> reconnectUpstream(true));
        return operation("reconnecting");
    }

    synchronized JSONObject resetPairingFromWeb() throws Exception {
        AgentConfig.prefs(this).edit()
                .remove(AgentConfig.KEY_AGENT_TOKEN)
                .remove(AgentConfig.KEY_PAIR_TOKEN)
                .remove(AgentConfig.KEY_DEVICE_ID)
                .putBoolean(AgentConfig.KEY_AGENT_ENABLED, false)
                .apply();
        stopUpstream(false);
        return new JSONObject().put("ok", true).put("state", "discoverable");
    }

    JSONObject refreshTelephonyFromWeb() throws Exception {
        TelephonyRepository repo = requireTelephony();
        repo.refresh();
        mainHandler.postDelayed(() -> {
            if (running && telephony == repo) repo.refresh();
        }, 3_000);
        return operation("refreshing");
    }

    JSONObject webSubscriptions() throws Exception {
        TelephonyRepository repo = requireTelephony();
        return new JSONObject()
                .put("selected_subscription_id", repo.selectedSubscriptionId())
                .put("subscriptions", repo.subscriptionsJSON());
    }

    JSONObject selectSubscriptionFromWeb(JSONObject body) throws Exception {
        int subscriptionID = body.getInt("subscription_id");
        TelephonyRepository repo = requireTelephony();
        repo.selectSubscription(subscriptionID);
        return new JSONObject().put("subscription_id", subscriptionID).put("selected", true);
    }

    JSONObject switchEsimFromWeb(JSONObject body) throws Exception {
        ESIMController controller = esim;
        if (controller == null) throw new IllegalStateException("eSIM controller is unavailable");
        return controller.switchProfile(body.getInt("subscription_id"),
                body.optInt("port_index", 0));
    }

    JSONObject webDiagnostics() throws Exception {
        TelephonyRepository repo = requireTelephony();
        JSONObject snapshot = repo.snapshot();
        AgentConfig.prefs(this).edit()
                .putString(AgentConfig.KEY_DIAGNOSTICS_JSON, snapshot.toString()).apply();
        return snapshot;
    }

    JSONObject webSmsList(int limit) throws Exception {
        return requireSms().list(limit);
    }

    JSONObject readSmsFromWeb(long id) throws Exception {
        return requireSms().read(id);
    }

    JSONObject sendSmsFromWeb(JSONObject body) throws Exception {
        TelephonyRepository repo = requireTelephony();
        int subscriptionID = body.optInt("subscription_id", repo.selectedSubscriptionId());
        return requireSms().send(subscriptionID, body.optString("to", ""),
                body.optString("body", ""));
    }

    JSONObject deleteSmsFromWeb(long id) throws Exception {
        return requireSms().delete(id);
    }

    JSONObject deleteAllSmsFromWeb() throws Exception {
        return requireSms().deleteAll();
    }

    private TelephonyRepository requireTelephony() {
        TelephonyRepository repo = telephony;
        if (repo == null) throw new IllegalStateException("telephony repository is unavailable");
        return repo;
    }

    private SmsController requireSms() {
        SmsController controller = sms;
        if (controller == null) throw new IllegalStateException("SMS controller is unavailable");
        return controller;
    }

    private JSONObject permissionSnapshot() throws Exception {
        JSONObject permissions = new JSONObject();
        putPermission(permissions, "read_phone_state", Manifest.permission.READ_PHONE_STATE);
        putPermission(permissions, "read_phone_numbers", Manifest.permission.READ_PHONE_NUMBERS);
        putPermission(permissions, "coarse_location", Manifest.permission.ACCESS_COARSE_LOCATION);
        putPermission(permissions, "fine_location", Manifest.permission.ACCESS_FINE_LOCATION);
        putPermission(permissions, "send_sms", Manifest.permission.SEND_SMS);
        putPermission(permissions, "receive_sms", Manifest.permission.RECEIVE_SMS);
        putPermission(permissions, "read_sms", Manifest.permission.READ_SMS);
        if (Build.VERSION.SDK_INT >= 33) {
            putPermission(permissions, "notifications", Manifest.permission.POST_NOTIFICATIONS);
        }
        putPermission(permissions, "privileged_phone_state",
                "android.permission.READ_PRIVILEGED_PHONE_STATE");
        putPermission(permissions, "modify_phone_state", "android.permission.MODIFY_PHONE_STATE");
        putPermission(permissions, "write_embedded_subscriptions",
                "android.permission.WRITE_EMBEDDED_SUBSCRIPTIONS");
        putPermission(permissions, "write_sms", "android.permission.WRITE_SMS");
        putPermission(permissions, "reboot", "android.permission.REBOOT");
        return permissions;
    }

    private void putPermission(JSONObject target, String name, String permission) throws Exception {
        target.put(name, checkSelfPermission(permission) == PackageManager.PERMISSION_GRANTED);
    }

    private JSONArray managementUrls() {
        JSONArray urls = new JSONArray();
        int port = AgentConfig.httpPort(this);
        try {
            Enumeration<NetworkInterface> interfaces = NetworkInterface.getNetworkInterfaces();
            if (interfaces == null) return urls;
            for (NetworkInterface network : Collections.list(interfaces)) {
                if (!network.isUp() || network.isLoopback()) continue;
                for (InetAddress address : Collections.list(network.getInetAddresses())) {
                    if (address.isLoopbackAddress() || address.isLinkLocalAddress()) continue;
                    String host = address.getHostAddress();
                    if (host == null || host.isEmpty()) continue;
                    int scope = host.indexOf('%');
                    if (scope >= 0) host = host.substring(0, scope);
                    if (host.contains(":")) host = "[" + host + "]";
                    urls.put("http://" + host + ":" + port + "/");
                }
            }
        } catch (Exception ignored) {
        }
        return urls;
    }

    private static boolean upstreamConfigured(SharedPreferences prefs) {
        return !empty(prefs.getString(AgentConfig.KEY_SERVER_URL, ""))
                && (!empty(prefs.getString(AgentConfig.KEY_AGENT_TOKEN, ""))
                || !empty(prefs.getString(AgentConfig.KEY_PAIR_TOKEN, "")));
    }

    private static void validateServerURL(String value) {
        if (value.isEmpty()) return;
        String candidate = value.contains("://") ? value : "http://" + value;
        URI uri = URI.create(candidate);
        String scheme = uri.getScheme();
        if (uri.getRawAuthority() == null || uri.getRawAuthority().isEmpty()
                || (!("http".equalsIgnoreCase(scheme)) && !("https".equalsIgnoreCase(scheme)))) {
            throw new IllegalArgumentException("server_url must be an HTTP or HTTPS URL with a host");
        }
    }

    private static String bounded(String value, int max, String field) {
        if (value.length() > max) throw new IllegalArgumentException(field + " is too long");
        return value;
    }

    private static boolean empty(String value) {
        return value == null || value.trim().isEmpty();
    }

    private static String detail(Exception error) {
        return error.getClass().getSimpleName() + ": "
                + (error.getMessage() == null ? "operation failed" : error.getMessage());
    }

    private static JSONObject operation(String state) throws Exception {
        return new JSONObject().put("state", state).put("accepted", true);
    }

    private class AgentListener extends WebSocketListener {
        @Override public void onOpen(WebSocket socket, Response response) {
            webSocket = socket;
            connected = true;
            reconnectAttempt = 0;
            setConnectionState("connected");
            sendHello();
            sendStatus("status_snapshot");
            flushEvents();
        }
        @Override public void onMessage(WebSocket socket, String text) { handleMessage(text); }
        @Override public void onClosed(WebSocket socket, int code, String reason) {
            if (webSocket != socket) return;
            webSocket = null;
            connected = false;
            closeStreams();
            scheduleReconnect();
        }
        @Override public void onFailure(WebSocket socket, Throwable error, Response response) {
            if (webSocket != socket) return;
            webSocket = null;
            connected = false;
            closeStreams();
            setConnectionState("connection error: " + error.getMessage());
            scheduleReconnect();
        }
    }

    private void closeStreams() {
        for (StreamBridge bridge : streams.values()) bridge.close();
        streams.clear();
    }

    private final class StreamBridge {
        private final String streamID;
        private final Socket socket;
        private final ExecutorService writer = Executors.newSingleThreadExecutor();
        private final AtomicBoolean closed = new AtomicBoolean();
        private final AtomicBoolean closeScheduled = new AtomicBoolean();
        StreamBridge(String streamID, Socket socket) { this.streamID = streamID; this.socket = socket; }
        void start() {
            ioPool.submit(() -> {
                try (InputStream input = socket.getInputStream()) {
                    byte[] buffer = new byte[16 * 1024];
                    int count;
                    while ((count = input.read(buffer)) >= 0) {
                        byte[] chunk = new byte[count];
                        System.arraycopy(buffer, 0, chunk, 0, count);
                        if (!send(envelope("stream_data").put("stream_id", streamID)
                                .put("data", Base64.getEncoder().encodeToString(chunk)))) break;
                    }
                    if (!closed.get()) send(envelope("stream_close").put("stream_id", streamID));
                } catch (Exception error) {
                    if (!closed.get()) {
                        try { send(envelope("stream_error").put("stream_id", streamID).put("error", error.getMessage())); }
                        catch (Exception ignored) {}
                    }
                } finally { close(); }
            });
        }
        void writeAsync(byte[] data) {
            if (closed.get() || closeScheduled.get()) return;
            try {
                writer.execute(() -> {
                    if (closed.get()) return;
                    try {
                        OutputStream output = socket.getOutputStream();
                        output.write(data);
                        output.flush();
                    } catch (IOException error) {
                        if (!closed.get()) {
                            try { send(envelope("stream_error").put("stream_id", streamID).put("error", error.getMessage())); }
                            catch (Exception ignored) {}
                        }
                        close();
                    }
                });
            } catch (RejectedExecutionException ignored) {
            }
        }
        void closeAfterWrites() {
            if (closed.get() || !closeScheduled.compareAndSet(false, true)) return;
            try {
                writer.execute(this::close);
            } catch (RejectedExecutionException ignored) {
                close();
            }
        }
        void close() {
            if (!closed.compareAndSet(false, true)) return;
            streams.remove(streamID, this);
            writer.shutdownNow();
            try { socket.close(); } catch (IOException ignored) {}
        }
    }

    private static final class HostPort {
        final String host; final int port;
        HostPort(String host, int port) { this.host = host; this.port = port; }
        static HostPort parse(String value) {
            if (value == null) throw new IllegalArgumentException("address is empty");
            String host;
            String portText;
            if (value.startsWith("[")) {
                int end = value.indexOf(']');
                if (end < 0 || end + 2 > value.length() || value.charAt(end + 1) != ':') {
                    throw new IllegalArgumentException("invalid IPv6 address: " + value);
                }
                host = value.substring(1, end);
                portText = value.substring(end + 2);
            } else {
                int colon = value.lastIndexOf(':');
                if (colon <= 0) throw new IllegalArgumentException("address must include port");
                host = value.substring(0, colon);
                portText = value.substring(colon + 1);
            }
            int port = Integer.parseInt(portText);
            if (port <= 0 || port > 65535) throw new IllegalArgumentException("invalid port");
            return new HostPort(host, port);
        }
    }

    private static byte[] hexToBytes(String value) {
        String hex = value == null ? "" : value.replaceAll("\\s+", "");
        if ((hex.length() & 1) != 0) throw new IllegalArgumentException("hex length must be even");
        byte[] out = new byte[hex.length() / 2];
        for (int i = 0; i < out.length; i++) out[i] = (byte) Integer.parseInt(hex.substring(i * 2, i * 2 + 2), 16);
        return out;
    }
    private static String bytesToHex(byte[] data) { return bytesToHex(data, 0, data.length); }
    private static String bytesToHex(byte[] data, int offset, int length) {
        StringBuilder out = new StringBuilder(length * 2);
        for (int i = offset; i < offset + length; i++) out.append(String.format("%02X", data[i] & 0xff));
        return out.toString();
    }
}
