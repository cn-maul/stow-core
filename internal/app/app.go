package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"stow-core/internal/store"
)

type App struct {
	store *store.Store
	mux   *http.ServeMux
}

type stockInput struct {
	Quantity int    `json:"quantity"`
	Note     string `json:"note"`
}

func New(s *store.Store) *App {
	a := &App{store: s, mux: http.NewServeMux()}
	a.routes()
	return a
}

func (a *App) Handler() http.Handler {
	return a.mux
}

func (a *App) routes() {
	a.mux.HandleFunc("GET /health", a.health)
	a.mux.HandleFunc("GET /api/items", a.listItems)
	a.mux.HandleFunc("POST /api/items", a.createItem)
	a.mux.HandleFunc("GET /api/items/{id}", a.getItem)
	a.mux.HandleFunc("PUT /api/items/{id}", a.updateItem)
	a.mux.HandleFunc("DELETE /api/items/{id}", a.deleteItem)
	a.mux.HandleFunc("POST /api/items/{id}/stock-in", a.stockIn)
	a.mux.HandleFunc("POST /api/items/{id}/stock-out", a.stockOut)
	a.mux.HandleFunc("POST /api/items/{id}/adjust", a.adjust)
	a.mux.HandleFunc("GET /api/items/{id}/movements", a.listMovements)
}

func (a *App) health(w http.ResponseWriter, r *http.Request) {
	if err := a.store.Ping(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) listItems(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.ListItems(r.Context())
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *App) createItem(w http.ResponseWriter, r *http.Request) {
	var input store.ItemInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Unit = strings.TrimSpace(input.Unit)
	item, err := a.store.CreateItem(r.Context(), input)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (a *App) getItem(w http.ResponseWriter, r *http.Request) {
	id, ok := itemID(w, r)
	if !ok {
		return
	}
	item, err := a.store.GetItem(r.Context(), id)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *App) updateItem(w http.ResponseWriter, r *http.Request) {
	id, ok := itemID(w, r)
	if !ok {
		return
	}
	var input store.ItemInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Unit = strings.TrimSpace(input.Unit)
	item, err := a.store.UpdateItem(r.Context(), id, input)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *App) deleteItem(w http.ResponseWriter, r *http.Request) {
	id, ok := itemID(w, r)
	if !ok {
		return
	}
	if err := a.store.DeleteItem(r.Context(), id); err != nil {
		handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) stockIn(w http.ResponseWriter, r *http.Request) {
	a.stockOperation(w, r, a.store.StockIn)
}

func (a *App) stockOut(w http.ResponseWriter, r *http.Request) {
	a.stockOperation(w, r, a.store.StockOut)
}

func (a *App) adjust(w http.ResponseWriter, r *http.Request) {
	a.stockOperation(w, r, a.store.Adjust)
}

func (a *App) stockOperation(
	w http.ResponseWriter,
	r *http.Request,
	operation func(context.Context, int64, int, string) (store.Item, error),
) {
	id, ok := itemID(w, r)
	if !ok {
		return
	}
	var input stockInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item, err := operation(r.Context(), id, input.Quantity, input.Note)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *App) listMovements(w http.ResponseWriter, r *http.Request) {
	id, ok := itemID(w, r)
	if !ok {
		return
	}
	movements, err := a.store.ListMovements(r.Context(), id)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, movements)
}

func itemID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid item id")
		return 0, false
	}
	return id, true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("invalid JSON body")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}

func handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrInsufficientStock):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrItemHasStock):
		writeError(w, http.StatusConflict, err.Error())
	default:
		log.Printf("request failed: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode response: %v", err)
	}
}
