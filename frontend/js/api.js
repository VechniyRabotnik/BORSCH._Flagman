const API = (() => {
  const BASE = '';
  const TOKEN_KEY = 'auth_token';

  function getToken() { return localStorage.getItem(TOKEN_KEY) || ''; }
  function setToken(t) { t ? localStorage.setItem(TOKEN_KEY, t) : localStorage.removeItem(TOKEN_KEY); }
  function getUser() {
    try { return JSON.parse(localStorage.getItem('auth_user') || 'null'); } catch { return null; }
  }
  function setUser(u) { u ? localStorage.setItem('auth_user', JSON.stringify(u)) : localStorage.removeItem('auth_user'); }
  function clearAuth() { setToken(''); setUser(null); }
  function isAdmin() { const u = getUser(); return u && u.role === 'admin'; }

  function authHeaders(extra) {
    const h = Object.assign({}, extra || {});
    const t = getToken();
    if (t) h['Authorization'] = 'Bearer ' + t;
    return h;
  }

  async function get(path) {
    const r = await fetch(BASE + path, { headers: authHeaders() });
    if (r.status === 401) { clearAuth(); window.dispatchEvent(new Event('unauthorized')); throw new Error('Требуется вход'); }
    if (!r.ok) {
      let msg = r.status + ' ' + r.statusText;
      try { const e = await r.json(); if (e.error) msg = e.error; } catch {}
      throw new Error(msg);
    }
    return r.json();
  }

  async function post(path, body) {
    const r = await fetch(BASE + path, {
      method: 'POST',
      headers: authHeaders({ 'Content-Type': 'application/json' }),
      body: JSON.stringify(body || {})
    });
    const data = await r.json().catch(() => ({}));
    if (r.status === 401) { clearAuth(); window.dispatchEvent(new Event('unauthorized')); throw new Error(data.error || 'Требуется вход'); }
    if (!r.ok) throw new Error(data.error || r.statusText);
    return data;
  }

  return {
    getToken, getUser, clearAuth, isAdmin,
    login: async (login, password) => {
      const sess = await post('/api/login', { login, password });
      setToken(sess.token);
      setUser({ login: sess.login, role: sess.role });
      return sess;
    },
    logout: async () => {
      try { return await post('/api/logout', {}); }
      finally { clearAuth(); }
    },
    me: () => get('/api/me'),
    getDashboard: () => get('/api/dashboard'),
    getProducts: () => get('/api/products'),
    getDishes: () => get('/api/dishes'),
    getStock: () => get('/api/stock'),
    getExpiring: (days) => get('/api/expiring?days=' + (days || 7)),
    getStopList: () => get('/api/stoplist'),
    getHot: () => get('/api/hot'),
    getSuggestions: () => get('/api/suggestions'),
    getPredictions: () => get('/api/predictions'),
    getSupplies: () => get('/api/supplies'),
    getOrders: () => get('/api/orders'),
    getWriteoffs: () => get('/api/writeoffs'),
    getEvents: () => get('/api/events'),
    getBatches: () => get('/api/batches'),
    createSupply: (items) => post('/api/supplies', { items }),
    createOrder: (items) => post('/api/orders', { items }),
    autoWriteoff: () => post('/api/writeoff', {}),
    adjustBatch: (batch_id, qty, reason) => post('/api/batches/adjust', { batch_id, qty, reason }),
    setExpiry: (batch_id, expiry) => post('/api/batches/expiry', { batch_id, expiry }),
    writeoffBatch: (batch_id, qty, reason) => post('/api/batches/writeoff', { batch_id, qty, reason }),
    deleteBatch: (batch_id) => post('/api/batches/delete', { batch_id }),
    cancelOrder: (order_id) => post('/api/orders/cancel', { order_id }),
    addManualStop: (dish_id, reason) => post('/api/stoplist/add', { dish_id, reason }),
    removeManualStop: (dish_id) => post('/api/stoplist/remove', { dish_id }),
    getUsers: () => get('/api/users'),
    saveUser: (login, password, role) => post('/api/users/save', { login, password, role }),
    deleteUser: (login) => post('/api/users/delete', { login }),
    stream: (onEvent) => {
      const t = getToken();
      const evt = new EventSource('/api/stream' + (t ? '?token=' + encodeURIComponent(t) : ''));
      ['stock', 'order', 'supply', 'writeoff', 'stoplist', 'demo', 'update'].forEach((name) => {
        evt.addEventListener(name, (e) => onEvent(name, e.data));
      });
      return evt;
    },

    // --- Демо-режим ---
    demoStatus: () => get('/api/demo'),
    demoStart: (cfg) => post('/api/demo/start', cfg || {}),
    demoStop: () => post('/api/demo/stop', {}),
    demoConfig: (cfg) => post('/api/demo/config', cfg || {}),
    demoRun: (action) => post('/api/demo/run', { action }),

    // --- Акции ---
    getPromotions: () => get('/api/promotions'),
    createPromotion: (p) => post('/api/promotions', p),
    updatePromotion: (id, name, discount, enabled) => post('/api/promotions/save', { id, name, discount, enabled }),
    deletePromotion: (id) => post('/api/promotions/delete', { promotion_id: id }),

    // --- График поставок ---
    getSupplySchedule: () => get('/api/supply-schedule'),
    setSupplySchedule: (product_id, days_between, lead_days, min_stock_level) =>
      post('/api/supply-schedule', { product_id, days_between, lead_days, min_stock_level }),
    deleteSupplySchedule: (product_id) => post('/api/supply-schedule/delete', { product_id }),
    generateAutoSupply: () => get('/api/auto-supply'),
    createAutoSupply: () => post('/api/auto-supply', {}),

    // --- Кастомизация заказа ---
    getOrderByID: (id) => get('/api/orders/by-id?id=' + id),
    customizeAdd: (order_id, dish_id, product_id, qty) =>
      post('/api/orders/customize/add', { order_id, dish_id, product_id, qty }),
    customizeRemove: (order_id, dish_id, product_id, qty) =>
      post('/api/orders/customize/remove', { order_id, dish_id, product_id, qty }),
    updateOrderStatus: (order_id, status) =>
      post('/api/orders/status', { order_id, status }),
    calcOrderDiscount: (order_id) =>
      post('/api/orders/discount', { order_id }),

    // --- Продукты ---
    addProduct: (name, unit, cost) => post('/api/products/save', { name, unit, cost }),
    setProductCost: (id, cost) => post('/api/products/save', { id, cost }),
    deleteProduct: (product_id) => post('/api/products/delete', { product_id }),

    // --- Блюда ---
    addDish: (name, price, ingredients) => post('/api/dishes/save', { name, price, ingredients }),
    updateDish: (id, name, price, ingredients) => post('/api/dishes/save', { id, name, price, ingredients }),
    deleteDish: (dish_id) => post('/api/dishes/delete', { dish_id }),
  };
})();
