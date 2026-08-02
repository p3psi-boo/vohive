package com.vohive.agent;

import android.content.SharedPreferences;
import android.os.Build;
import android.util.Log;

import org.json.JSONObject;

import java.net.DatagramPacket;
import java.net.DatagramSocket;
import java.net.Inet4Address;
import java.net.InetAddress;
import java.net.InterfaceAddress;
import java.net.NetworkInterface;
import java.nio.charset.StandardCharsets;
import java.security.SecureRandom;
import java.util.Collections;
import java.util.HashSet;
import java.util.Set;
import java.util.concurrent.atomic.AtomicBoolean;

final class LanDiscovery implements AutoCloseable {
    interface Listener {
        void onServerFound(String serverURL);
        void onPairingApproved(String serverURL, String deviceID, String pairCode);
    }

    private static final String TAG = "VoHiveDiscovery";
    private static final int PORT = 8764;
    private static final int PROTOCOL_VERSION = 1;

    private final AgentService service;
    private final Listener listener;
    private final AtomicBoolean running = new AtomicBoolean();
    private final String nonce = randomNonce();
    private volatile DatagramSocket socket;
    private volatile Thread worker;

    LanDiscovery(AgentService service, Listener listener) {
        this.service = service;
        this.listener = listener;
    }

    synchronized void start() {
        if (!running.compareAndSet(false, true)) return;
        worker = new Thread(this::run, "vohive-lan-discovery");
        worker.setDaemon(true);
        worker.start();
    }

    private void run() {
        try (DatagramSocket datagram = new DatagramSocket()) {
            socket = datagram;
            datagram.setBroadcast(true);
            datagram.setSoTimeout(1_000);
            long nextBroadcast = 0;
            byte[] buffer = new byte[4096];
            while (running.get()) {
                long now = System.currentTimeMillis();
                if (!paired() && now >= nextBroadcast) {
                    broadcast(datagram);
                    nextBroadcast = now + 5_000;
                }
                try {
                    DatagramPacket packet = new DatagramPacket(buffer, buffer.length);
                    datagram.receive(packet);
                    handle(packet);
                } catch (java.net.SocketTimeoutException ignored) {
                }
            }
        } catch (Exception error) {
            if (running.get()) Log.w(TAG, "LAN discovery stopped", error);
        } finally {
            socket = null;
            running.set(false);
        }
    }

    private boolean paired() {
        return !AgentConfig.prefs(service).getString(AgentConfig.KEY_AGENT_TOKEN, "").isEmpty();
    }

    private void broadcast(DatagramSocket datagram) throws Exception {
        SharedPreferences prefs = AgentConfig.prefs(service);
        JSONObject message = new JSONObject()
                .put("type", "vohive_agent_discover")
                .put("version", PROTOCOL_VERSION)
                .put("agent_id", prefs.getString(AgentConfig.KEY_AGENT_ID, ""))
                .put("model", Build.MANUFACTURER + " " + Build.MODEL)
                .put("app_version", service.appVersionForDiscovery())
                .put("http_port", AgentConfig.httpPort(service))
                .put("nonce", nonce);
        byte[] payload = message.toString().getBytes(StandardCharsets.UTF_8);
        Set<InetAddress> destinations = broadcastAddresses();
        destinations.add(InetAddress.getByName("255.255.255.255"));
        for (InetAddress destination : destinations) {
            try {
                datagram.send(new DatagramPacket(payload, payload.length, destination, PORT));
            } catch (Exception ignored) {
            }
        }
    }

    private void handle(DatagramPacket packet) {
        try {
            JSONObject message = new JSONObject(new String(packet.getData(), packet.getOffset(),
                    packet.getLength(), StandardCharsets.UTF_8));
            if (message.optInt("version") != PROTOCOL_VERSION
                    || !nonce.equals(message.optString("nonce"))) return;
            int apiPort = message.optInt("api_port", 7575);
            if (apiPort < 1 || apiPort > 65535) return;
            String serverURL = "http://" + packet.getAddress().getHostAddress() + ":" + apiPort;
            String type = message.optString("type");
            if ("vohive_server_offer".equals(type)) {
                listener.onServerFound(serverURL);
                return;
            }
            if (!"vohive_pair_approved".equals(type)) return;
            String configuredAgent = AgentConfig.prefs(service)
                    .getString(AgentConfig.KEY_AGENT_ID, "");
            if (!configuredAgent.equals(message.optString("agent_id"))) return;
            String deviceID = message.optString("device_id").trim();
            String pairCode = message.optString("pair_code").trim();
            if (deviceID.isEmpty() || pairCode.isEmpty()) return;
            listener.onPairingApproved(serverURL, deviceID, pairCode);
        } catch (Exception ignored) {
        }
    }

    private static Set<InetAddress> broadcastAddresses() {
        Set<InetAddress> out = new HashSet<>();
        try {
            for (NetworkInterface network : Collections.list(NetworkInterface.getNetworkInterfaces())) {
                if (!network.isUp() || network.isLoopback()) continue;
                for (InterfaceAddress address : network.getInterfaceAddresses()) {
                    InetAddress broadcast = address.getBroadcast();
                    if (broadcast instanceof Inet4Address) out.add(broadcast);
                }
            }
        } catch (Exception ignored) {
        }
        return out;
    }

    private static String randomNonce() {
        byte[] bytes = new byte[16];
        new SecureRandom().nextBytes(bytes);
        StringBuilder out = new StringBuilder(bytes.length * 2);
        for (byte value : bytes) out.append(String.format("%02x", value & 0xff));
        return out.toString();
    }

    @Override public synchronized void close() {
        running.set(false);
        DatagramSocket current = socket;
        if (current != null) current.close();
        Thread currentWorker = worker;
        worker = null;
        if (currentWorker != null) currentWorker.interrupt();
    }
}
