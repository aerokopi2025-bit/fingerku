package api

import (
	"encoding/json"
	"net/http"
)

type ErrorResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
}

type SuccessResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

type PaginatedResponse struct {
	Success bool        `json:"success"`
	Total   int64       `json:"total"`
	Limit   int         `json:"limit"`
	Offset  int         `json:"offset"`
	Data    interface{} `json:"data"`
}

func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func respondSuccess(w http.ResponseWriter, status int, data interface{}, message ...string) {
	msg := ""
	if len(message) > 0 {
		msg = message[0]
	}
	respondJSON(w, status, SuccessResponse{
		Success: true,
		Message: msg,
		Data:    data,
	})
}

func respondPaginated(w http.ResponseWriter, data interface{}, total int64, limit, offset int) {
	respondJSON(w, http.StatusOK, PaginatedResponse{
		Success: true,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
		Data:    data,
	})
}

func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, ErrorResponse{
		Success: false,
		Error:   message,
	})
}
