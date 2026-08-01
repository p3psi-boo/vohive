(() => {
  "use strict";

  const state = { csrf: "", username: "", status: null, config: null, subscriptions: [], timer: null };
  const $ = (selector) => document.querySelector(selector);
  const $$ = (selector) => Array.from(document.querySelectorAll(selector));

  async function api(path, options = {}) {
    const method = options.method || "GET";
    const headers = { Accept: "application/json", ...(options.headers || {}) };
    if (options.body !== undefined) headers["Content-Type"] = "application/json";
    if (!["GET", "HEAD"].includes(method) && state.csrf) headers["X-CSRF-Token"] = state.csrf;
    const response = await fetch(path, {
      method,
      headers,
      credentials: "same-origin",
      body: options.body === undefined ? undefined : JSON.stringify(options.body)
    });
    let data = {};
    try { data = await response.json(); } catch (_) { data = {}; }
    if (response.status === 401 && path !== "/api/auth/login") showLogin();
    if (!response.ok) throw new Error(data.error || `HTTP ${response.status}`);
    return data;
  }

  function showLogin() {
    clearInterval(state.timer);
    state.timer = null;
    state.csrf = "";
    $("#console-view").classList.add("hidden");
    $("#login-view").classList.remove("hidden");
    setTimeout(() => $("#login-password").focus(), 30);
  }

  function showConsole(session) {
    state.csrf = session.csrf_token;
    state.username = session.username;
    $("#security-username").value = session.username;
    $("#login-view").classList.add("hidden");
    $("#console-view").classList.remove("hidden");
    loadAll();
    clearInterval(state.timer);
    state.timer = setInterval(() => loadStatus(false), 7000);
  }

  async function restoreSession() {
    try { showConsole(await api("/api/auth/session")); }
    catch (_) { showLogin(); }
  }

  async function loadAll() {
    try {
      await Promise.all([loadStatus(false), loadConfig(), loadSubscriptions(), loadSms()]);
    } catch (error) { toast(error.message, true); }
  }

  async function loadStatus(notify = true) {
    try {
      const data = await api("/api/status");
      state.status = data;
      renderStatus(data);
      if (notify) toast("设备状态已刷新");
    } catch (error) { if (notify) toast(error.message, true); }
  }

  function renderStatus(data) {
    const upstream = data.upstream || {};
    const service = data.service || {};
    const telephony = data.telephony || {};
    const connected = Boolean(upstream.connected);
    $("#service-lamp").classList.toggle("live", Boolean(service.running));
    $("#service-label").textContent = service.running ? "前台服务运行中" : "服务已停止";
    $("#connection-state").textContent = connectionLabel(upstream.state);
    $("#connection-detail").textContent = upstream.configured
      ? `${upstream.enabled ? "连接已启用" : "连接已停用"} · ${upstream.state || "unknown"}`
      : "等待配置 Server URL 与配对凭据";
    $("#device-online").textContent = connected ? "UPSTREAM LIVE" : "LOCAL ONLY";
    $("#current-time").textContent = formatDate(data.timestamp);
    $("#metric-signal").textContent = value(telephony.signal_dbm, "—", " dBm");
    $("#metric-network").textContent = telephony.network_mode || "网络制式未知";
    $("#metric-carrier").textContent = telephony.operator || "—";
    $("#metric-sim").textContent = telephony.sim_inserted ? "SIM 已就绪" : "未检测到就绪 SIM";
    $("#metric-battery").textContent = value(telephony.battery_pct, "—", "%");
    $("#metric-power").textContent = telephony.battery_charging ? "正在充电" : "电池供电";
    $("#metric-uptime").textContent = formatDuration(service.uptime_ms || 0);
    $("#metric-version").textContent = `${service.model || "Android"} · v${service.app_version || "?"}`;
    renderFacts(data);
    renderPermissions(data.permissions || {}, data.default_sms_app);
    renderUrls((data.web || {}).urls || []);
  }

  function renderFacts(data) {
    const t = data.telephony || {};
    const s = data.service || {};
    const facts = [
      ["设备型号", s.model], ["Android", `${s.android_version || "—"} / API ${s.sdk || "—"}`],
      ["IMEI", t.imei], ["ICCID", t.iccid], ["IMSI", t.imsi], ["本机号码", t.msisdn],
      ["基带", t.baseband], ["固件", t.firmware]
    ];
    $("#device-facts").innerHTML = facts.map(([key, val]) =>
      `<div><dt>${escapeHtml(key)}</dt><dd title="${escapeHtml(val || "")}">${escapeHtml(val || "—")}</dd></div>`
    ).join("");
  }

  function renderPermissions(permissions, defaultSms) {
    const labels = {
      read_phone_state: "电话状态", read_phone_numbers: "电话号码", coarse_location: "粗略位置",
      fine_location: "精确位置", send_sms: "发送短信", receive_sms: "接收短信", read_sms: "读取短信",
      notifications: "通知", privileged_phone_state: "特权电话状态", modify_phone_state: "修改射频",
      write_embedded_subscriptions: "切换 eSIM", write_sms: "写入短信库", reboot: "设备重启"
    };
    const entries = Object.entries(permissions);
    const granted = entries.filter(([, ok]) => ok).length + (defaultSms ? 1 : 0);
    const total = entries.length + 1;
    $("#permission-score").textContent = `${granted}/${total}`;
    $("#permission-list").innerHTML = [
      `<div class="permission ${defaultSms ? "yes" : ""}"><i></i><span>默认短信角色</span></div>`,
      ...entries.map(([key, ok]) => `<div class="permission ${ok ? "yes" : ""}"><i></i><span>${escapeHtml(labels[key] || key)}</span></div>`)
    ].join("");
  }

  async function loadConfig() {
    const config = await api("/api/config");
    state.config = config;
    $("#config-server").value = config.server_url || "";
    $("#config-device").value = config.device_id || "";
    $("#config-agent").value = config.agent_id || "";
    $("#config-token").value = "";
    $("#config-token").placeholder = config.paired ? "已配对；留空保持 Agent Token" : (config.pair_token_configured ? "Pair Token 已保存" : "输入 Pair Token");
    $("#config-port").value = config.http_port || 8765;
    $("#config-autostart").checked = Boolean(config.auto_start);
    $("#config-enabled").checked = Boolean(config.agent_enabled);
    $("#security-username").value = config.web_username || state.username;
  }

  async function loadSubscriptions() {
    const data = await api("/api/subscriptions");
    state.subscriptions = data.subscriptions || [];
    const root = $("#subscription-list");
    if (!state.subscriptions.length) {
      root.innerHTML = '<p class="empty-state">没有可访问的 SIM / eSIM 订阅</p>';
    } else {
      root.innerHTML = state.subscriptions.map((sub) => {
        const name = sub.display_name || sub.carrier_name || `SUB ${sub.subscription_id}`;
        const kind = sub.embedded ? "eSIM" : "PHYSICAL SIM";
        return `<article class="subscription ${sub.selected ? "selected" : ""}" data-slot="${escapeHtml(String(sub.slot_index))}">
          <header><div><p class="section-no">${kind} / SLOT ${escapeHtml(String(sub.slot_index))}</p><h3>${escapeHtml(name)}</h3><p>${sub.active ? "ACTIVE" : "INACTIVE"} · subId ${escapeHtml(String(sub.subscription_id))}</p></div><span class="tag">${sub.selected ? "SELECTED" : kind}</span></header>
          <dl><div><dt>ICCID</dt><dd>${escapeHtml(sub.iccid || "—")}</dd></div><div><dt>号码</dt><dd>${escapeHtml(sub.msisdn || "—")}</dd></div><div><dt>IMEI</dt><dd>${escapeHtml(sub.imei || "—")}</dd></div><div><dt>默认用途</dt><dd>${[sub.default_data && "DATA", sub.default_sms && "SMS", sub.default_voice && "VOICE"].filter(Boolean).join(" / ") || "—"}</dd></div></dl>
          <footer><button class="secondary select-sub" data-id="${sub.subscription_id}" ${!sub.active || sub.selected ? "disabled" : ""}>设为当前订阅</button>${sub.embedded ? `<button class="ghost switch-esim" data-id="${sub.subscription_id}" data-port="${sub.port_index || 0}">切换 eSIM</button>` : ""}</footer>
        </article>`;
      }).join("");
    }
    const smsSelect = $("#sms-subscription");
    smsSelect.innerHTML = state.subscriptions.filter((sub) => sub.active).map((sub) =>
      `<option value="${sub.subscription_id}" ${sub.selected ? "selected" : ""}>${escapeHtml(sub.display_name || sub.carrier_name || `subId ${sub.subscription_id}`)}</option>`
    ).join("");
  }

  async function loadSms() {
    const data = await api("/api/sms?limit=100");
    const messages = data.messages || [];
    $("#sms-total").textContent = `${messages.length} 条`;
    $("#sms-list").innerHTML = messages.length ? messages.map((message) => {
      const incoming = Number(message.type) === 1;
      const peer = incoming ? message.sender : message.recipient;
      return `<article class="sms-item"><header><strong>${escapeHtml(peer || "未知号码")}</strong><time>${escapeHtml(formatDate(message.timestamp))}</time></header><p>${escapeHtml(message.content || "")}</p><span class="sms-direction">${incoming ? "INCOMING ↓" : "OUTGOING ↑"} · SUB ${escapeHtml(String(message.subscription_id))}</span><button class="sms-delete" data-id="${message.index}" title="删除">×</button></article>`;
    }).join("") : '<p class="empty-state">短信存储为空</p>';
  }

  async function agentAction(action) {
    try {
      await api(`/api/agent/${action}`, { method: "POST", body: {} });
      toast({ start: "正在启动上游连接", reconnect: "正在重新连接", stop: "上游连接已停止" }[action]);
      setTimeout(() => loadStatus(false), 700);
      setTimeout(() => loadConfig(), 700);
    } catch (error) { toast(error.message, true); }
  }

  function renderUrls(urls) {
    $("#management-urls").innerHTML = urls.length
      ? urls.map((url) => `<div class="url-item">${escapeHtml(url)}</div>`).join("")
      : '<p class="empty-state">尚未发现局域网地址</p>';
  }

  function bindEvents() {
    $("#login-form").addEventListener("submit", async (event) => {
      event.preventDefault();
      $("#login-error").textContent = "";
      try {
        const session = await api("/api/auth/login", { method: "POST", body: {
          username: $("#login-username").value.trim(), password: $("#login-password").value
        }});
        $("#login-password").value = "";
        showConsole(session);
      } catch (error) { $("#login-error").textContent = error.message; }
    });
    $("#logout").addEventListener("click", async () => {
      try { await api("/api/auth/logout", { method: "POST", body: {} }); } catch (_) {}
      showLogin();
    });
    $("#refresh-all").addEventListener("click", loadAll);
    $("#agent-start").addEventListener("click", () => agentAction("start"));
    $("#agent-reconnect").addEventListener("click", () => agentAction("reconnect"));
    $("#agent-stop").addEventListener("click", () => agentAction("stop"));
    $$(".nav-item").forEach((button) => button.addEventListener("click", () => selectView(button.dataset.view)));
    $("#refresh-telephony").addEventListener("click", async () => {
      try { await api("/api/telephony/refresh", { method: "POST", body: {} }); toast("射频状态正在刷新"); setTimeout(loadAll, 1200); }
      catch (error) { toast(error.message, true); }
    });
    $("#refresh-diagnostics").addEventListener("click", loadDiagnostics);
    $("#refresh-sms").addEventListener("click", () => loadSms().catch((error) => toast(error.message, true)));
    $("#subscription-list").addEventListener("click", handleSubscriptionClick);
    $("#sms-list").addEventListener("click", handleSmsClick);
    $("#sms-body").addEventListener("input", () => $("#sms-count").textContent = `${Array.from($("#sms-body").value).length} 字`);
    $("#sms-form").addEventListener("submit", sendSms);
    $("#config-form").addEventListener("submit", saveConfig);
    $("#password-form").addEventListener("submit", changePassword);
  }

  function selectView(name) {
    $$(".nav-item").forEach((item) => item.classList.toggle("active", item.dataset.view === name));
    $$(".view").forEach((view) => view.classList.toggle("active", view.id === `view-${name}`));
    if (name === "cellular") loadSubscriptions().catch((error) => toast(error.message, true));
    if (name === "messages") loadSms().catch((error) => toast(error.message, true));
    window.scrollTo({ top: 0, behavior: "smooth" });
  }

  async function handleSubscriptionClick(event) {
    const select = event.target.closest(".select-sub");
    const esim = event.target.closest(".switch-esim");
    try {
      if (select) {
        await api("/api/subscriptions/select", { method: "POST", body: { subscription_id: Number(select.dataset.id) }});
        toast("当前订阅已更新"); await loadAll();
      }
      if (esim) {
        if (!confirm("确认发起此 eSIM 切换？部分设备可能需要系统确认。")) return;
        await api("/api/esim/switch", { method: "POST", body: { subscription_id: Number(esim.dataset.id), port_index: Number(esim.dataset.port) }});
        toast("eSIM 切换请求已提交");
      }
    } catch (error) { toast(error.message, true); }
  }

  async function handleSmsClick(event) {
    const button = event.target.closest(".sms-delete");
    if (!button || !confirm("删除这条短信？")) return;
    try { await api(`/api/sms/${button.dataset.id}`, { method: "DELETE", body: {} }); toast("短信已删除"); await loadSms(); }
    catch (error) { toast(error.message, true); }
  }

  async function sendSms(event) {
    event.preventDefault();
    try {
      const result = await api("/api/sms/send", { method: "POST", body: {
        subscription_id: Number($("#sms-subscription").value), to: $("#sms-to").value.trim(), body: $("#sms-body").value
      }});
      toast(`短信已排队 · ${result.parts_total || 1} 段`);
      $("#sms-body").value = ""; $("#sms-count").textContent = "0 字";
      setTimeout(loadSms, 1400);
    } catch (error) { toast(error.message, true); }
  }

  async function saveConfig(event) {
    event.preventDefault();
    const payload = {
      server_url: $("#config-server").value.trim(), device_id: $("#config-device").value.trim(),
      agent_id: $("#config-agent").value.trim(), http_port: Number($("#config-port").value),
      auto_start: $("#config-autostart").checked, agent_enabled: $("#config-enabled").checked
    };
    if ($("#config-token").value.trim()) payload.pair_token = $("#config-token").value.trim();
    try {
      const config = await api("/api/config", { method: "PUT", body: payload });
      toast(config.web_restart_scheduled ? "配置已保存；管理站将在新端口重启" : "配置已保存");
      if (!config.web_restart_scheduled) await loadAll();
    } catch (error) { toast(error.message, true); }
  }

  async function changePassword(event) {
    event.preventDefault();
    const next = $("#security-new").value;
    if (next !== $("#security-confirm").value) { toast("两次输入的新密码不一致", true); return; }
    try {
      await api("/api/auth/password", { method: "PUT", body: {
        username: $("#security-username").value.trim(), current_password: $("#security-current").value, new_password: next
      }});
      $("#security-current").value = ""; $("#security-new").value = ""; $("#security-confirm").value = "";
      toast("凭据已更新，请重新登录"); setTimeout(showLogin, 800);
    } catch (error) { toast(error.message, true); }
  }

  async function loadDiagnostics() {
    const output = $("#diagnostics-output");
    output.textContent = "正在采集…";
    try { output.textContent = JSON.stringify(await api("/api/diagnostics"), null, 2); }
    catch (error) { output.textContent = error.message; toast(error.message, true); }
  }

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
    if (!value) return "未知";
    if (value === "connected") return "已连接";
    if (value === "stopped") return "已停止";
    if (value === "connecting") return "连接中";
    if (value.startsWith("reconnecting")) return "等待重连";
    if (value.startsWith("waiting")) return "等待配置";
    return "连接异常";
  }
  function value(input, fallback, suffix = "") { return input === undefined || input === null ? fallback : `${input}${suffix}`; }
  function formatDuration(ms) {
    const seconds = Math.floor(ms / 1000); const days = Math.floor(seconds / 86400); const hours = Math.floor((seconds % 86400) / 3600); const minutes = Math.floor((seconds % 3600) / 60);
    return days ? `${days}天 ${hours}时` : hours ? `${hours}时 ${minutes}分` : `${minutes}分`;
  }
  function formatDate(value) {
    if (!value) return "—";
    try { return new Intl.DateTimeFormat("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false }).format(new Date(value)); }
    catch (_) { return String(value); }
  }
  function escapeHtml(value) { return String(value).replace(/[&<>'"]/g, (char) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", "'": "&#39;", '"': "&quot;" }[char])); }

  bindEvents();
  restoreSession();
})();
