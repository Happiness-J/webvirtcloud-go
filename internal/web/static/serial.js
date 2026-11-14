(() => {
  const screen = document.getElementById('serial-screen');
  const status = document.getElementById('serial-status');
  const form = document.getElementById('serial-form');
  const input = document.getElementById('serial-input');
  if (!screen || !form || !input) return;
  let socket;
  const setStatus = (text, kind) => { status.textContent = text; status.className = 'pill ' + kind; };
  const connect = () => {
    const url = new URL(screen.dataset.ws, window.location.href);
    url.protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    socket = new WebSocket(url);
    socket.addEventListener('open', () => setStatus('已连接', 'ok'));
    socket.addEventListener('message', (event) => { screen.textContent += event.data; screen.scrollTop = screen.scrollHeight; });
    socket.addEventListener('close', () => setStatus('已断开', 'muted'));
    socket.addEventListener('error', () => setStatus('连接失败', 'bad'));
  };
  form.addEventListener('submit', (event) => {
    event.preventDefault();
    if (socket && socket.readyState === WebSocket.OPEN) socket.send(input.value + '\n');
    input.value = '';
    screen.focus();
  });
  document.getElementById('serial-reconnect')?.addEventListener('click', () => window.location.reload());
  connect();
})();
