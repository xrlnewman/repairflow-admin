package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	WorkOrderPending            = "待受理"
	WorkOrderDiagnosed          = "已诊断"
	WorkOrderQuoted             = "待报价"
	WorkOrderDispatched         = "待派工"
	WorkOrderOnSite             = "上门中"
	WorkOrderAwaitingAcceptance = "待验收"
	WorkOrderClosed             = "已结案"
)

var workOrderTransitions = map[string]map[string]bool{
	WorkOrderPending:            {WorkOrderDiagnosed: true},
	WorkOrderDiagnosed:          {WorkOrderQuoted: true},
	WorkOrderQuoted:             {WorkOrderDispatched: true},
	WorkOrderDispatched:         {WorkOrderOnSite: true},
	WorkOrderOnSite:             {WorkOrderAwaitingAcceptance: true},
	WorkOrderAwaitingAcceptance: {WorkOrderClosed: true},
	WorkOrderClosed:             {},
}

type WorkOrder struct {
	ID           string           `json:"id"`
	CustomerID   string           `json:"customerId,omitempty"`
	CustomerName string           `json:"customerName"`
	Phone        string           `json:"phone,omitempty"`
	Address      string           `json:"address,omitempty"`
	Category     string           `json:"category"`
	Description  string           `json:"description"`
	Priority     string           `json:"priority"`
	Status       string           `json:"status"`
	CreatedAt    string           `json:"createdAt"`
	UpdatedAt    string           `json:"updatedAt"`
	Quote        *Quote           `json:"quote,omitempty"`
	Dispatch     *Dispatch        `json:"dispatch,omitempty"`
	Acceptance   *Acceptance      `json:"acceptance,omitempty"`
	Warranty     *Warranty        `json:"warranty,omitempty"`
	Events       []WorkOrderEvent `json:"events,omitempty"`
}

type Quote struct {
	ID            string `json:"id"`
	WorkOrderID   string `json:"workOrderId"`
	LaborCents    int64  `json:"laborCents"`
	MaterialCents int64  `json:"materialCents"`
	TotalCents    int64  `json:"totalCents"`
	Note          string `json:"note,omitempty"`
	CreatedAt     string `json:"createdAt"`
}
type Dispatch struct {
	ID          string `json:"id"`
	WorkOrderID string `json:"workOrderId"`
	Technician  string `json:"technician"`
	ScheduledAt string `json:"scheduledAt"`
	Note        string `json:"note,omitempty"`
	CreatedAt   string `json:"createdAt"`
}
type Acceptance struct {
	ID           string `json:"id"`
	WorkOrderID  string `json:"workOrderId"`
	Result       string `json:"result"`
	CustomerSign string `json:"customerSign"`
	AcceptedAt   string `json:"acceptedAt"`
}
type Warranty struct {
	ID          string `json:"id"`
	WorkOrderID string `json:"workOrderId"`
	ExpiresAt   string `json:"expiresAt"`
	Note        string `json:"note,omitempty"`
	CreatedAt   string `json:"createdAt"`
}
type WorkOrderEvent struct {
	ID          string `json:"id"`
	WorkOrderID string `json:"workOrderId"`
	FromStatus  string `json:"fromStatus,omitempty"`
	ToStatus    string `json:"toStatus"`
	Type        string `json:"type"`
	Actor       string `json:"actor"`
	Note        string `json:"note,omitempty"`
	CreatedAt   string `json:"createdAt"`
}

type CreateWorkOrderInput struct {
	CustomerID   string `json:"customerId"`
	CustomerName string `json:"customerName"`
	Phone        string `json:"phone"`
	Address      string `json:"address"`
	Category     string `json:"category"`
	Description  string `json:"description"`
	Priority     string `json:"priority"`
}
type WorkOrderStatusInput struct {
	Status string `json:"status" binding:"required"`
	Actor  string `json:"actor"`
}
type WorkOrderQuoteInput struct {
	LaborCents    int64  `json:"laborCents"`
	MaterialCents int64  `json:"materialCents"`
	Note          string `json:"note"`
}
type WorkOrderDispatchInput struct {
	Technician  string `json:"technician" binding:"required"`
	ScheduledAt string `json:"scheduledAt" binding:"required"`
	Note        string `json:"note"`
}
type WorkOrderAcceptanceInput struct {
	Result       string `json:"result" binding:"required"`
	CustomerSign string `json:"customerSign" binding:"required"`
}
type WorkOrderWarrantyInput struct {
	ExpiresAt string `json:"expiresAt" binding:"required"`
	Note      string `json:"note"`
}

type WorkOrderStore interface {
	ListWorkOrders(context.Context, int, int, string) ([]WorkOrder, int, error)
	GetWorkOrder(context.Context, string) (WorkOrder, error)
	CreateWorkOrder(context.Context, WorkOrder) (WorkOrder, error)
	UpdateWorkOrderStatus(context.Context, string, string, string) (WorkOrder, WorkOrderEvent, error)
	CreateWorkOrderQuote(context.Context, string, Quote) (WorkOrder, WorkOrderEvent, error)
	DispatchWorkOrder(context.Context, string, Dispatch) (WorkOrder, WorkOrderEvent, error)
	AcceptWorkOrder(context.Context, string, Acceptance) (WorkOrder, WorkOrderEvent, error)
	CreateWorkOrderWarranty(context.Context, string, Warranty) (WorkOrder, WorkOrderEvent, error)
}

type RepairService struct {
	store WorkOrderStore
	idem  idempotencyStore
}

func NewRepairService(store WorkOrderStore, idem idempotencyStore) *RepairService {
	return &RepairService{store: store, idem: idem}
}

func (s *RepairService) CreateWorkOrder(ctx context.Context, input CreateWorkOrderInput, key string) (WorkOrder, error) {
	if strings.TrimSpace(key) == "" {
		return WorkOrder{}, ErrMissingIdempotencyKey
	}
	if strings.TrimSpace(input.CustomerName) == "" && strings.TrimSpace(input.CustomerID) == "" {
		return WorkOrder{}, fmt.Errorf("%w: customer is required", ErrInvalidInput)
	}
	if strings.TrimSpace(input.Description) == "" {
		return WorkOrder{}, fmt.Errorf("%w: description is required", ErrInvalidInput)
	}
	rk := "work-order:create:" + key
	if id, ok, err := s.idem.Get(ctx, rk); err != nil {
		return WorkOrder{}, err
	} else if ok {
		return s.store.GetWorkOrder(ctx, id)
	}
	release, err := s.idem.Lock(ctx, "work-order:create-lock", 10*time.Second)
	if err != nil {
		return WorkOrder{}, err
	}
	defer release()
	if id, ok, err := s.idem.Get(ctx, rk); err != nil {
		return WorkOrder{}, err
	} else if ok {
		return s.store.GetWorkOrder(ctx, id)
	}
	priority := input.Priority
	if priority == "" {
		priority = "普通"
	}
	category := input.Category
	if category == "" {
		category = "综合维修"
	}
	order, err := s.store.CreateWorkOrder(ctx, WorkOrder{CustomerID: input.CustomerID, CustomerName: input.CustomerName, Phone: input.Phone, Address: input.Address, Category: category, Description: input.Description, Priority: priority, Status: WorkOrderPending})
	if err != nil {
		return WorkOrder{}, err
	}
	if err = s.idem.Set(ctx, rk, order.ID, 24*time.Hour); err != nil {
		return WorkOrder{}, err
	}
	return order, nil
}

func (s *RepairService) UpdateWorkOrderStatus(ctx context.Context, id, status, actor, key string) (WorkOrder, WorkOrderEvent, error) {
	if strings.TrimSpace(key) == "" {
		return WorkOrder{}, WorkOrderEvent{}, ErrMissingIdempotencyKey
	}
	if !validWorkOrderStatus(status) {
		return WorkOrder{}, WorkOrderEvent{}, fmt.Errorf("%w: unknown work order status", ErrInvalidInput)
	}
	rk := "work-order:status:" + id + ":" + key
	if existing, ok, err := s.idem.Get(ctx, rk); err != nil {
		return WorkOrder{}, WorkOrderEvent{}, err
	} else if ok {
		order, e := s.store.GetWorkOrder(ctx, existing)
		if e != nil {
			return WorkOrder{}, WorkOrderEvent{}, e
		}
		return order, latestWorkOrderEvent(order), nil
	}
	release, err := s.idem.Lock(ctx, "work-order:status-lock:"+id, 10*time.Second)
	if err != nil {
		return WorkOrder{}, WorkOrderEvent{}, err
	}
	defer release()
	if actor == "" {
		actor = "调度员"
	}
	order, event, err := s.store.UpdateWorkOrderStatus(ctx, id, status, actor)
	if err != nil {
		return WorkOrder{}, WorkOrderEvent{}, err
	}
	if err = s.idem.Set(ctx, rk, order.ID, 24*time.Hour); err != nil {
		return WorkOrder{}, WorkOrderEvent{}, err
	}
	return order, event, nil
}

func (s *RepairService) CreateWorkOrderQuote(ctx context.Context, id string, input WorkOrderQuoteInput, key string) (WorkOrder, WorkOrderEvent, error) {
	if strings.TrimSpace(key) == "" {
		return WorkOrder{}, WorkOrderEvent{}, ErrMissingIdempotencyKey
	}
	if input.LaborCents < 0 || input.MaterialCents < 0 || input.LaborCents+input.MaterialCents <= 0 {
		return WorkOrder{}, WorkOrderEvent{}, fmt.Errorf("%w: quote amount is required", ErrInvalidInput)
	}
	rk := "work-order:quote:" + id + ":" + key
	if existing, ok, err := s.idem.Get(ctx, rk); err != nil {
		return WorkOrder{}, WorkOrderEvent{}, err
	} else if ok {
		order, e := s.store.GetWorkOrder(ctx, existing)
		if e != nil {
			return WorkOrder{}, WorkOrderEvent{}, e
		}
		return order, latestWorkOrderEvent(order), nil
	}
	quote := Quote{LaborCents: input.LaborCents, MaterialCents: input.MaterialCents, TotalCents: input.LaborCents + input.MaterialCents, Note: input.Note, CreatedAt: nowUTC()}
	order, event, err := s.store.CreateWorkOrderQuote(ctx, id, quote)
	if err != nil {
		return WorkOrder{}, WorkOrderEvent{}, err
	}
	if err = s.idem.Set(ctx, rk, order.ID, 24*time.Hour); err != nil {
		return WorkOrder{}, WorkOrderEvent{}, err
	}
	return order, event, nil
}
func (s *RepairService) DispatchWorkOrder(ctx context.Context, id string, input WorkOrderDispatchInput, key string) (WorkOrder, WorkOrderEvent, error) {
	if strings.TrimSpace(key) == "" {
		return WorkOrder{}, WorkOrderEvent{}, ErrMissingIdempotencyKey
	}
	if strings.TrimSpace(input.Technician) == "" || strings.TrimSpace(input.ScheduledAt) == "" {
		return WorkOrder{}, WorkOrderEvent{}, fmt.Errorf("%w: technician and schedule are required", ErrInvalidInput)
	}
	rk := "work-order:dispatch:" + id + ":" + key
	if existing, ok, err := s.idem.Get(ctx, rk); err != nil {
		return WorkOrder{}, WorkOrderEvent{}, err
	} else if ok {
		order, e := s.store.GetWorkOrder(ctx, existing)
		if e != nil {
			return WorkOrder{}, WorkOrderEvent{}, e
		}
		return order, latestWorkOrderEvent(order), nil
	}
	order, event, err := s.store.DispatchWorkOrder(ctx, id, Dispatch{Technician: input.Technician, ScheduledAt: input.ScheduledAt, Note: input.Note, CreatedAt: nowUTC()})
	if err != nil {
		return WorkOrder{}, WorkOrderEvent{}, err
	}
	if err = s.idem.Set(ctx, rk, order.ID, 24*time.Hour); err != nil {
		return WorkOrder{}, WorkOrderEvent{}, err
	}
	return order, event, nil
}
func (s *RepairService) AcceptWorkOrder(ctx context.Context, id string, input WorkOrderAcceptanceInput, key string) (WorkOrder, WorkOrderEvent, error) {
	if strings.TrimSpace(key) == "" {
		return WorkOrder{}, WorkOrderEvent{}, ErrMissingIdempotencyKey
	}
	if strings.TrimSpace(input.Result) == "" || strings.TrimSpace(input.CustomerSign) == "" {
		return WorkOrder{}, WorkOrderEvent{}, fmt.Errorf("%w: acceptance result and signature are required", ErrInvalidInput)
	}
	rk := "work-order:acceptance:" + id + ":" + key
	if existing, ok, err := s.idem.Get(ctx, rk); err != nil {
		return WorkOrder{}, WorkOrderEvent{}, err
	} else if ok {
		order, e := s.store.GetWorkOrder(ctx, existing)
		if e != nil {
			return WorkOrder{}, WorkOrderEvent{}, e
		}
		return order, latestWorkOrderEvent(order), nil
	}
	order, event, err := s.store.AcceptWorkOrder(ctx, id, Acceptance{Result: input.Result, CustomerSign: input.CustomerSign, AcceptedAt: nowUTC()})
	if err != nil {
		return WorkOrder{}, WorkOrderEvent{}, err
	}
	if err = s.idem.Set(ctx, rk, order.ID, 24*time.Hour); err != nil {
		return WorkOrder{}, WorkOrderEvent{}, err
	}
	return order, event, nil
}
func (s *RepairService) CreateWorkOrderWarranty(ctx context.Context, id string, input WorkOrderWarrantyInput, key string) (WorkOrder, WorkOrderEvent, error) {
	if strings.TrimSpace(key) == "" {
		return WorkOrder{}, WorkOrderEvent{}, ErrMissingIdempotencyKey
	}
	if strings.TrimSpace(input.ExpiresAt) == "" {
		return WorkOrder{}, WorkOrderEvent{}, fmt.Errorf("%w: expiry is required", ErrInvalidInput)
	}
	rk := "work-order:warranty:" + id + ":" + key
	if existing, ok, err := s.idem.Get(ctx, rk); err != nil {
		return WorkOrder{}, WorkOrderEvent{}, err
	} else if ok {
		order, e := s.store.GetWorkOrder(ctx, existing)
		if e != nil {
			return WorkOrder{}, WorkOrderEvent{}, e
		}
		return order, latestWorkOrderEvent(order), nil
	}
	order, event, err := s.store.CreateWorkOrderWarranty(ctx, id, Warranty{ExpiresAt: input.ExpiresAt, Note: input.Note, CreatedAt: nowUTC()})
	if err != nil {
		return WorkOrder{}, WorkOrderEvent{}, err
	}
	if err = s.idem.Set(ctx, rk, order.ID, 24*time.Hour); err != nil {
		return WorkOrder{}, WorkOrderEvent{}, err
	}
	return order, event, nil
}

func validWorkOrderStatus(status string) bool { _, ok := workOrderTransitions[status]; return ok }

func (s *MemoryStore) ListWorkOrders(_ context.Context, page, pageSize int, status string) ([]WorkOrder, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	all := make([]WorkOrder, 0, len(s.workOrders))
	for _, order := range s.workOrders {
		if status == "" || order.Status == status {
			all = append(all, s.workOrderSnapshot(order))
		}
	}
	return paginate(all, page, pageSize)
}
func (s *MemoryStore) GetWorkOrder(_ context.Context, id string) (WorkOrder, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	order, ok := s.workOrders[id]
	if !ok {
		return WorkOrder{}, ErrNotFound
	}
	return s.workOrderSnapshot(order), nil
}
func (s *MemoryStore) CreateWorkOrder(_ context.Context, order WorkOrder) (WorkOrder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if order.ID == "" {
		order.ID = s.next("WO")
	}
	if order.Status == "" {
		order.Status = WorkOrderPending
	}
	if order.CreatedAt == "" {
		order.CreatedAt = nowUTC()
	}
	order.UpdatedAt = order.CreatedAt
	s.workOrders[order.ID] = order
	s.workOrderEvents[order.ID] = []WorkOrderEvent{{ID: s.next("WOEV"), WorkOrderID: order.ID, ToStatus: order.Status, Type: "created", Actor: "system", CreatedAt: order.CreatedAt}}
	return s.workOrderSnapshot(order), nil
}
func (s *MemoryStore) UpdateWorkOrderStatus(_ context.Context, id, status, actor string) (WorkOrder, WorkOrderEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	order, ok := s.workOrders[id]
	if !ok {
		return WorkOrder{}, WorkOrderEvent{}, ErrNotFound
	}
	if !workOrderTransitions[order.Status][status] {
		return WorkOrder{}, WorkOrderEvent{}, ErrInvalidTransition
	}
	old := order.Status
	order.Status = status
	order.UpdatedAt = nowUTC()
	s.workOrders[id] = order
	event := WorkOrderEvent{ID: s.next("WOEV"), WorkOrderID: id, FromStatus: old, ToStatus: status, Type: "status", Actor: actor, CreatedAt: order.UpdatedAt}
	s.workOrderEvents[id] = append(s.workOrderEvents[id], event)
	return s.workOrderSnapshot(order), event, nil
}
func (s *MemoryStore) CreateWorkOrderQuote(_ context.Context, id string, quote Quote) (WorkOrder, WorkOrderEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	order, ok := s.workOrders[id]
	if !ok {
		return WorkOrder{}, WorkOrderEvent{}, ErrNotFound
	}
	if !workOrderTransitions[order.Status][WorkOrderQuoted] {
		return WorkOrder{}, WorkOrderEvent{}, ErrInvalidTransition
	}
	quote.ID = s.next("QUOTE")
	quote.WorkOrderID = id
	if quote.CreatedAt == "" {
		quote.CreatedAt = nowUTC()
	}
	order.Quote = &quote
	old := order.Status
	order.Status = WorkOrderQuoted
	order.UpdatedAt = quote.CreatedAt
	s.workOrders[id] = order
	event := WorkOrderEvent{ID: s.next("WOEV"), WorkOrderID: id, FromStatus: old, ToStatus: order.Status, Type: "quote", Actor: "报价专员", CreatedAt: quote.CreatedAt, Note: quote.Note}
	s.workOrderEvents[id] = append(s.workOrderEvents[id], event)
	return s.workOrderSnapshot(order), event, nil
}
func (s *MemoryStore) DispatchWorkOrder(_ context.Context, id string, dispatch Dispatch) (WorkOrder, WorkOrderEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	order, ok := s.workOrders[id]
	if !ok {
		return WorkOrder{}, WorkOrderEvent{}, ErrNotFound
	}
	if !workOrderTransitions[order.Status][WorkOrderDispatched] {
		return WorkOrder{}, WorkOrderEvent{}, ErrInvalidTransition
	}
	dispatch.ID = s.next("DISPATCH")
	dispatch.WorkOrderID = id
	if dispatch.CreatedAt == "" {
		dispatch.CreatedAt = nowUTC()
	}
	order.Dispatch = &dispatch
	old := order.Status
	order.Status = WorkOrderDispatched
	order.UpdatedAt = dispatch.CreatedAt
	s.workOrders[id] = order
	event := WorkOrderEvent{ID: s.next("WOEV"), WorkOrderID: id, FromStatus: old, ToStatus: order.Status, Type: "dispatch", Actor: "调度员", CreatedAt: dispatch.CreatedAt, Note: dispatch.Technician}
	s.workOrderEvents[id] = append(s.workOrderEvents[id], event)
	return s.workOrderSnapshot(order), event, nil
}
func (s *MemoryStore) AcceptWorkOrder(_ context.Context, id string, acceptance Acceptance) (WorkOrder, WorkOrderEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	order, ok := s.workOrders[id]
	if !ok {
		return WorkOrder{}, WorkOrderEvent{}, ErrNotFound
	}
	if !workOrderTransitions[order.Status][WorkOrderAwaitingAcceptance] && order.Status != WorkOrderAwaitingAcceptance {
		return WorkOrder{}, WorkOrderEvent{}, ErrInvalidTransition
	}
	if order.Status == WorkOrderOnSite {
		old := order.Status
		order.Status = WorkOrderAwaitingAcceptance
		event := WorkOrderEvent{ID: s.next("WOEV"), WorkOrderID: id, FromStatus: old, ToStatus: order.Status, Type: "acceptance", Actor: "客户", CreatedAt: nowUTC()}
		s.workOrderEvents[id] = append(s.workOrderEvents[id], event)
	}
	acceptance.ID = s.next("ACCEPT")
	acceptance.WorkOrderID = id
	if acceptance.AcceptedAt == "" {
		acceptance.AcceptedAt = nowUTC()
	}
	order.Acceptance = &acceptance
	old := order.Status
	order.Status = WorkOrderClosed
	order.UpdatedAt = acceptance.AcceptedAt
	s.workOrders[id] = order
	event := WorkOrderEvent{ID: s.next("WOEV"), WorkOrderID: id, FromStatus: old, ToStatus: order.Status, Type: "acceptance", Actor: acceptance.CustomerSign, CreatedAt: acceptance.AcceptedAt, Note: acceptance.Result}
	s.workOrderEvents[id] = append(s.workOrderEvents[id], event)
	return s.workOrderSnapshot(order), event, nil
}
func (s *MemoryStore) CreateWorkOrderWarranty(_ context.Context, id string, warranty Warranty) (WorkOrder, WorkOrderEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	order, ok := s.workOrders[id]
	if !ok {
		return WorkOrder{}, WorkOrderEvent{}, ErrNotFound
	}
	if order.Status != WorkOrderClosed {
		return WorkOrder{}, WorkOrderEvent{}, fmt.Errorf("%w: warranty requires closed order", ErrInvalidTransition)
	}
	warranty.ID = s.next("WAR")
	warranty.WorkOrderID = id
	if warranty.CreatedAt == "" {
		warranty.CreatedAt = nowUTC()
	}
	order.Warranty = &warranty
	order.UpdatedAt = warranty.CreatedAt
	s.workOrders[id] = order
	event := WorkOrderEvent{ID: s.next("WOEV"), WorkOrderID: id, FromStatus: order.Status, ToStatus: order.Status, Type: "warranty", Actor: "售后专员", CreatedAt: warranty.CreatedAt, Note: warranty.ExpiresAt}
	s.workOrderEvents[id] = append(s.workOrderEvents[id], event)
	return s.workOrderSnapshot(order), event, nil
}
func (s *MemoryStore) workOrderSnapshot(order WorkOrder) WorkOrder {
	if order.Quote != nil {
		q := *order.Quote
		order.Quote = &q
	}
	if order.Dispatch != nil {
		d := *order.Dispatch
		order.Dispatch = &d
	}
	if order.Acceptance != nil {
		a := *order.Acceptance
		order.Acceptance = &a
	}
	if order.Warranty != nil {
		w := *order.Warranty
		order.Warranty = &w
	}
	order.Events = append([]WorkOrderEvent(nil), s.workOrderEvents[order.ID]...)
	return order
}
func latestWorkOrderEvent(order WorkOrder) WorkOrderEvent {
	if len(order.Events) == 0 {
		return WorkOrderEvent{}
	}
	return order.Events[len(order.Events)-1]
}

// SQLStore implements the workflow persistence contract. Related entities are kept in dedicated MySQL tables.
func (s *SQLStore) ListWorkOrders(ctx context.Context, page, pageSize int, status string) ([]WorkOrder, int, error) {
	page, pageSize = normalizePage(page, pageSize)
	args := []any{}
	where := ""
	if status != "" {
		where = " WHERE status=?"
		args = append(args, status)
	}
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM work_orders"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := s.db.QueryContext(ctx, "SELECT id,customer_id,customer_name,phone,address,category,description,priority,status,created_at,updated_at FROM work_orders"+where+" ORDER BY created_at DESC LIMIT ? OFFSET ?", args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []WorkOrder{}
	for rows.Next() {
		var order WorkOrder
		if err := rows.Scan(&order.ID, &order.CustomerID, &order.CustomerName, &order.Phone, &order.Address, &order.Category, &order.Description, &order.Priority, &order.Status, &order.CreatedAt, &order.UpdatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, order)
	}
	return out, total, rows.Err()
}
func (s *SQLStore) GetWorkOrder(ctx context.Context, id string) (WorkOrder, error) {
	var order WorkOrder
	err := s.db.QueryRowContext(ctx, `SELECT id,customer_id,customer_name,phone,address,category,description,priority,status,created_at,updated_at FROM work_orders WHERE id=?`, id).Scan(&order.ID, &order.CustomerID, &order.CustomerName, &order.Phone, &order.Address, &order.Category, &order.Description, &order.Priority, &order.Status, &order.CreatedAt, &order.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return WorkOrder{}, ErrNotFound
	}
	if err != nil {
		return WorkOrder{}, err
	}
	var q Quote
	if err = s.db.QueryRowContext(ctx, `SELECT id,work_order_id,labor_cents,material_cents,total_cents,note,created_at FROM work_order_quotes WHERE work_order_id=? ORDER BY created_at DESC LIMIT 1`, id).Scan(&q.ID, &q.WorkOrderID, &q.LaborCents, &q.MaterialCents, &q.TotalCents, &q.Note, &q.CreatedAt); err == nil {
		order.Quote = &q
	}
	var d Dispatch
	if err = s.db.QueryRowContext(ctx, `SELECT id,work_order_id,technician,scheduled_at,note,created_at FROM work_order_dispatches WHERE work_order_id=? ORDER BY created_at DESC LIMIT 1`, id).Scan(&d.ID, &d.WorkOrderID, &d.Technician, &d.ScheduledAt, &d.Note, &d.CreatedAt); err == nil {
		order.Dispatch = &d
	}
	var a Acceptance
	if err = s.db.QueryRowContext(ctx, `SELECT id,work_order_id,result,customer_sign,accepted_at FROM work_order_acceptances WHERE work_order_id=? ORDER BY accepted_at DESC LIMIT 1`, id).Scan(&a.ID, &a.WorkOrderID, &a.Result, &a.CustomerSign, &a.AcceptedAt); err == nil {
		order.Acceptance = &a
	}
	var w Warranty
	if err = s.db.QueryRowContext(ctx, `SELECT id,work_order_id,expires_at,note,created_at FROM work_order_warranties WHERE work_order_id=? ORDER BY created_at DESC LIMIT 1`, id).Scan(&w.ID, &w.WorkOrderID, &w.ExpiresAt, &w.Note, &w.CreatedAt); err == nil {
		order.Warranty = &w
	}
	rows, e := s.db.QueryContext(ctx, `SELECT id,work_order_id,from_status,to_status,type,actor,note,created_at FROM work_order_events WHERE work_order_id=? ORDER BY created_at`, id)
	if e != nil {
		return WorkOrder{}, e
	}
	defer rows.Close()
	for rows.Next() {
		var event WorkOrderEvent
		if e = rows.Scan(&event.ID, &event.WorkOrderID, &event.FromStatus, &event.ToStatus, &event.Type, &event.Actor, &event.Note, &event.CreatedAt); e != nil {
			return WorkOrder{}, e
		}
		order.Events = append(order.Events, event)
	}
	return order, nil
}
func (s *SQLStore) CreateWorkOrder(ctx context.Context, order WorkOrder) (WorkOrder, error) {
	if order.ID == "" {
		order.ID = fmt.Sprintf("WO-%d", time.Now().UnixNano())
	}
	if order.CreatedAt == "" {
		order.CreatedAt = nowUTC()
	}
	order.UpdatedAt = order.CreatedAt
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return WorkOrder{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO work_orders (id,customer_id,customer_name,phone,address,category,description,priority,status,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)`, order.ID, order.CustomerID, order.CustomerName, order.Phone, order.Address, order.Category, order.Description, order.Priority, WorkOrderPending, order.CreatedAt, order.UpdatedAt)
	if err != nil {
		return WorkOrder{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO work_order_events (id,work_order_id,from_status,to_status,type,actor,created_at) VALUES (?,?,?,?,?,?,?)`, fmt.Sprintf("WOEV-%d", time.Now().UnixNano()), order.ID, "", WorkOrderPending, "created", "system", order.CreatedAt)
	if err != nil {
		return WorkOrder{}, err
	}
	if err = tx.Commit(); err != nil {
		return WorkOrder{}, err
	}
	return s.GetWorkOrder(ctx, order.ID)
}
func (s *SQLStore) UpdateWorkOrderStatus(ctx context.Context, id, status, actor string) (WorkOrder, WorkOrderEvent, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return WorkOrder{}, WorkOrderEvent{}, err
	}
	defer tx.Rollback()
	var order WorkOrder
	if err = tx.QueryRowContext(ctx, `SELECT id,status,updated_at FROM work_orders WHERE id=? FOR UPDATE`, id).Scan(&order.ID, &order.Status, &order.UpdatedAt); errors.Is(err, sql.ErrNoRows) {
		return WorkOrder{}, WorkOrderEvent{}, ErrNotFound
	} else if err != nil {
		return WorkOrder{}, WorkOrderEvent{}, err
	}
	if !workOrderTransitions[order.Status][status] {
		return WorkOrder{}, WorkOrderEvent{}, ErrInvalidTransition
	}
	old := order.Status
	ts := nowUTC()
	if _, err = tx.ExecContext(ctx, `UPDATE work_orders SET status=?,updated_at=? WHERE id=?`, status, ts, id); err != nil {
		return WorkOrder{}, WorkOrderEvent{}, err
	}
	event := WorkOrderEvent{ID: fmt.Sprintf("WOEV-%d", time.Now().UnixNano()), WorkOrderID: id, FromStatus: old, ToStatus: status, Type: "status", Actor: actor, CreatedAt: ts}
	if _, err = tx.ExecContext(ctx, `INSERT INTO work_order_events (id,work_order_id,from_status,to_status,type,actor,created_at) VALUES (?,?,?,?,?,?,?)`, event.ID, id, old, status, event.Type, actor, ts); err != nil {
		return WorkOrder{}, WorkOrderEvent{}, err
	}
	if err = tx.Commit(); err != nil {
		return WorkOrder{}, WorkOrderEvent{}, err
	}
	full, e := s.GetWorkOrder(ctx, id)
	return full, event, e
}
func (s *SQLStore) CreateWorkOrderQuote(ctx context.Context, id string, q Quote) (WorkOrder, WorkOrderEvent, error) {
	order, err := s.GetWorkOrder(ctx, id)
	if err != nil {
		return WorkOrder{}, WorkOrderEvent{}, err
	}
	if !workOrderTransitions[order.Status][WorkOrderQuoted] {
		return WorkOrder{}, WorkOrderEvent{}, ErrInvalidTransition
	}
	q.ID = fmt.Sprintf("QUOTE-%d", time.Now().UnixNano())
	q.WorkOrderID = id
	if q.CreatedAt == "" {
		q.CreatedAt = nowUTC()
	}
	if _, err = s.db.ExecContext(ctx, `INSERT INTO work_order_quotes (id,work_order_id,labor_cents,material_cents,total_cents,note,created_at) VALUES (?,?,?,?,?,?,?)`, q.ID, id, q.LaborCents, q.MaterialCents, q.TotalCents, q.Note, q.CreatedAt); err != nil {
		return WorkOrder{}, WorkOrderEvent{}, err
	}
	return s.UpdateWorkOrderStatus(ctx, id, WorkOrderQuoted, "报价专员")
}
func (s *SQLStore) DispatchWorkOrder(ctx context.Context, id string, d Dispatch) (WorkOrder, WorkOrderEvent, error) {
	order, err := s.GetWorkOrder(ctx, id)
	if err != nil {
		return WorkOrder{}, WorkOrderEvent{}, err
	}
	if !workOrderTransitions[order.Status][WorkOrderDispatched] {
		return WorkOrder{}, WorkOrderEvent{}, ErrInvalidTransition
	}
	d.ID = fmt.Sprintf("DISPATCH-%d", time.Now().UnixNano())
	d.WorkOrderID = id
	if d.CreatedAt == "" {
		d.CreatedAt = nowUTC()
	}
	if _, err = s.db.ExecContext(ctx, `INSERT INTO work_order_dispatches (id,work_order_id,technician,scheduled_at,note,created_at) VALUES (?,?,?,?,?,?)`, d.ID, id, d.Technician, d.ScheduledAt, d.Note, d.CreatedAt); err != nil {
		return WorkOrder{}, WorkOrderEvent{}, err
	}
	return s.UpdateWorkOrderStatus(ctx, id, WorkOrderDispatched, "调度员")
}
func (s *SQLStore) AcceptWorkOrder(ctx context.Context, id string, a Acceptance) (WorkOrder, WorkOrderEvent, error) {
	order, err := s.GetWorkOrder(ctx, id)
	if err != nil {
		return WorkOrder{}, WorkOrderEvent{}, err
	}
	if order.Status != WorkOrderOnSite && order.Status != WorkOrderAwaitingAcceptance {
		return WorkOrder{}, WorkOrderEvent{}, ErrInvalidTransition
	}
	a.ID = fmt.Sprintf("ACCEPT-%d", time.Now().UnixNano())
	a.WorkOrderID = id
	if a.AcceptedAt == "" {
		a.AcceptedAt = nowUTC()
	}
	if _, err = s.db.ExecContext(ctx, `INSERT INTO work_order_acceptances (id,work_order_id,result,customer_sign,accepted_at) VALUES (?,?,?,?,?)`, a.ID, id, a.Result, a.CustomerSign, a.AcceptedAt); err != nil {
		return WorkOrder{}, WorkOrderEvent{}, err
	}
	if order.Status == WorkOrderOnSite {
		if _, _, err = s.UpdateWorkOrderStatus(ctx, id, WorkOrderAwaitingAcceptance, "客户"); err != nil {
			return WorkOrder{}, WorkOrderEvent{}, err
		}
	}
	return s.UpdateWorkOrderStatus(ctx, id, WorkOrderClosed, a.CustomerSign)
}
func (s *SQLStore) CreateWorkOrderWarranty(ctx context.Context, id string, w Warranty) (WorkOrder, WorkOrderEvent, error) {
	order, err := s.GetWorkOrder(ctx, id)
	if err != nil {
		return WorkOrder{}, WorkOrderEvent{}, err
	}
	if order.Status != WorkOrderClosed {
		return WorkOrder{}, WorkOrderEvent{}, ErrInvalidTransition
	}
	w.ID = fmt.Sprintf("WAR-%d", time.Now().UnixNano())
	w.WorkOrderID = id
	if w.CreatedAt == "" {
		w.CreatedAt = nowUTC()
	}
	if _, err = s.db.ExecContext(ctx, `INSERT INTO work_order_warranties (id,work_order_id,expires_at,note,created_at) VALUES (?,?,?,?,?)`, w.ID, id, w.ExpiresAt, w.Note, w.CreatedAt); err != nil {
		return WorkOrder{}, WorkOrderEvent{}, err
	}
	full, e := s.GetWorkOrder(ctx, id)
	if e != nil {
		return WorkOrder{}, WorkOrderEvent{}, e
	}
	event := WorkOrderEvent{ID: fmt.Sprintf("WOEV-%d", time.Now().UnixNano()), WorkOrderID: id, FromStatus: order.Status, ToStatus: order.Status, Type: "warranty", Actor: "售后专员", Note: w.ExpiresAt, CreatedAt: w.CreatedAt}
	_, e = s.db.ExecContext(ctx, `INSERT INTO work_order_events (id,work_order_id,from_status,to_status,type,actor,note,created_at) VALUES (?,?,?,?,?,?,?,?)`, event.ID, id, order.Status, order.Status, event.Type, event.Actor, event.Note, event.CreatedAt)
	return full, event, e
}
