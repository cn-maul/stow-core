package app

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"stow-core/internal/store"
)

type nameInput struct {
	Name string `json:"name"`
}

type App struct {
	store      *store.Store
	engine     *gin.Engine
	keys       []string
	versionStr string
}

type stockInput struct {
	Quantity       int     `json:"quantity"`
	Note           string  `json:"note"`
	ExpirationDate *string `json:"expiration_date"`
}

func New(s *store.Store, version string, keys []string) *App {
	gin.SetMode(gin.DebugMode)
	a := &App{
		store:      s,
		engine:     gin.New(),
		keys:       keys,
		versionStr: version,
	}
	a.engine.Use(gin.Recovery())
	a.setup()
	return a
}

func (a *App) Handler() http.Handler {
	return a.engine
}

func (a *App) setup() {
	a.engine.Use(corsMiddleware)
	if len(a.keys) > 0 {
		a.engine.Use(authMiddleware(a.keys))
	}

	a.engine.GET("/health", a.health)
	a.engine.GET("/version", a.version)

	a.engine.GET("/api/items", a.listItems)
	a.engine.POST("/api/items", a.createItem)
	a.engine.GET("/api/items/:id", a.getItem)
	a.engine.PUT("/api/items/:id", a.updateItem)
	a.engine.DELETE("/api/items/:id", a.deleteItem)
	a.engine.POST("/api/items/:id/stock-in", a.stockIn)
	a.engine.POST("/api/items/:id/stock-out", a.stockOut)
	a.engine.POST("/api/items/:id/adjust", a.adjust)
	a.engine.GET("/api/items/:id/batches", a.listBatches)
	a.engine.GET("/api/items/:id/movements", a.listMovements)

	a.engine.GET("/api/categories", a.listCategories)
	a.engine.POST("/api/categories", a.createCategory)
	a.engine.GET("/api/categories/:id", a.getCategory)
	a.engine.PUT("/api/categories/:id", a.updateCategory)
	a.engine.DELETE("/api/categories/:id", a.deleteCategory)

	a.engine.GET("/api/locations", a.listLocations)
	a.engine.POST("/api/locations", a.createLocation)
	a.engine.GET("/api/locations/:id", a.getLocation)
	a.engine.PUT("/api/locations/:id", a.updateLocation)
	a.engine.DELETE("/api/locations/:id", a.deleteLocation)

	a.engine.GET("/api/export", a.export)
	a.engine.POST("/api/import", a.importData)
}

func corsMiddleware(c *gin.Context) {
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Stow-Key")
	if c.Request.Method == http.MethodOptions {
		c.AbortWithStatus(http.StatusNoContent)
		return
	}
	c.Next()
}

func authMiddleware(keys []string) gin.HandlerFunc {
	keySet := make(map[string]bool, len(keys))
	for _, k := range keys {
		keySet[k] = true
	}
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}
		key := c.GetHeader("X-Stow-Key")
		if key == "" {
			auth := c.GetHeader("Authorization")
			if strings.HasPrefix(auth, "Bearer ") {
				key = strings.TrimPrefix(auth, "Bearer ")
			}
		}
		if key == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing X-Stow-Key header"})
			return
		}
		if !keySet[key] {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid key"})
			return
		}
		c.Next()
	}
}

func (a *App) health(c *gin.Context) {
	if err := a.store.Ping(c.Request.Context()); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (a *App) version(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"version": a.versionStr})
}

func (a *App) export(c *gin.Context) {
	data, err := a.store.Export(c.Request.Context())
	if err != nil {
		handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, data)
}

func (a *App) importData(c *gin.Context) {
	var data store.ExportData
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<22)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&data); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request body must contain one JSON object"})
		return
	}
	if data.Version == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing version field"})
		return
	}
	if err := a.store.Import(c.Request.Context(), data); err != nil {
		handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "imported"})
}

func (a *App) listItems(c *gin.Context) {
	items, err := a.store.ListItems(c.Request.Context())
	if err != nil {
		handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, items)
}

func (a *App) createItem(c *gin.Context) {
	var input store.ItemInput
	if err := decodeJSON(c, &input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	item, err := a.store.CreateItem(c.Request.Context(), input)
	if err != nil {
		handleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (a *App) getItem(c *gin.Context) {
	id, ok := parseID(c, "item")
	if !ok {
		return
	}
	item, err := a.store.GetItem(c.Request.Context(), id)
	if err != nil {
		handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (a *App) updateItem(c *gin.Context) {
	id, ok := parseID(c, "item")
	if !ok {
		return
	}
	var input store.ItemInput
	if err := decodeJSON(c, &input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	item, err := a.store.UpdateItem(c.Request.Context(), id, input)
	if err != nil {
		handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (a *App) deleteItem(c *gin.Context) {
	id, ok := parseID(c, "item")
	if !ok {
		return
	}
	if err := a.store.DeleteItem(c.Request.Context(), id); err != nil {
		handleError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (a *App) stockIn(c *gin.Context) {
	id, ok := parseID(c, "item")
	if !ok {
		return
	}
	var input stockInput
	if err := decodeJSON(c, &input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := a.store.StockIn(c.Request.Context(), id, input.Quantity, input.Note, input.ExpirationDate)
	if err != nil {
		handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (a *App) stockOut(c *gin.Context) {
	id, ok := parseID(c, "item")
	if !ok {
		return
	}
	var input stockInput
	if err := decodeJSON(c, &input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := a.store.StockOut(c.Request.Context(), id, input.Quantity, input.Note)
	if err != nil {
		handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (a *App) adjust(c *gin.Context) {
	id, ok := parseID(c, "item")
	if !ok {
		return
	}
	var input stockInput
	if err := decodeJSON(c, &input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := a.store.Adjust(c.Request.Context(), id, input.Quantity, input.Note)
	if err != nil {
		handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (a *App) listBatches(c *gin.Context) {
	id, ok := parseID(c, "item")
	if !ok {
		return
	}
	batches, err := a.store.ListBatches(c.Request.Context(), id)
	if err != nil {
		handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, batches)
}

func (a *App) listMovements(c *gin.Context) {
	id, ok := parseID(c, "item")
	if !ok {
		return
	}
	movements, err := a.store.ListMovements(c.Request.Context(), id)
	if err != nil {
		handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, movements)
}

func (a *App) listCategories(c *gin.Context) {
	categories, err := a.store.ListCategories(c.Request.Context())
	if err != nil {
		handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, categories)
}

func (a *App) createCategory(c *gin.Context) {
	var input nameInput
	if err := decodeJSON(c, &input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	category, err := a.store.CreateCategory(c.Request.Context(), input.Name)
	if err != nil {
		handleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, category)
}

func (a *App) getCategory(c *gin.Context) {
	id, ok := parseID(c, "category")
	if !ok {
		return
	}
	category, err := a.store.GetCategory(c.Request.Context(), id)
	if err != nil {
		handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, category)
}

func (a *App) updateCategory(c *gin.Context) {
	id, ok := parseID(c, "category")
	if !ok {
		return
	}
	var input nameInput
	if err := decodeJSON(c, &input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	category, err := a.store.UpdateCategory(c.Request.Context(), id, input.Name)
	if err != nil {
		handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, category)
}

func (a *App) deleteCategory(c *gin.Context) {
	id, ok := parseID(c, "category")
	if !ok {
		return
	}
	if err := a.store.DeleteCategory(c.Request.Context(), id); err != nil {
		handleError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (a *App) listLocations(c *gin.Context) {
	locations, err := a.store.ListLocations(c.Request.Context())
	if err != nil {
		handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, locations)
}

func (a *App) createLocation(c *gin.Context) {
	var input nameInput
	if err := decodeJSON(c, &input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	location, err := a.store.CreateLocation(c.Request.Context(), input.Name)
	if err != nil {
		handleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, location)
}

func (a *App) getLocation(c *gin.Context) {
	id, ok := parseID(c, "location")
	if !ok {
		return
	}
	location, err := a.store.GetLocation(c.Request.Context(), id)
	if err != nil {
		handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, location)
}

func (a *App) updateLocation(c *gin.Context) {
	id, ok := parseID(c, "location")
	if !ok {
		return
	}
	var input nameInput
	if err := decodeJSON(c, &input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	location, err := a.store.UpdateLocation(c.Request.Context(), id, input.Name)
	if err != nil {
		handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, location)
}

func (a *App) deleteLocation(c *gin.Context) {
	id, ok := parseID(c, "location")
	if !ok {
		return
	}
	if err := a.store.DeleteLocation(c.Request.Context(), id); err != nil {
		handleError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func parseID(c *gin.Context, resource string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid " + resource + " id"})
		return 0, false
	}
	return id, true
}

func decodeJSON(c *gin.Context, target any) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("invalid JSON body")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}

func handleError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, store.ErrInvalidInput):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, store.ErrInsufficientStock):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	// Added by user later
	case errors.Is(err, store.ErrItemHasStock):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	// Added by user later
	case errors.Is(err, store.ErrInUse):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	// Added by user later
	case errors.Is(err, store.ErrDuplicate):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	// Added by user later
	case errors.Is(err, store.ErrExportError):
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	// Added by user later
	default:
		log.Printf("request failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
}
