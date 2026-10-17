package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"mail-manager/internal/classifier"
	"mail-manager/internal/storage"
)

type MonitoringHandler struct{ db *storage.Storage }

func NewMonitoringHandler(db *storage.Storage) *MonitoringHandler { return &MonitoringHandler{db: db} }
func (h *MonitoringHandler) Register(r *gin.RouterGroup) {
	r.GET("/queue/stats", h.queueStats)
	r.GET("/queue/problems", h.problems)
	r.GET("/classifications", h.classifications)
	r.GET("/classifications/:id", h.classification)
	r.GET("/classification/schema", h.taxonomy)
	r.GET("/reports/daily", h.dailyReport)
}
func (h *MonitoringHandler) taxonomy(c *gin.Context) {
	c.JSON(200, gin.H{"version": 2, "categories": classifier.Categories, "importances": classifier.Importances, "spam_verdicts": classifier.SpamVerdicts, "action_types": classifier.ActionTypes, "labels_ru": gin.H{"categories": classifier.CategoryLabelsRU, "importance": classifier.ImportanceLabelsRU, "spam_verdict": classifier.SpamLabelsRU, "action_types": classifier.ActionLabelsRU}})
}
func (h *MonitoringHandler) queueStats(c *gin.Context) {
	stats, err := h.db.QueueStats(c.Request.Context())
	if err != nil {
		c.JSON(500, gin.H{"error": "database error"})
		return
	}
	c.JSON(200, stats)
}
func (h *MonitoringHandler) problems(c *gin.Context) {
	limit, ok := parseIntParam(c, "limit", 50, 1, 200)
	if !ok {
		return
	}
	items, err := h.db.ListProblems(c.Request.Context(), limit)
	if err != nil {
		c.JSON(500, gin.H{"error": "database error"})
		return
	}
	c.JSON(200, gin.H{"count": len(items), "items": items})
}
func (h *MonitoringHandler) classifications(c *gin.Context) {
	limit, ok := parseIntParam(c, "limit", 50, 1, 200)
	if !ok {
		return
	}
	offset, ok := parseIntParam(c, "offset", 0, 0, 1000000)
	if !ok {
		return
	}
	var report, review *bool
	for _, pair := range []struct {
		key string
		dst **bool
	}{{"include_in_report", &report}, {"needs_review", &review}} {
		if raw, exists := c.GetQuery(pair.key); exists {
			v, err := strconv.ParseBool(raw)
			if err != nil {
				c.JSON(400, gin.H{"error": "invalid " + pair.key})
				return
			}
			*pair.dst = &v
		}
	}
	items, total, err := h.db.ListClassifications(c.Request.Context(), storage.ClassificationFilter{Limit: limit, Offset: offset, Category: c.Query("category"), Importance: c.Query("importance"), SpamVerdict: c.Query("spam_verdict"), IncludeInReport: report, NeedsReview: review, Query: c.Query("q")})
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid filters"})
		return
	}
	c.JSON(200, gin.H{"items": items, "total": total, "count": len(items), "limit": limit, "offset": offset})
}
func (h *MonitoringHandler) classification(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	x, err := h.db.GetClassificationItem(c.Request.Context(), id)
	if err != nil {
		c.JSON(404, gin.H{"error": "classification not found"})
		return
	}
	c.JSON(200, x)
}
func (h *MonitoringHandler) dailyReport(c *gin.Context) {
	day := time.Now().UTC()
	if ds := c.Query("date"); ds != "" {
		parsed, err := time.Parse("2006-01-02", ds)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "date must be YYYY-MM-DD"})
			return
		}
		day = parsed
	}
	report, err := h.db.GetDailyReport(c.Request.Context(), day)
	if err != nil {
		c.JSON(500, gin.H{"error": "database error"})
		return
	}
	c.JSON(200, report)
}
func parseIntParam(c *gin.Context, name string, fallback, minimum, maximum int) (int, bool) {
	raw := c.Query(name)
	if raw == "" {
		return fallback, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < minimum || value > maximum {
		c.JSON(400, gin.H{"error": "invalid query parameter: " + name})
		return 0, false
	}
	return value, true
}
func parseID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(400, gin.H{"error": "invalid email ID"})
		return 0, false
	}
	return id, true
}
