# CoatWindow

CoatWindow is a small Go service for tracking mixed coating batches during their pot-life window. It records the material volume, ordered applications, expiry, and one immutable closing outcome in a local JSON ledger.

The service uses only the Go standard library and requires Go 1.22 or newer.

## Run

Start the HTTP service with:

```text
go run ./cmd/coatwindow -ledger coatwindow-ledger.json
```

The default address is `:8080`. Use `-addr` to choose another listen address.

## API

Open a batch:

```text
POST /batches
Content-Type: application/json

{"materialName":"Harbor enamel","startingVolumeMl":500,"potLifeSeconds":3600}
```

Record an application while the batch is working and unexpired:

```text
POST /batches/{id}/applications
Content-Type: application/json

{"id":"hull-panel","areaLabel":"port hull panel","quantityMl":180}
```

Close a batch once:

```text
POST /batches/{id}/close
Content-Type: application/json

{"note":"held for inspection"}
```

Retrieve a batch with `GET /batches/{id}`. Before closure, `closedAt`, `outcome`, and `closeNote` are absent. Closing derives `fully-applied`, `expired-with-remainder`, or `discarded-with-remainder`; a closed record cannot be changed.

Invalid JSON, including trailing JSON values, returns a JSON error with a 400 status. Missing batches return 404, state conflicts return 409, and ledger failures return 500.

## Validate

```text
go test ./...
go run ./cmd/coatwindow -smoke
```
