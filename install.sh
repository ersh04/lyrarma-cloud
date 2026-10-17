#!/usr/bin/env bash
set -euo pipefail

INSTALL_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_TMP=""

fail() {
    printf 'ERROR: %s\n' "$*" >&2
    exit 1
}

as_root() {
    if [[ "$EUID" -eq 0 ]]; then
        "$@"
    else
        sudo "$@"
    fi
}

read_config_value() {
    local config_path="$1" config_key="$2"
    [[ -f "$config_path" ]] || return 0
    awk -v key="$config_key" '
        /^[[:space:]]*#/ { next }
        {
            separator = index($0, "=")
            if (!separator) next
            name = substr($0, 1, separator - 1)
            sub(/^[[:space:]]*export[[:space:]]+/, "", name)
            gsub(/^[[:space:]]+|[[:space:]]+$/, "", name)
            if (name != key) next
            value = substr($0, separator + 1)
            gsub(/^[[:space:]]+|[[:space:]]+$/, "", value)
            if (substr(value, 1, 1) == "\"" && substr(value, length(value), 1) == "\"")
                value = substr(value, 2, length(value) - 2)
        }
        END { print value }
    ' "$config_path"
}

install_go() {
    local required_version installed_version release_version archive_name archive_sha go_arch go_dir
    required_version="$(awk '$1 == "go" { print $2; exit }' "$INSTALL_DIR/go.mod")"
    [[ "$required_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail 'Cannot read the required Go version from go.mod'
    installed_version="$(GOTOOLCHAIN=local go version 2>/dev/null | awk '{ sub(/^go/, "", $3); print $3 }' || true)"
    if [[ "$installed_version" =~ ^[0-9]+\.[0-9]+(\.[0-9]+)?$ ]] && dpkg --compare-versions "$installed_version" ge "$required_version"; then
        printf 'Keeping Go %s\n' "$installed_version"
        return
    fi

    case "$(dpkg --print-architecture)" in
        amd64) go_arch=amd64 ;;
        arm64) go_arch=arm64 ;;
        *) fail 'Automatic Go installation supports amd64 and arm64 only' ;;
    esac
    curl --fail --silent --show-error --location 'https://go.dev/dl/?mode=json' -o "$INSTALL_TMP/go-releases.json"
    IFS=$'\t' read -r release_version archive_name archive_sha < <(
        jq -er --arg arch "$go_arch" '
            [.[] | select(.stable) | . as $release | .files[] |
             select(.os == "linux" and .arch == $arch and .kind == "archive") |
             [$release.version, .filename, .sha256]] | first | @tsv
        ' "$INSTALL_TMP/go-releases.json"
    )
    [[ "$release_version" =~ ^go[0-9]+\.[0-9]+\.[0-9]+$ && "$archive_sha" =~ ^[0-9a-f]{64}$ ]] || fail 'Invalid Go release metadata'
    [[ "$archive_name" == "$release_version.linux-$go_arch.tar.gz" ]] || fail 'Unexpected Go archive name'
    dpkg --compare-versions "${release_version#go}" ge "$required_version" || fail 'The latest stable Go release is older than go.mod requires'
    go_dir="/opt/lyrarma/$release_version"
    if [[ ! -e "$go_dir" ]]; then
        printf 'Installing %s from go.dev\n' "$release_version"
        curl --fail --silent --show-error --location "https://go.dev/dl/$archive_name" -o "$INSTALL_TMP/$archive_name"
        (cd -- "$INSTALL_TMP" && printf '%s  %s\n' "$archive_sha" "$archive_name" | sha256sum --check --status) || fail 'Go archive checksum verification failed'
        tar -xzf "$INSTALL_TMP/$archive_name" --no-same-owner -C "$INSTALL_TMP"
        as_root install -d -m 755 /opt/lyrarma /usr/local/bin
        as_root mv -- "$INSTALL_TMP/go" "$go_dir"
        as_root chown -R root:root "$go_dir"
    fi
    [[ -x "$go_dir/bin/go" ]] || fail "Incomplete Go installation at $go_dir"
    as_root ln -sfn "$go_dir/bin/go" /usr/local/bin/go
    as_root ln -sfn "$go_dir/bin/gofmt" /usr/local/bin/gofmt
    export PATH="/usr/local/bin:$PATH"
    hash -r
    go version
}

install_docker() {
    local package docker_codename docker_arch
    if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
        printf 'Keeping the existing Docker and Compose installation\n'
    else
        for package in docker.io docker-compose docker-compose-v2 docker-doc docker-buildx podman-docker containerd runc; do
            if dpkg-query -W -f='${db:Status-Status}' "$package" 2>/dev/null | grep -qx installed; then
                fail "Package $package conflicts with Docker CE; configure Docker Compose manually or use --skip-docker"
            fi
        done
        docker_codename="${UBUNTU_CODENAME:-${VERSION_CODENAME:-}}"
        [[ "$docker_codename" =~ ^[a-z][a-z0-9-]*$ ]] || fail 'Cannot determine the distribution codename for Docker'
        docker_arch="$(dpkg --print-architecture)"
        if ! apt-cache policy docker-ce | grep -Eq 'Candidate: [0-9]'; then
            curl --fail --silent --show-error --location "https://download.docker.com/linux/$ID/gpg" -o "$INSTALL_TMP/docker.asc"
            as_root install -d -m 755 /etc/apt/keyrings
            as_root install -m 644 "$INSTALL_TMP/docker.asc" /etc/apt/keyrings/lyrarma-docker.asc
            printf 'Types: deb\nURIs: https://download.docker.com/linux/%s\nSuites: %s\nComponents: stable\nArchitectures: %s\nSigned-By: /etc/apt/keyrings/lyrarma-docker.asc\n' \
                "$ID" "$docker_codename" "$docker_arch" > "$INSTALL_TMP/docker.sources"
            as_root install -m 644 "$INSTALL_TMP/docker.sources" /etc/apt/sources.list.d/lyrarma-docker.sources
            as_root apt-get update
        fi
        as_root env DEBIAN_FRONTEND=noninteractive apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
    fi
    as_root systemctl enable --now docker
    docker compose version
}

prepare_config() {
    if [[ -e "$config_path" ]]; then
        printf 'Keeping existing configuration: %s\n' "$config_path"
        return
    fi
    local jwt_secret beta_key
    jwt_secret="$(openssl rand -hex 32)"
    beta_key="$(openssl rand -hex 16)"
    (umask 077; set -o noclobber; sed \
        -e "s|^JWT_SECRET=.*|JWT_SECRET=$jwt_secret|" \
        -e "s|^BETA_TEST_KEY=.*|BETA_TEST_KEY=$beta_key|" \
        -e "s|^SCANNER_ENABLED=.*|SCANNER_ENABLED=$scanner_enabled|" \
        -e 's|^SCANNER_ANTIVIRUS_COMMAND=.*|SCANNER_ANTIVIRUS_COMMAND="/usr/bin/clamdscan --stream"|' \
        "$INSTALL_DIR/example.env" > "$config_path")
    printf 'Created configuration with generated secrets: %s\n' "$config_path"
}

install_scanner() {
    as_root env DEBIAN_FRONTEND=noninteractive apt-get install -y file clamav clamav-daemon clamdscan imagemagick ffmpeg p7zip-full
    for utility in file clamdscan identify ffprobe ffmpeg 7z; do
        command -v "$utility" >/dev/null 2>&1 || fail "Scanning utility $utility is unavailable after installation"
    done
    if [[ "$skip_clamav_update" == true ]]; then
        as_root systemctl stop clamav-freshclam
        printf 'ClamAV database update and readiness check skipped; install matching databases and signatures before enabling scans\n'
        return
    fi
    if [[ ! -s /var/lib/clamav/daily.cvd && ! -s /var/lib/clamav/daily.cld ]] ||
       [[ ! -s /var/lib/clamav/main.cvd && ! -s /var/lib/clamav/main.cld ]]; then
        as_root systemctl stop clamav-freshclam
        if ! as_root freshclam; then
            fail 'ClamAV database download failed; respect any CDN cooldown and rerun with --skip-clamav-update if installing databases manually'
        fi
    fi
    as_root systemctl enable --now clamav-freshclam
    as_root systemctl reset-failed clamav-daemon
    as_root systemctl enable --now clamav-daemon.socket
    if ! as_root systemctl enable --now clamav-daemon ||
       ! as_root timeout 120s /usr/bin/clamdscan --stream --no-summary /etc/hosts; then
        as_root journalctl -u clamav-daemon -n 30 --no-pager
        fail 'ClamAV is not ready; check its databases, signatures, configuration and available memory'
    fi
}

main() {
    local skip_docker=false skip_clamav_update=false scanner_enabled config_path
    for argument in "$@"; do
        case "$argument" in
            --skip-docker) skip_docker=true ;;
            --skip-clamav-update) skip_clamav_update=true ;;
            --help|-h)
                printf 'Usage: ./install.sh [--skip-docker] [--skip-clamav-update]\n\nInstall application dependencies on Debian/Ubuntu with systemd\nSCANNER_ENABLED and CONFIG_FILE follow the server configuration precedence\nExisting configuration and database volumes are preserved\n'
                return ;;
            *) fail "Unknown option: $argument" ;;
        esac
    done
    cd -- "$INSTALL_DIR"
    [[ -r /etc/os-release ]] || fail 'Cannot determine the operating system'
    . /etc/os-release
    case "${ID:-}" in
        debian|ubuntu) ;;
        *) fail 'This installer supports Debian and Ubuntu only' ;;
    esac
    [[ -d /run/systemd/system ]] || fail 'This installer requires a running systemd host'
    if [[ "$EUID" -ne 0 ]]; then
        command -v sudo >/dev/null 2>&1 || fail 'Install sudo or run the script as root'
        sudo -v
    fi
    config_path="${CONFIG_FILE:-$INSTALL_DIR/.env}"
    [[ ! -L "$config_path" ]] || fail 'The configuration file must not be a symlink'
    [[ ! -e "$config_path" || -f "$config_path" && -r "$config_path" ]] || fail 'The configuration must be a readable regular file'
    scanner_enabled="${SCANNER_ENABLED:-$(read_config_value "$config_path" SCANNER_ENABLED)}"
    scanner_enabled="$(printf '%s' "$scanner_enabled" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//')"
    case "$scanner_enabled" in
        ''|true|TRUE|True|t|T|1) scanner_enabled=true ;;
        false|FALSE|False|f|F|0) scanner_enabled=false ;;
        *) fail 'SCANNER_ENABLED must contain true or false' ;;
    esac
    INSTALL_TMP="$(mktemp -d /tmp/lyrarma-install.XXXXXXXX)"
    trap 'if [[ "${INSTALL_TMP:-}" == /tmp/lyrarma-install.* ]]; then rm -rf -- "$INSTALL_TMP"; fi' EXIT
    as_root apt-get update
    as_root env DEBIAN_FRONTEND=noninteractive apt-get install -y ca-certificates curl git jq lsof openssl tar gzip
    install_go
    if [[ "$skip_docker" == false ]]; then
        install_docker
    fi
    prepare_config
    if [[ "$scanner_enabled" == true ]]; then
        install_scanner
    else
        printf 'SCANNER_ENABLED=false: scanning dependencies and ClamAV setup skipped\n'
    fi
    go mod download
    printf '\nDependencies installed. Review %s, especially database URLs and S3 credentials, before starting the application\n' "$config_path"
    if [[ "$EUID" -ne 0 && "$skip_docker" == false ]] && ! docker info >/dev/null 2>&1; then
        printf 'Docker requires additional access for this user; follow the Docker post-install instructions before using run.sh\n'
    fi
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
