package coatwindow

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

type Server struct {
	service *Service
}

func NewServer(service *Service) http.Handler {
	return &Server{service: service}
}

type openRequest struct {
	MaterialName     string `json:"materialName"`
	StartingVolumeML *int64 `json:"startingVolumeMl"`
	PotLifeSeconds   *int64 `json:"potLifeSeconds"`
}

type applicationRequest struct {
	ID         string `json:"id"`
	AreaLabel  string `json:"areaLabel"`
	QuantityML *int64 `json:"quantityMl"`
}

type closeRequest struct {
	Note *string `json:"note"`
}

type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/batches" {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		s.open(w, r)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/batches/") {
		writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/batches/"), "/")
	if len(parts) == 1 && parts[0] != "" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		s.get(w, parts[0])
		return
	}
	if len(parts) == 2 && parts[0] != "" && parts[1] == "applications" {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		s.apply(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[0] != "" && parts[1] == "close" {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		s.close(w, r, parts[0])
		return
	}
	writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
}

func (s *Server) open(w http.ResponseWriter, r *http.Request) {
	var request openRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if request.StartingVolumeML == nil || request.PotLifeSeconds == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "startingVolumeMl and potLifeSeconds are required")
		return
	}
	if *request.PotLifeSeconds > int64((time.Duration(1<<63-1))/time.Second) {
		writeError(w, http.StatusBadRequest, "invalid_request", "potLifeSeconds is too large")
		return
	}
	view, err := s.service.OpenBatch(request.MaterialName, *request.StartingVolumeML, time.Duration(*request.PotLifeSeconds)*time.Second)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

func (s *Server) apply(w http.ResponseWriter, r *http.Request, batchID string) {
	var request applicationRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if request.QuantityML == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "quantityMl is required")
		return
	}
	view, err := s.service.Apply(batchID, request.ID, request.AreaLabel, *request.QuantityML)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) close(w http.ResponseWriter, r *http.Request, batchID string) {
	var request closeRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	view, err := s.service.Close(batchID, request.Note)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) get(w http.ResponseWriter, batchID string) {
	view, err := s.service.Get(batchID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func decodeJSON(r *http.Request, destination any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request must contain one JSON value")
		}
		return err
	}
	return nil
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrBatchNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, ErrBatchClosed), errors.Is(err, ErrBatchExpired), errors.Is(err, ErrDuplicateApplication), errors.Is(err, ErrInsufficientVolume):
		writeError(w, http.StatusConflict, "conflict", err.Error())
	case errors.Is(err, ErrLedgerStorage):
		writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
	case strings.Contains(err.Error(), "required"), strings.Contains(err.Error(), "positive"), strings.Contains(err.Error(), "too large"):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
	}
}

func methodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed")
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{Code: code, Message: message})
}
