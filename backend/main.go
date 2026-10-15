package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ========================
// Утилиты
// ========================

func randomToken() string {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

func hashPassword(pw string) string {
	h := sha256.Sum256([]byte(pw))
	return hex.EncodeToString(h[:])
}

func verifyPassword(pw, hashed string) bool {
	return subtle.ConstantTimeCompare([]byte(hashPassword(pw)), []byte(hashed)) == 1
}

// ========================
// Единицы измерения
// ========================

const (
	kindWeight = "weight"
	kindVolume = "volume"
	kindCount  = "count"
)

func unitKind(unit string) string {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "кг", "г", "гр", "грамм", "граммов":
		return kindWeight
	case "л", "мл", "литр", "литров":
		return kindVolume
	}
	return kindCount
}

func isCountUnit(unit string) bool {
	return unitKind(unit) == kindCount
}

// roundMeasure округляет до 3 знаков (граммы / миллилитры).
func roundMeasure(v float64) float64 { return math.Round(v*1000) / 1000 }

// roundCount округляет до целого (штуки).
func roundCount(v float64) float64 { return math.Round(v) }

// normalizeQty приводит количество к допустимой точности:
//
//	штуки — целые, вес/объем — до 3 знаков (граммы/миллилитры).
func normalizeQty(qty float64, unit string) float64 {
	if isCountUnit(unit) {
		return math.Round(qty)
	}
	return roundMeasure(qty)
}

// fmtQty human-readable: 1.5 кг / 250 г / 2 л / 400 мл / 12 шт.
func fmtQty(qty float64, unit string) string {
	switch unitKind(unit) {
	case kindWeight:
		g := math.Round(qty * 1000)
		if g < 1000 {
			return fmt.Sprintf("%.0f г", g)
		}
		return fmt.Sprintf("%.2f кг", qty)
	case kindVolume:
		ml := math.Round(qty * 1000)
		if ml < 1000 {
			return fmt.Sprintf("%.0f мл", ml)
		}
		return fmt.Sprintf("%.2f л", qty)
	}
	return fmt.Sprintf("%.0f шт", math.Round(qty))
}

// normalizeRecipeQty приводит норму расхода к допустимому значению.
// Штучные ингредиенты — округляем вверх до целого (2.5 яйца → 3).
func normalizeRecipeQty(qty float64, unit string) float64 {
	if isCountUnit(unit) {
		return math.Ceil(qty)
	}
	return roundMeasure(qty)
}

// ========================
// Модели
// ========================

type Product struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Unit string `json:"unit"`
	Kind string `json:"kind"`
	// Cost — себестоимость одной единицы (руб за кг/л/шт). Нужна для честного
	// пересчёта чека, когда гость просит добавить или убрать ингредиент.
	Cost float64 `json:"cost"`
}

type Batch struct {
	ID          int       `json:"id"`
	ProductID   int       `json:"product_id"`
	Product     string    `json:"product"`
	Unit        string    `json:"unit"`
	SupplyID    int       `json:"supply_id"`
	BatchNumber string    `json:"batch_number"`
	Qty         float64   `json:"qty"`
	InitialQty  float64   `json:"initial_qty"`
	ArrivalDate time.Time `json:"arrival_date"`
	ExpiryDate  time.Time `json:"expiry_date"`
	// Cost — себестоимость одной единицы (руб за кг/л/шт). Нужна для расчёта
	// кастомизации и аналитики маржинальности.
	Cost float64 `json:"cost"`
}

type Ingredient struct {
	ProductID int     `json:"product_id"`
	Qty       float64 `json:"qty"`
}

type Dish struct {
	ID          int          `json:"id"`
	Name        string       `json:"name"`
	Price       float64      `json:"price"`
	Ingredients []Ingredient `json:"ingredients"`
}

type SupplyItem struct {
	ProductID int     `json:"product_id"`
	Qty       float64 `json:"qty"`
	ShelfDays int     `json:"shelf_days"`
	// Cost — закупочная цена единицы; если не указана, берётся из справочника.
	Cost float64 `json:"cost,omitempty"`
}

type Supply struct {
	ID      int          `json:"id"`
	Number  string       `json:"number"`
	Date    time.Time    `json:"date"`
	Items   []SupplyItem `json:"items"`
	Batches []Batch      `json:"batches"`
}

type OrderItem struct {
	DishID int `json:"dish_id"`
	Qty    int `json:"qty"`
	// Customizations — добавки и убранные ингредиенты этого блюда в заказе.
	Customizations []OrderItemCustomization `json:"customizations,omitempty"`
}

// OrderItemCustomization — добавка ("add") или исключение ("remove") ингредиента
// к блюду на этапе готовки. Price — дельта чека: для добавки положительная,
// для убираемого ингредиента отрицательная.
type OrderItemCustomization struct {
	ProductID   int     `json:"product_id"`
	ProductName string  `json:"product_name"`
	Action      string  `json:"action"`
	Qty         float64 `json:"qty"`
	Unit        string  `json:"unit"`
	Display     string  `json:"display"`
	Price       float64 `json:"price"`
}

type StockRow struct {
	ProductID    int     `json:"product_id"`
	Product      string  `json:"product"`
	Unit         string  `json:"unit"`
	Kind         string  `json:"kind"`
	TotalQty     float64 `json:"total_qty"`
	TotalDisplay string  `json:"total_display"`
	NextExpiry   string  `json:"next_expiry"`
	DaysToExpiry float64 `json:"days_to_expiry"`
	Batches      []Batch `json:"batches"`
}

type StopItem struct {
	DishID      int      `json:"dish_id"`
	Dish        string   `json:"dish"`
	Missing     []string `json:"missing"`
	MaxPortions int      `json:"max_portions"`
	// Manual — блюдо убрано из продажи решением персонала, а не из-за дефицита.
	Manual       bool   `json:"manual"`
	ManualReason string `json:"manual_reason,omitempty"`
}

type HotItem struct {
	DishID   int      `json:"dish_id"`
	Dish     string   `json:"dish"`
	Price    float64  `json:"price"`
	Score    float64  `json:"score"`
	Reasons  []string `json:"reasons"`
	MinDays  float64  `json:"min_days"`
	Portions int      `json:"portions"`
}

type Order struct {
	ID        int         `json:"id"`
	Number    string      `json:"number"`
	Items     []OrderItem `json:"items"`
	CreatedAt time.Time   `json:"created_at"`
	// Status — новый, на кухне, готовится, подан, готов к выдаче, отменён.
	Status string `json:"status"`
	// Customizations — глобальные добавки/уборки ко всему заказу.
	Customizations []OrderCustomization `json:"customizations,omitempty"`
	// Total — итоговая стоимость (с учётом кастомизаций и скидок).
	Total float64 `json:"total"`
	// Discount — применённая скидка (проценты).
	Discount float64 `json:"discount"`
	// DiscountReason — название акции.
	DiscountReason string `json:"discount_reason,omitempty"`
}

type Writeoff struct {
	ID          int       `json:"id"`
	BatchID     int       `json:"batch_id"`
	Product     string    `json:"product"`
	BatchNumber string    `json:"batch_number"`
	Qty         float64   `json:"qty"`
	Unit        string    `json:"unit"`
	Display     string    `json:"display"`
	Reason      string    `json:"reason"`
	CreatedAt   time.Time `json:"created_at"`
}

type Event struct {
	ID        int       `json:"id"`
	Type      string    `json:"type"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

// ========================
// ДОПОЛНИТЕЛЬНЫЕ МОДЕЛИ
// ========================

type Promotion struct {
	ID         int       `json:"id"`
	Type       string    `json:"type"` // "birthday", "min_sum", "happy_hour", "near_expiry"
	Name       string    `json:"name"`
	Discount   float64   `json:"discount"`
	MinSum     float64   `json:"min_sum"`
	HappyHour  [2]int    `json:"happy_hour"` // часы начала и конца
	ActiveDays []string  `json:"active_days"`
	ValidFrom  time.Time `json:"valid_from"`
	ValidUntil time.Time `json:"valid_until"`
	Enabled    bool      `json:"enabled"`
}

type SupplySchedule struct {
	ProductID           int     `json:"product_id"`
	DaysBetween         int     `json:"days_between"`
	MinStockLevel       float64 `json:"min_stock_level"`
	ForecastDays        int     `json:"forecast_days"`
	AvgDailyConsumption float64 `json:"avg_daily_consumption"`
	Enabled             bool    `json:"enabled"`
}

type OrderCustomization struct {
	ID        int     `json:"id"`
	OrderID   int     `json:"order_id"`
	ProductID int     `json:"product_id"`
	Action    string  `json:"action"` // "add", "remove"
	Qty       float64 `json:"qty"`
	Price     float64 `json:"price"`
}

type User struct {
	Login    string `json:"login"`
	Password string `json:"-"`
	Role     string `json:"role"`
}

type Session struct {
	Token  string    `json:"-"`
	Login  string    `json:"login"`
	Role   string    `json:"role"`
	Expire time.Time `json:"-"`
}

// Хранилище

type Store struct {
	mu          sync.RWMutex
	products    []Product
	batches     []Batch
	dishes      []Dish
	supplies    []Supply
	orders      []Order
	writeoffs   []Writeoff
	events      []Event
	seq         map[string]int
	subs        map[chan string]bool
	subMu       sync.Mutex
	users       map[string]User     // логин -> пользователь
	sessions    map[string]*Session // токен -> сессия
	sessMu      sync.RWMutex
	manualStops map[int]string // dishID -> причина ручного стопа
	// --- Новое: акции, график поставок, кастомизация заказов ---
	promotions []Promotion
	schedules  map[int]*SupplySchedule // productID -> график
	customize  []OrderCustomization
	// consume — история расхода продукта по дням (для аналитики автозаказа).
	consume map[int]map[string]float64
}

func NewStore() *Store {
	return &Store{
		seq:         map[string]int{},
		subs:        map[chan string]bool{},
		users:       map[string]User{},
		sessions:    map[string]*Session{},
		manualStops: map[int]string{},
		schedules:   map[int]*SupplySchedule{},
		consume:     map[int]map[string]float64{},
	}
}

func (s *Store) nextID(kind string) int {
	s.seq[kind]++
	return s.seq[kind]
}

func (s *Store) log(typ, msg string) {
	s.events = append(s.events, Event{
		ID: len(s.events) + 1, Type: typ, Message: msg, CreatedAt: time.Now(),
	})
	if len(s.events) > 300 {
		s.events = s.events[len(s.events)-300:]
	}
}

// ---- СИД ----

func (s *Store) seed() {
	names := []struct{ n, u, k, c string }{
		{"Куриное филе", "кг", kindWeight, "180"}, {"Говядина", "кг", kindWeight, "350"}, {"Лосось", "кг", kindWeight, "520"},
		{"Сливки 20%", "л", kindVolume, "85"}, {"Молоко", "л", kindVolume, "60"}, {"Сыр пармезан", "кг", kindWeight, "480"},
		{"Помидоры черри", "кг", kindWeight, "150"}, {"Салат романо", "кг", kindWeight, "90"}, {"Авокадо", "кг", kindWeight, "120"},
		{"Лосось слабосолёный", "кг", kindWeight, "450"}, {"Бекон", "кг", kindWeight, "280"}, {"Яйцо куриное", "шт", kindCount, "12"},
		{"Спагетти", "кг", kindWeight, "65"}, {"Рис арборио", "кг", kindWeight, "95"}, {"Креветки", "кг", kindWeight, "420"},
		{"Картофель", "кг", kindWeight, "35"}, {"Лук репчатый", "кг", kindWeight, "25"}, {"Масло оливковое", "л", kindVolume, "180"},
		{"Мука", "кг", kindWeight, "45"}, {"Сахар", "кг", kindWeight, "40"}, {"Клубника", "кг", kindWeight, "280"}, {"Творог", "кг", kindWeight, "110"},
		{"Лимон", "кг", kindWeight, "100"}, {"Чеснок", "кг", kindWeight, "200"}, {"Багет", "кг", kindWeight, "55"},
		{"Кокосовые сливки", "л", kindVolume, "95"},
	}
	pmap := map[string]int{}
	punit := map[string]string{}
	pcost := map[string]float64{}
	for i, n := range names {
		c, _ := strconv.ParseFloat(n.c, 64)
		p := Product{ID: i + 1, Name: n.n, Unit: n.u, Kind: n.k, Cost: c}
		s.products = append(s.products, p)
		pmap[n.n] = p.ID
		punit[n.n] = n.u
		pcost[n.n] = c
	}
	s.seq["product"] = len(names)

	dishes := []struct {
		name  string
		price float64
		rec   []string
	}{
		{"Цезарь с курицей", 490, []string{"Куриное филе|0.15", "Салат романо|0.08", "Помидоры черри|0.06", "Сыр пармезан|0.02", "Багет|0.3", "Масло оливковое|0.02"}},
		{"Стейк из лосося", 890, []string{"Лосось|0.18", "Картофель|0.15", "Лимон|0.3", "Масло оливковое|0.02"}},
		{"Карбонара", 520, []string{"Спагетти|0.1", "Бекон|0.06", "Яйцо куриное|2", "Сыр пармезан|0.03", "Сливки 20%|0.05"}},
		{"Ризотто с креветками", 740, []string{"Рис арборио|0.09", "Креветки|0.1", "Сливки 20%|0.06", "Лук репчатый|0.03", "Сыр пармезан|0.02"}},
		{"Боул с авокадо и лососем", 690, []string{"Авокадо|0.15", "Лосось слабосолёный|0.08", "Рис арборио|0.08", "Салат романо|0.05", "Помидоры черри|0.05"}},
		{"Говядина по-строгановски", 820, []string{"Говядина|0.17", "Сливки 20%|0.08", "Лук репчатый|0.04", "Картофель|0.15"}},
		{"Том ям", 620, []string{"Креветки|0.12", "Кокосовые сливки|0.1", "Куриное филе|0.1"}},
		{"Сырный суп", 380, []string{"Сыр пармезан|0.04", "Картофель|0.12", "Сливки 20%|0.05", "Мука|0.02"}},
		{"Клубничный чизкейк", 420, []string{"Клубника|0.08", "Творог|0.1", "Сахар|0.04", "Мука|0.05", "Яйцо куриное|1"}},
		{"Омлет с беконом", 340, []string{"Яйцо куриное|3", "Бекон|0.05", "Молоко|0.06", "Сыр пармезан|0.02"}},
		{"Паста болоньезе", 540, []string{"Спагетти|0.1", "Говядина|0.12", "Помидоры черри|0.07", "Лук репчатый|0.03", "Чеснок|0.005"}},
		{"Греческий салат", 420, []string{"Помидоры черри|0.1", "Салат романо|0.07", "Сыр пармезан|0.03", "Масло оливковое|0.02"}},
	}
	for i, d := range dishes {
		dish := Dish{ID: i + 1, Name: d.name, Price: d.price}
		for _, r := range d.rec {
			parts := strings.Split(r, "|")
			q, _ := strconv.ParseFloat(parts[1], 64)
			if id, ok := pmap[parts[0]]; ok {
				// Нормализуем норму расхода по типу продукта:
				// штучные — округляем вверх до целого, вес/объём — до 3 знаков.
				q = normalizeRecipeQty(q, punit[parts[0]])
				dish.Ingredients = append(dish.Ingredients, Ingredient{ProductID: id, Qty: q})
			}
		}
		s.dishes = append(s.dishes, dish)
	}
	s.seq["dish"] = len(dishes)

	now := time.Now()
	seed := []struct {
		prod string
		qty  float64
		days int
		expo int
	}{
		{"Куриное филе", 8, 5, 2}, {"Куриное филе", 5, 5, 4},
		{"Говядина", 6, 4, 1}, {"Говядина", 7, 4, 3},
		{"Лосось", 4, 3, 1}, {"Лосось", 3, 3, 2},
		{"Сливки 20%", 9, 7, 5}, {"Молоко", 10, 6, 3},
		{"Сыр пармезан", 5, 30, 25}, {"Помидоры черри", 6, 4, 1},
		{"Салат романо", 4, 3, 1}, {"Авокадо", 12, 4, 2},
		{"Лосось слабосолёный", 3, 6, 2}, {"Бекон", 4, 10, 7},
		{"Яйцо куриное", 90, 20, 15}, {"Спагетти", 12, 180, 160},
		{"Рис арборио", 10, 240, 200}, {"Креветки", 4, 2, 1},
		{"Картофель", 20, 21, 18}, {"Лук репчатый", 12, 30, 28},
		{"Масло оливковое", 6, 300, 280}, {"Мука", 15, 180, 150},
		{"Сахар", 12, 365, 340}, {"Клубника", 3, 3, 1},
		{"Творог", 4, 5, 2}, {"Лимон", 20, 14, 11},
		{"Чеснок", 2, 60, 55}, {"Багет", 15, 2, 1},
	}
	sid := s.nextID("supply")
	sup := Supply{ID: sid, Number: "SUP-0001", Date: now.Add(-24 * time.Hour)}
	for i, x := range seed {
		pid := pmap[x.prod]
		unit := "шт"
		for _, p := range s.products {
			if p.ID == pid {
				unit = p.Unit
			}
		}
		b := Batch{
			ID: s.nextID("batch"), ProductID: pid, Product: x.prod, Unit: unit,
			SupplyID: sid, BatchNumber: fmt.Sprintf("B-%04d", i+1),
			Qty: x.qty, InitialQty: x.qty,
			ArrivalDate: now.Add(-24 * time.Hour),
			ExpiryDate:  now.Add(time.Duration(x.expo) * 24 * time.Hour),
			Cost:        pcost[x.prod],
		}
		s.batches = append(s.batches, b)
		sup.Batches = append(sup.Batches, b)
	}
	s.supplies = append(s.supplies, sup)
	s.log("system", "База инициализирована: "+strconv.Itoa(len(s.products))+" продуктов, "+strconv.Itoa(len(s.dishes))+" блюд")

	// Демо-акции по типовым параметрам — сразу видны в конструкторе.
	s.promotions = []Promotion{
		{ID: s.nextID("promotion"), Type: "happy_hour", Name: "Счастливые часы 15–17",
			Discount: 15, HappyHour: [2]int{15, 17}, Enabled: true, ValidFrom: now},
		{ID: s.nextID("promotion"), Type: "min_sum", Name: "Чек от 2000 ₽",
			Discount: 10, MinSum: 2000, Enabled: true, ValidFrom: now},
		{ID: s.nextID("promotion"), Type: "birthday", Name: "День рождения",
			Discount: 15, Enabled: true, ValidFrom: now},
		{ID: s.nextID("promotion"), Type: "near_expiry", Name: "Утилизируем вкусно",
			Discount: 25, Enabled: true, ValidFrom: now},
	}
	s.log("system", "Загружены типовые акции: счастливые часы, мин. чек, именинники, истекающий срок")

	// Базовый график поставок для скоропортящихся продуктов.
	for _, p := range s.products {
		if p.Kind == kindCount {
			continue
		}
		avg := s.avgDailyConsumption(p.ID)
		if avg <= 0 {
			continue
		}
		s.schedules[p.ID] = &SupplySchedule{
			ProductID: p.ID, DaysBetween: 2, ForecastDays: 1,
			MinStockLevel:       roundMeasure(avg * horizonDays),
			AvgDailyConsumption: avg,
			Enabled:             true,
		}
	}

	// Пользователи по умолчанию
	s.users = map[string]User{
		"admin": {Login: "admin", Password: hashPassword("admin"), Role: "admin"},
		"staff": {Login: "staff", Password: hashPassword("staff"), Role: "staff"},
	}
	s.log("system", "Созданы пользователи: admin/admin, staff/staff")
}

// ---- ЛОГИКА ----

func daysLeft(b Batch, now time.Time) float64 {
	return b.ExpiryDate.Sub(now).Hours() / 24
}

func (s *Store) Stock() []StockRow {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	byProd := map[int]*StockRow{}
	var order []int
	for _, b := range s.batches {
		if b.Qty <= 0.0000001 {
			continue
		}
		r, ok := byProd[b.ProductID]
		if !ok {
			r = &StockRow{ProductID: b.ProductID, Product: b.Product, Unit: b.Unit, Kind: unitKind(b.Unit)}
			byProd[b.ProductID] = r
			order = append(order, b.ProductID)
		}
		r.TotalQty += b.Qty
	}
	for _, b := range s.batches {
		if b.Qty <= 0.0000001 {
			continue
		}
		byProd[b.ProductID].Batches = append(byProd[b.ProductID].Batches, b)
	}
	out := make([]StockRow, 0, len(order))
	for _, id := range order {
		r := byProd[id]
		sort.Slice(r.Batches, func(i, j int) bool {
			return r.Batches[i].ExpiryDate.Before(r.Batches[j].ExpiryDate)
		})
		if len(r.Batches) > 0 {
			n := r.Batches[0]
			r.NextExpiry = n.ExpiryDate.Format("02.01.2006")
			r.DaysToExpiry = math.Round(daysLeft(n, now)*10) / 10
		}
		r.TotalQty = normalizeQty(r.TotalQty, r.Unit)
		r.TotalDisplay = fmtQty(r.TotalQty, r.Unit)
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].DaysToExpiry < out[j].DaysToExpiry
	})
	return out
}

// availSnapshot — потокобезопасный снимок доступности блюд.
// Ручные стоп-листы уже учтены: снятое блюдо недоступно к заказу.
func (s *Store) availSnapshot() map[int]int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.applyManualStops(s.availability())
}

func (s *Store) availability() map[int]int {
	res := map[int]int{}
	for _, d := range s.dishes {
		maxP := math.MaxInt32
		for _, ing := range d.Ingredients {
			total := 0.0
			for _, b := range s.batches {
				if b.ProductID == ing.ProductID && b.Qty > 0 {
					total += b.Qty
				}
			}
			p := int(math.Floor(total / ing.Qty))
			if p < maxP {
				maxP = p
			}
		}
		if maxP == math.MaxInt32 {
			maxP = 0
		}
		res[d.ID] = maxP
	}
	return res
}

// applyManualStops обнуляет доступность блюд, снятых с продажи вручную.
// Вызывается только из-под захваченного s.mu.
func (s *Store) applyManualStops(avail map[int]int) map[int]int {
	for id := range s.manualStops {
		avail[id] = 0
	}
	return avail
}

// AddManualStop убирает блюдо из продажи по решению персонала.
// Блюдо остаётся в стоп-листе, даже если продуктов на складе хватает.
func (s *Store) AddManualStop(dishID int, reason string, who string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	name := ""
	for _, d := range s.dishes {
		if d.ID == dishID {
			name = d.Name
			break
		}
	}
	if name == "" {
		return fmt.Errorf("блюдо %d не найдено", dishID)
	}
	if _, ok := s.manualStops[dishID]; ok {
		return fmt.Errorf("блюдо «%s» уже снято с продажи", name)
	}
	if reason == "" {
		reason = "снято с продажи вручную"
	}
	s.manualStops[dishID] = reason
	s.log("stoplist", fmt.Sprintf("%s: «%s» снято с продажи — %s", who, name, reason))
	s.broadcast("stoplist", name)
	return nil
}

// RemoveManualStop возвращает блюдо в продажу (снимает только ручную блокировку;
// если продуктов не хватает, блюдо останется в стоп-листе автоматически).
func (s *Store) RemoveManualStop(dishID int, who string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	reason, ok := s.manualStops[dishID]
	if !ok {
		return fmt.Errorf("блюдо %d не было снято вручную", dishID)
	}
	delete(s.manualStops, dishID)
	name := ""
	for _, d := range s.dishes {
		if d.ID == dishID {
			name = d.Name
			break
		}
	}
	s.log("stoplist", fmt.Sprintf("%s: «%s» возвращено в продажу (причина снятия: %s)", who, name, reason))
	s.broadcast("stoplist", name)
	return nil
}

// ========================
// УПРАВЛЕНИЕ ПРОДУКТАМИ
// ========================

// AddProduct добавляет новый продукт в справочник.
// cost — себестоимость единицы (руб за кг/л/шт); нужна для пересчёта чека
// при кастомизации заказа и для плановой себестоимости новых партий.
func (s *Store) AddProduct(name, unit string, cost float64) (Product, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name = strings.TrimSpace(name)
	if name == "" {
		return Product{}, fmt.Errorf("название продукта не может быть пустым")
	}
	if unit == "" {
		unit = "кг"
	}
	if cost < 0 {
		return Product{}, fmt.Errorf("себестоимость не может быть отрицательной")
	}
	for _, p := range s.products {
		if strings.EqualFold(p.Name, name) {
			return Product{}, fmt.Errorf("продукт «%s» уже существует", name)
		}
	}
	p := Product{
		ID:   s.nextID("product"),
		Name: name,
		Unit: unit,
		Kind: unitKind(unit),
		Cost: roundMeasure(cost),
	}
	s.products = append(s.products, p)
	s.log("product", fmt.Sprintf("Добавлен продукт: %s (%s)", name, unit))
	s.broadcast("stock", "product_add")
	return p, nil
}

// SetProductCost меняет себестоимость продукта.
func (s *Store) SetProductCost(productID int, cost float64, who string) (Product, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cost < 0 {
		return Product{}, fmt.Errorf("себестоимость не может быть отрицательной")
	}
	for i := range s.products {
		if s.products[i].ID != productID {
			continue
		}
		s.products[i].Cost = roundMeasure(cost)
		s.log("product", fmt.Sprintf("%s: себестоимость «%s» → %.2f ₽/%s",
			who, s.products[i].Name, s.products[i].Cost, s.products[i].Unit))
		s.broadcast("stock", "product_cost")
		return s.products[i], nil
	}
	return Product{}, fmt.Errorf("продукт %d не найден", productID)
}

// DeleteProduct удаляет продукт из справочника.
// Нельзя удалить продукт, используемый в рецептах блюд.
func (s *Store) DeleteProduct(productID int, who string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Проверяем использование в рецептах.
	for _, d := range s.dishes {
		for _, ing := range d.Ingredients {
			if ing.ProductID == productID {
				return fmt.Errorf("продукт используется в рецепте блюда «%s»", d.Name)
			}
		}
	}
	// Проверяем наличие партий.
	for _, b := range s.batches {
		if b.ProductID == productID {
			return fmt.Errorf("нельзя удалить продукт с активными партиями на складе")
		}
	}
	// Удаляем продукт.
	for i, p := range s.products {
		if p.ID == productID {
			s.products = append(s.products[:i], s.products[i+1:]...)
			s.log("product", fmt.Sprintf("%s: удалён продукт %d «%s»", who, productID, p.Name))
			s.broadcast("stock", "product_delete")
			return nil
		}
	}
	return fmt.Errorf("продукт %d не найден", productID)
}

// ========================
// УПРАВЛЕНИЕ БЛЮДАМИ
// ========================

// AddDish добавляет новое блюдо с рецептом.
func (s *Store) AddDish(name string, price float64, ingredients []Ingredient, who string) (Dish, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if price <= 0 {
		return Dish{}, fmt.Errorf("цена должна быть > 0")
	}
	for _, d := range s.dishes {
		if strings.EqualFold(d.Name, name) {
			return Dish{}, fmt.Errorf("блюдо «%s» уже существует", name)
		}
	}
	// Валидация ингредиентов.
	punit := map[int]string{}
	for _, p := range s.products {
		punit[p.ID] = p.Unit
	}
	dish := Dish{ID: s.nextID("dish"), Name: name, Price: price}
	for _, ing := range ingredients {
		if ing.Qty <= 0 {
			return Dish{}, fmt.Errorf("количество ингредиента продукта %d должно быть > 0", ing.ProductID)
		}
		if _, ok := punit[ing.ProductID]; !ok {
			return Dish{}, fmt.Errorf("продукт %d не найден в справочнике", ing.ProductID)
		}
		q := normalizeRecipeQty(ing.Qty, punit[ing.ProductID])
		dish.Ingredients = append(dish.Ingredients, Ingredient{ProductID: ing.ProductID, Qty: q})
	}
	s.dishes = append(s.dishes, dish)
	s.log("dish", fmt.Sprintf("%s: добавлено блюдо «%s» (цена: %.0f, ингредиентов: %d)",
		who, name, price, len(dish.Ingredients)))
	s.broadcast("dish", "add")
	return dish, nil
}

// UpdateDish обновляет блюдо (название, цена, ингредиенты).
func (s *Store) UpdateDish(dishID int, name string, price float64, ingredients []Ingredient, who string) (Dish, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if price <= 0 {
		return Dish{}, fmt.Errorf("цена должна быть > 0")
	}
	// Находим блюдо.
	idx := -1
	for i, d := range s.dishes {
		if d.ID == dishID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Dish{}, fmt.Errorf("блюдо %d не найдено", dishID)
	}
	if name != "" {
		for i, d := range s.dishes {
			if i != idx && strings.EqualFold(d.Name, name) {
				return Dish{}, fmt.Errorf("блюдо «%s» уже существует", name)
			}
		}
		s.dishes[idx].Name = name
	}
	s.dishes[idx].Price = price
	// Валидация ингредиентов.
	punit := map[int]string{}
	for _, p := range s.products {
		punit[p.ID] = p.Unit
	}
	newIngs := []Ingredient{}
	for _, ing := range ingredients {
		if ing.Qty <= 0 {
			return Dish{}, fmt.Errorf("количество ингредиента продукта %d должно быть > 0", ing.ProductID)
		}
		if _, ok := punit[ing.ProductID]; !ok {
			return Dish{}, fmt.Errorf("продукт %d не найден в справочнике", ing.ProductID)
		}
		q := normalizeRecipeQty(ing.Qty, punit[ing.ProductID])
		newIngs = append(newIngs, Ingredient{ProductID: ing.ProductID, Qty: q})
	}
	s.dishes[idx].Ingredients = newIngs
	nameOut := s.dishes[idx].Name
	s.log("dish", fmt.Sprintf("%s: обновлено блюдо «%s» (ID: %d)", who, nameOut, dishID))
	s.broadcast("dish", "update")
	return s.dishes[idx], nil
}

// DeleteDish удаляет блюдо.
func (s *Store) DeleteDish(dishID int, who string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := -1
	for i, d := range s.dishes {
		if d.ID == dishID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("блюдо %d не найдено", dishID)
	}
	name := s.dishes[idx].Name
	s.dishes = append(s.dishes[:idx], s.dishes[idx+1:]...)
	delete(s.manualStops, dishID)
	s.log("dish", fmt.Sprintf("%s: удалено блюдо «%s» (ID: %d)", who, name, dishID))
	s.broadcast("dish", "delete")
	return nil
}

// ========================
// АКЦИИ (PROMOTIONS)
// ========================

// AddPromotion добавляет новую акцию.
func (s *Store) AddPromotion(p Promotion, who string) (Promotion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.Discount < 0 || p.Discount > 99 {
		return Promotion{}, fmt.Errorf("скидка должна быть от 0 до 99")
	}
	if p.Type != "birthday" && p.Type != "min_sum" && p.Type != "happy_hour" && p.Type != "near_expiry" {
		return Promotion{}, fmt.Errorf("неизвестный тип акции: %s", p.Type)
	}
	p.ID = s.nextID("promotion")
	p.Enabled = true
	if p.ValidFrom.IsZero() {
		p.ValidFrom = time.Now()
	}
	s.promotions = append(s.promotions, p)
	s.log("promotion", fmt.Sprintf("%s: добавлена акция «%s» (тип: %s, скидка: %.0f%%)",
		who, p.Name, p.Type, p.Discount))
	s.broadcast("promotion", "add")
	return p, nil
}

// UpdatePromotion обновляет акцию.
func (s *Store) UpdatePromotion(promoID int, name string, discount float64, enabled bool, who string) (Promotion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, p := range s.promotions {
		if p.ID == promoID {
			if name != "" {
				s.promotions[i].Name = name
			}
			if discount >= 0 {
				s.promotions[i].Discount = discount
			}
			s.promotions[i].Enabled = enabled
			s.log("promotion", fmt.Sprintf("%s: обновлена акция «%s» (ID: %d)", who, name, promoID))
			return s.promotions[i], nil
		}
	}
	return Promotion{}, fmt.Errorf("акция %d не найдена", promoID)
}

// DeletePromotion удаляет акцию.
func (s *Store) DeletePromotion(promoID int, who string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, p := range s.promotions {
		if p.ID == promoID {
			s.promotions = append(s.promotions[:i], s.promotions[i+1:]...)
			s.log("promotion", fmt.Sprintf("%s: удалена акция «%s» (ID: %d)", who, p.Name, promoID))
			s.broadcast("promotion", "delete")
			return nil
		}
	}
	return fmt.Errorf("акция %d не найдена", promoID)
}

// GetAllPromotions возвращает все акции.
func (s *Store) GetAllPromotions() []Promotion {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Promotion, len(s.promotions))
	copy(out, s.promotions)
	return out
}

// dayAliases — дни недели в любом написании (англ./рус., полные и краткие).
var dayAliases = map[time.Weekday][]string{
	time.Sunday:    {"sun", "вс", "воскресенье"},
	time.Monday:    {"mon", "пн", "понедельник"},
	time.Tuesday:   {"tue", "вт", "вторник"},
	time.Wednesday: {"wed", "ср", "среда"},
	time.Thursday:  {"thu", "чт", "четверг"},
	time.Friday:    {"fri", "пт", "пятница"},
	time.Saturday:  {"sat", "сб", "суббота"},
}

func dayMatches(wd time.Weekday, want string) bool {
	want = strings.ToLower(strings.TrimSpace(want))
	for _, a := range dayAliases[wd] {
		if strings.HasPrefix(strings.ToLower(a), want) && len(want) >= 2 {
			return true
		}
		if strings.EqualFold(a, want) {
			return true
		}
	}
	return false
}

// CalcPromotionDiscount рассчитывает скидку для заказа по действующим акциям.
// Возвращает максимальную подходящую скидку в процентах и её название.
func (s *Store) CalcPromotionDiscount(orderTotal float64, orderTime time.Time) (float64, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.promotionDiscount(orderTotal, orderTime)
}

// promotionDiscount — версия без блокировки (вызывать из-под s.mu).
func (s *Store) promotionDiscount(orderTotal float64, orderTime time.Time) (float64, string) {
	if orderTotal <= 0 {
		return 0, ""
	}
	best := 0.0
	bestReason := ""
	consider := func(p Promotion, reason string) {
		if p.Discount > best {
			best = p.Discount
			bestReason = reason
		}
	}
	for _, p := range s.promotions {
		if !p.Enabled || p.Discount <= best {
			continue
		}
		if !p.ValidFrom.IsZero() && orderTime.Before(p.ValidFrom) {
			continue
		}
		if !p.ValidUntil.IsZero() && orderTime.After(p.ValidUntil) {
			continue
		}
		// Окно активных дней недели, если задано.
		if len(p.ActiveDays) > 0 {
			matched := false
			for _, d := range p.ActiveDays {
				if dayMatches(orderTime.Weekday(), d) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		switch p.Type {
		case "birthday":
			consider(p, fmt.Sprintf("%s: скидка %.0f%% по карте именинника", p.Name, p.Discount))
		case "min_sum":
			if orderTotal >= p.MinSum {
				consider(p, fmt.Sprintf("%s: чек %.0f ₽ ≥ %.0f ₽", p.Name, orderTotal, p.MinSum))
			}
		case "happy_hour":
			h := orderTime.Hour()
			if h >= p.HappyHour[0] && h < p.HappyHour[1] {
				consider(p, fmt.Sprintf("%s: %.0f–%.0f, сейчас %02d:00",
					p.Name, float64(p.HappyHour[0]), float64(p.HappyHour[1]), h))
			}
		case "near_expiry":
			// Скидка только на ту часть чека, что собрана из продуктов с истекающим сроком.
			share := s.nearExpiryShare(orderTime)
			if share > 0 {
				eff := math.Round(p.Discount*share*10) / 10
				if eff > best {
					best = eff
					bestReason = fmt.Sprintf("%s: %.0f%% от чека собрано из продуктов с истекающим сроком",
						p.Name, share*100)
				}
			}
		}
	}
	if best > 99 {
		best = 99
	}
	return math.Round(best*10) / 10, bestReason
}

// nearExpiryShare — доля ингредиентов текущего меню, для которой на складе есть
// партии с остатком срока ≤ 2 дней. Чем больше «рисковых» продуктов в обороте,
// тем сильнее скидка по акции near_expiry.
func (s *Store) nearExpiryShare(now time.Time) float64 {
	risky := map[int]bool{}
	for _, b := range s.batches {
		if b.Qty > 0 && daysLeft(b, now) <= horizonDays {
			risky[b.ProductID] = true
		}
	}
	if len(risky) == 0 {
		return 0
	}
	seen, hit := map[int]bool{}, map[int]bool{}
	for _, d := range s.dishes {
		for _, ing := range d.Ingredients {
			if seen[ing.ProductID] {
				continue
			}
			seen[ing.ProductID] = true
			if risky[ing.ProductID] {
				hit[ing.ProductID] = true
			}
		}
	}
	if len(seen) == 0 {
		return 0
	}
	return float64(len(hit)) / float64(len(seen))
}

// ========================
// ГРАФИК ПОСТАВОК (SUPPLY SCHEDULE)
// ========================

// horizonDays — горизонт планирования для минимально допустимого остатка
// (по требованию — расчёт на 2 дня вперёд).
const horizonDays = 2

// avgDailyConsumption — среднее дневное потребление продукта (литры/кг/шт в день).
// Берётся из фактического расхода по заказам за последние 7 дней; если истории
// нет — используется консервативная оценка по нормам закладки блюд.
// Вызывать только из-под захваченного s.mu.
func (s *Store) avgDailyConsumption(productID int) float64 {
	const window = 7.0
	total := 0.0
	dayKeys := map[string]bool{}
	cutoff := time.Now().Add(-window * 24 * time.Hour)
	for i := range s.orders {
		o := &s.orders[i]
		if o.CreatedAt.Before(cutoff) {
			continue
		}
		for _, it := range o.Items {
			for _, d := range s.dishes {
				if d.ID != it.DishID {
					continue
				}
				for _, ing := range d.Ingredients {
					if ing.ProductID == productID {
						total += ing.Qty * float64(it.Qty)
					}
				}
			}
		}
	}
	// Дни, по которым реально были заказы — делим на фактическое число дней,
	// иначе один вечерний заказ занилит среднее в 7 раз.
	for k := range s.consume[productID] {
		if t, err := time.Parse("2006-01-02", k); err == nil && !t.Before(cutoff) {
			dayKeys[k] = true
		}
	}
	days := float64(len(dayKeys))
	if days < 1 {
		days = 1
	}
	if total > 0 {
		return roundMeasure(total / math.Max(days, 3))
	}
	// Истории нет — оцениваем по нормам закладки: сколько продукт «ест» в порциях.
	norm := 0.0
	for _, d := range s.dishes {
		for _, ing := range d.Ingredients {
			if ing.ProductID == productID {
				norm += ing.Qty
			}
		}
	}
	if norm <= 0 {
		return 0
	}
	return roundMeasure(norm * 10) // 10 порций смешанного меню в день.
}

// unitOfLocked — единица измерения продукта без блокировки.
func (s *Store) unitOfLocked(productID int) string {
	for _, p := range s.products {
		if p.ID == productID {
			return p.Unit
		}
	}
	return "шт"
}

// nameOfLocked — название продукта без блокировки.
func (s *Store) nameOfLocked(productID int) string {
	for _, p := range s.products {
		if p.ID == productID {
			return p.Name
		}
	}
	return fmt.Sprintf("продукт #%d", productID)
}

// stockOfLocked — остаток непросроченных партий без блокировки.
func (s *Store) stockOfLocked(productID int, now time.Time) float64 {
	total := 0.0
	for _, b := range s.batches {
		if b.ProductID == productID && b.Qty > 0 && b.ExpiryDate.After(now) {
			total += b.Qty
		}
	}
	return total
}

// SetSupplySchedule настраивает график поставки для продукта.
// leadDays — сколько дней идёт поставка (время между заказом и приходом товара).
func (s *Store) SetSupplySchedule(productID, daysBetween, leadDays int, minStockLevel float64, who string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if daysBetween < 1 {
		return fmt.Errorf("период между поставками должен быть ≥ 1 дня")
	}
	if leadDays < 0 {
		leadDays = 0
	}
	if minStockLevel < 0 {
		return fmt.Errorf("минимальный остаток не может быть отрицательным")
	}
	found := false
	for _, p := range s.products {
		if p.ID == productID {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("продукт %d не найден", productID)
	}
	unit := s.unitOfLocked(productID)
	minStockLevel = normalizeQty(minStockLevel, unit)
	avg := s.avgDailyConsumption(productID)
	s.schedules[productID] = &SupplySchedule{
		ProductID:           productID,
		DaysBetween:         daysBetween,
		MinStockLevel:       minStockLevel,
		ForecastDays:        leadDays,
		AvgDailyConsumption: avg,
		Enabled:             true,
	}
	s.log("schedule", fmt.Sprintf("%s: график для «%s» — каждые %d дн., плечо %d дн., мин. остаток %s",
		who, s.nameOfLocked(productID), daysBetween, leadDays, fmtQty(minStockLevel, unit)))
	s.broadcast("schedule", fmt.Sprintf("update:%d", productID))
	return nil
}

// DeleteSupplySchedule снимает продукт с автозаказа.
func (s *Store) DeleteSupplySchedule(productID int, who string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.schedules[productID]; !ok {
		return fmt.Errorf("график для продукта %d не настроен", productID)
	}
	delete(s.schedules, productID)
	s.log("schedule", fmt.Sprintf("%s: снят с автозаказа продукт «%s»", who, s.nameOfLocked(productID)))
	s.broadcast("schedule", fmt.Sprintf("delete:%d", productID))
	return nil
}

// scheduleRow — строка графика поставок с расчётными полями.
type scheduleRow struct {
	ProductID           int     `json:"product_id"`
	Product             string  `json:"product"`
	Unit                string  `json:"unit"`
	DaysBetween         int     `json:"days_between"`
	LeadDays            int     `json:"lead_days"`
	MinStockLevel       float64 `json:"min_stock_level"`
	MinStockDisplay     string  `json:"min_stock_display"`
	AvgDailyConsumption float64 `json:"avg_daily_consumption"`
	AvgDailyDisplay     string  `json:"avg_daily_display"`
	CurrentQty          float64 `json:"current_qty"`
	CurrentDisplay      string  `json:"current_display"`
	PlanQty             float64 `json:"plan_qty"`
	PlanDisplay         string  `json:"plan_display"`
	OrderQty            float64 `json:"order_qty"`
	OrderDisplay        string  `json:"order_display"`
	RecommendedMin      float64 `json:"recommended_min"`
	RecommendedMinDisp  string  `json:"recommended_min_display"`
	DaysCovered         float64 `json:"days_covered"`
	Level               string  `json:"level"`
	Enabled             bool    `json:"enabled"`
}

// calcScheduleRow — расчёт одной позиции автозаказа.
//
// Потребность считается на всё «плечо» поставки: период между поставками
// плюс время доставки. Плюс сверху минимальный неснижаемый остаток
// (по умолчанию — норма на horizonDays = 2 дня).
func (s *Store) calcScheduleRow(pid int, sc *SupplySchedule, now time.Time) scheduleRow {
	unit := s.unitOfLocked(pid)
	cur := s.stockOfLocked(pid, now)
	avg := sc.AvgDailyConsumption
	if avg <= 0 {
		avg = s.avgDailyConsumption(pid)
	}
	// Период покрытия: сколько дней нужно «закрыть» запасом к моменту прихода.
	cover := sc.DaysBetween + sc.ForecastDays
	plan := roundMeasure(avg * float64(cover))
	// Рекомендуемый минимум = норма на горизонт (2 дня).
	recMin := roundMeasure(avg * horizonDays)
	minStock := sc.MinStockLevel
	if minStock <= 0 {
		minStock = recMin
	}
	need := roundMeasure(plan + minStock - cur)
	if need < 0 {
		need = 0
	}
	need = normalizeQty(need, unit)
	daysCovered := 0.0
	if avg > 0 {
		daysCovered = math.Round(cur/avg*10) / 10
	}
	level := "ok"
	switch {
	case cur <= 0:
		level = "critical"
	case cur <= minStock:
		level = "warning"
	case cur <= minStock*1.5:
		level = "notice"
	}
	return scheduleRow{
		ProductID: pid, Product: s.nameOfLocked(pid), Unit: unit,
		DaysBetween: sc.DaysBetween, LeadDays: sc.ForecastDays,
		MinStockLevel: minStock, MinStockDisplay: fmtQty(minStock, unit),
		AvgDailyConsumption: roundMeasure(avg), AvgDailyDisplay: fmtQty(avg, unit),
		CurrentQty: normalizeQty(cur, unit), CurrentDisplay: fmtQty(cur, unit),
		PlanQty: roundMeasure(plan), PlanDisplay: fmtQty(plan, unit),
		OrderQty: need, OrderDisplay: fmtQty(need, unit),
		RecommendedMin: recMin, RecommendedMinDisp: fmtQty(recMin, unit),
		DaysCovered: daysCovered, Level: level, Enabled: sc.Enabled,
	}
}

// SupplySchedule — полный график поставок со всеми расчётами.
func (s *Store) SupplySchedule() []scheduleRow {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	out := []scheduleRow{}
	ids := make([]int, 0, len(s.schedules))
	for pid := range s.schedules {
		ids = append(ids, pid)
	}
	sort.Ints(ids)
	for _, pid := range ids {
		out = append(out, s.calcScheduleRow(pid, s.schedules[pid], now))
	}
	sort.Slice(out, func(i, j int) bool {
		ri, rj := levelRank(out[i].Level), levelRank(out[j].Level)
		if ri != rj {
			return ri < rj
		}
		return out[i].DaysCovered < out[j].DaysCovered
	})
	return out
}

func levelRank(level string) int {
	switch level {
	case "critical":
		return 0
	case "warning":
		return 1
	case "notice":
		return 2
	}
	return 3
}

// GenerateAutoSupply — формирует позиции автозаказа по всем включённым графикам.
// Учитывает периодичность: если с последней поставки продукта прошло меньше
// дней, чем заданный период, продукт в заказ не попадает.
func (s *Store) GenerateAutoSupply() ([]SupplyItem, []scheduleRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	ids := make([]int, 0, len(s.schedules))
	for pid := range s.schedules {
		ids = append(ids, pid)
	}
	sort.Ints(ids)

	items := []SupplyItem{}
	rows := []scheduleRow{}
	skipped := []scheduleRow{}
	for _, pid := range ids {
		sc := s.schedules[pid]
		if !sc.Enabled {
			continue
		}
		row := s.calcScheduleRow(pid, sc, now)
		if row.OrderQty <= 0 {
			skipped = append(skipped, row)
			continue
		}
		// Периодичность: не чаще, чем раз в DaysBetween дней по этому продукту.
		if !s.supplyAllowed(pid, sc.DaysBetween, now) {
			row.Level = "waiting"
			skipped = append(skipped, row)
			continue
		}
		items = append(items, SupplyItem{ProductID: pid, Qty: row.OrderQty, ShelfDays: s.defaultShelfDays(pid)})
		rows = append(rows, row)
	}
	if len(items) == 0 {
		return nil, skipped, fmt.Errorf("автозаказ не требуется: остатки в пределах нормы")
	}
	s.log("auto_supply", fmt.Sprintf("Автозаказ рассчитан: %d позиций", len(items)))
	s.broadcast("auto_supply", fmt.Sprintf("generated:%d", len(items)))
	return items, rows, nil
}

// supplyAllowed — можно ли заказывать продукт (с последней поставки прошло
// достаточно дней).
func (s *Store) supplyAllowed(productID, daysBetween int, now time.Time) bool {
	last := time.Time{}
	for _, sup := range s.supplies {
		for _, it := range sup.Items {
			if it.ProductID == productID && sup.Date.After(last) {
				last = sup.Date
			}
		}
	}
	if last.IsZero() {
		return true
	}
	return !now.Before(last.Add(time.Duration(daysBetween) * 24 * time.Hour))
}

// defaultShelfDays — срок годности по умолчанию, взятый из истории продукта.
func (s *Store) defaultShelfDays(productID int) int {
	best := 0
	for _, b := range s.batches {
		if b.ProductID != productID || b.InitialQty <= 0 {
			continue
		}
		d := int(b.ExpiryDate.Sub(b.ArrivalDate).Hours() / 24)
		if d > best {
			best = d
		}
	}
	if best <= 0 {
		return 7
	}
	return best
}

// CreateAutoSupply — рассчитывает и сразу оформляет поставку по автозаказу.
func (s *Store) CreateAutoSupply(who string) (Supply, error) {
	items, rows, err := s.GenerateAutoSupply()
	if err != nil {
		return Supply{}, err
	}
	sup, err := s.CreateSupply("", items)
	if err != nil {
		return Supply{}, err
	}
	s.mu.Lock()
	s.log("auto_supply", fmt.Sprintf("%s: автозаказ %s оформлен (%d позиций)",
		who, sup.Number, len(rows)))
	s.mu.Unlock()
	return sup, nil
}

// recordConsumption — фиксирует факт расхода продукта за день (аналитика).
// Вызывать только из-под захваченного s.mu.
func (s *Store) recordConsumption(need map[int]float64) {
	if s.consume == nil {
		s.consume = map[int]map[string]float64{}
	}
	dayKey := time.Now().Format("2006-01-02")
	for pid, q := range need {
		if s.consume[pid] == nil {
			s.consume[pid] = map[string]float64{}
		}
		s.consume[pid][dayKey] += q
	}
}

func (s *Store) StopList() []StopItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	avail := s.availability()
	out := []StopItem{}
	for _, d := range s.dishes {
		// Ручная блокировка приоритетнее: показываем её даже при полном складе.
		if reason, manual := s.manualStops[d.ID]; manual {
			out = append(out, StopItem{
				DishID: d.ID, Dish: d.Name, Manual: true, ManualReason: reason,
				MaxPortions: avail[d.ID],
			})
			continue
		}
		if avail[d.ID] >= 1 {
			continue
		}
		var missing []string
		for _, ing := range d.Ingredients {
			total := 0.0
			for _, b := range s.batches {
				if b.ProductID == ing.ProductID {
					total += b.Qty
				}
			}
			if total < ing.Qty {
				pname := ""
				unit := ""
				for _, p := range s.products {
					if p.ID == ing.ProductID {
						pname, unit = p.Name, p.Unit
					}
				}
				missing = append(missing, fmt.Sprintf("%s: есть %.2f %s, нужно %.3f", pname, total, unit, ing.Qty))
			}
		}
		out = append(out, StopItem{
			DishID: d.ID, Dish: d.Name, Missing: missing,
			MaxPortions: avail[d.ID],
		})
	}
	return out
}

func (s *Store) HotOffers() []HotItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	// Снятые вручную блюда не должны попадать в горячие предложения.
	avail := s.applyManualStops(s.availability())
	stock := map[int]float64{}
	for _, b := range s.batches {
		if b.Qty > 0 {
			stock[b.ProductID] += b.Qty
		}
	}
	dishesUsing := map[int]int{}
	for _, d := range s.dishes {
		seen := map[int]bool{}
		for _, i := range d.Ingredients {
			if !seen[i.ProductID] {
				seen[i.ProductID] = true
				dishesUsing[i.ProductID]++
			}
		}
	}
	stop := map[int]bool{}
	for _, d := range s.dishes {
		if avail[d.ID] < 1 {
			stop[d.ID] = true
		}
	}
	out := []HotItem{}
	for _, d := range s.dishes {
		if stop[d.ID] {
			continue
		}
		score := 0.0
		minDays := math.Inf(1)
		var reasons []string
		for _, ing := range d.Ingredients {
			var best *Batch
			for i := range s.batches {
				b := &s.batches[i]
				if b.ProductID != ing.ProductID || b.Qty <= 0 {
					continue
				}
				if best == nil || b.ExpiryDate.Before(best.ExpiryDate) {
					best = b
				}
			}
			if best == nil {
				continue
			}
			dl := daysLeft(*best, now)
			if dl < minDays {
				minDays = dl
			}
			if dl <= 0 {
				continue
			}
			base := 100.0 / (dl + 0.5)
			w := ing.Qty / (stock[ing.ProductID] + ing.Qty)
			multi := 1 + 0.25*float64(dishesUsing[ing.ProductID]-1)
			part := base * (0.5 + w) * multi
			score += part
			if dl <= 2 {
				reasons = append(reasons, fmt.Sprintf("%s истекает через %.1f дн. (остаток %s)",
					best.Product, dl, fmtQty(stock[ing.ProductID], best.Unit)))
			} else if dl <= 5 {
				reasons = append(reasons, fmt.Sprintf("%s: осталось %.1f дн.", best.Product, dl))
			}
		}
		if math.IsInf(minDays, 1) {
			minDays = 0
		}
		if score <= 0 {
			continue
		}
		if len(reasons) == 0 {
			reasons = append(reasons, "Блюдо доступно к продаже")
		}
		out = append(out, HotItem{
			DishID: d.ID, Dish: d.Name, Price: d.Price,
			Score:    math.Round(score*10) / 10,
			Reasons:  reasons,
			MinDays:  math.Round(minDays*10) / 10,
			Portions: avail[d.ID],
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Score > out[j].Score
	})
	if len(out) > 10 {
		out = out[:10]
	}
	return out
}

func (s *Store) Expiring(withinDays float64) []map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	out := []map[string]interface{}{}
	for _, b := range s.batches {
		if b.Qty <= 0 {
			continue
		}
		dl := daysLeft(b, now)
		if dl > withinDays {
			continue
		}
		level := "warning"
		if dl <= 0 {
			level = "expired"
		} else if dl <= 1 {
			level = "critical"
		}
		out = append(out, map[string]interface{}{
			"batch_id": b.ID, "batch_number": b.BatchNumber,
			"product": b.Product, "qty": b.Qty, "unit": b.Unit,
			"expiry":    b.ExpiryDate.Format("02.01.2006"),
			"days_left": math.Round(dl*10) / 10, "level": level,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i]["days_left"].(float64) < out[j]["days_left"].(float64)
	})
	return out
}

func (s *Store) CreateSupply(number string, items []SupplyItem) (Supply, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(items) == 0 {
		return Supply{}, fmt.Errorf("пустая поставка")
	}
	now := time.Now()
	if number == "" {
		number = fmt.Sprintf("SUP-%04d", s.seq["supply"]+1)
	}
	sup := Supply{ID: s.nextID("supply"), Number: number, Date: now}
	for _, it := range items {
		if it.Qty <= 0 {
			return Supply{}, fmt.Errorf("количество должно быть > 0")
		}
		if it.ShelfDays <= 0 {
			it.ShelfDays = 7
		}
		pname, unit, pcost := "", "шт", 0.0
		found := false
		for _, p := range s.products {
			if p.ID == it.ProductID {
				pname, unit, pcost, found = p.Name, p.Unit, p.Cost, true
			}
		}
		if !found {
			return Supply{}, fmt.Errorf("продукт %d не найден", it.ProductID)
		}
		it.Qty = normalizeQty(it.Qty, unit)
		// Цена партии: либо указана в поставке, либо берётся из справочника.
		cost := it.Cost
		if cost <= 0 {
			cost = pcost
		}
		b := Batch{
			ID: s.nextID("batch"), ProductID: it.ProductID, Product: pname, Unit: unit,
			SupplyID: sup.ID, BatchNumber: fmt.Sprintf("%s-B%d", sup.Number, s.seq["batch"]),
			Qty: it.Qty, InitialQty: it.Qty, ArrivalDate: now,
			ExpiryDate: now.Add(time.Duration(it.ShelfDays) * 24 * time.Hour),
			Cost:       roundMeasure(cost),
		}
		s.batches = append(s.batches, b)
		sup.Batches = append(sup.Batches, b)
		// Раз поставка пришла — обновляем справочник, если цена оказалась другой.
		if it.Cost > 0 && math.Abs(it.Cost-pcost) > 1e-9 {
			for i := range s.products {
				if s.products[i].ID == it.ProductID {
					s.products[i].Cost = roundMeasure(it.Cost)
				}
			}
		}
	}
	s.supplies = append(s.supplies, sup)
	s.log("supply", fmt.Sprintf("Поставка %s: партий %d", sup.Number, len(sup.Batches)))
	s.broadcast("supply", sup.Number)
	return sup, nil
}

func (s *Store) CreateOrder(number string, items []OrderItem) (Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(items) == 0 {
		return Order{}, fmt.Errorf("пустой заказ")
	}
	need := map[int]float64{}
	punit := map[int]string{}
	for _, p := range s.products {
		punit[p.ID] = p.Unit
	}
	// Считаем потребность + добавляем ингредиенты из кастомизаций.
	for _, it := range items {
		if it.Qty <= 0 {
			return Order{}, fmt.Errorf("некорректное количество порций")
		}
		var dish *Dish
		for i := range s.dishes {
			if s.dishes[i].ID == it.DishID {
				dish = &s.dishes[i]
			}
		}
		if dish == nil {
			return Order{}, fmt.Errorf("блюдо %d не найдено", it.DishID)
		}
		// Снятое вручную блюдо нельзя заказать.
		if reason, blocked := s.manualStops[it.DishID]; blocked {
			return Order{}, fmt.Errorf("блюдо «%s» снято с продажи: %s", dish.Name, reason)
		}
		for _, ing := range dish.Ingredients {
			need[ing.ProductID] += ing.Qty * float64(it.Qty)
		}
		// Добавляем кастомизации к блюду — тоже списываем продукты.
		for _, c := range it.Customizations {
			if c.Action == "add" && c.Qty > 0 {
				q := normalizeQty(c.Qty, punit[c.ProductID])
				need[c.ProductID] += q
			}
		}
	}
	// Штучные продукты нельзя израсходовать частично.
	for pid, q := range need {
		need[pid] = normalizeQty(q, punit[pid])
	}
	var lack []string
	for pid, q := range need {
		total := 0.0
		for _, b := range s.batches {
			if b.ProductID == pid {
				total += b.Qty
			}
		}
		if total+1e-9 < q {
			name := ""
			unit := ""
			for _, p := range s.products {
				if p.ID == pid {
					name, unit = p.Name, p.Unit
				}
			}
			lack = append(lack, fmt.Sprintf("%s: нужно %s, на складе %s",
				name, fmtQty(q, unit), fmtQty(total, unit)))
		}
	}
	if len(lack) > 0 {
		sort.Strings(lack)
		s.log("order_reject", "Отказ в заказе: "+strings.Join(lack, "; "))
		s.broadcast("stock", "reject")
		return Order{}, fmt.Errorf("недостаточно продуктов: %s", strings.Join(lack, "; "))
	}
	// Списываем продукты по FEFO.
	for pid, q := range need {
		rem := q
		idx := []int{}
		for i := range s.batches {
			if s.batches[i].ProductID == pid && s.batches[i].Qty > 0 {
				idx = append(idx, i)
			}
		}
		sort.Slice(idx, func(a, b int) bool {
			return s.batches[idx[a]].ExpiryDate.Before(s.batches[idx[b]].ExpiryDate)
		})
		for _, i := range idx {
			if rem <= 1e-9 {
				break
			}
			take := math.Min(rem, s.batches[i].Qty)
			s.batches[i].Qty -= take
			s.batches[i].Qty = normalizeQty(s.batches[i].Qty, s.batches[i].Unit)
			rem -= take
		}
	}
	if number == "" {
		number = fmt.Sprintf("ORD-%04d", s.seq["order"]+1)
	}
	o := Order{
		ID:        s.nextID("order"),
		Number:    number,
		Items:     items,
		CreatedAt: time.Now(),
		Status:    "new",
	}
	o.Total = s.calcOrderTotal(o)
	if o.Total > 0 {
		o.Discount, o.DiscountReason = s.promotionDiscount(o.Total, o.CreatedAt)
	}
	s.orders = append(s.orders, o)

	// Записываем расход по каждому продукту для аналитики автозаказа.
	s.recordConsumption(need)

	total := 0
	for _, i := range items {
		total += i.Qty
	}
	s.log("order", fmt.Sprintf("Заказ %s: %d порций, чек: %.0f руб, списание по FEFO",
		o.Number, total, o.Total))
	s.broadcast("stock", o.Number)
	return o, nil
}

// calcOrderTotal — пересчёт итоговой стоимости заказа (блюда + кастомизации − скидки).
func (s *Store) calcOrderTotal(o Order) float64 {
	total := 0.0
	for _, it := range o.Items {
		for _, d := range s.dishes {
			if d.ID == it.DishID {
				total += d.Price * float64(it.Qty)
				break
			}
		}
		for _, c := range it.Customizations {
			total += c.Price
		}
	}
	// Глобальные кастомизации.
	for _, c := range o.Customizations {
		if c.Action == "add" {
			total += c.Price
		} else {
			total -= c.Price
		}
	}
	if total < 0 {
		total = 0
	}
	return math.Round(total*100) / 100
}

// CalcOrderDiscount — расчёт скидки для заказа.
func (s *Store) CalcOrderDiscount(orderID int) (float64, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var total float64
	for _, o := range s.orders {
		if o.ID != orderID {
			continue
		}
		total = s.calcOrderTotal(o)
		break
	}
	if total <= 0 {
		return 0, ""
	}
	return s.promotionDiscount(total, time.Now())
}

// recalcOrder — пересчитать итог и скидку заказа и обновить в хранилище.
func (s *Store) recalcOrder(orderID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.orders {
		if s.orders[i].ID != orderID {
			continue
		}
		o := &s.orders[i]
		o.Total = s.calcOrderTotal(*o)
		if o.Total > 0 {
			o.Discount, o.DiscountReason = s.promotionDiscount(o.Total, o.CreatedAt)
		} else {
			o.Discount, o.DiscountReason = 0, ""
		}
		s.log("order_recalc", fmt.Sprintf("Пересчёт заказа %s: %.0f руб (скидка: %.0f%%)",
			o.Number, o.Total, o.Discount))
		return
	}
}

// UpdateOrderStatus — смена статуса заказа (new → cooking → served → done/cancelled).
func (s *Store) UpdateOrderStatus(orderID int, status string, who string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.orders {
		if s.orders[i].ID == orderID {
			oldStatus := s.orders[i].Status
			s.orders[i].Status = status
			s.log("order_status", fmt.Sprintf("%s: заказ %s статус %s → %s",
				who, s.orders[i].Number, oldStatus, status))
			s.broadcast("order", fmt.Sprintf("status:%d", orderID))
			return nil
		}
	}
	return fmt.Errorf("заказ %d не найден", orderID)
}

// CustomizeOrderDish — добавить/убрать ингредиент к конкретному блюду в заказе.
// Вызывается на этапе готовки: гость указывает на ингредиент.
func (s *Store) CustomizeOrderDish(orderID int, dishID int, productID int, action string, qty float64, who string) (OrderItemCustomization, float64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Находим заказ.
	var order *Order
	for i := range s.orders {
		if s.orders[i].ID == orderID {
			order = &s.orders[i]
			break
		}
	}
	if order == nil {
		return OrderItemCustomization{}, 0, fmt.Errorf("заказ %d не найден", orderID)
	}
	if order.Status != "new" && order.Status != "cooking" {
		return OrderItemCustomization{}, 0, fmt.Errorf("заказ %s в статусе «%s» — нельзя менять (нужен new или cooking)", order.Number, order.Status)
	}
	// Проверяем продукт.
	var product *Product
	for _, p := range s.products {
		if p.ID == productID {
			product = &p
			break
		}
	}
	if product == nil {
		return OrderItemCustomization{}, 0, fmt.Errorf("продукт %d не найден", productID)
	}
	if qty <= 0 {
		return OrderItemCustomization{}, 0, fmt.Errorf("количество должно быть > 0")
	}
	qty = normalizeQty(qty, product.Unit)
	// Для "add" проверяем склад, для "remove" просто пересчитываем.
	if action == "add" {
		totalOnStock := 0.0
		for _, b := range s.batches {
			if b.ProductID == productID && b.Qty > 0 {
				totalOnStock += b.Qty
			}
		}
		if totalOnStock < qty-1e-9 {
			return OrderItemCustomization{}, 0, fmt.Errorf("на складе недостаточно %s (есть: %s, нужно: %s)",
				product.Name, fmtQty(totalOnStock, product.Unit), fmtQty(qty, product.Unit))
		}
		// Списываем продукт с FEFO.
		rem := qty
		for i := range s.batches {
			b := &s.batches[i]
			if b.ProductID == productID && b.Qty > 0 && rem > 1e-9 {
				take := math.Min(rem, b.Qty)
				b.Qty = normalizeQty(b.Qty-take, b.Unit)
				rem -= take
			}
		}
	} else if action == "remove" {
		// Возвращаем продукт на склад (в самую раннюю партию).
		idx := -1
		for i := range s.batches {
			if s.batches[i].ProductID == productID {
				if idx < 0 || s.batches[i].ExpiryDate.Before(s.batches[idx].ExpiryDate) {
					idx = i
				}
			}
		}
		if idx < 0 {
			sid := s.nextID("supply")
			b := Batch{
				ID: s.nextID("batch"), ProductID: productID, Product: product.Name, Unit: product.Unit,
				SupplyID: sid, BatchNumber: fmt.Sprintf("BUST-%04d", s.seq["batch"]),
				Qty: qty, InitialQty: qty, ArrivalDate: time.Now(),
				ExpiryDate: time.Now().Add(7 * 24 * time.Hour),
			}
			s.batches = append(s.batches, b)
		} else {
			s.batches[idx].Qty = normalizeQty(s.batches[idx].Qty+qty, s.batches[idx].Unit)
		}
	}
	// Цена: берём среднюю стоимость из партий.
	pricePerUnit := s.pricePerUnit(productID)
	itemPrice := pricePerUnit * qty
	if action == "remove" {
		itemPrice = -itemPrice // минус — возврат денег.
	}
	// Находим блюдо в заказе и добавляем кастомизацию.
	for j := range order.Items {
		if order.Items[j].DishID == dishID {
			cust := OrderItemCustomization{
				ProductID:   productID,
				ProductName: product.Name,
				Action:      action,
				Qty:         qty,
				Unit:        product.Unit,
				Display:     fmtQty(qty, product.Unit),
				Price:       itemPrice,
			}
			order.Items[j].Customizations = append(order.Items[j].Customizations, cust)
			s.log("customize_dish", fmt.Sprintf("%s: заказ %s, блюдо %d, %s %s «%s» (%s)",
				who, order.Number, dishID, action, product.Name, fmtQty(qty, product.Unit),
				fmtPriceDelta(itemPrice)))
			s.broadcast("order", fmt.Sprintf("customize_dish:%d", orderID))
			// Пересчитываем итог заказа.
			order.Total = s.calcOrderTotal(*order)
			if order.Total > 0 {
				order.Discount, order.DiscountReason = s.promotionDiscount(order.Total, order.CreatedAt)
			} else {
				order.Discount, order.DiscountReason = 0, ""
			}
			return cust, itemPrice, nil
		}
	}
	// Блюдо не найдено в заказе — добавляем как новый OrderItem.
	if action == "add" {
		cust := OrderItemCustomization{
			ProductID:   productID,
			ProductName: product.Name,
			Action:      action,
			Qty:         qty,
			Unit:        product.Unit,
			Display:     fmtQty(qty, product.Unit),
			Price:       itemPrice,
		}
		newItem := OrderItem{DishID: dishID, Qty: 1, Customizations: []OrderItemCustomization{cust}}
		order.Items = append(order.Items, newItem)
		s.log("customize_dish_new", fmt.Sprintf("%s: заказ %s, новое блюдо %d с %s «%s»",
			who, order.Number, dishID, action, product.Name))
		order.Total = s.calcOrderTotal(*order)
		return cust, itemPrice, nil
	}
	return OrderItemCustomization{}, 0, fmt.Errorf("блюдо %d не найдено в заказе %d", dishID, orderID)
}

// pricePerUnit — розничная цена единицы продукта (кг/л/шт) для пересчёта чека
// при добавлении ингредиента. Берётся из себестоимости с наценкой; если
// себестоимость не заведена — оценка по средней себестоимости партий.
func (s *Store) pricePerUnit(productID int) float64 {
	for _, p := range s.products {
		if p.ID == productID {
			if p.Cost > 0 {
				return math.Round(p.Cost*customMarkup*100) / 100
			}
			break
		}
	}
	totalCost, totalQty := 0.0, 0.0
	for _, b := range s.batches {
		if b.ProductID == productID && b.InitialQty > 0 {
			totalCost += b.Cost * b.InitialQty
			totalQty += b.InitialQty
		}
	}
	if totalQty > 0 && totalCost > 0 {
		return math.Round(totalCost/totalQty*customMarkup*100) / 100
	}
	return 0
}

// customMarkup — наценка на ингредиенты при кастомизации заказа (×2 от себестоимости).
const customMarkup = 2.0

// fmtPriceDelta — форматирование дельты чека.
func fmtPriceDelta(price float64) string {
	if price >= 0 {
		return fmt.Sprintf("+(%.0f руб)", price)
	}
	return fmt.Sprintf("−(%.0f руб)", math.Abs(price))
}

// GetOrder — получить заказ по ID.
func (s *Store) GetOrder(orderID int) (Order, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, o := range s.orders {
		if o.ID == orderID {
			return o, nil
		}
	}
	return Order{}, fmt.Errorf("заказ %d не найден", orderID)
}

// CancelOrder возвращает списанные по заказу продукты на склад (доступно админу).

func (s *Store) AutoWriteoff() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	n := 0
	for i := range s.batches {
		b := &s.batches[i]
		if b.Qty > 0 && b.ExpiryDate.Before(now) {
			s.writeoffs = append(s.writeoffs, Writeoff{
				ID: s.nextID("writeoff"), BatchID: b.ID, Product: b.Product,
				BatchNumber: b.BatchNumber, Qty: b.Qty,
				Unit: b.Unit, Display: fmtQty(b.Qty, b.Unit),
				Reason: "истёк срок годности", CreatedAt: now,
			})
			s.log("writeoff", fmt.Sprintf("Списание: %s %s — %s",
				b.Product, b.BatchNumber, fmtQty(b.Qty, b.Unit)))
			b.Qty = 0
			n++
		}
	}
	if n > 0 {
		s.broadcast("stock", "writeoff")
	}
	return n
}

func (s *Store) Predictions() []map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	avail := s.availability()
	out := []map[string]interface{}{}
	for _, d := range s.dishes {
		if avail[d.ID] >= 1 {
			continue
		}
		for _, ing := range d.Ingredients {
			total := 0.0
			for _, b := range s.batches {
				if b.ProductID == ing.ProductID {
					total += b.Qty
				}
			}
			if total >= ing.Qty {
				continue
			}
			name, unit := "", ""
			for _, p := range s.products {
				if p.ID == ing.ProductID {
					name, unit = p.Name, p.Unit
				}
			}
			out = append(out, map[string]interface{}{
				"dish": d.Name, "product": name, "unit": unit,
				"need": ing.Qty, "have": math.Round(total*1000) / 1000,
				"deficit": math.Round((ing.Qty-total)*1000) / 1000,
				"action": fmt.Sprintf("Заказать минимум %.2f %s",
					ing.Qty-total+ing.Qty*0.5, unit),
			})
		}
	}
	return out
}

func (s *Store) SuggestSupply() []map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	type agg struct {
		name string
		unit string
		have float64
		days float64
	}
	m := map[int]*agg{}
	var ids []int
	for _, b := range s.batches {
		if b.Qty <= 0 {
			continue
		}
		a, ok := m[b.ProductID]
		if !ok {
			a = &agg{name: b.Product, unit: b.Unit, days: 9999}
			m[b.ProductID] = a
			ids = append(ids, b.ProductID)
		}
		a.have += b.Qty
		dl := daysLeft(b, now)
		if dl < a.days {
			a.days = dl
		}
	}
	for _, p := range s.products {
		if _, ok := m[p.ID]; !ok {
			used := false
			for _, d := range s.dishes {
				for _, i := range d.Ingredients {
					if i.ProductID == p.ID {
						used = true
					}
				}
			}
			if used {
				m[p.ID] = &agg{name: p.Name, unit: p.Unit, have: 0, days: -1}
				ids = append(ids, p.ID)
			}
		}
	}
	out := []map[string]interface{}{}
	for _, id := range ids {
		a := m[id]
		if a.days > 7 && a.have > 3 {
			continue
		}
		level := "warning"
		if a.have <= 0 || a.days <= 0 {
			level = "expired"
		} else if a.days <= 2 {
			level = "critical"
		}
		out = append(out, map[string]interface{}{
			"product": a.name, "unit": a.unit,
			"have": a.have, "have_display": fmtQty(a.have, a.unit),
			"days_left": math.Round(a.days*10) / 10,
			"level":     level,
			"action": fmt.Sprintf("Заказать ~%s",
				fmtQty(math.Max(5-a.have, 2), a.unit)),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i]["days_left"].(float64) < out[j]["days_left"].(float64)
	})
	return out
}

func (s *Store) Dashboard() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	crit, warn, exp := 0, 0, 0
	var stockKg, stockL, stockPcs float64
	for _, b := range s.batches {
		if b.Qty <= 0 {
			continue
		}
		dl := daysLeft(b, now)
		switch {
		case dl <= 0:
			exp++
		case dl <= 1:
			crit++
		case dl <= 3:
			warn++
		}
		switch unitKind(b.Unit) {
		case kindWeight:
			stockKg += b.Qty
		case kindVolume:
			stockL += b.Qty
		default:
			stockPcs += b.Qty
		}
	}
	avail := s.availability()
	dashboardAvail := s.applyManualStops(avail)
	available := 0
	for _, v := range dashboardAvail {
		if v >= 1 {
			available++
		}
	}
	var lostKg, lostL, lostPcs float64
	for _, w := range s.writeoffs {
		switch unitKind(w.Unit) {
		case kindWeight:
			lostKg += w.Qty
		case kindVolume:
			lostL += w.Qty
		default:
			lostPcs += w.Qty
		}
	}
	return map[string]interface{}{
		"products":         len(s.products),
		"dishes":           len(s.dishes),
		"dishes_available": available,
		"dishes_stopped":   len(s.dishes) - available,
		"batches":          len(s.batches),
		"critical":         crit,
		"warning":          warn,
		"expired":          exp,
		"supplies":         len(s.supplies),
		"orders":           len(s.orders),
		"writeoffs":        len(s.writeoffs),
		"stock_weight_kg":  roundMeasure(stockKg),
		"stock_volume_l":   roundMeasure(stockL),
		"stock_count_pcs":  roundCount(stockPcs),
		"stock_display": fmt.Sprintf("%s • %s • %s",
			fmtQty(stockKg, "кг"), fmtQty(stockL, "л"), fmtQty(stockPcs, "шт")),
		"writeoff_weight_kg": roundMeasure(lostKg),
		"writeoff_volume_l":  roundMeasure(lostL),
		"writeoff_count_pcs": roundCount(lostPcs),
		"writeoff_display": fmt.Sprintf("%s • %s • %s",
			fmtQty(lostKg, "кг"), fmtQty(lostL, "л"), fmtQty(lostPcs, "шт")),
		"server_time": now.Format("15:04:05"),
	}
}

func (s *Store) broadcast(typ, data string) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	msg := fmt.Sprintf("event: %s\ndata: %s\n\n", typ, data)
	for ch := range s.subs {
		select {
		case ch <- msg:
		default:
		}
	}
}

func (s *Store) subscribe() chan string {
	ch := make(chan string, 16)
	s.subMu.Lock()
	s.subs[ch] = true
	s.subMu.Unlock()
	return ch
}

func (s *Store) unsubscribe(ch chan string) {
	s.subMu.Lock()
	delete(s.subs, ch)
	s.subMu.Unlock()
	close(ch)
}

// ---- ХЕЛПЕРЫ ДЛЯ ДЕМО-РЕЖИМА ----

// dishIDs — список идентификаторов всех блюд.
func (s *Store) dishIDs() []int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]int, 0, len(s.dishes))
	for _, d := range s.dishes {
		out = append(out, d.ID)
	}
	return out
}

// productIDs — список идентификаторов всех продуктов.
func (s *Store) productIDs() []int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]int, 0, len(s.products))
	for _, p := range s.products {
		out = append(out, p.ID)
	}
	return out
}

// unitOf — единица измерения продукта по идентификатору.
func (s *Store) unitOf(productID int) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.products {
		if p.ID == productID {
			return p.Unit
		}
	}
	return "шт"
}

// liveBatchSample — непросроченные партии с остатком, из которых можно списывать.
func (s *Store) liveBatchSample() []Batch {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	out := []Batch{}
	for _, b := range s.batches {
		if b.Qty > 0 && b.ExpiryDate.After(now) {
			out = append(out, b)
		}
	}
	return out
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

var store = NewStore()

func main() {
	flag.Parse()
	store.seed()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/api/dashboard", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, store.Dashboard())
	})

	mux.HandleFunc("/api/products", func(w http.ResponseWriter, r *http.Request) {
		store.mu.RLock()
		defer store.mu.RUnlock()
		writeJSON(w, 200, store.products)
	})

	mux.HandleFunc("/api/dishes", func(w http.ResponseWriter, r *http.Request) {
		store.mu.RLock()
		defer store.mu.RUnlock()
		out := []map[string]interface{}{}
		for _, d := range store.dishes {
			ings := []map[string]interface{}{}
			for _, i := range d.Ingredients {
				name, unit := "", ""
				for _, p := range store.products {
					if p.ID == i.ProductID {
						name, unit = p.Name, p.Unit
					}
				}
				ings = append(ings, map[string]interface{}{
					"product_id": i.ProductID,
					"product":    name, "unit": unit, "qty": i.Qty,
				})
			}
			out = append(out, map[string]interface{}{
				"id": d.ID, "name": d.Name, "price": d.Price,
				"ingredients": ings,
			})
		}
		writeJSON(w, 200, out)
	})

	mux.HandleFunc("/api/stock", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, store.Stock())
	})

	mux.HandleFunc("/api/expiring", func(w http.ResponseWriter, r *http.Request) {
		d, _ := strconv.ParseFloat(r.URL.Query().Get("days"), 64)
		if d <= 0 {
			d = 7
		}
		writeJSON(w, 200, store.Expiring(d))
	})

	mux.HandleFunc("/api/stoplist", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, store.StopList())
	})

	mux.HandleFunc("/api/hot", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, store.HotOffers())
	})

	mux.HandleFunc("/api/suggestions", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, store.SuggestSupply())
	})

	mux.HandleFunc("/api/predictions", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, store.Predictions())
	})

	mux.HandleFunc("/api/supplies", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			store.mu.RLock()
			defer store.mu.RUnlock()
			out := make([]Supply, len(store.supplies))
			for i, s := range store.supplies {
				out[i] = s
				out[i].Batches = []Batch{}
				for _, b := range store.batches {
					if b.SupplyID == s.ID {
						out[i].Batches = append(out[i].Batches, b)
					}
				}
			}
			sort.Slice(out, func(i, j int) bool {
				return out[i].ID > out[j].ID
			})
			writeJSON(w, 200, out)
		case http.MethodPost:
			var body struct {
				Number string       `json:"number"`
				Items  []SupplyItem `json:"items"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeErr(w, 400, "некорректный JSON: "+err.Error())
				return
			}
			sup, err := store.CreateSupply(body.Number, body.Items)
			if err != nil {
				writeErr(w, 400, err.Error())
				return
			}
			writeJSON(w, 201, sup)
		default:
			writeErr(w, 405, "method not allowed")
		}
	})

	mux.HandleFunc("/api/orders", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			store.mu.RLock()
			defer store.mu.RUnlock()
			out := make([]Order, len(store.orders))
			copy(out, store.orders)
			sort.Slice(out, func(i, j int) bool {
				return out[i].ID > out[j].ID
			})
			writeJSON(w, 200, out)
		case http.MethodPost:
			var body struct {
				Number string      `json:"number"`
				Items  []OrderItem `json:"items"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeErr(w, 400, "некорректный JSON: "+err.Error())
				return
			}
			o, err := store.CreateOrder(body.Number, body.Items)
			if err != nil {
				writeErr(w, 409, err.Error())
				return
			}
			writeJSON(w, 201, o)
		default:
			writeErr(w, 405, "method not allowed")
		}
	})

	mux.HandleFunc("/api/writeoffs", func(w http.ResponseWriter, r *http.Request) {
		store.mu.RLock()
		defer store.mu.RUnlock()
		out := make([]Writeoff, len(store.writeoffs))
		copy(out, store.writeoffs)
		sort.Slice(out, func(i, j int) bool {
			return out[i].ID > out[j].ID
		})
		writeJSON(w, 200, out)
	})

	mux.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
		store.mu.RLock()
		defer store.mu.RUnlock()
		out := make([]Event, len(store.events))
		copy(out, store.events)
		sort.Slice(out, func(i, j int) bool {
			return out[i].ID > out[j].ID
		})
		if len(out) > 60 {
			out = out[:60]
		}
		writeJSON(w, 200, out)
	})

	mux.HandleFunc("/api/writeoff", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, 405, "method not allowed")
			return
		}
		n := store.AutoWriteoff()
		writeJSON(w, 200, map[string]int{"written_off": n})
	})

	mux.HandleFunc("/api/batches", func(w http.ResponseWriter, r *http.Request) {
		store.mu.RLock()
		defer store.mu.RUnlock()
		now := time.Now()
		out := []map[string]interface{}{}
		for _, b := range store.batches {
			if b.Qty <= 0 {
				continue
			}
			dl := daysLeft(b, now)
			level := "ok"
			switch {
			case dl <= 0:
				level = "expired"
			case dl <= 1:
				level = "critical"
			case dl <= 3:
				level = "warning"
			}
			out = append(out, map[string]interface{}{
				"batch_id": b.ID, "batch_number": b.BatchNumber,
				"product": b.Product, "qty": b.Qty,
				"initial_qty": b.InitialQty, "unit": b.Unit,
				"arrival":   b.ArrivalDate.Format("02.01.2006"),
				"expiry":    b.ExpiryDate.Format("02.01.2006"),
				"days_left": math.Round(dl*10) / 10, "level": level,
			})
		}
		sort.Slice(out, func(i, j int) bool {
			return out[i]["days_left"].(float64) < out[j]["days_left"].(float64)
		})
		writeJSON(w, 200, out)
	})

	mux.HandleFunc("/api/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		fl, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", 500)
			return
		}
		ch := store.subscribe()
		defer store.unsubscribe(ch)
		fmt.Fprint(w, "event: hello\ndata: connected\n\n")
		fl.Flush()
		tick := time.NewTicker(15 * time.Second)
		defer tick.Stop()
		for {
			select {
			case msg := <-ch:
				fmt.Fprint(w, msg)
				fl.Flush()
			case <-tick.C:
				fmt.Fprint(w, ": ping\n\n")
				fl.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})

	// ========================
	// НОВЫЕ API: продукты, блюда, акции, автозаказ, кастомизация
	// ========================

	// --- Продукты: CRUD ---
	mux.HandleFunc("/api/products/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		requireAuth(func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				ID   int     `json:"id"`
				Name string  `json:"name"`
				Unit string  `json:"unit"`
				Cost float64 `json:"cost"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeErr(w, http.StatusBadRequest, "некорректный JSON")
				return
			}
			// id > 0 — правим себестоимость существующего продукта.
			if body.ID > 0 {
				p, err := store.SetProductCost(body.ID, body.Cost, sessionFromRequest(r).Login)
				if err != nil {
					writeErr(w, http.StatusBadRequest, err.Error())
					return
				}
				writeJSON(w, http.StatusOK, p)
				return
			}
			p, err := store.AddProduct(body.Name, body.Unit, body.Cost)
			if err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, p)
		})(w, r)
	})

	mux.HandleFunc("/api/products/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		requireAuth(func(w http.ResponseWriter, r *http.Request) {
			sess := sessionFromRequest(r)
			var body struct {
				ProductID int `json:"product_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeErr(w, http.StatusBadRequest, "некорректный JSON")
				return
			}
			if err := store.DeleteProduct(body.ProductID, sess.Login); err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})(w, r)
	})

	// --- Блюда: CRUD ---
	mux.HandleFunc("/api/dishes/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		requireAuth(func(w http.ResponseWriter, r *http.Request) {
			sess := sessionFromRequest(r)
			var body struct {
				ID          int          `json:"id"`
				Name        string       `json:"name"`
				Price       float64      `json:"price"`
				Ingredients []Ingredient `json:"ingredients"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeErr(w, http.StatusBadRequest, "некорректный JSON")
				return
			}
			body.Name = strings.TrimSpace(body.Name)
			if body.Name == "" {
				writeErr(w, http.StatusBadRequest, "укажите название блюда")
				return
			}
			var dish Dish
			var err error
			if body.ID > 0 {
				dish, err = store.UpdateDish(body.ID, body.Name, body.Price, body.Ingredients, sess.Login)
			} else {
				dish, err = store.AddDish(body.Name, body.Price, body.Ingredients, sess.Login)
			}
			if err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, dish)
		})(w, r)
	})

	mux.HandleFunc("/api/dishes/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		requireAuth(func(w http.ResponseWriter, r *http.Request) {
			sess := sessionFromRequest(r)
			var body struct {
				DishID int `json:"dish_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeErr(w, http.StatusBadRequest, "некорректный JSON")
				return
			}
			if err := store.DeleteDish(body.DishID, sess.Login); err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})(w, r)
	})

	// --- Кастомизация заказа ---
	mux.HandleFunc("/api/orders/customize/add", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		requireAuth(func(w http.ResponseWriter, r *http.Request) {
			sess := sessionFromRequest(r)
			var body struct {
				OrderID   int     `json:"order_id"`
				DishID    int     `json:"dish_id"`
				ProductID int     `json:"product_id"`
				Qty       float64 `json:"qty"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeErr(w, http.StatusBadRequest, "некорректный JSON")
				return
			}
			cust, price, err := store.CustomizeOrderDish(body.OrderID, body.DishID, body.ProductID, "add", body.Qty, sess.Login)
			if err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"customization": cust,
				"delta":         price,
			})
		})(w, r)
	})

	mux.HandleFunc("/api/orders/customize/remove", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		requireAuth(func(w http.ResponseWriter, r *http.Request) {
			sess := sessionFromRequest(r)
			var body struct {
				OrderID   int     `json:"order_id"`
				DishID    int     `json:"dish_id"`
				ProductID int     `json:"product_id"`
				Qty       float64 `json:"qty"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeErr(w, http.StatusBadRequest, "некорректный JSON")
				return
			}
			cust, price, err := store.CustomizeOrderDish(body.OrderID, body.DishID, body.ProductID, "remove", body.Qty, sess.Login)
			if err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"customization": cust,
				"delta":         price,
			})
		})(w, r)
	})

	// --- Статус заказа ---
	mux.HandleFunc("/api/orders/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		requireAuth(func(w http.ResponseWriter, r *http.Request) {
			sess := sessionFromRequest(r)
			var body struct {
				OrderID int    `json:"order_id"`
				Status  string `json:"status"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeErr(w, http.StatusBadRequest, "некорректный JSON")
				return
			}
			if err := store.UpdateOrderStatus(body.OrderID, body.Status, sess.Login); err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})(w, r)
	})

	// --- Акции: CRUD ---
	mux.HandleFunc("/api/promotions", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, store.GetAllPromotions())
		case http.MethodPost:
			requireAdmin(func(w http.ResponseWriter, r *http.Request) {
				sess := sessionFromRequest(r)
				var body struct {
					Type       string   `json:"type"`
					Name       string   `json:"name"`
					Discount   float64  `json:"discount"`
					MinSum     float64  `json:"min_sum"`
					HappyHour  [2]int   `json:"happy_hour"`
					ActiveDays []string `json:"active_days"`
					ValidFrom  string   `json:"valid_from"`
					ValidUntil string   `json:"valid_until"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					writeErr(w, http.StatusBadRequest, "некорректный JSON")
					return
				}
				p := Promotion{
					Type: body.Type, Name: body.Name, Discount: body.Discount,
					MinSum: body.MinSum, HappyHour: body.HappyHour,
					ActiveDays: body.ActiveDays,
				}
				if body.ValidFrom != "" {
					if t, err := time.Parse("2006-01-02", body.ValidFrom); err == nil {
						p.ValidFrom = t
					}
				}
				if body.ValidUntil != "" {
					if t, err := time.Parse("2006-01-02", body.ValidUntil); err == nil {
						p.ValidUntil = t
					}
				}
				res, err := store.AddPromotion(p, sess.Login)
				if err != nil {
					writeErr(w, http.StatusBadRequest, err.Error())
					return
				}
				writeJSON(w, http.StatusCreated, res)
			})(w, r)
		default:
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})

	mux.HandleFunc("/api/promotions/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		requireAdmin(func(w http.ResponseWriter, r *http.Request) {
			sess := sessionFromRequest(r)
			var body struct {
				ID       int     `json:"id"`
				Name     string  `json:"name"`
				Discount float64 `json:"discount"`
				Enabled  bool    `json:"enabled"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeErr(w, http.StatusBadRequest, "некорректный JSON")
				return
			}
			res, err := store.UpdatePromotion(body.ID, body.Name, body.Discount, body.Enabled, sess.Login)
			if err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, res)
		})(w, r)
	})

	mux.HandleFunc("/api/promotions/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		requireAdmin(func(w http.ResponseWriter, r *http.Request) {
			sess := sessionFromRequest(r)
			var body struct {
				PromotionID int `json:"promotion_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeErr(w, http.StatusBadRequest, "некорректный JSON")
				return
			}
			if err := store.DeletePromotion(body.PromotionID, sess.Login); err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})(w, r)
	})

	// --- График поставок ---
	mux.HandleFunc("/api/supply-schedule", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, store.SupplySchedule())
		case http.MethodPost:
			requireAdmin(func(w http.ResponseWriter, r *http.Request) {
				sess := sessionFromRequest(r)
				var body struct {
					ProductID     int     `json:"product_id"`
					DaysBetween   int     `json:"days_between"`
					LeadDays      int     `json:"lead_days"`
					MinStockLevel float64 `json:"min_stock_level"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					writeErr(w, http.StatusBadRequest, "некорректный JSON")
					return
				}
				if err := store.SetSupplySchedule(body.ProductID, body.DaysBetween, body.LeadDays, body.MinStockLevel, sess.Login); err != nil {
					writeErr(w, http.StatusBadRequest, err.Error())
					return
				}
				writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			})(w, r)
		default:
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})

	mux.HandleFunc("/api/supply-schedule/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		requireAdmin(func(w http.ResponseWriter, r *http.Request) {
			sess := sessionFromRequest(r)
			var body struct {
				ProductID int `json:"product_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeErr(w, http.StatusBadRequest, "некорректный JSON")
				return
			}
			if err := store.DeleteSupplySchedule(body.ProductID, sess.Login); err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})(w, r)
	})

	// --- Автозаказ ---
	mux.HandleFunc("/api/auto-supply", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			items, rows, err := store.GenerateAutoSupply()
			if err != nil {
				writeJSON(w, http.StatusOK, map[string]interface{}{
					"message": err.Error(),
					"items":   items,
					"rows":    rows,
				})
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"items": items,
				"rows":  rows,
			})
		case http.MethodPost:
			requireAuth(func(w http.ResponseWriter, r *http.Request) {
				sess := sessionFromRequest(r)
				sup, err := store.CreateAutoSupply(sess.Login)
				if err != nil {
					writeErr(w, http.StatusBadRequest, err.Error())
					return
				}
				writeJSON(w, http.StatusCreated, sup)
			})(w, r)
		default:
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})

	// --- Получить заказ по ID (для кастомизации) ---
	mux.HandleFunc("/api/orders/by-id", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		requireAuth(func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query().Get("id")
			id, err := strconv.Atoi(q)
			if err != nil {
				writeErr(w, http.StatusBadRequest, "укажите order_id")
				return
			}
			order, err := store.GetOrder(id)
			if err != nil {
				writeErr(w, http.StatusNotFound, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, order)
		})(w, r)
	})

	// --- Применить скидку к заказу ---
	mux.HandleFunc("/api/orders/discount", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		requireAuth(func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				OrderID int `json:"order_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeErr(w, http.StatusBadRequest, "некорректный JSON")
				return
			}
			discount, reason := store.CalcOrderDiscount(body.OrderID)
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"order_id": body.OrderID,
				"discount": discount,
				"reason":   reason,
			})
		})(w, r)
	})

	registerAuthRoutes(mux)
	registerDemoRoutes(mux)

	fs := http.FileServer(http.Dir(*staticDir))
	mux.Handle("/", fs)

	log.Printf("Сервер запущен на http://localhost:%s", port)
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           withCORS(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Fatal(srv.ListenAndServe())
}

var staticDir = flag.String("static", "../frontend", "каталог со статикой фронтенда")

func withCORS(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(204)
			return
		}
		h.ServeHTTP(w, r)
	})
}
