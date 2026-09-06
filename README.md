# Lyrarma Cloud

<p align="center">
</p>

<h1 align="center">Lyrarma Cloud</h1>
<p align="center">
  <strong>Fast open-source cloud platform</strong>
</p>

<p align="center">
  <a href="#"><img src="https://img.shields.io/badge/release-v1.0.1-lightgrey.svg" alt=""></a>
  <a href="#"><img src="https://img.shields.io/badge/Go-v1.26.5-blue.svg" alt="Go"></a>
  <a href="#"><img src="https://img.shields.io/badge/license-MIT-green.svg" alt="License"></a>
  <a href="#"><img src="https://img.shields.io/badge/PRs-welcome-brightgreen.svg" alt="PRs"></a>
</p>

## About

Lyrarma Cloud is an open-source file storage platform you can host and customize yourself. It provides a browser interface and an HTTP API for managing files, organizing folders, and sharing downloads.

The application runs as a single Go server. PostgreSQL stores accounts and file metadata, while an S3-compatible object store holds file contents. HTML pages are rendered on the server with Pongo2; the frontend uses plain JavaScript and CSS, with no separate frontend build step.

## Contents

- [Features](#features)
- [Requirements](#requirements)
- [Local setup](#local-setup)
- [Configuration](#configuration)
- [Using the web interface](#using-the-web-interface)
- [HTTP API](#http-api)
- [Project structure](#project-structure)
- [Development](#development)
- [Troubleshooting](#troubleshooting)
- [License](#license)

## Features

- Account registration and sign-in with bcrypt password hashing and JWT authentication.
- File uploads through a file picker or drag and drop, including multiple-file selection in the browser.
- Nested folders, file downloads, and deletion of files or entire folder trees.
- Public links for individual files and ZIP downloads of shared folders.
- Private files and folders by default, with access controlled by their owner.
- English and Russian interfaces.
- A web app manifest and service worker for installation in supporting browsers and caching interface assets. File operations still require a connection to the server.
- Configurable upload limits, request timeouts, and session cookies.

## Requirements

| Component | Purpose |
| --- | --- |
| Go | The module declares Go **1.26.5** in [go.mod](go.mod). Use a toolchain that satisfies that requirement. |
| PostgreSQL | Stores users, folders, and file metadata. The included Compose file uses `postgres:17.11-alpine`. |
| Docker with Compose | Starts the bundled local PostgreSQL service. Optional if you already have a PostgreSQL instance. |
| S3-compatible storage | Stores uploaded file contents. An existing bucket and working credentials are required. |
| Git | Clones the repository. |

[compose.db.yaml](compose.db.yaml) starts **only PostgreSQL**. The Go server runs on your host, and S3 storage must be configured separately.

## Local setup

### 1. Clone the repository

```sh
git clone https://github.com/ersh04/lyrarma-cloud.git
cd lyrarma-cloud
```

Run the following commands from this directory so the server can find its configuration, templates, and static assets.

### 2. Create your configuration

Copy the template if you do not already have a `.env` file:

```sh
cp -n example.env .env
```

Edit `.env` before starting the server:

- Keep `DATABASE_URL` from the template if you use the bundled PostgreSQL service.
- Set your own `JWT_SECRET` and `BETA_TEST_KEY`.
- Replace `S3_BUCKET`, `AWS_REGION`, `AWS_ACCESS_KEY_ID`, and `AWS_SECRET_ACCESS_KEY` with your storage settings.
- Set `S3_ENDPOINT` to your provider's endpoint. Remove the example endpoint for standard AWS S3 endpoint resolution.
- Set `S3_USE_PATH_STYLE` according to your provider's addressing requirements; the application defaults to `true`.

The S3 settings in `example.env` are placeholders. The application does not create a bucket for you. Its credentials must allow object uploads, downloads, and deletion, including multipart upload operations for large files.

### 3. Start PostgreSQL

```sh
docker compose -f compose.db.yaml up -d --wait
```

The service binds to `127.0.0.1:5432` and persists its database in the `postgres-data` named volume. On startup, the application automatically creates the `users`, `files`, and `folders` tables if they do not exist. The database itself must already exist; Compose creates it for the bundled service.

For an existing PostgreSQL server, skip this step and set `DATABASE_URL` to that database's connection string.

### 4. Start the application

```sh
go mod download
go run ./src
```

Open [http://localhost:8080](http://localhost:8080). To check the HTTP server from another terminal:

```sh
curl http://localhost:8080/health
```

Expected response:

```json
{"status":"ok"}
```

This endpoint reports that the HTTP server is responding; it does not probe PostgreSQL or S3. Upload and download a small file through the interface to check the complete storage flow.

### 5. Stop the local services

Press `Ctrl+C` in the terminal running Go, then stop PostgreSQL:

```sh
docker compose -f compose.db.yaml down
```

This preserves the database volume. Adding `-v` would remove the volume and its data.

## Configuration

Settings are loaded in this order:

1. Non-empty process environment variables take precedence.
2. The first existing configuration file is read: `.env`, then `../.env`, then `example.env`, relative to the working directory. These files are not merged.
3. Unspecified settings use the defaults shown below.

Restart the application after changing configuration. Duration values use Go duration syntax, such as `30s`, `2m`, or `24h`, and must be positive.

### Application and storage

| Variable | Built-in default | Description |
| --- | --- | --- |
| `DATABASE_URL` | Required | PostgreSQL connection string. The template matches the bundled database. |
| `JWT_SECRET` | Required | Secret used to sign and verify authentication tokens. |
| `BETA_TEST_KEY` | Required | Registration key checked by the web registration form. |
| `JWT_TTL` | `24h` | Lifetime of issued JWTs and browser authentication cookies. |
| `MAX_UPLOAD_MB` | `1024` | Maximum size of one file in MiB (`1024 × 1024` bytes per unit). Must be a positive integer. |
| `S3_BUCKET` | Required | Existing bucket for file contents. |
| `AWS_REGION` | Required | Region passed to the S3 client. |
| `S3_ENDPOINT` | Unset | Custom S3 endpoint; unset uses the SDK's standard endpoint resolution. |
| `AWS_ACCESS_KEY_ID` | Unset | Explicit access key; must be supplied together with the secret key. |
| `AWS_SECRET_ACCESS_KEY` | Unset | Secret paired with the access key. If both keys are absent, the client uses the AWS SDK's default credential chain. |
| `S3_USE_PATH_STYLE` | `true` | Use path-style S3 addressing. Accepts `true` or `false`. |
| `DATA_DIR` | `./data` | Retained in the configuration, but the current implementation stores file contents in S3. Setting this does not enable local file storage. |
| `VIRUS_SCANNER_API_URL` | `http://localhost:3311/scan` | Loaded by the configuration parser but not connected to the upload handler. Setting it does not enable virus scanning. |

Objects are stored under `users/<userID>/files/<fileID>`. Original filenames, folder relationships, and access flags are stored in PostgreSQL. A complete backup therefore needs both the database and the S3 objects.

`BETA_TEST_KEY` is checked by the web form at `/register`. The current JSON endpoint `/api/auth/register` accepts registration without this key, so the key does not restrict registration across the entire application.

### HTTP server and web resources

| Variable | Built-in default | Description |
| --- | --- | --- |
| `SERVER_ADDRESS` | `:8080` | Listening address. Use `127.0.0.1:8080` to bind only to localhost. |
| `SERVER_READ_TIMEOUT` | `30s` | Request read timeout. The template overrides this to `30m`. |
| `SERVER_WRITE_TIMEOUT` | `60s` | Response write timeout. The template overrides this to `120s`. |
| `WEB_STATIC_DIR` | `web/static` | CSS, JavaScript, translations, and web app assets. |
| `WEB_ICONS_DIR` | `web/icons` | Icons and favicon directory. |
| `WEB_TEMPLATES_DIR` | `web/templates` | Pongo2 HTML templates. |
| `WEB_TRANSLATIONS_FILE` | `web/static/translations.json` | Interface translations in JSON format. |

### Browser sessions

| Variable | Built-in default | Description |
| --- | --- | --- |
| `SESSION_COOKIE_NAME` | `auth-session` | Name of the authentication cookie. |
| `SESSION_COOKIE_PATH` | `/` | Cookie path. |
| `SESSION_COOKIE_HTTP_ONLY` | `true` | Prevents JavaScript from reading the authentication cookie. |
| `SESSION_COOKIE_SECURE` | `false` | Limits the cookie to HTTPS when enabled. Leave disabled for the local HTTP setup. |
| `SESSION_COOKIE_SAME_SITE` | `Lax` | Accepts `Lax`, `Strict`, or `None`. |

## Using the web interface

1. Open `/register`, choose a username and password, and enter the configured `BETA_TEST_KEY`. The API checks a minimum username length of 3 bytes and password length of 8 bytes.
2. Sign in at `/login` to open `/dashboard`.
3. Create folders and open the folder where you want to upload files. Select files or drop them into the upload area.
4. Download files from the dashboard or enable public access to share them.
5. Share `/shared-file/<fileID>` for a file or `/shared-folder/<folderID>` for a folder. Visitors can download public content without signing in.

Making a folder public exposes its entire subtree through the folder ZIP download, including files whose individual `is_public` flag is false. Changing a folder's visibility does not rewrite the access flags of its descendants. Deleting a folder removes its nested folders and files; there is no recycle bin in the current implementation.

## HTTP API

The API is served by the same process as the web interface. Protected routes require:

```http
Authorization: Bearer <token>
```

The browser interface manages its session cookie separately. For direct API calls, obtain a token from `/api/auth/login` and send it in the authorization header.

### Endpoints

| Method | Path | Authentication | Purpose |
| --- | --- | --- | --- |
| `GET` | `/health` | No | HTTP server health response. |
| `POST` | `/api/auth/register` | No | Register with JSON `username` and `password`. |
| `POST` | `/api/auth/login` | No | Sign in with JSON `username` and `password`; returns `token` and `expires_at`. |
| `GET` | `/api/files?folder_id=root` | Bearer token | List files in one folder; omitted `folder_id` means `root`. |
| `POST` | `/api/files/upload?folder_id=root` | Bearer token | Upload one multipart form field named `file`. |
| `GET` | `/api/files/:fileID/info` | Bearer token | Get metadata for an owned file. |
| `GET` | `/api/files/:fileID/download` | Bearer token | Download an owned file. |
| `DELETE` | `/api/files/:fileID` | Bearer token | Delete an owned file. |
| `GET` | `/api/files/:fileID/change_permission/:isPublic` | Bearer token | Set file visibility to `true` or `false`. |
| `GET` | `/api/folders` | Bearer token | List all folders belonging to the user. |
| `POST` | `/api/folders/create` | Bearer token | Create a folder with form fields `name` and optional `parent_id`. |
| `POST` | `/api/folders/:folderID/change_permission/:isPublic` | Bearer token | Set folder visibility to `true` or `false`. |
| `DELETE` | `/api/folders/:folderID` | Bearer token | Delete a folder and its contents recursively. |
| `GET` | `/api/public/files/:fileID/info` | No | Get public file metadata. |
| `GET` | `/api/public/files/:fileID/download` | No | Download a public file. |
| `GET` | `/api/public/folders/:folderID/info` | No | Get public folder metadata. |
| `GET` | `/api/public/folders/:folderID/download` | No | Download a public folder tree as ZIP. |

The file visibility endpoint currently uses `GET`, while the folder visibility endpoint uses `POST`. The table reflects the implemented routes.

### Example requests

Register an example account:

```sh
curl -X POST http://localhost:8080/api/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"username":"demo","password":"example-password"}'
```

Sign in:

```sh
curl -X POST http://localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"demo","password":"example-password"}'
```

Copy the returned `token` value into a shell variable:

```sh
TOKEN='paste-token-here'
```

Create a folder at the root:

```sh
curl -X POST http://localhost:8080/api/folders/create \
  -H "Authorization: Bearer $TOKEN" \
  --data-urlencode 'name=Documents' \
  --data-urlencode 'parent_id=root'
```

Upload a file to the root folder, then list that folder's files. Replace `./document.pdf` with an existing local file; to upload into the folder you created, replace `root` with its returned `id`.

```sh
curl -X POST 'http://localhost:8080/api/files/upload?folder_id=root' \
  -H "Authorization: Bearer $TOKEN" \
  -F 'file=@./document.pdf'

curl 'http://localhost:8080/api/files?folder_id=root' \
  -H "Authorization: Bearer $TOKEN"
```

File and folder lists return an object with `files` or `folders`, plus `total`. Successful uploads and folder creation return the created entry, including its `id`. API handler errors generally use this structure:

```json
{
  "error": "invalid_credentials",
  "message": "invalid username or password"
}
```

Common statuses include `201` for creation, `400` for invalid input, `401` for authentication failures, `404` for unavailable files or folders, `409` for an existing username, and `413` for an oversized upload.

## Project structure

```text
.
├── src/
│   ├── main.go
│   ├── api/
│   ├── config/
│   ├── envloader/
│   ├── httpresponse/
│   ├── managers/
│   ├── middleware/
│   ├── models/
│   ├── storage/
│   └── webui/
├── web/
│   ├── templates/
│   ├── static/
│   └── icons/
├── compose.db.yaml
├── example.env
├── go.mod
└── run.sh
```

The `api` package registers routes, `managers` implements request handlers, and `storage` handles PostgreSQL metadata and S3 objects. The `webui` package renders browser pages and delegates operations to the API; `envloader` reads and validates configuration.

## Development

Run the server with `go run ./src` during development. To build a binary from the repository root:

```sh
mkdir -p bin
go build -o bin/lyrarma-cloud ./src
./bin/lyrarma-cloud
```

Keep the working directory at the repository root, or configure the `WEB_*` paths for your deployment. Templates and static assets are loaded from the filesystem and are not embedded in the binary. Restart after changing Go code or templates.

Useful Go checks:

```sh
go test ./...
go vet ./...
```

The repository currently contains no Go test files, so `go test ./...` primarily checks package compilation. A manual smoke check should cover registration, login, folder creation, upload, download, public sharing, and deletion with a disposable file.

Interface text lives in [web/static/translations.json](web/static/translations.json), page layouts in [web/templates](web/templates), and styles in [web/static/style.css](web/static/style.css). When contributing, describe the change and how you verified it in your pull request.

## Troubleshooting

| Symptom | What to check |
| --- | --- |
| A required-variable error on startup | Ensure the selected configuration file or process environment supplies `DATABASE_URL`, `JWT_SECRET`, `BETA_TEST_KEY`, `S3_BUCKET`, and `AWS_REGION`. |
| PostgreSQL connection refused | Run `docker compose -f compose.db.yaml ps` and `docker compose -f compose.db.yaml logs database`; confirm the database is healthy and `DATABASE_URL` matches its address. |
| PostgreSQL password authentication fails after changing Compose settings | An existing database volume keeps its original credentials. Update the database credentials or use the credentials with which it was initialized. |
| The server starts but uploads fail | Check the server output, bucket existence, endpoint, region, credentials, and S3 permissions. `/health` does not validate storage access. |
| Upload returns `413` | Check `MAX_UPLOAD_MB` and any request-body limit configured in front of the application. |
| Large transfers time out | Check `SERVER_READ_TIMEOUT`, `SERVER_WRITE_TIMEOUT`, and any proxy timeouts. |
| Templates or translations cannot be found | Start from the repository root or set the corresponding `WEB_*` paths. |
| Login does not persist over local HTTP | Check that `SESSION_COOKIE_SECURE=false` and the cookie path matches the application. |
| Port 8080 or 5432 is already in use | Change `SERVER_ADDRESS`, or adjust the database port mapping together with `DATABASE_URL`. |
| The registration form rejects the beta key | Use `BETA_TEST_KEY` from the active configuration and restart after changing it. |

## License

Lyrarma Cloud is distributed under the [MIT License](LICENSE).
