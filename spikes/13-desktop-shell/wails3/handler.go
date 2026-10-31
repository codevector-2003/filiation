package main

import (
	"encoding/json"
	"net/http"
	"os"
	"runtime"
	"sync"
	"time"
)

// result is what one run measures.
type result struct {
	Shell     string `json:"shell"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	StartupMS int64  `json:"startup_ms"` // process start to the page's first script running
	APIMS     int64  `json:"api_ms"`     // the page's round trip to a Go handler, as the page timed it
	Binary    int64  `json:"binary_bytes"`
}

// page is the window's whole content: it times a call to the Go side, then
// reports that it is up. fil's interface would be served the same way.
const page = `<!doctype html><html><head><meta charset="utf-8"><title>Filiation spike</title></head>
<body style="font-family:system-ui;padding:24px"><h1>Filiation desktop shell spike</h1><p id="s">loading</p>
<script>
(async () => {
  const t0 = performance.now();
  const r = await fetch('/api/ping');
  await r.json();
  const api = Math.round(performance.now() - t0);
  document.getElementById('s').textContent = 'API round trip ' + api + ' ms';
  await fetch('/api/ready?api=' + api, {method: 'POST'});
})();
</script></body></html>`

// newHandler serves the page and the two API calls. On ready it writes the
// result to $SPIKE_RESULT and, with $SPIKE_AUTOQUIT=1, quits the app.
func newHandler(start time.Time, shell string, quit func()) http.Handler {
	var once sync.Once
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(page))
	})
	mux.HandleFunc("GET /api/ping", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("POST /api/ready", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
		once.Do(func() {
			res := result{Shell: shell, OS: runtime.GOOS, Arch: runtime.GOARCH,
				StartupMS: time.Since(start).Milliseconds()}
			json.Unmarshal([]byte(r.URL.Query().Get("api")), &res.APIMS)
			if exe, err := os.Executable(); err == nil {
				if fi, err := os.Stat(exe); err == nil {
					res.Binary = fi.Size()
				}
			}
			if path := os.Getenv("SPIKE_RESULT"); path != "" {
				b, _ := json.MarshalIndent(res, "", "  ")
				os.WriteFile(path, b, 0o644)
			}
			if os.Getenv("SPIKE_AUTOQUIT") == "1" {
				go func() { time.Sleep(300 * time.Millisecond); quit() }()
			}
		})
	})
	return mux
}
