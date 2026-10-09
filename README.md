# TaskQueue — Go Background Job API

A small, standard-library-only backend demonstrating REST APIs, goroutines, channels, synchronization, and graceful shutdown. Submit text and retrieve its word and Unicode character counts asynchronously.

## What it does

1. The client sends text to `POST /jobs`.
2. The API validates it, creates an ID, and places that ID in a buffered channel.
3. The API returns HTTP 202 with a `queued` job and a Location header.
4. One of two worker goroutines marks it `processing`, calculates counts, and marks it `completed`.
5. The client uses `GET /jobs/{id}` to retrieve the result.

Workers are fast, so the first status check may already show `completed`. This is expected. A goroutine is a concurrently executing function; the channel distributes work to available workers. An RWMutex protects the shared map. A WaitGroup waits for worker exit during shutdown.

| Endpoint | Authentication | Purpose |
|---|---|---|
| GET /health | None | Confirm server is running |
| POST /jobs | X-API-Key | Submit text; HTTP 202 |
| GET /jobs/{id} | X-API-Key | Retrieve status and result |

The queue holds up to 32 waiting jobs, in addition to jobs being processed. A full queue returns HTTP 503 with Retry-After: 1 rather than blocking the handler. The demo retains at most 1000 jobs per run. No duplicate-request protection is implemented: each submission is a new job.

Word count uses whitespace-separated tokens. Character count includes spaces and counts Unicode code points, not bytes or visible grapheme clusters. Input text is omitted from responses and removed from the stored job on completion. There is no database: a restart clears jobs and results. This is a single-process portfolio demo, with a shared API key and no per-user authorization, retries, real email sending, or production durability.

## 1. Open in VS Code on Windows

Extract the ZIP. In VS Code select File > Open Folder and choose the inner `taskqueue` folder containing `go.mod`. Keep it separate from PayTrack. Use the Go extension already installed for PayTrack. Select Terminal > New Terminal and use PowerShell.

## 2. Verify the code

Copy commands exactly; `./...` means the current package and subpackages.

```powershell
go version
gofmt -w main.go main_test.go
go vet ./...
go test -v -cover ./...
```

Expected: vet has no output and five tests pass. No coverage percentage is claimed until measured. This generation environment has no Go compiler: compilation and tests have NOT been executed here. Replace this note with your actual results after running them.

Optional race check in Linux/WSL, or Windows with CGO and a compatible C compiler:

```powershell
go test -race ./...
```

| Test | Behavior checked |
|---|---|
| TestJobLifecycle | Submission, result, Unicode counting, input privacy |
| TestValidationAndAuth | Bad input, authentication, health, missing IDs |
| TestQueueFull | Deterministic rejection without recording rejected job |
| TestShutdownDrainsJobs | Accepted work finishes; late work is rejected |
| TestConcurrentSubmissions | Twenty concurrent requests complete |

## 3. Start the backend (terminal 1)

```powershell
$env:API_KEY = 'local-taskqueue-secret-123456'
go run .
```

Server listens on port 8081, so it can run alongside PayTrack on 8080. Leave this terminal running. Waiting without returning a prompt is normal. If the port is occupied, stop the other TaskQueue instance.

## 4. Health check (terminal 2)

Open Terminal > New Terminal, then run:

```powershell
Invoke-RestMethod -Uri 'http://localhost:8081/health'
```

Expected: status `ok`.

## 5. Submit a job (terminal 2)

```powershell
$headers = @{ 'X-API-Key' = 'local-taskqueue-secret-123456' }
$body = @{ text = 'Go makes backend development fun' } | ConvertTo-Json

$job = Invoke-RestMethod -Uri 'http://localhost:8081/jobs' -Method Post -Headers $headers -ContentType 'application/json' -Body $body
$job | Format-List
```

Expected: generated ID, status `queued`, and a timestamp. HTTP 202 means accepted for background processing, not that processing has completed.

## 6. Retrieve the result

```powershell
$result = Invoke-RestMethod -Uri "http://localhost:8081/jobs/$($job.id)" -Headers $headers
$result | ConvertTo-Json -Depth 5
```

Expected completed result:

```json
{
  "id": "<generated ID>",
  "status": "completed",
  "result": { "words": 5, "characters": 32 },
  "created_at": "<timestamp>",
  "completed_at": "<timestamp>"
}
```

If it is queued or processing, repeat the retrieval command. Do not submit again just to check status.

## 7. Test invalid requests

Missing authentication (expected HTTP 401, shown as a PowerShell error):

```powershell
Invoke-RestMethod -Uri "http://localhost:8081/jobs/$($job.id)"
```

Blank text (expected HTTP 400):

```powershell
Invoke-RestMethod -Uri 'http://localhost:8081/jobs' -Method Post -Headers $headers -ContentType 'application/json' -Body '{"text":""}'
```

Unknown ID (expected HTTP 404):

```powershell
Invoke-RestMethod -Uri 'http://localhost:8081/jobs/not-a-real-id' -Headers $headers
```

The queue-full test deliberately starts no workers to fill the channel predictably. Small manual jobs run too quickly to guarantee a full queue.

## 8. Stop the backend

Press Ctrl+C in terminal 1. The HTTP server stops accepting requests, workers drain accepted jobs, and the program exits. Restart with `go run .` when needed. Old job IDs then return 404 because storage is in memory.

## 9. Upload to GitHub

Sign in as **saisugandhe-reddy**, the account used successfully for PayTrack. Create a new empty repository named **taskqueue-go-api**. Choose Public if desired. Do not initialize a README, .gitignore, or license on GitHub.

In the local taskqueue folder (not PayTrack), run:

```powershell
git init
git config user.name "Sai Sugandh Eppa"
git config user.email "YOUR_GITHUB_VERIFIED_OR_NOREPLY_EMAIL"
git add .
git status
git commit -m "Add Go background job API with worker pool and tests"
git branch -M main
git remote add origin https://github.com/saisugandhe-reddy/taskqueue-go-api.git
git push -u origin main
```

Replace the email placeholder. Use your GitHub verified email or private noreply commit email. Complete GitHub sign-in as saisugandhe-reddy if prompted. This guide does not create the remote repository for you; create it before pushing.

Verify at https://github.com/saisugandhe-reddy/taskqueue-go-api. Review the README and update the testing note with real results before publishing. Never add real keys or personal input data.

Later changes:

```powershell
git add .
git commit -m "Describe your change"
git push
```

## Optional Docker

With Docker Desktop running:

```powershell
docker build -t taskqueue .
docker run --rm -p 8081:8081 -e API_KEY=local-taskqueue-secret-123456 taskqueue
```

Stop the locally running server first to release port 8081. The Dockerfile has not been built in this generation environment. Add Docker to your resume only after building and running it.

## Read the code in this order

1. `Job` and `Result`: request status and output fields.
2. `Queue`: map for history, channel for pending IDs, mutex for synchronization.
3. `submit`: validation, job ID generation, nonblocking enqueue, HTTP 202/503.
4. `start`: worker pool, processing state, counts, completion state.
5. `get`: read stored status using a read lock.
6. `stop` and `main`: signal handling, HTTP shutdown, queue draining.
7. Tests: see each behavior checked through the HTTP handler.

Interview explanation: "I built an asynchronous Go API where clients submit text jobs and poll for results. A buffered channel feeds two workers. A mutex protects shared job state, and a full queue rejects submissions rather than creating unlimited goroutines. On shutdown, workers finish accepted work. The demo uses memory; a durable queue would be the next step."

## Resume entry after local verification

**TaskQueue — Background Job API | Golang, REST APIs, Goroutines, Channels**

- Built an asynchronous Go REST API with a two-worker pool, buffered job queue, and status polling endpoints.
- Implemented synchronized job tracking, queue overload handling, API key authentication, and shutdown draining; added tests for concurrency and invalid requests.

Do not claim a distributed queue, durable storage, real customers, throughput, race-free execution, or measured coverage unless implemented or verified. Suggested personal extension: add a SHA-256 text-digest job type with its own validation and tests.
