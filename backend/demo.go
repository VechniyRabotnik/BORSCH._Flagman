package main

import (
	"encoding/json"
	"fmt"
	"math"
	mrand "math/rand"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// ========================
// ДЕМО-РЕЖИМ
// ========================
//
// Имитирует живую работу ресторана для демонстрации жюри:
// заказы, поставки, списания, входы персонала, стоп-лист.
// Каждый тип действия имеет собственный интервал и может быть
// отключён независимо.

// Ключи типов действий демо-режима.
const (
	demoOrders       = "orders"
	demoSupplies     = "supplies"
	demoWriteoffs    = "writeoffs"
	demoAuth         = "auth"
	demoStopList     = "stoplist"
	demoAdjust       = "adjust"
	demoCustomize    = "customize"
)

// demoOrder — порядок обхода действий при первом запуске.
// Гарантирует детерминированный разнесение по времени (каждое +3 сек).
var demoOrder = []string{
	demoOrders,
	demoSupplies,
	demoWriteoffs,
	demoAuth,
	demoStopList,
	demoAdjust,
	demoCustomize,
}

// demoAction — настройки одного типа действий.
type demoAction struct {
	Enabled  bool  `json:"enabled"`
	Interval int   `json:"interval"` // секунд между срабатываниями
	DueAt    int64 `json:"-"`        // unix-время следующего запуска
}

// Demo — движок демонстрации. Все обращения к данным идут через публичные
// методы Store, поэтому отдельная блокировка для данных не нужна.
type Demo struct {
	mu      sync.Mutex
	running bool
	stop    chan struct{}
	actions map[string]*demoAction
	rnd     *mrand.Rand
	// stopped — блюда, снятые с продажи самим демо. Возвращаем в продаже
	// только их, ручные стопы пользователя не трогаем.
	stopped  map[int]bool
	orders   int
	supplies int
}

func NewDemo() *Demo {
	return &Demo{
		actions: map[string]*demoAction{
			demoOrders:    {Enabled: true, Interval: 30},
			demoSupplies:  {Enabled: true, Interval: 30},
			demoWriteoffs: {Enabled: true, Interval: 30},
			demoAuth:      {Enabled: true, Interval: 30},
			demoStopList:  {Enabled: true, Interval: 30},
			demoAdjust:    {Enabled: true, Interval: 30},
			demoCustomize: {Enabled: true, Interval: 30},
		},
		rnd:     mrand.New(mrand.NewSource(time.Now().UnixNano())),
		stopped: map[int]bool{},
	}
}

// clampInterval не даёт выставить интервал, который уронит сервер.
func clampInterval(v int) int {
	if v < 5 {
		return 5
	}
	if v > 600 {
		return 600
	}
	return v
}

// Apply точечно обновляет включённость и интервалы.
// nil в поле патча означает «оставить как было».
func (d *Demo) Apply(patches map[string]demoPatch) {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now().Unix()
	for name, p := range patches {
		act, ok := d.actions[name]
		if !ok {
			continue
		}
		intervalBefore, enabledBefore := act.Interval, act.Enabled
		if p.Enabled != nil {
			act.Enabled = *p.Enabled
		}
		if p.Interval != nil && *p.Interval > 0 {
			act.Interval = clampInterval(*p.Interval)
		}
		// Если темп поменяли на ходу или действие только что включили —
		// отсчитываем интервал от текущего момента, иначе счётчик в окне
		// остался бы по старому расписанию.
		if d.running && (act.Interval != intervalBefore || (!enabledBefore && act.Enabled)) {
			act.DueAt = now + int64(act.Interval)
		}
	}
}

// Status — снимок состояния для фронтенда.
func (d *Demo) Status() map[string]interface{} {
	d.mu.Lock()
	defer d.mu.Unlock()
	acts := map[string]interface{}{}
	for name, a := range d.actions {
		next := 0
		if d.running && a.Enabled {
			left := int(a.DueAt - time.Now().Unix())
			if left < 0 {
				left = 0
			}
			next = left
		}
		acts[name] = map[string]interface{}{
			"enabled":  a.Enabled,
			"interval": a.Interval,
			"next_in":  next,
		}
	}
	return map[string]interface{}{
		"enabled":  d.running,
		"actions":  acts,
		"orders":   d.orders,
		"supplies": d.supplies,
	}
}

// Start включает демонстрацию. Повторный вызов только обновляет настройки.
func (d *Demo) Start(patches map[string]demoPatch) {
	d.Apply(patches)
	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		return
	}
	d.running = true
	d.stop = make(chan struct{})
	now := time.Now().Unix()
	// Разносим первые срабатывания по времени: иначе при старте все шесть
	// действий сработают в одну секунду и на дашборде будет каша вместо
	// последовательной демонстрации.
	for i, name := range demoOrder {
		a := d.actions[name]
		a.DueAt = now + int64(a.Interval) + int64(i)*3
	}
	stop := d.stop
	d.mu.Unlock()

	d.logDemo("демонстрация запущена")
	go d.loop(stop)
}

// Stop выключает демонстрацию и возвращает снятые демо-режимом блюда в продажу.
func (d *Demo) Stop() {
	d.mu.Lock()
	if !d.running {
		d.mu.Unlock()
		return
	}
	d.running = false
	close(d.stop)
	owned := make([]int, 0, len(d.stopped))
	for id := range d.stopped {
		owned = append(owned, id)
	}
	d.mu.Unlock()

	for _, id := range owned {
		_ = store.RemoveManualStop(id, "демонстрация")
	}
	d.mu.Lock()
	d.stopped = map[int]bool{}
	d.mu.Unlock()

	d.logDemo("демонстрация остановлена")
}

func (d *Demo) logDemo(msg string) {
	store.mu.Lock()
	store.log("demo", msg)
	store.mu.Unlock()
	store.broadcast("demo", msg)
}

// loop — один тикер на весь демо-режим. Каждое действие сверяется со своим
// дедлайном, поэтому интервалы независимы и не требуют горутину на каждый тип.
func (d *Demo) loop(stop chan struct{}) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			d.tick()
		}
	}
}

func (d *Demo) tick() {
	d.mu.Lock()
	if !d.running {
		d.mu.Unlock()
		return
	}
	now := time.Now().Unix()
	due := []string{}
	for name, a := range d.actions {
		if a.Enabled && now >= a.DueAt {
			a.DueAt = now + int64(a.Interval)
			due = append(due, name)
		}
	}
	d.mu.Unlock()

	sort.Strings(due) // детерминированный порядок — удобнее читать журнал
	for _, name := range due {
		switch name {
		case demoOrders:
			d.runOrders()
		case demoSupplies:
			d.runSupplies()
		case demoWriteoffs:
			d.runWriteoffs()
		case demoAuth:
			d.runAuth()
		case demoStopList:
			d.runStopList()
		case demoAdjust:
			d.runAdjust()
		case demoCustomize:
			d.runCustomize()
		}
	}
}

func (d *Demo) intn(n int) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	if n <= 1 {
		return 0
	}
	return d.rnd.Intn(n)
}

func (d *Demo) floatn() float64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.rnd.Float64()
}

func (d *Demo) runOrders() {
	avail := store.availSnapshot()
	ok := []int{}
	for _, id := range store.dishIDs() {
		if avail[id] >= 1 {
			ok = append(ok, id)
		}
	}
	if len(ok) == 0 {
		return
	}
	n := 1 + d.intn(2)
	items := []OrderItem{}
	for i := 0; i < n; i++ {
		items = append(items, OrderItem{DishID: ok[d.intn(len(ok))], Qty: 1 + d.intn(2)})
	}
	if _, err := store.CreateOrder("", items); err == nil {
		d.mu.Lock()
		d.orders++
		d.mu.Unlock()
	}
}

func (d *Demo) runSupplies() {
	prods := store.productIDs()
	if len(prods) == 0 {
		return
	}
	n := 2 + d.intn(3)
	items := []SupplyItem{}
	for i := 0; i < n; i++ {
		pid := prods[d.intn(len(prods))]
		unit := store.unitOf(pid)
		qty := 1.0
		if isCountUnit(unit) {
			qty = float64(5 + d.intn(25))
		} else {
			qty = math.Round((1+d.floatn()*8)*10) / 10
		}
		items = append(items, SupplyItem{
			ProductID: pid,
			Qty:       qty,
			ShelfDays: 2 + d.intn(10),
		})
	}
	if _, err := store.CreateSupply("", items); err == nil {
		d.mu.Lock()
		d.supplies++
		d.mu.Unlock()
	}
}

// runWriteoffs имитирует спонтанную порчу небольшой части живой партии.
func (d *Demo) runWriteoffs() {
	batches := store.liveBatchSample()
	if len(batches) == 0 {
		return
	}
	b := batches[d.intn(len(batches))]
	qty := b.Qty
	if !isCountUnit(b.Unit) {
		qty = math.Round(b.Qty*0.1*1000) / 1000
		if qty <= 0 {
			return
		}
	}
	if _, err := store.ManualWriteoff(b.ID, qty, "порча (демо)", "демонстрация"); err == nil {
		store.AutoWriteoff()
	}
}

// runAuth имитирует активность персонала. Сессия сразу закрывается,
// чтобы демо не накапливало их в памяти.
func (d *Demo) runAuth() {
	logins := []string{"staff", "admin"}
	login := logins[d.intn(len(logins))]
	sess, err := store.DemoSession(login)
	if err != nil {
		return
	}
	store.Logout(sess.Token)
}

// runStopList то снимает блюдо с продажи, то возвращает ранее снятое демо.
func (d *Demo) runStopList() {
	d.mu.Lock()
	owned := make([]int, 0, len(d.stopped))
	for id := range d.stopped {
		owned = append(owned, id)
	}
	d.mu.Unlock()

	// Если демо уже что-то сняло — чаще возвращаем обратно, чтобы стоп-лист жил.
	if len(owned) > 0 && d.intn(100) < 60 {
		id := owned[d.intn(len(owned))]
		if err := store.RemoveManualStop(id, "демонстрация"); err == nil {
			d.mu.Lock()
			delete(d.stopped, id)
			d.mu.Unlock()
		}
		return
	}

	avail := store.availSnapshot()
	candidates := []int{}
	for _, id := range store.dishIDs() {
		if avail[id] >= 3 {
			candidates = append(candidates, id)
		}
	}
	if len(candidates) == 0 {
		return
	}
	id := candidates[d.intn(len(candidates))]
	reasons := []string{
		"закончилась фирменная подача",
		"отзыв шеф-повара",
		"проверка качества кухни",
		"не завезли упаковку",
	}
	if err := store.AddManualStop(id, reasons[d.intn(len(reasons))], "демонстрация"); err == nil {
		d.mu.Lock()
		d.stopped[id] = true
		d.mu.Unlock()
	}
}

// runAdjust имитирует инвентаризацию — небольшую корректировку остатка.
func (d *Demo) runAdjust() {
	batches := store.liveBatchSample()
	if len(batches) == 0 {
		return
	}
	b := batches[d.intn(len(batches))]
	delta := 1.0
	if !isCountUnit(b.Unit) {
		delta = math.Round((0.1+d.floatn()*0.5)*1000) / 1000
	}
	newQty := math.Max(0, b.Qty-delta)
	_, _ = store.AdjustBatchQty(b.ID, newQty, "инвентаризация (демо)", "демонстрация")
}

// runCustomize имитирует кастомизацию заказа — добавляет или убирает ингредиент.
func (d *Demo) runCustomize() {
	// Берём самый свежий заказ со статусом new или cooking.
	var best *Order
	for i := len(store.orders) - 1; i >= 0; i-- {
		o := &store.orders[i]
		if o.Status == "new" || o.Status == "cooking" {
			best = o
			break
		}
	}
	if best == nil || len(best.Items) == 0 {
		return
	}
	// Случайно выбираем блюдо из заказа.
	item := best.Items[d.intn(len(best.Items))]
	// Случайный продукт.
	prods := store.productIDs()
	if len(prods) == 0 {
		return
	}
	productID := prods[d.intn(len(prods))]
	product := store.unitOf(productID)
	qty := 0.5
	if isCountUnit(product) {
		qty = 1.0
	}
	action := "add"
	// Иногда убираем, если в блюде есть хотя бы один ингредиент.
	if len(item.Customizations) > 0 {
		action = "remove"
	}
	_, delta, err := store.CustomizeOrderDish(best.ID, item.DishID, productID, action, qty, "демонстрация")
	if err == nil {
		store.log("demo", fmt.Sprintf("Кастомизация: заказ %s, %s ингредиента на %.0f ₽",
			best.Number, action, delta))
	}
}

var demo = NewDemo()

// ========================
// HTTP-РОУТЫ ДЕМО-РЕЖИМА
// ========================

// demoPatch — частичное обновление. nil-значения означают «не менять».
type demoPatch struct {
	Enabled  *bool `json:"enabled"`
	Interval *int  `json:"interval"`
}

// parseDemoBody читает частичное обновление действий из тела запроса.
func parseDemoBody(r *http.Request) (map[string]demoPatch, error) {
	raw := map[string]demoPatch{}
	if r.Body == nil {
		return nil, nil
	}
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil && !strings.Contains(err.Error(), "EOF") {
		return nil, err
	}
	return raw, nil
}

func registerDemoRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/demo", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		writeJSON(w, http.StatusOK, demo.Status())
	})

	// Старт/стоп — любой авторизованный пользователь (демо можно запустить
	// и для тренировки, не обязательно быть админом).
	mux.HandleFunc("/api/demo/start", requireAuth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		patches, err := parseDemoBody(r)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "некорректный JSON: "+err.Error())
			return
		}
		demo.Start(patches)
		writeJSON(w, http.StatusOK, demo.Status())
	}))

	mux.HandleFunc("/api/demo/stop", requireAuth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		demo.Stop()
		writeJSON(w, http.StatusOK, demo.Status())
	}))

	// Точечная настройка — только администратору.
	mux.HandleFunc("/api/demo/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		requireAdmin(func(w http.ResponseWriter, r *http.Request) {
			patches, err := parseDemoBody(r)
			if err != nil {
				writeErr(w, http.StatusBadRequest, "некорректный JSON: "+err.Error())
				return
			}
			demo.Apply(patches)
			writeJSON(w, http.StatusOK, demo.Status())
		})(w, r)
	})

	// Разовый запуск конкретного действия — только администратору.
	mux.HandleFunc("/api/demo/run", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		requireAdmin(func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Action string `json:"action"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeErr(w, http.StatusBadRequest, "некорректный JSON")
				return
			}
			switch body.Action {
			case demoOrders:
				demo.runOrders()
			case demoSupplies:
				demo.runSupplies()
			case demoWriteoffs:
				demo.runWriteoffs()
			case demoAuth:
				demo.runAuth()
			case demoStopList:
				demo.runStopList()
			case demoAdjust:
				demo.runAdjust()
			case demoCustomize:
				demo.runCustomize()
			default:
				writeErr(w, http.StatusBadRequest, fmt.Sprintf("неизвестное действие: %s", body.Action))
				return
			}
			writeJSON(w, http.StatusOK, demo.Status())
		})(w, r)
	})
}
