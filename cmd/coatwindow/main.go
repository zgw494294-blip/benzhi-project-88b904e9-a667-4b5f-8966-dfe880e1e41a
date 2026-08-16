package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"coatwindow"
)

func main() {
	address := flag.String("addr", ":8080", "HTTP listen address")
	ledgerPath := flag.String("ledger", "coatwindow-ledger.json", "local JSON ledger path")
	smoke := flag.Bool("smoke", false, "run a bounded API workflow and exit")
	flag.Parse()

	if *smoke {
		if err := runSmoke(); err != nil {
			fmt.Fprintln(os.Stderr, "smoke:", err)
			os.Exit(1)
		}
		fmt.Println("smoke: ok")
		return
	}

	ledger, err := coatwindow.NewLedger(*ledgerPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open ledger:", err)
		os.Exit(1)
	}
	server := &http.Server{Addr: *address, Handler: coatwindow.NewServer(coatwindow.NewService(ledger))}
	fmt.Println("CoatWindow listening on", *address)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, "serve:", err)
		os.Exit(1)
	}
}

type smokeOpenResponse struct {
	ID string `json:"id"`
}

type smokeBatchResponse struct {
	Applications      []coatwindow.Application `json:"applications"`
	RemainingVolumeML int64                    `json:"remainingVolumeMl"`
	Outcome           *coatwindow.Outcome      `json:"outcome"`
	ClosedAt          *time.Time               `json:"closedAt"`
	CloseNote         *string                  `json:"closeNote"`
}

func runSmoke() error {
	temporary, err := os.CreateTemp("", "coatwindow-smoke-*.json")
	if err != nil {
		return err
	}
	ledgerPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		return err
	}
	defer os.Remove(ledgerPath)
	if err := os.Remove(ledgerPath); err != nil {
		return err
	}

	ledger, err := coatwindow.NewLedger(ledgerPath)
	if err != nil {
		return err
	}
	httpServer := httptest.NewServer(coatwindow.NewServer(coatwindow.NewService(ledger)))
	defer httpServer.Close()
	client := httpServer.Client()

	var opened smokeOpenResponse
	status, err := smokeJSON(client, http.MethodPost, httpServer.URL+"/batches", map[string]any{
		"materialName":     "Harbor enamel",
		"startingVolumeMl": 500,
		"potLifeSeconds":   3600,
	}, &opened)
	if err != nil {
		return err
	}
	if status != http.StatusCreated || opened.ID == "" {
		return fmt.Errorf("open returned status %d without an id", status)
	}

	status, err = smokeJSON(client, http.MethodPost, httpServer.URL+"/batches/"+opened.ID+"/applications", map[string]any{
		"id":         "hull-panel",
		"areaLabel":  "port hull panel",
		"quantityMl": 180,
	}, nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("apply returned status %d", status)
	}

	note := "held for inspection"
	var closed smokeBatchResponse
	status, err = smokeJSON(client, http.MethodPost, httpServer.URL+"/batches/"+opened.ID+"/close", map[string]any{"note": note}, &closed)
	if err != nil {
		return err
	}
	if status != http.StatusOK || closed.RemainingVolumeML != 320 || closed.Outcome == nil || *closed.Outcome != coatwindow.OutcomeDiscardedWithRemainder || closed.ClosedAt == nil || closed.CloseNote == nil || *closed.CloseNote != note {
		return errors.New("close did not produce the expected immutable outcome")
	}

	status, err = smokeJSON(client, http.MethodPost, httpServer.URL+"/batches/"+opened.ID+"/applications", map[string]any{
		"id":         "late-panel",
		"areaLabel":  "starboard hull panel",
		"quantityMl": 1,
	}, nil)
	if err != nil {
		return err
	}
	if status != http.StatusConflict {
		return fmt.Errorf("closed batch accepted a mutation with status %d", status)
	}

	var retrieved smokeBatchResponse
	status, err = smokeJSON(client, http.MethodGet, httpServer.URL+"/batches/"+opened.ID, nil, &retrieved)
	if err != nil {
		return err
	}
	if status != http.StatusOK || len(retrieved.Applications) != 1 || retrieved.RemainingVolumeML != 320 {
		return errors.New("retrieved record did not retain the immutable result")
	}
	reloaded, err := coatwindow.NewLedger(ledgerPath)
	if err != nil {
		return err
	}
	if _, err := coatwindow.NewService(reloaded).Get(opened.ID); err != nil {
		return fmt.Errorf("reloaded ledger: %w", err)
	}
	return nil
}

func smokeJSON(client *http.Client, method, url string, request any, response any) (int, error) {
	var body io.Reader
	if request != nil {
		data, err := json.Marshal(request)
		if err != nil {
			return 0, err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return 0, err
	}
	if request != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return res.StatusCode, err
	}
	if response != nil {
		if err := json.Unmarshal(data, response); err != nil {
			return res.StatusCode, err
		}
	}
	return res.StatusCode, nil
}
