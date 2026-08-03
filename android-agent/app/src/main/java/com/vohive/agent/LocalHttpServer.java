package com.vohive.agent;

import android.annotation.SuppressLint;
import android.util.Base64;
import android.util.Log;

import org.json.JSONObject;

import java.io.BufferedInputStream;
import java.io.ByteArrayOutputStream;
import java.io.Closeable;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.ServerSocket;
import java.net.Socket;
import java.net.URLDecoder;
import java.nio.charset.StandardCharsets;
import java.security.SecureRandom;
import java.time.Instant;
import java.util.LinkedHashMap;
import java.util.Locale;
import java.util.Map;
import java.util.concurrent.ArrayBlockingQueue;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.ThreadPoolExecutor;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;

@SuppressLint("ApplySharedPref") // Credential/config changes must be visible before the response returns.
final class LocalHttpServer implements Closeable {
    private static final String TAG = "VoHiveWeb";
    private static final String SESSION_COOKIE = "vohive_session";
    private static final int MAX_REQUEST_LINE = 8 * 1024;
    private static final int MAX_HEADER_LINE = 8 * 1024;
    private static final int MAX_HEADERS = 32 * 1024;
    private static final int MAX_BODY = 1024 * 1024;
    private static final long SESSION_TTL_MS = 12 * 60 * 60 * 1000L;
    private static final long LOGIN_WINDOW_MS = 10 * 60 * 1000L;
    private static final long LOGIN_BLOCK_MS = 60 * 1000L;
    private static final int MAX_LOGIN_FAILURES = 5;
    private static final SecureRandom RANDOM = new SecureRandom();

    private final AgentService service;
    private final int port;
    private final ExecutorService acceptor;
    private final ThreadPoolExecutor workers;
    private final Map<String, Session> sessions = new ConcurrentHashMap<>();
    private final Map<String, LoginGuard> loginGuards = new ConcurrentHashMap<>();
    private final Map<String, Asset> assets = new LinkedHashMap<>();

    private volatile boolean running;
    private ServerSocket serverSocket;

    LocalHttpServer(AgentService service, int port) throws IOException {
        this.service = service;
        this.port = port;
        this.acceptor = Executors.newSingleThreadExecutor(r -> {
            Thread thread = new Thread(r, "vohive-http-accept");
            thread.setDaemon(true);
            return thread;
        });
        AtomicInteger workerID = new AtomicInteger();
        this.workers = new ThreadPoolExecutor(2, 8, 30, TimeUnit.SECONDS,
                new ArrayBlockingQueue<>(32), r -> {
                    Thread thread = new Thread(r, "vohive-http-" + workerID.incrementAndGet());
                    thread.setDaemon(true);
                    return thread;
                }, new ThreadPoolExecutor.AbortPolicy());
        loadAssets("web");
        assets.put("/", assets.get("/index.html"));
    }

    // 递归加载 assets/web 下全部构建产物，按扩展名白名单映射 MIME。
    // 请求路径只与预加载表精确匹配，天然杜绝路径穿越。
    private void loadAssets(String assetDir) throws IOException {
        String[] children = service.getAssets().list(assetDir);
        if (children == null) return;
        for (String child : children) {
            String assetPath = assetDir + "/" + child;
            String requestPath = "/" + assetPath.substring("web/".length());
            if (child.indexOf('.') < 0) {
                // Vite 产物中无扩展名的条目均为目录（assets/、icons/）
                loadAssets(assetPath);
                continue;
            }
            String contentType = contentTypeOf(child);
            if (contentType == null) continue;
            assets.put(requestPath, loadAsset(assetPath, contentType));
        }
    }

    private static String contentTypeOf(String name) {
        String lower = name.toLowerCase(Locale.ROOT);
        if (lower.endsWith(".html")) return "text/html; charset=utf-8";
        if (lower.endsWith(".js")) return "application/javascript; charset=utf-8";
        if (lower.endsWith(".css")) return "text/css; charset=utf-8";
        if (lower.endsWith(".json") || lower.endsWith(".webmanifest")) return "application/json; charset=utf-8";
        if (lower.endsWith(".svg")) return "image/svg+xml";
        if (lower.endsWith(".png")) return "image/png";
        if (lower.endsWith(".ico")) return "image/x-icon";
        if (lower.endsWith(".woff2")) return "font/woff2";
        return null;
    }

    private static String cachePolicyOf(String requestPath) {
        // HTML、manifest 与 Service Worker 必须每次校验，否则更新无法生效
        if ("/".equals(requestPath) || "/index.html".equals(requestPath)
                || "/sw.js".equals(requestPath) || "/registerSW.js".equals(requestPath)
                || "/manifest.webmanifest".equals(requestPath)) {
            return "no-store";
        }
        // Vite 产物带内容 hash，可永久缓存
        if (requestPath.startsWith("/assets/")) return "public, max-age=31536000, immutable";
        return "public, max-age=300";
    }

    synchronized void start() throws IOException {
        if (running) return;
        ServerSocket socket = new ServerSocket();
        socket.setReuseAddress(true);
        socket.bind(new InetSocketAddress(InetAddress.getByName("0.0.0.0"), port), 50);
        serverSocket = socket;
        running = true;
        acceptor.execute(this::acceptLoop);
        Log.i(TAG, "HTTP management server listening on 0.0.0.0:" + port);
    }

    int port() { return port; }

    boolean isRunning() { return running; }

    private void acceptLoop() {
        while (running) {
            Socket socket = null;
            try {
                socket = serverSocket.accept();
                socket.setSoTimeout(12_000);
                socket.setTcpNoDelay(true);
                Socket accepted = socket;
                workers.execute(() -> handle(accepted));
            } catch (Exception error) {
                closeQuietly(socket);
                if (running) Log.w(TAG, "HTTP accept failed", error);
            }
        }
    }

    private void handle(Socket socket) {
        try (socket; InputStream rawInput = new BufferedInputStream(socket.getInputStream());
             OutputStream output = socket.getOutputStream()) {
            Request request = readRequest(rawInput, socket);
            Response response;
            try {
                response = route(request);
            } catch (HttpError error) {
                response = error(error.status, error.getMessage());
            } catch (IllegalArgumentException error) {
                response = error(400, error.getMessage());
            } catch (SecurityException error) {
                response = error(403, error.getMessage());
            } catch (Exception error) {
                Log.w(TAG, "HTTP route failed: " + request.method + " " + request.path, error);
                response = error(500, "internal server error");
            }
            writeResponse(output, response);
        } catch (HttpError error) {
            try {
                OutputStream output = socket.getOutputStream();
                writeResponse(output, error(error.status, error.getMessage()));
            } catch (Exception ignored) {
            }
            closeQuietly(socket);
        } catch (Exception ignored) {
            closeQuietly(socket);
        }
    }

    private Response route(Request request) throws Exception {
        if ("GET".equals(request.method)) {
            Asset asset = assets.get(request.path);
            if (asset != null) return Response.bytes(200, asset.contentType, asset.data)
                    .header("Cache-Control", cachePolicyOf(request.path));
        }

        if ("POST".equals(request.method) && "/api/auth/login".equals(request.path)) {
            return login(request);
        }

        Session session = authenticate(request);
        if (session == null) throw new HttpError(401, "authentication required");
        session.expiresAt = System.currentTimeMillis() + SESSION_TTL_MS;

        if ("GET".equals(request.method) && "/api/auth/session".equals(request.path)) {
            return json(200, new JSONObject()
                    .put("authenticated", true)
                    .put("username", session.username)
                    .put("csrf_token", session.csrfToken)
                    .put("expires_at", Instant.ofEpochMilli(session.expiresAt).toString()));
        }

        if (isMutation(request.method)) requireCsrf(request, session);

        if ("POST".equals(request.method) && "/api/auth/logout".equals(request.path)) {
            sessions.remove(session.token);
            return json(200, new JSONObject().put("ok", true))
                    .header("Set-Cookie", SESSION_COOKIE
                            + "=; Path=/; HttpOnly; SameSite=Strict; Max-Age=0");
        }
        if ("PUT".equals(request.method) && "/api/auth/password".equals(request.path)) {
            return changePassword(request, session);
        }
        if ("GET".equals(request.method) && "/api/status".equals(request.path)) {
            return json(200, service.webStatus());
        }
        if ("GET".equals(request.method) && "/api/config".equals(request.path)) {
            return json(200, service.webConfig());
        }
        if ("PUT".equals(request.method) && "/api/config".equals(request.path)) {
            return json(200, service.updateWebConfig(request.json()));
        }
        if ("POST".equals(request.method) && "/api/agent/start".equals(request.path)) {
            return json(200, service.startUpstreamFromWeb());
        }
        if ("POST".equals(request.method) && "/api/agent/stop".equals(request.path)) {
            return json(200, service.stopUpstreamFromWeb());
        }
        if ("POST".equals(request.method) && "/api/agent/reconnect".equals(request.path)) {
            return json(200, service.reconnectUpstreamFromWeb());
        }
        if ("POST".equals(request.method) && "/api/pairing/reset".equals(request.path)) {
            return json(200, service.resetPairingFromWeb());
        }
        if ("POST".equals(request.method) && "/api/telephony/refresh".equals(request.path)) {
            return json(200, service.refreshTelephonyFromWeb());
        }
        if ("GET".equals(request.method) && "/api/subscriptions".equals(request.path)) {
            return json(200, service.webSubscriptions());
        }
        if ("POST".equals(request.method) && "/api/subscriptions/select".equals(request.path)) {
            return json(200, service.selectSubscriptionFromWeb(request.json()));
        }
        if ("POST".equals(request.method) && "/api/esim/switch".equals(request.path)) {
            return json(200, service.switchEsimFromWeb(request.json()));
        }
        if ("GET".equals(request.method) && "/api/diagnostics".equals(request.path)) {
            return json(200, service.webDiagnostics());
        }
        if ("GET".equals(request.method) && "/api/sms".equals(request.path)) {
            return json(200, service.webSmsList(integerQuery(request.query, "limit", 100)));
        }
        if ("POST".equals(request.method) && "/api/sms/send".equals(request.path)) {
            return json(200, service.sendSmsFromWeb(request.json()));
        }
        if ("DELETE".equals(request.method) && "/api/sms".equals(request.path)) {
            return json(200, service.deleteAllSmsFromWeb());
        }
        if (request.path.startsWith("/api/sms/")) {
            long id;
            try {
                id = Long.parseLong(request.path.substring("/api/sms/".length()));
            } catch (NumberFormatException error) {
                throw new HttpError(400, "invalid SMS id");
            }
            if ("GET".equals(request.method)) return json(200, service.readSmsFromWeb(id));
            if ("DELETE".equals(request.method)) return json(200, service.deleteSmsFromWeb(id));
        }
        throw new HttpError(404, "not found");
    }

    private Response login(Request request) throws Exception {
        String remote = request.remoteAddress;
        long now = System.currentTimeMillis();
        LoginGuard guard = loginGuards.computeIfAbsent(remote, ignored -> new LoginGuard(now));
        synchronized (guard) {
            if (guard.blockedUntil > now) {
                long wait = Math.max(1, (guard.blockedUntil - now + 999) / 1000);
                return error(429, "too many login attempts")
                        .header("Retry-After", Long.toString(wait));
            }
            if (now - guard.windowStarted > LOGIN_WINDOW_MS) guard.reset(now);
        }
        JSONObject body = request.json();
        String username = body.optString("username", "");
        String password = body.optString("password", "");
        if (!WebAuth.verify(service, username, password)) {
            synchronized (guard) {
                guard.failures++;
                if (guard.failures >= MAX_LOGIN_FAILURES) guard.blockedUntil = now + LOGIN_BLOCK_MS;
            }
            throw new HttpError(401, "invalid username or password");
        }
        loginGuards.remove(remote);
        cleanupSessions(now);
        Session session = new Session(randomToken(32), randomToken(24), username,
                now + SESSION_TTL_MS);
        sessions.put(session.token, session);
        return json(200, new JSONObject()
                .put("authenticated", true)
                .put("username", username)
                .put("csrf_token", session.csrfToken)
                .put("expires_at", Instant.ofEpochMilli(session.expiresAt).toString()))
                .header("Set-Cookie", SESSION_COOKIE + "=" + session.token
                        + "; Path=/; HttpOnly; SameSite=Strict; Max-Age="
                        + (SESSION_TTL_MS / 1000));
    }

    private Response changePassword(Request request, Session session) throws Exception {
        JSONObject body = request.json();
        String currentPassword = body.optString("current_password", "");
        String newPassword = body.optString("new_password", "");
        String newUsername = body.optString("username", session.username).trim();
        if (!WebAuth.verify(service, session.username, currentPassword)) {
            throw new HttpError(403, "current password is incorrect");
        }
        if (newUsername.length() < 3 || newUsername.length() > 64) {
            throw new HttpError(400, "username must contain 3 to 64 characters");
        }
        if (newPassword.length() < 12 || newPassword.length() > 256) {
            throw new HttpError(400, "new password must contain 12 to 256 characters");
        }
        AgentConfig.prefs(service).edit()
                .putString(AgentConfig.KEY_WEB_USERNAME, newUsername).commit();
        WebAuth.setPassword(service, newPassword);
        sessions.clear();
        return json(200, new JSONObject().put("ok", true))
                .header("Set-Cookie", SESSION_COOKIE
                        + "=; Path=/; HttpOnly; SameSite=Strict; Max-Age=0");
    }

    private Session authenticate(Request request) {
        String cookieHeader = request.headers.get("cookie");
        if (cookieHeader == null) return null;
        String token = "";
        for (String item : cookieHeader.split(";")) {
            String[] pair = item.trim().split("=", 2);
            if (pair.length == 2 && SESSION_COOKIE.equals(pair[0])) {
                token = pair[1];
                break;
            }
        }
        Session session = sessions.get(token);
        if (session == null) return null;
        if (session.expiresAt <= System.currentTimeMillis()) {
            sessions.remove(token);
            return null;
        }
        return session;
    }

    private static void requireCsrf(Request request, Session session) throws HttpError {
        String supplied = request.headers.get("x-csrf-token");
        if (supplied == null || !constantEquals(supplied, session.csrfToken)) {
            throw new HttpError(403, "invalid CSRF token");
        }
    }

    private static boolean constantEquals(String left, String right) {
        return java.security.MessageDigest.isEqual(left.getBytes(StandardCharsets.UTF_8),
                right.getBytes(StandardCharsets.UTF_8));
    }

    private void cleanupSessions(long now) {
        sessions.entrySet().removeIf(entry -> entry.getValue().expiresAt <= now);
    }

    private static String randomToken(int bytes) {
        byte[] value = new byte[bytes];
        RANDOM.nextBytes(value);
        return Base64.encodeToString(value, Base64.URL_SAFE | Base64.NO_WRAP | Base64.NO_PADDING);
    }

    private Request readRequest(InputStream input, Socket socket) throws Exception {
        String requestLine = readLine(input, MAX_REQUEST_LINE);
        if (requestLine == null || requestLine.isEmpty()) throw new HttpError(400, "empty request");
        String[] parts = requestLine.split(" ", 3);
        if (parts.length != 3 || !parts[2].startsWith("HTTP/1.")) {
            throw new HttpError(400, "invalid request line");
        }
        String method = parts[0].toUpperCase(Locale.ROOT);
        if (!method.matches("GET|POST|PUT|DELETE")) throw new HttpError(405, "method not allowed");
        String target = parts[1];
        int question = target.indexOf('?');
        String rawPath = question < 0 ? target : target.substring(0, question);
        String query = question < 0 ? "" : target.substring(question + 1);
        String path;
        try {
            path = URLDecoder.decode(rawPath, StandardCharsets.UTF_8.name());
        } catch (Exception error) {
            throw new HttpError(400, "invalid URL encoding");
        }
        if (!path.startsWith("/") || path.contains("..")) throw new HttpError(400, "invalid path");

        Map<String, String> headers = new LinkedHashMap<>();
        int headerBytes = 0;
        while (true) {
            String line = readLine(input, MAX_HEADER_LINE);
            if (line == null) throw new HttpError(400, "incomplete headers");
            if (line.isEmpty()) break;
            headerBytes += line.length();
            if (headerBytes > MAX_HEADERS) throw new HttpError(431, "headers too large");
            int colon = line.indexOf(':');
            if (colon <= 0) throw new HttpError(400, "invalid header");
            headers.put(line.substring(0, colon).trim().toLowerCase(Locale.ROOT),
                    line.substring(colon + 1).trim());
        }
        if (headers.containsKey("transfer-encoding")) {
            throw new HttpError(400, "transfer encoding is not supported");
        }
        int length = 0;
        if (headers.containsKey("content-length")) {
            try {
                length = Integer.parseInt(headers.get("content-length"));
            } catch (NumberFormatException error) {
                throw new HttpError(400, "invalid content length");
            }
        }
        if (length < 0 || length > MAX_BODY) throw new HttpError(413, "request body too large");
        byte[] body = readExactly(input, length);
        return new Request(method, path, query, headers, body,
                socket.getInetAddress().getHostAddress());
    }

    private static String readLine(InputStream input, int maxBytes) throws Exception {
        ByteArrayOutputStream buffer = new ByteArrayOutputStream();
        int previous = -1;
        while (buffer.size() <= maxBytes) {
            int value = input.read();
            if (value < 0) {
                if (buffer.size() == 0) return null;
                break;
            }
            if (value == '\n') break;
            if (previous == '\r') buffer.write('\r');
            if (value != '\r') buffer.write(value);
            previous = value;
        }
        if (buffer.size() > maxBytes) throw new HttpError(431, "line too large");
        return buffer.toString(StandardCharsets.ISO_8859_1.name());
    }

    private static int integerQuery(String query, String name, int fallback) throws HttpError {
        if (query == null || query.isEmpty()) return fallback;
        for (String item : query.split("&")) {
            String[] pair = item.split("=", 2);
            if (!name.equals(pair[0])) continue;
            try {
                return Integer.parseInt(pair.length == 2 ? pair[1] : "");
            } catch (NumberFormatException error) {
                throw new HttpError(400, "invalid query parameter: " + name);
            }
        }
        return fallback;
    }

    private static void writeResponse(OutputStream output, Response response) throws IOException {
        String reason = reason(response.status);
        StringBuilder head = new StringBuilder()
                .append("HTTP/1.1 ").append(response.status).append(' ').append(reason).append("\r\n")
                .append("Content-Type: ").append(response.contentType).append("\r\n")
                .append("Content-Length: ").append(response.body.length).append("\r\n")
                .append("Connection: close\r\n")
                .append("X-Content-Type-Options: nosniff\r\n")
                .append("X-Frame-Options: DENY\r\n")
                .append("Referrer-Policy: no-referrer\r\n")
                .append("Permissions-Policy: camera=(), microphone=(), geolocation=()\r\n")
                .append("Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'\r\n");
        for (Map.Entry<String, String> header : response.headers.entrySet()) {
            head.append(header.getKey()).append(": ").append(header.getValue()).append("\r\n");
        }
        head.append("\r\n");
        output.write(head.toString().getBytes(StandardCharsets.ISO_8859_1));
        output.write(response.body);
        output.flush();
    }

    private Asset loadAsset(String path, String contentType) throws IOException {
        try (InputStream input = service.getAssets().open(path)) {
            ByteArrayOutputStream output = new ByteArrayOutputStream();
            byte[] buffer = new byte[8192];
            int count;
            while ((count = input.read(buffer)) >= 0) output.write(buffer, 0, count);
            return new Asset(output.toByteArray(), contentType);
        }
    }

    private static byte[] readExactly(InputStream input, int length) throws Exception {
        byte[] result = new byte[length];
        int offset = 0;
        while (offset < length) {
            int count = input.read(result, offset, length - offset);
            if (count < 0) throw new HttpError(400, "incomplete request body");
            offset += count;
        }
        return result;
    }

    private static Response json(int status, JSONObject body) {
        return Response.bytes(status, "application/json; charset=utf-8",
                body.toString().getBytes(StandardCharsets.UTF_8))
                .header("Cache-Control", "no-store");
    }

    private static Response error(int status, String message) {
        try {
            return json(status, new JSONObject().put("error", message == null ? "request failed" : message));
        } catch (Exception ignored) {
            return Response.bytes(status, "application/json; charset=utf-8",
                    "{\"error\":\"request failed\"}".getBytes(StandardCharsets.UTF_8));
        }
    }

    private static boolean isMutation(String method) {
        return "POST".equals(method) || "PUT".equals(method) || "DELETE".equals(method);
    }

    private static String reason(int status) {
        switch (status) {
            case 200: return "OK";
            case 400: return "Bad Request";
            case 401: return "Unauthorized";
            case 403: return "Forbidden";
            case 404: return "Not Found";
            case 405: return "Method Not Allowed";
            case 413: return "Payload Too Large";
            case 429: return "Too Many Requests";
            case 431: return "Request Header Fields Too Large";
            default: return "Internal Server Error";
        }
    }

    @Override
    public synchronized void close() {
        running = false;
        closeQuietly(serverSocket);
        serverSocket = null;
        sessions.clear();
        acceptor.shutdownNow();
        workers.shutdownNow();
    }

    private static void closeQuietly(Closeable closeable) {
        if (closeable == null) return;
        try { closeable.close(); } catch (IOException ignored) {}
    }

    private static final class Request {
        final String method;
        final String path;
        final String query;
        final Map<String, String> headers;
        final byte[] body;
        final String remoteAddress;

        Request(String method, String path, String query, Map<String, String> headers,
                byte[] body, String remoteAddress) {
            this.method = method;
            this.path = path;
            this.query = query;
            this.headers = headers;
            this.body = body;
            this.remoteAddress = remoteAddress;
        }

        JSONObject json() throws HttpError {
            String type = headers.get("content-type");
            if (body.length > 0 && (type == null
                    || !type.toLowerCase(Locale.ROOT).startsWith("application/json"))) {
                throw new HttpError(400, "Content-Type must be application/json");
            }
            try {
                return body.length == 0 ? new JSONObject()
                        : new JSONObject(new String(body, StandardCharsets.UTF_8));
            } catch (Exception error) {
                throw new HttpError(400, "invalid JSON body");
            }
        }
    }

    private static final class Response {
        final int status;
        final String contentType;
        final byte[] body;
        final Map<String, String> headers = new LinkedHashMap<>();

        private Response(int status, String contentType, byte[] body) {
            this.status = status;
            this.contentType = contentType;
            this.body = body;
        }

        static Response bytes(int status, String contentType, byte[] body) {
            return new Response(status, contentType, body);
        }

        Response header(String name, String value) {
            headers.put(name, value);
            return this;
        }
    }

    private static final class Asset {
        final byte[] data;
        final String contentType;
        Asset(byte[] data, String contentType) {
            this.data = data;
            this.contentType = contentType;
        }
    }

    private static final class Session {
        final String token;
        final String csrfToken;
        final String username;
        volatile long expiresAt;
        Session(String token, String csrfToken, String username, long expiresAt) {
            this.token = token;
            this.csrfToken = csrfToken;
            this.username = username;
            this.expiresAt = expiresAt;
        }
    }

    private static final class LoginGuard {
        long windowStarted;
        int failures;
        long blockedUntil;
        LoginGuard(long now) { reset(now); }
        void reset(long now) {
            windowStarted = now;
            failures = 0;
            blockedUntil = 0;
        }
    }

    private static final class HttpError extends Exception {
        final int status;
        HttpError(int status, String message) {
            super(message);
            this.status = status;
        }
    }
}
