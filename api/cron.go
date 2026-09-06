package handler

import (
	"encoding/json"
	"net/http"

	"notif-layer/pkg/check"
)

func Handler(w http.ResponseWriter, r *http.Request) {
	result, err := check.Run()
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":    false,
			"error": err.Error(),
			"data":  result,
		})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":   true,
		"data": result,
	})
}
