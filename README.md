# Lyrarma Cloud

<p align="center">
</p>

<h1 align="center">Lyrarma Cloud</h1>
<p align="center">
  <strong>Fast open-source cloud platform</strong>
</p>

<p align="center">
  <a href="#"><img src="https://img.shields.io/badge/release-v2.0.x-lightgrey.svg" alt=""></a>
  <a href="#"><img src="https://img.shields.io/badge/Go-v1.26.5-blue.svg" alt="Go"></a>
  <a href="#"><img src="https://img.shields.io/badge/license-MIT-green.svg" alt="License"></a>
  <a href="#"><img src="https://img.shields.io/badge/PRs-welcome-brightgreen.svg" alt="PRs"></a>
</p>

## About

Lyrarma Cloud is an open-source file storage platform you can host and customize yourself. It provides a browser interface and an HTTP API for managing files, organizing folders, and sharing downloads

The application runs as a single Go server. One PostgreSQL database stores cloud profiles and file metadata, while the dedicated `lyrarma-services-users` database stores authentication data. An S3-compatible object store holds file contents. HTML pages are rendered on the server with Pongo2; the frontend uses plain JavaScript and CSS, with no separate frontend build step

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

- Account registration and sign-in with bcrypt password hashing and JWT authentication
- File uploads through a file picker or drag and drop, including multiple-file selection in the browser
- Quarantine scanning with MIME detection, SHA-256, ClamAV, media validation, archive limits, and pluggable content moderation
- Nested folders, file downloads, and deletion of files or entire folder trees
- Public links for individual files and ZIP downloads of shared folders
- Private files and folders by default, with access controlled by their owner
- English and Russian interfaces
- A web app manifest and service worker for installation in supporting browsers and caching interface assets. File operations still require a connection to the server
- Configurable upload limits, request timeouts, and session cookies

## Requirements

| Component | Purpose |
| --- | --- |
| Go | The module declares Go **1.26.5** in [go.mod](go.mod). Use a toolchain that satisfies that requirement |
| PostgreSQL | Separately stores authentication data and cloud metadata. Both included Compose files use `postgres:17.11-alpine` |
| Docker with Compose | Starts the two bundled local PostgreSQL services. Optional if you already have PostgreSQL instances |
| S3-compatible storage | Stores uploaded file contents. Two existing buckets and working credentials are required |
| File scanning utilities | Requires `file`, `clamdscan`, ImageMagick `identify`, `ffprobe`, `ffmpeg`, and `7z` on the application host |
| Git | Clones the repository |

[compose.db.yaml](compose.db.yaml) and [compose.users_db.yaml](compose.users_db.yaml) start **only PostgreSQL**. The Go server runs on your host, and S3 storage must be configured separately

## Local setup

### 1. Clone the repository

```sh
git clone https://github.com/ersh04/lyrarma-cloud.git
cd lyrarma-cloud
```

Run the following commands from this directory so the server can find its configuration, templates, and static assets

### 2. Create your configuration

Copy the template if you do not already have a `.env` file:

```sh
cp -n example.env .env
chmod 600 .env
```

Edit `.env` before starting the server:

- Keep `DATABASE_URL` and `USERS_DATABASE_URL` from the template if you use the bundled PostgreSQL services
- Set your own `JWT_SECRET` and `BETA_TEST_KEY`
- Replace `S3_BUCKET`, `S3_QUARANTINE_BUCKET`, `AWS_REGION`, `AWS_ACCESS_KEY_ID`, and `AWS_SECRET_ACCESS_KEY` with your storage settings
- Install the scanning utilities and set `SCANNER_MODERATION_COMMAND` when semantic content moderation is required
- Set `S3_ENDPOINT` to your provider's endpoint. Remove the example endpoint for standard AWS S3 endpoint resolution
- Set `S3_USE_PATH_STYLE` according to your provider's addressing requirements; the application defaults to `true`

The S3 settings in `example.env` are placeholders. The application does not create buckets for you. The production and quarantine buckets must be different, and the credentials must allow uploads, downloads, copies, and deletion, including multipart upload operations for large files

To start both PostgreSQL databases and Lyrarma Cloud in one command, run:

```sh
./run.sh
```

The application starts in the background and writes its output to `log`. The commands below describe the equivalent manual startup

### 3. Start PostgreSQL

```sh
docker compose -f compose.db.yaml up -d --wait
docker compose -f compose.users_db.yaml up -d --wait
```

The cloud database binds to `127.0.0.1:5432`. The dedicated `lyrarma-services-users` database binds to `127.0.0.1:5433`. Each service uses its own named volume. On startup, the application creates or migrates cloud metadata tables in the first database and creates the authentication `users` table in the second database with exactly `id`, `username`, `password`, `email`, and `plan` columns. The `password` column contains a bcrypt hash, never plaintext, and `plan` is a non-empty PostgreSQL `TEXT[]` array. The cloud `users` table contains only `id` and `created_at`, so `id` is the sole value duplicated between the two user tables

Existing installations must first copy their account names and bcrypt hashes from the legacy cloud `users.password_hash` column into `lyrarma-services-users`, supplying a real email for every account. During startup, the application matches legacy records by username, copies each existing cloud ID into the authentication table, moves the existing cloud plan into the authentication plan array, verifies every cloud profile has an authentication account, and then removes `username`, `password_hash`, and `plan` from the cloud table. Accounts without an existing plan receive `DEFAULT_USER_PLAN`. If a matching authentication account is missing or a plan is not configured, startup stops before the legacy columns are removed

For existing PostgreSQL servers, skip this step and set `DATABASE_URL` and `USERS_DATABASE_URL` to their respective connection strings

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

This endpoint reports that the HTTP server is responding; it does not probe PostgreSQL or S3. Upload and download a small file through the interface to check the complete storage flow

### 5. Stop the local services

Press `Ctrl+C` in the terminal running Go, then stop PostgreSQL:

```sh
docker compose -f compose.db.yaml down
docker compose -f compose.users_db.yaml down
```

This preserves the database volume. Adding `-v` would remove the volume and its data

## Configuration

Settings are loaded in this order:

1. Non-empty process environment variables take precedence
2. `CONFIG_FILE` selects an explicit dotenv file when set; otherwise only `.env` in the working directory is considered
3. Unspecified non-sensitive settings use the defaults shown below

`example.env` is a template and is never loaded automatically. A dotenv file must be a regular file, no larger than 1 MiB, and inaccessible to group and other users (for example, mode `0600`). Restart the application after changing configuration. Duration values use Go duration syntax, such as `30s`, `2m`, or `24h`, and must be positive

### Application and storage

| Variable | Built-in default | Description |
| --- | --- | --- |
| `DATABASE_URL` | Required | PostgreSQL connection string. The template matches the bundled database |
| `USERS_DATABASE_URL` | Required | PostgreSQL connection string for the dedicated `lyrarma-services-users` authentication database |
| `JWT_SECRET` | Required | Secret used to sign and verify authentication tokens. It must contain at least 32 bytes and must not equal the template value |
| `BETA_TEST_KEY` | Required | Registration key required by both the web form and JSON API. It must contain at least 16 bytes and must not equal the template value |
| `JWT_TTL` | `24h` | Lifetime of issued JWTs and browser authentication cookies |
| `MAX_UPLOAD_MB` | Required | Absolute file-size ceiling in MiB. To allow files up to this ceiling, configure the reverse proxy request-body limit at least 1 MiB higher for multipart overhead; use a lower proxy limit only intentionally |
| `MAX_STORAGE_MB` | Required | Absolute per-user storage ceiling in MiB |
| `DEFAULT_USER_PLAN` | Required | Name placed in the initial plan list for new users and legacy accounts without a plan during migration |
| `USER_PLANS_JSON` | Required | JSON array of plan objects with `name`, `max_upload_mb`, and `max_storage_mb` |
| `S3_BUCKET` | Required | Existing bucket for file contents |
| `S3_QUARANTINE_BUCKET` | Required | Separate existing bucket for files awaiting a scan result |
| `AWS_REGION` | Required | Region passed to the S3 client |
| `S3_ENDPOINT` | Unset | Custom S3 endpoint; unset uses the SDK's standard endpoint resolution |
| `AWS_ACCESS_KEY_ID` | Unset | Explicit access key; must be supplied together with the secret key |
| `AWS_SECRET_ACCESS_KEY` | Unset | Secret paired with the access key. If both keys are absent, the client uses the AWS SDK's default credential chain |
| `S3_USE_PATH_STYLE` | `true` | Use path-style S3 addressing. Accepts `true` or `false` |
| `DATA_DIR` | `./data` | Retained in the configuration, but the current implementation stores file contents in S3. Setting this does not enable local file storage |

Quarantine and approved objects use `users/<userID>/files/<fileID>` in their respective buckets. Original filenames, folder relationships, access flags, scan status, hashes, user IDs, and active upload reservations are stored in the cloud PostgreSQL database. IDs, usernames, bcrypt password hashes, email addresses, and assigned plan arrays are stored in `lyrarma-services-users`. A complete backup therefore needs both databases and both S3 buckets

All limit values are positive integers in MiB. Plan limits cannot exceed their corresponding absolute limits, and a plan's file limit cannot exceed its storage quota. When an account has multiple plans, the highest storage limit and the highest upload limit are applied. Invalid names, duplicate plan definitions, unknown JSON properties, missing defaults, and invalid limits stop application startup

```dotenv
DEFAULT_USER_PLAN=user
USER_PLANS_JSON=[{"name":"user","max_upload_mb":1024,"max_storage_mb":10240},{"name":"premium","max_upload_mb":10240,"max_storage_mb":102400}]
```

Plan names must start with a lowercase ASCII letter, may contain lowercase letters, digits, `_`, and `-`, and may be up to 32 bytes long

To add and assign plans without changing the application code or database schema:

1. Append another object to `USER_PLANS_JSON`
2. Restart the application so every instance uses the same immutable catalog
3. Assign one or more plans in `lyrarma-services-users`:

```sql
UPDATE users
SET plan = ARRAY['user', 'enterprise-v2']::text[]
WHERE username = 'demo';
```

The database requires a non-empty, one-dimensional plan array without null values, while application startup verifies that every assigned plan exists in `USER_PLANS_JSON`. Before removing a plan from the JSON catalog, remove it from every user's array; otherwise the next startup fails safely. Clients cannot choose their own plans. Uploads reserve quota atomically before S3 transfer, so concurrent requests cannot overrun the configured storage allowance

### File scanning

New uploads are written only to the quarantine bucket with status `uploading`. PostgreSQL acts as the durable queue: workers claim files as `scanning`, run the configured utilities, and store `approved`, `blocked`, or `scan_failed`. Only approved files are copied to the production bucket and made downloadable. Blocked files are removed from quarantine, while failed scans stay quarantined for diagnosis or deletion

| Variable | Built-in default | Description |
| --- | --- | --- |
| `SCANNER_WORKERS` | `2` | Number of concurrent scanner workers |
| `SCANNER_POLL_INTERVAL` | `2s` | Delay before polling an empty queue again |
| `SCANNER_TIMEOUT` | `10m` | Maximum duration of one scan and the lease timeout for interrupted work |
| `SCANNER_FILE_COMMAND` | `file` | libmagic command used to detect the actual MIME type |
| `SCANNER_ANTIVIRUS_COMMAND` | `clamdscan` | ClamAV client command; exit code `1` blocks the file |
| `SCANNER_IMAGE_COMMAND` | `identify` | ImageMagick command used to validate images and read dimensions |
| `SCANNER_VIDEO_COMMAND` | `ffprobe` | Command used to validate audio and video streams |
| `SCANNER_FRAME_COMMAND` | `ffmpeg` | Command used to sample video frames for moderation |
| `SCANNER_ARCHIVE_COMMAND` | `7z` | Command used to list, test, and extract supported archives |
| `SCANNER_MODERATION_COMMAND` | Unset | Optional executable that receives a file path and detected MIME type. Exit code `0` allows content, `1` blocks it, and any other result produces `scan_failed`. Video moderation receives sampled JPEG frames |
| `SCANNER_MAX_ARCHIVE_DEPTH` | `5` | Maximum nested archive depth |
| `SCANNER_MAX_ARCHIVE_FILES` | `10000` | Maximum total file count across nested archives |
| `SCANNER_MAX_ARCHIVE_MB` | `5120` | Maximum declared unpacked size across nested archives in MiB |
| `SCANNER_MAX_IMAGE_PIXELS` | `100000000` | Maximum width multiplied by height for an uploaded image |
| `SCANNER_BLOCKED_MIME_TYPES` | Executable MIME types | Comma-separated denylist based on detected content rather than the submitted filename |

Utility lookup, nonzero error exits, timeouts, malformed output, and truncated output fail closed as `scan_failed`. The moderation command is intentionally provider-neutral so deployments can connect a local model, perceptual-hash service, or external moderation adapter

### HTTP server and web resources

| Variable | Built-in default | Description |
| --- | --- | --- |
| `SERVER_ADDRESS` | `:8080` | Listening address. Use `127.0.0.1:8080` to bind only to localhost |
| `SERVER_READ_TIMEOUT` | `30s` | Request read timeout. The template overrides this to `30m` |
| `SERVER_WRITE_TIMEOUT` | `60s` | Response write timeout. The template overrides this to `120s` |
| `WEB_STATIC_DIR` | `web/static` | CSS, JavaScript, translations, and web app assets |
| `WEB_ICONS_DIR` | `web/icons` | Icons and favicon directory |
| `WEB_TEMPLATES_DIR` | `web/templates` | Pongo2 HTML templates |
| `WEB_TRANSLATIONS_FILE` | `web/static/translations.json` | Interface translations in JSON format |

### Browser sessions

| Variable | Built-in default | Description |
| --- | --- | --- |
| `SESSION_COOKIE_NAME` | `auth-session` | Name of the authentication cookie |
| `SESSION_COOKIE_PATH` | `/` | Cookie path |
| `SESSION_COOKIE_HTTP_ONLY` | `true` | Prevents JavaScript from reading the authentication cookie |
| `SESSION_COOKIE_SECURE` | `false` | Limits the cookie to HTTPS when enabled. Leave disabled for the local HTTP setup |
| `SESSION_COOKIE_SAME_SITE` | `Lax` | Accepts `Lax`, `Strict`, or `None` |

## Using the web interface

1. Open `/register`, choose a username, email, and password, and enter the configured `BETA_TEST_KEY`. The API checks a minimum username length of 3 bytes, a valid email address, and password length of 8 bytes
2. Sign in at `/login` to open `/dashboard`
3. Create folders and open the folder where you want to upload files. Select files or drop them into the upload area
4. Wait for scanning to complete, then download approved files from the dashboard or enable public access to share them
5. Share `/shared-file/<fileID>` for a file or `/shared-folder/<folderID>` for a folder. Visitors can download public content without signing in

Making a folder public exposes its entire subtree through the folder ZIP download, including files whose individual `is_public` flag is false. Changing a folder's visibility does not rewrite the access flags of its descendants. Deleting a folder removes its nested folders and files; there is no recycle bin in the current implementation

## HTTP API

The API is served by the same process as the web interface. Protected routes require:

```http
Authorization: Bearer <token>
```

The browser interface manages its session cookie separately. For direct API calls, obtain a token from `/api/auth/login` and send it in the authorization header

### Endpoints

| Method | Path | Authentication | Purpose |
| --- | --- | --- | --- |
| `GET` | `/health` | No | HTTP server health response |
| `POST` | `/api/auth/register` | No | Register with JSON `username`, `email`, `password`, and `beta_key` |
| `POST` | `/api/auth/login` | No | Sign in with JSON `username` and `password`; returns `token` and `expires_at` |
| `GET` | `/api/files?folder_id=root` | Bearer token | List files in one folder; omitted `folder_id` means `root` |
| `POST` | `/api/files/upload?folder_id=root` | Bearer token | Upload one multipart form field named `file` to quarantine and return status `uploading` |
| `GET` | `/api/files/:fileID/info` | Bearer token | Get metadata for an owned file |
| `GET` | `/api/files/:fileID/download` | Bearer token | Download an approved owned file |
| `DELETE` | `/api/files/:fileID` | Bearer token | Delete an owned file |
| `GET` | `/api/files/:fileID/change_permission/:isPublic` | Bearer token | Set file visibility to `true` or `false` |
| `GET` | `/api/folders` | Bearer token | List all folders belonging to the user |
| `POST` | `/api/folders/create` | Bearer token | Create a folder with form fields `name` and optional `parent_id` |
| `POST` | `/api/folders/:folderID/change_permission/:isPublic` | Bearer token | Set folder visibility to `true` or `false` |
| `DELETE` | `/api/folders/:folderID` | Bearer token | Delete a folder and its contents recursively |
| `GET` | `/api/public/files/:fileID/info` | No | Get public file metadata |
| `GET` | `/api/public/files/:fileID/download` | No | Download an approved public file |
| `GET` | `/api/public/folders/:folderID/info` | No | Get public folder metadata |
| `GET` | `/api/public/folders/:folderID/download` | No | Download a public folder tree as ZIP |
| `GET` | `/api/info/max_upload_size` | No | Get the absolute system file-size ceiling in bytes |
| `GET` | `/api/info/limits` | Bearer token | Get the current user's plan list, effective limits, used storage, and reserved storage in bytes |

The file visibility endpoint currently uses `GET`, while the folder visibility endpoint uses `POST`. The table reflects the implemented routes

### Example requests

Register an example account:

```sh
curl -X POST http://localhost:8080/api/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"username":"demo","email":"demo@example.com","password":"example-password","beta_key":"your-beta-test-key"}'
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

Upload a file to the root folder, then list that folder's files. Replace `./document.pdf` with an existing local file; to upload into the folder you created, replace `root` with its returned `id`

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

Common statuses include `201` for creation, `400` for invalid input, `401` for authentication failures, `403` for an invalid registration key, `404` for unavailable files or folders, `409` for an existing username or email, `413` for an oversized file, and `507` when the user's storage quota is exhausted

## Project structure

```text
.
├── src/
│   ├── main.go
│   ├── api/
│   ├── config/
│   ├── httpresponse/
│   ├── handlers/
│   ├── middleware/
│   ├── models/
│   ├── scanner/
│   ├── storage/
│   └── webui/
├── web/
│   ├── templates/
│   ├── static/
│   └── icons/
├── compose.db.yaml
├── compose.users_db.yaml
├── example.env
├── go.mod
└── run.sh
```

The `config` package loads and validates one immutable settings snapshot. The `api` package registers routes, `handlers` implements request handling, `scanner` runs background content checks, and `storage` handles PostgreSQL metadata, quota reservations, and S3 objects. The `webui` package renders browser pages and delegates operations to the API

## Development

Run the server with `go run ./src` during development. To build a binary from the repository root:

```sh
mkdir -p bin
go build -o bin/lyrarma-cloud ./src
./bin/lyrarma-cloud
```

Keep the working directory at the repository root, or configure the `WEB_*` paths for your deployment. Templates and static assets are loaded from the filesystem and are not embedded in the binary. Restart after changing Go code or templates

Interface text lives in [web/static/translations.json](web/static/translations.json), page layouts in [web/templates](web/templates), and styles in [web/static/style.css](web/static/style.css). When contributing, describe the change and how you verified it in your pull request

## Troubleshooting

| Symptom | What to check |
| --- | --- |
| A required-variable error on startup | Ensure the selected configuration file or process environment supplies the database, secret, S3, absolute-limit, and plan-limit variables shown above |
| Configuration file permissions are rejected | Run `chmod 600 .env`, or provide settings through process environment variables |
| PostgreSQL connection refused | Check both Compose projects and confirm `DATABASE_URL` and `USERS_DATABASE_URL` match their addresses |
| PostgreSQL password authentication fails after changing Compose settings | An existing database volume keeps its original credentials. Update the database credentials or use the credentials with which it was initialized |
| The server starts but uploads fail | Check the server output, bucket existence, endpoint, region, credentials, and S3 permissions. `/health` does not validate storage access |
| Files remain in `scan_failed` | Check that every scanner utility is installed and executable, ClamAV can reach `clamd`, and the moderation adapter follows the documented exit-code contract |
| Upload returns `413` | Check the user's plan file limit, `MAX_UPLOAD_MB`, and the request-body limit configured in front of the application |
| Upload returns `507` | The sum of stored files and active upload reservations exceeds the user's plan storage quota |
| Large transfers time out | Check `SERVER_READ_TIMEOUT`, `SERVER_WRITE_TIMEOUT`, and any proxy timeouts |
| Templates or translations cannot be found | Start from the repository root or set the corresponding `WEB_*` paths |
| Login does not persist over local HTTP | Check that `SESSION_COOKIE_SECURE=false` and the cookie path matches the application |
| Port 8080, 5432, or 5433 is already in use | Change `SERVER_ADDRESS`, or adjust the corresponding database port mapping together with its URL |
| The registration form rejects the beta key | Use `BETA_TEST_KEY` from the active configuration and restart after changing it |

## License

Lyrarma Cloud is distributed under the [MIT License](LICENSE)
