
(() => {
  const toast = (msg, type) => (window.showToast || (() => {}))(msg, type);

  const ACTIONS = [
    { key: 'orders',    icon: '🛒', title: 'Заказы',         hint: 'Гости оформляют блюда, продукты списываются по FEFO' },
    { key: 'supplies',  icon: '🚚', title: 'Поставки',       hint: 'На склад приходят новые партии с новыми сроками' },
    { key: 'writeoffs', icon: '🗑', title: 'Списания',       hint: 'Порча и авто-списание просроченных партий' },
    { key: 'auth',      icon: '🔐', title: 'Авторизации',    hint: 'Входы персонала в систему' },
    { key: 'stoplist',  icon: '🚫', title: 'Стоп-лист',      hint: 'Блюда снимаются с продажи и возвращаются обратно' },
    { key: 'adjust',    icon: '✏️', title: 'Инвентаризация', hint: 'Ручные корректировки остатков партий' },
  ];

  const DEFAULT_INTERVAL = 30;
  const MIN_INTERVAL = 5;
  const MAX_INTERVAL = 600;

  let status = null;       
  let settingsOpen = false; 
  let pollTimer = null;
  let closeWatcher = null;

  function clamp(v) {
    if (isNaN(v)) return DEFAULT_INTERVAL;
    return Math.min(MAX_INTERVAL, Math.max(MIN_INTERVAL, v));
  }

  function cfgOf(key) {
    return (status && status.actions && status.actions[key]) || {};
  }

  function actionRow(a) {
    const cfg = cfgOf(a.key);
    const enabled = cfg.enabled !== false;
    const running = !!(status && status.enabled);
    const next = cfg.next_in || 0;
    const can = API.isAdmin();
    const lock = can ? '' : 'disabled';
    return `
      <div class="demo-row${running && enabled ? ' live' : ''}">
        <label class="demo-switch" title="${enabled ? 'Включено' : 'Выключено'}">
          <input type="checkbox" data-demo-toggle="${a.key}" ${enabled ? 'checked' : ''} ${lock}>
          <span class="demo-slider"></span>
        </label>
        <div class="demo-row-main">
          <div class="demo-row-title">${a.icon} ${a.title}</div>
          <div class="demo-row-hint">${a.hint}</div>
        </div>
        <div class="demo-row-interval">
          <span>каждые</span>
          <input type="number" data-demo-interval="${a.key}" value="${cfg.interval || DEFAULT_INTERVAL}"
                 min="${MIN_INTERVAL}" max="${MAX_INTERVAL}" step="5" ${lock}>
          <span>сек</span>
        </div>
        <div class="demo-row-next" data-demo-next="${a.key}">${running && enabled ? '⏳ ' + next + ' с' : '—'}</div>
        ${can ? `<button class="btn-xs" data-demo-run="${a.key}" title="Выполнить сейчас">▶</button>` : ''}
      </div>`;
  }

  function settingsHTML() {
    const running = !!(status && status.enabled);
    return `
      <p class="demo-note">Система сама выполнит типовые операции ресторана, будто за пультами сидят сотрудники.
      Темп каждого действия настраивается отдельно и применяется сразу, без перезапуска.</p>
      <div class="demo-list">${ACTIONS.map(actionRow).join('')}</div>
      <div class="demo-totals">
        <span>🛒 создано заказов: <strong>${(status && status.orders) || 0}</strong></span>
        <span>🚚 принято поставок: <strong>${(status && status.supplies) || 0}</strong></span>
      </div>
      <div style="display:flex;gap:10px;margin-top:16px">
        <button class="btn ${running ? 'danger' : 'primary'}" style="flex:1" onclick="applyDemoMode()" title="${running ? 'Остановить имитацию работы' : 'Запустить имитацию работы ресторана'}">
          ${running ? '⏹ Остановить демонстрацию' : '▶ Запустить демонстрацию'}
        </button>
        <button class="btn secondary" onclick="resetDemoIntervals()">↺ Сбросить</button>
      </div>`;
  }

  function showSettings() {
    settingsOpen = true;
    window.openModal('🎬 Демонстрация работы системы', settingsHTML());
    bindSettingsEvents();
  }

  function refreshCounters() {
    const running = !!(status && status.enabled);
    ACTIONS.forEach(a => {
      const row = document.querySelector(`[data-demo-next="${a.key}"]`);
      if (!row) return;
      const enabled = cfgOf(a.key).enabled !== false;
      row.textContent = running && enabled ? '⏳ ' + (cfgOf(a.key).next_in || 0) + ' с' : '—';
    });
  }

  function readForm() {
    const cfg = {};
    ACTIONS.forEach(a => {
      const toggle = document.querySelector(`[data-demo-toggle="${a.key}"]`);
      const interval = document.querySelector(`[data-demo-interval="${a.key}"]`);
      cfg[a.key] = {
        enabled: toggle ? toggle.checked : true,
        interval: clamp(parseInt(interval ? interval.value : DEFAULT_INTERVAL, 10)),
      };
    });
    return cfg;
  }

  async function saveForm() {
    try {
      status = await API.demoConfig(readForm());
      refreshCounters();
      refreshDemoButton();
    } catch (e) {
      toast('Не удалось сохранить настройки: ' + e.message, 'err');
    }
  }

  function bindSettingsEvents() {
    document.querySelectorAll('[data-demo-interval]').forEach(el => {
      el.addEventListener('change', () => {
        el.value = clamp(parseInt(el.value, 10));
        saveForm();
      });
    });
    document.querySelectorAll('[data-demo-toggle]').forEach(el => {
      el.addEventListener('change', saveForm);
    });
    document.querySelectorAll('[data-demo-run]').forEach(el => {
      el.addEventListener('click', async () => {
        try {
          status = await API.demoRun(el.dataset.demoRun);
          toast('Действие выполнено', 'ok');
          showSettings();
          refreshDemoButton();
        } catch (e) {
          toast('Ошибка: ' + e.message, 'err');
        }
      });
    });
  }

  window.openDemoModal = async function() {
    try {
      status = await API.demoStatus();
      showSettings();
    } catch (e) {
      toast('Ошибка загрузки настроек: ' + e.message, 'err');
    }
  };

  window.applyDemoMode = async function() {
    const running = !!(status && status.enabled);
    try {
      status = running ? await API.demoStop() : await API.demoStart(readForm());
      toast(status.enabled ? '🎬 Демонстрация запущена' : 'Демонстрация остановлена', 'ok');
      showSettings();
      refreshDemoButton();
    } catch (e) {
      toast('Ошибка: ' + e.message, 'err');
    }
  };

  window.resetDemoIntervals = function() {
    document.querySelectorAll('[data-demo-interval]').forEach(el => { el.value = DEFAULT_INTERVAL; });
    document.querySelectorAll('[data-demo-toggle]').forEach(el => { el.checked = true; });
    saveForm();
    toast('Интервалы сброшены к 30 секундам', 'ok');
  };

  window.toggleDemo = async function() {
    try {
      const current = await API.demoStatus();
      status = current.enabled ? await API.demoStop() : await API.demoStart();
      toast(status.enabled
        ? '🎬 Демонстрация запущена — система работает сама'
        : 'Демонстрация остановлена', 'ok');
      refreshDemoButton();
      if (settingsOpen) showSettings();
    } catch (e) {
      toast('Ошибка: ' + e.message, 'err');
    }
  };

  function refreshDemoButton() {
    const btn = document.getElementById('demo-btn');
    if (!btn) return;
    const running = !!(status && status.enabled);
    btn.classList.toggle('active', running);
    btn.innerHTML = running
      ? '<span class="demo-dot"></span> Демонстрация: ВКЛ'
      : '🎬 Демонстрация';
    btn.title = running
      ? 'Клик — остановить, долгое нажатие или правая кнопка — настройки'
      : 'Клик — запустить, долгое нажатие или правая кнопка — настройки';
  }

  let polling = false;
  async function poll() {
    if (polling) return;
    polling = true;
    try {
      status = await API.demoStatus();
      if (settingsOpen) refreshCounters();
      refreshDemoButton();
    } catch (e) {
    } finally {
      polling = false;
    }
  }

  function startPolling() {
    if (pollTimer) return;
    pollTimer = setInterval(() => {
      const active = (status && status.enabled) || settingsOpen;
      if (active) poll();
    }, 1000);
    poll();
  }

  function mountButton() {
    const footer = document.querySelector('.sidebar-footer');
    if (!footer || document.getElementById('demo-btn')) return;

    const btn = document.createElement('button');
    btn.id = 'demo-btn';
    btn.className = 'demo-btn';
    btn.innerHTML = '🎬 Демонстрация';

    let pressTimer = null;
    let longPressed = false;

    const clearPress = () => {
      if (pressTimer) { clearTimeout(pressTimer); pressTimer = null; }
    };

    btn.addEventListener('click', () => {
      if (longPressed) { longPressed = false; return; }
      window.toggleDemo();
    });
    btn.addEventListener('contextmenu', (e) => {
      e.preventDefault();
      clearPress();
      window.openDemoModal();
    });
    btn.addEventListener('mousedown', () => {
      longPressed = false;
      pressTimer = setTimeout(() => {
        pressTimer = null;
        longPressed = true;
        window.openDemoModal();
      }, 600);
    });
    ['mouseup', 'mouseleave'].forEach(ev => btn.addEventListener(ev, clearPress));

    footer.insertBefore(btn, footer.firstChild);
  }

  function watchModalClose() {
    if (closeWatcher) return;
    closeWatcher = setInterval(() => {
      const modal = document.getElementById('global-modal');
      if (modal && modal.classList.contains('hidden')) settingsOpen = false;
    }, 300);
  }

  document.addEventListener('DOMContentLoaded', () => {
    mountButton();
    startPolling();
    watchModalClose();
  });

  window.refreshDemoStatus = function() {
    API.demoStatus().then(s => { status = s; refreshDemoButton(); if (settingsOpen) refreshCounters(); })
      .catch(() => {});
  };
})();