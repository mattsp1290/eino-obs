package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"

	einoobs "github.com/mattsp1290/eino-obs"
	"github.com/mattsp1290/eino-obs/exporter/datadog"
)

func main() {
	var requests int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&requests, 1)
		body := io.Reader(r.Body)
		if r.Header.Get("Content-Encoding") == "gzip" {
			gzipReader, err := gzip.NewReader(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			defer gzipReader.Close()
			body = gzipReader
		}
		var payload struct {
			Spans []map[string]any `json:"spans"`
		}
		if err := json.NewDecoder(body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		fmt.Printf("fake Datadog intake received %d span(s)\n", len(payload.Spans))
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	exporter, err := datadog.New(datadog.Config{
		APIKey:                  "example-key",
		Endpoint:                server.URL,
		MLApp:                   "example-agent",
		Service:                 "example-agent",
		Env:                     "local",
		MaxRetriesOverride:      datadog.Int(0),
		BatchSizeOverride:       datadog.Int(2),
		MaxPayloadBytesOverride: datadog.Int(16 * 1024),
	})
	if err != nil {
		panic(err)
	}
	observer := einoobs.New(einoobs.Config{Exporter: exporter})

	ctx := einoobs.ContextWithCorrelation(context.Background(), einoobs.Correlation{
		TraceID:       "example-trace",
		ObservationID: "example-session",
		SessionID:     "example-session",
	})
	session := observer.StartSession(ctx, einoobs.SessionStart{Name: "datadog example"})
	run := session.StartRun(einoobs.RunStart{Name: "fake endpoint run"})
	run.End(einoobs.RunEnd{})
	session.End(einoobs.SessionEnd{})
	if err := observer.Flush(context.Background()); err != nil {
		panic(err)
	}
	if err := observer.Shutdown(context.Background()); err != nil {
		panic(err)
	}
	fmt.Printf("sent %d request(s)\n", atomic.LoadInt64(&requests))
}
