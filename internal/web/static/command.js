'use strict';

(() => {
  const root = document.getElementById('command-center');
  if (!root) return;

  const elements = {
    list: document.getElementById('command-list'),
    count: document.getElementById('command-count'),
    search: document.getElementById('command-search'),
    toolFilter: document.getElementById('command-tool-filter'),
    categoryFilter: document.getElementById('command-category-filter'),
    empty: document.getElementById('command-empty'),
    form: document.getElementById('command-form'),
    category: document.getElementById('command-category'),
    title: document.getElementById('command-title'),
    summary: document.getElementById('command-summary'),
    badges: document.getElementById('command-badges'),
    usage: document.getElementById('command-usage'),
    docs: document.getElementById('command-docs'),
    disabled: document.getElementById('command-disabled'),
    qemuIOHint: document.getElementById('command-qemu-io-hint'),
    tool: document.getElementById('command-tool'),
    toolValue: document.getElementById('command-tool-value'),
    builder: document.getElementById('command-builder-fields'),
    mode: document.getElementById('command-mode'),
    argsJSON: document.getElementById('command-args-json'),
    advancedToggle: document.getElementById('command-advanced-toggle'),
    advancedFields: document.getElementById('command-advanced-fields'),
    rawArgs: document.getElementById('command-raw-args'),
    preview: document.getElementById('command-preview'),
    submit: document.getElementById('command-submit'),
  };

  const state = {
    items: [],
    filtered: [],
    selected: null,
    fields: [],
    target: null,
    limit: 80,
  };

  const make = (tag, className, text) => {
    const node = document.createElement(tag);
    if (className) node.className = className;
    if (text !== undefined) node.textContent = text;
    return node;
  };

  const riskClass = (risk) => {
    if (risk === '低') return 'ok';
    if (risk === '高' || risk === '极高') return 'bad';
    return 'warn';
  };

  const badge = (text, kind) => make('span', 'pill ' + (kind || 'muted'), text);


  const commandPurpose = (item) => {
    const purpose = {
      help: '查看命令帮助', version: '查看版本', connect: '连接 libvirt', list: '列出对象',
      dominfo: '查看虚拟机信息', dumpxml: '导出虚拟机配置', start: '启动虚拟机', shutdown: '正常关闭虚拟机',
      destroy: '强制关闭虚拟机', reboot: '重启虚拟机', screenshot: '截取虚拟机画面'
    };
    return purpose[item.command] || (item.summary && /[\u4e00-\u9fff]/.test(item.summary) ? item.summary : item.category + '命令');
  };
  const commandState = (item) => {
    if (item.toolAvailable === false) return '不可执行：工具未安装';
    if (item.disabled) return '不可直接执行：交互式命令';
    return '可执行';
  };

  const sameItem = (left, right) => left && right && left.tool === right.tool && left.command === right.command;

  const quoteArg = (value) => {
    if (value === '') return "''";
    if (!/[\s'"\\$`;&|<>(){}[\]*?!]/.test(value)) return value;
    return "'" + value.replace(/'/g, "'\\''") + "'";
  };

  const filterItems = () => {
    const term = (elements.search.value || '').trim().toLowerCase();
    const tool = elements.toolFilter.value;
    const category = elements.categoryFilter.value;
    state.filtered = state.items.filter((item) => {
      if (tool && item.tool !== tool) return false;
      if (category && item.category !== category) return false;
      if (!term) return true;
      const haystack = [item.tool, item.command, item.category, item.summary, item.usage].join(' ').toLowerCase();
      return haystack.includes(term);
    });
    state.limit = 80;
    renderList();
  };

  const renderList = () => {
    elements.list.replaceChildren();
    elements.count.textContent = state.filtered.length + ' / ' + state.items.length + ' 条';
    if (state.filtered.length === 0) {
      elements.list.append(make('p', 'muted command-loading', '没有匹配的命令'));
      return;
    }
    let group = '';
    const visible = state.filtered.slice(0, state.limit);
    for (const item of visible) {
      const nextGroup = item.tool + '\u0000' + item.category;
      if (nextGroup !== group) {
        group = nextGroup;
        const heading = make('div', 'command-group-title');
        heading.append(make('span', '', item.tool), document.createTextNode(' · ' + item.category));
        elements.list.append(heading);
      }
      const button = make('button', 'command-item');
      button.type = 'button';
      if (sameItem(item, state.selected)) button.classList.add('selected');
      if (item.disabled || item.toolAvailable === false) button.classList.add('disabled');
      const head = make('span', 'command-item-head');
      head.append(make('code', '', item.tool + ' ' + item.command));
      head.append(badge(item.risk, riskClass(item.risk)));
      button.append(head);
      button.append(make('span', 'command-item-summary', commandPurpose(item)));
      const meta = make('span', 'command-item-meta');
      meta.append(make('span', item.disabled || item.toolAvailable === false ? 'tiny bad-text' : 'tiny ok-text', commandState(item)));
      if (item.readOnly) meta.append(make('span', 'tiny ok-text', '只读'));
      if (item.sensitive) meta.append(make('span', 'tiny warn-text', '敏感'));
      if (item.toolAvailable === false) meta.append(make('span', 'tiny bad-text', '工具未安装'));
      if (item.probeState === 'missing') meta.append(make('span', 'tiny warn-text', '当前版本未列出'));
      button.append(meta);
      button.addEventListener('click', () => selectItem(item));
      elements.list.append(button);
    }
    if (state.filtered.length > visible.length) {
      const more = make('button', 'btn small command-more', '显示更多（剩余 ' + (state.filtered.length - visible.length) + ' 条）');
      more.type = 'button';
      more.addEventListener('click', () => {
        state.limit += 80;
        renderList();
      });
      elements.list.append(more);
    }
  };

  const populateCategories = () => {
    const seen = new Set();
    for (const item of state.items) {
      if (!item.category || seen.has(item.category)) continue;
      seen.add(item.category);
      const option = make('option', '', item.category);
      option.value = item.category;
      elements.categoryFilter.append(option);
    }
  };

  const addSection = (title, hint) => {
    const section = make('section', 'command-field-section');
    const head = make('div', 'command-field-head');
    head.append(make('h3', '', title));
    if (hint) head.append(make('span', 'hint', hint));
    section.append(head);
    elements.builder.append(section);
    return section;
  };

  const addDatalist = (container, id, choices) => {
    if (!choices || choices.length === 0) return;
    const list = document.createElement('datalist');
    list.id = id;
    for (const choice of choices) {
      const option = document.createElement('option');
      option.value = choice;
      list.append(option);
    }
    container.append(list);
  };

  const createValueInput = (spec, id) => {
    const input = document.createElement(spec.multiple || spec.valueType === 'textarea' ? 'textarea' : 'input');
    input.id = id;
    input.value = '';
    input.placeholder = spec.placeholder || '';
    input.setAttribute('aria-label', spec.label || spec.name);
    input.autocomplete = 'off';
    input.spellcheck = false;
    if (input.tagName === 'TEXTAREA') input.rows = 2;
    if (input.tagName === 'INPUT') {
      input.type = 'text';
      if (spec.valueType === 'number') input.inputMode = 'numeric';
    }
    if (spec.multiple) input.placeholder = '每行一个值';
    return input;
  };

  const addParameter = (section, spec, kind, sequence) => {
    if (spec.valueType === 'literal') {
      state.fields.push({ spec, kind, toggle: null, input: null });
      return;
    }
    const row = make('div', 'parameter-row');
    const head = make('div', 'parameter-label');
    const id = 'command-param-' + kind + '-' + sequence;
    let toggle = null;
    let input = null;

    if (kind === 'option' || (kind === 'common' && spec.flag)) {
      toggle = document.createElement('input');
      toggle.type = 'checkbox';
      toggle.id = id + '-enabled';
      toggle.checked = Boolean(spec.required);
      if (spec.required || spec.disabled) toggle.disabled = true;
      if (spec.disabled) toggle.checked = false;
      const label = document.createElement('label');
      label.htmlFor = toggle.id;
      label.append(toggle, make('code', '', spec.flag));
      head.append(label);
    } else {
      head.append(make('span', '', spec.label || spec.name));
    }
    if (spec.required) head.append(badge('必填', 'warn'));
    if (spec.multiple) head.append(badge('可重复', 'muted'));
    if (spec.disabled) head.append(badge('不可异步', 'bad'));
    row.append(head);

    if (spec.disabled && spec.disabledReason) {
      row.append(make('p', 'hint bad-text', spec.disabledReason));
    }

    if (spec.valueType !== 'bool') {
      input = createValueInput(spec, id + '-value');
      if (toggle) {
        input.disabled = !toggle.checked;
        toggle.addEventListener('change', () => {
          input.disabled = !toggle.checked;
          update();
        });
      }
      if (spec.disabled) input.disabled = true;
      if (spec.choices && spec.choices.length > 0) {
        const datalistID = id + '-choices';
        input.setAttribute('list', datalistID);
        addDatalist(row, datalistID, spec.choices);
      }
      input.addEventListener('input', update);
      row.append(input);
    } else if (toggle) {
      toggle.addEventListener('change', update);
    }

    state.fields.push({ spec, kind, toggle, input });
    section.append(row);
  };

  const renderParameters = (item) => {
    elements.builder.replaceChildren();
    state.fields = [];
    let sequence = 0;

    if (item.common && item.common.length > 0) {
      const isQemuIO = item.buildMode === 'qemu-io';
      const section = addSection(
        isQemuIO ? 'qemu-io 文件选项' : '全局命令选项',
        isQemuIO ? '这些选项加在 -c 命令之前' : '这些选项加在子命令之前'
      );
      for (const spec of item.common) {
        addParameter(section, spec, 'common', sequence++);
      }
    }
    if (item.positionals && item.positionals.length > 0) {
      const section = addSection('位置参数', '按官方语法顺序生成 argv');
      for (const spec of item.positionals) {
        addParameter(section, spec, 'positional', sequence++);
      }
    }
    if (item.options && item.options.length > 0) {
      const hint = item.buildMode === 'qemu-io'
        ? '这些选项写入 -c 命令串；qemu-io 不解析 shell 引号，参数不能包含空白'
        : '互斥选项通常只选一项；枚举字段仍可手动输入其他版本值';
      const section = addSection('命令选项', hint);
      for (const spec of item.options) {
        addParameter(section, spec, 'option', sequence++);
      }
    }
    if (state.fields.length === 0) {
      const section = addSection('无需额外参数', '该命令可直接提交');
      section.append(make('p', 'hint', '如需版本特有参数，请使用下方高级原始参数。'));
    }
  };

  const fieldValue = (field) => {
    if (!field.input) return '';
    return field.input.value.trim();
  };

  const fieldValues = (field) => {
    const value = fieldValue(field);
    if (!value) return [];
    if (field.spec.multiple) {
      return value.split(/\r?\n/).map((part) => part.trim()).filter(Boolean);
    }
    return [value];
  };

  const positionalValues = (field) => {
    if (field.spec.disabled) return [];
    if (field.spec.literal) return [field.spec.literal];
    return fieldValues(field).map((value) => (field.spec.valuePrefix || '') + value);
  };

  const fieldEnabled = (field) => {
    return !field.toggle || field.toggle.checked;
  };

  const appendOption = (args, field) => {
    if (field.spec.disabled) return;
    if (!fieldEnabled(field)) return;
    if (field.spec.valueType === 'bool') {
      args.push(field.spec.flag);
      return;
    }
    const values = fieldValues(field);
    if (values.length === 0) {
      if (field.spec.optionalValue) args.push(field.spec.flag);
      return;
    }
    for (const value of values) args.push(field.spec.flag, value);
  };

  const buildQemuIO = (item) => {
    const args = [];
    let image = '';
    const before = [];
    const after = [];
    for (const field of state.fields) {
      if (field.kind === 'common' && field.spec.name === 'image') {
        image = fieldValue(field);
        continue;
      }
      if (field.kind === 'common' && field.spec.name === 'commands_before') {
        before.push(...fieldValues(field));
        continue;
      }
      if (field.kind === 'common' && field.spec.name === 'commands_after') {
        after.push(...fieldValues(field));
        continue;
      }
      if (field.kind === 'common') appendOption(args, field);
    }
    const commandArgs = [item.command];
    for (const field of state.fields) {
      if (field.kind === 'option') appendOption(commandArgs, field);
      if (field.kind === 'positional') {
        for (const value of positionalValues(field)) commandArgs.push(value);
      }
    }
    // qemu-io's -c parser splits on whitespace and does not implement shell
    // quoting. Do not add quotes here; validateBuilder rejects whitespace in
    // command arguments instead of producing a misleading argv.
    for (const command of before) args.push('-c', command);
    args.push('-c', commandArgs.join(' '));
    for (const command of after) args.push('-c', command);
    if (image) args.push(image);
    return args;
  };

  const buildArgs = (item) => {
    if (!item) return [];
    if (item.buildMode === 'qemu-io') return buildQemuIO(item);
    const args = [];
    for (const field of state.fields) {
      if (field.kind === 'common') appendOption(args, field);
    }
    args.push(...(item.argPrefix || []));
    for (const field of state.fields) {
      if (field.kind === 'option') appendOption(args, field);
    }
    for (const field of state.fields) {
      if (field.kind !== 'positional') continue;
      for (const value of positionalValues(field)) args.push(value);
    }
    return args;
  };

  const validateBuilder = () => {
    let valid = true;
    for (const field of state.fields) {
      if (field.spec.disabled) continue;
      if (field.spec.valueType === 'literal') continue;
      if (field.toggle && field.toggle.checked && field.spec.valueType !== 'bool' && !fieldValue(field)) {
        valid = field.spec.optionalValue ? valid : false;
      }
      if (state.selected && state.selected.buildMode === 'qemu-io' && field.kind !== 'common') {
        for (const value of fieldValues(field)) {
          if (/\s/.test(value)) {
            valid = false;
          }
        }
      }
      const required = Boolean(field.spec.required);
      if (!required) continue;
      if (field.spec.valueType === 'bool') continue;
      if (field.kind === 'option' && !fieldEnabled(field)) {
        valid = false;
        continue;
      }
      if (!fieldValue(field)) valid = false;
    }
    return valid;
  };

  const currentTool = () => elements.toolValue.value;

  const selectedToolAvailable = () => {
    if (!state.selected) return false;
    if (!elements.advancedToggle.checked || currentTool() === state.selected.tool) {
      return state.selected.toolAvailable !== false;
    }
    return state.items.some((item) => item.tool === currentTool() && item.toolAvailable !== false);
  };

  const update = () => {
    const item = state.selected;
    if (!item) return;
    if (elements.advancedToggle.checked) {
      const raw = elements.rawArgs.value.trim();
      elements.preview.textContent = raw ? currentTool() + ' ' + raw : '请输入原始参数';
      elements.preview.classList.remove('invalid');
      elements.argsJSON.value = '';
      elements.submit.disabled = item.disabled || !selectedToolAvailable() || !raw;
      return;
    }
    const args = buildArgs(item);
    const valid = validateBuilder();
    elements.argsJSON.value = args.length > 0 ? JSON.stringify(args) : '[]';
    elements.preview.textContent = (currentTool() + (args.length ? ' ' + args.map(quoteArg).join(' ') : '')).trim();
    elements.submit.disabled = item.disabled || !selectedToolAvailable() || !valid;
    elements.preview.classList.toggle('invalid', !valid);
  };

  const renderEditor = (item) => {
    elements.empty.classList.add('hidden');
    elements.form.classList.remove('hidden');
    elements.category.textContent = item.tool + ' · ' + item.category;
    elements.title.textContent = item.tool + ' ' + item.command;
    elements.summary.textContent = item.summary || '';
    elements.usage.textContent = item.usage || item.command;
    elements.docs.href = item.docsUrl || '#';
    elements.docs.classList.toggle('hidden', !item.docsUrl);
    elements.badges.replaceChildren();
    elements.badges.append(badge(item.risk, riskClass(item.risk)));
    elements.badges.append(badge(item.readOnly ? '只读' : '写操作', item.readOnly ? 'ok' : 'warn'));
    if (item.sensitive) elements.badges.append(badge('敏感', 'bad'));
    if (item.toolAvailable === false) elements.badges.append(badge(state.target && state.target.remote ? '目标主机未安装/不支持' : '工具未安装', 'bad'));
    if (item.probeState === 'advertised') elements.badges.append(badge('当前版本已列出', 'ok'));
    if (item.probeState === 'missing') elements.badges.append(badge('当前版本未列出', 'warn'));
    if (item.probeState === 'unsupported') elements.badges.append(badge('执行目标不支持', 'bad'));
    if (item.disabled) elements.badges.append(badge('不可异步执行', 'bad'));

    const unavailableReasons = [];
    if (item.toolAvailable === false) unavailableReasons.push(item.unavailableReason || ('执行目标未安装 ' + item.tool + '，无法提交该命令。'));
    if (item.disabled) unavailableReasons.push(item.disabledReason || '该命令不能作为异步任务执行。');
    elements.disabled.classList.toggle('hidden', unavailableReasons.length === 0);
    elements.disabled.textContent = unavailableReasons.join(' ');
    const statefulQemuIO = item.buildMode === 'qemu-io' && ['break', 'remove_break', 'resume', 'wait_break'].includes(item.command);
    elements.qemuIOHint.hidden = !statefulQemuIO;
    elements.tool.value = item.tool;
    elements.toolValue.value = item.tool;
    elements.tool.disabled = true;
    elements.advancedToggle.checked = false;
    elements.advancedToggle.disabled = item.disabled;
    elements.advancedFields.classList.add('hidden');
    elements.builder.classList.remove('hidden');
    elements.rawArgs.value = '';
    elements.mode.value = 'builder';
    renderParameters(item);
    update();
  };

  const selectItem = (item) => {
    state.selected = item;
    renderList();
    renderEditor(item);
  };

  const load = async () => {
    const response = await fetch(root.dataset.catalogUrl, {
      credentials: 'same-origin',
      headers: { Accept: 'application/json' },
    });
    if (!response.ok) throw new Error('HTTP ' + response.status);
    const payload = await response.json();
    state.items = Array.isArray(payload.items) ? payload.items : [];
    state.target = payload.target || null;
    populateCategories();
    filterItems();
  };

  elements.search.addEventListener('input', filterItems);
  elements.toolFilter.addEventListener('change', filterItems);
  elements.categoryFilter.addEventListener('change', filterItems);
  elements.advancedToggle.addEventListener('change', () => {
    const advanced = elements.advancedToggle.checked;
    elements.mode.value = advanced ? 'advanced' : 'builder';
    elements.builder.classList.toggle('hidden', advanced);
    elements.advancedFields.classList.toggle('hidden', !advanced);
    elements.tool.disabled = !advanced;
    if (!advanced && state.selected) {
      elements.tool.value = state.selected.tool;
      elements.toolValue.value = state.selected.tool;
    }
    update();
  });
  elements.rawArgs.addEventListener('input', update);
  elements.tool.addEventListener('change', () => {
    elements.toolValue.value = elements.tool.value;
    update();
  });
  elements.form.addEventListener('submit', (event) => {
    if (!state.selected || state.selected.disabled || !selectedToolAvailable()) {
      event.preventDefault();
      return;
    }
    if (elements.advancedToggle.checked) return;
    if (!validateBuilder()) {
      event.preventDefault();
      update();
      return;
    }
    elements.argsJSON.value = JSON.stringify(buildArgs(state.selected));
  });

  load().catch((error) => {
    elements.list.replaceChildren(make('p', 'flash error', '命令目录加载失败：' + error.message));
    elements.count.textContent = '加载失败';
  });
})();
