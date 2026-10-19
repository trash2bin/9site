// Package healthz - ответ для проверок деплоя: версия бинарника и номер
// схемы, чтобы откат и рестарт проверяли не факт жизни процесса, а то, что
// поднялся именно ожидаемый бинарник на ожидаемой схеме.
package healthz

import (
	"encoding/json"
	"net/http"
	"time"
)

// Handler - обработчик /healthz: status, version, schema, uptime.
func Handler(version string, schema int) http.Handler {
	start := time.Now()
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  "ok",
			"version": version,
			"schema":  schema,
			"uptime":  time.Since(start).Round(time.Second).String(),
		})
	})
}
