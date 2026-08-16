package coatwindow

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestServerLifecycleAndJSONBoundaries(t *testing.T) {
	clock := time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)
	ledger := newTestLedger(t)
	service := NewServiceWithOptions(ledger, ServiceOptions{
		Now:   func() time.Time { return clock },
		NewID: func() (string, error) { return "batch-http", nil },
	})
	server := httptest.NewServer(NewServer(service))
	defer server.Close()

	status, body := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/batches", `{"materialName":"Gel coat","startingVolumeMl":200,"potLifeSeconds":600}`)
	if status != http.StatusCreated {
		t.Fatalf("open status = %d, body = %s", status, body)
	}
	var opened BatchView
	decodeResponse(t, body, &opened)
	if opened.ClosedAt != nil || opened.Outcome != nil || opened.CloseNote != nil {
		t.Fatalf("open response included close fields: %s", body)
	}

	status, body = requestJSON(t, server.Client(), http.MethodPost, server.URL+"/batches/batch-http/applications", `{"id":"first","areaLabel":"mold","quantityMl":75}`)
	if status != http.StatusOK {
		t.Fatalf("apply status = %d, body = %s", status, body)
	}
	status, body = requestJSON(t, server.Client(), http.MethodPost, server.URL+"/batches/batch-http/applications", `{"id":"second","areaLabel":"mold","quantityMl":1} {"id":"third"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("trailing JSON status = %d, body = %s", status, body)
	}
	status, body = requestJSON(t, server.Client(), http.MethodPost, server.URL+"/batches/batch-http/applications", `{"id":"first","areaLabel":"mold","quantityMl":1}`)
	if status != http.StatusConflict {
		t.Fatalf("duplicate status = %d, body = %s", status, body)
	}

	status, body = requestJSON(t, server.Client(), http.MethodPost, server.URL+"/batches/batch-http/close", `{"note":"ready"}`)
	if status != http.StatusOK {
		t.Fatalf("close status = %d, body = %s", status, body)
	}
	var closed BatchView
	decodeResponse(t, body, &closed)
	if closed.Outcome == nil || *closed.Outcome != OutcomeDiscardedWithRemainder || closed.CloseNote == nil || *closed.CloseNote != "ready" {
		t.Fatalf("close response = %s", body)
	}

	status, body = requestJSON(t, server.Client(), http.MethodGet, server.URL+"/batches/batch-http", "")
	if status != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", status, body)
	}
	var retrieved BatchView
	decodeResponse(t, body, &retrieved)
	if len(retrieved.Applications) != 1 || retrieved.RemainingVolumeML != 125 {
		t.Fatalf("retrieved response = %s", body)
	}
}

func TestServerRejectsInvalidOpenAndReportsMissingBatch(t *testing.T) {
	ledger := newTestLedger(t)
	server := httptest.NewServer(NewServer(NewService(ledger)))
	defer server.Close()

	status, body := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/batches", `{"materialName":"","startingVolumeMl":0,"potLifeSeconds":0}`)
	if status != http.StatusBadRequest || body == "" {
		t.Fatalf("invalid open status = %d, body = %s", status, body)
	}
	status, body = requestJSON(t, server.Client(), http.MethodGet, server.URL+"/batches/missing", "")
	if status != http.StatusNotFound || body == "" {
		t.Fatalf("missing get status = %d, body = %s", status, body)
	}
}

func TestLedgerPersistsSuccessfulWorkflow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.json")
	ledger, err := NewLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, time.August, 17, 13, 0, 0, 0, time.UTC)
	service := NewServiceWithOptions(ledger, ServiceOptions{
		Now:   func() time.Time { return clock },
		NewID: func() (string, error) { return "persisted", nil },
	})
	if _, err := service.OpenBatch("Primer", 50, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply("persisted", "coat", "panel", 50); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Close("persisted", nil); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	view, err := NewService(reloaded).Get("persisted")
	if err != nil {
		t.Fatal(err)
	}
	if view.Outcome == nil || *view.Outcome != OutcomeFullyApplied || view.RemainingVolumeML != 0 {
		t.Fatalf("reloaded view = %#v", view)
	}
}

func requestJSON(t *testing.T, client *http.Client, method, url, body string) (int, string) {
	t.Helper()
	request, err := http.NewRequest(method, url, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var data bytes.Buffer
	if _, err := data.ReadFrom(response.Body); err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, data.String()
}

func decodeResponse(t *testing.T, body string, destination any) {
	t.Helper()
	if err := json.Unmarshal([]byte(body), destination); err != nil {
		t.Fatalf("decode response: %v (%s)", err, body)
	}
}
