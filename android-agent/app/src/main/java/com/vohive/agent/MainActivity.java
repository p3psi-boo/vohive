package com.vohive.agent;

import android.Manifest;
import android.annotation.SuppressLint;
import android.app.Activity;
import android.app.AlertDialog;
import android.app.role.RoleManager;
import android.content.ClipData;
import android.content.ClipboardManager;
import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.content.pm.PackageManager;
import android.graphics.Color;
import android.graphics.Typeface;
import android.graphics.drawable.GradientDrawable;
import android.net.Uri;
import android.net.wifi.WifiInfo;
import android.net.wifi.WifiManager;
import android.os.Build;
import android.os.Bundle;
import android.os.PowerManager;
import android.provider.Settings;
import android.provider.Telephony;
import android.text.InputType;
import android.text.TextUtils;
import android.text.format.Formatter;
import android.view.Gravity;
import android.view.View;
import android.view.ViewGroup;
import android.widget.Button;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;
import android.widget.Toast;

import org.json.JSONObject;

import java.net.Inet4Address;
import java.net.InetAddress;
import java.net.NetworkInterface;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;

public class MainActivity extends Activity {
    private static final int REQ_PERMISSIONS = 1001;
    private static final int REQ_DEFAULT_SMS = 1002;

    private TextView statusBadge;
    private TextView localUrlText;
    private TextView upstreamStatusText;
    private TextView permPhoneText;
    private TextView permSmsText;
    private TextView permDefaultSmsText;
    private TextView permBatteryText;
    private TextView permLocationText;

    private Button btnToggleService;
    private Button btnFixPhone;
    private Button btnFixSms;
    private Button btnFixDefaultSms;
    private Button btnFixBattery;
    private Button btnFixLocation;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        AgentConfig.ensureInitialized(this);
        setContentView(buildContentView());
        refreshState();
    }

    @Override
    protected void onResume() {
        super.onResume();
        refreshState();
    }

    private View buildContentView() {
        ScrollView scrollView = new ScrollView(this);
        scrollView.setBackgroundColor(Color.parseColor("#F8FAFC"));

        LinearLayout root = new LinearLayout(this);
        root.setOrientation(LinearLayout.VERTICAL);
        root.setPadding(dp(16), dp(20), dp(16), dp(24));

        // Header Title
        TextView title = new TextView(this);
        title.setText("VoHive Agent 控制台");
        title.setTextSize(22);
        title.setTypeface(null, Typeface.BOLD);
        title.setTextColor(Color.parseColor("#0F172A"));
        root.addView(title);

        TextView subTitle = new TextView(this);
        subTitle.setText("面向 4G/5G 模组与 Android 设备的无缝代理与通信 Agent");
        subTitle.setTextSize(12);
        subTitle.setTextColor(Color.parseColor("#64748B"));
        subTitle.setPadding(0, dp(4), 0, dp(16));
        root.addView(subTitle);

        // Card 1: Service & Network Status
        root.addView(buildStatusCard());

        // Card 2: QR & Pairing
        root.addView(buildPairingCard());

        // Card 3: Permission & Diagnostics Checklist
        root.addView(buildDiagnosticsCard());

        // Card 4: Keep-Alive & Autostart Guide
        root.addView(buildKeepAliveCard());

        scrollView.addView(root);
        return scrollView;
    }

    private View buildStatusCard() {
        LinearLayout card = createCardLayout();

        TextView cardTitle = createCardTitle("🟢 前台服务与连接状态");
        card.addView(cardTitle);

        statusBadge = new TextView(this);
        statusBadge.setTextSize(14);
        statusBadge.setTypeface(null, Typeface.BOLD);
        statusBadge.setPadding(0, dp(6), 0, dp(4));
        card.addView(statusBadge);

        localUrlText = new TextView(this);
        localUrlText.setTextSize(13);
        localUrlText.setTextColor(Color.parseColor("#334155"));
        localUrlText.setPadding(0, dp(2), 0, dp(4));
        card.addView(localUrlText);

        upstreamStatusText = new TextView(this);
        upstreamStatusText.setTextSize(12);
        upstreamStatusText.setTextColor(Color.parseColor("#475569"));
        card.addView(upstreamStatusText);

        LinearLayout btnRow = new LinearLayout(this);
        btnRow.setOrientation(LinearLayout.HORIZONTAL);
        btnRow.setPadding(0, dp(12), 0, 0);

        btnToggleService = createPrimaryButton("启动服务", v -> toggleService());
        btnRow.addView(btnToggleService, createWeightedLayoutParams());

        Button btnOpenWeb = createSecondaryButton("打开本地网页", v -> openLocalWeb());
        btnRow.addView(btnOpenWeb, createWeightedLayoutParams());

        Button btnCopyUrl = createSecondaryButton("复制 IP 地址", v -> copyLocalUrl());
        btnRow.addView(btnCopyUrl, createWeightedLayoutParams());

        card.addView(btnRow);
        return card;
    }

    private View buildPairingCard() {
        LinearLayout card = createCardLayout();

        TextView cardTitle = createCardTitle("🔗 扫码与服务器配对");
        card.addView(cardTitle);

        TextView desc = new TextView(this);
        desc.setText("使用 VoHive Web 控制台生成的二维码或六位配对码完成快速绑定。");
        desc.setTextSize(12);
        desc.setTextColor(Color.parseColor("#475569"));
        desc.setPadding(0, dp(4), 0, dp(10));
        card.addView(desc);

        LinearLayout btnRow = new LinearLayout(this);
        btnRow.setOrientation(LinearLayout.HORIZONTAL);

        Button btnScanQr = createPrimaryButton("📷 导入 / 粘贴二维码", v -> showQrImportDialog());
        btnRow.addView(btnScanQr, createWeightedLayoutParams());

        Button btnPairCode = createSecondaryButton("⌨️ 六位码配对", v -> showPairCodeDialog());
        btnRow.addView(btnPairCode, createWeightedLayoutParams());

        card.addView(btnRow);
        return card;
    }

    private View buildDiagnosticsCard() {
        LinearLayout card = createCardLayout();

        TextView cardTitle = createCardTitle("⚡️ 权限与系统配置诊断");
        card.addView(cardTitle);

        // 1. Phone state
        permPhoneText = new TextView(this);
        btnFixPhone = createSmallButton("授权", v -> requestPhonePermissions());
        card.addView(createDiagRow("设备标识与电话状态", permPhoneText, btnFixPhone));

        // 2. SMS powers
        permSmsText = new TextView(this);
        btnFixSms = createSmallButton("授权", v -> requestSmsPermissions());
        card.addView(createDiagRow("短信收发与读写", permSmsText, btnFixSms));

        // 3. Default SMS App
        permDefaultSmsText = new TextView(this);
        btnFixDefaultSms = createSmallButton("设为默认", v -> requestDefaultSmsApp());
        card.addView(createDiagRow("默认短信应用角色", permDefaultSmsText, btnFixDefaultSms));

        // 4. Battery optimization
        permBatteryText = new TextView(this);
        btnFixBattery = createSmallButton("加入白名单", v -> requestIgnoreBatteryOptimization());
        card.addView(createDiagRow("忽略电池优化 (保活)", permBatteryText, btnFixBattery));

        // 5. Location (Cellular network info)
        permLocationText = new TextView(this);
        btnFixLocation = createSmallButton("授权", v -> requestLocationPermissions());
        card.addView(createDiagRow("蜂窝网络定位信息", permLocationText, btnFixLocation));

        return card;
    }

    private View buildKeepAliveCard() {
        LinearLayout card = createCardLayout();

        TextView cardTitle = createCardTitle("🛡 手机厂商后台保活指南");
        card.addView(cardTitle);

        TextView guide = new TextView(this);
        guide.setText(
                "为防止 Agent 常驻后台时被系统清理，请完成以下配置：\n\n" +
                "• 小米 (MIUI/HyperOS)：设置 -> 应用设置 -> 授权管理 -> 自启动管理 -> 开启 VoHive Agent\n" +
                "• 华为 (HarmonyOS)：设置 -> 应用 -> 应用启动管理 -> 关闭自动管理 -> 开启允许自启动与后台活动\n" +
                "• OPPO / vivo：设置 -> 电池 -> 后台耗电管理 -> 允许高耗电行为 / 允许后台自启动\n" +
                "• 在多任务卡片页中将 VoHive Agent 卡片拖动上锁"
        );
        guide.setTextSize(12);
        guide.setTextColor(Color.parseColor("#475569"));
        guide.setLineSpacing(dp(2), 1.1f);
        guide.setPadding(0, dp(4), 0, 0);
        card.addView(guide);

        return card;
    }

    // --- State Refresh & Diagnostic Checks ---

    private void refreshState() {
        boolean isRunning = isServiceRunning();
        int port = AgentConfig.httpPort(this);
        String lanIp = getLanIpAddress();
        SharedPreferences prefs = AgentConfig.prefs(this);

        if (isRunning) {
            statusBadge.setText("状态：前台服务运行中");
            statusBadge.setTextColor(Color.parseColor("#059669"));
            btnToggleService.setText("停止服务");
            btnToggleService.setBackground(createRoundDrawable("#EF4444", dp(8)));
        } else {
            statusBadge.setText("状态：服务未启动");
            statusBadge.setTextColor(Color.parseColor("#DC2626"));
            btnToggleService.setText("启动服务");
            btnToggleService.setBackground(createRoundDrawable("#0D9488", dp(8)));
        }

        localUrlText.setText("本地管理地址：http://" + lanIp + ":" + port + "/");

        String serverUrl = prefs.getString(AgentConfig.KEY_SERVER_URL, "");
        String connState = prefs.getString(AgentConfig.KEY_CONNECTION_STATE, "stopped");
        boolean hasPairToken = !TextUtils.isEmpty(prefs.getString(AgentConfig.KEY_PAIR_TOKEN, ""));
        boolean isPaired = !TextUtils.isEmpty(prefs.getString(AgentConfig.KEY_AGENT_TOKEN, ""));

        if (!TextUtils.isEmpty(serverUrl)) {
            String label = isPaired ? "已配对" : (hasPairToken ? "正在等待 VoHive 服务器批准..." : connState);
            upstreamStatusText.setText("VoHive 服务器：" + serverUrl + " (" + label + ")");
        } else {
            upstreamStatusText.setText("VoHive 服务器：未配置/未配对");
        }

        // 1. Phone permissions check
        boolean hasPhone = checkPerm(Manifest.permission.READ_PHONE_STATE);
        updateDiagItem(permPhoneText, btnFixPhone, hasPhone);

        // 2. SMS permissions check
        boolean hasSms = checkPerm(Manifest.permission.RECEIVE_SMS) && checkPerm(Manifest.permission.READ_SMS);
        updateDiagItem(permSmsText, btnFixSms, hasSms);

        // 3. Default SMS app check
        boolean isDefaultSms = SmsController.isDefaultSmsApp(this);
        updateDiagItem(permDefaultSmsText, btnFixDefaultSms, isDefaultSms);

        // 4. Battery optimization check
        PowerManager pm = (PowerManager) getSystemService(Context.POWER_SERVICE);
        boolean isIgnoringBattery = pm != null && pm.isIgnoringBatteryOptimizations(getPackageName());
        updateDiagItem(permBatteryText, btnFixBattery, isIgnoringBattery);

        // 5. Location permission check
        boolean hasLocation = checkPerm(Manifest.permission.ACCESS_FINE_LOCATION);
        updateDiagItem(permLocationText, btnFixLocation, hasLocation);
    }

    private void updateDiagItem(TextView textView, Button fixButton, boolean ok) {
        if (ok) {
            textView.setText("已完成");
            textView.setTextColor(Color.parseColor("#059669"));
            fixButton.setVisibility(View.GONE);
        } else {
            textView.setText("未设置");
            textView.setTextColor(Color.parseColor("#D97706"));
            fixButton.setVisibility(View.VISIBLE);
        }
    }

    private boolean checkPerm(String perm) {
        return checkSelfPermission(perm) == PackageManager.PERMISSION_GRANTED;
    }

    private boolean isServiceRunning() {
        return AgentConfig.agentEnabled(this);
    }

    private void toggleService() {
        boolean isRunning = isServiceRunning();
        Intent serviceIntent = new Intent(this, AgentService.class);
        if (isRunning) {
            AgentConfig.prefs(this).edit().putBoolean(AgentConfig.KEY_AGENT_ENABLED, false).apply();
            stopService(serviceIntent);
            Toast.makeText(this, "已停止 VoHive Agent 服务", Toast.LENGTH_SHORT).show();
        } else {
            AgentConfig.prefs(this).edit().putBoolean(AgentConfig.KEY_AGENT_ENABLED, true).apply();
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                startForegroundService(serviceIntent);
            } else {
                startService(serviceIntent);
            }
            Toast.makeText(this, "已启动 VoHive Agent 服务", Toast.LENGTH_SHORT).show();
        }
        refreshState();
    }

    private void openLocalWeb() {
        int port = AgentConfig.httpPort(this);
        Intent intent = new Intent(Intent.ACTION_VIEW, Uri.parse("http://127.0.0.1:" + port + "/"));
        startActivity(intent);
    }

    private void copyLocalUrl() {
        int port = AgentConfig.httpPort(this);
        String url = "http://" + getLanIpAddress() + ":" + port + "/";
        ClipboardManager cm = (ClipboardManager) getSystemService(Context.CLIPBOARD_SERVICE);
        if (cm != null) {
            cm.setPrimaryClip(ClipData.newPlainText("VoHive Agent URL", url));
            Toast.makeText(this, "已复制地址：" + url, Toast.LENGTH_SHORT).show();
        }
    }

    // --- Pairing Actions (QR & Manual 6-digit Code) ---

    private void showQrImportDialog() {
        AlertDialog.Builder builder = new AlertDialog.Builder(this);
        builder.setTitle("配对二维码解析 / 粘贴");

        LinearLayout layout = new LinearLayout(this);
        layout.setOrientation(LinearLayout.VERTICAL);
        layout.setPadding(dp(20), dp(10), dp(20), dp(10));

        TextView tip = new TextView(this);
        tip.setText("请粘贴在 VoHive 控制台中生成的配对二维码内容 (JSON 或 vohive:// 格式)：");
        tip.setTextSize(12);
        tip.setTextColor(Color.parseColor("#475569"));
        tip.setPadding(0, 0, 0, dp(10));
        layout.addView(tip);

        EditText input = new EditText(this);
        input.setHint("例如：{\"server_url\":\"http://192.168.1.100:7575\",\"code\":\"123456\"}");
        input.setTextSize(13);
        layout.addView(input);

        builder.setView(layout);
        builder.setPositiveButton("开始配对", (dialog, which) -> {
            String payload = input.getText().toString().trim();
            if (!TextUtils.isEmpty(payload)) {
                processPairPayload(payload);
            }
        });
        builder.setNegativeButton("取消", null);

        // Auto paste clipboard if matches json
        ClipboardManager cm = (ClipboardManager) getSystemService(Context.CLIPBOARD_SERVICE);
        if (cm != null && cm.hasPrimaryClip() && cm.getPrimaryClip().getItemCount() > 0) {
            CharSequence clipText = cm.getPrimaryClip().getItemAt(0).getText();
            if (clipText != null && (clipText.toString().contains("server_url") || clipText.toString().startsWith("vohive://"))) {
                input.setText(clipText.toString());
            }
        }

        builder.show();
    }

    private void showPairCodeDialog() {
        AlertDialog.Builder builder = new AlertDialog.Builder(this);
        builder.setTitle("使用六位配对码绑定");

        LinearLayout layout = new LinearLayout(this);
        layout.setOrientation(LinearLayout.VERTICAL);
        layout.setPadding(dp(20), dp(10), dp(20), dp(10));

        EditText serverInput = new EditText(this);
        serverInput.setHint("VoHive 服务器地址 (如 http://192.168.1.100:7575)");
        serverInput.setTextSize(13);
        layout.addView(serverInput);

        EditText codeInput = new EditText(this);
        codeInput.setHint("六位配对码 (如 123456)");
        codeInput.setTextSize(13);
        codeInput.setInputType(InputType.TYPE_CLASS_NUMBER);
        layout.addView(codeInput);

        builder.setView(layout);
        builder.setPositiveButton("配对", (dialog, which) -> {
            String server = serverInput.getText().toString().trim();
            String code = codeInput.getText().toString().trim();
            if (TextUtils.isEmpty(server) || TextUtils.isEmpty(code)) {
                Toast.makeText(this, "请输入完整的服务器地址与配对码", Toast.LENGTH_SHORT).show();
                return;
            }
            executePairing(server, code);
        });
        builder.setNegativeButton("取消", null);
        builder.show();
    }

    private void processPairPayload(String payload) {
        try {
            if (payload.startsWith("vohive://")) {
                Uri uri = Uri.parse(payload);
                String server = uri.getQueryParameter("server");
                String code = uri.getQueryParameter("code");
                if (!TextUtils.isEmpty(server) && !TextUtils.isEmpty(code)) {
                    executePairing(server, code);
                    return;
                }
            }
            JSONObject json = new JSONObject(payload);
            String server = json.optString("server_url", "");
            String code = json.optString("code", "");
            if (!TextUtils.isEmpty(server) && !TextUtils.isEmpty(code)) {
                executePairing(server, code);
                return;
            }
            Toast.makeText(this, "未找到有效的服务器地址或配对码", Toast.LENGTH_SHORT).show();
        } catch (Exception e) {
            Toast.makeText(this, "二维码解析失败: " + e.getMessage(), Toast.LENGTH_SHORT).show();
        }
    }

    private void executePairing(String serverUrl, String pairCode) {
        SharedPreferences.Editor edit = AgentConfig.prefs(this).edit();
        edit.putString(AgentConfig.KEY_SERVER_URL, serverUrl);
        edit.putString(AgentConfig.KEY_PAIR_TOKEN, pairCode);
        edit.remove(AgentConfig.KEY_AGENT_TOKEN);
        edit.putBoolean(AgentConfig.KEY_AGENT_ENABLED, true);
        edit.apply();

        Intent serviceIntent = new Intent(this, AgentService.class);
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            startForegroundService(serviceIntent);
        } else {
            startService(serviceIntent);
        }

        Toast.makeText(this, "已发起配对，正在连接服务器...", Toast.LENGTH_LONG).show();
        refreshState();
    }

    // --- Permissions Request Helpers ---

    private void requestPhonePermissions() {
        requestPermissions(new String[]{
                Manifest.permission.READ_PHONE_STATE,
                Manifest.permission.READ_PHONE_NUMBERS
        }, REQ_PERMISSIONS);
    }

    private void requestSmsPermissions() {
        requestPermissions(new String[]{
                Manifest.permission.RECEIVE_SMS,
                Manifest.permission.SEND_SMS,
                Manifest.permission.READ_SMS
        }, REQ_PERMISSIONS);
    }

    private void requestLocationPermissions() {
        requestPermissions(new String[]{
                Manifest.permission.ACCESS_FINE_LOCATION,
                Manifest.permission.ACCESS_COARSE_LOCATION
        }, REQ_PERMISSIONS);
    }

    private void requestDefaultSmsApp() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            RoleManager roleManager = getSystemService(RoleManager.class);
            if (roleManager != null && roleManager.isRoleAvailable(RoleManager.ROLE_SMS)) {
                startActivityForResult(roleManager.createRequestRoleIntent(RoleManager.ROLE_SMS), REQ_DEFAULT_SMS);
                return;
            }
        }
        Intent intent = new Intent(Telephony.Sms.Intents.ACTION_CHANGE_DEFAULT);
        intent.putExtra(Telephony.Sms.Intents.EXTRA_PACKAGE_NAME, getPackageName());
        startActivityForResult(intent, REQ_DEFAULT_SMS);
    }

    @SuppressLint("BatteryLife")
    private void requestIgnoreBatteryOptimization() {
        try {
            Intent intent = new Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS);
            intent.setData(Uri.parse("package:" + getPackageName()));
            startActivity(intent);
        } catch (Exception e) {
            Intent intent = new Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS);
            startActivity(intent);
        }
    }

    @Override
    public void onRequestPermissionsResult(int requestCode, String[] permissions, int[] grantResults) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults);
        refreshState();
    }

    @Override
    protected void onActivityResult(int requestCode, int resultCode, Intent data) {
        super.onActivityResult(requestCode, resultCode, data);
        refreshState();
    }

    // --- UI Layout Generators ---

    private LinearLayout createCardLayout() {
        LinearLayout card = new LinearLayout(this);
        card.setOrientation(LinearLayout.VERTICAL);
        card.setPadding(dp(16), dp(16), dp(16), dp(16));
        card.setBackground(createRoundDrawable("#FFFFFF", dp(14)));

        LinearLayout.LayoutParams lp = new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT);
        lp.setMargins(0, 0, 0, dp(14));
        card.setLayoutParams(lp);
        return card;
    }

    private TextView createCardTitle(String title) {
        TextView tv = new TextView(this);
        tv.setText(title);
        tv.setTextSize(15);
        tv.setTypeface(null, Typeface.BOLD);
        tv.setTextColor(Color.parseColor("#0F766E"));
        tv.setPadding(0, 0, 0, dp(8));
        return tv;
    }

    private View createDiagRow(String label, TextView statusText, Button fixBtn) {
        LinearLayout row = new LinearLayout(this);
        row.setOrientation(LinearLayout.HORIZONTAL);
        row.setGravity(Gravity.CENTER_VERTICAL);
        row.setPadding(0, dp(8), 0, dp(8));

        TextView labelText = new TextView(this);
        labelText.setText(label);
        labelText.setTextSize(13);
        labelText.setTextColor(Color.parseColor("#1E293B"));
        row.addView(labelText, createWeightedLayoutParams());

        statusText.setTextSize(12);
        statusText.setPadding(dp(8), 0, dp(8), 0);
        row.addView(statusText);

        row.addView(fixBtn);
        return row;
    }

    private Button createPrimaryButton(String text, View.OnClickListener listener) {
        Button btn = new Button(this);
        btn.setText(text);
        btn.setTextSize(13);
        btn.setTextColor(Color.WHITE);
        btn.setTypeface(null, Typeface.BOLD);
        btn.setBackground(createRoundDrawable("#0D9488", dp(8)));
        btn.setOnClickListener(listener);
        return btn;
    }

    private Button createSecondaryButton(String text, View.OnClickListener listener) {
        Button btn = new Button(this);
        btn.setText(text);
        btn.setTextSize(12);
        btn.setTextColor(Color.parseColor("#0F766E"));
        btn.setBackground(createRoundDrawable("#CCFBF1", dp(8)));
        btn.setOnClickListener(listener);
        return btn;
    }

    private Button createSmallButton(String text, View.OnClickListener listener) {
        Button btn = new Button(this);
        btn.setText(text);
        btn.setTextSize(11);
        btn.setTextColor(Color.parseColor("#0F766E"));
        btn.setBackground(createRoundDrawable("#E0F2FE", dp(6)));
        btn.setPadding(dp(10), dp(4), dp(10), dp(4));
        btn.setOnClickListener(listener);
        return btn;
    }

    private GradientDrawable createRoundDrawable(String colorHex, int radiusPx) {
        GradientDrawable gd = new GradientDrawable();
        gd.setColor(Color.parseColor(colorHex));
        gd.setCornerRadius(radiusPx);
        return gd;
    }

    private LinearLayout.LayoutParams createWeightedLayoutParams() {
        LinearLayout.LayoutParams lp = new LinearLayout.LayoutParams(
                0, ViewGroup.LayoutParams.WRAP_CONTENT, 1.0f);
        lp.setMargins(dp(3), 0, dp(3), 0);
        return lp;
    }

    private int dp(int dpVal) {
        float density = getResources().getDisplayMetrics().density;
        return Math.round(dpVal * density);
    }

    private String getLanIpAddress() {
        try {
            List<NetworkInterface> interfaces = Collections.list(NetworkInterface.getNetworkInterfaces());
            for (NetworkInterface intf : interfaces) {
                if (!intf.isUp() || intf.isLoopback()) continue;
                List<InetAddress> addrs = Collections.list(intf.getInetAddresses());
                for (InetAddress addr : addrs) {
                    if (!addr.isLoopbackAddress() && addr instanceof Inet4Address) {
                        return addr.getHostAddress();
                    }
                }
            }
        } catch (Exception ignored) {}
        return "127.0.0.1";
    }
}
