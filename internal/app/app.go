package app

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"stow-core/internal/store"
)

type nameInput struct {
	Name string `json:"name"`
}

type App struct {
	store *store.Store
	mux   *http.ServeMux
}

type stockInput struct {
	Quantity       int     `json:"quantity"`
	Note           string  `json:"note"`
	ExpirationDate *string `json:"expiration_date"`
}



func New(s *store.Store) *App {
	a := &App{store: s, mux: http.NewServeMux()}
	a.routes()
	return a
}

func (a *App) Handler() http.Handler {
	return corsMiddleware(a.mux)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
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
	a.mux.HandleFunc("GET /api/items/{id}/batches", a.listBatches)
	a.mux.HandleFunc("GET /api/items/{id}/movements", a.listMovements)

	a.mux.HandleFunc("GET /api/categories", a.listCategories)
	a.mux.HandleFunc("POST /api/categories", a.createCategory)
	a.mux.HandleFunc("GET /api/categories/{id}", a.getCategory)
	a.mux.HandleFunc("PUT /api/categories/{id}", a.updateCategory)
	a.mux.HandleFunc("DELETE /api/categories/{id}", a.deleteCategory)

	a.mux.HandleFunc("GET /api/locations", a.listLocations)
	a.mux.HandleFunc("POST /api/locations", a.createLocation)
	a.mux.HandleFunc("GET /api/locations/{id}", a.getLocation)
	a.mux.HandleFunc("PUT /api/locations/{id}", a.updateLocation)
	a.mux.HandleFunc("DELETE /api/locations/{id}", a.deleteLocation)
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
	id, ok := itemID(w, r)
	if !ok {
		return
	}
	var input stockInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item, err := a.store.StockIn(r.Context(), id, input.Quantity, input.Note, input.ExpirationDate)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *App) stockOut(w http.ResponseWriter, r *http.Request) {
	id, ok := itemID(w, r)
	if !ok {
		return
	}
	var input stockInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item, err := a.store.StockOut(r.Context(), id, input.Quantity, input.Note)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *App) adjust(w http.ResponseWriter, r *http.Request) {
	id, ok := itemID(w, r)
	if !ok {
		return
	}
	var input stockInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item, err := a.store.Adjust(r.Context(), id, input.Quantity, input.Note)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *App) listBatches(w http.ResponseWriter, r *http.Request) {
	id, ok := itemID(w, r)
	if !ok {
		return
	}
	batches, err := a.store.ListBatches(r.Context(), id)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, batches)
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

func (a *App) listCategories(w http.ResponseWriter, r *http.Request) {
	categories, err := a.store.ListCategories(r.Context())
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, categories)
}

func (a *App) createCategory(w http.ResponseWriter, r *http.Request) {
	var input nameInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	category, err := a.store.CreateCategory(r.Context(), input.Name)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, category)
}

func (a *App) getCategory(w http.ResponseWriter, r *http.Request) {
	id, ok := categoryID(w, r)
	if !ok {
		return
	}
	category, err := a.store.GetCategory(r.Context(), id)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, category)
}

func (a *App) updateCategory(w http.ResponseWriter, r *http.Request) {
	id, ok := categoryID(w, r)
	if !ok {
		return
	}
	var input nameInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	category, err := a.store.UpdateCategory(r.Context(), id, input.Name)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, category)
}

func (a *App) deleteCategory(w http.ResponseWriter, r *http.Request) {
	id, ok := categoryID(w, r)
	if !ok {
		return
	}
	if err := a.store.DeleteCategory(r.Context(), id); err != nil {
		handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) listLocations(w http.ResponseWriter, r *http.Request) {
	locations, err := a.store.ListLocations(r.Context())
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, locations)
}

func (a *App) createLocation(w http.ResponseWriter, r *http.Request) {
	var input nameInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	location, err := a.store.CreateLocation(r.Context(), input.Name)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, location)
}

func (a *App) getLocation(w http.ResponseWriter, r *http.Request) {
	id, ok := locationID(w, r)
	if !ok {
		return
	}
	location, err := a.store.GetLocation(r.Context(), id)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, location)
}

func (a *App) updateLocation(w http.ResponseWriter, r *http.Request) {
	id, ok := locationID(w, r)
	if !ok {
		return
	}
	var input nameInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	location, err := a.store.UpdateLocation(r.Context(), id, input.Name)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, location)
}

func (a *App) deleteLocation(w http.ResponseWriter, r *http.Request) {
	id, ok := locationID(w, r)
	if !ok {
		return
	}
	if err := a.store.DeleteLocation(r.Context(), id); err != nil {
		handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func itemID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid item id")
		return 0, false
	}
	return id, true
}

func categoryID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid category id")
		return 0, false
	}
	return id, true
}

func locationID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid location id")
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
	case errors.Is(err, store.ErrInUse):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrDuplicate):
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
