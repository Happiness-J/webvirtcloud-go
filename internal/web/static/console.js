const screen = document.getElementById('screen');
const status = document.getElementById('console-status');
const allowInsecureConsole = screen?.dataset.allowInsecureConsole === 'true';
let wsURL = screen?.dataset.ws || '';
if (wsURL) {
  const resolved = new URL(wsURL, window.location.href);
  resolved.protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  wsURL = resolved.toString();
}
const passwordURL = screen?.dataset.passwordUrl || '';
let rfb = null;

const setStatus = (text, kind = 'warn') => {
  if (!status) return;
  status.textContent = text;
  status.className = 'pill ' + kind;
};

const blockInsecureContext = () => {
  setStatus('需要 HTTPS', 'bad');
  const warning = document.getElementById('console-security-warning');
  if (warning) {
    warning.hidden = false;
    warning.textContent = '当前页面不是浏览器安全上下文，noVNC 已停止加载。面板与浏览器在同一台机器时可使用 http://localhost:8080；远程设备请通过 HTTPS 地址访问。';
  }
  screen?.classList.add('hidden');
  document.querySelector('.console-tools')?.classList.add('hidden');
};

const showInsecureConsoleWarning = () => {
  const warning = document.getElementById('console-security-warning');
  if (!warning) return;
  warning.hidden = false;
  warning.textContent = 'HTTP 内网模式已启用。登录凭据、会话和 VNC 流量未加密，请仅在可信内网或 VPN 中使用。';
};

const loadPassword = async () => {
  if (!passwordURL) return '';
  try {
    const response = await fetch(passwordURL, {
      cache: 'no-store',
      credentials: 'same-origin',
      referrerPolicy: 'no-referrer',
    });
    if (!response.ok) return '';
    const body = await response.json();
    return typeof body.password === 'string' ? body.password : '';
  } catch (_) {
    return '';
  }
};

const connect = async () => {
  if (!screen || !wsURL) return;
  if (!window.isSecureContext && !allowInsecureConsole) {
    blockInsecureContext();
    return;
  }
  if (!window.isSecureContext) showInsecureConsoleWarning();
  try {
    const { default: RFB } = await import('/static/novnc/core/rfb.js');
    const options = {
      shared: true,
      scaleViewport: true,
      resizeSession: false,
    };
    const password = await loadPassword();
    if (password) options.credentials = { password };
    rfb = new RFB(screen, wsURL, options);
    rfb.addEventListener('connect', () => setStatus('已连接', 'ok'));
    rfb.addEventListener('clipboard', (event) => {
      const text = event.detail?.text || '';
      if (text && navigator.clipboard?.writeText) navigator.clipboard.writeText(text).catch(() => {});
    });
    rfb.addEventListener('disconnect', (event) => {
      if (event.detail.clean) setStatus('已断开', 'muted');
      else setStatus('连接断开', 'bad');
    });
    rfb.addEventListener('securityfailure', (event) => {
      setStatus('认证失败: ' + event.detail.reason, 'bad');
    });
    rfb.addEventListener('credentialsrequired', () => {
      const value = window.prompt('VNC 密码');
      if (value) rfb.sendCredentials({ password: value });
    });
    screen.focus();
  } catch (err) {
    setStatus('初始化失败: ' + err.message, 'bad');
  }
};

document.getElementById('send-ctrl-alt-del')?.addEventListener('click', () => rfb?.sendCtrlAltDel());
document.getElementById('paste-local')?.addEventListener('click', async () => {
  if (!rfb || !navigator.clipboard?.readText) return;
  const text = await navigator.clipboard.readText();
  if (text) rfb.clipboardPasteFrom(text);
});
document.getElementById('scale-toggle')?.addEventListener('click', () => {
  if (!rfb) return;
  rfb.scaleViewport = !rfb.scaleViewport;
});
window.addEventListener('beforeunload', () => rfb?.disconnect());
if (!window.isSecureContext && !allowInsecureConsole) blockInsecureContext();
else connect();
