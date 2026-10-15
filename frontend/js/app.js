(() => {
  const main = document.getElementById('main');
  const toastEl = document.getElementById('toast');
  const clockEl = document.getElementById('clock');
  const loginOverlay = document.getElementById('login-overlay');
  const userInfo = document.getElementById('user-info');

  let products = [];
  let dishes = [];
  let currentTab = 'dashboard';
  let stockFilter = '';
  let stockStateFilter = 'all';
  let logFilter = 'all';
  let logSearch = '';
  let orderDishes = [];
  let supplyFormItems = [];
  let currentUser = null;
  let users = [];
  let editingUser = null;

  // --- Управление темой ---
  window.toggleTheme = function() {
    const html = document.documentElement;
    const current = html.getAttribute('data-theme');
    const next = current === 'dark' ? 'light' : 'dark';
    html.setAttribute('data-theme', next);
    localStorage.setItem('theme', next);
    const lbl = document.getElementById('theme-label');
    if (lbl) lbl.textContent = next === 'dark' ? 'Светлая тема' : 'Тёмная тема';
  };

  // --- Авторизация ---
  window.doLogin = async function(e) {
    e.preventDefault();
    const loginInput = document.getElementById('login-input');
    const passInput = document.getElementById('password-input');
    const errEl = document.getElementById('login-error');
    errEl.style.display = 'none';
    try {
      const sess = await API.login(loginInput.value.trim(), passInput.value);
      currentUser = { login: sess.login, role: sess.role };
      localStorage.setItem('auth_user', JSON.stringify(currentUser));
      loginOverlay.style.display = 'none';
      updateUserUI();
      toast(`Добро пожаловать, ${sess.login}!`, 'ok');
      loadAll();
    } catch (e) {
      errEl.textContent = e.message;
      errEl.style.display = 'block';
    }
  };

  window.doLogout = async function() {
    try { await API.logout(); } catch {}
    currentUser = null;
    loginOverlay.style.display = 'flex';
    document.getElementById('login-input').value = '';
    document.getElementById('password-input').value = '';
    toast('Вы вышли из системы', 'ok');
  };

  function updateUserUI() {
    if (!currentUser) return;
    if (userInfo) {
      userInfo.textContent = `${currentUser.login} (${currentUser.role === 'admin' ? 'админ' : 'сотр'})`;
    }
    document.querySelectorAll('.admin-only').forEach(el => {
      el.style.display = currentUser.role === 'admin' ? '' : 'none';
    });
  }

  window.addEventListener('unauthorized', () => {
    loginOverlay.style.display = 'flex';
  });

  const savedTheme = localStorage.getItem('theme') || 'light';
  document.documentElement.setAttribute('data-theme', savedTheme);
  setTimeout(() => {
    const lbl = document.getElementById('theme-label');
    if (lbl) lbl.textContent = savedTheme === 'dark' ? 'Светлая тема' : 'Тёмная тема';
  }, 100);

  function toast(msg, type) {
    if (!toastEl) return;
    toastEl.textContent = msg;
    toastEl.className = 'toast ' + (type || '');
    clearTimeout(toast._t);
    toast._t = setTimeout(() => { toastEl.className = 'toast hidden'; }, 4000);
  }
  window.showToast = toast;

  function levelBadge(level) {
    const map = { critical: 'critical', warning: 'warning', expired: 'expired', ok: 'ok' };
    const labels = { critical: '⚠️ Критично', warning: '⚡ Внимание', expired: '❌ Просрочено', ok: '✓ Норма' };
    return `<span class="badge ${map[level] || 'ok'}">${labels[level] || level}</span>`;
  }

  function unitKind(unit) {
    const u = (unit || '').toLowerCase().trim();
    if (['кг', 'г', 'гр', 'грамм', 'граммов'].includes(u)) return 'weight';
    if (['л', 'мл', 'литр', 'литров'].includes(u)) return 'volume';
    return 'count';
  }

  function trimNum(v) {
    return String(Math.round(v * 1000) / 1000);
  }

  // Форматирует количество: 1.5 кг / 250 г / 2 л / 400 мл / 12 шт.
  function fmtQty(qty, unit) {
    const q = Number(qty) || 0;
    const kind = unitKind(unit);
    if (kind === 'weight') {
      const g = Math.round(q * 1000);
      return g < 1000 ? `${g} г` : `${trimNum(q)} кг`;
    }
    if (kind === 'volume') {
      const ml = Math.round(q * 1000);
      return ml < 1000 ? `${ml} мл` : `${trimNum(q)} л`;
    }
    return `${Math.round(q)} шт`;
  }

  // Разбирает ввод формы приёмки (кг/г, л/мл, шт) во внутреннюю единицу.
  function parseSupplyQty(kind, mainVal, subVal) {
    if (kind === 'count') return Math.round(mainVal);
    return Math.round((mainVal + subVal / 1000) * 1000) / 1000;
  }

  function daysColor(days) {
    if (days <= 0) return '#dc2626';
    if (days <= 1) return '#ef4444';
    if (days <= 3) return '#f59e0b';
    if (days <= 7) return '#3b82f6';
    return '#059669';
  }

  setInterval(() => {
    if (clockEl) clockEl.textContent = new Date().toLocaleString('ru-RU');
  }, 1000);
  if (clockEl) clockEl.textContent = new Date().toLocaleString('ru-RU');

  async function loadAll() {
    try {
      [products, dishes] = await Promise.all([API.getProducts(), API.getDishes()]);
      render(currentTab);
    } catch (e) {
      toast('Ошибка загрузки: ' + e.message, 'err');
    }
  }

  const titles = {
    dashboard: 'Операционная сводка',
    stock: 'Управление складом',
    products: 'Справочник продуктов',
    orders: 'Касса и заказы',
    supplies: 'Приёмка поставок',
    schedule: 'График поставок',
    promotions: 'Акции и скидки',
    stoplist: 'Стоп-лист и закупки',
    hot: 'Горячие предложения',
    analytics: 'Аналитика и прогнозы',
    logs: 'Журнал системных событий'
  };

  document.addEventListener('DOMContentLoaded', () => {
    document.querySelectorAll('.tab').forEach(btn => {
      btn.addEventListener('click', () => {
        const tabName = btn.dataset.tab;
        if (tabName) window.switchTab(tabName);
      });
    });
  });

window.switchTab = function(tabName) {
    currentTab = tabName;
    document.querySelectorAll('.tab').forEach(b => {
      if (b.dataset.tab === tabName) {
        b.classList.add('bg-gradient-to-r', 'from-brand-600/30', 'to-accent-purple/30', 'text-white', 'border-l-4', 'border-brand-500');
        b.classList.remove('text-slate-400');
      } else {
        b.classList.remove('bg-gradient-to-r', 'from-brand-600/30', 'to-accent-purple/30', 'text-white', 'border-l-4', 'border-brand-500');
        b.classList.add('text-slate-400');
      }
    });
    render(tabName);
  };

  function render(tab) {
    const handlers = {
      dashboard: renderDashboard,
      stock: renderStock,
      products: renderProducts,
      orders: renderOrders,
      supplies: renderSupplies,
      schedule: renderSupplySchedule,
      promotions: renderPromotions,
      stoplist: renderStopList,
      hot: renderHot,
      analytics: renderAnalytics,
      logs: renderLogs,
      users: renderUsers
    };

    const mainContainer = document.querySelector('.main-content');
    if (mainContainer) {
      mainContainer.scrollTop = 0;
    }

    (handlers[tab] || renderDashboard)();
  }

  window.openModal = function(title, htmlContent) {
    document.getElementById('modal-title').textContent = title;
    document.getElementById('modal-body').innerHTML = htmlContent;
    document.getElementById('global-modal').classList.remove('hidden');
  };

  window.closeModal = function(e) {
    if (!e || e.target.id === 'global-modal' || e.target.classList.contains('close-btn')) {
      document.getElementById('global-modal').classList.add('hidden');
    }
  };

  // ===================== ДАШБОРД =====================
  async function renderDashboard() {
    try {
      const d = await API.getDashboard();
      const cards = [
        { num: d.products, lbl: 'Продуктов в базе', cls: 'blue', tab: 'stock' },
        { num: d.dishes, lbl: 'Всего блюд', cls: '', tab: 'hot' },
        { num: d.dishes_available, lbl: 'Доступно к заказу', cls: 'green', tab: 'orders' },
        { num: d.dishes_stopped, lbl: 'В стоп-листе', cls: 'red', tab: 'stoplist' },
        { num: d.batches, lbl: 'Активных партий', cls: '', tab: 'stock' },
        { num: d.critical, lbl: '⚠️ Критичные остатки', cls: 'red', tab: 'stock' },
        { num: d.warning, lbl: '⚡ Скоропортящиеся', cls: 'orange', tab: 'stock' },
        { num: d.expired, lbl: '❌ Просрочено', cls: 'red', tab: 'stock' },
        { num: d.supplies, lbl: 'Всего поставок', cls: 'blue', tab: 'supplies' },
        { num: d.orders, lbl: 'Выполнено заказов', cls: 'green', tab: 'orders' },
        { num: d.writeoffs, lbl: 'Акт. списаний', cls: '', tab: 'analytics' },
      ];

      let html = '<div class="card"><h2>📊 Операционная сводка ресторана</h2>';
      html += `<p style="color:var(--text-muted);margin-bottom:8px">Остатки на складе (вес • объём • штуки): <strong>${d.stock_display || '—'}</strong></p>`;
      html += `<p style="color:var(--text-muted);margin-bottom:16px">Всего списано: <strong>${d.writeoff_display || '—'}</strong> • Синхронизировано: ${d.server_time || ''}</p>`;
      html += '<div class="grid">';
      cards.forEach(c => {
        html += `<div class="stat-card ${c.cls}" onclick="switchTab('${c.tab}')"><span class="num">${c.num}</span><span class="lbl">${c.lbl}</span></div>`;
      });
      html += '</div></div>';

      html += `<div class="card"><h2>⚡ Быстрые действия</h2>
        <div style="display:flex;gap:12px;flex-wrap:wrap">
          <button class="btn danger" onclick="doAutoWriteoff()">🗑 Авто-списание просрочки</button>
          <button class="btn primary" onclick="openOrderModal()">🛒 Быстрый заказ</button>
          <button class="btn secondary" onclick="openSupplyModal()">🚚 Принять поставку</button>
          <button class="btn secondary" onclick="runAnalyticsTest()">🧪 Стресс-тест аналитики</button>
        </div></div>`;

      main.innerHTML = html;
    } catch (e) {
      toast('Ошибка дашборда: ' + e.message, 'err');
    }
  }

  // ===================== СКЛАД =====================
  async function renderStock() {
    try {
      const stock = await API.getStock();

      const totalProds = stock.length;
      let expCount = 0;
      let critCount = 0;

      stock.forEach(s => {
        if (s.days_to_expiry <= 0) expCount++;
        else if (s.days_to_expiry <= 2) critCount++;
      });

      let html = `
        <div class="stock-summary-grid">
          <div class="card" style="margin:0;padding:16px;text-align:center">
            <span style="font-size:12px;color:var(--text-muted)">Всего позиций</span>
            <div style="font-size:24px;font-weight:700">${totalProds}</div>
          </div>
          <div class="card" style="margin:0;padding:16px;text-align:center">
            <span style="font-size:12px;color:var(--text-muted)">⚡ Скоро истекают (≤ 2 дн.)</span>
            <div style="font-size:24px;font-weight:700;color:var(--warning)">${critCount}</div>
          </div>
          <div class="card" style="margin:0;padding:16px;text-align:center">
            <span style="font-size:12px;color:var(--text-muted)">❌ Просрочено</span>
            <div style="font-size:24px;font-weight:700;color:var(--danger)">${expCount}</div>
          </div>
        </div>

        <div class="card">
          <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:20px;flex-wrap:wrap;gap:12px">
            <h2>📋 Инвентаризация и остатки партий</h2>
            <div class="filter-bar" style="margin-bottom:0">
              <input type="text" placeholder="🔍 Поиск по названию..." value="${stockFilter}" oninput="window.updateStockSearch(this.value)">
              <select onchange="window.setStockStateFilter(this.value)">
                <option value="all" ${stockStateFilter === 'all' ? 'selected' : ''}>Все состояния</option>
                <option value="expiring" ${stockStateFilter === 'expiring' ? 'selected' : ''}>⚡ Скоро истекает</option>
                <option value="expired" ${stockStateFilter === 'expired' ? 'selected' : ''}>❌ Просрочено</option>
                <option value="low" ${stockStateFilter === 'low' ? 'selected' : ''}>⚠️ Малый остаток</option>
              </select>
            </div>
          </div>
      `;

      const filtered = stock.filter(s => {
        const matchesName = s.product.toLowerCase().includes(stockFilter.toLowerCase());
        let matchesState = true;
        if (stockStateFilter === 'expiring') matchesState = s.days_to_expiry > 0 && s.days_to_expiry <= 3;
        if (stockStateFilter === 'expired') matchesState = s.days_to_expiry <= 0;
        if (stockStateFilter === 'low') matchesState = s.total_qty <= 3;
        return matchesName && matchesState;
      });

      if (filtered.length === 0) {
        html += '<p class="empty">Продукты не найдены</p>';
      } else {
        filtered.forEach(s => {
          const days = s.days_to_expiry;
          const percent = Math.min(Math.max(Math.round((days / 10) * 100), 5), 100);
          const barColor = daysColor(days);

          html += `
            <div class="stock-item-row" id="stock-row-${s.product_id}">
              <div class="stock-item-header" onclick="window.toggleStockRow(${s.product_id})">
                <div class="stock-item-title">
                  <span class="arrow-icon">▼</span>
                  <strong>${s.product}</strong>
                </div>
                
                <div class="stock-item-stats">
                  <div>Остаток: <strong style="color:var(--text-main)">${s.total_qty} ${s.unit}</strong></div>
                  <div>Партий: <strong>${s.batches ? s.batches.length : 0}</strong></div>
                  
                  <div style="display:flex;align-items:center;gap:8px">
                    <span style="font-size:12px">Срок:</span>
                    <div class="freshness-bar-wrap" title="Осталось ${days} дней">
                      <div class="freshness-bar" style="width:${days <= 0 ? 100 : percent}%;background:${barColor}"></div>
                    </div>
                    <span style="color:${barColor};font-weight:600;min-width:55px">${days <= 0 ? 'Истёк' : days + ' дн.'}</span>
                  </div>

                  <div>${levelBadge(days <= 0 ? 'expired' : days <= 1 ? 'critical' : days <= 3 ? 'warning' : 'ok')}</div>
                </div>
              </div>

              <div class="stock-batches-body">
                <h4 style="font-size:13px;margin-bottom:10px;color:var(--text-muted)">Активные партии на складе:</h4>
                <table>
                  <thead>
                    <tr>
                      <th>№ Партии</th>
                      <th>Остаток</th>
                      <th>Дата прихода</th>
                      <th>Срок годности</th>
                      <th>Статус партии</th>
                    </tr>
                  </thead>
                  <tbody>
          `;

          if (s.batches && s.batches.length > 0) {
            s.batches.forEach(b => {
              const bDays = Math.round(((new Date(b.expiry_date) - new Date()) / (1000 * 60 * 60 * 24)) * 10) / 10;
              const canEdit = currentUser && currentUser.role === 'admin';
              const actions = canEdit ? `
                <div style="display:flex;gap:4px;margin-top:6px">
                  <button class="btn-xs" onclick="editBatchQty(${b.id},'${b.unit}')">✏️ Остаток</button>
                  <button class="btn-xs" onclick="editBatchExpiry(${b.id})">📅 Срок</button>
                  <button class="btn-xs" onclick="manualWriteoffBatch(${b.id},'${b.unit}')">🗑 Списать</button>
                  ${API.isAdmin() ? `<button class="btn-xs danger" onclick="deleteBatch(${b.id})">✕ Удалить</button>` : ''}
                </div>` : '';
              html += `
                <tr>
                  <td><strong>${b.batch_number}</strong></td>
                  <td>${fmtQty(b.qty, b.unit)} / ${fmtQty(b.initial_qty || b.qty, b.unit)}</td>
                  <td>${b.arrival_date ? new Date(b.arrival_date).toLocaleDateString('ru-RU') : '—'}</td>
                  <td style="color:${daysColor(bDays)};font-weight:600">${new Date(b.expiry_date).toLocaleDateString('ru-RU')} (${bDays <= 0 ? 'Просрочено' : bDays + ' дн.'})</td>
                  <td>${levelBadge(bDays <= 0 ? 'expired' : bDays <= 1 ? 'critical' : bDays <= 3 ? 'warning' : 'ok')}</td>
                </tr>
                ${actions ? `<tr><td colspan="5" style="padding:0 0 8px 16px">${actions}</td></tr>` : ''}
              `;
            });
          } else {
            html += '<tr><td colspan="5" class="empty" style="padding:10px">Нет активных партий</td></tr>';
          }

          html += `
                  </tbody>
                </table>
              </div>
            </div>
          `;
        });
      }

      html += '</div>';
      main.innerHTML = html;
    } catch (e) {
      toast('Ошибка загрузки склада: ' + e.message, 'err');
    }
  }

  window.toggleStockRow = function(id) {
    const row = document.getElementById(`stock-row-${id}`);
    if (row) row.classList.toggle('open');
  };

  window.updateStockSearch = function(val) {
    stockFilter = val;
    renderStock();
  };

  window.setStockStateFilter = function(val) {
    stockStateFilter = val;
    renderStock();
  };

  // ===================== РЕДАКТИРОВАНИЕ ПАРТИЙ (инвентаризация) =====================
  function qtyInputFields(kind, idPrefix, initial) {
    if (kind === 'count') {
      return `<div class="form-group"><label>Количество (шт)</label>
        <input type="number" id="${idPrefix}-main" value="${Math.round(initial)}" min="0" step="1"></div>`;
    }
    if (kind === 'weight') {
      const kg = Math.floor(initial);
      const g = Math.round((initial - kg) * 1000);
      return `<div class="form-row">
        <div class="form-group"><label>Килограммы</label><input type="number" id="${idPrefix}-kg" value="${kg}" min="0"></div>
        <div class="form-group"><label>Грамм</label><input type="number" id="${idPrefix}-g" value="${g}" min="0" max="999"></div>
      </div>`;
    }
    const l = Math.floor(initial);
    const ml = Math.round((initial - l) * 1000);
    return `<div class="form-row">
      <div class="form-group"><label>Литры</label><input type="number" id="${idPrefix}-l" value="${l}" min="0"></div>
      <div class="form-group"><label>Миллилитры</label><input type="number" id="${idPrefix}-ml" value="${ml}" min="0" max="999"></div>
    </div>`;
  }

  function readQtyInput(kind, idPrefix) {
    if (kind === 'count') {
      return Math.round(parseFloat(document.getElementById(idPrefix + '-main')?.value) || 0);
    }
    if (kind === 'weight') {
      const kg = parseFloat(document.getElementById(idPrefix + '-kg')?.value) || 0;
      const g = parseFloat(document.getElementById(idPrefix + '-g')?.value) || 0;
      return Math.round((kg + g / 1000) * 1000) / 1000;
    }
    const l = parseFloat(document.getElementById(idPrefix + '-l')?.value) || 0;
    const ml = parseFloat(document.getElementById(idPrefix + '-ml')?.value) || 0;
    return Math.round((l + ml / 1000) * 1000) / 1000;
  }

  window.editBatchQty = function(batchId, unit) {
    const kind = unitKind(unit);
    const html = `
      <p style="color:var(--text-muted);margin-bottom:12px">Укажите новый остаток партии. Для штук — только целые значения.</p>
      ${qtyInputFields(kind, 'eq')}
      <div class="form-group"><label>Причина корректировки</label>
        <input type="text" id="eq-reason" placeholder="Инвентаризация, бой, усушка..."></div>
      <button class="btn primary" style="width:100%" onclick="submitEditBatchQty(${batchId},'${unit}')">💾 Сохранить</button>
    `;
    openModal('Корректировка остатка', html);
  };

  window.submitEditBatchQty = async function(batchId, unit) {
    const kind = unitKind(unit);
    const qty = readQtyInput(kind, 'eq');
    const reason = document.getElementById('eq-reason')?.value || 'Ручная корректировка';
    try {
      await API.adjustBatch(batchId, qty, reason);
      toast('Остаток обновлён', 'ok');
      closeModal();
      renderStock();
    } catch (e) { toast('Ошибка: ' + e.message, 'err'); }
  };

  window.editBatchExpiry = function(batchId) {
    const html = `
      <p style="color:var(--text-muted);margin-bottom:12px">Изменение срока годности (только администратор).</p>
      <div class="form-group"><label>Новая дата</label>
        <input type="date" id="be-date" value="${new Date().toISOString().slice(0,10)}"></div>
      <button class="btn primary" style="width:100%" onclick="submitEditBatchExpiry(${batchId})">💾 Сохранить</button>
    `;
    openModal('Изменение срока годности', html);
  };

  window.submitEditBatchExpiry = async function(batchId) {
    const v = document.getElementById('be-date')?.value;
    if (!v) { toast('Укажите дату', 'err'); return; }
    try {
      await API.setExpiry(batchId, v);
      toast('Срок обновлён', 'ok');
      closeModal();
      renderStock();
    } catch (e) { toast('Ошибка: ' + e.message, 'err'); }
  };

  window.manualWriteoffBatch = function(batchId, unit) {
    const kind = unitKind(unit);
    const html = `
      <p style="color:var(--text-muted);margin-bottom:12px">Списание части партии (порча, бой, инвентаризация).</p>
      ${qtyInputFields(kind, 'wo')}
      <div class="form-group"><label>Причина</label>
        <input type="text" id="wo-reason" value="Порча" placeholder="Порча, бой, возврат..."></div>
      <button class="btn danger" style="width:100%" onclick="submitManualWriteoff(${batchId},'${unit}')">🗑 Списать</button>
    `;
    openModal('Ручное списание', html);
  };

  window.submitManualWriteoff = async function(batchId, unit) {
    const kind = unitKind(unit);
    const qty = readQtyInput(kind, 'wo');
    const reason = document.getElementById('wo-reason')?.value || 'Ручное списание';
    try {
      await API.writeoffBatch(batchId, qty, reason);
      toast('Списано', 'ok');
      closeModal();
      renderStock();
    } catch (e) { toast('Ошибка: ' + e.message, 'err'); }
  };

  window.deleteBatch = async function(batchId) {
    if (!confirm('Удалить партию безвозвратно?')) return;
    try {
      await API.deleteBatch(batchId);
      toast('Партия удалена', 'ok');
      renderStock();
    } catch (e) { toast('Ошибка: ' + e.message, 'err'); }
  };

  window.cancelOrder = async function(orderId) {
    if (!confirm('Отменить заказ и вернуть продукты на склад?')) return;
    try {
      await API.cancelOrder(orderId);
      toast('Заказ отменён', 'ok');
      renderOrders();
    } catch (e) { toast('Ошибка: ' + e.message, 'err'); }
  };

  // ===================== ПОЛЬЗОВАТЕЛИ =====================
  async function renderUsers() {
    try {
      users = await API.getUsers();
      let html = `<div class="card">
        <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:16px">
          <h2>👥 Управление пользователями</h2>
          <button class="btn primary" onclick="openUserForm()">+ Новый пользователь</button>
        </div>
        <table><thead><tr><th>Логин</th><th>Роль</th><th></th></tr></thead><tbody>`;
      users.forEach(u => {
        const role = u.role === 'admin' ? '👑 Администратор' : '👤 Сотрудник';
        html += `<tr>
          <td><strong>${u.login}</strong></td>
          <td>${role}</td>
          <td style="text-align:right">
            <button class="btn-xs" onclick="openUserForm('${u.login}','${u.role}')">✏️ Изменить</button>
            ${u.login !== currentUser?.login ? `<button class="btn-xs danger" onclick="deleteUser('${u.login}')">✕ Удалить</button>` : ''}
          </td>
        </tr>`;
      });
      html += '</tbody></table></div>';
      main.innerHTML = html;
    } catch (e) {
      toast('Ошибка загрузки пользователей: ' + e.message, 'err');
    }
  }

  window.openUserForm = function(login, role) {
    editingUser = login || null;
    const isEdit = !!editingUser;
    const html = `
      <div class="form-group"><label>Логин</label>
        <input type="text" id="uf-login" value="${login || ''}" ${isEdit ? 'readonly' : ''}></div>
      <div class="form-group"><label>${isEdit ? 'Новый пароль (пусто — не менять)' : 'Пароль'}</label>
        <input type="password" id="uf-pass"></div>
      <div class="form-group"><label>Роль</label>
        <select id="uf-role">
          <option value="staff" ${role === 'staff' || !role ? 'selected' : ''}>Сотрудник</option>
          <option value="admin" ${role === 'admin' ? 'selected' : ''}>Администратор</option>
        </select></div>
      <button class="btn primary" style="width:100%" onclick="saveUserForm()">💾 Сохранить</button>
    `;
    openModal(isEdit ? 'Редактирование пользователя' : 'Новый пользователь', html);
  };

  window.saveUserForm = async function() {
    const login = document.getElementById('uf-login').value.trim();
    const pass = document.getElementById('uf-pass').value;
    const role = document.getElementById('uf-role').value;
    if (!login) { toast('Укажите логин', 'err'); return; }
    try {
      await API.saveUser(login, pass, role);
      toast('Пользователь сохранён', 'ok');
      closeModal();
      renderUsers();
    } catch (e) { toast('Ошибка: ' + e.message, 'err'); }
  };

  window.deleteUser = async function(login) {
    if (!confirm(`Удалить пользователя ${login}?`)) return;
    try {
      await API.deleteUser(login);
      toast('Пользователь удалён', 'ok');
      renderUsers();
    } catch (e) { toast('Ошибка: ' + e.message, 'err'); }
  };

  // ===================== ЗАКАЗЫ =====================
  window.openOrderModal = function() {
    orderDishes = [];
    const html = `
      <div class="form-row" style="flex-direction:column;align-items:stretch">
        <div class="form-group"><label>Выберите блюдо</label><select id="modal-order-dish"><option value="">— Выбрать —</option>${dishes.map(d => `<option value="${d.id}">${d.name} (${d.price} ₽)</option>`).join('')}</select></div>
        <div class="form-group"><label>Порций</label><input type="number" id="modal-order-qty" value="1" min="1"></div>
        <button class="btn secondary" onclick="modalAddOrderItem()">➕ Добавить в заказ</button>
      </div>
      <div style="max-height:180px;overflow-y:auto;margin:12px 0"><table id="modal-order-table"><thead><tr><th>Блюдо</th><th>Порций</th></tr></thead><tbody id="modal-order-tbody"><tr><td colspan="2" class="empty" style="padding:10px">Пусто</td></tr></tbody></table></div>
      <button class="btn primary" style="width:100%" onclick="submitModalOrder()">✅ Оформить заказ</button>
    `;
    openModal('Оформление заказа', html);
  };

  window.modalAddOrderItem = function() {
    const sel = document.getElementById('modal-order-dish');
    const qty = parseInt(document.getElementById('modal-order-qty').value) || 1;
    if (!sel.value || qty <= 0) return;
    const dish = dishes.find(d => d.id === parseInt(sel.value));
    if (!dish) return;
    const ex = orderDishes.find(i => i.dish_id === dish.id);
    if (ex) ex.qty += qty;
    else orderDishes.push({ dish_id: dish.id, qty });
    
    const tbody = document.getElementById('modal-order-tbody');
    if (tbody) {
      tbody.innerHTML = orderDishes.map(item => {
        const d = dishes.find(x => x.id === item.dish_id);
        return `<tr><td>${d ? d.name : ''}</td><td>${item.qty}</td></tr>`;
      }).join('');
    }
  };

  window.submitModalOrder = async function() {
    if (orderDishes.length === 0) {
      toast('Добавьте блюда', 'err');
      return;
    }
    try {
      const order = await API.createOrder(orderDishes);
      toast(`Заказ ${order.number} оформлен!`, 'ok');
      closeModal();
      renderOrders();
    } catch (e) {
      toast('Ошибка: ' + e.message, 'err');
    }
  };

  let currentOrderId = null;
  let currentOrder = null;

  window.openOrderDetail = async function(orderId) {
    try {
      currentOrderId = orderId;
      const order = await API.getOrderByID(orderId);
      currentOrder = order;
      renderOrderDetail(order);
    } catch (e) {
      toast('Ошибка загрузки заказа: ' + e.message, 'err');
    }
  };

  function renderOrderDetail(order) {
    const statuses = [
      { val: 'new', label: '🆕 Новый' },
      { val: 'cooking', label: '👨‍🍳 Готовится' },
      { val: 'served', label: '🍽️ Подан' },
      { val: 'done', label: '✅ Выполнен' },
      { val: 'cancelled', label: '❌ Отменён' }
    ];

    let html = `
      <div style="margin-bottom:16px">
        <h3 style="font-size:18px;font-weight:700;margin-bottom:8px">${order.number} — ${order.status}</h3>
        <p style="color:var(--text-muted);font-size:13px">Создан: ${new Date(order.created_at).toLocaleString('ru-RU')}</p>
        <p style="color:var(--text-muted);font-size:13px">Итого: <strong style="color:var(--success)">${order.total.toFixed(2)} ₽</strong>${order.discount > 0 ? ` (скидка ${order.discount}% — ${order.discount_reason})` : ''}</p>
      </div>
    `;

    html += '<div style="margin-bottom:20px"><h4 style="font-size:14px;font-weight:600;margin-bottom:8px;color:var(--text-muted)">Статус заказа</h4><div style="display:flex;gap:8px;flex-wrap:wrap">';
    statuses.forEach(s => {
      const active = order.status === s.val ? 'style="background:var(--primary);color:#fff;border-color:var(--primary)"' : '';
      html += `<button class="btn-xs" ${active} onclick="window.changeOrderStatus(${order.id},'${s.val}')">${s.label}</button>`;
    });
    html += '</div></div>';

    html += '<h4 style="font-size:14px;font-weight:600;margin-bottom:8px;color:var(--text-muted)">Состав заказа</h4>';
    if (order.items && order.items.length > 0) {
      order.items.forEach((item, idx) => {
        const dish = dishes.find(d => d.id === item.dish_id);
        html += `<div class="suggestion-item" style="margin-bottom:12px;padding:12px">
          <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:8px">
            <strong>${dish ? dish.name : '??'} ×${item.qty}</strong>
            <span style="font-weight:700;color:var(--success)">${(dish ? dish.price * item.qty : 0).toFixed(2)} ₽</span>
          </div>`;

        if (dish && dish.ingredients && dish.ingredients.length > 0) {
          html += '<div style="font-size:12px;color:var(--text-muted);margin-bottom:8px">Ингредиенты:</div>';
          html += '<div style="display:flex;flex-wrap:wrap;gap:6px;margin-bottom:10px">';
          dish.ingredients.forEach(ing => {
            const prod = products.find(p => p.id === ing.product_id);
            if (!prod) return;
            const removable = trimNum(ing.qty * item.qty);
            const chip = `<span style="font-size:11px;background:var(--border-color);padding:3px 8px;border-radius:6px">${prod.name}: ${fmtQty(removable, prod.unit)}</span>`;
            html += (order.status === 'new' || order.status === 'cooking')
              ? `<button class="btn-xs" title="Убрать из блюда" style="padding:3px 8px;font-size:11px"
                   onclick="openCustomizeRemove(${order.id},${item.dish_id},${ing.product_id},${removable})">${chip} ➖</button>`
              : chip;
          });
          html += '</div>';
        }

        if (item.customizations && item.customizations.length > 0) {
          html += '<div style="font-size:12px;color:var(--text-muted);margin-bottom:6px">Кастомизации:</div>';
          item.customizations.forEach(c => {
            const color = c.action === 'add' ? 'var(--success)' : 'var(--danger)';
            html += `<div style="font-size:12px;color:${color};margin-bottom:4px">
              ${c.action === 'add' ? '➕' : '➖'} ${c.product_name} ${c.display} ${c.price >= 0 ? '+' : ''}${c.price.toFixed(2)} ₽
            </div>`;
          });
        }

        if (order.status === 'new' || order.status === 'cooking') {
          html += `<div style="display:flex;gap:6px;margin-top:8px;flex-wrap:wrap">
            <button class="btn-xs" onclick="openCustomizeAdd(${order.id},${item.dish_id})">➕ Добавить ингредиент</button>
          </div>`;
        }

        html += '</div>';
      });
    } else {
      html += '<p class="empty" style="padding:20px">Нет позиций</p>';
    }

    if (order.status === 'new' || order.status === 'cooking') {
      html += `<button class="btn secondary" style="width:100%;margin-top:12px" onclick="openAddDishToOrder(${order.id})">➕ Добавить блюдо к заказу</button>`;
    }

    openModal(`Заказ ${order.number}`, html);
  }

  window.changeOrderStatus = async function(orderId, status) {
    try {
      await API.updateOrderStatus(orderId, status);
      toast(`Статус: ${status}`, 'ok');
      closeModal();
      renderOrders();
    } catch (e) {
      toast('Ошибка: ' + e.message, 'err');
    }
  };

  window.openCustomizeAdd = function(orderId, dishId) {
    const html = `
      <p style="color:var(--text-muted);margin-bottom:12px">Выберите ингредиент и количество для добавления в блюдо заказа.</p>
      <div class="form-group"><label>Продукт</label>
        <select id="cust-prod" onchange="window.updateCustQtyFields('add')">
          <option value="">— Выбрать —</option>${products.map(p => `<option value="${p.id}" data-kind="${p.kind}">${p.name} (${p.unit})</option>`).join('')}
        </select></div>
      <div id="cust-add-qty-fields"></div>
      <button class="btn primary" style="width:100%" onclick="submitCustomize(${orderId},${dishId},'add')">➕ Добавить</button>
    `;
    openModal('Добавить ингредиент', html);
    setTimeout(() => updateCustQtyFields('add'), 50);
  };

  window.openCustomizeRemove = function(orderId, dishId, productId, defaultQty) {
    const prod = products.find(p => p.id === productId);
    if (!prod) return;
    const kind = unitKind(prod.unit);
    const html = `
      <p style="color:var(--text-muted);margin-bottom:12px">Убрать ингредиент «${prod.name}» из блюда. Вернём деньги за него.</p>
      ${qtyInputFields(kind, 'cr', defaultQty)}
      <button class="btn danger" style="width:100%" onclick="submitCustomize(${orderId},${dishId},'remove',${productId})">➖ Убрать</button>
    `;
    openModal('Убрать ингредиент', html);
  };

  window.updateCustQtyFields = function(action) {
    const sel = document.getElementById('cust-prod');
    if (!sel) return;
    const opt = sel.options[sel.selectedIndex];
    const kind = opt && opt.dataset ? opt.dataset.kind : 'count';
    const container = document.getElementById('cust-add-qty-fields');
    if (!container) return;
    const prefix = action === 'add' ? 'ca' : 'cr';
    if (kind === 'count') {
      container.innerHTML = `<div class="form-group"><label>Количество (шт)</label><input type="number" id="${prefix}-main" value="1" min="1"></div>`;
    } else if (kind === 'weight') {
      container.innerHTML = `<div class="form-row">
        <div class="form-group"><label>Килограммы</label><input type="number" id="${prefix}-kg" value="0" min="0"></div>
        <div class="form-group"><label>Грамм</label><input type="number" id="${prefix}-g" value="50" min="0" max="999"></div>
      </div>`;
    } else {
      container.innerHTML = `<div class="form-row">
        <div class="form-group"><label>Литры</label><input type="number" id="${prefix}-l" value="0" min="0"></div>
        <div class="form-group"><label>Миллилитры</label><input type="number" id="${prefix}-ml" value="100" min="0" max="999"></div>
      </div>`;
    }
  };

  window.submitCustomize = async function(orderId, dishId, action, productId) {
    const sel = document.getElementById('cust-prod');
    const pid = productId || (sel && parseInt(sel.value));
    if (!pid) { toast('Выберите продукт', 'err'); return; }
    const prod = products.find(p => p.id === pid);
    if (!prod) { toast('Продукт не найден', 'err'); return; }
    const qty = readQtyInput(unitKind(prod.unit), action === 'add' ? 'ca' : 'cr');
    if (qty <= 0) { toast('Укажите количество', 'err'); return; }
    try {
      const apiFn = action === 'add' ? API.customizeAdd : API.customizeRemove;
      const res = await apiFn(orderId, dishId, pid, qty);
      toast(`${action === 'add' ? 'Добавлено' : 'Убрано'}: ${res.customization.product_name} ${res.customization.display} (${res.delta >= 0 ? '+' : ''}${res.delta.toFixed(2)} ₽)`, 'ok');
      closeModal();
      openOrderDetail(orderId);
    } catch (e) {
      toast('Ошибка: ' + e.message, 'err');
    }
  };

  window.openAddDishToOrder = function(orderId) {
    const html = `
      <p style="color:var(--text-muted);margin-bottom:12px">Добавить блюдо к заказу ${currentOrder ? currentOrder.number : '#' + orderId}.</p>
      <div class="form-group"><label>Блюдо</label>
        <select id="add-dish-sel"><option value="">— Выбрать —</option>${dishes.map(d => `<option value="${d.id}">${d.name} (${d.price} ₽)</option>`).join('')}</select></div>
      <div class="form-group"><label>Порций</label><input type="number" id="add-dish-qty" value="1" min="1"></div>
      <button class="btn primary" style="width:100%" onclick="submitAddDishToOrder(${orderId})">➕ Добавить</button>
    `;
    openModal('Добавить блюдо', html);
  };

  window.submitAddDishToOrder = async function(orderId) {
    const sel = document.getElementById('add-dish-sel');
    const qty = parseInt(document.getElementById('add-dish-qty').value) || 1;
    if (!sel.value || qty <= 0) { toast('Выберите блюдо и укажите порции', 'err'); return; }
    try {
      toast('Добавление блюда к существующему заказу — создайте новый заказ с нужными блюдами', 'ok');
      closeModal();
    } catch (e) {
      toast('Ошибка: ' + e.message, 'err');
    }
  };

  async function renderOrders() {
    try {
      const orders = await API.getOrders();
      let html = '<div class="card"><div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:16px"><h2>🛒 Управление заказами</h2><button class="btn primary" onclick="openOrderModal()">+ Новый заказ</button></div>';
      
      if (orders.length > 0) {
        const canCancel = API.isAdmin();
        const canEdit = API.isAdmin();
        html += `<table><thead><tr><th>№</th><th>Состав</th><th>Порций</th><th>Сумма</th><th>Статус</th><th>Время</th><th></th></tr></thead><tbody>`;
        orders.slice(0, 20).forEach(o => {
          const items = o.items.map(i => {
            const dish = dishes.find(d => d.id === i.dish_id);
            return `${dish ? dish.name : '??'} ×${i.qty}`;
          }).join(', ');
          const total = o.items.reduce((s, i) => s + i.qty, 0);
          const dt = new Date(o.created_at);
          const statusLabels = { new: '🆕', cooking: '👨‍🍳', served: '🍽️', done: '✅', cancelled: '❌' };
          const statusLabel = statusLabels[o.status] || '🆕';
          html += `<tr>
            <td><strong>${o.number}</strong></td>
            <td style="max-width:200px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="${items}">${items}</td>
            <td>${total}</td>
            <td style="font-weight:600;color:var(--success)">${o.total.toFixed(2)} ₽</td>
            <td>${statusLabel} ${o.status}</td>
            <td>${dt.toLocaleTimeString('ru-RU')}</td>
            <td style="text-align:right;white-space:nowrap">
              <button class="btn-xs" onclick="openOrderDetail(${o.id})">👁️</button>
              ${canCancel ? `<button class="btn-xs danger" onclick="cancelOrder(${o.id})">✕</button>` : ''}
            </td>
          </tr>`;
        });
        html += '</tbody></table>';
      } else {
        html += '<p class="empty">Нет заказов</p>';
      }
      html += '</div>';
      main.innerHTML = html;
    } catch (e) {
      toast('Ошибка заказов: ' + e.message, 'err');
    }
  }

  // ===================== ПОСТАВКИ =====================
  window.openSupplyModal = function() {
    supplyFormItems = [];
    const html = `
      <div class="form-row" style="flex-direction:column;align-items:stretch">
        <div class="form-group">
          <label>Продукт</label>
          <select id="modal-sup-prod" onchange="window.updateSupplyQtyFields()">
            <option value="">— Выбрать —</option>${products.map(p => `<option value="${p.id}" data-kind="${p.kind}">${p.name} (${p.unit})</option>`).join('')}
          </select>
        </div>
        <div id="modal-sup-qty-fields"></div>
        <div class="form-group"><label>Срок хранения (дней)</label><input type="number" id="modal-sup-days" value="7" min="1"></div>
        <button class="btn secondary" onclick="modalAddSupplyItem()">➕ Добавить продукт</button>
      </div>
      <div style="max-height:180px;overflow-y:auto;margin:12px 0"><table id="modal-sup-table"><thead><tr><th>Продукт</th><th>Кол-во</th><th>Срок</th></tr></thead><tbody id="modal-sup-tbody"><tr><td colspan="3" class="empty" style="padding:10px">Пусто</td></tr></tbody></table></div>
      <button class="btn primary" style="width:100%" onclick="submitModalSupply()">✅ Принять поставку</button>
    `;
    openModal('Приёмка поставки', html);
    setTimeout(() => updateSupplyQtyFields(), 50);
  };

  window.updateSupplyQtyFields = function() {
    const sel = document.getElementById('modal-sup-prod');
    if (!sel) return;
    const opt = sel.options[sel.selectedIndex];
    const kind = opt && opt.dataset ? opt.dataset.kind : 'count';
    const container = document.getElementById('modal-sup-qty-fields');
    if (!container) return;

    let html = '';
    if (kind === 'count') {
      html = `<div class="form-group"><label>Количество (шт)</label><input type="number" id="modal-sup-qty-main" value="10" min="1"></div>`;
    } else if (kind === 'weight') {
      html = `<div class="form-row">
        <div class="form-group"><label>Килограммы</label><input type="number" id="modal-sup-kg" value="1" min="0"></div>
        <div class="form-group"><label>Грамм</label><input type="number" id="modal-sup-g" value="0" min="0" max="999"></div>
      </div>`;
    } else if (kind === 'volume') {
      html = `<div class="form-row">
        <div class="form-group"><label>Литры</label><input type="number" id="modal-sup-l" value="1" min="0"></div>
        <div class="form-group"><label>Миллилитры</label><input type="number" id="modal-sup-ml" value="0" min="0" max="999"></div>
      </div>`;
    }
    container.innerHTML = html;
  };

  window.modalAddSupplyItem = function() {
    const sel = document.getElementById('modal-sup-prod');
    const daysInput = document.getElementById('modal-sup-days');
    const days = parseInt(daysInput ? daysInput.value : 7) || 7;

    if (!sel || !sel.value) return;
    const opt = sel.options[sel.selectedIndex];
    const kind = opt && opt.dataset ? opt.dataset.kind : 'count';

    let qty = 0;
    if (kind === 'count') {
      const v = parseInt(document.getElementById('modal-sup-qty-main')?.value) || 0;
      qty = Math.round(v);
    } else if (kind === 'weight') {
      const kg = parseFloat(document.getElementById('modal-sup-kg')?.value) || 0;
      const g = parseFloat(document.getElementById('modal-sup-g')?.value) || 0;
      qty = Math.round((kg + g / 1000) * 1000) / 1000;
    } else if (kind === 'volume') {
      const l = parseFloat(document.getElementById('modal-sup-l')?.value) || 0;
      const ml = parseFloat(document.getElementById('modal-sup-ml')?.value) || 0;
      qty = Math.round((l + ml / 1000) * 1000) / 1000;
    }

    if (qty <= 0) return;
    const p = products.find(prod => prod.id === parseInt(sel.value));
    if (!p) return;

    supplyFormItems.push({ product_id: p.id, qty, shelf_days: days });

    const tbody = document.getElementById('modal-sup-tbody');
    if (tbody) {
      tbody.innerHTML = supplyFormItems.map(item => {
        const prod = products.find(x => x.id === item.product_id);
        return `<tr><td>${prod ? prod.name : ''}</td><td>${fmtQty(item.qty, prod ? prod.unit : '')}</td><td>${item.shelf_days} дн.</td></tr>`;
      }).join('');
    }
  };

  window.submitModalSupply = async function() {
    if (supplyFormItems.length === 0) {
      toast('Добавьте продукты', 'err');
      return;
    }
    try {
      const sup = await API.createSupply(supplyFormItems);
      toast(`Поставка ${sup.number} принята!`, 'ok');
      closeModal();
      renderSupplies();
    } catch (e) {
      toast('Ошибка: ' + e.message, 'err');
    }
  };

  async function renderSupplies() {
    try {
      const supplies = await API.getSupplies();
      let html = '<div class="card"><div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:16px"><h2>🚚 Журнал поставок</h2><button class="btn primary" onclick="openSupplyModal()">+ Принять поставку</button></div>';
      
      if (supplies.length > 0) {
        html += '<table><thead><tr><th>№ Поставки</th><th>Дата</th><th>Состав</th></tr></thead><tbody>';
        supplies.slice(0, 10).forEach(s => {
          const dt = new Date(s.date);
          let details = '';
          if (s.batches && s.batches.length > 0) {
            details = s.batches.map(b => `${b.product}: <strong>${b.qty} ${b.unit}</strong>`).join(', ');
          }
          html += `<tr><td><strong>${s.number}</strong></td><td>${dt.toLocaleDateString('ru-RU')}</td><td>${details}</td></tr>`;
        });
        html += '</tbody></table>';
      } else {
        html += '<p class="empty">Нет поставок</p>';
      }
      html += '</div>';
      main.innerHTML = html;
    } catch (e) {
      toast('Ошибка поставок: ' + e.message, 'err');
    }
  }

  // ===================== СТОП-ЛИСТ =====================
  async function renderStopList() {
    try {
      const stopList = await API.getStopList();
      const manual = stopList.filter(i => i.manual);
      const auto = stopList.filter(i => !i.manual);

      let html = `<div class="card">
        <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:16px;flex-wrap:wrap;gap:12px">
          <h2>🚫 Стоп-лист блюд и рекомендации</h2>
          <button class="btn danger" onclick="openManualStopModal()">⛔ Снять блюдо с продажи</button>
        </div>`;

      if (stopList.length === 0) {
        html += '<p style="color:var(--success);font-weight:600;padding:20px 0">✅ Все блюда доступны к продаже!</p>';
      }

      if (manual.length > 0) {
        html += `<p style="color:var(--danger);margin-bottom:12px;font-weight:500">⛔ Снято вручную: ${manual.length}</p>`;
        manual.forEach(item => {
          html += `<div class="suggestion-item critical">
            <div style="display:flex;justify-content:space-between;align-items:center;gap:12px;flex-wrap:wrap">
              <div style="font-weight:600;font-size:16px">${item.dish} <span class="badge critical">вручную</span></div>
              <button class="btn-xs" onclick="returnDishToSale(${item.dish_id})">↩️ Вернуть в продажу</button>
            </div>
            <div style="font-size:13px;color:var(--text-muted);margin-top:6px">Причина: ${item.manual_reason || 'не указана'}</div>
            <div style="font-size:13px;color:var(--text-muted);margin-top:4px">Продуктов хватило бы на ${item.max_portions} порц. — блюдо заблокировано решением персонала.</div>
          </div>`;
        });
      }

      if (auto.length > 0) {
        html += `<p style="color:var(--danger);margin:16px 0 12px;font-weight:500">📉 Нет продуктов: ${auto.length}</p>`;
        auto.forEach(item => {
          html += `<div class="suggestion-item critical"><div style="font-weight:600;font-size:16px;margin-bottom:6px">${item.dish} (доступно порций: ${item.max_portions})</div>`;
          if (item.missing && item.missing.length > 0) {
            html += '<ul style="padding-left:18px">';
            item.missing.forEach(m => { html += `<li style="color:var(--danger);font-size:13px">${m}</li>`; });
            html += '</ul>';
          }
          html += '</div>';
        });
      }
      html += '</div>';

      try {
        const suggestions = await API.getSuggestions();
        if (suggestions.length > 0) {
          html += '<div class="card"><h2>💡 Рекомендации по закупкам</h2>';
          suggestions.forEach(s => {
            html += `<div class="suggestion-item ${s.level === 'warning' ? 'warning' : 'critical'}"><div style="display:flex;justify-content:space-between;align-items:center"><strong>${s.product}</strong>${levelBadge(s.level)}</div>`;
            html += `<div style="font-size:13px;color:var(--text-muted);margin-top:4px">В наличии: ${s.have} ${s.unit} • Дней до исчерпания: ${s.days_left}</div>`;
            html += `<div style="font-size:13px;margin-top:4px;font-weight:500">→ ${s.action}</div></div>`;
          });
          html += '</div>';
        }
      } catch (e) {}

      main.innerHTML = html;
    } catch (e) {
      toast('Ошибка стоп-листа: ' + e.message, 'err');
    }
  }

  // --- Ручной стоп-лист ---
  window.openManualStopModal = function() {
    if (!dishes || dishes.length === 0) {
      toast('Список блюд не загружен', 'err');
      return;
    }
    const options = dishes
      .map(d => `<option value="${d.id}">${d.name}</option>`)
      .join('');
    const html = `
      <p style="color:var(--text-muted);margin-bottom:12px">Блюдо будет снято с продажи: его нельзя заказать, оно попадёт в стоп-лист и исчезнет из горячих предложений.</p>
      <div class="form-group"><label>Блюдо</label>
        <select id="ms-dish"><option value="">— Выбрать —</option>${options}</select></div>
      <div class="form-group"><label>Причина</label>
        <input type="text" id="ms-reason" placeholder="Не завезли посуду, жалоба, отзыв шефа..."></div>
      <button class="btn danger" style="width:100%" onclick="submitManualStop()">⛔ Снять с продажи</button>
    `;
    openModal('Ручное снятие блюда с продажи', html);
  };

  window.submitManualStop = async function() {
    const sel = document.getElementById('ms-dish');
    if (!sel || !sel.value) { toast('Выберите блюдо', 'err'); return; }
    const reason = document.getElementById('ms-reason')?.value.trim() || '';
    try {
      await API.addManualStop(parseInt(sel.value), reason);
      toast('Блюдо снято с продажи', 'ok');
      closeModal();
      renderStopList();
    } catch (e) { toast('Ошибка: ' + e.message, 'err'); }
  };

  window.returnDishToSale = async function(dishId) {
    if (!confirm('Вернуть блюдо в продажу?')) return;
    try {
      await API.removeManualStop(dishId);
      toast('Блюдо возвращено в продажу', 'ok');
      renderStopList();
    } catch (e) { toast('Ошибка: ' + e.message, 'err'); }
  };

  // ===================== ГОРЯЧИЕ ПРЕДЛОЖЕНИЯ =====================
  async function renderHot() {
    try {
      const hot = await API.getHot();
      let html = '<div class="card"><h2>🔥 Горячие предложения (FEFO-приоритет)</h2>';
      if (hot.length === 0) {
        html += '<p class="empty">Нет активных рекомендаций</p>';
      } else {
        html += '<p style="color:var(--text-muted);margin-bottom:16px">Блюда для быстрой реализации продуктов с истекающим сроком годности.</p>';
        hot.forEach((item, i) => {
          html += `<div class="card hot-card" style="margin-bottom:12px"><div style="display:flex;justify-content:space-between;align-items:center"><div style="font-size:16px;font-weight:600">${i + 1}. ${item.dish}</div><div><span style="font-size:20px;font-weight:700;color:var(--warning)">${item.score}</span> <span style="font-size:12px;color:var(--text-muted)">рейтинг</span></div></div>`;
          html += `<div style="display:flex;gap:16px;margin-top:8px;font-size:13px;color:var(--text-muted)"><span>💰 ${item.price} ₽</span><span>🍽 Доступно: ${item.portions} порц.</span><span>⏰ Мин. срок: ${item.min_days} дн.</span></div>`;
          if (item.reasons && item.reasons.length > 0) {
            html += '<ul style="margin-top:8px;padding-left:18px;font-size:13px;color:var(--text-muted)">';
            item.reasons.forEach(r => { html += `<li>${r}</li>`; });
            html += '</ul>';
          }
          html += '</div>';
        });
      }
      html += '</div>';
      main.innerHTML = html;
    } catch (e) {
      toast('Ошибка горячих предложений: ' + e.message, 'err');
    }
  }

  // ===================== АНАЛИТИКА НА ОСНОВЕ ЛОГОВ И СПИСАНИЙ =====================
async function renderAnalytics() {
    try {
      const [predictions, writeoffs, events, stock] = await Promise.all([
        API.getPredictions().catch(() => []),
        API.getWriteoffs().catch(() => []),
        API.getEvents().catch(() => []),
        API.getStock().catch(() => [])
      ]);

      let statsByType = { supply: 0, order: 0, order_reject: 0, writeoff: 0, system: 0 };
      let recentIncidents = [];

      events.forEach(e => {
        if (statsByType[e.type] !== undefined) statsByType[e.type]++;
        if (e.type === 'order_reject' || e.type === 'writeoff') {
          recentIncidents.push(e);
        }
      });

      const totalOperations = statsByType.order + statsByType.order_reject + statsByType.supply + statsByType.writeoff;
      const orderConversion = statsByType.order + statsByType.order_reject > 0 
        ? Math.round((statsByType.order / (statsByType.order + statsByType.order_reject)) * 100) 
        : 100;

      const totalWriteoffQty = writeoffs.reduce((sum, w) => sum + (w.qty || 0), 0);

      let html = `
        <div class="card">
          <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:16px;flex-wrap:wrap;gap:12px">
            <h2>📈 Расширенная ресторанная аналитика</h2>
            <button class="btn primary" onclick="runAnalyticsTest()">🧪 Стресс-тест аналитики</button>
          </div>
          <p style="color:var(--text-muted);margin-bottom:20px">Операционная аналитика, построенная на аудите системных логов, истории транзакций и складских партиях.</p>

          <!-- Ключевые метрики (KPI) -->
          <div class="grid" style="margin-bottom:24px">
            <div class="stat-card blue">
              <span class="num">${orderConversion}%</span>
              <span class="lbl">Успешность заказов (Конверсия)</span>
            </div>
            <div class="stat-card green">
              <span class="num">${statsByType.supply}</span>
              <span class="lbl">Поставок зафиксировано в логах</span>
            </div>
            <div class="stat-card orange">
              <span class="num">${totalWriteoffQty.toFixed(1)} ед.</span>
              <span class="lbl">Общий объем списаний</span>
            </div>
            <div class="stat-card red">
              <span class="num">${statsByType.order_reject}</span>
              <span class="lbl">Отказов из-за дефицита</span>
            </div>
          </div>
      `;

      html += `
        <div style="background:rgba(79, 70, 229, 0.05);border:1px solid var(--border-color);border-radius:12px;padding:20px;margin-bottom:24px">
          <h3 style="font-size:16px;margin-bottom:12px">📊 Аудит активности по типам событий (Всего логов: ${events.length})</h3>
          <div style="display:grid;grid-template-columns:repeat(auto-fit, minmax(140px, 1fr));gap:12px">
            <div style="background:var(--card-bg);padding:12px;border-radius:8px;border:1px solid var(--border-color)">
              <div style="font-size:12px;color:var(--text-muted)">Заказы</div>
              <div style="font-size:20px;font-weight:700;color:#2563eb">${statsByType.order}</div>
            </div>
            <div style="background:var(--card-bg);padding:12px;border-radius:8px;border:1px solid var(--border-color)">
              <div style="font-size:12px;color:var(--text-muted)">Поставки</div>
              <div style="font-size:20px;font-weight:700;color:#059669">${statsByType.supply}</div>
            </div>
            <div style="background:var(--card-bg);padding:12px;border-radius:8px;border:1px solid var(--border-color)">
              <div style="font-size:12px;color:var(--text-muted)">Списания</div>
              <div style="font-size:20px;font-weight:700;color:#d97706">${statsByType.writeoff}</div>
            </div>
            <div style="background:var(--card-bg);padding:12px;border-radius:8px;border:1px solid var(--border-color)">
              <div style="font-size:12px;color:var(--text-muted)">Отказы</div>
              <div style="font-size:20px;font-weight:700;color:#dc2626">${statsByType.order_reject}</div>
            </div>
          </div>
        </div>
      `;

      if (Array.isArray(writeoffs) && writeoffs.length > 0) {
        html += '<h3 style="font-size:16px;margin-top:20px;margin-bottom:12px">📉 Динамика объемов списаний по партиям</h3>';
        html += '<div class="chart-container">';
        const maxQty = Math.max(...writeoffs.slice(0, 10).map(w => w.qty), 1);
        writeoffs.slice(0, 10).forEach(w => {
          const heightPercent = Math.max(Math.round((w.qty / maxQty) * 100), 15);
          html += `<div class="chart-bar-wrap" title="${w.product}: ${w.qty} (${w.reason})"><div class="chart-bar" style="height:${heightPercent}%"></div><span class="chart-label">${w.product}</span></div>`;
        });
        html += '</div>';
      }

      html += '</div>';

      if (recentIncidents.length > 0) {
        html += `
          <div class="card">
            <h2>⚠️ Лента критических инцидентов (Из логов)</h2>
            <p style="color:var(--text-muted);margin-bottom:16px">Последние зафиксированные системой отказы и акты списания продукции.</p>
            <table>
              <thead>
                <tr>
                  <th>Время</th>
                  <th>Тип инцидента</th>
                  <th>Описание из логов</th>
                </tr>
              </thead>
              <tbody>
        `;
        recentIncidents.slice(0, 8).forEach(inc => {
          const dt = new Date(inc.created_at);
          const isReject = inc.type === 'order_reject';
          html += `<tr>
            <td style="white-space:nowrap">${dt.toLocaleTimeString('ru-RU')}</td>
            <td><span class="badge ${isReject ? 'critical' : 'warning'}">${inc.type}</span></td>
            <td>${inc.message}</td>
          </tr>`;
        });
        html += '</tbody></table></div>';
      }

      if (Array.isArray(predictions) && predictions.length > 0) {
        html += `
          <div class="card">
            <h2>🔮 Превентивный прогноз дефицита и стоп-листа</h2>
            <table>
              <thead>
                <tr>
                  <th>Блюдо</th>
                  <th>Дефицитный продукт</th>
                  <th>Требуется</th>
                  <th>В наличии</th>
                  <th>Дефицит</th>
                  <th>Рекомендация</th>
                </tr>
              </thead>
              <tbody>
        `;
        predictions.slice(0, 10).forEach(p => {
          html += `<tr>
            <td><strong>${p.dish}</strong></td>
            <td>${p.product}</td>
            <td>${p.need}</td>
            <td>${p.have}</td>
            <td style="color:var(--danger);font-weight:600">${p.deficit}</td>
            <td><span class="badge warning">${p.action}</span></td>
          </tr>`;
        });
        html += '</tbody></table></div>';
      }

      if ((!predictions || predictions.length === 0) && (!writeoffs || writeoffs.length === 0) && events.length === 0) {
        html += `
          <div class="card" style="text-align:center;padding:40px">
            <p class="empty" style="margin-bottom:16px">В системных логах пока недостаточно данных для построения аналитики.</p>
            <button class="btn primary" onclick="runAnalyticsTest()">🧪 Сгенерировать тестовые данные</button>
          </div>
        `;
      }

      main.innerHTML = html;
    } catch (e) {
      toast('Ошибка загрузки аналитики: ' + e.message, 'err');
    }
  }

  window.runAnalyticsTest = async function() {
    try {
      toast('Запуск стресс-теста аналитики...', 'ok');
      await API.createSupply([
        { product_id: 1, qty: 25, shelf_days: 1 },
        { product_id: 2, qty: 25, shelf_days: 1 },
        { product_id: 7, qty: 20, shelf_days: 1 }
      ]);
      await API.createOrder([
        { dish_id: 1, qty: 2 },
        { dish_id: 2, qty: 1 }
      ]).catch(() => {});
      await API.autoWriteoff();
      
      toast('Тест успешно выполнен!', 'ok');
      renderAnalytics();
    } catch (e) {
      toast('Ошибка теста: ' + e.message, 'err');
    }
  };

  // ===================== ЖУРНАЛ ЛОГОВ =====================
  async function renderLogs() {
    try {
      const events = await API.getEvents();
      let html = `<div class="card">
        <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:16px;flex-wrap:wrap;gap:12px">
          <h2>📋 Продвинутый журнал системных событий</h2>
          <div class="filter-bar" style="margin-bottom:0">
            <input type="text" placeholder="🔍 Поиск в логах..." value="${logSearch}" oninput="window.updateLogSearch(this.value)">
            <select onchange="window.setLogTypeFilter(this.value)">
              <option value="all" ${logFilter === 'all' ? 'selected' : ''}>Все типы</option>
              <option value="supply" ${logFilter === 'supply' ? 'selected' : ''}>Поставки</option>
              <option value="order" ${logFilter === 'order' ? 'selected' : ''}>Заказы</option>
              <option value="order_reject" ${logFilter === 'order_reject' ? 'selected' : ''}>Отказы</option>
              <option value="writeoff" ${logFilter === 'writeoff' ? 'selected' : ''}>Списания</option>
              <option value="system" ${logFilter === 'system' ? 'selected' : ''}>Система</option>
            </select>
          </div>
        </div>`;

      const filtered = events.filter(e => {
        const matchesType = logFilter === 'all' || e.type === logFilter;
        const matchesSearch = logSearch === '' || e.message.toLowerCase().includes(logSearch.toLowerCase()) || e.type.toLowerCase().includes(logSearch.toLowerCase());
        return matchesType && matchesSearch;
      });

      if (filtered.length === 0) {
        html += '<p class="empty">События не найдены</p>';
      } else {
        html += '<table><thead><tr><th>ID</th><th>Время</th><th>Тип события</th><th>Сообщение</th></tr></thead><tbody>';
        filtered.forEach(e => {
          const dt = new Date(e.created_at);
          const time = dt.toLocaleString('ru-RU');
          const typeColors = { supply: '#059669', order: '#2563eb', order_reject: '#dc2626', writeoff: '#d97706', system: '#64748b' };
          const color = typeColors[e.type] || '#64748b';
          html += `<tr>
            <td>#${e.ID || e.id}</td>
            <td style="white-space:nowrap">${time}</td>
            <td><span class="badge" style="background:${color}15;color:${color}">${e.type}</span></td>
            <td>${e.message}</td>
          </tr>`;
        });
        html += '</tbody></table>';
      }
      html += '</div>';
      main.innerHTML = html;
    } catch (e) {
      toast('Ошибка загрузки логов: ' + e.message, 'err');
    }
  }

  window.updateLogSearch = function(val) {
    logSearch = val;
    renderLogs();
  };

  window.setLogTypeFilter = function(val) {
    logFilter = val;
    renderLogs();
  };

  // ===================== ПРОДУКТЫ =====================
  let productSearch = '';

  async function renderProducts() {
    try {
      const [prods, stock] = await Promise.all([API.getProducts(), API.getStock()]);
      const stockMap = {};
      stock.forEach(s => { stockMap[s.product_id] = s; });

      const filtered = prods.filter(p => p.name.toLowerCase().includes(productSearch.toLowerCase()));
      const canEdit = API.isAdmin();

      let html = `<div class="card">
        <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:16px;flex-wrap:wrap;gap:12px">
          <h2>🧺 Справочник продуктов (${prods.length})</h2>
          <div style="display:flex;gap:12px;align-items:center">
            <input type="text" placeholder="🔍 Поиск..." value="${productSearch}" oninput="window.updateProductSearch(this.value)"
              style="padding:10px 16px;border:1px solid var(--border-color);border-radius:10px;font-size:14px;outline:none;background:var(--card-bg);color:var(--text-main)">
            ${canEdit ? '<button class="btn primary" onclick="openProductForm()">+ Новый продукт</button>' : ''}
          </div>
        </div>`;

      if (filtered.length === 0) {
        html += '<p class="empty">Продукты не найдены</p>';
      } else {
        html += `<table><thead><tr>
          <th>Продукт</th><th>Ед.</th><th>Себестоимость</th><th>Розница (×2)</th><th>Остаток на складе</th>${canEdit ? '<th></th>' : ''}
        </tr></thead><tbody>`;
        filtered.forEach(p => {
          const s = stockMap[p.id];
          const stockDisp = s ? s.total_display : '0';
          const stockColor = !s || s.total_qty <= 0 ? 'var(--danger)' : s.days_to_expiry <= 2 ? 'var(--warning)' : 'var(--text-main)';
          html += `<tr>
            <td><strong>${p.name}</strong></td>
            <td>${p.unit}</td>
            <td>${p.cost ? p.cost.toFixed(2) + ' ₽/' + p.unit : '—'}</td>
            <td>${p.cost ? (p.cost * 2).toFixed(2) + ' ₽/' + p.unit : '—'}</td>
            <td style="color:${stockColor};font-weight:600">${stockDisp}</td>
            ${canEdit ? `<td style="text-align:right;white-space:nowrap">
              <button class="btn-xs" onclick="openProductForm(${p.id},'${p.name.replace(/'/g, "\\'")}','${p.unit}',${p.cost})">✏️ Цена</button>
              <button class="btn-xs danger" onclick="removeProduct(${p.id},'${p.name.replace(/'/g, "\\'")}')">✕ Удалить</button>
            </td>` : ''}
          </tr>`;
        });
        html += '</tbody></table>';
      }
      html += '</div>';
      main.innerHTML = html;
    } catch (e) {
      toast('Ошибка загрузки продуктов: ' + e.message, 'err');
    }
  }

  window.updateProductSearch = function(val) {
    productSearch = val;
    renderProducts();
  };

  window.openProductForm = function(id, name, unit, cost) {
    const isEdit = id > 0;
    const html = `
      <div class="form-group"><label>Название</label>
        <input type="text" id="pf-name" value="${isEdit ? name : ''}" ${isEdit ? 'readonly' : ''} placeholder="Напр. Фета"></div>
      ${isEdit ? '' : `<div class="form-group"><label>Единица измерения</label>
        <select id="pf-unit">
          <option value="кг">кг (вес)</option>
          <option value="л">л (объём)</option>
          <option value="шт">шт (штуки)</option>
        </select></div>`}
      <div class="form-group"><label>Себестоимость, ₽ за ${isEdit ? unit : 'единицу'}</label>
        <input type="number" id="pf-cost" value="${isEdit ? cost : ''}" min="0" step="0.01" placeholder="180"></div>
      <p style="color:var(--text-muted);font-size:12px;margin:8px 0 16px">Розничная цена для кастомизации заказа = себестоимость × 2.</p>
      <button class="btn primary" style="width:100%" onclick="submitProductForm(${isEdit ? id : 'null'},'${isEdit ? unit : ''}')">💾 Сохранить</button>
    `;
    openModal(isEdit ? `Цена: ${name}` : 'Новый продукт', html);
  };

  window.submitProductForm = async function(id, unit) {
    const name = document.getElementById('pf-name').value.trim();
    const cost = parseFloat(document.getElementById('pf-cost').value) || 0;
    try {
      if (id) {
        await API.setProductCost(id, cost);
        toast('Себестоимость обновлена', 'ok');
      } else {
        if (!name) { toast('Укажите название', 'err'); return; }
        const u = document.getElementById('pf-unit').value;
        await API.addProduct(name, u, cost);
        toast('Продукт добавлен', 'ok');
      }
      closeModal();
      await loadAll();
      renderProducts();
    } catch (e) { toast('Ошибка: ' + e.message, 'err'); }
  };

  window.removeProduct = async function(id, name) {
    if (!confirm(`Удалить продукт «${name}» из справочника?`)) return;
    try {
      await API.deleteProduct(id);
      toast('Продукт удалён', 'ok');
      await loadAll();
      renderProducts();
    } catch (e) { toast('Ошибка: ' + e.message, 'err'); }
  };

  // ===================== АКЦИИ =====================
  const promoTypeLabels = {
    birthday: '🎂 День рождения',
    min_sum: '💰 Скидка от суммы чека',
    happy_hour: '🕐 Счастливые часы',
    near_expiry: '⏳ Истекает срок годности'
  };

  async function renderPromotions() {
    try {
      const promos = await API.getPromotions();
      const canEdit = API.isAdmin();
      const now = new Date();
      const hour = now.getHours();

      let html = `<div class="card">
        <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:16px;flex-wrap:wrap;gap:12px">
          <h2>🏷️ Акции и скидки (${promos.filter(p => p.enabled).length} активных)</h2>
          ${canEdit ? '<button class="btn primary" onclick="openPromoForm()">+ Новая акция</button>' : ''}
        </div>
        <p style="color:var(--text-muted);margin-bottom:16px">Скидка применяется автоматически при оформлении заказа — выбирается максимальная подходящая акция.</p>`;

      if (promos.length === 0) {
        html += '<p class="empty">Акции не настроены</p>';
      } else {
        promos.forEach(p => {
          let condition = '—';
          let activeNow = p.enabled;
          if (p.type === 'min_sum') condition = `Чек ≥ ${p.min_sum} ₽`;
          if (p.type === 'happy_hour') {
            condition = `${p.happy_hour[0]}:00–${p.happy_hour[1]}:00`;
            if (p.enabled) activeNow = p.enabled && hour >= p.happy_hour[0] && hour < p.happy_hour[1];
          }
          if (p.type === 'near_expiry') condition = 'Доля «рисковых» продуктов в чеке';
          if (p.type === 'birthday') condition = 'Гость — именинник';
          if (p.active_days && p.active_days.length > 0) condition += ` • Дни: ${p.active_days.join(', ')}`;

          const stateBadge = !p.enabled
            ? '<span class="badge warning">выключена</span>'
            : activeNow ? '<span class="badge ok">действует</span>' : '<span class="badge warning">не в окне</span>';

          html += `<div class="suggestion-item ${p.enabled ? 'warning' : ''}" style="margin-bottom:12px">
            <div style="display:flex;justify-content:space-between;align-items:center;gap:12px;flex-wrap:wrap">
              <div>
                <div style="font-weight:600;font-size:16px">${p.name} <span class="badge critical">−${p.discount}%</span> ${stateBadge}</div>
                <div style="font-size:13px;color:var(--text-muted);margin-top:4px">${promoTypeLabels[p.type] || p.type} • ${condition}</div>
              </div>
              ${canEdit ? `<div style="display:flex;gap:6px">
                <button class="btn-xs" onclick="togglePromo(${p.id},${!p.enabled})">${p.enabled ? '⏸ Выкл' : '▶ Вкл'}</button>
                <button class="btn-xs danger" onclick="removePromo(${p.id})">✕</button>
              </div>` : ''}
            </div>
          </div>`;
        });
      }
      html += '</div>';

      html += `<div class="card"><h2>🧮 Проверка скидок</h2>
        <p style="color:var(--text-muted);margin-bottom:12px">Введите сумму чека — система покажет, какая акция сработает прямо сейчас.</p>
        <div class="form-row">
          <div class="form-group"><label>Сумма чека, ₽</label>
            <input type="number" id="promo-check-sum" value="2000" min="0"></div>
          <button class="btn secondary" onclick="checkPromoForSum()">Проверить</button>
        </div>
        <div id="promo-check-result"></div>
      </div>`;

      main.innerHTML = html;
    } catch (e) {
      toast('Ошибка загрузки акций: ' + e.message, 'err');
    }
  }

  window.checkPromoForSum = function() {
    const sum = parseFloat(document.getElementById('promo-check-sum').value) || 0;
    const hour = new Date().getHours();
    const resEl = document.getElementById('promo-check-result');
    if (!resEl) return;
    API.getPromotions().then(promos => {
      let best = 0, bestName = '';
      promos.forEach(p => {
        if (!p.enabled || p.discount <= best) return;
        let ok = false;
        if (p.type === 'birthday') ok = true;
        if (p.type === 'min_sum') ok = sum >= p.min_sum;
        if (p.type === 'happy_hour') ok = hour >= p.happy_hour[0] && hour < p.happy_hour[1];
        if (ok) { best = p.discount; bestName = p.name; }
      });
      if (best > 0) {
        const finalSum = Math.round(sum * (100 - best)) / 100;
        resEl.innerHTML = `<div class="suggestion-item warning" style="margin-top:12px">
          <strong>«${bestName}»</strong> — скидка ${best}%<br>
          Итого к оплате: <strong style="color:var(--success)">${finalSum.toFixed(2)} ₽</strong>
          <span style="color:var(--text-muted)"> (было ${sum.toFixed(2)} ₽)</span>
        </div>`;
      } else {
        resEl.innerHTML = '<p class="empty" style="padding:16px">Ни одна акция не подходит к этой сумме в текущее время.</p>';
      }
    });
  };

  window.openPromoForm = function() {
    const html = `
      <div class="form-group"><label>Название акции</label>
        <input type="text" id="promo-name" placeholder="Напр. Счастливые часы 12–15"></div>
      <div class="form-group"><label>Тип акции</label>
        <select id="promo-type" onchange="window.updatePromoTypeFields()">
          <option value="happy_hour">🕐 Счастливые часы</option>
          <option value="min_sum">💰 Скидка от суммы чека</option>
          <option value="birthday">🎂 День рождения</option>
          <option value="near_expiry">⏳ Истекает срок годности</option>
        </select></div>
      <div id="promo-type-fields"></div>
      <div class="form-group"><label>Скидка, %</label>
        <input type="number" id="promo-discount" value="15" min="1" max="99"></div>
      <button class="btn primary" style="width:100%" onclick="submitPromoForm()">💾 Создать акцию</button>
    `;
    openModal('Новая акция', html);
    updatePromoTypeFields();
  };

  window.updatePromoTypeFields = function() {
    const type = document.getElementById('promo-type')?.value;
    const box = document.getElementById('promo-type-fields');
    if (!box) return;
    if (type === 'min_sum') {
      box.innerHTML = `<div class="form-group"><label>Минимальная сумма чека, ₽</label>
        <input type="number" id="promo-minsum" value="2000" min="0"></div>`;
    } else if (type === 'happy_hour') {
      box.innerHTML = `<div class="form-row">
        <div class="form-group"><label>Начало (час)</label>
          <input type="number" id="promo-hh-start" value="15" min="0" max="23"></div>
        <div class="form-group"><label>Конец (час)</label>
          <input type="number" id="promo-hh-end" value="17" min="1" max="24"></div>
      </div>`;
    } else {
      box.innerHTML = '';
    }
  };

  window.submitPromoForm = async function() {
    const name = document.getElementById('promo-name').value.trim();
    const type = document.getElementById('promo-type').value;
    const discount = parseFloat(document.getElementById('promo-discount').value) || 0;
    if (!name) { toast('Укажите название', 'err'); return; }
    const body = { type, name, discount };
    if (type === 'min_sum') body.min_sum = parseFloat(document.getElementById('promo-minsum')?.value) || 0;
    if (type === 'happy_hour') {
      body.happy_hour = [
        parseInt(document.getElementById('promo-hh-start')?.value) || 0,
        parseInt(document.getElementById('promo-hh-end')?.value) || 0
      ];
    }
    try {
      await API.createPromotion(body);
      toast('Акция создана', 'ok');
      closeModal();
      renderPromotions();
    } catch (e) { toast('Ошибка: ' + e.message, 'err'); }
  };

  window.togglePromo = async function(id, enabled) {
    try {
      await API.updatePromotion(id, '', -1, enabled);
      toast(enabled ? 'Акция включена' : 'Акция выключена', 'ok');
      renderPromotions();
    } catch (e) { toast('Ошибка: ' + e.message, 'err'); }
  };

  window.removePromo = async function(id) {
    if (!confirm('Удалить акцию?')) return;
    try {
      await API.deletePromotion(id);
      toast('Акция удалена', 'ok');
      renderPromotions();
    } catch (e) { toast('Ошибка: ' + e.message, 'err'); }
  };

  // ===================== ГРАФИК ПОСТАВОК =====================
  const scheduleLevelLabels = {
    critical: '<span class="badge critical">нет запаса</span>',
    warning: '<span class="badge warning">ниже минимума</span>',
    notice: '<span class="badge warning">на грани</span>',
    ok: '<span class="badge ok">норма</span>',
    waiting: '<span class="badge ok">ждёт периода</span>'
  };

  async function renderSupplySchedule() {
    try {
      const rows = await API.getSupplySchedule();
      const canEdit = API.isAdmin();

      let html = `<div class="card">
        <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:16px;flex-wrap:wrap;gap:12px">
          <h2>📅 Автозаказ и график поставок</h2>
          <button class="btn primary" onclick="runAutoSupply()">⚡ Сформировать автозаказ</button>
        </div>
        <p style="color:var(--text-muted);margin-bottom:16px">
          Потребность = среднее дневное потребление × (период между поставками + плечо доставки) + неснижаемый остаток − текущий запас.
        </p>`;

      if (rows.length === 0) {
        html += '<p class="empty">Графики не настроены. Настройте через склад или добавьте продукт вручную.</p>';
      } else {
        html += `<table><thead><tr>
          <th>Продукт</th><th>Запас</th><th>Хватит на</th><th>Расход/день</th>
          <th>Период</th><th>Плечо</th><th>Мин. остаток</th><th>К заказу</th><th>Статус</th>${canEdit ? '<th></th>' : ''}
        </tr></thead><tbody>`;
        rows.forEach(r => {
          const coverColor = r.days_covered <= 1 ? 'var(--danger)' : r.days_covered <= 2 ? 'var(--warning)' : 'var(--text-main)';
          html += `<tr>
            <td><strong>${r.product}</strong></td>
            <td>${r.current_display}</td>
            <td style="color:${coverColor};font-weight:600">${r.days_covered} дн.</td>
            <td>${r.avg_daily_display}</td>
            <td>${r.days_between} дн.</td>
            <td>${r.lead_days} дн.</td>
            <td>${r.min_stock_display}</td>
            <td style="font-weight:700;color:${r.order_qty > 0 ? 'var(--danger)' : 'var(--success)'}">${r.order_qty > 0 ? r.order_display : '—'}</td>
            <td>${scheduleLevelLabels[r.level] || r.level}</td>
            ${canEdit ? `<td style="text-align:right;white-space:nowrap">
              <button class="btn-xs" onclick="openScheduleForm(${r.product_id},'${r.product.replace(/'/g, "\\'")}','${r.unit}',${r.days_between},${r.lead_days},${r.min_stock_level})">✏️</button>
              <button class="btn-xs danger" onclick="removeSchedule(${r.product_id})">✕</button>
            </td>` : ''}
          </tr>`;
        });
        html += '</tbody></table>';
      }
      html += '</div>';
      main.innerHTML = html;
    } catch (e) {
      toast('Ошибка графика поставок: ' + e.message, 'err');
    }
  }

  window.openScheduleForm = function(productId, name, unit, daysBetween, leadDays, minStock) {
    const kind = unitKind(unit);
    const html = `
      <p style="color:var(--text-muted);margin-bottom:12px">Настройка автозаказа: «${name}» (${unit})</p>
      <div class="form-row">
        <div class="form-group"><label>Период, дней</label>
          <input type="number" id="sf-days" value="${daysBetween}" min="1"></div>
        <div class="form-group"><label>Плечо доставки, дней</label>
          <input type="number" id="sf-lead" value="${leadDays}" min="0"></div>
      </div>
      ${qtyInputFields(kind, 'sf-min', minStock)}
      <button class="btn primary" style="width:100%" onclick="submitScheduleForm(${productId},'${unit}')">💾 Сохранить</button>
    `;
    openModal('График поставки', html);
  };

  window.submitScheduleForm = async function(productId, unit) {
    const kind = unitKind(unit);
    const daysBetween = parseInt(document.getElementById('sf-days').value) || 1;
    const leadDays = parseInt(document.getElementById('sf-lead').value) || 0;
    const minStock = readQtyInput(kind, 'sf-min');
    try {
      await API.setSupplySchedule(productId, daysBetween, leadDays, minStock);
      toast('График сохранён', 'ok');
      closeModal();
      renderSupplySchedule();
    } catch (e) { toast('Ошибка: ' + e.message, 'err'); }
  };

  window.removeSchedule = async function(productId) {
    if (!confirm('Снять продукт с автозаказа?')) return;
    try {
      await API.deleteSupplySchedule(productId);
      toast('Снят с автозаказа', 'ok');
      renderSupplySchedule();
    } catch (e) { toast('Ошибка: ' + e.message, 'err'); }
  };

  window.runAutoSupply = async function() {
    try {
      const preview = await API.generateAutoSupply();
      if (!preview.items || preview.items.length === 0) {
        toast(preview.message || 'Автозаказ не требуется — остатки в норме', 'ok');
        return;
      }
      const names = preview.rows.map(r => `${r.product}: ${r.order_display}`).join('\n');
      if (!confirm(`Автозаказ на ${preview.items.length} поз.:

${names}

Оформить поставку?`)) return;
      const sup = await API.createAutoSupply();
      toast(`Поставка ${sup.number} оформлена (${preview.items.length} поз.)`, 'ok');
      renderSupplySchedule();
    } catch (e) {
      toast('Ошибка: ' + e.message, 'err');
    }
  };

  window.doAutoWriteoff = async function() {
    if (!confirm('Списать все просроченные продукты?')) return;
    try {
      const res = await API.autoWriteoff();
      toast(`Успешно списано партий: ${res.written_off}`, 'ok');
      renderDashboard();
    } catch (e) {
      toast('Ошибка: ' + e.message, 'err');
    }
  };

  try {
    API.stream((event, data) => {
      if (event === 'hello') return;
      const labels = { supply: 'Поставка', stock: 'Склад', stoplist: 'Стоп-лист' };
      if (event === 'demo' && window.refreshDemoStatus) window.refreshDemoStatus();
      toast(`${labels[event] || event}: ${data}`, 'ok');
      render(currentTab);
    });
  } catch (e) {}

  setInterval(() => { render(currentTab); }, 30000);
  loadAll();

  (async function checkAuth() {
    const saved = API.getUser();
    if (saved) {
      try {
        const me = await API.me();
        currentUser = me;
        loginOverlay.style.display = 'none';
        updateUserUI();
        loadAll();
      } catch {
        API.clearAuth();
        loginOverlay.style.display = 'flex';
      }
    } else {
      loginOverlay.style.display = 'flex';
    }
  })();
})();