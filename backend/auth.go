package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// ========================
// АВТОРИЗАЦИЯ
// ========================

// loginTTL — срок жизни сессии.
const loginTTL = 12 * time.Hour

type ctxKey int

const userCtxKey ctxKey = iota

// Login проверяет учётные данные и создаёт сессию. Возвращает токен.
func (s *Store) Login(login, password string) (*Session, error) {
	s.mu.RLock()
	u, ok := s.users[login]
	s.mu.RUnlock()
	if !ok || !verifyPassword(password, u.Password) {
		return nil, fmt.Errorf("неверный логин или пароль")
	}
	sess := &Session{
		Token:  randomToken(),
		Login:  u.Login,
		Role:   u.Role,
		Expire: time.Now().Add(loginTTL),
	}
	s.sessMu.Lock()
	s.sessions[sess.Token] = sess
	s.sessMu.Unlock()
	s.log("auth", fmt.Sprintf("Вход: %s (%s)", u.Login, u.Role))
	return sess, nil
}

// Logout завершает сессию по токену.
func (s *Store) Logout(token string) {
	s.sessMu.Lock()
	delete(s.sessions, token)
	s.sessMu.Unlock()
}

// DemoSession создаёт сессию для пользователя без проверки пароля.
// Нужен демо-режиму: пароли хранятся хэшами, восстановить их нельзя,
// а имитировать вход персонала нужно. Вызывать только из демо-кода.
func (s *Store) DemoSession(login string) (*Session, error) {
	s.mu.RLock()
	u, ok := s.users[login]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("пользователь %s не найден", login)
	}
	sess := &Session{
		Token:  randomToken(),
		Login:  u.Login,
		Role:   u.Role,
		Expire: time.Now().Add(loginTTL),
	}
	s.sessMu.Lock()
	s.sessions[sess.Token] = sess
	s.sessMu.Unlock()
	s.log("auth", fmt.Sprintf("Вход: %s (%s)", u.Login, u.Role))
	return sess, nil
}

// SessionByToken возвращает активную сессию или nil.
func (s *Store) SessionByToken(token string) *Session {
	if token == "" {
		return nil
	}
	s.sessMu.RLock()
	defer s.sessMu.RUnlock()
	sess := s.sessions[token]
	if sess == nil || time.Now().After(sess.Expire) {
		return nil
	}
	return sess
}

// bearerToken извлекает токен из заголовка Authorization.
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	// Резервный вариант — query-параметр (для EventSource).
	return r.URL.Query().Get("token")
}

// requireAuth пропускает только аутентифицированные запросы.
func requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess := store.SessionByToken(bearerToken(r))
		if sess == nil {
			writeErr(w, http.StatusUnauthorized, "требуется авторизация")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userCtxKey, sess)))
	}
}

// requireAdmin пропускает только роль admin.
func requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth(func(w http.ResponseWriter, r *http.Request) {
		sess := r.Context().Value(userCtxKey).(*Session)
		if sess.Role != "admin" {
			writeErr(w, http.StatusForbidden, "недостаточно прав (нужна роль администратора)")
			return
		}
		next(w, r)
	})
}

func sessionFromRequest(r *http.Request) *Session {
	if v, ok := r.Context().Value(userCtxKey).(*Session); ok {
		return v
	}
	return nil
}

// ========================
// РУЧНОЕ РЕДАКТИРОВАНИЕ
// ========================

// AdjustBatchQty корректирует остаток партии (инвентаризация).
// Дробь допустима только для веса/объёма; штуки всегда целые.
func (s *Store) AdjustBatchQty(batchID int, newQty float64, reason string, who string) (Batch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.batches {
		b := &s.batches[i]
		if b.ID != batchID {
			continue
		}
		newQty = normalizeQty(newQty, b.Unit)
		if newQty < 0 {
			return Batch{}, fmt.Errorf("остаток не может быть отрицательным")
		}
		old := b.Qty
		b.Qty = newQty
		diff := newQty - old
		s.log("adjust", fmt.Sprintf("%s: %s %s — %s → %s (%s)",
			who, b.Product, b.BatchNumber, fmtQty(old, b.Unit), fmtQty(newQty, b.Unit), reason))
		s.broadcast("stock", "adjust")
		_ = diff
		return *b, nil
	}
	return Batch{}, fmt.Errorf("партия %d не найдена", batchID)
}

// SetBatchExpiry меняет срок годности партии (доступно админу).
func (s *Store) SetBatchExpiry(batchID int, expiry time.Time, who string) (Batch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.batches {
		b := &s.batches[i]
		if b.ID != batchID {
			continue
		}
		b.ExpiryDate = expiry
		s.log("adjust", fmt.Sprintf("%s: изменён срок партии %s (%s) до %s",
			who, b.Product, b.BatchNumber, expiry.Format("02.01.2006")))
		s.broadcast("stock", "expiry")
		return *b, nil
	}
	return Batch{}, fmt.Errorf("партия %d не найдена", batchID)
}

// ManualWriteoff списывает произвольное количество из партии.
func (s *Store) ManualWriteoff(batchID int, qty float64, reason string, who string) (Writeoff, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.batches {
		b := &s.batches[i]
		if b.ID != batchID {
			continue
		}
		qty = normalizeQty(qty, b.Unit)
		if qty <= 0 {
			return Writeoff{}, fmt.Errorf("количество должно быть > 0")
		}
		if qty > b.Qty+1e-9 {
			return Writeoff{}, fmt.Errorf("нельзя списать %s, на складе только %s",
				fmtQty(qty, b.Unit), fmtQty(b.Qty, b.Unit))
		}
		if reason == "" {
			reason = "ручное списание"
		}
		b.Qty = normalizeQty(b.Qty-qty, b.Unit)
		w := Writeoff{
			ID: s.nextID("writeoff"), BatchID: b.ID, Product: b.Product,
			BatchNumber: b.BatchNumber, Qty: qty,
			Unit: b.Unit, Display: fmtQty(qty, b.Unit),
			Reason: reason, CreatedAt: time.Now(),
		}
		s.writeoffs = append(s.writeoffs, w)
		s.log("writeoff", fmt.Sprintf("%s списал %s: %s %s — %s",
			who, reason, b.Product, b.BatchNumber, fmtQty(qty, b.Unit)))
		s.broadcast("stock", "writeoff")
		return w, nil
	}
	return Writeoff{}, fmt.Errorf("партия %d не найдена", batchID)
}

// DeleteBatch удаляет партию (доступно админу).
func (s *Store) DeleteBatch(batchID int, who string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.batches {
		if s.batches[i].ID == batchID {
			b := s.batches[i]
			s.batches = append(s.batches[:i], s.batches[i+1:]...)
			s.log("adjust", fmt.Sprintf("%s: удалена партия %s (%s)", who, b.Product, b.BatchNumber))
			s.broadcast("stock", "delete")
			return nil
		}
	}
	return fmt.Errorf("партия %d не найдена", batchID)
}

// CancelOrder возвращает списанные по заказу продукты на склад (доступно админу).
func (s *Store) CancelOrder(orderID int, who string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ord *Order
	for i := range s.orders {
		if s.orders[i].ID == orderID {
			ord = &s.orders[i]
			break
		}
	}
	if ord == nil {
		return fmt.Errorf("заказ %d не найден", orderID)
	}
	// Восстанавливаем потребность по рецептам.
	need := map[int]float64{}
	punit := map[int]string{}
	for _, p := range s.products {
		punit[p.ID] = p.Unit
	}
	for _, it := range ord.Items {
		for _, d := range s.dishes {
			if d.ID != it.DishID {
				continue
			}
			for _, ing := range d.Ingredients {
				need[ing.ProductID] += ing.Qty * float64(it.Qty)
			}
		}
	}
	for pid, q := range need {
		q = normalizeQty(q, punit[pid])
		// Возвращаем в самую раннюю партию продукта.
		idx := -1
		for i := range s.batches {
			if s.batches[i].ProductID != pid {
				continue
			}
			if idx < 0 || s.batches[i].ExpiryDate.Before(s.batches[idx].ExpiryDate) {
				idx = i
			}
		}
		if idx < 0 {
			continue
		}
		s.batches[idx].Qty = normalizeQty(s.batches[idx].Qty+q, s.batches[idx].Unit)
	}
	// Удаляем заказ из истории.
	for i := range s.orders {
		if s.orders[i].ID == orderID {
			s.orders = append(s.orders[:i], s.orders[i+1:]...)
			break
		}
	}
	s.log("order_cancel", fmt.Sprintf("%s: отменён заказ %s, продукты возвращены", who, ord.Number))
	s.broadcast("stock", "cancel")
	return nil
}

// ========================
// HTTP-РОУТЫ АВТОРИЗАЦИИ
// ========================

func registerAuthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var body struct {
			Login    string `json:"login"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "некорректный JSON")
			return
		}
		sess, err := store.Login(strings.TrimSpace(body.Login), body.Password)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"token": sess.Token, "login": sess.Login, "role": sess.Role,
		})
	})

	mux.HandleFunc("/api/logout", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		store.Logout(bearerToken(r))
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("/api/me", func(w http.ResponseWriter, r *http.Request) {
		requireAuth(func(w http.ResponseWriter, r *http.Request) {
			sess := sessionFromRequest(r)
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"login": sess.Login, "role": sess.Role,
			})
		})(w, r)
	})

	// --- Ручное редактирование партий ---
	mux.HandleFunc("/api/batches/adjust", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		requireAuth(func(w http.ResponseWriter, r *http.Request) {
			sess := sessionFromRequest(r)
			var body struct {
				BatchID int     `json:"batch_id"`
				Qty     float64 `json:"qty"`
				Reason  string  `json:"reason"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeErr(w, http.StatusBadRequest, "некорректный JSON")
				return
			}
			b, err := store.AdjustBatchQty(body.BatchID, body.Qty, body.Reason, sess.Login)
			if err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, b)
		})(w, r)
	})

	mux.HandleFunc("/api/batches/expiry", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		requireAdmin(func(w http.ResponseWriter, r *http.Request) {
			sess := sessionFromRequest(r)
			var body struct {
				BatchID int    `json:"batch_id"`
				Expiry  string `json:"expiry"` // формат 2006-01-02
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeErr(w, http.StatusBadRequest, "некорректный JSON")
				return
			}
			t, err := time.Parse("2006-01-02", body.Expiry)
			if err != nil {
				writeErr(w, http.StatusBadRequest, "ожидалась дата в формате ГГГГ-ММ-ДД")
				return
			}
			b, err := store.SetBatchExpiry(body.BatchID, t, sess.Login)
			if err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, b)
		})(w, r)
	})

	mux.HandleFunc("/api/batches/writeoff", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		requireAuth(func(w http.ResponseWriter, r *http.Request) {
			sess := sessionFromRequest(r)
			var body struct {
				BatchID int     `json:"batch_id"`
				Qty     float64 `json:"qty"`
				Reason  string  `json:"reason"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeErr(w, http.StatusBadRequest, "некорректный JSON")
				return
			}
			wo, err := store.ManualWriteoff(body.BatchID, body.Qty, body.Reason, sess.Login)
			if err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, wo)
		})(w, r)
	})

	mux.HandleFunc("/api/batches/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		requireAdmin(func(w http.ResponseWriter, r *http.Request) {
			sess := sessionFromRequest(r)
			var body struct {
				BatchID int `json:"batch_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeErr(w, http.StatusBadRequest, "некорректный JSON")
				return
			}
			if err := store.DeleteBatch(body.BatchID, sess.Login); err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})(w, r)
	})

	mux.HandleFunc("/api/orders/cancel", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		requireAdmin(func(w http.ResponseWriter, r *http.Request) {
			sess := sessionFromRequest(r)
			var body struct {
				OrderID int `json:"order_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeErr(w, http.StatusBadRequest, "некорректный JSON")
				return
			}
			if err := store.CancelOrder(body.OrderID, sess.Login); err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})(w, r)
	})

	// --- Ручной стоп-лист блюд ---
	mux.HandleFunc("/api/stoplist/add", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		requireAuth(func(w http.ResponseWriter, r *http.Request) {
			sess := sessionFromRequest(r)
			var body struct {
				DishID int    `json:"dish_id"`
				Reason string `json:"reason"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeErr(w, http.StatusBadRequest, "некорректный JSON")
				return
			}
			if err := store.AddManualStop(body.DishID, strings.TrimSpace(body.Reason), sess.Login); err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})(w, r)
	})

	mux.HandleFunc("/api/stoplist/remove", func(w http.ResponseWriter, r *http.Request) {
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
			if err := store.RemoveManualStop(body.DishID, sess.Login); err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})(w, r)
	})

	// --- Управление пользователями (только админ) ---
	mux.HandleFunc("/api/users", func(w http.ResponseWriter, r *http.Request) {
		requireAdmin(func(w http.ResponseWriter, r *http.Request) {
			store.mu.RLock()
			out := make([]User, 0, len(store.users))
			for _, u := range store.users {
				out = append(out, u)
			}
			store.mu.RUnlock()
			sort.Slice(out, func(i, j int) bool { return out[i].Login < out[j].Login })
			writeJSON(w, http.StatusOK, out)
		})(w, r)
	})

	mux.HandleFunc("/api/users/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		requireAdmin(func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Login    string `json:"login"`
				Password string `json:"password"`
				Role     string `json:"role"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeErr(w, http.StatusBadRequest, "некорректный JSON")
				return
			}
			body.Login = strings.TrimSpace(body.Login)
			if body.Login == "" {
				writeErr(w, http.StatusBadRequest, "логин не может быть пустым")
				return
			}
			if body.Role != "admin" && body.Role != "staff" {
				writeErr(w, http.StatusBadRequest, "роль должна быть admin или staff")
				return
			}
			store.mu.Lock()
			existing, ok := store.users[body.Login]
			if ok && body.Password == "" {
				// Пароль не меняем.
				body.Password = existing.Password
			} else if body.Password == "" {
				store.mu.Unlock()
				writeErr(w, http.StatusBadRequest, "укажите пароль для нового пользователя")
				return
			}
			store.users[body.Login] = User{
				Login: body.Login, Password: hashPassword(body.Password), Role: body.Role,
			}
			store.mu.Unlock()
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "login": body.Login})
		})(w, r)
	})

	mux.HandleFunc("/api/users/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		requireAdmin(func(w http.ResponseWriter, r *http.Request) {
			sess := sessionFromRequest(r)
			var body struct {
				Login string `json:"login"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeErr(w, http.StatusBadRequest, "некорректный JSON")
				return
			}
			if body.Login == sess.Login {
				writeErr(w, http.StatusBadRequest, "нельзя удалить собственную учётную запись")
				return
			}
			store.mu.Lock()
			if _, ok := store.users[body.Login]; !ok {
				store.mu.Unlock()
				writeErr(w, http.StatusBadRequest, "пользователь не найден")
				return
			}
			delete(store.users, body.Login)
			store.mu.Unlock()
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})(w, r)
	})
}
