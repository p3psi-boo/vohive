(() => {
  "use strict";
  const $ = (selector) => document.querySelector(selector);
  const state = { csrf: "", timer: null };

  async function api(path, options = {}) {
    const headers = { Accept: "application/json", ...(options.headers || {}) };
    if (options.body !== undefined) headers["Content-Type"] = "application/json";
    if (state.csrf && options.method && options.method !== "GET") headers["X-CSRF-Token"] = state.csrf;
    const response = await fetch(path, {
      method: options.method || "GET",
      credentials: "same-origin",
      headers,
      body: options.body === undefined ? undefined : JSON.stringify(options.body)
    });
    const text = await response.text();
    let payload = {};
    try { payload = text ? JSON.parse(text) : {}; } catch (_) { payload = { message: text }; }
    if (!response.ok) {
      if (response.status === 401) showLogin();
      throw new Error(payload.message || "请求失败");
    }
    return payload;
  }

  async function restoreSession() {
    try {
      const session = await api("/api/auth/session");
      showConsole(session);
    } catch (_) {
      showLogin();
    }
  }

  function showLogin() {
    clearInterval(state.timer);
    state.timer = null;
    state.csrf = "";
    $("#console-view").classList.add("hidden");
    $("#login-view").classList.remove("hidden");
  }

  function showConsole(session) {
    state.csrf = session.csrf_token || "";
    $("#login-view").classList.add("hidden");
    $("#console-view").classList.remove("hidden");
    loadAll();
    clearInterval(state.timer);
    state.timer = setInterval(() => loadAll(true), 5000);
  }

  async function loadAll(silent = false) {
    try {
      const [status, config] = await Promise.all([api("/api/status"), api("/api/config")]);
      render(status, config);
    } catch (error) {
      if (!silent) toast(error.message, true);
    }
  }

  function render(status, config) {
    const upstream = status.upstream || {};
    const service = status.service || {};
    const telephony = status.telephony || {};
    const permissions = status.permissions || {};
    const paired = config.paired === true;
    const connected = upstream.connected === true;

    $("#setup-panel").classList.toggle("hidden", paired);
    $("#paired-panel").classList.toggle("hidden", !paired);
    $("#top-lamp").classList.toggle("online", connected);
    $("#top-label").textContent = connected ? "已连接" : paired ? "等待连接" : "未配对";
    $("#connection-title").textContent = connected ? "已接入" : paired ? "正在连接" : "等待配对";
    $("#connection-copy").textContent = connected
      ? "运行正常"
      : paired ? connectionLabel(upstream.state) : "正在局域网广播";

    const discovered = config.discovered_server_url || "";
    $("#discovered-server").textContent = discovered ? "发现 " + discovered : "尚未发现服务器";
    if (discovered && !$("#pair-server").value) $("#pair-server").value = discovered;
    $("#open-vohive").href = config.server_url || "#";

    const smsReady = permissions.send_sms && permissions.receive_sms && permissions.read_sms;
    setCapability("sms", smsReady ? "可用" : "需授权", smsReady ? "短信收发就绪" : "缺少短信权限", smsReady);
    const networkReady = connected && telephony.data_connected;
    setCapability("proxy", networkReady ? "可用" : connected ? "等待蜂窝网络" : "等待连接",
      networkReady ? "蜂窝代理就绪" : "配对后自动启动", networkReady);
    const esimSupported = telephony.esim_supported === true;
    const esimPrivileged = permissions.write_embedded_subscriptions === true;
    setCapability("esim", !esimSupported ? "不支持" : esimPrivileged ? "可用" : "需确认",
      !esimSupported ? "无 eUICC" : esimPrivileged ? "无交互切换" : "由 Android 确认", esimSupported);

    $("#security-username").value = config.web_username || "admin";
    $("#runtime-facts").innerHTML = [
      ["设备", service.model || "—"],
      ["Android", service.android_version || "—"],
      ["Agent", service.app_version || "—"],
      ["运行时间", formatDuration(service.uptime_ms || 0)],
      ["设备 ID", config.device_id || "未分配"],
      ["Agent ID", config.agent_id || "—"]
    ].map((item) => "<div><dt>" + escapeHtml(item[0]) + "</dt><dd>" + escapeHtml(item[1]) + "</dd></div>").join("");
    const urls = status.web && status.web.urls ? status.web.urls : [];
    $("#management-urls").innerHTML = urls.length
      ? urls.map((url) => "<code>" + escapeHtml(url) + "</code>").join("")
      : "<span>尚未识别局域网地址</span>";
  }

  function setCapability(prefix, result, detail, ready) {
    $("#" + prefix + "-result").textContent = result;
    $("#" + prefix + "-result").classList.toggle("ready", ready);
    $("#" + prefix + "-detail").textContent = detail;
  }

  $("#login-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    $("#login-error").textContent = "";
    try {
      const session = await api("/api/auth/login", { method: "POST", body: {
        username: $("#login-username").value.trim(),
        password: $("#login-password").value
      }});
      $("#login-password").value = "";
      showConsole(session);
    } catch (error) {
      $("#login-error").textContent = error.message;
    }
  });

  $("#logout").addEventListener("click", async () => {
    try { await api("/api/auth/logout", { method: "POST", body: {} }); } catch (_) {}
    showLogin();
  });

  $("#pair-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    const code = $("#pair-code").value.trim();
    if (!/^[0-9]{6}$/.test(code)) {
      toast("请输入六位配对码", true);
      return;
    }
    try {
      await api("/api/config", { method: "PUT", body: {
        server_url: $("#pair-server").value.trim(),
        pair_token: code,
        agent_enabled: true
      }});
      toast("配对请求已提交");
      $("#pair-code").value = "";
      setTimeout(loadAll, 800);
    } catch (error) {
      toast(error.message, true);
    }
  });

  $("#reconnect").addEventListener("click", async () => {
    try {
      await api("/api/agent/reconnect", { method: "POST", body: {} });
      toast("正在重新连接");
      setTimeout(loadAll, 800);
    } catch (error) { toast(error.message, true); }
  });

  $("#reset-pairing").addEventListener("click", async () => {
    if (!confirm("解除当前配对并重新进入局域网发现模式？")) return;
    try {
      await api("/api/pairing/reset", { method: "POST", body: {} });
      toast("已进入重新配对模式");
      setTimeout(loadAll, 500);
    } catch (error) { toast(error.message, true); }
  });

  $("#password-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    const next = $("#security-new").value;
    if (next !== $("#security-confirm").value) {
      toast("两次输入的新密码不一致", true);
      return;
    }
    try {
      await api("/api/auth/password", { method: "PUT", body: {
        username: $("#security-username").value.trim(),
        current_password: $("#security-current").value,
        new_password: next
      }});
      toast("凭据已更新，请重新登录");
      setTimeout(showLogin, 700);
    } catch (error) { toast(error.message, true); }
  });

  let toastTimer;
  function toast(message, error = false) {
    const node = $("#toast");
    node.textContent = message || "操作完成";
    node.classList.toggle("error", error);
    node.classList.add("show");
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => node.classList.remove("show"), 3000);
  }

  function connectionLabel(value) {
    if (!value) return "状态未知";
    if (value === "connecting") return "正在连接 VoHive";
    if (value.startsWith("reconnecting")) return "网络中断，自动重试";
    if (value.startsWith("waiting")) return "检查连接配置";
    return "未连接";
  }

  function formatDuration(ms) {
    const minutes = Math.floor(ms / 60000);
    const hours = Math.floor(minutes / 60);
    const days = Math.floor(hours / 24);
    if (days) return days + "天 " + (hours % 24) + "小时";
    if (hours) return hours + "小时 " + (minutes % 60) + "分钟";
    return minutes + "分钟";
  }

  function escapeHtml(value) {
    return String(value).replace(/[&<>'"]/g, (char) => ({
      "&": "&amp;", "<": "&lt;", ">": "&gt;", "'": "&#39;", '"': "&quot;"
    })[char]);
  }

  restoreSession();
})();
