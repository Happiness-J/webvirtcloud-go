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
