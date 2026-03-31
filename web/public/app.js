const S = {
  locale: localStorage.getItem("rana.locale") || "zh-CN",
  msg: {},
  supportedLocales: ["zh-CN", "en-US"],
  access: localStorage.getItem("rana.access") || "",
  refresh: localStorage.getItem("rana.refresh") || "",
  me: null,
  modules: {},
  tab: localStorage.getItem("rana.tab") || "dashboard",
  servers: [],
  policies: [],
  executions: [],
  schedules: [],
  users: [],
  audits: [],
  settings: null,
  logs: {},
  selectedExecution: localStorage.getItem("rana.selected_execution") || "",
  logFilters: {},
  executionFilter: {
    status: "",
    trigger: "",
    server: "",
  },
  stream: null,
  streamExecutionID: "",
};

const PERMISSIONS = {
  admin: new Set([
    "execution:read",
    "execution:write",
    "server:read",
    "server:write",
    "policy:read",
    "policy:write",
    "user:write",
    "settings:write",
    "audit:read",
  ]),
  operator: new Set([
    "execution:read",
    "execution:write",
    "server:read",
    "server:write",
    "policy:read",
    "policy:write",
  ]),
  viewer: new Set(["execution:read", "server:read", "policy:read"]),
};

const root = document.getElementById("root");
const BASE_PATH = resolveBasePath();
boot();

async function boot() {
  await loadLocale(S.locale);
  if (S.access) {
    try {
      await initSession();
    } catch {
      clearAuth();
    }
  }
  render();
}

async function initSession() {
  await me();
  await preload();
  if (S.selectedExecution && S.executions.some((item) => item.id === S.selectedExecution)) {
    await loadLogs(S.selectedExecution);
  }
}

function t(key) {
  return S.msg[key] || key;
}

function esc(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

function splitCSV(value) {
  return String(value || "")
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);
}

function uuid() {
  if (typeof crypto !== "undefined" && crypto.randomUUID) return crypto.randomUUID();
  return `k_${Date.now()}_${Math.random().toString(16).slice(2)}`;
}

function getCookie(name) {
  const cookies = document.cookie ? document.cookie.split(";") : [];
  for (const raw of cookies) {
    const entry = raw.trim();
    if (!entry) continue;
    const idx = entry.indexOf("=");
    const key = idx >= 0 ? entry.slice(0, idx) : entry;
    if (key === name) {
      return decodeURIComponent(idx >= 0 ? entry.slice(idx + 1) : "");
    }
  }
  return "";
}

function hasPerm(permission) {
  const role = String(S.me?.role || "").toLowerCase();
  const set = PERMISSIONS[role];
  return !!set && set.has(permission);
}

function canReadServers() {
  return hasPerm("server:read");
}

function canWriteServers() {
  return hasPerm("server:write");
}

function canReadPolicies() {
  return hasPerm("policy:read");
}

function canWritePolicies() {
  return hasPerm("policy:write");
}

function canReadExecutions() {
  return hasPerm("execution:read");
}

function canWriteExecutions() {
  return hasPerm("execution:write");
}

function canManageUsers() {
  return hasPerm("user:write");
}

function canReadAudit() {
  return hasPerm("audit:read");
}

function canWriteSettings() {
  return hasPerm("settings:write");
}

function navItems() {
  const items = [{ tab: "dashboard", key: "nav.dashboard", enabled: true }];
  items.push({ tab: "executions", key: "nav.executions", enabled: canReadExecutions() });
  items.push({ tab: "servers", key: "nav.servers", enabled: canReadServers() });
  items.push({ tab: "policies", key: "nav.policies", enabled: canReadPolicies() });
  items.push({ tab: "schedules", key: "nav.schedules", enabled: !!S.modules.schedule && canReadExecutions() });
  items.push({ tab: "users", key: "nav.users", enabled: !!S.modules.users && canManageUsers() });
  items.push({ tab: "audit", key: "nav.audit", enabled: !!S.modules.audit && canReadAudit() });
  items.push({ tab: "settings", key: "nav.settings", enabled: true });
  return items;
}

function ensureTab() {
  const active = navItems().find((item) => item.enabled && item.tab === S.tab);
  if (active) return;
  const fallback = navItems().find((item) => item.enabled);
  S.tab = fallback ? fallback.tab : "dashboard";
  localStorage.setItem("rana.tab", S.tab);
}

function localeOptions() {
  return S.supportedLocales
    .map((loc) => `<option value="${esc(loc)}" ${loc === S.locale ? "selected" : ""}>${esc(loc)}</option>`)
    .join("");
}

function resolveBasePath() {
  const { pathname } = window.location;
  const apiIndex = pathname.indexOf("/api/");
  if (apiIndex > 0) return pathname.slice(0, apiIndex);
  if (pathname === "/" || !pathname) return "";
  const normalized = pathname.endsWith("/") ? pathname.slice(0, -1) : pathname;
  const lastSlash = normalized.lastIndexOf("/");
  if (lastSlash <= 0) return "";
  return normalized.slice(0, lastSlash);
}

function withBasePath(path) {
  if (!path) return BASE_PATH || "";
  if (/^https?:\/\//i.test(path)) return path;
  const normalized = path.startsWith("/") ? path : `/${path}`;
  return `${BASE_PATH}${normalized}`;
}

async function loadLocale(locale) {
  const selected = locale || "zh-CN";
  const res = await fetch(withBasePath(`/locales/${encodeURIComponent(selected)}.json`), { cache: "no-store" });
  if (!res.ok) throw new Error(`locale ${selected} not found`);
  S.msg = await res.json();
  S.locale = selected;
  localStorage.setItem("rana.locale", selected);
}

function setAuth(access, refresh) {
  S.access = access || "";
  S.refresh = refresh || "";
  if (S.access) localStorage.setItem("rana.access", S.access);
  else localStorage.removeItem("rana.access");
  if (S.refresh) localStorage.setItem("rana.refresh", S.refresh);
  else localStorage.removeItem("rana.refresh");
}

function clearAuth() {
  setAuth("", "");
  S.me = null;
  S.modules = {};
  S.servers = [];
  S.policies = [];
  S.executions = [];
  S.schedules = [];
  S.users = [];
  S.audits = [];
  S.settings = null;
  S.logs = {};
  S.selectedExecution = "";
  S.logFilters = {};
  S.executionFilter = { status: "", trigger: "", server: "" };
  S.supportedLocales = ["zh-CN", "en-US"];
  localStorage.removeItem("rana.selected_execution");
  closeStream();
}

async function api(path, method = "GET", body, auth = true, retry = true) {
  const upperMethod = String(method || "GET").toUpperCase();
  const headers = { Accept: "application/json" };
  if (body !== undefined) headers["Content-Type"] = "application/json";
  if (auth && S.access) headers.Authorization = `Bearer ${S.access}`;

  if (!["GET", "HEAD", "OPTIONS"].includes(upperMethod)) {
    const csrf = getCookie("rana_csrf");
    if (csrf) headers["X-CSRF-Token"] = csrf;
    headers["Idempotency-Key"] = uuid();
  }

  const res = await fetch(withBasePath(path), {
    method: upperMethod,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  });

  if (res.status === 401 && auth && retry && S.refresh) {
    await refreshToken();
    return api(path, method, body, auth, false);
  }

  const payload = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(payload.message || payload.code || `${res.status}`);
  }
  return payload;
}

async function refreshToken() {
  const payload = await api("/api/v1/auth/refresh", "POST", { refresh_token: S.refresh }, false, false);
  setAuth(payload.data?.access_token, payload.data?.refresh_token);
}

async function me() {
  const payload = await api("/api/v1/me");
  S.me = payload.data || null;
}

async function preload() {
  const modulesPayload = await api("/api/v1/modules");
  S.modules = modulesPayload.data || {};

  try {
    const localesPayload = await api("/api/v1/i18n/locales");
    if (Array.isArray(localesPayload.data) && localesPayload.data.length > 0) {
      S.supportedLocales = localesPayload.data;
    }
  } catch {
    // keep default locale options
  }

  if (canReadServers()) {
    try {
      const serversPayload = await api("/api/v1/servers?page=1&page_size=200");
      S.servers = serversPayload.data || [];
    } catch {
      S.servers = [];
    }
  }

  if (canReadPolicies()) {
    try {
      const policyPayload = await api("/api/v1/policies?page=1&page_size=200");
      S.policies = policyPayload.data || [];
    } catch {
      S.policies = [];
    }
  }

  if (canReadExecutions()) {
    await fetchExecutions();
  }

  if (S.modules.schedule && canReadExecutions()) {
    try {
      const schedulePayload = await api("/api/v1/schedules?page=1&page_size=200");
      S.schedules = schedulePayload.data || [];
    } catch {
      S.schedules = [];
    }
  } else {
    S.schedules = [];
  }

  if (S.modules.users && canManageUsers()) {
    try {
      const userPayload = await api("/api/v1/users?page=1&page_size=200");
      S.users = userPayload.data || [];
    } catch {
      S.users = [];
    }
  } else {
    S.users = [];
  }

  if (S.modules.audit && canReadAudit()) {
    try {
      const auditPayload = await api("/api/v1/audit-logs?page=1&page_size=200");
      S.audits = auditPayload.data || [];
    } catch {
      S.audits = [];
    }
  } else {
    S.audits = [];
  }

  try {
    const settingsPayload = await api("/api/v1/settings");
    S.settings = settingsPayload.data || null;
  } catch {
    S.settings = null;
  }

  if (S.selectedExecution && !S.executions.some((item) => item.id === S.selectedExecution)) {
    closeStream();
    S.selectedExecution = "";
    localStorage.removeItem("rana.selected_execution");
  }
}

async function fetchExecutions() {
  try {
    const url = new URL("/api/v1/executions", location.origin);
    url.searchParams.set("page", "1");
    url.searchParams.set("page_size", "200");
    if (S.executionFilter.status) url.searchParams.set("status", S.executionFilter.status);
    if (S.executionFilter.trigger) url.searchParams.set("trigger_type", S.executionFilter.trigger);
    if (S.executionFilter.server) url.searchParams.set("server", S.executionFilter.server);
    const executionPayload = await api(`${url.pathname}${url.search}`);
    S.executions = executionPayload.data || [];
  } catch {
    S.executions = [];
  }
}

function render() {
  if (!S.access) {
    renderLogin();
    return;
  }

  ensureTab();
  const items = navItems();
  root.innerHTML = `
    <div class="shell">
      <div class="app-grid">
        <aside class="card sidebar">
          <h2 class="side-title">${esc(t("app.title"))}</h2>
          <p class="side-sub">${esc(t("app.subtitle"))}</p>
          <div class="nav-list">
            ${items
              .map(
                (item) =>
                  `<button class="nav-btn ${S.tab === item.tab ? "active" : ""}" data-tab="${esc(item.tab)}" ${
                    item.enabled ? "" : "disabled"
                  }>${esc(t(item.key))}</button>`,
              )
              .join("")}
          </div>
        </aside>
        <main class="card content">
          <div class="topbar">
            <div>
              <h3 class="section-title">${esc(t(`nav.${S.tab}`))}</h3>
              <p class="section-sub">${esc(S.me?.username || "")} (${esc(S.me?.role || "-")})</p>
            </div>
            <div class="top-actions">
              <button id="refresh-btn" class="btn-ghost">${esc(t("action.refresh"))}</button>
              <select id="locale-select">${localeOptions()}</select>
              <button id="logout-btn" class="btn-danger">${esc(t("auth.signout"))}</button>
            </div>
          </div>
          <div id="panel">${panel()}</div>
        </main>
      </div>
    </div>
    <div id="toast" class="toast"></div>
  `;
  bindShell();
}

function renderLogin() {
  root.innerHTML = `
    <div class="auth-wrap">
      <section class="card auth-card">
        <h1 class="brand">${esc(t("app.title"))}</h1>
        <p class="subtitle">${esc(t("app.subtitle"))}</p>
        <form id="login-form">
          <div class="field">
            <label>${esc(t("auth.username"))}</label>
            <input name="username" required />
          </div>
          <div class="field">
            <label>${esc(t("auth.password"))}</label>
            <input name="password" type="password" required />
          </div>
          <div class="btn-row">
            <button type="submit" class="btn-primary">${esc(t("auth.signin"))}</button>
            <select id="login-locale">${localeOptions()}</select>
          </div>
          <div id="err" class="error"></div>
        </form>
      </section>
    </div>
    <div id="toast" class="toast"></div>
  `;

  document.getElementById("login-locale")?.addEventListener("change", async (event) => {
    await loadLocale(event.target.value);
    renderLogin();
  });

  document.getElementById("login-form")?.addEventListener("submit", async (event) => {
    event.preventDefault();
    const formData = new FormData(event.currentTarget);
    const username = String(formData.get("username") || "").trim();
    const password = String(formData.get("password") || "");

    try {
      const payload = await api("/api/v1/auth/login", "POST", { username, password }, false);
      setAuth(payload.data?.access_token, payload.data?.refresh_token);
      await initSession();
      render();
    } catch (err) {
      document.getElementById("err").textContent = err.message;
    }
  });
}

function bindShell() {
  document.querySelectorAll(".nav-btn").forEach((button) => {
    button.addEventListener("click", () => {
      const next = button.dataset.tab;
      if (!next || next === S.tab) return;
      if (S.tab === "executions" && next !== "executions") closeStream();
      S.tab = next;
      localStorage.setItem("rana.tab", S.tab);
      render();
    });
  });

  document.getElementById("refresh-btn")?.addEventListener("click", async () => {
    try {
      await preload();
      toast(t("common.refreshed"));
      render();
    } catch (err) {
      toast(err.message, true);
    }
  });

  document.getElementById("logout-btn")?.addEventListener("click", async () => {
    try {
      await api("/api/v1/auth/logout", "POST");
    } catch {
      // ignore logout error
    }
    clearAuth();
    render();
  });

  document.getElementById("locale-select")?.addEventListener("change", async (event) => {
    const locale = event.target.value;
    try {
      await loadLocale(locale);
      if (S.access) {
        await api("/api/v1/me/locale", "PUT", { locale: S.locale });
      }
      render();
    } catch (err) {
      toast(err.message, true);
    }
  });

  bindPanelActions();
}

function panel() {
  switch (S.tab) {
    case "servers":
      return serverPanel();
    case "policies":
      return policyPanel();
    case "executions":
      return executionPanel();
    case "schedules":
      return schedulePanel();
    case "users":
      return userPanel();
    case "audit":
      return auditPanel();
    case "settings":
      return settingsPanel();
    default:
      return dashboardPanel();
  }
}

function dashboardPanel() {
  const total = S.executions.length;
  const active = S.executions.filter((item) => ["pending", "running", "retrying"].includes(item.status)).length;
  const success = S.executions.filter((item) => item.status === "success").length;
  const failed = S.executions.filter((item) => ["failed", "canceled"].includes(item.status)).length;
  const successRate = total > 0 ? Math.round((success / total) * 100) : 0;
  const recent = S.executions.slice(0, 8);

  return `
    <section>
      <div class="stats">
        <article class="stat-card"><div class="stat-label">${esc(t("dashboard.total"))}</div><div class="stat-value">${total}</div></article>
        <article class="stat-card"><div class="stat-label">${esc(t("dashboard.running"))}</div><div class="stat-value">${active}</div></article>
        <article class="stat-card"><div class="stat-label">${esc(t("dashboard.failed"))}</div><div class="stat-value">${failed}</div></article>
        <article class="stat-card"><div class="stat-label">${esc(t("dashboard.success_rate"))}</div><div class="stat-value">${successRate}%</div></article>
      </div>
      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th>${esc(t("common.id"))}</th>
              <th>${esc(t("common.result"))}</th>
              <th>${esc(t("common.trigger"))}</th>
              <th>${esc(t("common.server_names"))}</th>
              <th>${esc(t("common.started_at"))}</th>
            </tr>
          </thead>
          <tbody>
            ${recent
              .map(
                (item) => `<tr>
                  <td class="mono">${esc((item.id || "").slice(0, 8))}</td>
                  <td>${statusTag(item.status)}</td>
                  <td>${esc(item.trigger_type || "-")}</td>
                  <td>${esc((item.server_names || []).join(", "))}</td>
                  <td>${esc(formatTime(item.started_at))}</td>
                </tr>`,
              )
              .join("") || `<tr><td colspan="5" class="muted">${esc(t("common.no_data"))}</td></tr>`}
          </tbody>
        </table>
      </div>
    </section>
  `;
}

function serverPanel() {
  if (!canReadServers()) {
    return `<section><p class="muted">${esc(t("common.no_permission"))}</p></section>`;
  }

  const form = canWriteServers()
    ? `
      <form id="server-form" class="grid-3">
        <input type="hidden" name="id" />
        <input name="name" placeholder="${esc(t("servers.name"))}" required />
        <input name="host" placeholder="${esc(t("servers.host"))}" required />
        <input name="port" type="number" value="22" min="1" max="65535" required />
        <input name="user" placeholder="${esc(t("servers.user"))}" required />
        <input name="key_path" placeholder="${esc(t("servers.key_path"))}" required />
        <input name="passphrase" placeholder="${esc(t("servers.passphrase"))}" />
        <input name="tags" placeholder="${esc(t("servers.tags"))}" />
        <input name="paths" placeholder="${esc(t("servers.paths"))}" required />
        <input name="rclone_remote" placeholder="${esc(t("servers.rclone_remote"))}" required />
        <input name="rclone_flags" placeholder="${esc(t("servers.rclone_flags"))}" />
        <label class="checkbox-inline"><input type="checkbox" name="enabled" checked /> ${esc(t("common.enabled"))}</label>
        <div class="btn-row">
          <button class="btn-primary" type="submit">${esc(t("action.save"))}</button>
          <button class="btn-ghost" type="button" id="server-reset">${esc(t("action.reset"))}</button>
        </div>
      </form>
    `
    : "";

  return `
    <section>
      ${form}
      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th>${esc(t("servers.name"))}</th>
              <th>${esc(t("servers.host"))}</th>
              <th>${esc(t("servers.user"))}</th>
              <th>${esc(t("servers.paths"))}</th>
              <th>${esc(t("common.enabled"))}</th>
              <th>${esc(t("common.actions"))}</th>
            </tr>
          </thead>
          <tbody>
            ${S.servers
              .map(
                (srv) => `<tr>
                  <td>${esc(srv.name)}</td>
                  <td class="mono">${esc(srv.host)}:${esc(srv.port)}</td>
                  <td>${esc(srv.user)}</td>
                  <td class="mono">${esc((srv.paths || []).join(", "))}</td>
                  <td>${srv.enabled ? esc(t("common.on")) : esc(t("common.off"))}</td>
                  <td>
                    <div class="btn-row">
                      <button class="btn-ghost server-test" data-id="${esc(srv.id)}">${esc(t("action.test_connection"))}</button>
                      ${
                        canWriteServers()
                          ? `<button class="btn-ghost server-edit" data-id="${esc(srv.id)}">${esc(t("action.edit"))}</button>
                             <button class="btn-danger server-del" data-id="${esc(srv.id)}">${esc(t("action.delete"))}</button>`
                          : ""
                      }
                    </div>
                  </td>
                </tr>`,
              )
              .join("") || `<tr><td colspan="6" class="muted">${esc(t("common.no_data"))}</td></tr>`}
          </tbody>
        </table>
      </div>
    </section>
  `;
}

function policyPanel() {
  if (!canReadPolicies()) {
    return `<section><p class="muted">${esc(t("common.no_permission"))}</p></section>`;
  }

  const serverOptions = S.servers
    .filter((srv) => srv.enabled)
    .map((srv) => `<option value="${esc(srv.name)}">${esc(srv.name)}</option>`)
    .join("");
  const exFilter = S.executionFilter;

  const form = canWritePolicies()
    ? `
      <form id="policy-form" class="grid-3">
        <input type="hidden" name="id" />
        <input name="name" placeholder="${esc(t("policies.name"))}" required />
        <select name="server_names" multiple required>${serverOptions}</select>
        <input name="paths" placeholder="${esc(t("policies.paths"))}" required />
        <input name="rclone_remote" placeholder="${esc(t("policies.rclone_remote"))}" required />
        <input name="rclone_flags" placeholder="${esc(t("policies.rclone_flags"))}" />
        <input type="number" name="timeout_sec" min="0" value="0" placeholder="${esc(t("policies.timeout_sec"))}" />
        <input type="number" name="retry_limit" min="0" value="0" placeholder="${esc(t("policies.retry_limit"))}" />
        <label class="checkbox-inline"><input type="checkbox" name="enabled" checked /> ${esc(t("common.enabled"))}</label>
        <div class="btn-row">
          <button class="btn-primary" type="submit">${esc(t("action.save"))}</button>
          <button class="btn-ghost" type="button" id="policy-reset">${esc(t("action.reset"))}</button>
        </div>
      </form>
    `
    : "";

  return `
    <section>
      ${form}
      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th>${esc(t("policies.name"))}</th>
              <th>${esc(t("common.server_names"))}</th>
              <th>${esc(t("policies.rclone_remote"))}</th>
              <th>${esc(t("policies.timeout_sec"))}</th>
              <th>${esc(t("policies.retry_limit"))}</th>
              <th>${esc(t("common.enabled"))}</th>
              <th>${esc(t("common.actions"))}</th>
            </tr>
          </thead>
          <tbody>
            ${S.policies
              .map(
                (policy) => `<tr>
                  <td>${esc(policy.name)}</td>
                  <td>${esc((policy.server_names || []).join(", "))}</td>
                  <td class="mono">${esc(policy.rclone_remote)}</td>
                  <td>${esc(policy.timeout_sec || 0)}</td>
                  <td>${esc(policy.retry_limit || 0)}</td>
                  <td>${policy.enabled ? esc(t("common.on")) : esc(t("common.off"))}</td>
                  <td>
                    <div class="btn-row">
                      ${
                        canWriteExecutions()
                          ? `<button class="btn-primary policy-run" data-id="${esc(policy.id)}">${esc(t("action.run"))}</button>`
                          : ""
                      }
                      ${
                        canWritePolicies()
                          ? `<button class="btn-ghost policy-edit" data-id="${esc(policy.id)}">${esc(t("action.edit"))}</button>
                             <button class="btn-danger policy-del" data-id="${esc(policy.id)}">${esc(t("action.delete"))}</button>`
                          : ""
                      }
                    </div>
                  </td>
                </tr>`,
              )
              .join("") || `<tr><td colspan="7" class="muted">${esc(t("common.no_data"))}</td></tr>`}
          </tbody>
        </table>
      </div>
    </section>
  `;
}

function executionPanel() {
  if (!canReadExecutions()) {
    return `<section><p class="muted">${esc(t("common.no_permission"))}</p></section>`;
  }

  const activePolicyOptions = S.policies
    .filter((item) => item.enabled)
    .map((item) => `<option value="${esc(item.id)}">${esc(item.name)}</option>`)
    .join("");
  const serverOptions = S.servers
    .filter((srv) => srv.enabled)
    .map((srv) => `<option value="${esc(srv.name)}">${esc(srv.name)}</option>`)
    .join("");

  const rows = S.executions
    .map((item) => {
      const canCancel = canWriteExecutions() && ["pending", "running", "retrying"].includes(item.status);
      const canRetry = canWriteExecutions() && !["pending", "running", "retrying"].includes(item.status);
      return `<tr>
        <td class="mono">${esc((item.id || "").slice(0, 8))}</td>
        <td>${statusTag(item.status)}</td>
        <td>${esc(item.trigger_type || "-")}</td>
        <td>${esc(item.policy_id || "-")}</td>
        <td>${esc((item.server_names || []).join(", "))}</td>
        <td>${esc(formatTime(item.started_at))}</td>
        <td>${esc(item.duration_ms || 0)}</td>
        <td class="mono">${esc(item.error || "-")}</td>
        <td>
          <div class="btn-row">
            <button class="btn-ghost ex-log" data-id="${esc(item.id)}">${esc(t("executions.logs"))}</button>
            ${canCancel ? `<button class="btn-warn ex-cancel" data-id="${esc(item.id)}">${esc(t("action.cancel"))}</button>` : ""}
            ${canRetry ? `<button class="btn-primary ex-retry" data-id="${esc(item.id)}">${esc(t("action.retry"))}</button>` : ""}
          </div>
        </td>
      </tr>`;
    })
    .join("");

  const filters = S.logFilters[S.selectedExecution] || { level: "", contains: "" };
  const logText = (S.logs[S.selectedExecution] || [])
    .map((line) => `[${formatTime(line.timestamp)}] [${line.level}] ${line.line}`)
    .join("\n");

  return `
    <section>
      <form id="execution-filter-form" class="grid-3">
        <select name="status">
          <option value="">${esc(t("executions.filter_all_status"))}</option>
          <option value="pending" ${exFilter.status === "pending" ? "selected" : ""}>${esc(t("status.pending"))}</option>
          <option value="running" ${exFilter.status === "running" ? "selected" : ""}>${esc(t("status.running"))}</option>
          <option value="retrying" ${exFilter.status === "retrying" ? "selected" : ""}>${esc(t("status.retrying"))}</option>
          <option value="success" ${exFilter.status === "success" ? "selected" : ""}>${esc(t("status.success"))}</option>
          <option value="failed" ${exFilter.status === "failed" ? "selected" : ""}>${esc(t("status.failed"))}</option>
          <option value="canceled" ${exFilter.status === "canceled" ? "selected" : ""}>${esc(t("status.canceled"))}</option>
        </select>
        <select name="trigger">
          <option value="">${esc(t("executions.filter_all_trigger"))}</option>
          <option value="manual" ${exFilter.trigger === "manual" ? "selected" : ""}>manual</option>
          <option value="schedule" ${exFilter.trigger === "schedule" ? "selected" : ""}>schedule</option>
          <option value="retry" ${exFilter.trigger === "retry" ? "selected" : ""}>retry</option>
        </select>
        <div class="btn-row">
          <select name="server">
            <option value="">${esc(t("executions.filter_all_server"))}</option>
            ${S.servers.map((srv) => `<option value="${esc(srv.name)}" ${exFilter.server === srv.name ? "selected" : ""}>${esc(srv.name)}</option>`).join("")}
          </select>
          <button class="btn-ghost" type="submit">${esc(t("action.apply"))}</button>
          <button class="btn-ghost" type="button" id="execution-filter-reset">${esc(t("action.reset"))}</button>
        </div>
      </form>

      ${
        canWriteExecutions()
          ? `<div class="panel-grid">
              <form id="run-policy-form" class="card mini-card">
                <h4>${esc(t("executions.run_by_policy"))}</h4>
                <select name="policy_id">
                  <option value="">${esc(t("executions.select_policy"))}</option>
                  ${activePolicyOptions}
                </select>
                <button class="btn-primary" type="submit">${esc(t("action.run"))}</button>
              </form>
              <form id="run-server-form" class="card mini-card">
                <h4>${esc(t("executions.run_by_servers"))}</h4>
                <select name="server_names" multiple>${serverOptions}</select>
                <button class="btn-primary" type="submit">${esc(t("action.run"))}</button>
              </form>
            </div>`
          : ""
      }

      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th>${esc(t("common.id"))}</th>
              <th>${esc(t("common.result"))}</th>
              <th>${esc(t("common.trigger"))}</th>
              <th>${esc(t("common.policy"))}</th>
              <th>${esc(t("common.server_names"))}</th>
              <th>${esc(t("common.started_at"))}</th>
              <th>${esc(t("common.duration_ms"))}</th>
              <th>${esc(t("common.error"))}</th>
              <th>${esc(t("common.actions"))}</th>
            </tr>
          </thead>
          <tbody>
            ${rows || `<tr><td colspan="9" class="muted">${esc(t("common.no_data"))}</td></tr>`}
          </tbody>
        </table>
      </div>

      <div class="log-panel">
        <div class="log-head split">
          <strong>${esc(t("executions.logs"))}: ${esc(S.selectedExecution ? S.selectedExecution.slice(0, 8) : "-")}</strong>
          <div class="btn-row">
            <button id="load-logs" class="btn-ghost">${esc(t("action.refresh"))}</button>
            <button id="download-logs" class="btn-ghost">${esc(t("action.download"))}</button>
            <button id="stream-toggle" class="btn-ghost">${esc(S.stream ? t("executions.close_stream") : t("executions.open_stream"))}</button>
          </div>
        </div>
        <div class="log-head">
          <select id="log-level-filter">
            <option value="" ${filters.level === "" ? "selected" : ""}>${esc(t("logs.level_all"))}</option>
            <option value="INFO" ${filters.level === "INFO" ? "selected" : ""}>INFO</option>
            <option value="WARN" ${filters.level === "WARN" ? "selected" : ""}>WARN</option>
            <option value="ERROR" ${filters.level === "ERROR" ? "selected" : ""}>ERROR</option>
          </select>
          <input id="log-contains-filter" value="${esc(filters.contains || "")}" placeholder="${esc(t("logs.contains"))}" />
        </div>
        <pre id="log-out" class="log-body">${esc(logText || t("executions.select_execution_for_logs"))}</pre>
      </div>
    </section>
  `;
}

function schedulePanel() {
  if (!S.modules.schedule) {
    return `<section><p class="muted">${esc(t("feature_disabled"))}</p></section>`;
  }
  if (!canReadExecutions()) {
    return `<section><p class="muted">${esc(t("common.no_permission"))}</p></section>`;
  }

  const policyOptions = S.policies
    .filter((item) => item.enabled)
    .map((item) => `<option value="${esc(item.id)}">${esc(item.name)}</option>`)
    .join("");

  const form = canWriteExecutions()
    ? `
      <form id="schedule-form" class="grid-3">
        <input type="hidden" name="id" />
        <input name="name" placeholder="${esc(t("schedules.name"))}" required />
        <input name="cron_expr" placeholder="${esc(t("schedules.cron"))}" required />
        <input name="timezone" value="UTC" placeholder="${esc(t("schedules.timezone"))}" />
        <select name="policy_id">
          <option value="">${esc(t("schedules.policy_optional"))}</option>
          ${policyOptions}
        </select>
        <input name="server_names" placeholder="${esc(t("schedules.server_names_hint"))}" />
        <select name="misfire_policy">
          <option value="run_once">run_once</option>
          <option value="skip">skip</option>
        </select>
        <label class="checkbox-inline"><input type="checkbox" name="enabled" checked /> ${esc(t("common.enabled"))}</label>
        <div class="btn-row">
          <button class="btn-primary" type="submit">${esc(t("action.save"))}</button>
          <button class="btn-ghost" type="button" id="schedule-reset">${esc(t("action.reset"))}</button>
        </div>
      </form>
    `
    : "";

  return `
    <section>
      ${form}
      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th>${esc(t("schedules.name"))}</th>
              <th>${esc(t("schedules.cron"))}</th>
              <th>${esc(t("common.policy"))}</th>
              <th>${esc(t("common.server_names"))}</th>
              <th>${esc(t("schedules.next_run"))}</th>
              <th>${esc(t("common.enabled"))}</th>
              <th>${esc(t("common.actions"))}</th>
            </tr>
          </thead>
          <tbody>
            ${S.schedules
              .map(
                (item) => `<tr>
                  <td>${esc(item.name)}</td>
                  <td class="mono">${esc(item.cron_expr)}</td>
                  <td class="mono">${esc(item.policy_id || "-")}</td>
                  <td>${esc((item.server_names || []).join(", "))}</td>
                  <td>${esc(formatTime(item.next_run_at))}</td>
                  <td>${item.enabled ? esc(t("common.on")) : esc(t("common.off"))}</td>
                  <td>
                    <div class="btn-row">
                      ${
                        canWriteExecutions()
                          ? `<button class="btn-ghost sch-edit" data-id="${esc(item.id)}">${esc(t("action.edit"))}</button>
                             <button class="btn-danger sch-del" data-id="${esc(item.id)}">${esc(t("action.delete"))}</button>`
                          : ""
                      }
                    </div>
                  </td>
                </tr>`,
              )
              .join("") || `<tr><td colspan="7" class="muted">${esc(t("common.no_data"))}</td></tr>`}
          </tbody>
        </table>
      </div>
    </section>
  `;
}

function userPanel() {
  if (!S.modules.users) {
    return `<section><p class="muted">${esc(t("feature_disabled"))}</p></section>`;
  }
  if (!canManageUsers()) {
    return `<section><p class="muted">${esc(t("common.no_permission"))}</p></section>`;
  }

  return `
    <section>
      <form id="user-form" class="grid-3">
        <input type="hidden" name="id" />
        <input name="username" placeholder="${esc(t("users.username"))}" required />
        <select name="role" required>
          <option value="admin">admin</option>
          <option value="operator">operator</option>
          <option value="viewer">viewer</option>
        </select>
        <select name="locale" required>${localeOptions()}</select>
        <input name="password" type="password" placeholder="${esc(t("users.password"))}" />
        <div class="btn-row">
          <button class="btn-primary" type="submit">${esc(t("action.save"))}</button>
          <button class="btn-ghost" type="button" id="user-reset">${esc(t("action.reset"))}</button>
        </div>
      </form>

      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th>${esc(t("users.username"))}</th>
              <th>${esc(t("users.role"))}</th>
              <th>${esc(t("users.locale"))}</th>
              <th>${esc(t("users.last_login"))}</th>
              <th>${esc(t("common.created_at"))}</th>
              <th>${esc(t("common.actions"))}</th>
            </tr>
          </thead>
          <tbody>
            ${S.users
              .map(
                (user) => `<tr>
                  <td>${esc(user.username)}</td>
                  <td>${esc(user.role)}</td>
                  <td>${esc(user.locale || "-")}</td>
                  <td>${esc(formatTime(user.last_login_at))}</td>
                  <td>${esc(formatTime(user.created_at))}</td>
                  <td>
                    <div class="btn-row">
                      <button class="btn-ghost user-edit" data-id="${esc(user.id)}">${esc(t("action.edit"))}</button>
                      <button class="btn-ghost user-pass" data-id="${esc(user.id)}">${esc(t("users.reset_password"))}</button>
                      <button class="btn-danger user-del" data-id="${esc(user.id)}">${esc(t("action.delete"))}</button>
                    </div>
                  </td>
                </tr>`,
              )
              .join("") || `<tr><td colspan="6" class="muted">${esc(t("common.no_data"))}</td></tr>`}
          </tbody>
        </table>
      </div>
    </section>
  `;
}

function auditPanel() {
  if (!S.modules.audit) {
    return `<section><p class="muted">${esc(t("feature_disabled"))}</p></section>`;
  }
  if (!canReadAudit()) {
    return `<section><p class="muted">${esc(t("common.no_permission"))}</p></section>`;
  }

  return `
    <section>
      <div class="btn-row">
        <button id="audit-download" class="btn-ghost">${esc(t("action.download"))}</button>
      </div>
      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th>${esc(t("common.created_at"))}</th>
              <th>${esc(t("audit.actor"))}</th>
              <th>${esc(t("audit.action"))}</th>
              <th>${esc(t("audit.resource"))}</th>
              <th>${esc(t("audit.ip"))}</th>
              <th>${esc(t("audit.diff"))}</th>
            </tr>
          </thead>
          <tbody>
            ${S.audits
              .map(
                (item) => `<tr>
                  <td>${esc(formatTime(item.timestamp))}</td>
                  <td class="mono">${esc(item.actor_id || "-")}</td>
                  <td class="mono">${esc(item.action || "-")}</td>
                  <td class="mono">${esc(`${item.resource_type || "-"}:${item.resource_id || "-"}`)}</td>
                  <td class="mono">${esc(item.ip || "-")}</td>
                  <td class="mono">${esc(item.diff || "-")}</td>
                </tr>`,
              )
              .join("") || `<tr><td colspan="6" class="muted">${esc(t("common.no_data"))}</td></tr>`}
          </tbody>
        </table>
      </div>
    </section>
  `;
}

function settingsPanel() {
  const currentLocale = S.me?.locale || S.locale;
  const meCard = `
    <section class="card mini-card">
      <h4>${esc(t("settings.profile"))}</h4>
      <p class="mono">${esc(t("users.username"))}: ${esc(S.me?.username || "-")}</p>
      <p class="mono">${esc(t("users.role"))}: ${esc(S.me?.role || "-")}</p>
      <p class="mono">${esc(t("users.locale"))}: ${esc(currentLocale)}</p>
      <form id="me-password-form" class="stack">
        <input name="old_password" type="password" placeholder="${esc(t("users.old_password"))}" required />
        <input name="new_password" type="password" placeholder="${esc(t("users.new_password"))}" required />
        <button class="btn-primary" type="submit">${esc(t("users.change_my_password"))}</button>
      </form>
    </section>
  `;

  if (!canWriteSettings()) {
    return `<section>${meCard}</section>`;
  }

  const cfg = S.settings || {};
  const global = cfg.global || {};
  const web = cfg.web || {};
  const modules = cfg.modules || {};
  const i18n = cfg.i18n || {};
  const notify = cfg.notify || {};
  const email = notify.email || {};

  return `
    <section>
      ${meCard}
      <form id="settings-form" class="grid-3">
        <input name="timeout" value="${esc(global.timeout || "")}" placeholder="${esc(t("settings.timeout"))}" />
        <input name="concurrency" type="number" value="${esc(global.concurrency ?? 0)}" min="0" placeholder="${esc(t("settings.concurrency"))}" />
        <label class="checkbox-inline"><input type="checkbox" name="strict_host_key" ${global.ssh?.strict_host_key ? "checked" : ""} /> ${esc(t("settings.strict_host_key"))}</label>
        <input name="known_hosts_path" value="${esc(global.ssh?.known_hosts_path || "")}" placeholder="${esc(t("settings.known_hosts_path"))}" />
        <label class="checkbox-inline"><input type="checkbox" name="csrf_enabled" ${web.csrf_enabled ? "checked" : ""} /> ${esc(t("settings.csrf_enabled"))}</label>
        <input name="cors_allow_origins" value="${esc((web.cors_allow_origins || []).join(", "))}" placeholder="${esc(t("settings.cors_allow_origins"))}" />
        <input name="ip_allow_list" value="${esc((web.ip_allow_list || []).join(", "))}" placeholder="${esc(t("settings.ip_allow_list"))}" />
        <select name="default_locale">${S.supportedLocales
          .map(
            (loc) =>
              `<option value="${esc(loc)}" ${i18n.default_locale === loc ? "selected" : ""}>${esc(loc)}</option>`,
          )
          .join("")}</select>
        <input name="notify_webhook_url" value="${esc(notify.webhook_url || "")}" placeholder="${esc(t("settings.notify_webhook_url"))}" />
        <label class="checkbox-inline"><input type="checkbox" name="notify_email_enabled" ${email.enabled ? "checked" : ""} /> Email</label>
        <input name="notify_email_smtp_host" value="${esc(email.smtp_host || "")}" placeholder="SMTP host" />
        <input name="notify_email_smtp_port" type="number" min="0" value="${esc(email.smtp_port ?? 0)}" placeholder="SMTP port" />
        <input name="notify_email_username" value="${esc(email.username || "")}" placeholder="SMTP username" />
        <input name="notify_email_password" type="password" value="${esc(email.password || "")}" placeholder="SMTP password" />
        <input name="notify_email_from" value="${esc(email.from || "")}" placeholder="From email" />
        <input name="notify_email_to" value="${esc((email.to || []).join(", "))}" placeholder="To emails (comma separated)" />
        <label class="checkbox-inline"><input type="checkbox" name="notify_email_use_tls" ${email.use_tls ? "checked" : ""} /> SMTPS/TLS</label>
        <label class="checkbox-inline"><input type="checkbox" name="notify_on_success" ${notify.on_success ? "checked" : ""} /> ${esc(t("settings.notify_on_success"))}</label>
        <label class="checkbox-inline"><input type="checkbox" name="notify_on_failure" ${notify.on_failure ? "checked" : ""} /> ${esc(t("settings.notify_on_failure"))}</label>
        <input name="notify_suppression_window" value="${esc(notify.suppression_window || "0s")}" placeholder="${esc(t("settings.notify_suppression_window"))}" />
        <label class="checkbox-inline"><input type="checkbox" name="schedule" ${modules.schedule ? "checked" : ""} /> ${esc(t("nav.schedules"))}</label>
        <label class="checkbox-inline"><input type="checkbox" name="audit" ${modules.audit ? "checked" : ""} /> ${esc(t("nav.audit"))}</label>
        <label class="checkbox-inline"><input type="checkbox" name="notify" ${modules.notify ? "checked" : ""} /> ${esc(t("settings.notify"))}</label>
        <label class="checkbox-inline"><input type="checkbox" name="users" ${modules.users ? "checked" : ""} /> ${esc(t("nav.users"))}</label>
        <div class="btn-row"><button class="btn-primary" type="submit">${esc(t("action.save"))}</button></div>
      </form>
    </section>
  `;
}

function bindPanelActions() {
  if (S.tab === "servers") bindServers();
  if (S.tab === "policies") bindPolicies();
  if (S.tab === "executions") bindExecutions();
  if (S.tab === "schedules") bindSchedules();
  if (S.tab === "users") bindUsers();
  if (S.tab === "audit") bindAudit();
  if (S.tab === "settings") bindSettings();
}

function bindAudit() {
  document.getElementById("audit-download")?.addEventListener("click", () => {
    const url = new URL(withBasePath("/api/v1/audit-logs"), location.origin);
    url.searchParams.set("download", "1");
    url.searchParams.set("access_token", S.access);
    window.open(url.toString(), "_blank");
  });
}

function bindServers() {
  if (!canWriteServers()) {
    document.querySelectorAll(".server-test").forEach((button) => {
      button.addEventListener("click", async () => {
        try {
          await api(`/api/v1/servers/${encodeURIComponent(button.dataset.id)}/test-connection`, "POST");
          toast(t("servers.test_ok"));
        } catch (err) {
          toast(err.message, true);
        }
      });
    });
    return;
  }

  const form = document.getElementById("server-form");
  form?.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(form);
    const id = String(data.get("id") || "");
    const body = {
      name: String(data.get("name") || "").trim(),
      host: String(data.get("host") || "").trim(),
      port: Number(data.get("port") || 22),
      user: String(data.get("user") || "").trim(),
      key_path: String(data.get("key_path") || "").trim(),
      passphrase: String(data.get("passphrase") || ""),
      tags: splitCSV(data.get("tags")),
      enabled: !!data.get("enabled"),
      paths: splitCSV(data.get("paths")),
      rclone_remote: String(data.get("rclone_remote") || "").trim(),
      rclone_flags: splitCSV(data.get("rclone_flags")),
    };

    try {
      if (id) {
        await api(`/api/v1/servers/${encodeURIComponent(id)}`, "PUT", body);
      } else {
        await api("/api/v1/servers", "POST", body);
      }
      await preload();
      toast(t("common.saved"));
      render();
    } catch (err) {
      toast(err.message, true);
    }
  });

  document.getElementById("server-reset")?.addEventListener("click", () => {
    form?.reset();
    const hidden = form?.querySelector('input[name="id"]');
    if (hidden) hidden.value = "";
  });

  document.querySelectorAll(".server-edit").forEach((button) => {
    button.addEventListener("click", () => fillServerForm(button.dataset.id));
  });

  document.querySelectorAll(".server-test").forEach((button) => {
    button.addEventListener("click", async () => {
      try {
        await api(`/api/v1/servers/${encodeURIComponent(button.dataset.id)}/test-connection`, "POST");
        toast(t("servers.test_ok"));
      } catch (err) {
        toast(err.message, true);
      }
    });
  });

  document.querySelectorAll(".server-del").forEach((button) => {
    button.addEventListener("click", async () => {
      if (!confirm(t("common.confirm_delete"))) return;
      try {
        await api(`/api/v1/servers/${encodeURIComponent(button.dataset.id)}`, "DELETE");
        await preload();
        toast(t("common.deleted"));
        render();
      } catch (err) {
        toast(err.message, true);
      }
    });
  });
}

function fillServerForm(id) {
  const server = S.servers.find((item) => item.id === id);
  if (!server) return;
  const form = document.getElementById("server-form");
  if (!form) return;
  form.querySelector('input[name="id"]').value = server.id || "";
  form.querySelector('input[name="name"]').value = server.name || "";
  form.querySelector('input[name="host"]').value = server.host || "";
  form.querySelector('input[name="port"]').value = server.port || 22;
  form.querySelector('input[name="user"]').value = server.user || "";
  form.querySelector('input[name="key_path"]').value = server.key_path || "";
  form.querySelector('input[name="passphrase"]').value = "";
  form.querySelector('input[name="tags"]').value = (server.tags || []).join(", ");
  form.querySelector('input[name="paths"]').value = (server.paths || []).join(", ");
  form.querySelector('input[name="rclone_remote"]').value = server.rclone_remote || "";
  form.querySelector('input[name="rclone_flags"]').value = (server.rclone_flags || []).join(", ");
  form.querySelector('input[name="enabled"]').checked = !!server.enabled;
}

function bindPolicies() {
  if (canWritePolicies()) {
    const form = document.getElementById("policy-form");
    form?.addEventListener("submit", async (event) => {
      event.preventDefault();
      const data = new FormData(form);
      const id = String(data.get("id") || "");
      const serverNames = Array.from(form.querySelector('select[name="server_names"]').selectedOptions).map((o) => o.value);
      const body = {
        name: String(data.get("name") || "").trim(),
        server_names: serverNames,
        paths: splitCSV(data.get("paths")),
        rclone_remote: String(data.get("rclone_remote") || "").trim(),
        rclone_flags: splitCSV(data.get("rclone_flags")),
        timeout_sec: Number(data.get("timeout_sec") || 0),
        retry_limit: Number(data.get("retry_limit") || 0),
        enabled: !!data.get("enabled"),
      };
      try {
        if (id) {
          await api(`/api/v1/policies/${encodeURIComponent(id)}`, "PUT", body);
        } else {
          await api("/api/v1/policies", "POST", body);
        }
        await preload();
        toast(t("common.saved"));
        render();
      } catch (err) {
        toast(err.message, true);
      }
    });

    document.getElementById("policy-reset")?.addEventListener("click", () => {
      form?.reset();
      const hidden = form?.querySelector('input[name="id"]');
      if (hidden) hidden.value = "";
    });

    document.querySelectorAll(".policy-edit").forEach((button) => {
      button.addEventListener("click", () => fillPolicyForm(button.dataset.id));
    });

    document.querySelectorAll(".policy-del").forEach((button) => {
      button.addEventListener("click", async () => {
        if (!confirm(t("common.confirm_delete"))) return;
        try {
          await api(`/api/v1/policies/${encodeURIComponent(button.dataset.id)}`, "DELETE");
          await preload();
          toast(t("common.deleted"));
          render();
        } catch (err) {
          toast(err.message, true);
        }
      });
    });
  }

  if (canWriteExecutions()) {
    document.querySelectorAll(".policy-run").forEach((button) => {
      button.addEventListener("click", async () => {
        try {
          await api("/api/v1/executions", "POST", { policy_id: button.dataset.id, trigger_type: "manual" });
          await preload();
          S.tab = "executions";
          toast(t("common.created"));
          render();
        } catch (err) {
          toast(err.message, true);
        }
      });
    });
  }
}

function fillPolicyForm(id) {
  const policy = S.policies.find((item) => item.id === id);
  if (!policy) return;
  const form = document.getElementById("policy-form");
  if (!form) return;
  form.querySelector('input[name="id"]').value = policy.id || "";
  form.querySelector('input[name="name"]').value = policy.name || "";
  form.querySelector('input[name="paths"]').value = (policy.paths || []).join(", ");
  form.querySelector('input[name="rclone_remote"]').value = policy.rclone_remote || "";
  form.querySelector('input[name="rclone_flags"]').value = (policy.rclone_flags || []).join(", ");
  form.querySelector('input[name="timeout_sec"]').value = policy.timeout_sec || 0;
  form.querySelector('input[name="retry_limit"]').value = policy.retry_limit || 0;
  form.querySelector('input[name="enabled"]').checked = !!policy.enabled;

  const select = form.querySelector('select[name="server_names"]');
  const set = new Set(policy.server_names || []);
  Array.from(select.options).forEach((option) => {
    option.selected = set.has(option.value);
  });
}

function bindExecutions() {
  document.getElementById("execution-filter-form")?.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    S.executionFilter.status = String(data.get("status") || "").trim();
    S.executionFilter.trigger = String(data.get("trigger") || "").trim();
    S.executionFilter.server = String(data.get("server") || "").trim();
    await fetchExecutions();
    render();
  });

  document.getElementById("execution-filter-reset")?.addEventListener("click", async () => {
    S.executionFilter = { status: "", trigger: "", server: "" };
    await fetchExecutions();
    render();
  });

  if (canWriteExecutions()) {
    document.getElementById("run-policy-form")?.addEventListener("submit", async (event) => {
      event.preventDefault();
      const data = new FormData(event.currentTarget);
      const policyID = String(data.get("policy_id") || "").trim();
      if (!policyID) {
        toast(t("executions.select_policy"), true);
        return;
      }
      try {
        await api("/api/v1/executions", "POST", { policy_id: policyID, trigger_type: "manual" });
        await preload();
        toast(t("common.created"));
        render();
      } catch (err) {
        toast(err.message, true);
      }
    });

    document.getElementById("run-server-form")?.addEventListener("submit", async (event) => {
      event.preventDefault();
      const select = event.currentTarget.querySelector('select[name="server_names"]');
      const serverNames = Array.from(select.selectedOptions).map((option) => option.value);
      if (serverNames.length === 0) {
        toast(t("executions.select_servers"), true);
        return;
      }
      try {
        await api("/api/v1/executions", "POST", { server_names: serverNames, trigger_type: "manual" });
        await preload();
        toast(t("common.created"));
        render();
      } catch (err) {
        toast(err.message, true);
      }
    });

    document.querySelectorAll(".ex-cancel").forEach((button) => {
      button.addEventListener("click", async () => {
        try {
          await api(`/api/v1/executions/${encodeURIComponent(button.dataset.id)}/cancel`, "POST");
          await preload();
          toast(t("common.saved"));
          render();
        } catch (err) {
          toast(err.message, true);
        }
      });
    });

    document.querySelectorAll(".ex-retry").forEach((button) => {
      button.addEventListener("click", async () => {
        try {
          await api(`/api/v1/executions/${encodeURIComponent(button.dataset.id)}/retry`, "POST");
          await preload();
          toast(t("common.created"));
          render();
        } catch (err) {
          toast(err.message, true);
        }
      });
    });
  }

  document.querySelectorAll(".ex-log").forEach((button) => {
    button.addEventListener("click", async () => {
      if (S.stream && S.streamExecutionID && S.streamExecutionID !== button.dataset.id) {
        closeStream();
      }
      S.selectedExecution = button.dataset.id;
      localStorage.setItem("rana.selected_execution", S.selectedExecution);
      if (!S.logFilters[S.selectedExecution]) {
        S.logFilters[S.selectedExecution] = { level: "", contains: "" };
      }
      await loadLogs(S.selectedExecution);
      render();
    });
  });

  document.getElementById("load-logs")?.addEventListener("click", async () => {
    if (!S.selectedExecution) {
      toast(t("executions.select_execution_for_logs"), true);
      return;
    }
    await loadLogs(S.selectedExecution);
    render();
  });

  document.getElementById("download-logs")?.addEventListener("click", () => {
    if (!S.selectedExecution) {
      toast(t("executions.select_execution_for_logs"), true);
      return;
    }
    const url = new URL(withBasePath(`/api/v1/executions/${encodeURIComponent(S.selectedExecution)}/logs`), location.origin);
    url.searchParams.set("download", "1");
    url.searchParams.set("access_token", S.access);
    window.open(url.toString(), "_blank");
  });

  document.getElementById("stream-toggle")?.addEventListener("click", () => {
    if (!S.selectedExecution) {
      toast(t("executions.select_execution_for_logs"), true);
      return;
    }
    if (S.stream) {
      closeStream();
    } else {
      openStream(S.selectedExecution);
    }
    render();
  });

  document.getElementById("log-level-filter")?.addEventListener("change", async (event) => {
    if (!S.selectedExecution) return;
    if (!S.logFilters[S.selectedExecution]) S.logFilters[S.selectedExecution] = { level: "", contains: "" };
    S.logFilters[S.selectedExecution].level = event.target.value;
    await loadLogs(S.selectedExecution);
    render();
  });

  document.getElementById("log-contains-filter")?.addEventListener("change", async (event) => {
    if (!S.selectedExecution) return;
    if (!S.logFilters[S.selectedExecution]) S.logFilters[S.selectedExecution] = { level: "", contains: "" };
    S.logFilters[S.selectedExecution].contains = String(event.target.value || "").trim();
    await loadLogs(S.selectedExecution);
    render();
  });
}

async function loadLogs(id) {
  if (!id) return;
  const filter = S.logFilters[id] || { level: "", contains: "" };
  const url = new URL(withBasePath(`/api/v1/executions/${encodeURIComponent(id)}/logs`), location.origin);
  url.searchParams.set("page", "1");
  url.searchParams.set("page_size", "500");
  if (filter.level) url.searchParams.set("level", filter.level);
  if (filter.contains) url.searchParams.set("contains", filter.contains);
  try {
    const payload = await api(`${url.pathname}${url.search}`);
    S.logs[id] = payload.data || [];
  } catch (err) {
    toast(err.message, true);
  }
}

function openStream(id) {
  closeStream();
  const url = new URL(withBasePath(`/api/v1/executions/${encodeURIComponent(id)}/stream`), location.origin);
  url.searchParams.set("access_token", S.access);
  const stream = new EventSource(url.toString());

  stream.addEventListener("log", (event) => {
    const line = JSON.parse(event.data);
    if (!S.logs[id]) S.logs[id] = [];
    S.logs[id].push(line);
    const out = document.getElementById("log-out");
    if (out && S.selectedExecution === id) {
      out.textContent += `${out.textContent ? "\n" : ""}[${formatTime(line.timestamp)}] [${line.level}] ${line.line}`;
      out.scrollTop = out.scrollHeight;
    }
  });

  stream.onerror = () => {
    closeStream();
    toast(t("executions.stream_disconnected"), true);
    render();
  };

  S.stream = stream;
  S.streamExecutionID = id;
}

function closeStream() {
  if (S.stream) {
    S.stream.close();
  }
  S.stream = null;
  S.streamExecutionID = "";
}

function bindSchedules() {
  if (!canWriteExecutions()) return;

  const form = document.getElementById("schedule-form");
  form?.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(form);
    const id = String(data.get("id") || "");
    const policyID = String(data.get("policy_id") || "").trim();
    const payload = {
      name: String(data.get("name") || "").trim(),
      cron_expr: String(data.get("cron_expr") || "").trim(),
      timezone: String(data.get("timezone") || "UTC").trim(),
      policy_id: policyID,
      server_names: policyID ? [] : splitCSV(data.get("server_names")),
      misfire_policy: String(data.get("misfire_policy") || "run_once").trim(),
      enabled: !!data.get("enabled"),
    };

    try {
      if (id) {
        await api(`/api/v1/schedules/${encodeURIComponent(id)}`, "PUT", payload);
      } else {
        await api("/api/v1/schedules", "POST", payload);
      }
      await preload();
      toast(t("common.saved"));
      render();
    } catch (err) {
      toast(err.message, true);
    }
  });

  document.getElementById("schedule-reset")?.addEventListener("click", () => {
    form?.reset();
    const hidden = form?.querySelector('input[name="id"]');
    if (hidden) hidden.value = "";
  });

  document.querySelectorAll(".sch-edit").forEach((button) => {
    button.addEventListener("click", () => fillScheduleForm(button.dataset.id));
  });

  document.querySelectorAll(".sch-del").forEach((button) => {
    button.addEventListener("click", async () => {
      if (!confirm(t("common.confirm_delete"))) return;
      try {
        await api(`/api/v1/schedules/${encodeURIComponent(button.dataset.id)}`, "DELETE");
        await preload();
        toast(t("common.deleted"));
        render();
      } catch (err) {
        toast(err.message, true);
      }
    });
  });
}

function fillScheduleForm(id) {
  const item = S.schedules.find((schedule) => schedule.id === id);
  if (!item) return;
  const form = document.getElementById("schedule-form");
  if (!form) return;
  form.querySelector('input[name="id"]').value = item.id || "";
  form.querySelector('input[name="name"]').value = item.name || "";
  form.querySelector('input[name="cron_expr"]').value = item.cron_expr || "";
  form.querySelector('input[name="timezone"]').value = item.timezone || "UTC";
  form.querySelector('select[name="policy_id"]').value = item.policy_id || "";
  form.querySelector('input[name="server_names"]').value = (item.server_names || []).join(", ");
  form.querySelector('select[name="misfire_policy"]').value = item.misfire_policy || "run_once";
  form.querySelector('input[name="enabled"]').checked = !!item.enabled;
}

function bindUsers() {
  const form = document.getElementById("user-form");
  form?.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(form);
    const id = String(data.get("id") || "");
    const username = String(data.get("username") || "").trim();
    const role = String(data.get("role") || "viewer").trim();
    const locale = String(data.get("locale") || S.locale).trim();
    const password = String(data.get("password") || "");

    try {
      if (id) {
        await api(`/api/v1/users/${encodeURIComponent(id)}`, "PUT", { username, role, locale });
        if (password) {
          await api(`/api/v1/users/${encodeURIComponent(id)}/password`, "PUT", { new_password: password });
        }
      } else {
        await api("/api/v1/users", "POST", { username, role, locale, password });
      }
      await preload();
      toast(t("common.saved"));
      render();
    } catch (err) {
      toast(err.message, true);
    }
  });

  document.getElementById("user-reset")?.addEventListener("click", () => {
    form?.reset();
    const hidden = form?.querySelector('input[name="id"]');
    if (hidden) hidden.value = "";
  });

  document.querySelectorAll(".user-edit").forEach((button) => {
    button.addEventListener("click", () => fillUserForm(button.dataset.id));
  });

  document.querySelectorAll(".user-pass").forEach((button) => {
    button.addEventListener("click", async () => {
      const pwd = prompt(t("users.prompt_new_password"));
      if (!pwd) return;
      try {
        await api(`/api/v1/users/${encodeURIComponent(button.dataset.id)}/password`, "PUT", { new_password: pwd });
        toast(t("common.saved"));
      } catch (err) {
        toast(err.message, true);
      }
    });
  });

  document.querySelectorAll(".user-del").forEach((button) => {
    button.addEventListener("click", async () => {
      if (!confirm(t("common.confirm_delete"))) return;
      try {
        await api(`/api/v1/users/${encodeURIComponent(button.dataset.id)}`, "DELETE");
        await preload();
        toast(t("common.deleted"));
        render();
      } catch (err) {
        toast(err.message, true);
      }
    });
  });
}

function fillUserForm(id) {
  const user = S.users.find((item) => item.id === id);
  if (!user) return;
  const form = document.getElementById("user-form");
  if (!form) return;
  form.querySelector('input[name="id"]').value = user.id || "";
  form.querySelector('input[name="username"]').value = user.username || "";
  form.querySelector('select[name="role"]').value = user.role || "viewer";
  form.querySelector('select[name="locale"]').value = user.locale || S.locale;
  form.querySelector('input[name="password"]').value = "";
}

function bindSettings() {
  document.getElementById("me-password-form")?.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    const oldPassword = String(data.get("old_password") || "");
    const newPassword = String(data.get("new_password") || "");
    try {
      await api("/api/v1/me/password", "PUT", {
        old_password: oldPassword,
        new_password: newPassword,
      });
      event.currentTarget.reset();
      toast(t("common.saved"));
    } catch (err) {
      toast(err.message, true);
    }
  });

  if (!canWriteSettings()) return;

  document.getElementById("settings-form")?.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    const payload = {
      global: {
        timeout: String(data.get("timeout") || "").trim(),
        concurrency: Number(data.get("concurrency") || 0),
        ssh: {
          strict_host_key: !!data.get("strict_host_key"),
          known_hosts_path: String(data.get("known_hosts_path") || "").trim(),
        },
      },
      web: {
        csrf_enabled: !!data.get("csrf_enabled"),
        cors_allow_origins: splitCSV(data.get("cors_allow_origins")),
        ip_allow_list: splitCSV(data.get("ip_allow_list")),
      },
      i18n: {
        default_locale: String(data.get("default_locale") || S.locale),
      },
      notify: {
        webhook_url: String(data.get("notify_webhook_url") || "").trim(),
        email: {
          enabled: !!data.get("notify_email_enabled"),
          smtp_host: String(data.get("notify_email_smtp_host") || "").trim(),
          smtp_port: Number(data.get("notify_email_smtp_port") || 0),
          username: String(data.get("notify_email_username") || "").trim(),
          password: String(data.get("notify_email_password") || "").trim(),
          from: String(data.get("notify_email_from") || "").trim(),
          to: splitCSV(data.get("notify_email_to")),
          use_tls: !!data.get("notify_email_use_tls"),
        },
        on_success: !!data.get("notify_on_success"),
        on_failure: !!data.get("notify_on_failure"),
        suppression_window: String(data.get("notify_suppression_window") || "0s").trim(),
      },
      modules: {
        schedule: !!data.get("schedule"),
        audit: !!data.get("audit"),
        notify: !!data.get("notify"),
        users: !!data.get("users"),
      },
    };

    try {
      await api("/api/v1/settings", "PUT", payload);
      await preload();
      toast(t("common.saved"));
      render();
    } catch (err) {
      toast(err.message, true);
    }
  });
}

function formatTime(value) {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return String(value);
  return date.toLocaleString();
}

function statusTag(status) {
  const value = String(status || "unknown").toLowerCase();
  const key = `status.${value}`;
  return `<span class="tag ${esc(value)}">${esc(t(key))}</span>`;
}

function toast(message, isError = false) {
  const el = document.getElementById("toast");
  if (!el) return;
  el.textContent = String(message || "");
  el.className = `toast ${isError ? "error" : ""} show`;
  setTimeout(() => {
    el.className = "toast";
  }, 2600);
}
