'use strict';

document.addEventListener('click', (event) => {
  const button = event.target.closest('button[data-confirm]');
  if (button && !window.confirm(button.dataset.confirm || '确定继续吗？')) {
    event.preventDefault();
  }
});

document.addEventListener('click', (event) => {
  const sample = event.target.closest('.sample-args');
  if (!sample) return;
  const input = document.getElementById('command-raw-args');
  if (input) {
    input.value = sample.dataset.args || '';
    const advanced = document.getElementById('command-advanced-toggle');
    if (advanced && !advanced.checked) {
      advanced.checked = true;
      advanced.dispatchEvent(new Event('change', { bubbles: true }));
    }
    input.dispatchEvent(new Event('input', { bubbles: true }));
    input.focus();
  }
});

document.addEventListener('change', (event) => {
  const master = event.target.closest('input[type="checkbox"][data-select-all]');
  if (!master) return;
  const group = master.dataset.selectAll;
  document.querySelectorAll('input[type="checkbox"][data-select-group]').forEach((item) => {
    if (item.dataset.selectGroup === group && !item.disabled) item.checked = master.checked;
  });
});

(() => {
  const panel = document.getElementById('job-output-panel');
  if (!panel) return;
  const id = panel.dataset.jobId;
  if (!id) return;
  const status = document.getElementById('job-status');
  const output = document.getElementById('job-output');
  const error = document.getElementById('job-error');
  const refresh = document.getElementById('job-refresh');
  const created = document.getElementById('job-created');
  const started = document.getElementById('job-started');
  const finished = document.getElementById('job-finished');
  const cancelForm = document.getElementById('job-cancel-form');
  if (!status || !output) return;
  let failures = 0;

  const formatTime = (value) => {
    if (!value) return '-';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return '-';
    const pad = (part) => String(part).padStart(2, '0');
    return date.getFullYear() + '-' + pad(date.getMonth() + 1) + '-' + pad(date.getDate()) +
      ' ' + pad(date.getHours()) + ':' + pad(date.getMinutes()) + ':' + pad(date.getSeconds());
  };

  const classify = (value) => {
    if (value === 'succeeded' || value === 'running') return 'ok';
    if (value === 'failed' || value === 'cancelled') return 'bad';
    if (value === 'queued') return 'warn';
    return 'muted';
  };

  const poll = async () => {
    if (!['queued', 'running'].includes(status.textContent.trim())) return;
    try {
      const response = await fetch('/api/jobs/' + encodeURIComponent(id), {
        credentials: 'same-origin',
        headers: { 'Accept': 'application/json' },
      });
      if (!response.ok) throw new Error('HTTP ' + response.status);
      const job = await response.json();
      status.textContent = job.status;
      status.className = 'pill ' + classify(job.status);
      output.textContent = job.output || '（暂无输出）';
      if (created) created.textContent = formatTime(job.created_at);
      if (started) started.textContent = formatTime(job.started_at);
      if (finished) finished.textContent = formatTime(job.finished_at);
      if (job.error) {
        error.textContent = job.error;
        error.classList.remove('hidden');
      } else {
        error.classList.add('hidden');
      }
      failures = 0;
      if (!['queued', 'running'].includes(job.status)) {
        if (refresh) refresh.textContent = '已结束';
        if (cancelForm) cancelForm.classList.add('hidden');
        return;
      }
    } catch (err) {
      failures += 1;
      if (refresh) refresh.textContent = '刷新失败，重试中';
    }
    setTimeout(poll, Math.min(10000, 1500 + failures * 1000));
  };
  setTimeout(poll, 1200);
})();

(() => {
  const board = document.getElementById('resource-board');
  if (!board) return;
  const history = new Map();
  const draw = (card, values) => {
    const canvas = card.querySelector('canvas');
    if (!canvas) return;
    const rect = canvas.getBoundingClientRect();
    canvas.width = Math.max(240, Math.floor(rect.width * devicePixelRatio));
    canvas.height = Math.floor(72 * devicePixelRatio);
    const ctx = canvas.getContext('2d');
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    ctx.strokeStyle = '#d5e0ea';
    ctx.beginPath(); ctx.moveTo(0, canvas.height - 1); ctx.lineTo(canvas.width, canvas.height - 1); ctx.stroke();
    ctx.strokeStyle = '#2f80ed'; ctx.lineWidth = 2 * devicePixelRatio; ctx.beginPath();
    values.forEach((value, index) => {
      const x = values.length === 1 ? 0 : (index / (values.length - 1)) * canvas.width;
      const y = canvas.height - (Math.max(0, Math.min(100, value)) / 100) * (canvas.height - 4);
      if (index === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
    });
    ctx.stroke();
  };
  const render = (payload) => {
    const root = board.querySelector('.resource-groups') || board.appendChild(Object.assign(document.createElement('div'), {className:'resource-groups'}));
    root.innerHTML = '';
    (payload.hosts || []).forEach((host) => {
      const section = document.createElement('section'); section.className='resource-group';
      const title = document.createElement('h3'); title.textContent = host.Host && host.Host.Name ? host.Host.Name : '主机';
      section.appendChild(title);
      if (host.Warning) { const warn = document.createElement('p'); warn.className='flash warn'; warn.textContent = host.Warning; section.appendChild(warn); }
      const grid = document.createElement('div'); grid.className='resource-grid';
      (host.Domains || []).forEach((vm) => {
        const key = host.Host.ID + ':' + vm.Name;
        const values = (history.get(key) || []).concat(vm.Usage || 0).slice(-30);
        history.set(key, values);
        const card = document.createElement('article'); card.className='resource-card';
        card.innerHTML = '<header><strong></strong><span class="pill"></span></header><dl><div><dt>内存</dt><dd class="memory"></dd></div><div><dt>磁盘</dt><dd class="disk"></dd></div><div><dt>CPU</dt><dd class="cpu"></dd></div></dl><canvas aria-label="内存趋势"></canvas><div class="meter"><span></span></div>';
        card.querySelector('strong').textContent = vm.Name;
        const pill = card.querySelector('.pill'); pill.textContent = vm.State || '-'; pill.classList.add(vm.State === 'running' ? 'ok' : 'muted');
        card.querySelector('.memory').textContent = vm.Memory || '-';
        card.querySelector('.disk').textContent = vm.Disk || '-';
        card.querySelector('.cpu').textContent = vm.CPU || '-';
        card.querySelector('.meter span').style.width = (vm.Usage || 0) + '%';
        grid.appendChild(card); draw(card, values);
      });
      if (!host.Domains || host.Domains.length === 0) grid.innerHTML = '<p class="empty">没有正在运行的虚拟机。</p>';
      section.appendChild(grid); root.appendChild(section);
    });
  };
  const poll = async () => {
    try {
      const response = await fetch(board.dataset.resourceUrl, {credentials:'same-origin', headers:{'Accept':'application/json'}});
      if (response.ok) render(await response.json());
    } catch (_) {}
  };
  poll(); setInterval(poll, 15000);
})();
