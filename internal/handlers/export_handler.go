package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/store"
)

// ExportHandler handles dashboard export and import operations.
type ExportHandler struct {
	exportStore *store.ExportStore
}

// NewExportHandler creates a new ExportHandler.
func NewExportHandler(exportStore *store.ExportStore) *ExportHandler {
	return &ExportHandler{exportStore: exportStore}
}

// Export serialises the entire dashboard state as a downloadable JSON file.
func (h *ExportHandler) Export(w http.ResponseWriter, r *http.Request) {
	data, err := h.exportStore.ExportAll()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Failed to export data: " + err.Error(),
		})
		return
	}

	jsonBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Failed to marshal export data: " + err.Error(),
		})
		return
	}

	filename := fmt.Sprintf("gopulse_export_%s.json", time.Now().Format("2006-01-02_150405"))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.WriteHeader(http.StatusOK)
	w.Write(jsonBytes)
}

// ExportPreview returns entity counts without downloading the file.
func (h *ExportHandler) ExportPreview(w http.ResponseWriter, r *http.Request) {
	data, err := h.exportStore.ExportAll()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Failed to read data: " + err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data": map[string]int{
			"servers":   len(data.Servers),
			"projects":  len(data.Projects),
			"databases": len(data.Databases),
			"domains":   len(data.Domains),
			"users":     len(data.Users),
			"cron_jobs": len(data.CronJobs),
		},
	})
}

// Import reads an uploaded JSON file and imports the data into the stores.
func (h *ExportHandler) Import(w http.ResponseWriter, r *http.Request) {
	// Limit upload to 50 MB.
	r.Body = http.MaxBytesReader(w, r.Body, 50<<20)

	if err := r.ParseMultipartForm(50 << 20); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Failed to parse upload: " + err.Error(),
		})
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Missing 'file' field in upload: " + err.Error(),
		})
		return
	}
	defer file.Close()

	body, err := io.ReadAll(file)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Failed to read uploaded file: " + err.Error(),
		})
		return
	}

	var exportData models.GoPulseExport
	if err := json.Unmarshal(body, &exportData); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Invalid JSON format: " + err.Error(),
		})
		return
	}

	// Validate version.
	if exportData.Version == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Missing or invalid export version. Is this a valid GoPulse export file?",
		})
		return
	}

	mode := r.FormValue("mode")
	if mode == "" {
		mode = "skip"
	}

	report, err := h.exportStore.ImportAll(&exportData, mode)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    report,
	})
}
