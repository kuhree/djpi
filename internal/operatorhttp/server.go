// Package operatorhttp serves a read-only operational view of a djpi station.
package operatorhttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	stationstatus "git.kuhree.com/kuhree.com/djpi/internal/status"
)

type ReportProvider func() (stationstatus.Report, error)

// Handler returns the complete operator HTTP surface. It has no mutation
// routes and never receives destination credentials.
func Handler(provider ReportProvider) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(writer http.ResponseWriter, _ *http.Request) {
		if _, err := provider(); err != nil {
			writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
			return
		}
		writeJSON(writer, http.StatusOK, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("GET /api/v1/status", func(writer http.ResponseWriter, _ *http.Request) {
		report, err := provider()
		if err != nil {
			writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "station status unavailable"})
			return
		}
		writeJSON(writer, http.StatusOK, report)
	})
	mux.HandleFunc("GET /", func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(indexHTML))
	})
	return securityHeaders(mux)
}

// ListenAndServe runs until context cancellation or a listener/server error.
func ListenAndServe(ctx context.Context, address string, handler http.Handler) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("listen for operator status: %w", err)
	}
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	result := make(chan error, 1)
	go func() { result <- server.Serve(listener) }()
	select {
	case err := <-result:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("shut down operator status: %w", err)
		}
		err := <-result
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(writer, request)
	})
}

func writeJSON(writer http.ResponseWriter, statusCode int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	_ = json.NewEncoder(writer).Encode(value)
}

const indexHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>djpi station</title>
<style>
:root{color-scheme:dark;font-family:ui-monospace,SFMono-Regular,Menlo,monospace;background:#101513;color:#e8f0eb}body{max-width:62rem;margin:0 auto;padding:2rem}h1{font-size:1.4rem}header{display:flex;align-items:center;justify-content:space-between}.pill{padding:.3rem .6rem;border:1px solid #3f5d4b;border-radius:999px}.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(13rem,1fr));gap:1rem}.card{background:#18211d;border:1px solid #283b31;border-radius:.7rem;padding:1rem}.number{font-size:2rem;margin:.3rem 0}.muted{color:#9db0a4}pre{white-space:pre-wrap;word-break:break-word}a{color:#8ee6af}.bad{color:#ff9b8f}
</style>
</head>
<body>
<header><h1>djpi camera station</h1><span class="pill" id="freshness">loading</span></header>
<main id="content"><p>Reading station evidence…</p></main>
<script>
const esc=value=>String(value).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
async function refresh(){try{const response=await fetch('/api/v1/status',{cache:'no-store'});if(!response.ok)throw Error(response.status);const s=await response.json();const attached=s.attached_sources||[],j=s.journal||{},r=s.runtime||{state:'unknown'};document.getElementById('freshness').textContent=new Date(s.time).toLocaleString();const retry=r.retry_at?'\nretry at '+new Date(r.retry_at).toLocaleString():'';const detail=[r.camera,r.files?r.files+' files':null,r.message].filter(Boolean).join('\n')+retry;document.getElementById('content').innerHTML='<section class="grid"><div class="card"><div class="muted">station state</div><div class="number">'+esc(r.state||'unknown')+'</div></div><div class="card"><div class="muted">attached cameras</div><div class="number">'+attached.length+'</div></div><div class="card"><div class="muted">journal items</div><div class="number">'+(j.items||0)+'</div></div><div class="card"><div class="muted">verified deliveries</div><div class="number">'+(j.verified_deliveries||0)+'</div></div><div class="card"><div class="muted">failed deliveries</div><div class="number">'+(j.failed_deliveries||0)+'</div></div></section><h2>Current operation</h2><pre>'+esc(detail||'Waiting for a camera')+'</pre><h2>Sources</h2><pre>'+esc(attached.length?attached.map(x=>x.camera_model+'  '+x.block_path+'  '+(x.mountpoint||'not mounted')).join('\n'):'No camera attached')+'</pre><h2>Snapshots</h2><pre>'+esc((j.source_summaries||[]).map(x=>x.camera+'  '+x.items+' items  ready='+x.all_destinations_ready).join('\n')||'No completed snapshot yet')+'</pre><p class="muted">Cleanup is disabled. Verified delivery is never deletion or format authorization.</p>';}catch(error){document.getElementById('freshness').textContent='unavailable';document.getElementById('freshness').classList.add('bad');document.getElementById('content').innerHTML='<p class="bad">Station status is unavailable.</p>';}}
refresh();setInterval(refresh,5000);
</script>
</body>
</html>`
